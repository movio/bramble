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
)

func newExecutableSchema(plugins []Plugin, maxRequestsPerQuery int64, client *GraphQLClient, services ...*Service) *ExecutableSchema {
	serviceMap := make(map[string]*Service)

	for _, s := range services {
		serviceMap[s.ServiceURL] = s
	}

	if client == nil {
		client = NewClient()
	}

	return &ExecutableSchema{
		Services: serviceMap,

		GraphqlClient:       client,
		plugins:             plugins,
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

	mutex   sync.RWMutex
	plugins []Plugin
}

// UpdateServiceList replaces the list of services with the provided one and
// update the schema.
func (s *ExecutableSchema) UpdateServiceList(services []string) error {
	newServices := make(map[string]*Service)
	for _, svcURL := range services {
		if svc, ok := s.Services[svcURL]; ok {
			newServices[svcURL] = svc
		} else {
			newServices[svcURL] = NewService(svcURL)
		}
	}
	s.Services = newServices

	return s.UpdateSchema(true)
}

// UpdateSchema updates the schema from every service and then update the merged
// schema.
func (s *ExecutableSchema) UpdateSchema(forceRebuild bool) error {
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

	promServiceUpdateError.Reset()

	for url, s := range s.Services {
		logger := log.WithFields(log.Fields{
			"url":     url,
			"version": s.Version,
			"service": s.Name,
		})
		updated, err := s.Update()
		if err != nil {
			promServiceUpdateError.WithLabelValues(s.ServiceURL).Inc()
			invalidSchema, forceRebuild = true, true
			logger.WithError(err).Error("unable to update service")
			// Ignore this service in this update
			continue
		}

		if updated {
			logger.Info("service was upgraded")
			updatedServices = append(updatedServices, s.Name)
		}

		services = append(services, s)
		schemas = append(schemas, s.Schema)
	}

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
	start := time.Now()

	opctx := graphql.GetOperationContext(ctx)
	op := opctx.Operation

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	variables := map[string]interface{}{}
	if graphql.HasOperationContext(ctx) {
		reqctx := graphql.GetOperationContext(ctx)
		if reqctx != nil {
			variables = reqctx.Variables
		}
	}

	// The op passed in is a cached value
	// so it must be copied before modification
	op = s.evaluateSkipAndInclude(variables, op)

	var errs gqlerror.List
	perms, hasPerms := GetPermissionsFromContext(ctx)
	if hasPerms {
		errs = perms.FilterAuthorizedFields(op)
	}

	filteredSchema := s.MergedSchema
	if hasPerms {
		filteredSchema = perms.FilterSchema(s.MergedSchema)
	}

	plan, err := Plan(&PlanningContext{
		Operation:  op,
		Schema:     s.Schema(),
		Locations:  s.Locations,
		IsBoundary: s.IsBoundary,
		Services:   s.Services,
	})

	if err != nil {
		return graphql.ErrorResponse(ctx, err.Error())
	}

	AddField(ctx, "operation.name", op.Name)
	AddField(ctx, "operation.type", op.Operation)

	qe := newQueryExecution(ctx, s.GraphqlClient, s.Schema(), s.BoundaryQueries, int32(s.MaxRequestsPerQuery))
	results, executeErrs := qe.Execute(plan)
	if len(executeErrs) > 0 {
		return &graphql.Response{
			Errors: executeErrs,
		}
	}

	timings := make(map[string]interface{})
	timings["execution"] = time.Since(start).Round(time.Millisecond).String()

	extensions := make(map[string]interface{})
	if debugInfo, ok := ctx.Value(DebugKey).(DebugInfo); ok {
		if debugInfo.Query {
			extensions["query"] = op
		}
		if debugInfo.Variables {
			extensions["variables"] = variables
		}
		if debugInfo.Plan {
			extensions["plan"] = plan
		}
	}

	for _, plugin := range s.plugins {
		if err := plugin.ModifyExtensions(ctx, qe, extensions); err != nil {
			AddField(ctx, fmt.Sprintf("%s-plugin-error", plugin.ID()), err.Error())
		}
	}

	for _, result := range results {
		errs = append(errs, result.Errors...)
	}

	introspectionData := s.resolveIntrospectionFields(ctx, op.SelectionSet, filteredSchema)
	if len(introspectionData) > 0 {
		results = append([]executionResult{
			{
				ServiceURL: internalServiceName,
				Data:       introspectionData,
			},
		}, results...)
	}

	mergeStart := time.Now()
	mergedResult, err := mergeExecutionResults(results)
	if err != nil {
		errs = append(errs, &gqlerror.Error{Message: err.Error()})
		AddField(ctx, "errors", errs)
		return &graphql.Response{
			Errors: errs,
		}
	}
	timings["merge"] = time.Since(mergeStart).Round(time.Millisecond).String()

	bubbleErrs, err := bubbleUpNullValuesInPlace(qe.schema, op.SelectionSet, mergedResult)
	if err == errNullBubbledToRoot {
		mergedResult = nil
	} else if err != nil {
		errs = append(errs, &gqlerror.Error{Message: err.Error()})
		AddField(ctx, "errors", errs)
		return &graphql.Response{
			Errors: errs,
		}
	}

	errs = append(errs, bubbleErrs...)

	formattingStart := time.Now()
	formattedResponse, err := formatResponseData(qe.schema, op.SelectionSet, mergedResult)
	if err != nil {
		errs = append(errs, &gqlerror.Error{Message: err.Error()})
		AddField(ctx, "errors", errs)
		return &graphql.Response{
			Errors: errs,
		}
	}
	timings["format"] = time.Since(formattingStart).Round(time.Millisecond).String()

	if debugInfo, ok := ctx.Value(DebugKey).(DebugInfo); ok {
		if debugInfo.Timing {
			extensions["timing"] = timings
		}
	}

	for name, value := range extensions {
		graphql.RegisterExtension(ctx, name, value)
	}

	if len(errs) > 0 {
		AddField(ctx, "errors", errs)
	}

	return &graphql.Response{
		Data:   formattedResponse,
		Errors: errs,
	}
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

func (s *ExecutableSchema) resolveIntrospectionFields(ctx context.Context, selectionSet ast.SelectionSet, filteredSchema *ast.Schema) map[string]interface{} {
	introspectionResult := make(map[string]interface{})
	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "__type":
			name := f.Arguments.ForName("name").Value.Raw
			introspectionResult[f.Alias] = s.resolveType(ctx, filteredSchema, &ast.Type{NamedType: name}, f.SelectionSet)
		case "__schema":
			introspectionResult[f.Alias] = s.resolveSchema(ctx, filteredSchema, f.SelectionSet)
		}
	}

	return introspectionResult
}

