package bramble

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/opentracing/opentracing-go"
	log "github.com/sirupsen/logrus"
	"github.com/uber/jaeger-client-go"
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

type ExecutableSchema struct {
	MergedSchema        *ast.Schema
	Locations           FieldURLMap
	IsBoundary          map[string]bool
	Services            map[string]*Service
	GraphqlClient       *GraphQLClient
	Tracer              opentracing.Tracer
	MaxRequestsPerQuery int64

	mutex   sync.RWMutex
	plugins []Plugin
}

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

func (s *ExecutableSchema) UpdateSchema(forceRebuild bool) error {
	var services []*Service
	var schemas []*ast.Schema
	var updatedServices []string
	var invalidschema float64 = 0

	defer func() { promInvalidSchema.Set(invalidschema) }()

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
			invalidschema = 1
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
			invalidschema = 1
			return fmt.Errorf("update of service %v caused schema error: %w", updatedServices, err)
		}

		locations := buildFieldURLMap(services...)
		isBoundary := buildIsBoundaryMap(services...)

		s.mutex.Lock()
		s.Locations = locations
		s.IsBoundary = isBoundary
		s.MergedSchema = schema
		s.mutex.Unlock()
	}

	return nil
}

func (s *ExecutableSchema) Exec(ctx context.Context) graphql.ResponseHandler {
	return s.ExecuteQuery
}

func (s *ExecutableSchema) ExecuteQuery(ctx context.Context) *graphql.Response {
	start := time.Now()

	opctx := graphql.GetOperationContext(ctx)
	op := opctx.Operation

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	result := make(map[string]interface{})

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
	perms, ok := GetPermissionsFromContext(ctx)
	if ok {
		errs = perms.FilterAuthorizedFields(op)
	}

	for _, f := range selectionSetToFields(op.SelectionSet) {
		switch f.Name {
		case "__type":
			name := f.Arguments.ForName("name").Value.Raw
			result[f.Alias] = s.resolveType(ctx, perms.FilterSchema(s.MergedSchema), &ast.Type{NamedType: name}, f.SelectionSet)
		case "__schema":
			result[f.Alias] = s.resolveSchema(ctx, perms.FilterSchema(s.MergedSchema), f.SelectionSet)
		}
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

	qe := newQueryExecution(s.GraphqlClient, s.Schema(), s.Tracer, s.MaxRequestsPerQuery)
	executionErrors := qe.execute(ctx, plan, result)
	errs = append(errs, executionErrors...)
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
		if debugInfo.Timing {
			extensions["timing"] = time.Since(start).Round(time.Millisecond).String()
		}
		if debugInfo.TraceID {
			extensions["traceid"] = TraceIDFromContext(ctx)
		}
	}

	for _, plugin := range s.plugins {
		if err := plugin.ModifyExtensions(ctx, qe, extensions); err != nil {
			AddField(ctx, fmt.Sprintf("%s-plugin-error", plugin.ID()), err.Error())
		}
	}

	for name, value := range extensions {
		graphql.RegisterExtension(ctx, name, value)
	}

	res, err := marshalResult(result, op.SelectionSet, s.MergedSchema, &ast.Type{NamedType: strings.Title(string(op.Operation))})
	if err != nil {
		errs = append(errs, &gqlerror.Error{Message: err.Error()})
		AddField(ctx, "errors", errs)
		return &graphql.Response{
			Errors: errs,
		}
	}

	if len(errs) > 0 {
		AddField(ctx, "errors", errs)
	}
	return &graphql.Response{
		Data:   res,
		Errors: errs,
	}

}

func TraceIDFromContext(ctx context.Context) string {
	span := opentracing.SpanFromContext(ctx)
	if span == nil {
		return ""
	}
	jaegerContext, ok := span.Context().(jaeger.SpanContext)
	if !ok {
		return ""
	}
	return jaegerContext.TraceID().String()
}

func (s *ExecutableSchema) Schema() *ast.Schema {
	return s.MergedSchema
}

