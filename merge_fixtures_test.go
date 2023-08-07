package bramble

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

type MergeTestFixture struct {
	Input1   string
	Input2   string
	Expected string
	Error    string
}

type BuildFieldURLMapFixture struct {
	Schema1   string
	Location1 string
	Schema2   string
	Location2 string
	Expected  FieldURLMap
}

func (f MergeTestFixture) CheckSuccess(t *testing.T) {
	t.Helper()
	var schemas []*ast.Schema
	if f.Input1 != "" {
		schemas = append(schemas, loadSchema(f.Input1))
	}
	if f.Input2 != "" {
		schemas = append(schemas, loadSchema(f.Input2))
	}
	actual := mustMergeSchemas(t, schemas...)

	// If resulting Query type is empty, remove it from schema to avoid
	// generating an invalid schema when formatting (empty Query type: `type Query {}`)
	if actual.Query != nil && len(filterBuiltinFields(actual.Query.Fields)) == 0 {
		delete(actual.Types, "Query")
		delete(actual.PossibleTypes, "Query")
	}

	assertSchemaConsistency(t, actual)
	assert.Equal(t, loadAndFormatSchema(f.Expected), formatSchema(actual))
}

func (f MergeTestFixture) CheckError(t *testing.T) {
	t.Helper()
	var schemas []*ast.Schema
	if f.Input1 != "" {
		schemas = append(schemas, loadSchema(f.Input1))
	}
	if f.Input2 != "" {
		schemas = append(schemas, loadSchema(f.Input2))
	}
	_, err := MergeSchemas(schemas...)
	assert.Error(t, err)
	assert.Equal(t, f.Error, err.Error())
}

func (f BuildFieldURLMapFixture) Check(t *testing.T) {
	t.Helper()
	var services []*Service
	if f.Schema1 != "" {
		services = append(
			services,
			&Service{ServiceURL: f.Location1, Schema: loadSchema(f.Schema1)},
		)
	}
	if f.Schema2 != "" {
		services = append(
			services,
			&Service{ServiceURL: f.Location2, Schema: loadSchema(f.Schema2)},
		)
	}
	locations := buildFieldURLMap(services...)
	assert.Equal(t, f.Expected, locations)
}

func loadSchema(input string) *ast.Schema {
	return gqlparser.MustLoadSchema(&ast.Source{Name: "schema", Input: input})
}

func loadAndFormatSchema(input string) string {
	return formatSchema(loadSchema(input))
}

func mustMergeSchemas(t *testing.T, sources ...*ast.Schema) *ast.Schema {
	t.Helper()
	s, err := MergeSchemas(sources...)
	require.NoError(t, err)
	return s
}

func assertSchemaConsistency(t *testing.T, schema *ast.Schema) {
	t.Helper()
	assertSchemaImplementsConsistency(t, schema)
	assertSchemaPossibleTypesConsistency(t, schema)
	assertSchemaIntrospectionTypes(t, schema)
	assertSchemaBuiltinDirectives(t, schema)
}

func assertSchemaImplementsConsistency(t *testing.T, schema *ast.Schema) {
	t.Helper()
	actual := getImplements(schema)
	expected := getImplements(loadSchema(formatSchema(schema)))
	assert.Equal(t, expected, actual, "schema.Implements is not consistent")
}

func assertSchemaPossibleTypesConsistency(t *testing.T, schema *ast.Schema) {
	t.Helper()
	actual := getPossibleTypes(schema)
	expected := getPossibleTypes(loadSchema(formatSchema(schema)))
	actualKeys, expectedKeys := []string{}, []string{}
	for key := range actual {
		actualKeys = append(actualKeys, key)
	}
	for key := range expected {
		expectedKeys = append(expectedKeys, key)
	}
	assert.ElementsMatch(t, expectedKeys, actualKeys, "schema.PossibleTypes is not consistent")
	for typeName := range actual {
		assert.ElementsMatchf(t, actual[typeName], expected[typeName], "schema.PossibleTypes[%s] is not consistent", typeName)
	}
}

func assertSchemaIntrospectionTypes(t *testing.T, schema *ast.Schema) {
	t.Helper()
	emptyAST := gqlparser.MustLoadSchema(&ast.Source{Name: "empty", Input: ""})
	fields := []string{"__Schema", "__Directive", "__DirectiveLocation", "__EnumValue", "__Field", "__Type", "__TypeKind"}
	for _, field := range fields {
		assert.Equal(t, ast.Dump(emptyAST.Types[field]), ast.Dump(schema.Types[field]), "introspection field '%s' is missing", field)
	}
}

func assertSchemaBuiltinDirectives(t *testing.T, schema *ast.Schema) {
	t.Helper()
	emptyAST := gqlparser.MustLoadSchema(&ast.Source{Name: "empty", Input: ""})
	builtInDirectives := []string{"skip", "include", "deprecated"}
	for _, d := range builtInDirectives {
		expected := emptyAST.Directives[d]
		actual := schema.Directives[d]
		assert.Equal(t, ast.Dump(expected), ast.Dump(actual))
	}
}

func getPossibleTypes(schema *ast.Schema) map[string][]string {
	result := map[string][]string{}
	for k, v := range schema.PossibleTypes {
		for _, def := range v {
			result[k] = append(result[k], def.Name)
		}
	}
	return result
}

func getImplements(schema *ast.Schema) map[string][]string {
	result := map[string][]string{}
	for k, v := range schema.Implements {
		for _, def := range v {
			result[k] = append(result[k], def.Name)
		}
	}
	return result
}
