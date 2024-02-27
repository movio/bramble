package bramble

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql"
	log "github.com/sirupsen/logrus"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
)

func NewExecutableSchema(plugins []Plugin, maxRequestsPerQuery int64, client *GraphQLClient, services ...*Service) *ExecutableSchema {
	serviceMap := make(map[string]*Service)

	for _, s := range services {
		serviceMap[s.ServiceURL] = s
	}

	if client == nil {
		client = NewClientWithPlugins(plugins)
	}

	return &ExecutableSchema{
		Services: serviceMap,

		GraphqlClient:       client,
		plugins:             plugins,
		tracer:              otel.GetTracerProvider().Tracer(instrumentationName),
		MaxRequestsPerQuery: maxRequestsPerQuery,
	}
}

// ExecutableSchema contains all the necessary information to execute queries
type ExecutableSchema struct {
	MergedSchema        *ast.Schema
	Locations           FieldURLMap
	IsBoundary          map[string]bool
	Services            map[string]*Service
	BoundaryQueries     BoundaryFieldsMap
	GraphqlClient       *GraphQLClient
	MaxRequestsPerQuery int64

	tracer  trace.Tracer
	mutex   sync.RWMutex
	plugins []Plugin
}

// UpdateServiceList replaces the list of services with the provided one and
// update the schema.
func (s *ExecutableSchema) UpdateServiceList(ctx context.Context, services []string) error {
	ctx, span := s.tracer.Start(ctx, "Federated Services Update",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.StringSlice("graphql.federation.services", services),
		),
	)

	defer span.End()

	newServices := make(map[string]*Service)
	for _, svcURL := range services {
		if svc, ok := s.Services[svcURL]; ok {
			newServices[svcURL] = svc
		} else {
			newServices[svcURL] = NewService(svcURL, WithHTTPClient(s.GraphqlClient.HTTPClient))
		}
	}
	s.Services = newServices

	return s.UpdateSchema(ctx, true)
}

// UpdateSchema updates the schema from every service and then update the merged
// schema.
func (s *ExecutableSchema) UpdateSchema(ctx context.Context, forceRebuild bool) error {
	var services []*Service
	var schemas []*ast.Schema
	var updatedServices []string
	var invalidSchema bool

	defer func() {
		if invalidSchema {
			promInvalidSchema.Set(1)
		} else {
			promInvalidSchema.Set(0)
		}
	}()

	// Only fetch at most 64 services in parallel
	var mutex sync.Mutex

	group := errgroup.Group{}
	// Avoid fetching more than 64 servides in parallel,
	// as high concurrency can actually hurt performance
	group.SetLimit(64)
	for url_, s_ := range s.Services {
		url := url_
		s := s_
		group.Go(func() error {
			logger := log.WithField("url", url)
			updated, err := s.Update(ctx)
			if err != nil {
				promServiceUpdateErrorCounter.WithLabelValues(s.ServiceURL).Inc()
				promServiceUpdateErrorGauge.WithLabelValues(s.ServiceURL).Set(1)
				invalidSchema, forceRebuild = true, true
				logger.WithError(err).Error("unable to update service")
				// Ignore this service in this update
				return nil
			}
			promServiceUpdateErrorGauge.WithLabelValues(s.ServiceURL).Set(0)
			logger = log.WithFields(log.Fields{
				"version": s.Version,
				"service": s.Name,
			})

			mutex.Lock()
			defer mutex.Unlock()
			if updated {
				logger.Info("service was updated")
				updatedServices = append(updatedServices, s.Name)
			}

			services = append(services, s)
			schemas = append(schemas, s.Schema)

			return nil
		})
	}

	group.Wait()

	if len(updatedServices) > 0 || forceRebuild {
		log.Info("rebuilding merged schema")
		schema, err := MergeSchemas(schemas...)
		if err != nil {
			invalidSchema = true
			return fmt.Errorf("update of service %v caused schema error: %w", updatedServices, err)
		}

		boundaryQueries := buildBoundaryFieldsMap(services...)
		locations := buildFieldURLMap(services...)
		isBoundary := buildIsBoundaryMap(services...)

		s.mutex.Lock()
		s.Locations = locations
		s.IsBoundary = isBoundary
		s.MergedSchema = schema
		s.BoundaryQueries = boundaryQueries
		s.mutex.Unlock()
	}

	return nil
}

