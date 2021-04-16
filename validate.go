package bramble

import (
	"fmt"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// ValidateSchema validates that the schema respects the Bramble specs
func ValidateSchema(schema *ast.Schema) error {
	if err := validateRootObjectNames(schema); err != nil {
		return err
	}
	if err := validateBoundaryObjects(schema); err != nil {
		return err
	}
	if err := validateNamespaceObjects(schema); err != nil {
		return err
	}
	if err := validateServiceQuery(schema); err != nil {
		return err
	}
	if err := validateServiceObject(schema); err != nil {
		return err
	}
	if err := validateSchemaValidAfterMerge(schema); err != nil {
		return err
	}
	return nil
}

func validateBoundaryObjects(schema *ast.Schema) error {
	if !usesBoundaryDirective(schema) {
		return nil
	}

	if err := validateBoundaryDirective(schema); err != nil {
		return err
	}

	if err := validateBoundaryObjectsFormat(schema); err != nil {
		return err
	}

	if usesFieldsBoundaryDirective(schema) {
		if err := validateBoundaryQueries(schema); err != nil {
			return err
		}

		// node compatibility
		if !hasNodeQuery(schema) {
			if err := validateBoundaryFields(schema); err != nil {
				return err
			}
		}
	} else {
		if err := validateNodeInterface(schema); err != nil {
			return err
		}
		if err := validateImplementsNode(schema); err != nil {
			return err
		}
	}

	if hasNodeQuery(schema) {
		if err := validateNodeQuery(schema); err != nil {
			return err
		}
	}

	return nil
}

func validateNamespaceObjects(schema *ast.Schema) error {
	if usesNamespaceDirective(schema) {
		if err := validateNamespaceDirective(schema); err != nil {
			return err
		}
		if err := validateNamespaceTypesAscendence(schema); err != nil {
			return err
		}
		if err := validateNamespacesFields(schema, schema.Query, "Query"); err != nil {
			return err
		}
		if err := validateNamespacesFields(schema, schema.Mutation, "Mutation"); err != nil {
			return err
		}
		if err := validateNamespacesFields(schema, schema.Subscription, "Subscription"); err != nil {
			return err
		}
	}
	return nil
}

func validateServiceObject(schema *ast.Schema) error {
	for _, t := range schema.Types {
		if t.Name != serviceObjectName {
			continue
		}
		if t.Kind != ast.Object {
			return fmt.Errorf("the Service type must be an object")
		}
		if len(t.Fields) != 3 {
			return fmt.Errorf("the Service object should have exactly 3 fields")
		}
		for _, field := range t.Fields {
			switch field.Name {
			case "name", "version", "schema":
				if !isNonNullableTypeNamed(field.Type, "String") {
					return fmt.Errorf("the Service object should have a field called '%s' of type 'String!'", field.Name)
				}
			default:
				return fmt.Errorf("the Service object should not have a field called %s", field.Name)
			}
		}
		return nil
	}
	return fmt.Errorf("the Service object was not found")
}

func validateServiceQuery(schema *ast.Schema) error {
	if schema.Query == nil {
		return fmt.Errorf("the schema is missing a Query type")
	}
	for _, f := range schema.Query.Fields {
		if f.Name != serviceRootFieldName {
			continue
		}
		if len(f.Arguments) != 0 {
			return fmt.Errorf("the 'service' field of Query must take no arguments")
		}
		if !isNonNullableTypeNamed(f.Type, serviceObjectName) {
			return fmt.Errorf("the 'service' field of Query must be of type 'Service!'")
		}
		return nil
	}
	return fmt.Errorf("the Query type is missing the 'service' field")
}

func validateNodeQuery(schema *ast.Schema) error {
	if schema.Query == nil {
		return fmt.Errorf("the schema is missing a Query type")
	}
	for _, f := range schema.Query.Fields {
		if f.Name != nodeRootFieldName {
			continue
		}
		if len(f.Arguments) != 1 {
			return fmt.Errorf("the 'node' field of Query must take a single argument")
		}
		arg := f.Arguments[0]
		if arg.Name != idFieldName {
			return fmt.Errorf("the 'node' field of Query must take a single argument called 'id'")
		}
		if !isIDType(arg.Type) {
			return fmt.Errorf("the 'node' field of Query must take a single argument of type 'ID!'")
		}
		if !isNullableTypeNamed(f.Type, nodeInterfaceName) {
			return fmt.Errorf("the 'node' field of Query must be of type 'Node'")
		}
		return nil
	}
	return fmt.Errorf("the Query type is missing the 'node' field")
}

func validateNodeInterface(schema *ast.Schema) error {
	for _, t := range schema.Types {
		if t.Name != nodeInterfaceName {
			continue
		}
		if t.Kind != ast.Interface {
			return fmt.Errorf("the Node type must be an interface")
		}
		if len(t.Fields) != 1 {
			return fmt.Errorf("the Node interface should have exactly one field")
		}
		field := t.Fields[0]
		if field.Name != idFieldName {
			return fmt.Errorf("the Node interface should have a field called 'id'")
		}
		if !isIDType(field.Type) {
			return fmt.Errorf("the Node interface should have a field called 'id' of type 'ID!'")
		}
		return nil
	}
	return fmt.Errorf("the Node interface was not found")
}

func validateImplementsNode(schema *ast.Schema) error {
	for _, t := range schema.Types {
		if t.Kind != ast.Object {
			continue
		}
		if t.Directives.ForName(boundaryDirectiveName) == nil {
			continue
		}
		if implementsNode(schema, t) {
			continue
		}
		return fmt.Errorf("object '%s' has the boundary directive but doesn't implement Node", t.Name)
	}
	return nil
}

func implementsNode(schema *ast.Schema, def *ast.Definition) bool {
	for _, i := range schema.GetImplements(def) {
		if i.Name == nodeInterfaceName {
			return true
		}
	}
	return false
}

func hasNodeQuery(schema *ast.Schema) bool {
	return schema.Query.Fields.ForName(nodeRootFieldName) != nil
}

func usesNamespaceDirective(schema *ast.Schema) bool {
	for _, t := range schema.Types {
		if t.Kind != ast.Object {
			continue
		}
		if t.Directives.ForName(namespaceDirectiveName) != nil {
			return true
		}
	}
	return false
}

func validateNamespaceDirective(schema *ast.Schema) error {
	for _, d := range schema.Directives {
		if d.Name != namespaceDirectiveName {
			continue
		}
		if len(d.Arguments) != 0 {
			return fmt.Errorf("@namespace directive may not take arguments")
		}
		if len(d.Locations) != 1 {
			return fmt.Errorf("@namespace directive should have 1 location")
		}
		if d.Locations[0] != ast.LocationObject {
			return fmt.Errorf("@namespace directive should have location OBJECT")
		}
		return nil
	}
	return fmt.Errorf("@namespace directive not found")
}

func validateNamespacesFields(schema *ast.Schema, currentType *ast.Definition, rootType string) error {
	if currentType == nil {
		return nil
	}

	for _, f := range currentType.Fields {
		ft := schema.Types[f.Type.Name()]
		if isNamespaceObject(ft) {
			if !f.Type.NonNull {
				return fmt.Errorf("namespace return type should be non nullable on %s.%s", currentType.Name, f.Name)
			}

			err := validateNamespacesFields(schema, ft, rootType)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// validateNamespaceTypesAscendence validates that namespace types are only used in other namespaces type or Query/Mutation/Subscription
func validateNamespaceTypesAscendence(schema *ast.Schema) error {
	for _, t := range schema.Types {
		if isNamespaceObject(t) || t.Name == queryObjectName || t.Name == mutationObjectName || t.Name == subscriptionObjectName {
			continue
		}

		for _, f := range t.Fields {
			ft := schema.Types[f.Type.Name()]
			if isNamespaceObject(ft) {
				return fmt.Errorf("type %q (namespace type) is used for field %q in non-namespace object %q", ft.Name, f.Name, t.Name)
			}
		}
	}

	return nil
}

func usesBoundaryDirective(schema *ast.Schema) bool {
	for _, t := range schema.Types {
		if t.Kind != ast.Object {
			continue
		}
		if t.Directives.ForName(boundaryDirectiveName) != nil {
			return true
		}
	}
	return false
}

func validateBoundaryDirective(schema *ast.Schema) error {
	for _, d := range schema.Directives {
		if d.Name != boundaryDirectiveName {
			continue
		}
		if len(d.Arguments) != 0 {
			return fmt.Errorf("@boundary directive may not take arguments")
		}
		if len(d.Locations) == 1 {
			// compatibility with existing @boundary directives
			if d.Locations[0] != ast.LocationObject {
				return fmt.Errorf("@boundary directive should have location OBJECT")
			}
		} else if len(d.Locations) == 2 {
			if (d.Locations[0] != ast.LocationObject && d.Locations[0] != ast.LocationFieldDefinition) ||
				(d.Locations[1] != ast.LocationObject && d.Locations[1] != ast.LocationFieldDefinition) ||
				(d.Locations[0] == d.Locations[1]) {
				return fmt.Errorf("@boundary directive should have locations OBJECT | FIELD_DEFINITION")
			}
		} else {
			return fmt.Errorf("@boundary directive should have locations OBJECT | FIELD_DEFINITION")
		}
		return nil
	}
	return fmt.Errorf("@boundary directive not found")
}

func usesFieldsBoundaryDirective(schema *ast.Schema) bool {
	d, ok := schema.Directives[boundaryDirectiveName]
	if !ok {
		return false
	}
	return len(d.Locations) == 2
}

// validateBoundaryFields checks that all boundary types have a getter and all getters are matching with a boundary type
func validateBoundaryFields(schema *ast.Schema) error {
	boundaryTypes := make(map[string]struct{})
	for _, t := range schema.Types {
		if t.Kind == ast.Object && isBoundaryObject(t) {
			boundaryTypes[t.Name] = struct{}{}
		}
	}

	for _, f := range schema.Query.Fields {
		if hasBoundaryDirective(f) {
			if _, ok := boundaryTypes[f.Type.Name()]; !ok {
				return fmt.Errorf("declared boundary query for non-boundary type %q", f.Type.Name())
			}

			delete(boundaryTypes, f.Type.Name())
		}
	}

	if len(boundaryTypes) > 0 {
		var missingBoundaryQueries []string
		for k := range boundaryTypes {
			missingBoundaryQueries = append(missingBoundaryQueries, k)
		}

		return fmt.Errorf("missing boundary queries for the following types: %v", missingBoundaryQueries)
	}

	return nil
}

func validateBoundaryObjectsFormat(schema *ast.Schema) error {
	for _, t := range schema.Types {
		if t.Directives.ForName(boundaryDirectiveName) == nil {
			continue
		}

		idField := t.Fields.ForName(idFieldName)
		if idField == nil {
			return fmt.Errorf(`missing "id: ID!" field in boundary type %q`, t.Name)
		}

		if idField.Type.String() != "ID!" {
			return fmt.Errorf(`id field should have type "ID!" in boundary type %q`, t.Name)
		}
	}

	return nil
}

func validateBoundaryQueries(schema *ast.Schema) error {
	for _, f := range schema.Query.Fields {
		if hasBoundaryDirective(f) {
			if err := validateBoundaryQuery(f); err != nil {
				return fmt.Errorf("invalid boundary query %q: %w", f.Name, err)
			}
		}
	}

	return nil
}

func validateBoundaryQuery(f *ast.FieldDefinition) error {
	if len(f.Arguments) != 1 {
		return fmt.Errorf(`boundary query must have a single "id: ID!" argument`)
	}

	if f.Arguments[0].Type.Elem != nil {
		// array type check
		if idsField := f.Arguments.ForName("ids"); idsField == nil || idsField.Type.String() != "[ID!]" {
			return fmt.Errorf(`boundary query must have a single "id: ID!" argument`)
		}

		if !f.Type.NonNull || f.Type.Elem == nil {
			return fmt.Errorf("return type should be a non-null array of nullable elements")
		}

		return nil
	}

	// regular type check
	if idField := f.Arguments.ForName(idFieldName); idField == nil || idField.Type.String() != "ID!" {
		return fmt.Errorf(`boundary query must have a single "id: ID!" argument`)
	}

	if f.Type.NonNull {
		return fmt.Errorf("return type of boundary query should be nullable")
	}

	return nil
}

func validateRootObjectNames(schema *ast.Schema) error {
	if q := schema.Query; q != nil && q.Name != queryObjectName {
		return fmt.Errorf("the schema Query type can not be renamed to %s", q.Name)
	}
	if m := schema.Mutation; m != nil && m.Name != mutationObjectName {
		return fmt.Errorf("the schema Mutation type can not be renamed to %s", m.Name)
	}
	if s := schema.Subscription; s != nil && s.Name != subscriptionObjectName {
		return fmt.Errorf("the schema Subscription type can not be renamed to %s", s.Name)
	}
	return nil
}

// validateSchemaValidAfterMerge validates that the schema is still going to be
// valid once it gets merged with another schema and special types are removed.
// For example the Service type should not be used outside of the Query type.
func validateSchemaValidAfterMerge(schema *ast.Schema) error {
	mergedSchema, err := MergeSchemas(schema)
	if err != nil {
		return fmt.Errorf("merge schema error: %w", err)
	}

	// If resulting Query type is empty, remove it from schema to avoid
	// generating an invalid schema when formatting (empty Query type: `type Query {}`)
	if len(filterBuiltinFields(mergedSchema.Query.Fields)) == 0 {
		delete(mergedSchema.Types, "Query")
	}

	// format and reload the schema to ensure it is valid
	res := formatSchema(mergedSchema)
	_, gqlErr := gqlparser.LoadSchema(&ast.Source{Name: "merged schema", Input: res})
	if gqlErr != nil {
		return fmt.Errorf("schema will become invalid after merge operation: %w", gqlErr)
	}

	return nil
}
