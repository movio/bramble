package bramble

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

type AllowedFields struct {
	AllowAll         bool
	AllowedSubfields map[string]AllowedFields
}

// IsAllowed returns whether the sub field is allowed along with the
// permissions for its own subfields
func (a AllowedFields) IsAllowed(fieldName string) (bool, AllowedFields) {
	if fieldName == "__schema" || fieldName == "__type" {
		return true, AllowedFields{AllowAll: true}
	}
	if fieldName == "__typename" {
		return true, AllowedFields{}
	}

	if f, ok := a.AllowedSubfields[fieldName]; ok {
		return true, f
	}

	return false, AllowedFields{}
}

// OperationPermissions represents the user permissions for all operation types
type OperationPermissions struct {
	AllowedRootQueryFields        AllowedFields `json:"query"`
	AllowedRootMutationFields     AllowedFields `json:"mutation"`
	AllowedRootSubscriptionFields AllowedFields `json:"subscription"`
}

func (a AllowedFields) MarshalJSON() ([]byte, error) {
	if a.AllowAll {
		return json.Marshal("*")
	}
	fields := make([]string, 0, len(a.AllowedSubfields))
	for field, subfields := range a.AllowedSubfields {
		if !subfields.AllowAll {
			return json.Marshal(a.AllowedSubfields)
		}
		fields = append(fields, field)
	}
	return json.Marshal(fields)
}

func (a *AllowedFields) UnmarshalJSON(input []byte) error {
	a.AllowAll = false
	var str string
	if err := json.Unmarshal(input, &str); err == nil && str == "*" {
		a.AllowAll = true
		return nil
	}
	var fields []string
	if err := json.Unmarshal(input, &fields); err == nil {
		if a.AllowedSubfields == nil {
			a.AllowedSubfields = map[string]AllowedFields{}
		}
		for _, field := range fields {
			a.AllowedSubfields[field] = AllowedFields{AllowAll: true}
		}
		return nil
	}
	return json.Unmarshal(input, &a.AllowedSubfields)
}

func (o OperationPermissions) MarshalJSON() ([]byte, error) {
	m := make(map[string]AllowedFields)
	if o.AllowedRootQueryFields.AllowAll || o.AllowedRootQueryFields.AllowedSubfields != nil {
		m["query"] = o.AllowedRootQueryFields
	}
	if o.AllowedRootMutationFields.AllowAll || o.AllowedRootMutationFields.AllowedSubfields != nil {
		m["mutation"] = o.AllowedRootMutationFields
	}
	if o.AllowedRootSubscriptionFields.AllowAll || o.AllowedRootSubscriptionFields.AllowedSubfields != nil {
		m["subscription"] = o.AllowedRootSubscriptionFields
	}
	return json.Marshal(m)
}

// FilterAuthorizedFields filters the operation's selection set and removes all
// fields that are not explicitly authorized.
// Every unauthorized field is returned as an error.
func (o *OperationPermissions) FilterAuthorizedFields(op *ast.OperationDefinition) gqlerror.List {
	var res ast.SelectionSet
	var errs gqlerror.List

	switch op.Operation {
	case ast.Query:
		res, errs = filterFields([]string{"query"}, op.SelectionSet, o.AllowedRootQueryFields)
	case ast.Mutation:
		res, errs = filterFields([]string{"mutation"}, op.SelectionSet, o.AllowedRootMutationFields)
	case ast.Subscription:
		res, errs = filterFields([]string{"subscription"}, op.SelectionSet, o.AllowedRootSubscriptionFields)
	default:
		panic(fmt.Sprintf("invalid operation %q in operation filtering", op.Operation))
	}

	op.SelectionSet = res

	return errs
}

