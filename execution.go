package bramble

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"golang.org/x/sync/errgroup"
)

var errNullBubbledToRoot = errors.New("bubbleUpNullValuesInPlace: null bubbled up to root")

type executionResult struct {
	ServiceURL     string
	InsertionPoint []string
	Data           interface{}
	Errors         gqlerror.List
}

type queryExecution struct {
	ctx            context.Context
	operationName  string
	schema         *ast.Schema
	requestCount   int32
	maxRequest     int32
	graphqlClient  *GraphQLClient
	boundaryFields BoundaryFieldsMap

	group   *errgroup.Group
	results chan executionResult
}

func newQueryExecution(ctx context.Context, operationName string, client *GraphQLClient, schema *ast.Schema, boundaryFields BoundaryFieldsMap, maxRequest int32) *queryExecution {
	group, ctx := errgroup.WithContext(ctx)
	return &queryExecution{
		ctx:            ctx,
		operationName:  operationName,
		schema:         schema,
		graphqlClient:  client,
		boundaryFields: boundaryFields,
		maxRequest:     maxRequest,
		group:          group,
		results:        make(chan executionResult),
	}
}

func (q *queryExecution) Execute(queryPlan *QueryPlan) ([]executionResult, gqlerror.List) {
	wg := &sync.WaitGroup{}
	results := []executionResult{}

	for _, step := range queryPlan.RootSteps {
		if step.ServiceURL == internalServiceName {
			r, err := executeBrambleStep(step)
			if err != nil {
				return nil, q.createGQLErrors(step, err)
			}
			results = append(results, *r)
			continue
		}

		step := step
		q.group.Go(func() error {
			return q.executeRootStep(step)
		})
	}

	wg.Add(1)
	go func() {
		for result := range q.results {
			results = append(results, result)
		}
		wg.Done()
	}()

	if err := q.group.Wait(); err != nil {
		return nil, gqlerror.List{
			&gqlerror.Error{
				Message: err.Error(),
			},
		}
	}
	close(q.results)
	wg.Wait()
	return results, nil
}

func (q *queryExecution) executeRootStep(step *QueryPlanStep) error {
	var document string

	var variables map[string]interface{}
	switch step.ParentType {
	case queryObjectName, mutationObjectName:
		document, variables = formatDocument(q.ctx, q.schema, step.ParentType, step.SelectionSet)
	default:
		return errors.New("expected mutation or query root step")
	}

	req := NewRequest(document).
		WithVariables(variables).
		WithHeaders(GetOutgoingRequestHeadersFromContext(q.ctx)).
		WithOperationName(q.operationName).
		WithOperationType(step.ParentType)

	var data map[string]interface{}
	err := q.graphqlClient.Request(q.ctx, step.ServiceURL, req, &data)
	if err != nil {
		q.writeExecutionResult(step, data, err)
		return nil
	}

	q.writeExecutionResult(step, data, nil)

	for _, childStep := range step.Then {
		boundaryIDs, err := extractAndDedupeBoundaryIDs(data, childStep.InsertionPoint, childStep.ParentType)
		if err != nil {
			return err
		}
		if len(boundaryIDs) == 0 {
			continue
		}

		childStep := childStep
		q.group.Go(func() error {
			return q.executeChildStep(childStep, boundaryIDs)
		})
	}
	return nil
}

func (q *queryExecution) writeExecutionResult(step *QueryPlanStep, data interface{}, err error) {
	result := executionResult{
		ServiceURL:     step.ServiceURL,
		InsertionPoint: step.InsertionPoint,
		Data:           data,
	}
	if err != nil {
		result.Errors = q.createGQLErrors(step, err)
	}

	q.results <- result
}