func (s *ExecutableSchema) resolveSchema(ctx context.Context, schema *ast.Schema, selectionSet ast.SelectionSet) map[string]interface{} {
	result := make(map[string]interface{})

	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "types":
			types := []map[string]interface{}{}
			for _, t := range schema.Types {
				types = append(types, s.resolveType(ctx, schema, &ast.Type{NamedType: t.Name}, f.SelectionSet))
			}
			result[f.Alias] = types
		case "queryType":
			result[f.Alias] = s.resolveType(ctx, schema, &ast.Type{NamedType: "Query"}, f.SelectionSet)
		case "mutationType":
			result[f.Alias] = s.resolveType(ctx, schema, &ast.Type{NamedType: "Mutation"}, f.SelectionSet)
		case "subscriptionType":
			result[f.Alias] = s.resolveType(ctx, schema, &ast.Type{NamedType: "Subscription"}, f.SelectionSet)
		case "directives":
			directives := []map[string]interface{}{}
			for _, d := range s.Schema().Directives {
				directives = append(directives, s.resolveDirective(ctx, schema, d, f.SelectionSet))
			}
			result[f.Alias] = directives
		}
	}

	return result
}

func (s *ExecutableSchema) resolveType(ctx context.Context, schema *ast.Schema, typ *ast.Type, selectionSet ast.SelectionSet) map[string]interface{} {
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
				result[f.Alias] = s.resolveType(ctx, schema, &ast.Type{
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
				result[f.Alias] = s.resolveType(ctx, schema, typ.Elem, f.SelectionSet)
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
				fields = append(fields, s.resolveField(ctx, schema, fi, f.SelectionSet))
			}
			result[f.Alias] = fields
		case "description":
			result[f.Alias] = namedType.Description
		case "interfaces":
			interfaces := []map[string]interface{}{}
			for _, i := range namedType.Interfaces {
				interfaces = append(interfaces, s.resolveType(ctx, schema, &ast.Type{NamedType: i}, f.SelectionSet))
			}
			result[f.Alias] = interfaces
		case "possibleTypes":
			if len(namedType.Types) > 0 {
				types := []map[string]interface{}{}
				for _, t := range namedType.Types {
					types = append(types, s.resolveType(ctx, schema, &ast.Type{NamedType: t}, f.SelectionSet))
				}
				result[f.Alias] = types
			} else {
				result[f.Alias] = nil
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
				enums = append(enums, s.resolveEnumValue(e, f.SelectionSet))
			}
			result[f.Alias] = enums
		case "inputFields":
			inputFields := []map[string]interface{}{}
			for _, fi := range namedType.Fields {
				// call resolveField instead of resolveInputValue because it has
				// the right type and is a superset of it
				inputFields = append(inputFields, s.resolveField(ctx, schema, fi, f.SelectionSet))
			}
			result[f.Alias] = inputFields
		default:
			result[f.Alias] = nil
		}
	}

	return result
}

