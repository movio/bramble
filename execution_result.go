package bramble

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

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
	switch ptr := data.(type) {
	case nil:
		data = make(map[string]interface{})
	case map[string]interface{}:
		if ptr == nil {
			data = make(map[string]interface{})
		}
	}

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

				dstType, err := boundaryTypeFromMap(ptr)
				if err != nil {
					return err
				}

				for _, result := range boundaryResults {
					srcType, err := boundaryTypeFromMap(result)
					if err != nil {
						return err
					}

					if srcType != dstType {
						continue
					}

					dstID, err := boundaryIDFromMap(ptr)
					if err != nil {
						return err
					}

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
	return "", fmt.Errorf(`boundaryIDFromMap: "_bramble_id" not found`)
}

func boundaryTypeFromMap(boundaryMap map[string]interface{}) (string, error) {
	tpe, ok := boundaryMap["_bramble__typename"].(string)
	if ok {
		return tpe, nil
	}
	return "", fmt.Errorf(`boundaryTypeFromMap: "_bramble__typename" not found`)
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

		itemWritten := false
		for _, selection := range filteredSelectionSet {
			var innerBody []byte
			switch selection := selection.(type) {
			case *ast.InlineFragment:
				innerBody = formatResponseDataRec(schema, selection.SelectionSet, result, true)
			case *ast.FragmentSpread:
				innerBody = formatResponseDataRec(schema, selection.Definition.SelectionSet, result, true)
			case *ast.Field:
				field := selection
				var innerBuf bytes.Buffer
				fmt.Fprintf(&innerBuf, `"%s":`, field.Alias)
				fieldData, ok := result[field.Alias]
				if !ok {
					innerBuf.WriteString("null")
				} else if field.SelectionSet != nil && len(field.SelectionSet) > 0 {
					val := formatResponseDataRec(schema, field.SelectionSet, fieldData, false)
					innerBuf.Write(val)
				} else {
					fieldJSON, err := json.Marshal(&fieldData)
					if err != nil {
						// We panic here because the data we're working on has already come through
						// from downstream services as JSON. We should never get to this point with invalid JSON.
						log.Panicf("invalid json when formatting response: %v", err)
					}
					innerBuf.Write(fieldJSON)
				}
				innerBody = innerBuf.Bytes()
			}
			if len(innerBody) > 0 {
				if itemWritten {
					buf.WriteString(",")
				}
				buf.Write(innerBody)
				itemWritten = true
			}
		}
		if !insideFragment {
			buf.WriteString("}")
		}
	case []interface{}:
		buf.WriteString("[")
		for i, v := range result {
			if i > 0 {
				buf.WriteString(",")
			}
			innerBody := formatResponseDataRec(schema, selectionSet, v, false)
			buf.Write(innerBody)
		}
		buf.WriteString("]")
	case []map[string]interface{}:
		buf.WriteString("[")
		for i, v := range result {
			if i > 0 {
				buf.WriteString(",")
			}
			innerBody := formatResponseDataRec(schema, selectionSet, v, false)
			buf.Write(innerBody)
		}
		buf.WriteString("]")
	}

	return buf.Bytes()
}
