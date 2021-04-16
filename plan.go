package bramble

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
)

// QueryPlanStep is a single execution step
type QueryPlanStep struct {
	ServiceURL     string
	ServiceName    string
	ParentType     string
	SelectionSet   ast.SelectionSet
	InsertionPoint []string
	Then           []*QueryPlanStep
}

// MarshalJSON marshals the step the JSON
func (s *QueryPlanStep) MarshalJSON() ([]byte, error) {
	ctx := graphql.WithOperationContext(context.Background(), &graphql.OperationContext{
		Variables: map[string]interface{}{},
	})
	return json.Marshal(&struct {
		ServiceURL     string
		ParentType     string
		SelectionSet   string
		InsertionPoint []string
		Then           []*QueryPlanStep
	}{
		ServiceURL:     s.ServiceURL,
		ParentType:     s.ParentType,
		SelectionSet:   formatSelectionSetSingleLine(ctx, nil, s.SelectionSet),
		InsertionPoint: s.InsertionPoint,
		Then:           s.Then,
	})
}

// QueryPlan is a query execution plan
type QueryPlan struct {
	RootSteps []*QueryPlanStep
}

// PlanningContext contains the necessary information used to plan a query.
type PlanningContext struct {
	Operation  *ast.OperationDefinition
	Schema     *ast.Schema
	Locations  FieldURLMap
	IsBoundary map[string]bool
	Services   map[string]*Service
}

// Plan returns a query plan from the given planning context
func Plan(ctx *PlanningContext) (*QueryPlan, error) {
	var parentType string
	switch ctx.Operation.Operation {
	case ast.Query:
		parentType = queryObjectName
	case ast.Mutation:
		parentType = mutationObjectName
	default:
		return nil, fmt.Errorf("not implemented")
	}

	steps, err := createSteps(ctx, nil, parentType, "", ctx.Operation.SelectionSet, false)
	if err != nil {
		return nil, err
	}
	return &QueryPlan{
		RootSteps: steps,
	}, nil
}

func createSteps(ctx *PlanningContext, insertionPoint []string, parentType, parentLocation string, selectionSet ast.SelectionSet, childstep bool) ([]*QueryPlanStep, error) {
	var result []*QueryPlanStep

	routedSelectionSet, err := routeSelectionSet(ctx, parentType, parentLocation, selectionSet)
	if err != nil {
		return nil, err
	}

	for location, selectionSet := range routedSelectionSet {
		selectionSetForLocation, childrenSteps, err := extractSelectionSet(ctx, insertionPoint, parentType, selectionSet, location, childstep)

		if err != nil {
			return nil, err
		}
		name := "unknown"
		if service, ok := ctx.Services[location]; ok {
			name = service.Name
		}

		// the insertionPoint slice can be modified later as we're appending
		// values to it while recursively traversing the selection set, so we
		// need to make a copy
		var insertionPointCopy []string
		if len(insertionPoint) > 0 {
			insertionPointCopy = make([]string, len(insertionPoint))
			copy(insertionPointCopy, insertionPoint)
		}

		result = append(result, &QueryPlanStep{
			InsertionPoint: insertionPointCopy,
			Then:           childrenSteps,
			ServiceURL:     location,
			ServiceName:    name,
			ParentType:     parentType,
			SelectionSet:   selectionSetForLocation,
		})
	}
	return result, nil
}

