package bramble

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"golang.org/x/sync/errgroup"
)

var (
	errNullBubbledToRoot = errors.New("bubbleUpNullValuesInPlace: null bubbled up to root")
)

type executionResult struct {
	ServiceURL     string
	InsertionPoint []string
	Data           interface{}
	Errors         gqlerror.List
}

type queryExecution struct {
	ctx            context.Context
	schema         *ast.Schema
	requestCount   int32
	maxRequest     int32
	graphqlClient  *GraphQLClient
	boundaryFields BoundaryFieldsMap

	group   *errgroup.Group
	results chan executionResult
}

func newQueryExecution(ctx context.Context, client *GraphQLClient, schema *ast.Schema, boundaryFields BoundaryFieldsMap, maxRequest int32) *queryExecution {
	group, ctx := errgroup.WithContext(ctx)
	return &queryExecution{
		ctx:            ctx,
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

	switch operationType := step.ParentType; operationType {
	case queryObjectName, mutationObjectName:
		document = strings.ToLower(operationType) + formatSelectionSet(q.ctx, q.schema, step.SelectionSet)
	default:
		return errors.New("expected mutation or query root step")
	}

	var data map[string]interface{}

	err := q.executeDocument(document, step.ServiceURL, &data)
	if err != nil {
		q.writeExecutionResult(step, data, err)
		return nil
	}

	q.writeExecutionResult(step, data, nil)

	for _, childStep := range step.Then {
		boundaryIDs, err := extractAndDedupeBoundaryIDs(data, childStep.InsertionPoint)
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

func (q *queryExecution) executeDocument(document string, serviceURL string, response interface{}) error {
	req := NewRequest(document).
		WithHeaders(GetOutgoingRequestHeadersFromContext(q.ctx))
	return q.graphqlClient.Request(q.ctx, serviceURL, req, &response)
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
	atomic.AddInt32(&q.requestCount, 1)
	if atomic.LoadInt32(&q.requestCount) > q.maxRequest {
		return fmt.Errorf("exceeded max requests of %v", q.maxRequest)
	}

	boundaryField, err := q.boundaryFields.Field(step.ServiceURL, step.ParentType)
	if err != nil {
		return err
	}

	documents, err := buildBoundaryQueryDocuments(q.ctx, q.schema, step, boundaryIDs, boundaryField, 50)
	if err != nil {
		return err
	}

	data, err := q.executeBoundaryQuery(documents, step.ServiceURL, boundaryField)
	if err != nil {
		q.writeExecutionResult(step, data, err)
		return nil
	}

	q.writeExecutionResult(step, data, nil)

	nonNillBoundaryResults := extractNonNilBoundaryResults(data)

	if len(nonNillBoundaryResults) > 0 {
		for _, childStep := range step.Then {
			boundaryResultInsertionPoint, err := trimInsertionPointForNestedBoundaryStep(nonNillBoundaryResults, childStep.InsertionPoint)
			if err != nil {
				return err
			}
			boundaryIDs, err := extractAndDedupeBoundaryIDs(nonNillBoundaryResults, boundaryResultInsertionPoint)
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
		if d != nil {
			nonNilResults = append(nonNilResults, d)
		}

	}

	return nonNilResults
}

func (q *queryExecution) executeBoundaryQuery(documents []string, serviceURL string, boundaryFieldGetter BoundaryField) ([]interface{}, error) {
	output := make([]interface{}, 0)
	if !boundaryFieldGetter.Array {
		for _, document := range documents {
			partialData := make(map[string]interface{})
			err := q.executeDocument(document, serviceURL, &partialData)
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

	err := q.executeDocument(documents[0], serviceURL, &data)
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
	if errors.As(err, &gqlErr) {
		for _, ge := range gqlErr {
			extensions := ge.Extensions
			if extensions == nil {
				extensions = make(map[string]interface{})
			}
			extensions["selectionSet"] = formatSelectionSetSingleLine(q.ctx, q.schema, step.SelectionSet)
			extensions["serviceName"] = step.ServiceName
			extensions["serviceUrl"] = step.ServiceURL

			outputErrs = append(outputErrs, &gqlerror.Error{
				Message:    ge.Message,
				Path:       path,
				Locations:  locs,
				Extensions: extensions,
			})
		}
		return outputErrs
	} else {
		outputErrs = append(outputErrs, &gqlerror.Error{
			Message:   err.Error(),
			Path:      path,
			Locations: locs,
			Extensions: map[string]interface{}{
				"selectionSet": formatSelectionSetSingleLine(q.ctx, q.schema, step.SelectionSet),
			},
		})
	}

	return outputErrs
}

// The insertionPoint represents the level a piece of data should be inserted at, relative to the root of the root step's data.
// However results from a boundary query only contain a portion of that tree. For example, you could
// have insertionPoint: ["foo", "bar", "movies", "movie", "compTitles"], with the below example as the boundary result we're
// crawling for ids:
// [
// 	 {
//     "_bramble_id": "MOVIE1",
//     "compTitles": [
//       {
//   	   "_bramble_id": "1"
// 		 }
//	   ]
//   }
// ]
//
// We therefore cannot use the insertionPoint as is in order to extract the boundary ids for the next child step.
// This function trims the insertionPoint up until we find a key that exists in both the boundary result and insertionPoint.
// When a match is found, the remainder of the insertionPoint is used, which in this case is only ["compTitles"].
// This logic is only needed when we are already in a child step, which itself contains it's own child steps.
func trimInsertionPointForNestedBoundaryStep(data []interface{}, childInsertionPoint []string) ([]string, error) {
	if len(data) < 1 {
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

func fragmentImplementsAbstractType(schema *ast.Schema, abstractObjectTypename, fragmentTypeDefinition string) bool {
	for _, def := range schema.Implements[fragmentTypeDefinition] {
		if def.Name == abstractObjectTypename {
			return true
		}
	}
	return false
}

func extractAndDedupeBoundaryIDs(data interface{}, insertionPoint []string) ([]string, error) {
	boundaryIDs, err := extractBoundaryIDs(data, insertionPoint)
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

	return sort.StringSlice(deduped), nil
}

func extractBoundaryIDs(data interface{}, insertionPoint []string) ([]string, error) {
	ptr := data
	if ptr == nil {
		return nil, nil
	}
	if len(insertionPoint) == 0 {
		switch ptr := ptr.(type) {
		case map[string]interface{}:
			id, err := boundaryIDFromMap(ptr)
			return []string{id}, err
		case []interface{}:
			result := []string{}
			for _, innerPtr := range ptr {
				ids, err := extractBoundaryIDs(innerPtr, insertionPoint)
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
		return extractBoundaryIDs(ptr[insertionPoint[0]], insertionPoint[1:])
	case []interface{}:
		result := []string{}
		for _, innerPtr := range ptr {
			ids, err := extractBoundaryIDs(innerPtr, insertionPoint)
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

func buildBoundaryQueryDocuments(ctx context.Context, schema *ast.Schema, step *QueryPlanStep, ids []string, parentTypeBoundaryField BoundaryField, batchSize int) ([]string, error) {
	selectionSetQL := formatSelectionSetSingleLine(ctx, schema, step.SelectionSet)
	if parentTypeBoundaryField.Array {
		qids := []string{}
		for _, id := range ids {
			qids = append(qids, fmt.Sprintf("%q", id))
		}
		idsQL := fmt.Sprintf("[%s]", strings.Join(qids, ", "))
		return []string{fmt.Sprintf(`{ _result: %s(%s: %s) %s }`, parentTypeBoundaryField.Field, parentTypeBoundaryField.Argument, idsQL, selectionSetQL)}, nil
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
		document := "{ " + strings.Join(selections, " ") + " }"
		documents = append(documents, document)
	}

	return documents, nil
}

func batchBy(items []string, batchSize int) (batches [][]string) {
	for batchSize < len(items) {
		items, batches = items[batchSize:], append(batches, items[0:batchSize:batchSize])
	}

	return append(batches, items)
}

func mergeExecutionResults(results []executionResult) (map[string]interface{}, error) {
	if len(results) == 0 {
		return nil, errors.New("mergeExecutionResults: nothing to merge")
	}

	if len(results) == 1 {
		data := results[0].Data
		if data == nil {
			return nil, nil
		}

		dataMap, ok := data.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("a complete graphql response should be map[string]interface{}, got %T", results[0].Data)
		}
		return dataMap, nil
	}

	data := results[0].Data
	for _, result := range results[1:] {
		if err := mergeExecutionResultsRec(result.Data, data, result.InsertionPoint); err != nil {
			return nil, err
		}
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("merged execution results should be map[string]interface{}, got %T", data)
	}

	return dataMap, nil
}

func mergeExecutionResultsRec(src interface{}, dst interface{}, insertionPoint []string) error {
	// base case
	if len(insertionPoint) == 0 {
		switch ptr := dst.(type) {
		case nil:
			return nil
		case map[string]interface{}:
			switch src := src.(type) {
			// base case for root step merging
			case map[string]interface{}:
				mergeMaps(ptr, src)

			// base case for children step merging
			case []interface{}:
				boundaryResults, err := getBoundaryFieldResults(src)
				if err != nil {
					return err
				}

				dstID, err := boundaryIDFromMap(ptr)
				if err != nil {
					return err
				}

				for _, result := range boundaryResults {
					srcID, err := boundaryIDFromMap(result)
					if err != nil {
						return err
					}
					if srcID == dstID {
						for k, v := range result {
							if k == "_bramble_id" {
								continue
							}

							ptr[k] = v
						}
					}
				}

			}
		case []interface{}:
			for _, innerPtr := range ptr {
				if err := mergeExecutionResultsRec(src, innerPtr, insertionPoint); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("mergeExecutionResultsRec: unxpected type '%T' for top-level merge", ptr)
		}
		return nil
	}

	// recursive case
	switch ptr := dst.(type) {
	case map[string]interface{}:
		switch ptr := ptr[insertionPoint[0]].(type) {
		case []interface{}:
			for _, innerPtr := range ptr {
				if err := mergeExecutionResultsRec(src, innerPtr, insertionPoint[1:]); err != nil {
					return err
				}
			}
		default:
			if err := mergeExecutionResultsRec(src, ptr, insertionPoint[1:]); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, innerPtr := range ptr {
			if err := mergeExecutionResultsRec(src, innerPtr, insertionPoint); err != nil {
				return err
			}
		}
	case nil:
		// The destination is nil, so the src can not be merged
		return nil
	default:
		return fmt.Errorf("mergeExecutionResultsRec: unxpected type '%T' for non top-level merge", ptr)
	}
	return nil
}

func boundaryIDFromMap(boundaryMap map[string]interface{}) (string, error) {
	id, ok := boundaryMap["_bramble_id"].(string)
	if ok {
		return id, nil
	}
	return "", errors.New("boundaryIDFromMap: \"_bramble_id\" not found")
}

func getBoundaryFieldResults(src []interface{}) ([]map[string]interface{}, error) {
	var results []map[string]interface{}
	for i, element := range src {
		if element == nil {
			continue
		}
		elementMap, ok := element.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("getBoundaryFieldResults: expect value at index %d to be map[string]interface{}' but got '%T'", i, element)
		}
		results = append(results, elementMap)
	}
	return results, nil
}

// bubbleUpNullValuesInPlace checks for expected null values (as per schema) and bubbles them up if needed, and checks for
// unexpected null values and returns errors for each (these unexpected nulls are also bubbled up).
// See https://spec.graphql.org/June2018/#sec-Errors-and-Non-Nullability
func bubbleUpNullValuesInPlace(schema *ast.Schema, selectionSet ast.SelectionSet, result map[string]interface{}) ([]*gqlerror.Error, error) {
	errs, bubbleUp, err := bubbleUpNullValuesInPlaceRec(schema, nil, selectionSet, result, ast.Path{})
	if err != nil {
		return nil, err
	}
	if bubbleUp {
		return errs, errNullBubbledToRoot
	}
	return errs, nil
}

func bubbleUpNullValuesInPlaceRec(schema *ast.Schema, currentType *ast.Type, selectionSet ast.SelectionSet, result interface{}, path ast.Path) (errs []*gqlerror.Error, bubbleUp bool, err error) {
	switch result := result.(type) {
	case map[string]interface{}:
		objectTypename := extractAndCastTypenameField(result)
		filteredSelectionSet := unionAndTrimSelectionSet(objectTypename, schema, selectionSet)

		for _, selection := range filteredSelectionSet {
			switch selection := selection.(type) {
			case *ast.Field:
				field := selection
				if strings.HasPrefix(field.Name, "__") {
					continue
				}
				value := result[field.Alias]
				if value == nil {
					if field.Definition.Type.NonNull {
						errs = append(errs, &gqlerror.Error{
							Message:    fmt.Sprintf("got a null response for non-nullable field %q", field.Alias),
							Path:       append(path, ast.PathName(field.Alias)),
							Extensions: nil,
						})
						bubbleUp = true
					}
					return
				}
				if field.SelectionSet != nil {
					lowerErrs, lowerBubbleUp, lowerErr := bubbleUpNullValuesInPlaceRec(schema, field.Definition.Type, field.SelectionSet, value, append(path, ast.PathName(field.Alias)))
					if lowerErr != nil {
						return nil, false, lowerErr
					}
					if lowerBubbleUp {
						if field.Definition.Type.NonNull {
							bubbleUp = true
						} else {
							result[field.Alias] = nil
						}
					}
					errs = append(errs, lowerErrs...)
				}
			case *ast.FragmentSpread:
				fragment := selection
				lowerErrs, lowerBubbleUp, lowerErr := bubbleUpNullValuesInPlaceRec(schema, nil, fragment.Definition.SelectionSet, result, path)
				if lowerErr != nil {
					return nil, false, lowerErr
				}
				bubbleUp = lowerBubbleUp
				errs = append(errs, lowerErrs...)
			case *ast.InlineFragment:
				fragment := selection
				lowerErrs, lowerBubbleUp, lowerErr := bubbleUpNullValuesInPlaceRec(schema, nil, fragment.SelectionSet, result, path)
				if lowerErr != nil {
					return nil, false, lowerErr
				}
				bubbleUp = lowerBubbleUp
				errs = append(errs, lowerErrs...)
			default:
				err = fmt.Errorf("unknown selection type: %T", selection)
				return
			}
		}
	case []interface{}:
		for i, value := range result {
			pathWithIndex := appendPathIndex(path, i)
			lowerErrs, lowerBubbleUp, lowerErr := bubbleUpNullValuesInPlaceRec(schema, currentType, selectionSet, value, pathWithIndex)
			if lowerErr != nil {
				return nil, false, lowerErr
			}
			if lowerBubbleUp {
				if currentType.Elem.NonNull {
					bubbleUp = true
				} else {
					result[i] = nil
				}
			}
			errs = append(errs, lowerErrs...)
		}
	case []map[string]interface{}:
		for i, value := range result {
			pathWithIndex := appendPathIndex(path, i)
			lowerErrs, lowerBubbleUp, lowerErr := bubbleUpNullValuesInPlaceRec(schema, currentType, selectionSet, value, pathWithIndex)
			if lowerErr != nil {
				return nil, false, lowerErr
			}
			if lowerBubbleUp {
				if currentType.Elem.NonNull {
					bubbleUp = true
				} else {
					result[i] = nil
				}
			}
			errs = append(errs, lowerErrs...)
		}
	default:
		return nil, false, fmt.Errorf("bubbleUpNullValuesInPlaceRec: unxpected result type '%T'", result)
	}
	return
}

func appendPathIndex(path []ast.PathElement, index int) []ast.PathElement {
	pathCopy := make([]ast.PathElement, len(path))
	copy(pathCopy, path)
	return append(pathCopy, ast.PathIndex(index))
}

func formatResponseData(schema *ast.Schema, selectionSet ast.SelectionSet, result map[string]interface{}) []byte {
	return formatResponseDataRec(schema, selectionSet, result, false)
}

func formatResponseDataRec(schema *ast.Schema, selectionSet ast.SelectionSet, result interface{}, insideFragment bool) []byte {
	var buf bytes.Buffer
	if result == nil {
		return []byte("null")
	}
	switch result := result.(type) {
	case map[string]interface{}:
		if len(result) == 0 {
			return []byte("null")
		}
		if !insideFragment {
			buf.WriteString("{")
		}

		objectTypename := extractAndCastTypenameField(result)
		filteredSelectionSet := unionAndTrimSelectionSet(objectTypename, schema, selectionSet)

		for i, selection := range filteredSelectionSet {
			switch selection := selection.(type) {
			case *ast.InlineFragment:
				innerBody := formatResponseDataRec(schema, selection.SelectionSet, result, true)
				buf.Write(innerBody)

			case *ast.FragmentSpread:
				innerBody := formatResponseDataRec(schema, selection.Definition.SelectionSet, result, true)
				buf.Write(innerBody)
			case *ast.Field:
				field := selection
				fieldData, ok := result[field.Alias]
				buf.WriteString(fmt.Sprintf(`"%s":`, field.Alias))
				if !ok {
					buf.WriteString("null")
					if i < len(filteredSelectionSet)-1 {
						buf.WriteString(",")
					}
					continue
				}
				if field.SelectionSet != nil && len(field.SelectionSet) > 0 {
					innerBody := formatResponseDataRec(schema, field.SelectionSet, fieldData, false)
					buf.Write(innerBody)
				} else {
					fieldJSON, err := json.Marshal(&fieldData)
					if err != nil {
						// We panic here because the data we're working on has already come through
						// from downstream services as JSON. We should never get to this point with invalid JSON.
						log.Panicf("invalid json when formatting response: %v", err)
					}

					buf.Write(fieldJSON)
				}
			}
			if i < len(filteredSelectionSet)-1 {
				buf.WriteString(",")
			}
		}
		if !insideFragment {
			buf.WriteString("}")
		}
	case []interface{}:
		buf.WriteString("[")
		for i, v := range result {
			innerBody := formatResponseDataRec(schema, selectionSet, v, false)
			buf.Write(innerBody)

			if i < len(result)-1 {
				buf.WriteString(",")
			}
		}
		buf.WriteString("]")
	case []map[string]interface{}:
		buf.WriteString("[")
		for i, v := range result {
			innerBody := formatResponseDataRec(schema, selectionSet, v, false)
			buf.Write(innerBody)

			if i < len(result)-1 {
				buf.WriteString(",")
			}
		}
		buf.WriteString("]")
	}

	return buf.Bytes()
}

// When formatting the response data, the shape of the selection set has to potentially be modified to more closely resemble the shape
// of the response. This only happens when running into fragments, there are two cases we need to deal with:
//   1. the selection set of the target fragment has to be unioned with the selection set at the level for which the target fragment is referenced
//   2. if the target fragments are an implementation of an abstract type, we need to use the __typename from the response body to check which
//   implementation was resolved. Any fragments that do not match are dropped from the selection set.
func unionAndTrimSelectionSet(objectTypename string, schema *ast.Schema, selectionSet ast.SelectionSet) ast.SelectionSet {
	return unionAndTrimSelectionSetRec(objectTypename, schema, selectionSet, map[string]*ast.Field{})
}

func unionAndTrimSelectionSetRec(objectTypename string, schema *ast.Schema, selectionSet ast.SelectionSet, seenFields map[string]*ast.Field) ast.SelectionSet {
	var filteredSelectionSet ast.SelectionSet
	for _, selection := range selectionSet {
		switch selection := selection.(type) {
		case *ast.Field:
			if seenField, ok := seenFields[selection.Alias]; ok {
				if seenField.Name == selection.Name && seenField.SelectionSet != nil && selection.SelectionSet != nil {
					seenField.SelectionSet = append(seenField.SelectionSet, selection.SelectionSet...)
				}
			} else {
				seenFields[selection.Alias] = selection
				filteredSelectionSet = append(filteredSelectionSet, selection)
			}
		case *ast.InlineFragment:
			fragment := selection
			if fragment.ObjectDefinition.IsAbstractType() &&
				fragmentImplementsAbstractType(schema, fragment.ObjectDefinition.Name, fragment.TypeCondition) &&
				objectTypenameMatchesDifferentFragment(objectTypename, fragment) {
				continue
			}

			filteredSelections := unionAndTrimSelectionSetRec(objectTypename, schema, fragment.SelectionSet, seenFields)
			if len(filteredSelections) > 0 {
				fragment.SelectionSet = filteredSelections
				filteredSelectionSet = append(filteredSelectionSet, selection)
			}
		case *ast.FragmentSpread:
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

func objectTypenameMatchesDifferentFragment(typename string, fragment *ast.InlineFragment) bool {
	return fragment.TypeCondition != typename
}