// Exec returns the query execution handler
func (s *ExecutableSchema) Exec(ctx context.Context) graphql.ResponseHandler {
	return s.ExecuteQuery
}

func (s *ExecutableSchema) ExecuteQuery(ctx context.Context) *graphql.Response {
	operationCtx := graphql.GetOperationContext(ctx)
	operation := operationCtx.Operation
	variables := operationCtx.Variables

	ctx, span := s.tracer.Start(ctx, "Federated GraphQL Query",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			semconv.GraphqlOperationTypeKey.String(string(operation.Operation)),
			semconv.GraphqlOperationName(operationCtx.OperationName),
			semconv.GraphqlDocument(operationCtx.RawQuery),
		),
	)

	defer span.End()

	traceErr := func(err error) {
		if err == nil {
			return
		}

		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	for _, plugin := range s.plugins {
		plugin.InterceptRequest(ctx, operation.Name, operationCtx.RawQuery, variables)
	}

	AddField(ctx, "operation.name", operation.Name)
	AddField(ctx, "operation.type", operation.Operation)

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	// The op passed in is a cached value
	// so it must be copied before modification
	operation = s.evaluateSkipAndInclude(variables, operation)
	filteredSchema := s.MergedSchema

	var errs gqlerror.List
	perms, hasPerms := GetPermissionsFromContext(ctx)
	if hasPerms {
		filteredSchema = perms.FilterSchema(s.MergedSchema)
		errs = perms.FilterAuthorizedFields(operation)
	}

	plan, err := Plan(&PlanningContext{
		Operation:  operation,
		Schema:     filteredSchema,
		Locations:  s.Locations,
		IsBoundary: s.IsBoundary,
		Services:   s.Services,
	})
	if err != nil {
		traceErr(err)
		return s.interceptResponse(ctx, operation.Name, operationCtx.RawQuery, variables, graphql.ErrorResponse(ctx, err.Error()))
	}

	extensions := make(map[string]interface{})
	timings := make(map[string]interface{})
	if debugInfo, ok := ctx.Value(DebugKey).(DebugInfo); ok {
		if debugInfo.Query {
			extensions["query"] = operation
		}
		if debugInfo.Variables {
			extensions["variables"] = variables
		}
		if debugInfo.Plan {
			extensions["plan"] = plan
		}
		if debugInfo.Timing {
			extensions["timings"] = timings
		}
	}

	for name, value := range extensions {
		graphql.RegisterExtension(ctx, name, value)
	}

	executionStart := time.Now()

	qe := newQueryExecution(ctx, operationCtx.OperationName, s.GraphqlClient, filteredSchema, s.BoundaryQueries, int32(s.MaxRequestsPerQuery))

	results, executeErrs := qe.Execute(plan)
	if len(executeErrs) > 0 {
		traceErr(executeErrs)
		return s.interceptResponse(ctx, operation.Name, operationCtx.RawQuery, variables, &graphql.Response{
			Errors: executeErrs,
		})
	}

	for _, result := range results {
		errs = append(errs, result.Errors...)
	}

	if !operationCtx.DisableIntrospection {
		introspectionData := resolveIntrospectionFields(ctx, operation.SelectionSet, filteredSchema)
		if len(introspectionData) > 0 {
			results = append([]executionResult{
				{
					ServiceURL: internalServiceName,
					Data:       introspectionData,
				},
			}, results...)
		}
	}

	timings["execution"] = time.Since(executionStart).String()

	mergeStart := time.Now()
	mergedResult, err := mergeExecutionResults(results)
	if err != nil {
		errs = append(errs, &gqlerror.Error{Message: err.Error()})

		traceErr(errs)
		AddField(ctx, "errors", errs)

		return s.interceptResponse(ctx, operation.Name, operationCtx.RawQuery, variables, &graphql.Response{
			Errors: errs,
		})
	}

	bubbleErrs, err := bubbleUpNullValuesInPlace(filteredSchema, operation.SelectionSet, mergedResult)
	if err == errNullBubbledToRoot {
		mergedResult = nil
	} else if err != nil {
		errs = append(errs, &gqlerror.Error{Message: err.Error()})

		traceErr(errs)
		AddField(ctx, "errors", errs)

		return s.interceptResponse(ctx, operation.Name, operationCtx.RawQuery, variables, &graphql.Response{
			Errors: errs,
		})
	}

	errs = append(errs, bubbleErrs...)
	timings["merge"] = time.Since(mergeStart).String()

	formattingStart := time.Now()
	formattedResponse := formatResponseData(filteredSchema, operation.SelectionSet, mergedResult)
	timings["format"] = time.Since(formattingStart).String()

	if len(errs) > 0 {
		traceErr(errs)
		AddField(ctx, "errors", errs)
	}

	return s.interceptResponse(ctx, operation.Name, operationCtx.RawQuery, variables, &graphql.Response{
		Data:   formattedResponse,
		Errors: errs,
	})
}