func extractSelectionSet(ctx *PlanningContext, insertionPoint []string, parentType string, input ast.SelectionSet, location string, childstep bool) (ast.SelectionSet, []*QueryPlanStep, error) {
	var selectionSetResult []ast.Selection
	var childrenStepsResult []*QueryPlanStep
	for _, selection := range input {
		switch selection := selection.(type) {
		case *ast.Field:
			if parentType != queryObjectName && parentType != mutationObjectName && ctx.IsBoundary[parentType] && selection.Name == "id" {
				selectionSetResult = append(selectionSetResult, selection)
				continue
			}
			loc, err := ctx.Locations.URLFor(parentType, location, selection.Name)
			if err != nil {
				// namespace
				subSS, steps, err := extractSelectionSet(ctx, append(insertionPoint, selection.Name), selection.Definition.Type.Name(), selection.SelectionSet, location, childstep)
				if err != nil {
					return nil, nil, err
				}
				selection.SelectionSet = subSS
				selectionSetResult = append(selectionSetResult, selection)
				childrenStepsResult = append(childrenStepsResult, steps...)
				continue
			}
			if loc == location {
				if selection.SelectionSet == nil {
					selectionSetResult = append(selectionSetResult, selection)
				} else {
					newField := *selection
					selectionSet, childrenSteps, err := extractSelectionSet(
						ctx,
						append(insertionPoint, selection.Alias),
						selection.Definition.Type.Name(),
						selection.SelectionSet,
						location,
						childstep,
					)
					if err != nil {
						return nil, nil, err
					}
					newField.SelectionSet = selectionSet
					selectionSetResult = append(selectionSetResult, &newField)
					childrenStepsResult = append(childrenStepsResult, childrenSteps...)
				}
			} else {
				mergedWithExistingStep := false
				for _, step := range childrenStepsResult {
					if stringArraysEqual(step.InsertionPoint, insertionPoint) && step.ServiceURL == loc {
						step.SelectionSet = append(step.SelectionSet, selection)
						mergedWithExistingStep = true
						break
					}
				}

				if !mergedWithExistingStep {
					newSelectionSet := []ast.Selection{selection}
					childrenSteps, err := createSteps(ctx, insertionPoint, parentType, location, newSelectionSet, true)
					if err != nil {
						return nil, nil, err
					}
					childrenStepsResult = append(childrenStepsResult, childrenSteps...)
				}
			}
		case *ast.InlineFragment:
			selectionSet, childrenSteps, err := extractSelectionSet(
				ctx,
				insertionPoint,
				selection.TypeCondition,
				selection.SelectionSet,
				location,
				childstep,
			)
			if err != nil {
				return nil, nil, err
			}
			inlineFragment := *selection
			inlineFragment.SelectionSet = selectionSet
			selectionSetResult = append(selectionSetResult, &inlineFragment)
			childrenStepsResult = append(childrenStepsResult, childrenSteps...)
		case *ast.FragmentSpread:
			selectionSet, childrenSteps, err := extractSelectionSet(
				ctx,
				insertionPoint,
				selection.Definition.TypeCondition,
				selection.Definition.SelectionSet,
				location,
				childstep,
			)
			if err != nil {
				return nil, nil, err
			}
			inlineFragment := ast.InlineFragment{
				TypeCondition: selection.Definition.TypeCondition,
				SelectionSet:  selectionSet,
			}
			selectionSetResult = append(selectionSetResult, &inlineFragment)
			childrenStepsResult = append(childrenStepsResult, childrenSteps...)
		default:
			return nil, nil, fmt.Errorf("unexpected %T in SelectionSet", selection)
		}
	}

	// We need to add the id field only if it's a boundary type and the result
	// is going to be merged with another step (we have children steps or it's a
	// child step).
	if parentType != queryObjectName && parentType != mutationObjectName &&
		ctx.IsBoundary[parentType] &&
		ctx.Schema.Types[parentType].Fields.ForName("id") != nil &&
		(childstep || len(childrenStepsResult) > 0) {
		if !selectionSetHasFieldNamed(selectionSetResult, "id") {
			id := &ast.Field{
				Alias:      "_id",
				Name:       "id",
				Definition: ctx.Schema.Types[parentType].Fields.ForName("id"),
			}
			selectionSetResult = append([]ast.Selection{id}, selectionSetResult...)
		}
	}
	return selectionSetResult, childrenStepsResult, nil
}

