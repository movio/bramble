package bramble

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestFormatSelectionSetVerySimple(t *testing.T) {
	schema := loadSchema(`
			type Gizmo {
				name: String!
				weight: Float!
			}
			type Query {
				gizmo: Gizmo
			}`,
	)
	selectionSet := []ast.Selection{
		&ast.Field{
			Alias:            "gizmo",
			Name:             "gizmo",
			Definition:       schema.Query.Fields.ForName("gizmo"),
			ObjectDefinition: schema.Query,
			SelectionSet: []ast.Selection{
				&ast.Field{
					Alias:            "name",
					Name:             "name",
					Definition:       schema.Types["Gizmo"].Fields.ForName("name"),
					ObjectDefinition: schema.Types["Gizmo"],
				},
				&ast.Field{
					Alias:            "weight",
					Name:             "weight",
					Definition:       schema.Types["Gizmo"].Fields.ForName("weight"),
					ObjectDefinition: schema.Types["Gizmo"],
				},
			},
		},
	}
	assert.Equal(t, formatSelectionSetSingleLine(testContextWithoutVariables(nil), schema, selectionSet), `{ gizmo { name weight } }`)
}

func TestFormatSelectionSetWithTypename(t *testing.T) {
	schema := loadSchema(`
			type Gizmo {
				name: String!
				weight: Float!
			}
			type Query {
				gizmo: Gizmo
			}`,
	)
	selectionSet := []ast.Selection{
		&ast.Field{
			Alias:            "gizmo",
			Name:             "gizmo",
			Definition:       schema.Query.Fields.ForName("gizmo"),
			ObjectDefinition: schema.Query,
			SelectionSet: []ast.Selection{
				&ast.Field{
					Alias:            "name",
					Name:             "name",
					Definition:       schema.Types["Gizmo"].Fields.ForName("name"),
					ObjectDefinition: schema.Types["Gizmo"],
				},
				&ast.Field{
					Alias:            "weight",
					Name:             "weight",
					Definition:       schema.Types["Gizmo"].Fields.ForName("weight"),
					ObjectDefinition: schema.Types["Gizmo"],
				},
				&ast.Field{
					Alias:      "__typename",
					Name:       "__typename",
					Definition: &ast.FieldDefinition{Name: "__typename", Type: ast.NamedType("String", nil)},
				},
			},
		},
	}
	assert.Equal(t, formatSelectionSetSingleLine(testContextWithoutVariables(nil), schema, selectionSet), `{ gizmo { name weight __typename } }`)
}

func TestFormatSelectionSetWithObjectVariable(t *testing.T) {
	schema := loadSchema(`
	enum Genre {
		ACTION
		COMEDY
	}

	type Movie {
		genre: Genre
	}

	input SubObject {
		genre: Genre!
	}

	input SearchInput {
		genre: Genre!
		genreList: [Genre!]
		stringList: [String!]
		intList: [Int!]
		subObject: SubObject!
	}
	type Query {
		search(input: SearchInput!): [Movie!]
	}
	`)

	query := gqlparser.MustLoadQuery(schema, `query ($input: SearchInput!) {
		search(input: $input) { genre }
	}`)

	res := formatSelectionSetSingleLine(testContextWithVariables(map[string]interface{}{"input": map[string]interface{}{
		"genre":      "ACTION",
		"genreList":  []string{"ACTION", "COMEDY"},
		"stringList": []string{"abc", "123"},
		"intList":    []int{123},
		"subObject": map[string]interface{}{
			"genre": "ACTION",
		},
	}}, nil), schema, query.Operations[0].SelectionSet)
	assert.Equal(t, `{ search(input: {genre: ACTION genreList: [ACTION, COMEDY] stringList: ["abc", "123"] intList: [123] subObject: {genre: ACTION}}) { genre } }`, res)
}

func TestFormatSelectionSetWithListOfObjectVariable(t *testing.T) {
	schema := loadSchema(`
	input Value {
		name: String!
		value: String!
	}

	type Query {
		search(input: [Value!]!): String
	}
	`)

	query := gqlparser.MustLoadQuery(schema, `query ($input: [Value!]!) {
		search(input: $input)
	}`)

	res := formatSelectionSetSingleLine(testContextWithVariables(map[string]interface{}{"input": []interface{}{
		map[string]interface{}{"name": "name", "value": "value"},
	}}, nil), schema, query.Operations[0].SelectionSet)
	assert.Equal(t, `{ search(input: [{name: "name" value: "value"}]) }`, res)
}

func TestFormatSelectionSetWithListContainingVariable(t *testing.T) {
	schema := loadSchema(`
	type Movie {
		id: ID!
	}

	type Query {
		moviesByIds(ids: [Int!]!): [Movie!]
	}
	`)

	query := gqlparser.MustLoadQuery(schema, `query ($id: Int!) {
		moviesByIds(ids: [$id]) { id }
	}`)

	res := formatSelectionSetSingleLine(testContextWithVariables(map[string]interface{}{"id": 1234}, nil), schema, query.Operations[0].SelectionSet)
	assert.Equal(t, `{ moviesByIds(ids: [1234]) { id } }`, res)
}

func TestFormatSelectionSetWithEnum(t *testing.T) {
	schema := loadSchema(`
	enum Genre {
		ACTION
		COMEDY
	}

	type Movie {
		genre: Genre
	}

	input SearchInput {
		genre: Genre!
	}
	type Query {
		search(input: SearchInput!): [Movie!]
	}
	`)

	query := gqlparser.MustLoadQuery(schema, `query {
		search(input: { genre: ACTION }) { genre }
	}`)

	res := formatSelectionSetSingleLine(testContextWithoutVariables(nil), schema, query.Operations[0].SelectionSet)
	assert.Equal(t, `{ search(input: {genre:ACTION}) { genre } }`, res)
}