func (s *ExecutableSchema) interceptResponse(ctx context.Context, operationName, rawQuery string, variables map[string]interface{}, response *graphql.Response) *graphql.Response {
	for _, plugin := range s.plugins {
		response = plugin.InterceptResponse(ctx, operationName, rawQuery, variables, response)
	}
	return response
}

// Schema returns the merged schema
func (s *ExecutableSchema) Schema() *ast.Schema {
	return s.MergedSchema
}

// Complexity returns the query complexity (unimplemented)
func (s *ExecutableSchema) Complexity(typeName, fieldName string, childComplexity int, args map[string]interface{}) (int, bool) {
	// FIXME: TBD
	return 0, false
}

func resolveIntrospectionFields(ctx context.Context, selectionSet ast.SelectionSet, filteredSchema *ast.Schema) map[string]interface{} {
	introspectionResult := make(map[string]interface{})
	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "__type":
			name := f.Arguments.ForName("name").Value.Raw
			introspectionResult[f.Alias] = resolveType(ctx, filteredSchema, &ast.Type{NamedType: name}, f.SelectionSet)
		case "__schema":
			introspectionResult[f.Alias] = resolveSchema(ctx, filteredSchema, f.SelectionSet)
		}
	}

	return introspectionResult
}

func resolveSchema(ctx context.Context, schema *ast.Schema, selectionSet ast.SelectionSet) map[string]interface{} {
	result := make(map[string]interface{})

	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "types":
			types := []map[string]interface{}{}
			for _, t := range schema.Types {
				types = append(types, resolveType(ctx, schema, &ast.Type{NamedType: t.Name}, f.SelectionSet))
			}
			result[f.Alias] = types
		case "queryType":
			result[f.Alias] = resolveType(ctx, schema, &ast.Type{NamedType: "Query"}, f.SelectionSet)
		case "mutationType":
			result[f.Alias] = resolveType(ctx, schema, &ast.Type{NamedType: "Mutation"}, f.SelectionSet)
		case "subscriptionType":
			result[f.Alias] = resolveType(ctx, schema, &ast.Type{NamedType: "Subscription"}, f.SelectionSet)
		case "directives":
			directives := []map[string]interface{}{}
			for _, d := range schema.Directives {
				directives = append(directives, resolveDirective(ctx, schema, d, f.SelectionSet))
			}
			result[f.Alias] = directives
		}
	}

	return result
}

