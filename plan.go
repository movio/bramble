package bramble

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
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

	steps, err := createSteps(ctx, nil, parentType, "", ctx.Operation.SelectionSet)
	if err != nil {
		return nil, err
	}
	return &QueryPlan{
		RootSteps: steps,
	}, nil
}

func createSteps(ctx *PlanningContext, insertionPoint []string, parentType string, parentLocation string, selectionSet ast.SelectionSet) ([]*QueryPlanStep, error) {
	var result []*QueryPlanStep

	routedSelectionSet, err := routeSelectionSet(ctx, parentType, parentLocation, selectionSet)
	if err != nil {
		return nil, err
	}

	for location, selectionSet := range routedSelectionSet {
		selectionSetForLocation, childrenSteps, err := extractSelectionSet(ctx, insertionPoint, parentType, selectionSet, location)

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

var reservedAliases = map[string]string{
	"_bramble__typename": "__typename",
	"_bramble_id":        IdFieldName,
}

func extractSelectionSet(ctx *PlanningContext, insertionPoint []string, parentType string, input ast.SelectionSet, location string) (ast.SelectionSet, []*QueryPlanStep, error) {
	var selectionSetResult []ast.Selection
	var childrenStepsResult []*QueryPlanStep
	var remoteSelections []ast.Selection
	for _, selection := range input {
		switch selection := selection.(type) {
		case *ast.Field:
			for reservedAlias, requiredName := range reservedAliases {
				if selection.Alias == reservedAlias && selection.Name != requiredName {
					return nil, nil, gqlerror.Errorf("%s.%s: alias \"%s\" is reserved for system use", strings.Join(insertionPoint, "."), reservedAlias, reservedAlias)
				}
			}
			if parentType != queryObjectName && parentType != mutationObjectName && ctx.IsBoundary[parentType] && selection.Name == IdFieldName {
				selectionSetResult = append(selectionSetResult, selection)
				continue
			}
			loc, err := ctx.Locations.URLFor(parentType, location, selection.Name)
			// Errors are returned for unmapped namespace/interface locations (needs refactor)
			if err == nil && loc != location {
				// field transitions to another service location
				remoteSelections = append(remoteSelections, selection)
			} else if selection.SelectionSet == nil {
				// field is a leaf type in the current service
				selectionSetResult = append(selectionSetResult, selection)
			} else {
				// field is a composite type in the current service
				selectionSet, childrenSteps, err := extractSelectionSet(
					ctx,
					append(insertionPoint, selection.Alias),
					selection.Definition.Type.Name(),
					selection.SelectionSet,
					location,
				)
				if err != nil {
					return nil, nil, err
				}
				newField := *selection
				newField.SelectionSet = selectionSet
				selectionSetResult = append(selectionSetResult, &newField)
				childrenStepsResult = append(childrenStepsResult, childrenSteps...)
			}
		case *ast.InlineFragment:
			selectionSet, childrenSteps, err := extractSelectionSet(
				ctx,
				insertionPoint,
				selection.TypeCondition,
				selection.SelectionSet,
				location,
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

	if len(remoteSelections) > 0 {
		// Create child steps for all remote field selections
		childrenSteps, err := createSteps(ctx, insertionPoint, parentType, location, remoteSelections)
		if err != nil {
			return nil, nil, err
		}
		childrenStepsResult = append(childrenStepsResult, childrenSteps...)
	}

	if len(childrenStepsResult) > 1 {
		// Merge steps targeting distinct service/path locations
		mergedSteps := []*QueryPlanStep{}
		mergedStepsMap := map[string]*QueryPlanStep{}
		for _, step := range childrenStepsResult {
			key := strings.Join(append([]string{step.ServiceURL}, step.InsertionPoint...), "/")
			if existingStep, ok := mergedStepsMap[key]; ok {
				existingStep.SelectionSet = append(existingStep.SelectionSet, step.SelectionSet...)
				existingStep.Then = append(existingStep.Then, step.Then...)
			} else {
				mergedStepsMap[key] = step
				mergedSteps = append(mergedSteps, step)
			}
		}
		childrenStepsResult = mergedSteps
	}

	parentDef := ctx.Schema.Types[parentType]
	if parentDef == nil {
		return nil, nil, fmt.Errorf("definition is nil for parentType %v", parentType)
	}
	if parentDef.IsAbstractType() {
		// For abstract types, add an id fragment for all possible boundary
		// implementations. This assures that abstract boundaries always return
		// with an id, even if they didn't make a selection on the returned type
		for implementationName, abstractTypes := range ctx.Schema.Implements {
			if !ctx.IsBoundary[implementationName] {
				continue
			}
			for _, abstractType := range abstractTypes {
				if abstractType.Name != parentType {
					continue
				}
				implementationType := ctx.Schema.Types[implementationName]

				if idDef := implementationType.Fields.ForName(IdFieldName); idDef != nil {
					possibleId := &ast.InlineFragment{
						TypeCondition:    implementationName,
						SelectionSet:     []ast.Selection{&ast.Field{Alias: "_bramble_id", Name: IdFieldName, Definition: idDef}},
						ObjectDefinition: implementationType,
					}
					selectionSetResult = append(selectionSetResult, possibleId)
				}
				break
			}
		}
		selectionSetResult = append(selectionSetResult, &ast.Field{
			Alias:      "_bramble__typename",
			Name:       "__typename",
			Definition: &ast.FieldDefinition{Name: "__typename", Type: ast.NamedType("String", nil)},
		})
	} else if parentType != queryObjectName && parentType != mutationObjectName && ctx.IsBoundary[parentType] {
		// Otherwise, add an id selection to all boundary types
		if idDef := parentDef.Fields.ForName(IdFieldName); idDef != nil {
			selectionSetResult = append(selectionSetResult,
				&ast.Field{Alias: "_bramble_id", Name: IdFieldName, Definition: idDef},
				&ast.Field{Alias: "_bramble__typename", Name: "__typename", Definition: &ast.FieldDefinition{Name: "__typename", Type: ast.NamedType("String", nil)}},
			)
		}
	}
	return selectionSetResult, childrenStepsResult, nil
}

func routeSelectionSet(ctx *PlanningContext, parentType string, parentLocation string, input ast.SelectionSet) (map[string]ast.SelectionSet, error) {
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

// BoundaryField contains the name and format for a boundary query
type BoundaryField struct {
	Field string
	// Name of the received id argument
	Argument string
	// Whether the query is in the array format
	Array bool
}

// BoundaryFieldsMap is a mapping service -> type -> boundary query
type BoundaryFieldsMap map[string]map[string]BoundaryField

// RegisterField registers a boundary field
func (m BoundaryFieldsMap) RegisterField(serviceURL, typeName string, field string, argument string, array bool) {
	if _, ok := m[serviceURL]; !ok {
		m[serviceURL] = make(map[string]BoundaryField)
	}

	// We prefer to use the array based boundary lookup
	_, exists := m[serviceURL][typeName]
	if exists && !array {
		return
	}

	m[serviceURL][typeName] = BoundaryField{Field: field, Argument: argument, Array: array}
}

// Query returns the boundary field for the given service and type
func (m BoundaryFieldsMap) Field(serviceURL, typeName string) (BoundaryField, error) {
	serviceMap, ok := m[serviceURL]
	if !ok {
		return BoundaryField{}, fmt.Errorf("could not find BoundaryFieldsMap entry for service %s", serviceURL)
	}

	field, ok := serviceMap[typeName]
	if !ok {
		return BoundaryField{}, fmt.Errorf("could not find BoundaryFieldsMap entry for typeName %s", typeName)
	}

	return field, nil
}
