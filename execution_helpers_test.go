package bramble

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestBuildBoundaryQueryDocuments(t *testing.T) {
	ddl := `
		type Gizmo {
			id: ID!
			color: String!
			owner: Owner
		}

		type Owner {
			id: ID!
			name: String!
		}

		type Query {
			gizmos: [Gizmo!]!
			getOwners(ids: [ID!]!): [Owner!]!
		}
	`
	schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})
	boundaryField := BoundaryField{Field: "getOwners", Argument: "ids", Array: true}
	ids := []string{"1", "2", "3"}
	selectionSet := []ast.Selection{
		&ast.Field{
			Alias:            "_bramble_id",
			Name:             "id",
			Definition:       schema.Types["Owner"].Fields.ForName("id"),
			ObjectDefinition: schema.Types["Owner"],
		},
		&ast.Field{
			Alias:            "name",
			Name:             "name",
			Definition:       schema.Types["Owner"].Fields.ForName("name"),
			ObjectDefinition: schema.Types["Owner"],
		},
	}
	step := &QueryPlanStep{
		ServiceURL:     "http://example.com:8080",
		ServiceName:    "test",
		ParentType:     "Gizmo",
		SelectionSet:   selectionSet,
		InsertionPoint: []string{"gizmos", "owner"},
		Then:           nil,
	}
	expected := []string{`query operationName { _result: getOwners(ids: ["1", "2", "3"]) { _bramble_id: id name } }`}
	ctx := testContextWithoutVariables(&ast.OperationDefinition{Name: "operationName"})
	docs, vars, err := buildBoundaryQueryDocuments(ctx, schema, step, ids, boundaryField, 1)
	require.NoError(t, err)
	require.Equal(t, expected, docs)
	require.Equal(t, (map[string]interface{})(nil), vars)
}

func TestBuildBoundaryQueryDocumentsWithVariables(t *testing.T) {
	ddl := `
		type Gizmo {
			id: ID!
			color: String!
			owner: Owner
		}

		type Owner {
			id: ID!
			name(format: String): String!
		}

		type Query {
			gizmos: [Gizmo!]!
			getOwners(ids: [ID!]!): [Owner!]!
		}
	`
	schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})
	boundaryField := BoundaryField{Field: "getOwners", Argument: "ids", Array: true}
	ids := []string{"1", "2", "3"}
	query := gqlparser.MustLoadQuery(schema, `query ($format: String) {
		getOwners(ids: []) { _bramble_id: id name(format: $format) }
	}`)

	step := &QueryPlanStep{
		ServiceURL:     "http://example.com:8080",
		ServiceName:    "test",
		ParentType:     "Gizmo",
		SelectionSet:   query.Operations[0].SelectionSet[0].(*ast.Field).SelectionSet,
		InsertionPoint: []string{"gizmos", "owner"},
		Then:           nil,
	}
	expected := []string{`query ($format: String) { _result: getOwners(ids: ["1", "2", "3"]) { _bramble_id: id name(format: $format) } }`}
	ctx := testContextWithVariables(map[string]interface{}{"format": "upper"}, query.Operations[0])
	docs, vars, err := buildBoundaryQueryDocuments(ctx, schema, step, ids, boundaryField, 1)
	require.NoError(t, err)
	require.Equal(t, expected, docs)
	require.Equal(t, map[string]interface{}{"format": "upper"}, vars)
}