func resolveType(ctx context.Context, schema *ast.Schema, typ *ast.Type, selectionSet ast.SelectionSet) map[string]interface{} {
	if typ == nil {
		return nil
	}

	result := make(map[string]interface{})

	// If the type is NON_NULL or LIST then use that first (in that order), then
	// recursively call in "ofType"

	if typ.NonNull {
		for _, f := range selectionSetToFields(selectionSet) {
			switch f.Name {
			case "kind":
				result[f.Alias] = "NON_NULL"
			case "ofType":
				result[f.Alias] = resolveType(ctx, schema, &ast.Type{
					NamedType: typ.NamedType,
					Elem:      typ.Elem,
					NonNull:   false,
				}, f.SelectionSet)
			default:
				result[f.Alias] = nil
			}
		}
		return result
	}

	if typ.Elem != nil {
		for _, f := range selectionSetToFields(selectionSet) {
			switch f.Name {
			case "kind":
				result[f.Alias] = "LIST"
			case "ofType":
				result[f.Alias] = resolveType(ctx, schema, typ.Elem, f.SelectionSet)
			default:
				result[f.Alias] = nil
			}
		}
		return result
	}

	namedType, ok := schema.Types[typ.NamedType]
	if !ok {
		return nil
	}
	var variables map[string]interface{}
	reqctx := graphql.GetOperationContext(ctx)
	if reqctx != nil {
		variables = reqctx.Variables
	}
	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "kind":
			result[f.Alias] = namedType.Kind
		case "name":
			result[f.Alias] = namedType.Name
		case "fields":
			includeDeprecated := false
			if deprecatedArg := f.Arguments.ForName("includeDeprecated"); deprecatedArg != nil {
				v, err := deprecatedArg.Value.Value(variables)
				if err == nil {
					includeDeprecated, _ = v.(bool)
				}
			}

			fields := []map[string]interface{}{}
			for _, fi := range namedType.Fields {
				if isGraphQLBuiltinName(fi.Name) {
					continue
				}
				if !includeDeprecated {
					if deprecated, _ := hasDeprecatedDirective(fi.Directives); deprecated {
						continue
					}
				}
				fields = append(fields, resolveField(ctx, schema, fi, f.SelectionSet))
			}
			result[f.Alias] = fields
		case "description":
			result[f.Alias] = namedType.Description
		case "interfaces":
			interfaces := []map[string]interface{}{}
			for _, i := range namedType.Interfaces {
				interfaces = append(interfaces, resolveType(ctx, schema, &ast.Type{NamedType: i}, f.SelectionSet))
			}
			result[f.Alias] = interfaces
		case "possibleTypes":
			if namedType.Kind != ast.Interface && namedType.Kind != ast.Union {
				result[f.Alias] = nil
			} else {
				types := []map[string]interface{}{}
				for _, t := range schema.PossibleTypes[namedType.Name] {
					types = append(types, resolveType(ctx, schema, &ast.Type{NamedType: t.Name}, f.SelectionSet))
				}
				result[f.Alias] = types
			}
		case "enumValues":
			includeDeprecated := false
			if deprecatedArg := f.Arguments.ForName("includeDeprecated"); deprecatedArg != nil {
				v, err := deprecatedArg.Value.Value(variables)
				if err == nil {
					includeDeprecated, _ = v.(bool)
				}
			}

			enums := []map[string]interface{}{}
			for _, e := range namedType.EnumValues {
				if !includeDeprecated {
					if deprecated, _ := hasDeprecatedDirective(e.Directives); deprecated {
						continue
					}
				}
				enums = append(enums, resolveEnumValue(e, f.SelectionSet))
			}
			result[f.Alias] = enums
		case "inputFields":
			if namedType.Kind == ast.InputObject {
				inputFields := []map[string]interface{}{}
				for _, fi := range namedType.Fields {
					// call resolveField instead of resolveInputValue because it has
					// the right type and is a superset of it
					inputFields = append(inputFields, resolveField(ctx, schema, fi, f.SelectionSet))
				}
				result[f.Alias] = inputFields
			} else {
				result[f.Alias] = nil
			}
		default:
			result[f.Alias] = nil
		}
	}

	return result
}

func resolveField(ctx context.Context, schema *ast.Schema, field *ast.FieldDefinition, selectionSet ast.SelectionSet) map[string]interface{} {
	result := make(map[string]interface{})

	deprecated, deprecatedReason := hasDeprecatedDirective(field.Directives)

	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "name":
			result[f.Alias] = field.Name
		case "description":
			result[f.Alias] = field.Description
		case "args":
			args := []map[string]interface{}{}
			for _, arg := range field.Arguments {
				args = append(args, resolveInputValue(ctx, schema, arg, f.SelectionSet))
			}
			result[f.Alias] = args
		case "type":
			result[f.Alias] = resolveType(ctx, schema, field.Type, f.SelectionSet)
		case "isDeprecated":
			result[f.Alias] = deprecated
		case "deprecationReason":
			result[f.Alias] = deprecatedReason
		case "defaultValue":
			if field.DefaultValue != nil {
				result[f.Alias] = field.DefaultValue.String()
			} else {
				result[f.Alias] = nil
			}
		}
	}

	return result
}