func TestFormatSelectionSetWithEnumVariable(t *testing.T) {
	schema := loadSchema(`
	enum Genre {
		ACTION
		COMEDY
	}

	type Movie {
		genre: Genre
	}

	input SearchInput {
		genre: Genre!
	}
	type Query {
		search(input: SearchInput!): [Movie!]
	}
	`)

	query := gqlparser.MustLoadQuery(schema, `query($genre: Genre!) {
		search(input: { genre: $genre}) { genre }
	}`)

	res := formatSelectionSetSingleLine(testContextWithVariables(map[string]interface{}{"genre": "ACTION"}, nil), schema, query.Operations[0].SelectionSet)
	assert.Equal(t, `{ search(input: {genre:ACTION}) { genre } }`, res)
}

func TestFormatSelectionSetWithNullEnumVariable(t *testing.T) {
	schema := loadSchema(`
	enum Genre {
		ACTION
		COMEDY
	}

	type Movie {
		genre: Genre
	}

	input SearchInput {
		genre: Genre
	}
	type Query {
		search(input: SearchInput!): [Movie!]
	}
	`)

	query := gqlparser.MustLoadQuery(schema, `query($genre: Genre!) {
		search(input: { genre: $genre}) { genre }
	}`)

	res := formatSelectionSetSingleLine(testContextWithVariables(map[string]interface{}{"genre": nil}, nil), schema, query.Operations[0].SelectionSet)
	assert.Equal(t, `{ search(input: {genre:null}) { genre } }`, res)
}

func TestFormatSelectionSetInlineFragment(t *testing.T) {
	schema := loadSchema(`
			interface Named {
				name: String!
			}
			type Gizmo implements Named {
				name: String!
				weight: Float!
			}
			type Query {
				read: [Named]
			}`,
	)
	selectionSet := []ast.Selection{
		&ast.Field{
			Alias:            "read",
			Name:             "read",
			Definition:       schema.Query.Fields.ForName("read"),
			ObjectDefinition: schema.Query,
			SelectionSet: []ast.Selection{
				&ast.InlineFragment{
					TypeCondition:    "Gizmo",
					ObjectDefinition: schema.Types["Gizmo"],
					SelectionSet: []ast.Selection{
						&ast.Field{
							Alias:            "name",
							Name:             "name",
							Definition:       schema.Types["Gizmo"].Fields.ForName("name"),
							ObjectDefinition: schema.Types["Gizmo"],
						},
						&ast.Field{
							Alias:            "weight",
							Name:             "weight",
							Definition:       schema.Types["Gizmo"].Fields.ForName("weight"),
							ObjectDefinition: schema.Types["Gizmo"],
						},
					},
				},
			},
		},
	}
	assert.Equal(t, formatSelectionSetSingleLine(testContextWithoutVariables(nil), schema, selectionSet), `{ read { ... on Gizmo { name weight } } }`)
}

func TestFormatSelectionSetInlineFragmentAndDirective(t *testing.T) {
	schema := loadSchema(`
			interface Named {
				name: String!
			}
			type Gizmo implements Named {
				name: String!
				weight: Float!
			}
			type Query {
				read: [Named]
			}`,
	)
	selectionSet := []ast.Selection{
		&ast.Field{
			Alias:            "read",
			Name:             "read",
			Definition:       schema.Query.Fields.ForName("read"),
			ObjectDefinition: schema.Query,
			Directives: ast.DirectiveList{
				&ast.Directive{
					Name: "skip",
					Arguments: ast.ArgumentList{
						&ast.Argument{
							Name: "if",
							Value: &ast.Value{
								Raw:          "false",
								Kind:         ast.BooleanValue,
								ExpectedType: &ast.Type{NamedType: "Boolean"},
							},
						},
					},
				},
			},
			SelectionSet: []ast.Selection{
				&ast.InlineFragment{
					TypeCondition:    "Gizmo",
					ObjectDefinition: schema.Types["Gizmo"],
					SelectionSet: []ast.Selection{
						&ast.Field{
							Alias:            "name",
							Name:             "name",
							Definition:       schema.Types["Gizmo"].Fields.ForName("name"),
							ObjectDefinition: schema.Types["Gizmo"],
						},
						&ast.Field{
							Alias:            "weight",
							Name:             "weight",
							Definition:       schema.Types["Gizmo"].Fields.ForName("weight"),
							ObjectDefinition: schema.Types["Gizmo"],
						},
					},
				},
			},
		},
	}
	assert.Equal(t, formatSelectionSetSingleLine(testContextWithoutVariables(nil), schema, selectionSet), `{ read @skip(if: false) { ... on Gizmo { name weight } } }`)
}

func TestFormatEnum(t *testing.T) {
	schema := loadSchema(`
		enum Language {
			French
			English
			Italian
		}`,
	)

	typ := &ast.Type{
		NamedType: "Language",
		NonNull:   false,
	}
	vars := map[string]interface{}{
		"f": "French",
		"e": "English",
	}

	assert.Equal(t, "French", formatArgument(schema, &ast.Value{Kind: ast.Variable, Raw: "f", ExpectedType: typ}, vars))
	assert.Equal(t, "English", formatArgument(schema, &ast.Value{Kind: ast.Variable, Raw: "e", ExpectedType: typ}, vars))
}