func (q *queryExecution) executeChildStep(step *QueryPlanStep, boundaryIDs []string) error {
	newRequestCount := atomic.AddInt32(&q.requestCount, 1)
	if newRequestCount > q.maxRequest {
		return fmt.Errorf("exceeded max requests of %v", q.maxRequest)
	}

	boundaryField, err := q.boundaryFields.Field(step.ServiceURL, step.ParentType)
	if err != nil {
		return err
	}

	documents, variables, err := buildBoundaryQueryDocuments(q.ctx, q.schema, step, boundaryIDs, boundaryField, 50)
	if err != nil {
		return err
	}

	data, err := q.executeBoundaryQuery(documents, step.ServiceURL, variables, boundaryField)
	if err != nil {
		q.writeExecutionResult(step, data, err)
		return nil
	}

	q.writeExecutionResult(step, data, nil)

	nonNilBoundaryResults := extractNonNilBoundaryResults(data)

	if len(nonNilBoundaryResults) > 0 {
		for _, childStep := range step.Then {
			boundaryResultInsertionPoint, err := trimInsertionPointForNestedBoundaryStep(nonNilBoundaryResults, childStep.InsertionPoint)
			if err != nil {
				return err
			}
			boundaryIDs, err := extractAndDedupeBoundaryIDs(nonNilBoundaryResults, boundaryResultInsertionPoint, childStep.ParentType)
			if err != nil {
				return err
			}
			if len(boundaryIDs) == 0 {
				continue
			}
			childStep := childStep
			q.group.Go(func() error {
				return q.executeChildStep(childStep, boundaryIDs)
			})
		}
	}

	return nil
}

func extractNonNilBoundaryResults(data []interface{}) []interface{} {
	var nonNilResults []interface{}
	for _, d := range data {
		if d == nil {
			continue
		}
		nonNilResults = append(nonNilResults, d)
	}

	return nonNilResults
}

func (q *queryExecution) executeBoundaryQuery(documents []string, serviceURL string, variables map[string]interface{}, boundaryFieldGetter BoundaryField) ([]interface{}, error) {
	output := make([]interface{}, 0)
	if !boundaryFieldGetter.Array {
		for _, document := range documents {
			req := NewRequest(document).
				WithVariables(variables).
				WithHeaders(GetOutgoingRequestHeadersFromContext(q.ctx)).
				WithOperationName(q.operationName).
				WithOperationType(queryObjectName)

			partialData := make(map[string]interface{})
			err := q.graphqlClient.Request(q.ctx, serviceURL, req, &partialData)
			if err != nil {
				return nil, err
			}
			for _, value := range partialData {
				output = append(output, value)
			}
		}
		return output, nil
	}

	if len(documents) != 1 {
		return nil, errors.New("there should only be a single document for array boundary field lookups")
	}

	data := struct {
		Result []interface{} `json:"_result"`
	}{}

	req := NewRequest(documents[0]).
		WithVariables(variables).
		WithHeaders(GetOutgoingRequestHeadersFromContext(q.ctx)).
		WithOperationName(q.operationName).
		WithOperationType(queryObjectName)

	err := q.graphqlClient.Request(q.ctx, serviceURL, req, &data)
	return data.Result, err
}

func (q *queryExecution) createGQLErrors(step *QueryPlanStep, err error) gqlerror.List {
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

		// if the field has a selection set it's part of the path
		if len(f.SelectionSet) > 0 {
			path = append(path, ast.PathName(f.Alias))
		}
	}

	var gqlErr GraphqlErrors
	var outputErrs gqlerror.List

	switch {
	case errors.As(err, &gqlErr):
		for _, ge := range gqlErr {
			extensions := ge.Extensions
			if extensions == nil {
				extensions = make(map[string]interface{})
			}
			extensions["selectionSet"] = formatSelectionSetSingleLine(q.ctx, q.schema, step.SelectionSet)
			extensions["selectionPath"] = path
			extensions["serviceName"] = step.ServiceName
			extensions["serviceUrl"] = step.ServiceURL

			outputErrs = append(outputErrs, &gqlerror.Error{
				Err:        err,
				Message:    ge.Message,
				Path:       ge.Path,
				Locations:  locs,
				Extensions: extensions,
				Rule:       "",
			})
		}
		return outputErrs

	case os.IsTimeout(err):
		outputErrs = append(outputErrs, &gqlerror.Error{
			Err:       err,
			Message:   "downstream request timed out",
			Path:      path,
			Locations: locs,
			Extensions: map[string]interface{}{
				"selectionSet": formatSelectionSetSingleLine(q.ctx, q.schema, step.SelectionSet),
			},
			Rule: "",
		})

	default:
		outputErrs = append(outputErrs, &gqlerror.Error{
			Err:       err,
			Message:   err.Error(),
			Path:      path,
			Locations: locs,
			Extensions: map[string]interface{}{
				"selectionSet": formatSelectionSetSingleLine(q.ctx, q.schema, step.SelectionSet),
			},
			Rule: "",
		})
	}

	return outputErrs
}