func TestBuildNonArrayBoundaryQueryDocuments(t *testing.T) {
	ddl := `
		type Gizmo {
			id: ID!
			color: String!
			owner: Owner
		}

		type Owner {
			id: ID!
			name: String!
		}

		type Query {
			gizmos: [Gizmo!]!
			getOwner(id: ID!): Owner!
		}
	`
	schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})
	boundaryField := BoundaryField{Field: "getOwner", Argument: "id", Array: false}
	ids := []string{"1", "2", "3"}
	selectionSet := []ast.Selection{
		&ast.Field{
			Alias:            "_bramble_id",
			Name:             "id",
			Definition:       schema.Types["Owner"].Fields.ForName("id"),
			ObjectDefinition: schema.Types["Owner"],
		},
		&ast.Field{
			Alias:            "name",
			Name:             "name",
			Definition:       schema.Types["Owner"].Fields.ForName("name"),
			ObjectDefinition: schema.Types["Owner"],
		},
	}
	step := &QueryPlanStep{
		ServiceURL:     "http://example.com:8080",
		ServiceName:    "test",
		ParentType:     "Gizmo",
		SelectionSet:   selectionSet,
		InsertionPoint: []string{"gizmos", "owner"},
		Then:           nil,
	}
	expected := []string{`query name { _0: getOwner(id: "1") { _bramble_id: id name } _1: getOwner(id: "2") { _bramble_id: id name } _2: getOwner(id: "3") { _bramble_id: id name } }`}
	ctx := testContextWithoutVariables(&ast.OperationDefinition{Name: "name"})
	docs, vars, err := buildBoundaryQueryDocuments(ctx, schema, step, ids, boundaryField, 10)
	require.NoError(t, err)
	require.Equal(t, expected, docs)
	require.Equal(t, (map[string]interface{})(nil), vars)
}

func TestBuildNonArrayBoundaryQueryDocumentsWithVariables(t *testing.T) {
	ddl := `
		type Gizmo {
			id: ID!
			color: String!
			owner: Owner
		}

		type Owner {
			id: ID!
			name(format: String): String!
		}

		type Query {
			gizmos: [Gizmo!]!
			getOwner(id: ID!): Owner!
		}
	`
	schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})
	boundaryField := BoundaryField{Field: "getOwner", Argument: "id", Array: false}
	ids := []string{"1", "2", "3"}
	query := gqlparser.MustLoadQuery(schema, `query ($format: String) {
		getOwner(id: "") { _bramble_id: id name(format: $format) }
	}`)

	step := &QueryPlanStep{
		ServiceURL:     "http://example.com:8080",
		ServiceName:    "test",
		ParentType:     "Gizmo",
		SelectionSet:   query.Operations[0].SelectionSet[0].(*ast.Field).SelectionSet,
		InsertionPoint: []string{"gizmos", "owner"},
		Then:           nil,
	}

	expected := []string{`query ($format: String) { _0: getOwner(id: "1") { _bramble_id: id name(format: $format) } _1: getOwner(id: "2") { _bramble_id: id name(format: $format) } _2: getOwner(id: "3") { _bramble_id: id name(format: $format) } }`}
	ctx := testContextWithVariables(map[string]interface{}{"format": "lower"}, query.Operations[0])
	docs, vars, err := buildBoundaryQueryDocuments(ctx, schema, step, ids, boundaryField, 10)
	require.NoError(t, err)
	require.Equal(t, expected, docs)
	require.Equal(t, map[string]interface{}{"format": "lower"}, vars)
}

func TestBuildBatchedNonArrayBoundaryQueryDocuments(t *testing.T) {
	ddl := `
		type Gizmo {
			id: ID!
			color: String!
			owner: Owner
		}

		type Owner {
			id: ID!
			name: String!
		}

		type Query {
			gizmos: [Gizmo!]!
			getOwner(id: ID!): Owner!
		}
	`
	schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})
	boundaryField := BoundaryField{Field: "getOwner", Argument: "id", Array: false}
	ids := []string{"1", "2", "3"}
	selectionSet := []ast.Selection{
		&ast.Field{
			Alias:            "_bramble_id",
			Name:             "id",
			Definition:       schema.Types["Owner"].Fields.ForName("id"),
			ObjectDefinition: schema.Types["Owner"],
		},
		&ast.Field{
			Alias:            "name",
			Name:             "name",
			Definition:       schema.Types["Owner"].Fields.ForName("name"),
			ObjectDefinition: schema.Types["Owner"],
		},
	}
	step := &QueryPlanStep{
		ServiceURL:     "http://example.com:8080",
		ServiceName:    "test",
		ParentType:     "Gizmo",
		SelectionSet:   selectionSet,
		InsertionPoint: []string{"gizmos", "owner"},
		Then:           nil,
	}
	expected := []string{`query op { _0: getOwner(id: "1") { _bramble_id: id name } _1: getOwner(id: "2") { _bramble_id: id name } }`, `query op { _2: getOwner(id: "3") { _bramble_id: id name } }`}
	ctx := testContextWithoutVariables(&ast.OperationDefinition{Name: "op"})
	docs, vars, err := buildBoundaryQueryDocuments(ctx, schema, step, ids, boundaryField, 2)
	require.NoError(t, err)
	require.Equal(t, expected, docs)
	require.Equal(t, (map[string]interface{})(nil), vars)
}