func (s *ExecutableSchema) Complexity(typeName, fieldName string, childComplexity int, args map[string]interface{}) (int, bool) {
	// FIXME: TBD
	return 0, false
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

type QueryExecution struct {
	Schema       *ast.Schema
	Errors       []*gqlerror.Error
	RequestCount int64

	maxRequest    int64
	tracer        opentracing.Tracer
	wg            sync.WaitGroup
	m             sync.Mutex
	graphqlClient *GraphQLClient
}

func newQueryExecution(client *GraphQLClient, schema *ast.Schema, tracer opentracing.Tracer, maxRequest int64) *QueryExecution {
	return &QueryExecution{
		Schema:        schema,
		graphqlClient: client,
		tracer:        tracer,
		maxRequest:    maxRequest,
	}
}

func (e *QueryExecution) execute(ctx context.Context, plan *queryPlan, resData map[string]interface{}) []*gqlerror.Error {
	e.wg.Add(len(plan.RootSteps))
	for _, step := range plan.RootSteps {
		if step.ServiceURL == internalServiceName {
			e.executeBrambleStep(ctx, step, resData)
			continue
		}
		go e.executeRootStep(ctx, step, resData)
	}

	e.wg.Wait()

	if e.RequestCount > e.maxRequest {
		e.Errors = append(e.Errors, &gqlerror.Error{
			Message: fmt.Sprintf("query exceeded max requests count of %d with %d requests, data will be incomplete", e.maxRequest, e.RequestCount),
		})
	}

	return e.Errors
}

func (e *QueryExecution) addError(step *queryPlanStep, msg string) {
	var path ast.Path
	for _, p := range step.InsertionPoint {
		path = append(path, ast.PathName(p))
	}

	var locs []gqlerror.Location
	for _, f := range selectionSetToFields(step.SelectionSet) {
		pos := f.GetPosition()
		if pos == nil {
			continue
		}
		locs = append(locs, gqlerror.Location{Line: pos.Line, Column: pos.Column})

		// if the field has a subset it's part of the path
		if len(f.SelectionSet) > 0 {
			path = append(path, ast.PathName(f.Alias))
		}
	}

	err := &gqlerror.Error{
		Message:   msg,
		Path:      path,
		Locations: locs,
	}

	e.m.Lock()
	e.Errors = append(e.Errors, err)
	e.m.Unlock()
}

func (e *QueryExecution) executeRootStep(ctx context.Context, step *queryPlanStep, result map[string]interface{}) {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			AddField(ctx, "panic", map[string]interface{}{
				"err":        r,
				"stacktrace": string(debug.Stack()),
			})
			e.addError(step, "an error happened during query execution")
		}
	}()

	if e.tracer != nil {
		contextSpan := opentracing.SpanFromContext(ctx)
		if contextSpan != nil {
			span := e.tracer.StartSpan(step.ServiceName, opentracing.ChildOf(contextSpan.Context()))
			ctx = opentracing.ContextWithSpan(ctx, span)
			defer span.Finish()
		}
	}

	q := formatSelectionSet(ctx, e.Schema, step.SelectionSet)
	if step.ParentType == mutationObjectName {
		q = "mutation " + q
	} else {
		q = "query " + q
	}

	resp := map[string]json.RawMessage{}
	promHTTPInFlightGauge.Inc()
	req := NewRequest(q)
	req.Headers = GetOutgoingRequestHeadersFromContext(ctx)
	err := e.graphqlClient.Request(ctx, step.ServiceURL, req, &resp)
	promHTTPInFlightGauge.Dec()
	if err != nil {
		e.addError(step, fmt.Sprintf("error while querying %s with %s: %s", step.ServiceURL, formatSelectionSetSingleLine(ctx, e.Schema, step.SelectionSet), err))
	}

	e.m.Lock()
	mergeMaps(result, jsonMapToInterfaceMap(resp))
	e.m.Unlock()

	for _, subStep := range step.Then {
		e.wg.Add(1)
		go e.executeChildStep(ctx, subStep, result)
	}
}

func jsonMapToInterfaceMap(m map[string]json.RawMessage) map[string]interface{} {
	res := make(map[string]interface{}, len(m))
	for k, v := range m {
		res[k] = v
	}

	return res
}