func (s *ExecutableSchema) resolveField(ctx context.Context, schema *ast.Schema, field *ast.FieldDefinition, selectionSet ast.SelectionSet) map[string]interface{} {
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
				args = append(args, s.resolveInputValue(ctx, schema, arg, f.SelectionSet))
			}
			result[f.Alias] = args
		case "type":
			result[f.Alias] = s.resolveType(ctx, schema, field.Type, f.SelectionSet)
		case "isDeprecated":
			result[f.Alias] = deprecated
		case "deprecationReason":
			result[f.Alias] = deprecatedReason
		}
	}

	return result
}

func (s *ExecutableSchema) resolveInputValue(ctx context.Context, schema *ast.Schema, arg *ast.ArgumentDefinition, selectionSet ast.SelectionSet) map[string]interface{} {
	result := make(map[string]interface{})

	for _, f := range selectionSetToFields(selectionSet) {
		switch f.Name {
		case "name":
			result[f.Alias] = arg.Name
		case "description":
			result[f.Alias] = arg.Description
		case "type":
			result[f.Alias] = s.resolveType(ctx, schema, arg.Type, f.SelectionSet)
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

func (s *ExecutableSchema) resolveEnumValue(enum *ast.EnumValueDefinition, selectionSet ast.SelectionSet) map[string]interface{} {
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

func (s *ExecutableSchema) resolveDirective(ctx context.Context, schema *ast.Schema, directive *ast.DirectiveDefinition, selectionSet ast.SelectionSet) map[string]interface{} {
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
				args = append(args, s.resolveInputValue(ctx, schema, arg, f.SelectionSet))
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

// mergeMaps merge dst into src, unmarshalling json.RawMessages when necessary
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
				panic("invalid merge")
			}

			switch value := b.(type) {
			case json.RawMessage:
				var m map[string]json.RawMessage
				_ = json.Unmarshal([]byte(value), &m)
				bValue = jsonMapToInterfaceMap(m)
			case map[string]interface{}:
				bValue = value
			default:
				panic("invalid merge")
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
					ObjectDefinition: selection.Definition.Definition,
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