// The insertionPoint represents the level a piece of data should be inserted at, relative to the root of the root step's data.
// However results from a boundary query only contain a portion of that tree. For example, you could
// have insertionPoint: ["foo", "bar", "movies", "movie", "compTitles"], with the below example as the boundary result we're
// crawling for ids:
// [
//
//		 {
//	    "_bramble_id": "MOVIE1",
//	    "compTitles": [
//	      {
//	  	   "_bramble_id": "1"
//			 }
//		   ]
//	  }
//
// ]
//
// We therefore cannot use the insertionPoint as is in order to extract the boundary ids for the next child step.
// This function trims the insertionPoint up until we find a key that exists in both the boundary result and insertionPoint.
// When a match is found, the remainder of the insertionPoint is used, which in this case is only ["compTitles"].
// This logic is only needed when we are already in a child step, which itself contains it's own child steps.
func trimInsertionPointForNestedBoundaryStep(data []interface{}, childInsertionPoint []string) ([]string, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("no boundary results to process")
	}

	firstBoundaryResult, ok := data[0].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("a single boundary result should be a map[string]interface{}")
	}
	for i, point := range childInsertionPoint {
		_, ok := firstBoundaryResult[point]
		if ok {
			return childInsertionPoint[i:], nil
		}
	}
	return nil, fmt.Errorf("could not find any insertion points inside boundary data")
}

func executeBrambleStep(queryPlanStep *QueryPlanStep) (*executionResult, error) {
	result, err := buildTypenameResponseMap(queryPlanStep.SelectionSet, queryPlanStep.ParentType)
	if err != nil {
		return nil, err
	}

	return &executionResult{
		ServiceURL:     internalServiceName,
		InsertionPoint: []string{},
		Data:           result,
	}, nil
}

func buildTypenameResponseMap(selectionSet ast.SelectionSet, parentTypeName string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	for _, field := range selectionSetToFields(selectionSet) {
		if field.SelectionSet != nil {
			if field.Definition.Type.NamedType == "" {
				return nil, fmt.Errorf("buildTypenameResponseMap: expected named type")
			}

			var err error
			result[field.Alias], err = buildTypenameResponseMap(field.SelectionSet, field.Definition.Type.Name())
			if err != nil {
				return nil, err
			}
		} else {
			if field.Name != "__typename" {
				return nil, fmt.Errorf("buildTypenameResponseMap: expected __typename")
			}
			result[field.Alias] = parentTypeName
		}
	}
	return result, nil
}

func extractAndDedupeBoundaryIDs(data interface{}, insertionPoint []string, parentType string) ([]string, error) {
	boundaryIDs, err := extractBoundaryIDs(data, insertionPoint, parentType)
	if err != nil {
		return nil, err
	}
	dedupeMap := make(map[string]struct{}, len(boundaryIDs))
	for _, boundaryID := range boundaryIDs {
		dedupeMap[boundaryID] = struct{}{}
	}

	deduped := make([]string, 0, len(boundaryIDs))
	for id := range dedupeMap {
		deduped = append(deduped, id)
	}

	return deduped, nil
}