func TestUnionAndTrimSelectionSet(t *testing.T) {
	schemaString := `
		directive @boundary on OBJECT
		interface Tool {
			id: ID!
			name: String!
		}

		union GadgetOrGizmo = Gadget | Gizmo

		type Gizmo @boundary {
			id: ID!
		}

		type Gadget @boundary {
			id: ID!
		}

		type Agent {
			id: ID!
			name: String!
			country: Country!
		}

		type Country {
			id: ID!
			name: String!
		}

		type GizmoImplementation implements Tool {
			id: ID!
			name: String!
			gizmos: [Gizmo!]!
		}

		type GadgetImplementation implements Tool {
			id: ID!
			name: String!
			gadgets: [Gadget!]!
		}

		type Query {
			tool(id: ID!): Tool!
		}`

	schema := gqlparser.MustLoadSchema(&ast.Source{Input: schemaString})
	ctx := testContextWithoutVariables(nil)

	t.Run("does not touch simple selection sets", func(t *testing.T) {
		selectionSet := ast.SelectionSet{
			&ast.Field{
				Alias:            "id",
				Name:             "id",
				Definition:       schema.Types["Agent"].Fields.ForName("id"),
				ObjectDefinition: schema.Types["Agent"],
			},
			&ast.Field{
				Alias:            "name",
				Name:             "name",
				Definition:       schema.Types["Agent"].Fields.ForName("name"),
				ObjectDefinition: schema.Types["Agent"],
			},
			&ast.Field{
				Alias:            "country",
				Name:             "country",
				Definition:       schema.Types["Agent"].Fields.ForName("country"),
				ObjectDefinition: schema.Types["Agent"],
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "id",
						Name:             "id",
						Definition:       schema.Types["Country"].Fields.ForName("id"),
						ObjectDefinition: schema.Types["Country"],
					},
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["Country"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["Country"],
					},
				},
			},
		}

		filtered := unionAndTrimSelectionSet("", schema, selectionSet)
		require.Equal(t, selectionSet, filtered)
	})

	t.Run("removes duplicate leaf values and merges composite scopes", func(t *testing.T) {
		selectionSet := ast.SelectionSet{
			&ast.Field{
				Alias:            "name",
				Name:             "name",
				Definition:       schema.Types["Agent"].Fields.ForName("name"),
				ObjectDefinition: schema.Types["Agent"],
			},
			&ast.Field{
				Alias:            "name",
				Name:             "name",
				Definition:       schema.Types["Agent"].Fields.ForName("name"),
				ObjectDefinition: schema.Types["Agent"],
			},
			&ast.Field{
				Alias:            "country",
				Name:             "country",
				Definition:       schema.Types["Agent"].Fields.ForName("country"),
				ObjectDefinition: schema.Types["Agent"],
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "id",
						Name:             "id",
						Definition:       schema.Types["Country"].Fields.ForName("id"),
						ObjectDefinition: schema.Types["Country"],
					},
				},
			},
			&ast.Field{
				Alias:            "country",
				Name:             "country",
				Definition:       schema.Types["Agent"].Fields.ForName("country"),
				ObjectDefinition: schema.Types["Agent"],
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["Country"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["Country"],
					},
				},
			},
		}

		filtered := unionAndTrimSelectionSet("", schema, selectionSet)
		require.Equal(t, formatSelectionSetSingleLine(ctx, schema, filtered), "{ name country { id name } }")
	})

	t.Run("removes field duplicates from inline fragment", func(t *testing.T) {
		initialSelectionSet := ast.SelectionSet{
			&ast.Field{
				Alias:            "id",
				Name:             "id",
				Definition:       schema.Types["Tool"].Fields.ForName("id"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.Field{
				Alias:            "name",
				Name:             "name",
				Definition:       schema.Types["Tool"].Fields.ForName("name"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.InlineFragment{
				TypeCondition: schema.Types["GizmoImplementation"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "id",
						Name:             "id",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("id"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
					},
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
					},
					&ast.Field{
						Alias:            "gizmos",
						Name:             "gizmos",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("gizmos"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
						SelectionSet: []ast.Selection{
							&ast.Field{
								Alias:            "id",
								Name:             "id",
								Definition:       schema.Types["Gizmo"].Fields.ForName("id"),
								ObjectDefinition: schema.Types["Gizmo"],
							},
						},
					},
				},
				ObjectDefinition: schema.Types["GizmoImplementation"],
			},
		}

		expected := ast.SelectionSet{
			&ast.Field{
				Alias:            "id",
				Name:             "id",
				Definition:       schema.Types["Tool"].Fields.ForName("id"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.Field{
				Alias:            "name",
				Name:             "name",
				Definition:       schema.Types["Tool"].Fields.ForName("name"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.InlineFragment{
				TypeCondition: schema.Types["GizmoImplementation"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "gizmos",
						Name:             "gizmos",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("gizmos"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
						SelectionSet: []ast.Selection{
							&ast.Field{
								Alias:            "id",
								Name:             "id",
								Definition:       schema.Types["Gizmo"].Fields.ForName("id"),
								ObjectDefinition: schema.Types["Gizmo"],
							},
						},
					},
				},
				ObjectDefinition: schema.Types["GizmoImplementation"],
			},
		}

		filtered := unionAndTrimSelectionSet("GizmoImplementation", schema, initialSelectionSet)
		require.Equal(t, formatSelectionSetSingleLine(ctx, schema, expected), formatSelectionSetSingleLine(ctx, schema, filtered))
	})

	t.Run("removes inline fragment if it only contains duplicate selections", func(t *testing.T) {
		initialSelectionSet := ast.SelectionSet{
			&ast.Field{
				Alias:            "id",
				Name:             "id",
				Definition:       schema.Types["Tool"].Fields.ForName("id"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.Field{
				Alias:            "name",
				Name:             "name",
				Definition:       schema.Types["Tool"].Fields.ForName("name"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.InlineFragment{
				TypeCondition: schema.Types["GizmoImplementation"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "id",
						Name:             "id",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("id"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
					},
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
					},
				},
				ObjectDefinition: schema.Types["GizmoImplementation"],
			},
		}

		expected := ast.SelectionSet{
			&ast.Field{
				Alias:            "id",
				Name:             "id",
				Definition:       schema.Types["Tool"].Fields.ForName("id"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.Field{
				Alias:            "name",
				Name:             "name",
				Definition:       schema.Types["Tool"].Fields.ForName("name"),
				ObjectDefinition: schema.Types["Tool"],
			},
		}

		filtered := unionAndTrimSelectionSet("GizmoImplementation", schema, initialSelectionSet)
		require.Equal(t, formatSelectionSetSingleLine(ctx, schema, expected), formatSelectionSetSingleLine(ctx, schema, filtered))
	})

	t.Run("removes inline fragment that does not match typename", func(t *testing.T) {
		initialSelectionSet := ast.SelectionSet{
			&ast.Field{
				Alias:            "id",
				Name:             "id",
				Definition:       schema.Types["Tool"].Fields.ForName("id"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.InlineFragment{
				TypeCondition: schema.Types["GizmoImplementation"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "id",
						Name:             "id",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("id"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
					},
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
					},
				},
				ObjectDefinition: schema.Types["GizmoImplementation"],
			},
			&ast.InlineFragment{
				TypeCondition: schema.Types["GadgetImplementation"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["GadgetImplementation"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["GadgetImplementation"],
					},
				},
				ObjectDefinition: schema.Types["GadgetImplementation"],
			},
		}

		expected := ast.SelectionSet{
			&ast.Field{
				Alias:            "id",
				Name:             "id",
				Definition:       schema.Types["Tool"].Fields.ForName("id"),
				ObjectDefinition: schema.Types["Tool"],
			},
			&ast.InlineFragment{
				TypeCondition: schema.Types["GizmoImplementation"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["GizmoImplementation"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["GizmoImplementation"],
					},
				},
				ObjectDefinition: schema.Types["GizmoImplementation"],
			},
		}

		filtered := unionAndTrimSelectionSet("GizmoImplementation", schema, initialSelectionSet)
		require.Equal(t, formatSelectionSetSingleLine(ctx, schema, expected), formatSelectionSetSingleLine(ctx, schema, filtered))
	})

	t.Run("works with unions", func(t *testing.T) {
		initialSelectionSet := ast.SelectionSet{
			&ast.InlineFragment{
				TypeCondition: schema.Types["Gizmo"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "id",
						Name:             "id",
						Definition:       schema.Types["Gizmo"].Fields.ForName("id"),
						ObjectDefinition: schema.Types["Gizmo"],
					},
				},
				ObjectDefinition: schema.Types["GadgetOrGizmo"],
			},
			&ast.InlineFragment{
				TypeCondition: schema.Types["Gadget"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["Gadget"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["Gadget"],
					},
				},
				ObjectDefinition: schema.Types["GadgetOrGizmo"],
			},
		}

		expected := ast.SelectionSet{
			&ast.InlineFragment{
				TypeCondition: schema.Types["Gadget"].Name,
				SelectionSet: []ast.Selection{
					&ast.Field{
						Alias:            "name",
						Name:             "name",
						Definition:       schema.Types["Gadget"].Fields.ForName("name"),
						ObjectDefinition: schema.Types["Gadget"],
					},
				},
				ObjectDefinition: schema.Types["GadgetOrGizmo"],
			},
		}

		filtered := unionAndTrimSelectionSet("Gadget", schema, initialSelectionSet)
		require.Equal(t, formatSelectionSetSingleLine(ctx, schema, expected), formatSelectionSetSingleLine(ctx, schema, filtered))
	})
}

func TestExtractBoundaryIDs(t *testing.T) {
	dataJSON := `{
		"gizmos": [
			{
				"_bramble_id": "1",
				"_bramble__typename": "Gizmo",
				"name": "Gizmo 1",
				"owner": {
					"_bramble_id": "1",
					"_bramble__typename": "Owner"
				}
			},
			{
				"_bramble_id": "2",
				"_bramble__typename": "Gizmo",
				"name": "Gizmo 2",
				"owner": {
					"_bramble_id": "1",
					"_bramble__typename": "Owner"
				}
			},
			{
				"_bramble_id": "3",
				"_bramble__typename": "Gizmo",
				"name": "Gizmo 3",
				"owner": {
					"_bramble_id": "2",
					"_bramble__typename": "Owner"
				}
			},
			{
				"_bramble_id": "4",
				"_bramble__typename": "Gizmo",
				"name": "Gizmo 4",
				"owner": {
					"_bramble_id": "5",
					"_bramble__typename": "Owner"
				}
			}
		]
	}`
	data := map[string]interface{}{}
	expected := []string{"1", "1", "2", "5"}
	insertionPoint := []string{"gizmos", "owner"}
	require.NoError(t, json.Unmarshal([]byte(dataJSON), &data))
	result, err := extractBoundaryIDs(data, insertionPoint, "Owner")
	require.NoError(t, err)
	require.Equal(t, expected, result)
}

func TestTrimInsertionPointForNestedBoundaryQuery(t *testing.T) {
	dataJSON := `[
			{
				"id": "1",
				"name": "Gizmo 1",
				"owner": {
					"_bramble_id": "1"
				}
			},
			{
				"id": "2",
				"name": "Gizmo 2",
				"owner": {
					"id": "1"
				}
			},
			{
				"id": "3",
				"name": "Gizmo 3",
				"owner": {
					"_bramble_id": "2"
				}
			},
			{
				"id": "4",
				"name": "Gizmo 4",
				"owner": {
					"id": "5"
				}
			}
		]`
	insertionPoint := []string{"namespace", "gizmos", "owner"}
	expected := []string{"owner"}
	result, err := trimInsertionPointForNestedBoundaryStep(jsonToInterfaceSlice(dataJSON), insertionPoint)
	require.NoError(t, err)
	require.Equal(t, expected, result)
}