// executeChildStep executes a child step. It finds the insertion targets for
// the step's insertion point and queries the specified service using the node
// query type.
func (e *QueryExecution) executeChildStep(ctx context.Context, step *queryPlanStep, result map[string]interface{}) {
	defer e.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			AddField(ctx, "panic", map[string]interface{}{
				"err":        r,
				"stacktrace": string(debug.Stack()),
			})
			e.addError(step, "an error happened during query execution")
		}
	}()

	if e.tracer != nil {
		contextSpan := opentracing.SpanFromContext(ctx)
		if contextSpan != nil {
			span := e.tracer.StartSpan(step.ServiceName, opentracing.ChildOf(contextSpan.Context()))
			ctx = opentracing.ContextWithSpan(ctx, span)
			defer span.Finish()
		}
	}

	e.m.Lock()
	result = prepareMapForInsertion(step.InsertionPoint, result).(map[string]interface{})
	e.m.Unlock()

	insertionPoints := buildInsertionSlice(step.InsertionPoint, result)
	if len(insertionPoints) == 0 {
		return
	}

	atomic.AddInt64(&e.RequestCount, 1)

	if e.RequestCount > e.maxRequest {
		return
	}

	selectionSet := formatSelectionSet(ctx, e.Schema, step.SelectionSet)
	var b strings.Builder
	b.WriteString("{")
	for i, ip := range insertionPoints {
		b.WriteString(fmt.Sprintf("%s: node(id: %q) { ... on %s %s } ", nodeAlias(i), ip.ID, step.ParentType, selectionSet))
	}
	b.WriteString("}")

	query := b.String()

	// If there's no sub-calls on the data we want to store it as returned.
	// This is to preserve fields order with inline fragments on unions, as we
	// have no way to determine which type was matched.
	// e.g.: { ... on Cat { name, age } ... on Dog { age, name } }
	if len(step.Then) == 0 {
		resp := map[string]map[string]json.RawMessage{}
		promHTTPInFlightGauge.Inc()
		req := NewRequest(query)
		req.Headers = GetOutgoingRequestHeadersFromContext(ctx)
		err := e.graphqlClient.Request(ctx, step.ServiceURL, req, &resp)
		promHTTPInFlightGauge.Dec()
		if err != nil {
			e.addError(step, fmt.Sprintf("error while querying %s with %s: %s", step.ServiceURL, formatSelectionSetSingleLine(ctx, e.Schema, step.SelectionSet), err))
		}
		e.m.Lock()
		for i := range insertionPoints {
			for k, v := range resp[nodeAlias(i)] {
				insertionPoints[i].Target[k] = v
			}
		}
		e.m.Unlock()
		return
	}

	resp := map[string]map[string]interface{}{}
	promHTTPInFlightGauge.Inc()
	req := NewRequest(query)
	req.Headers = GetOutgoingRequestHeadersFromContext(ctx)
	err := e.graphqlClient.Request(ctx, step.ServiceURL, req, &resp)
	promHTTPInFlightGauge.Dec()
	if err != nil {
		e.addError(step, fmt.Sprintf("error while querying %s with %s: %s", step.ServiceURL, formatSelectionSetSingleLine(ctx, e.Schema, step.SelectionSet), err))
	}
	e.m.Lock()
	for i := range insertionPoints {
		for k, v := range resp[nodeAlias(i)] {
			insertionPoints[i].Target[k] = v
		}
	}
	e.m.Unlock()

	for _, subStep := range step.Then {
		e.wg.Add(1)
		go e.executeChildStep(ctx, subStep, result)
	}
}

// executeBrambleStep executes the Bramble-specific operations
func (e *QueryExecution) executeBrambleStep(ctx context.Context, step *queryPlanStep, result map[string]interface{}) {
	m := buildTypenameResponseMap(step.SelectionSet, step.ParentType)
	mergeMaps(result, m)
	e.wg.Done()
}