func routeSelectionSet(ctx *PlanningContext, parentType, parentLocation string, input ast.SelectionSet) (map[string]ast.SelectionSet, error) {
	result := map[string]ast.SelectionSet{}
	if parentLocation == "" {
		// if we're at the root, we extract the selection set for each service
		for _, svc := range ctx.Services {
			loc := svc.ServiceURL
			if parentLocation != "" && loc != parentLocation {
				continue
			}
			ss := filterSelectionSetByLoc(ctx, input, loc, parentType)
			if len(ss) > 0 {
				result[loc] = ss
			}
		}
		// filter fields living only on the gateway
		if ss := filterSelectionSetByLoc(ctx, input, internalServiceName, parentType); len(ss) > 0 {
			result[internalServiceName] = ss
		}

		return result, nil
	}

	for _, selection := range input {
		switch selection := selection.(type) {
		case *ast.Field:
			if isGraphQLBuiltinName(selection.Name) && parentLocation == "" {
				continue
			}
			loc, err := ctx.Locations.URLFor(parentType, parentLocation, selection.Name)
			if err != nil {
				return nil, err
			}
			result[loc] = append(result[loc], selection)
		case *ast.InlineFragment:
			inner, err := routeSelectionSet(ctx, parentType, parentLocation, selection.SelectionSet)
			if err != nil {
				return nil, err
			}
			for loc, selectionSet := range inner {
				inlineFragment := *selection
				inlineFragment.SelectionSet = selectionSet
				result[loc] = append(result[loc], &inlineFragment)
			}
		case *ast.FragmentSpread:
			inner, err := routeSelectionSet(ctx, parentType, parentLocation, selection.Definition.SelectionSet)
			if err != nil {
				return nil, err
			}
			for loc, selectionSet := range inner {
				result[loc] = append(result[loc], selectionSet...)
			}
		}
	}
	return result, nil
}

func filterSelectionSetByLoc(ctx *PlanningContext, ss ast.SelectionSet, loc, parentType string) ast.SelectionSet {
	var res ast.SelectionSet
	for _, selection := range selectionSetToFields(ss) {
		fieldLocation, err := ctx.Locations.URLFor(parentType, "", selection.Name)
		if err != nil {
			// Namespace
			subSS := filterSelectionSetByLoc(ctx, selection.SelectionSet, loc, selection.Definition.Type.Name())
			if len(subSS) == 0 {
				continue
			}
			s := *selection
			s.SelectionSet = subSS
			res = append(res, &s)
		} else if fieldLocation == loc {
			res = append(res, selection)
		} else if loc == internalServiceName && selection.Name == "__typename" {
			// __typename fields on namespaces
			res = append(res, selection)
		}
	}

	return res
}

func selectionSetHasFieldNamed(selectionSet []ast.Selection, fieldName string) bool {
	for _, selection := range selectionSet {
		field, ok := selection.(*ast.Field)
		if ok && field.Name == fieldName {
			return true
		}
	}
	return false
}

// FieldURLMap maps fields to service URLs
type FieldURLMap map[string]string

// URLFor returns the URL for the given field
func (m FieldURLMap) URLFor(parent, parentLocation, field string) (string, error) {
	if field == "__typename" {
		return parentLocation, nil
	}
	key := m.keyFor(parent, field)
	value, exists := m[key]
	if !exists {
		return "", fmt.Errorf("could not find location for %q", key)
	}
	return value, nil
}

// RegisterURL registers the location for the given field
func (m FieldURLMap) RegisterURL(parent string, field string, location string) {
	key := m.keyFor(parent, field)
	m[key] = location
}

func (m FieldURLMap) keyFor(parent string, field string) string {
	return fmt.Sprintf("%s.%s", parent, field)
}

func stringArraysEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i, v := range a {
		if v != b[i] {
			return false
		}
	}

	return true
}

// BoundaryQuery contains the name and format for a boundary query
type BoundaryQuery struct {
	Query string
	// Whether the query is in the array format
	Array bool
}

// BoundaryQueriesMap is a mapping service -> type -> boundary query
type BoundaryQueriesMap map[string]map[string]BoundaryQuery

// RegisterQuery registers a boundary query
func (m BoundaryQueriesMap) RegisterQuery(serviceURL, typeName, query string, array bool) {
	if _, ok := m[serviceURL]; !ok {
		m[serviceURL] = make(map[string]BoundaryQuery)
	}

	m[serviceURL][typeName] = BoundaryQuery{Query: query, Array: array}
}

// Query returns the boundary query for the given service and type
func (m BoundaryQueriesMap) Query(serviceURL, typeName string) BoundaryQuery {
	serviceMap, ok := m[serviceURL]
	if !ok {
		return BoundaryQuery{Query: "node"}
	}

	query, ok := serviceMap[typeName]
	if !ok {
		return BoundaryQuery{Query: "node"}
	}

	return query
}
