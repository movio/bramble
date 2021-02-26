package bramble

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

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
	if err := validateNamingConventions(schema); err != nil {
		return err
	}
	if err := validateSchemaValidAfterMerge(schema); err != nil {
		return err
	}
	return nil
}

func validateBoundaryObjects(schema *ast.Schema) error {
	if usesBoundaryDirective(schema) {
		if err := validateBoundaryDirective(schema); err != nil {
			return err
		}
		if err := validateNodeInterface(schema); err != nil {
			return err
		}
		if err := validateImplementsNode(schema); err != nil {
			return err
		}
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
		if err := validateNamespacesNaming(schema, schema.Query, "Query"); err != nil {
			return err
		}
		if err := validateNamespacesNaming(schema, schema.Mutation, "Mutation"); err != nil {
			return err
		}
		if err := validateNamespacesNaming(schema, schema.Subscription, "Subscription"); err != nil {
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

func validateNamespacesNaming(schema *ast.Schema, currentType *ast.Definition, rootType string) error {
	if currentType == nil {
		return nil
	}

	for _, f := range currentType.Fields {
		ft := schema.Types[f.Type.Name()]
		if isNamespaceObject(ft) {
			if !f.Type.NonNull {
				return fmt.Errorf("namespace return type should be non nullable on %s.%s", currentType.Name, f.Name)
			}

			if !strings.HasSuffix(f.Type.Name(), rootType) {
				return fmt.Errorf("type %q is used as a %s namespace but doesn't have the %q suffix", f.Type.Name(), strings.ToLower(rootType), rootType)
			}

			err := validateNamespacesNaming(schema, ft, rootType)
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
		if len(d.Locations) != 1 {
			return fmt.Errorf("@boundary directive should have 1 location")
		}
		if d.Locations[0] != ast.LocationObject {
			return fmt.Errorf("@boundary directive should have location OBJECT")
		}
		return nil
	}
	return fmt.Errorf("@boundary directive not found")
}

func validateNamingConventions(schema *ast.Schema) error {
	for _, t := range schema.Types {
		if isGraphQLBuiltinName(t.Name) {
			continue
		}
		if t.Kind == ast.Object || t.Kind == ast.InputObject || t.Kind == ast.Interface {
			if !isPascalCase(t.Name) {
				return fmt.Errorf("type '%s' isn't PascalCase", t.Name)
			}

			for _, f := range t.Fields {
				if isGraphQLBuiltinName(f.Name) {
					continue
				}
				if !isCamelCase(f.Name) {
					return fmt.Errorf("field '%s.%s' isn't camelCase", t.Name, f.Name)
				}
				if t.Kind == ast.Object || t.Kind == ast.Interface {
					for _, a := range f.Arguments {
						if !isCamelCase(a.Name) {
							return fmt.Errorf("argument '%s' of field '%s.%s' isn't camelCase", a.Name, t.Name, f.Name)
						}
					}
				}
			}
		}
		if t.Kind == ast.Enum {
			if !isPascalCase(t.Name) {
				return fmt.Errorf("enum type '%s' isn't PascalCase", t.Name)
			}
			for _, v := range t.EnumValues {
				if !isAllCaps(v.Name) {
					return fmt.Errorf("enum value '%s.%s' isn't ALL_CAPS", t.Name, v.Name)
				}
			}
		}
		if t.Kind == ast.Union {
			if !isPascalCase(t.Name) {
				return fmt.Errorf("union type '%s' isn't PascalCase", t.Name)
			}
		}
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

	// format and reload the schema to ensure it is valid
	res := formatSchema(mergedSchema)
	_, gqlErr := gqlparser.LoadSchema(&ast.Source{Name: "merged schema", Input: res})
	if gqlErr != nil {
		return fmt.Errorf("schema will become invalid after merge operation: %w", gqlErr)
	}

	return nil
}

var camelCaseRegexp = regexp.MustCompile(`^[a-z][A-Za-z0-9]+$`)

func isCamelCase(s string) bool {
	return camelCaseRegexp.MatchString(s)
}

var allCapsRegexp = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

func isAllCaps(s string) bool {
	return allCapsRegexp.MatchString(s)
}

var pascalCaseRegexp = regexp.MustCompile(`^[A-Z][A-Za-z0-9]+$`)

func isPascalCase(s string) bool {
	return pascalCaseRegexp.MatchString(s)
}