func resolveInputValue(ctx context.Context, schema *ast.Schema, arg *ast.ArgumentDefinition, selectionSet ast.SelectionSet) map[string]interface{} {
	result := make(map[string]interface{})

	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "name":
			result[f.Alias] = arg.Name
		case "description":
			result[f.Alias] = arg.Description
		case "type":
			result[f.Alias] = resolveType(ctx, schema, arg.Type, f.SelectionSet)
		case "defaultValue":
			if arg.DefaultValue != nil {
				result[f.Alias] = arg.DefaultValue.String()
			} else {
				result[f.Alias] = nil
			}
		}
	}

	return result
}

func resolveEnumValue(enum *ast.EnumValueDefinition, selectionSet ast.SelectionSet) map[string]interface{} {
	result := make(map[string]interface{})

	deprecated, deprecatedReason := hasDeprecatedDirective(enum.Directives)

	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "name":
			result[f.Alias] = enum.Name
		case "description":
			result[f.Alias] = enum.Description
		case "isDeprecated":
			result[f.Alias] = deprecated
		case "deprecationReason":
			result[f.Alias] = deprecatedReason
		}
	}

	return result
}

func resolveDirective(ctx context.Context, schema *ast.Schema, directive *ast.DirectiveDefinition, selectionSet ast.SelectionSet) map[string]interface{} {
	result := make(map[string]interface{})

	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "name":
			result[f.Alias] = directive.Name
		case "description":
			result[f.Alias] = directive.Description
		case "locations":
			result[f.Alias] = directive.Locations
		case "args":
			args := []map[string]interface{}{}
			for _, arg := range directive.Arguments {
				args = append(args, resolveInputValue(ctx, schema, arg, f.SelectionSet))
			}
			result[f.Alias] = args
		}
	}

	return result
}

func selectionSetToFields(selectionSet ast.SelectionSet) []*ast.Field {
	var result []*ast.Field
	for _, s := range selectionSet {
		switch s := s.(type) {
		case *ast.Field:
			result = append(result, s)
		case *ast.FragmentSpread:
			result = append(result, selectionSetToFields(s.Definition.SelectionSet)...)
		case *ast.InlineFragment:
			result = append(result, selectionSetToFields(s.SelectionSet)...)
		}
	}

	return result
}

func hasDeprecatedDirective(directives ast.DirectiveList) (bool, *string) {
	for _, d := range directives {
		if d.Name == "deprecated" {
			var reason string
			reasonArg := d.Arguments.ForName("reason")
			if reasonArg != nil {
				reason = reasonArg.Value.Raw
			}
			return true, &reason
		}
	}

	return false, nil
}

func jsonMapToInterfaceMap(m map[string]json.RawMessage) map[string]interface{} {
	res := make(map[string]interface{}, len(m))
	for k, v := range m {
		res[k] = v
	}

	return res
}

// mergeMaps merge src into dst, unmarshalling json.RawMessages when necessary
func mergeMaps(dst, src map[string]interface{}) {
	for k, v := range dst {
		if b, ok := src[k]; ok {
			// The value is in both maps, we need to merge them.
			// If any of the 2 values is a json.RawMessage, unmarshal it first

			var aValue map[string]interface{}
			var bValue map[string]interface{}

			switch value := v.(type) {
			case json.RawMessage:
				// we want to unmarshal only what's necessary, so unmarshal only
				// one level of the result
				var m map[string]json.RawMessage
				_ = json.Unmarshal([]byte(value), &m)
				aValue = jsonMapToInterfaceMap(m)
				dst[k] = aValue
			case map[string]interface{}:
				aValue = value
			default:
				panic(fmt.Sprintf("mergeMaps: dst value is %T not a map[string]interface{} or json.RawMessage", value))
			}

			switch value := b.(type) {
			case json.RawMessage:
				var m map[string]json.RawMessage
				_ = json.Unmarshal([]byte(value), &m)
				bValue = jsonMapToInterfaceMap(m)
			case map[string]interface{}:
				bValue = value
			default:
				panic(fmt.Sprintf("mergeMaps: src value is %T not a map[string]interface{} or json.RawMessage", value))
			}

			mergeMaps(aValue, bValue)
			continue
		}
	}

	for k, v := range src {
		if _, ok := dst[k]; ok {
			continue
		}
		dst[k] = v
	}
}