func extractBoundaryIDs(data interface{}, insertionPoint []string, parentType string) ([]string, error) {
	ptr := data
	if ptr == nil {
		return nil, nil
	}
	if len(insertionPoint) == 0 {
		switch ptr := ptr.(type) {
		case map[string]interface{}:
			tpe, err := boundaryTypeFromMap(ptr)
			if err != nil {
				return nil, err
			}

			if tpe != parentType {
				return []string{}, nil
			}

			id, err := boundaryIDFromMap(ptr)
			return []string{id}, err
		case []interface{}:
			var result []string
			for _, innerPtr := range ptr {
				ids, err := extractBoundaryIDs(innerPtr, insertionPoint, parentType)
				if err != nil {
					return nil, err
				}
				result = append(result, ids...)
			}
			return result, nil
		default:
			return nil, fmt.Errorf("extractBoundaryIDs: unexpected type: %T", ptr)
		}
	}
	switch ptr := ptr.(type) {
	case map[string]interface{}:
		return extractBoundaryIDs(ptr[insertionPoint[0]], insertionPoint[1:], parentType)
	case []interface{}:
		var result []string
		for _, innerPtr := range ptr {
			ids, err := extractBoundaryIDs(innerPtr, insertionPoint, parentType)
			if err != nil {
				return nil, err
			}
			result = append(result, ids...)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("extractBoundaryIDs: unexpected type: %T", ptr)
	}
}

func buildBoundaryQueryDocuments(ctx context.Context, schema *ast.Schema, step *QueryPlanStep, ids []string, parentTypeBoundaryField BoundaryField, batchSize int) ([]string, map[string]interface{}, error) {
	operation, variables := formatOperation(ctx, step.SelectionSet)

	selectionSetQL := formatSelectionSetSingleLine(ctx, schema, step.SelectionSet)
	if parentTypeBoundaryField.Array {
		var qids []string
		for _, id := range ids {
			qids = append(qids, fmt.Sprintf("%q", id))
		}
		idsQL := fmt.Sprintf("[%s]", strings.Join(qids, ", "))
		return []string{fmt.Sprintf(`query %s { _result: %s(%s: %s) %s }`, operation, parentTypeBoundaryField.Field, parentTypeBoundaryField.Argument, idsQL, selectionSetQL)}, variables, nil
	}

	var (
		documents      []string
		selectionIndex int
	)
	for _, batch := range batchBy(ids, batchSize) {
		var selections []string
		for _, id := range batch {
			selection := fmt.Sprintf("%s: %s(%s: %q) %s", fmt.Sprintf("_%d", selectionIndex), parentTypeBoundaryField.Field, parentTypeBoundaryField.Argument, id, selectionSetQL)
			selections = append(selections, selection)
			selectionIndex++
		}
		document := fmt.Sprintf("query %s { %s }", operation, strings.Join(selections, " "))
		documents = append(documents, document)
	}

	return documents, variables, nil
}

func batchBy(items []string, batchSize int) (batches [][]string) {
	for batchSize < len(items) {
		items, batches = items[batchSize:], append(batches, items[0:batchSize:batchSize])
	}

	return append(batches, items)
}

// When formatting the response data, the shape of the selection set has to potentially be modified to more closely resemble the shape
// of the response. This only happens when running into fragments, there are two cases we need to deal with:
//  1. the selection set of the target fragment has to be unioned with the selection set at the level for which the target fragment is referenced
//  2. if the target fragments are an implementation of an abstract type, we need to use the __typename from the response body to check which
//     implementation was resolved. Any fragments that do not match are dropped from the selection set.
func unionAndTrimSelectionSet(responseObjectTypeName string, schema *ast.Schema, selectionSet ast.SelectionSet) ast.SelectionSet {
	filteredSelectionSet := eliminateUnwantedFragments(responseObjectTypeName, schema, selectionSet)
	return mergeWithTopLevelFragmentFields(filteredSelectionSet)
}

func eliminateUnwantedFragments(responseObjectTypeName string, schema *ast.Schema, selectionSet ast.SelectionSet) ast.SelectionSet {
	var filteredSelectionSet ast.SelectionSet

	for _, selection := range selectionSet {
		var (
			fragmentObjectDefinition *ast.Definition
			fragmentTypeCondition    string
		)
		switch selection := selection.(type) {
		case *ast.Field:
			filteredSelectionSet = append(filteredSelectionSet, selection)

		case *ast.InlineFragment:
			fragmentObjectDefinition = selection.ObjectDefinition
			fragmentTypeCondition = selection.TypeCondition

		case *ast.FragmentSpread:
			fragmentObjectDefinition = selection.ObjectDefinition
			fragmentTypeCondition = selection.Definition.TypeCondition
		}

		if fragmentObjectDefinition != nil && includeFragment(responseObjectTypeName, schema, fragmentObjectDefinition, fragmentTypeCondition) {
			filteredSelectionSet = append(filteredSelectionSet, selection)
		}
	}

	return filteredSelectionSet
}

func includeFragment(responseObjectTypeName string, schema *ast.Schema, objectDefinition *ast.Definition, typeCondition string) bool {
	return !(objectDefinition.IsAbstractType() &&
		fragmentImplementsAbstractType(schema, objectDefinition.Name, typeCondition) &&
		objectTypenameMatchesDifferentFragment(responseObjectTypeName, typeCondition))
}

func fragmentImplementsAbstractType(schema *ast.Schema, abstractObjectTypename, fragmentTypeDefinition string) bool {
	for _, def := range schema.Implements[fragmentTypeDefinition] {
		if def.Name == abstractObjectTypename {
			return true
		}
	}
	return false
}

func objectTypenameMatchesDifferentFragment(typename, fragmentTypeCondition string) bool {
	return fragmentTypeCondition != typename
}

func mergeWithTopLevelFragmentFields(selectionSet ast.SelectionSet) ast.SelectionSet {
	merged := newSelectionSetMerger()

	for _, selection := range selectionSet {
		switch selection := selection.(type) {
		case *ast.Field:
			merged.addField(selection)
		case *ast.InlineFragment:
			fragment := selection
			merged.addInlineFragment(fragment)
		case *ast.FragmentSpread:
			fragment := selection
			merged.addFragmentSpread(fragment)
		}
	}

	return merged.selectionSet
}

type selectionSetMerger struct {
	selectionSet ast.SelectionSet
	seenFields   map[string]*ast.Field
}

func newSelectionSetMerger() *selectionSetMerger {
	return &selectionSetMerger{
		selectionSet: []ast.Selection{},
		seenFields:   make(map[string]*ast.Field),
	}
}

func (s *selectionSetMerger) addField(field *ast.Field) {
	shouldAppend := s.shouldAppendField(field)
	if shouldAppend {
		s.selectionSet = append(s.selectionSet, field)
	}
}

func (s *selectionSetMerger) shouldAppendField(field *ast.Field) bool {
	if seenField, ok := s.seenFields[field.Alias]; ok {
		if seenField.Name == field.Name && seenField.SelectionSet != nil && field.SelectionSet != nil {
			seenField.SelectionSet = append(seenField.SelectionSet, field.SelectionSet...)
		}
		return false
	} else {
		s.seenFields[field.Alias] = field
		return true
	}
}

func (s *selectionSetMerger) addInlineFragment(fragment *ast.InlineFragment) {
	dedupedSelectionSet := s.dedupeFragmentSelectionSet(fragment.SelectionSet)
	if len(dedupedSelectionSet) > 0 {
		fragment.SelectionSet = dedupedSelectionSet
		s.selectionSet = append(s.selectionSet, fragment)
	}
}

func (s *selectionSetMerger) addFragmentSpread(fragment *ast.FragmentSpread) {
	dedupedSelectionSet := s.dedupeFragmentSelectionSet(fragment.Definition.SelectionSet)
	if len(dedupedSelectionSet) > 0 {
		fragment.Definition.SelectionSet = dedupedSelectionSet
		s.selectionSet = append(s.selectionSet, fragment)
	}
}

func (s *selectionSetMerger) dedupeFragmentSelectionSet(selectionSet ast.SelectionSet) ast.SelectionSet {
	var filteredSelectionSet ast.SelectionSet
	for _, selection := range selectionSet {
		switch selection := selection.(type) {
		case *ast.Field:
			shouldAppend := s.shouldAppendField(selection)
			if shouldAppend {
				filteredSelectionSet = append(filteredSelectionSet, selection)
			}
		case *ast.InlineFragment, *ast.FragmentSpread:
			filteredSelectionSet = append(filteredSelectionSet, selection)
		}
	}

	return filteredSelectionSet
}

func extractAndCastTypenameField(result map[string]interface{}) string {
	typeNameInterface, ok := result["_bramble__typename"]
	if !ok {
		return ""
	}

	return typeNameInterface.(string)
}