// FilterSchema returns a copy of the given schema stripped of any unauthorized
// fields and types
func (o *OperationPermissions) FilterSchema(schema *ast.Schema) *ast.Schema {
	newSchema := *schema
	newSchema.Types = make(map[string]*ast.Definition)

	newSchema.Query = filterDefinition(schema, nil, newSchema.Types, schema.Query, o.AllowedRootQueryFields)
	if newSchema.Query != nil {
		newSchema.Types["Query"] = newSchema.Query
	}
	newSchema.Mutation = filterDefinition(schema, nil, newSchema.Types, schema.Mutation, o.AllowedRootMutationFields)
	if newSchema.Mutation != nil {
		newSchema.Types["Mutation"] = newSchema.Mutation
	}
	newSchema.Subscription = filterDefinition(schema, nil, newSchema.Types, schema.Subscription, o.AllowedRootSubscriptionFields)
	if newSchema.Subscription != nil {
		newSchema.Types["Subscription"] = newSchema.Subscription
	}

	return &newSchema
}

func filterDefinition(sourceSchema *ast.Schema, visited map[string]bool, types map[string]*ast.Definition, def *ast.Definition, allowedFields AllowedFields) *ast.Definition {
	if def == nil {
		return nil
	}

	resDef := *def
	resDef.Fields = nil

	if allowedFields.AllowAll {
		if visited == nil {
			// visited keeps track of already visited subfields, so that we
			// don't enter into an infinite recursion
			visited = make(map[string]bool)
		}
		resDef.Fields = def.Fields

		// copy recursively all the subtypes for every field into the result schema
		for _, f := range def.Fields {
			typeName := f.Type.Name()
			if visited[def.Name+f.Name] {
				continue
			}
			visited[def.Name+f.Name] = true
			types[typeName] = sourceSchema.Types[typeName]
			for _, a := range f.Arguments {
				types[a.Type.Name()] = sourceSchema.Types[a.Type.Name()]
				_ = filterDefinition(sourceSchema, visited, types, sourceSchema.Types[a.Type.Name()], AllowedFields{AllowAll: true})
			}
			_ = filterDefinition(sourceSchema, visited, types, sourceSchema.Types[typeName], AllowedFields{AllowAll: true})
		}

		// unions
		for _, t := range def.Types {
			types[t] = sourceSchema.Types[t]
			_ = filterDefinition(sourceSchema, visited, types, sourceSchema.Types[t], AllowedFields{AllowAll: true})
		}

		return &resDef
	}

	for _, f := range def.Fields {
		if allowedSubFields, ok := allowedFields.AllowedSubfields[f.Name]; ok {
			resDef.Fields = append(resDef.Fields, f)
			typename := f.Type.Name()
			newTypeDef := filterDefinition(sourceSchema, visited, types, sourceSchema.Types[typename], allowedSubFields)
			if typeDef, ok := types[typename]; ok {
				// a type could be accessed through multiple paths, so we need
				// to merge the fields
				addFields(typeDef, newTypeDef)
			} else {
				types[typename] = newTypeDef
			}

			// add input types
			for _, a := range f.Arguments {
				types[a.Type.Name()] = sourceSchema.Types[a.Type.Name()]
				_ = filterDefinition(sourceSchema, visited, types, sourceSchema.Types[a.Type.Name()], AllowedFields{AllowAll: true})
			}
		}
	}

	return &resDef
}

func addFields(a, b *ast.Definition) {
	for _, f := range b.Fields {
		if a.Fields.ForName(f.Name) == nil {
			a.Fields = append(a.Fields, f)
		}
	}
}

// filterFields filters allowed fields and returns a new selection set
func filterFields(path []string, ss ast.SelectionSet, allowedFields AllowedFields) (ast.SelectionSet, gqlerror.List) {
	res := make(ast.SelectionSet, 0, len(ss))
	var errs gqlerror.List

	if allowedFields.AllowAll {
		return ss, nil
	}

	for _, f := range selectionSetToFields(ss) {
		if allowed, fieldsPerms := allowedFields.IsAllowed(f.Name); allowed {
			if fieldsPerms.AllowAll {
				res = append(res, f)
				continue
			}

			var ferrs gqlerror.List
			fieldPath := append(path, f.Name)
			f.SelectionSet, ferrs = filterFields(fieldPath, f.SelectionSet, fieldsPerms)
			res = append(res, f)
			errs = append(errs, ferrs...)
		} else {
			errs = append(errs, gqlerror.Errorf("user do not have permission to access field %s.%s", strings.Join(path, "."), f.Name))
		}
	}

	return res, errs
}