func (s *ExecutableSchema) evaluateSkipAndInclude(vars map[string]interface{}, op *ast.OperationDefinition) *ast.OperationDefinition {
	return &ast.OperationDefinition{
		Operation:           op.Operation,
		Name:                op.Name,
		VariableDefinitions: op.VariableDefinitions,
		Directives:          op.Directives,
		SelectionSet:        s.evaluateSkipAndIncludeRec(vars, op.SelectionSet),
		Position:            op.Position,
	}
}

func (s *ExecutableSchema) evaluateSkipAndIncludeRec(vars map[string]interface{}, selectionSet ast.SelectionSet) ast.SelectionSet {
	if selectionSet == nil {
		return nil
	}
	result := ast.SelectionSet{}
	for _, someSelection := range selectionSet {
		var skipDirective, includeDirective *ast.Directive
		switch selection := someSelection.(type) {
		case *ast.Field:
			skipDirective = selection.Directives.ForName("skip")
			includeDirective = selection.Directives.ForName("include")
		case *ast.InlineFragment:
			skipDirective = selection.Directives.ForName("skip")
			includeDirective = selection.Directives.ForName("include")
		case *ast.FragmentSpread:
			skipDirective = selection.Directives.ForName("skip")
			includeDirective = selection.Directives.ForName("include")
		}
		skip, include := false, true
		if skipDirective != nil {
			skip = resolveIfArgument(skipDirective, vars)
		}
		if includeDirective != nil {
			include = resolveIfArgument(includeDirective, vars)
		}
		if !skip && include {
			switch selection := someSelection.(type) {
			case *ast.Field:
				result = append(result, &ast.Field{
					Alias:            selection.Alias,
					Name:             selection.Name,
					Arguments:        selection.Arguments,
					Directives:       removeSkipAndInclude(selection.Directives),
					SelectionSet:     s.evaluateSkipAndIncludeRec(vars, selection.SelectionSet),
					Position:         selection.Position,
					Definition:       selection.Definition,
					ObjectDefinition: selection.ObjectDefinition,
				})
			case *ast.InlineFragment:
				result = append(result, &ast.InlineFragment{
					TypeCondition:    selection.TypeCondition,
					Directives:       removeSkipAndInclude(selection.Directives),
					SelectionSet:     s.evaluateSkipAndIncludeRec(vars, selection.SelectionSet),
					Position:         selection.Position,
					ObjectDefinition: selection.ObjectDefinition,
				})
			case *ast.FragmentSpread:
				result = append(result, &ast.FragmentSpread{
					Name:             selection.Name,
					Directives:       removeSkipAndInclude(selection.Directives),
					Position:         selection.Position,
					ObjectDefinition: selection.ObjectDefinition,
					Definition: &ast.FragmentDefinition{
						Name:               selection.Definition.Name,
						VariableDefinition: selection.Definition.VariableDefinition,
						TypeCondition:      selection.Definition.TypeCondition,
						Directives:         removeSkipAndInclude(selection.Definition.Directives),
						SelectionSet:       s.evaluateSkipAndIncludeRec(vars, selection.Definition.SelectionSet),
						Definition:         selection.Definition.Definition,
						Position:           selection.Definition.Position,
					},
				})
			}
		}
	}
	return result
}

func removeSkipAndInclude(directives ast.DirectiveList) ast.DirectiveList {
	var result ast.DirectiveList
	for _, d := range directives {
		if d.Name == "include" || d.Name == "skip" {
			continue
		}
		result = append(result, d)
	}
	return result
}

func resolveIfArgument(d *ast.Directive, variables map[string]interface{}) bool {
	arg := d.Arguments.ForName("if")
	if arg == nil {
		panic(fmt.Sprintf("%s: argument 'if' not defined", d.Name))
	}
	value, err := arg.Value.Value(variables)
	if err != nil {
		panic(err)
	}
	result, ok := value.(bool)
	if !ok {
		panic(fmt.Sprintf("%s: argument 'if' is not a boolean", d.Name))
	}
	return result
}