// buildTypenameResponseMap recursively builds the response map for `__typename`
// fields. This is used for namespaces as they do not belong to any service.
func buildTypenameResponseMap(ss ast.SelectionSet, currentType string) map[string]interface{} {
	res := make(map[string]interface{})
	for _, f := range selectionSetToFields(ss) {
		if len(f.SelectionSet) > 0 {
			res[f.Alias] = buildTypenameResponseMap(f.SelectionSet, f.Definition.Type.Name())
			continue
		}

		if f.Name == "__typename" {
			res[f.Alias] = currentType
		}
	}
	return res
}

func nodeAlias(i int) string {
	return fmt.Sprintf("_%d", i)
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
				var m map[string]interface{}
				_ = json.Unmarshal([]byte(value), &m)
				dst[k] = m
				aValue = m
			case map[string]interface{}:
				aValue = value
			default:
				panic("invalid merge")
			}

			switch value := b.(type) {
			case json.RawMessage:
				var m map[string]interface{}
				_ = json.Unmarshal([]byte(value), &m)
				bValue = m
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

type insertionTarget struct {
	ID     string
	Target map[string]interface{}
}

// prepareMapForInsertion recursively traverses the result map to the insertion
// point and unmarshals any json.RawMessage it finds on the way
func prepareMapForInsertion(insertionPoint []string, in interface{}) interface{} {
	if len(insertionPoint) == 0 {
		switch in := in.(type) {
		case json.RawMessage:
			var i interface{}
			_ = json.Unmarshal([]byte(in), &i)
			switch i := i.(type) {
			case map[string]interface{}, []interface{}:
				return i
			case nil:
				return nil
			default:
				panic("unknown type after unmarshalling")
			}
		default:
			return in
		}
	}

	switch in := in.(type) {
	case map[string]interface{}:
		in[insertionPoint[0]] = prepareMapForInsertion(insertionPoint[1:], in[insertionPoint[0]])
		return in
	case json.RawMessage:
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(in), &m)
		if m == nil {
			return nil
		}
		m[insertionPoint[0]] = prepareMapForInsertion(insertionPoint[1:], m[insertionPoint[0]])
		return m
	case []interface{}:
		for i, e := range in {
			in[i] = prepareMapForInsertion(insertionPoint, e)
		}
		return in
	case nil:
		return nil
	default:
		panic(fmt.Sprintf("unhandled type: %s", reflect.TypeOf(in).Name()))
	}
}

// buildInsertionSlice returns the list of maps where the data should be inserted
// It recursively traverses maps and list to find the insertion points.
// For example, if we have "insertionPoint" [movie, compTitles] and "in"
// movie { compTitles: [
//	{ id: 1 },
//  { id: 2 }
// ] }
// we want to return [{ id: 1 }, { id: 2 }]
func buildInsertionSlice(insertionPoint []string, in interface{}) []insertionTarget {
	if len(insertionPoint) == 0 {
		switch in := in.(type) {
		case map[string]interface{}:
			eid := ""
			if id, ok := in["_id"]; ok {
				eid = id.(string)
			} else if id, ok := in["id"]; ok {
				eid = id.(string)
			}

			if eid == "" {
				return nil
			}

			return []insertionTarget{{
				ID:     eid,
				Target: in,
			}}
		case []interface{}:
			var result []insertionTarget
			for _, e := range in {
				result = append(result, buildInsertionSlice(insertionPoint, e)...)
			}
			return result
		case json.RawMessage:
			var m map[string]interface{}
			_ = json.Unmarshal([]byte(in), &m)
			return buildInsertionSlice(nil, m)
		case nil:
			return nil
		default:
			panic(fmt.Sprintf("unhandled insertion point type: %q", reflect.TypeOf(in).Name()))
		}
	}

	switch in := in.(type) {
	case map[string]interface{}:
		return buildInsertionSlice(insertionPoint[1:], in[insertionPoint[0]])
	case []interface{}:
		var result []insertionTarget
		for _, e := range in {
			result = append(result, buildInsertionSlice(insertionPoint, e)...)
		}
		return result
	case nil:
		return nil
	default:
		panic(fmt.Sprintf("unhandled insertion point type: %s", reflect.TypeOf(in).Name()))
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
