package bramble

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func TestMergeExecutionResults(t *testing.T) {
	t.Run("merges single map", func(t *testing.T) {
		inputMap := jsonToInterfaceMap(`{
			"gizmo": {
				"_bramble_id": "1",
				"id": "1",
				"color": "Gizmo A"
			}
		}`)

		result := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMap,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{result})

		require.NoError(t, err)
		require.Equal(t, inputMap, mergedMap)
	})

	t.Run("merges two top level results", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmoA": {
				"id": "1",
				"color": "Gizmo A"
			}
		}`)

		resultA := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMapA,
		}

		inputMapB := jsonToInterfaceMap(`{
			"gizmoB": {
				"id": "2",
				"color": "Gizmo B"
			}
		}`)

		resultB := executionResult{
			ServiceURL:     "http://service-b",
			InsertionPoint: []string{},
			Data:           inputMapB,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{resultA, resultB})

		expected := jsonToInterfaceMap(`{
			"gizmoA": {
				"id": "1",
				"color": "Gizmo A"
			},
			"gizmoB": {
				"id": "2",
				"color": "Gizmo B"
			}
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})

	t.Run("merges mid level array", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmo": {
				"_bramble_id": "1",
				"_bramble__typename": "Gizmo",
				"gadgets": [
					{"_bramble_id": "GADGET1", "_bramble__typename": "Gadget", "owner": { "_bramble_id": "OWNER1", "_bramble__typename": "Owner" }},
					{"_bramble_id": "GADGET3", "_bramble__typename": "Gadget", "owner": { "_bramble_id": "OWNER3", "_bramble__typename": "Owner" }},
					{"_bramble_id": "GADGET2", "_bramble__typename": "Gadget", "owner": null}
				]
			}
		}`)

		resultA := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMapA,
		}

		inputMapB := jsonToInterfaceSlice(`[
			{
				"_bramble_id": "OWNER1",
				"_bramble__typename": "Owner",
				"name": "008"
			}
		]`)

		resultB := executionResult{
			ServiceURL:     "http://service-b",
			InsertionPoint: []string{"gizmo", "gadgets", "owner"},
			Data:           inputMapB,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{resultA, resultB})

		expected := jsonToInterfaceMap(`
		{
			"gizmo": {
				"gadgets": [
					{
						"_bramble_id": "GADGET1",
						"_bramble__typename": "Gadget",
						"owner": {
							"_bramble_id": "OWNER1",
							"_bramble__typename": "Owner",
							"name": "008"
						}
					},
					{
						"_bramble_id": "GADGET3",
						"_bramble__typename": "Gadget",
						"owner": {
							"_bramble_id": "OWNER3",
							"_bramble__typename": "Owner"
						}
					},
					{
						"_bramble_id": "GADGET2",
						"_bramble__typename": "Gadget",
						"owner": null
					}
				],
				"_bramble_id": "1",
				"_bramble__typename": "Gizmo"
			}
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})

	t.Run("merges nested mid-level array", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmo": {
				"_bramble_id": "1",
				"_bramble__typename": "Gizmo",
				"gadgets": [
					[
						{"_bramble_id": "GADGET1", "_bramble__typename": "Gadget", "owner": { "_bramble_id": "OWNER1", "_bramble__typename": "Owner" }},
						{"_bramble_id": "GADGET3", "_bramble__typename": "Gadget", "owner": { "_bramble_id": "OWNER3", "_bramble__typename": "Owner" }}
					],
					[
						{"_bramble_id": "GADGET2", "_bramble__typename": "Gadget", "owner": null}
					]
				]
			}
		}`)

		resultA := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMapA,
		}

		inputMapB := jsonToInterfaceSlice(`[
			{
				"_bramble_id": "OWNER1",
				"_bramble__typename": "Owner",
				"name": "008"
			}
		]`)

		resultB := executionResult{
			ServiceURL:     "http://service-b",
			InsertionPoint: []string{"gizmo", "gadgets", "owner"},
			Data:           inputMapB,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{resultA, resultB})

		expected := jsonToInterfaceMap(`
		{
			"gizmo": {
				"gadgets": [
					[
						{
							"_bramble_id": "GADGET1",
							"_bramble__typename": "Gadget",
							"owner": {
								"_bramble_id": "OWNER1",
								"_bramble__typename": "Owner",
								"name": "008"
							}
						},
						{
							"_bramble_id": "GADGET3",
							"_bramble__typename": "Gadget",
							"owner": {
								"_bramble_id": "OWNER3",
								"_bramble__typename": "Owner"
							}
						}
					],
					[
						{
							"_bramble_id": "GADGET2",
							"_bramble__typename": "Gadget",
							"owner": null
						}
					]
				],
				"_bramble_id": "1",
				"_bramble__typename": "Gizmo"
			}
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})

	t.Run("merges root step with child step (root step returns object, boundary field is non array)", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmo": {
				"id": "1",
				"color": "Gizmo A",
				"owner": {
					"_bramble_id": "1",
					"_bramble__typename": "Owner"
				}
			}
		}`)

		resultA := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMapA,
		}

		inputSliceB := jsonToInterfaceSlice(`[
			{
				"_bramble_id": "1",
				"_bramble__typename": "Owner",
				"name": "Owner A"
			}
		]`)

		resultB := executionResult{
			ServiceURL:     "http://service-b",
			InsertionPoint: []string{"gizmo", "owner"},
			Data:           inputSliceB,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{resultA, resultB})

		expected := jsonToInterfaceMap(`{
			"gizmo": {
				"id": "1",
				"color": "Gizmo A",
				"owner": {
					"_bramble_id": "1",
					"_bramble__typename": "Owner",
					"name": "Owner A"
				}
			}
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})

	t.Run("merges root step with child step (root step returns array, boundary field is non array)", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"id": "1",
					"color": "RED",
					"owner": {
						"_bramble_id": "4",
						"_bramble__typename": "Owner"
					}
				},
				{
					"id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"_bramble__typename": "Owner"
					}
				},
				{
					"id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
						"_bramble__typename": "Owner"
					}
				}
			]
		}`)

		resultA := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMapA,
		}

		inputSliceB := jsonToInterfaceSlice(`[
			{
				"_bramble_id": "4",
				"_bramble__typename": "Owner",
				"name": "Owner A"
			},
			{
				"_bramble_id": "5",
				"_bramble__typename": "Owner",
				"name": "Owner B"
			},
			{
				"_bramble_id": "6",
				"_bramble__typename": "Owner",
				"name": "Owner C"
			}
		]`)

		resultB := executionResult{
			ServiceURL:     "http://service-b",
			InsertionPoint: []string{"gizmos", "owner"},
			Data:           inputSliceB,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{resultA, resultB})

		expected := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"id": "1",
					"color": "RED",
					"owner": {
						"_bramble_id": "4",
						"_bramble__typename": "Owner",
						"name": "Owner A"
					}
				},
				{
					"id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"_bramble__typename": "Owner",
						"name": "Owner B"
					}
				},
				{
					"id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
						"_bramble__typename": "Owner",
						"name": "Owner C"
					}
				}
			]
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})

	t.Run("merges root step with child step (root step returns array, boundary field is array)", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"id": "1",
					"color": "RED",
					"owner": {
						"_bramble_id": "4",
						"_bramble__typename": "Owner"
					}
				},
				{
					"id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"_bramble__typename": "Owner"
					}
				},
				{
					"id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
						"_bramble__typename": "Owner"
					}
				}
			]
		}`)

		resultA := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMapA,
		}

		inputSliceB := jsonToInterfaceSlice(`[
			{
				"_bramble_id": "4",
				"_bramble__typename": "Owner",
				"name": "Owner A"
			},
			{
				"_bramble_id": "5",
				"_bramble__typename": "Owner",
				"name": "Owner B"
			},
			{
				"_bramble_id": "6",
				"_bramble__typename": "Owner",
				"name": "Owner C"
			}
		]`)

		resultB := executionResult{
			ServiceURL:     "http://service-b",
			InsertionPoint: []string{"gizmos", "owner"},
			Data:           inputSliceB,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{resultA, resultB})

		expected := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"id": "1",
					"color": "RED",
					"owner": {
						"_bramble_id": "4",
						"_bramble__typename": "Owner",
						"name": "Owner A"
					}
				},
				{
					"id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"_bramble__typename": "Owner",
						"name": "Owner B"
					}
				},
				{
					"id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
						"_bramble__typename": "Owner",
						"name": "Owner C"
					}
				}
			]
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})

	t.Run("merges using '_bramble_id'", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"_bramble_id": "1",
					"_bramble__typename": "Gizmo",
					"color": "RED",
					"owner": {
						"_bramble_id": "4",
						"_bramble__typename": "Owner"
					}
				},
				{
					"_bramble_id": "2",
					"_bramble__typename": "Gizmo",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"_bramble__typename": "Owner"
					}
				},
				{
					"_bramble_id": "3",
					"_bramble__typename": "Gizmo",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
						"_bramble__typename": "Owner"
					}
				}
			]
		}`)

		resultA := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMapA,
		}

		inputSliceB := jsonToInterfaceSlice(`[
			{
				"_bramble_id": "4",
				"_bramble__typename": "Owner",
				"name": "Owner A"
			},
			{
				"_bramble_id": "5",
				"_bramble__typename": "Owner",
				"name": "Owner B"
			},
			{
				"_bramble_id": "6",
				"_bramble__typename": "Owner",
				"name": "Owner C"
			}
		]`)

		resultB := executionResult{
			ServiceURL:     "http://service-b",
			InsertionPoint: []string{"gizmos", "owner"},
			Data:           inputSliceB,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{resultA, resultB})

		expected := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"_bramble_id": "1",
					"_bramble__typename": "Gizmo",
					"color": "RED",
					"owner": {
						"_bramble_id": "4",
						"_bramble__typename": "Owner",
						"name": "Owner A"
					}
				},
				{
					"_bramble_id": "2",
					"_bramble__typename": "Gizmo",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"_bramble__typename": "Owner",
						"name": "Owner B"
					}
				},
				{
					"_bramble_id": "3",
					"_bramble__typename": "Gizmo",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
						"_bramble__typename": "Owner",
						"name": "Owner C"
					}
				}
			]
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})

	t.Run("merges with nil destination", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmo": {
				"_bramble_id": "1",
				"_bramble__typename": "Gizmo",
				"gadgets": [
					[
						{"_bramble_id": "GADGET1", "_bramble__typename": "Gadget", "details": { "owner": {"_bramble_id": "OWNER1", "_bramble__typename": "Owner" }}}
					],
					[
						{"_bramble_id": "GADGET2", "_bramble__typename": "Gadget", "details": null}
					]
				]
			}
		}`)

		resultA := executionResult{
			ServiceURL:     "http://service-a",
			InsertionPoint: []string{},
			Data:           inputMapA,
		}

		inputMapB := jsonToInterfaceSlice(`[
			{
				"_bramble_id": "OWNER1",
				"_bramble__typename": "Owner",
				"name": "Alice"
			}
		]`)

		resultB := executionResult{
			ServiceURL:     "http://service-b",
			InsertionPoint: []string{"gizmo", "gadgets", "details", "owner"},
			Data:           inputMapB,
		}

		mergedMap, err := mergeExecutionResults([]executionResult{resultA, resultB})

		expected := jsonToInterfaceMap(`
		{
			"gizmo": {
				"gadgets": [
					[
						{
							"_bramble_id": "GADGET1",
							"_bramble__typename": "Gadget",
							"details": {
								"owner": {
									"_bramble_id": "OWNER1",
									"_bramble__typename": "Owner",
									"name": "Alice"
								}
							}
						}
					],
					[
						{
							"_bramble_id": "GADGET2",
							"_bramble__typename": "Gadget",
							"details": null
						}
					]
				],
				"_bramble_id": "1",
				"_bramble__typename": "Gizmo"
			}
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})
}

func TestBubbleUpNullValuesInPlace(t *testing.T) {
	t.Run("no expected or unexpected nulls", func(t *testing.T) {
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
		}`

		result := jsonToInterfaceMap(`
			{
				"gizmos": [
					{ "id": "GIZMO1" },
					{ "id": "GIZMO2" },
					{ "id": "GIZMO3" }
				]
			}
		`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
				}
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Nil(t, errs)
	})

	t.Run("1 expected null (bubble to root)", func(t *testing.T) {
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
		}`

		result := jsonToInterfaceMap(`
			{
				"gizmos": [
					{ "id": "GIZMO1", "color": "RED" },
					{ "id": "GIZMO2", "color": "GREEN" },
					{ "id": "GIZMO3", "color": null }
				]
			}
		`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
					color
				}
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.Equal(t, errNullBubbledToRoot, err)
		require.Len(t, errs, 1)
	})

	t.Run("1 expected null (bubble to middle)", func(t *testing.T) {
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
			gizmos: [Gizmo!]
			getOwners(ids: [ID!]!): [Owner!]!
		}`

		result := jsonToInterfaceMap(`
			{
				"gizmos": [
					{ "id": "GIZMO1", "color": "RED" },
					{ "id": "GIZMO2", "color": "GREEN" },
					{ "id": "GIZMO3", "color": null }
				]
			}
		`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
					color
				}
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Equal(t, []*gqlerror.Error([]*gqlerror.Error{
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("gizmos"), ast.PathIndex(2), ast.PathName("color")},
				Extensions: nil,
			}}), errs)
		require.Equal(t, jsonToInterfaceMap(`{ "gizmos": null }`), result)
	})

	t.Run("all nulls (bubble to middle)", func(t *testing.T) {
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
			gizmos: [Gizmo!]
			getOwners(ids: [ID!]!): [Owner!]!
		}`

		result := jsonToInterfaceMap(`
			{
				"gizmos": [
					{ "id": "GIZMO1", "color": null },
					{ "id": "GIZMO2", "color": null },
					{ "id": "GIZMO3", "color": null }
				]
			}
		`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
					color
				}
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Equal(t, []*gqlerror.Error([]*gqlerror.Error{
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("gizmos"), ast.PathIndex(0), ast.PathName("color")},
				Extensions: nil,
			},
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("gizmos"), ast.PathIndex(1), ast.PathName("color")},
				Extensions: nil,
			},
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("gizmos"), ast.PathIndex(2), ast.PathName("color")},
				Extensions: nil,
			},
		}), errs)
		require.Equal(t, jsonToInterfaceMap(`{ "gizmos": null }`), result)
	})

	t.Run("1 expected null (bubble to middle in array)", func(t *testing.T) {
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
			gizmos: [Gizmo]!
			getOwners(ids: [ID!]!): [Owner!]!
		}`

		result := jsonToInterfaceMap(`
			{
				"gizmos": [
					{ "id": "GIZMO1", "color": "RED" },
					{ "id": "GIZMO3", "color": null },
					{ "id": "GIZMO2", "color": "GREEN" }
				]
			}
		`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
					color
				}
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Equal(t, []*gqlerror.Error([]*gqlerror.Error{
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("gizmos"), ast.PathIndex(1), ast.PathName("color")},
				Extensions: nil,
			}}), errs)
		require.Equal(t, jsonToInterfaceMap(`{ "gizmos": [ { "id": "GIZMO1", "color": "RED" }, null, { "id": "GIZMO2", "color": "GREEN" } ]	}`), result)
	})

	t.Run("0 expected nulls", func(t *testing.T) {
		ddl := `
		type Gizmo {
			id: ID!
			color: String
			owner: Owner
		}

		type Owner {
			id: ID!
			name: String!
		}

		type Query {
			gizmos: [Gizmo!]!
			getOwners(ids: [ID!]!): [Owner!]!
		}`

		resultJSON := `{
			"gizmos": [
				{ "id": "GIZMO1", "color": "RED" },
				{ "id": "GIZMO2", "color": "GREEN" },
				{ "id": "GIZMO3", "color": null }
			]
		}`

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
					color
				}
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		result := jsonToInterfaceMap(resultJSON)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Empty(t, errs)
		require.Equal(t, jsonToInterfaceMap(resultJSON), result)
	})

	t.Run("works with fragment spreads", func(t *testing.T) {
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
			gizmos: [Gizmo]!
			getOwners(ids: [ID!]!): [Owner!]!
		}`

		resultJSON := `{
			"gizmos": [
				{ "id": "GIZMO1", "color": "RED", "_bramble__typename": "Gizmo" },
				{ "id": "GIZMO2", "color": "GREEN", "_bramble__typename": "Gizmo" },
				{ "id": "GIZMO3", "color": null, "_bramble__typename": "Gizmo" }
			]
		}`

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			fragment GizmoDetails on Gizmo {
				id
				color
				__typename
			}
			{
				gizmos {
					...GizmoDetails
				}
			}
		`

		document := gqlparser.MustLoadQuery(schema, query)

		result := jsonToInterfaceMap(resultJSON)

		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Equal(t, []*gqlerror.Error([]*gqlerror.Error{
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("gizmos"), ast.PathIndex(2), ast.PathName("color")},
				Extensions: nil,
			}}), errs)
		require.Equal(t, jsonToInterfaceMap(`{ "gizmos": [ { "id": "GIZMO1", "color": "RED", "_bramble__typename": "Gizmo" }, { "id": "GIZMO2", "color": "GREEN", "_bramble__typename": "Gizmo" }, null ]	}`), result)
	})

	t.Run("works with inline fragments", func(t *testing.T) {
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
			gizmos: [Gizmo]!
			getOwners(ids: [ID!]!): [Owner!]!
		}`

		resultJSON := `{
			"gizmos": [
				{ "id": "GIZMO1", "color": "RED", "_bramble__typename": "Gizmo" },
				{ "id": "GIZMO2", "color": "GREEN", "_bramble__typename": "Gizmo" },
				{ "id": "GIZMO3", "color": null, "_bramble__typename": "Gizmo" }
			]
		}`

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					... on Gizmo {
						id
						color
						__typename
					}
				}
			}
		`

		document := gqlparser.MustLoadQuery(schema, query)
		result := jsonToInterfaceMap(resultJSON)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Equal(t, []*gqlerror.Error([]*gqlerror.Error{
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("gizmos"), ast.PathIndex(2), ast.PathName("color")},
				Extensions: nil,
			}}), errs)
		require.Equal(t, jsonToInterfaceMap(`{ "gizmos": [ { "id": "GIZMO1", "color": "RED", "_bramble__typename": "Gizmo" }, { "id": "GIZMO2", "color": "GREEN", "_bramble__typename": "Gizmo" }, null ]	}`), result)
	})

	t.Run("inline fragment inside interface", func(t *testing.T) {
		ddl := `
		interface Critter {
			id: ID!
		}

		type Gizmo implements Critter {
			id: ID!
			color: String!
		}

		type Gremlin implements Critter {
			id: ID!
			name: String!
		}

		type Query {
			critters: [Critter]!
		}`

		resultJSON := `{
			"critters": [
				{ "id": "GIZMO1", "color": "RED", "_bramble__typename": "Gizmo" },
				{ "id": "GREMLIN1", "name": "Spikey", "_bramble__typename": "Gremlin" },
				{ "id": "GIZMO2", "color": null, "_bramble__typename": "Gizmo" }
			]
		}`

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				critters {
					id
					... on Gizmo {
						color
						__typename
					}
					... on Gremlin {
						name
						__typename
					}
				}
			}
		`

		document := gqlparser.MustLoadQuery(schema, query)
		result := jsonToInterfaceMap(resultJSON)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Equal(t, []*gqlerror.Error([]*gqlerror.Error{
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("critters"), ast.PathIndex(2), ast.PathName("color")},
				Extensions: nil,
			}}), errs)
		require.Equal(t, jsonToInterfaceMap(`{ "critters": [ { "id": "GIZMO1", "color": "RED", "_bramble__typename": "Gizmo"  }, { "id": "GREMLIN1", "name": "Spikey", "_bramble__typename": "Gremlin" }, null ]	}`), result)
	})

	t.Run("fragment spread inside interface", func(t *testing.T) {
		ddl := `
		interface Critter {
			id: ID!
		}

		type Gizmo implements Critter {
			id: ID!
			color: String!
		}

		type Gremlin implements Critter {
			id: ID!
			name: String!
		}

		type Query {
			critters: [Critter]!
		}`

		resultJSON := `{
			"critters": [
				{ "id": "GIZMO1", "color": "RED", "_bramble__typename": "Gizmo" },
				{ "id": "GREMLIN1", "name": "Spikey", "_bramble__typename": "Gremlin" },
				{ "id": "GIZMO2", "color": null, "_bramble__typename": "Gizmo" }
			]
		}`

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			fragment CritterDetails on Critter {
				... on Gizmo {
					color
					__typename
				}
				... on Gremlin {
					name
					__typename
				}
			}

			{
				critters {
					id
					... CritterDetails
				}
			}
		`

		document := gqlparser.MustLoadQuery(schema, query)
		result := jsonToInterfaceMap(resultJSON)
		errs, err := bubbleUpNullValuesInPlace(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.Equal(t, []*gqlerror.Error([]*gqlerror.Error{
			{
				Message:    `got a null response for non-nullable field "color"`,
				Path:       ast.Path{ast.PathName("critters"), ast.PathIndex(2), ast.PathName("color")},
				Extensions: nil,
			}}), errs)
		require.Equal(t, jsonToInterfaceMap(`{ "critters": [ { "id": "GIZMO1", "color": "RED", "_bramble__typename": "Gizmo"  }, { "id": "GREMLIN1", "name": "Spikey", "_bramble__typename": "Gremlin" }, null ]	}`), result)
	})
}

func TestFormatResponseBody(t *testing.T) {
	t.Run("simple response with no errors", func(t *testing.T) {
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
		}`

		result := jsonToInterfaceMap(`
			{
				"gizmos": [
					{ "color": "RED","owner": { "name": "Owner1", "id": "1" }, "id": "GIZMO1" },
					{ "color": "BLUE","owner": { "name": "Owner2", "id": "2" }, "id": "GIZMO2" },
					{ "color": "GREEN","owner": { "name": "Owner3", "id": "3" }, "id": "GIZMO3" }
				]
			}
		`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
					color
					owner {
						id
						name
					}
				}
			}`

		expectedJSON := `
			{
				"gizmos": [
					{ "id": "GIZMO1", "color": "RED", "owner": { "id": "1", "name": "Owner1" } },
					{ "id": "GIZMO2", "color": "BLUE", "owner": { "id": "2", "name": "Owner2" } },
					{ "id": "GIZMO3", "color": "GREEN", "owner": { "id": "3", "name": "Owner3" } }
				]
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("null data", func(t *testing.T) {
		ddl := `
		type Gizmo {
			id: ID!
			color: String!
			owner: Owner
		}

		type Owner {
			id: ID!
			name: String
		}

		type Query {
			gizmos: [Gizmo!]!
			gizmo: Gizmo!
		}`

		result := jsonToInterfaceMap(`
			{
				"gizmos": [
					{ "color": "RED","owner": null, "id": "GIZMO1" },
					{ "color": "BLUE","owner": { "name": "Owner2", "id": "2" }, "id": "GIZMO2" },
					{ "color": "GREEN","owner": { "name": null, "id": "3" }, "id": "GIZMO3" }
				]
			}
		`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
					color
					owner {
						id
						name
					}
				}
			}`

		expectedJSON := `
			{
				"gizmos": [
					{ "id": "GIZMO1", "color": "RED", "owner": null },
					{ "id": "GIZMO2", "color": "BLUE", "owner": { "id": "2", "name": "Owner2" } },
					{ "id": "GIZMO3", "color": "GREEN", "owner": { "id": "3", "name": null } }
				]
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("simple response with errors", func(t *testing.T) {
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
		}`

		result := jsonToInterfaceMap(`
			{
				"gizmos": [
					{ "color": "RED","owner": { "name": "Owner1", "id": "1" }, "id": "GIZMO1" },
					{ "color": "BLUE","owner": { "name": "Owner2", "id": "2" }, "id": "GIZMO2" },
					{ "color": "GREEN","owner": { "name": "Owner3", "id": "3" }, "id": "GIZMO3" }
				]
			}
		`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
			{
				gizmos {
					id
					color
					owner {
						id
						name
					}
				}
			}`

		expectedJSON := `
			{
				"gizmos": [
					{ "id": "GIZMO1", "color": "RED", "owner": { "id": "1", "name": "Owner1" } },
					{ "id": "GIZMO2", "color": "BLUE", "owner": { "id": "2", "name": "Owner2" } },
					{ "id": "GIZMO3", "color": "GREEN", "owner": { "id": "3", "name": "Owner3" } }
				]
			}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("field selection overlaps with fragment selection", func(t *testing.T) {
		ddl := `
			interface Gizmo {
				id: ID!
				name: String!
			}

			type Owner {
				id: ID!
				fullName: String!
			}

			type Gadget implements Gizmo {
				id: ID!
				name: String!
				owner: Owner
			}

			type Query {
				gizmo: Gizmo!
			}
		`

		result := jsonToInterfaceMap(`{
			"gizmo": {
				"id": "GADGET1",
				"name": "Gadget #1",
				"owner": {
					"id": "OWNER1",
					"fullName": "James Bond"
				},
				"_bramble__typename": "Gadget",
				"__typename": "Gadget"
			}
		}`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
		query Gizmo {
			gizmo {
				__typename
				...GizmoDetails
			}
		}

		fragment GizmoDetails on Gizmo {
			id
			name
			... on Gadget {
				id
				name
				owner {
					id
					fullName
				}
			}
		}`

		expectedJSON := `
		{
			"gizmo": {
				"id": "GADGET1",
				"name": "Gadget #1",
				"owner": {
					"id": "OWNER1",
					"fullName": "James Bond"
				},
				"__typename": "Gadget"
			}
		}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("field selection entirely overlaps with fragment selection", func(t *testing.T) {
		ddl := `
			interface Gizmo {
				id: ID!
				name: String!
			}

			type Owner {
				id: ID!
				fullName: String!
			}

			type Gadget implements Gizmo {
				id: ID!
				name: String!
				owner: Owner
			}

			type Query {
				gizmo: Gizmo!
			}
		`

		result := jsonToInterfaceMap(`{
			"gizmo": {
				"id": "GADGET1",
				"name": "Gadget #1",
				"_bramble__typename": "Gadget",
				"__typename": "Gadget"
			}
		}
	`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
		query Gizmo {
			gizmo {
				...GizmoDetails
				__typename
			}
		}

		fragment GizmoDetails on Gizmo {
			id
			name
			... on Gadget {
				id
				name
			}
		}`

		expectedJSON := `
		{
			"gizmo": {
				"id": "GADGET1",
				"name": "Gadget #1",
				"__typename": "Gadget"
			}
		}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("multiple implementation fragment spreads", func(t *testing.T) {
		ddl := `
			interface Gizmo {
				id: ID!
				name: String!
			}

			type Owner {
				id: ID!
				fullName: String!
			}

			type Gadget implements Gizmo {
				id: ID!
				name: String!
				owner: Owner
			}

			type Tool implements Gizmo {
				id: ID!
				name: String!
				category: String!
			}

			type Query {
				gizmo: Gizmo!
			}
		`

		result := jsonToInterfaceMap(`{
			"gizmo": {
				"id": "GADGET1",
				"name": "Gadget #1",
				"__typename": "Gadget",
				"_bramble__typename": "Gadget"
			}
		}
	`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
		query Gizmo {
			gizmo {
				...GizmoDetails
				__typename
			}
		}

		fragment GizmoDetails on Gizmo {
			id
			name
			... on Gadget {
				id
				name
			}
			... on Tool {
				category
			}
		}`

		expectedJSON := `
		{
			"gizmo": {
				"id": "GADGET1",
				"name": "Gadget #1",
				"__typename": "Gadget"
			}
		}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("multiple implementations with incomplete fragment spreads", func(t *testing.T) {
		ddl := `
			interface Gizmo {
				id: ID!
				name: String!
			}

			type Gadget implements Gizmo {
				id: ID!
				name: String!
				owner: String!
			}

			type Tool implements Gizmo {
				id: ID!
				name: String!
				category: String!
			}

			type Query {
				gizmos: [Gizmo!]!
			}
		`

		result := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"id": "GADGET1",
					"name": "Gadget #1",
					"owner": "Bob",
					"__typename": "Gadget",
					"_bramble__typename": "Gadget"
				},
				{
					"id": "GADGET2",
					"name": "Gadget #2",
					"category": "Plastic",
					"__typename": "Tool",
					"_bramble__typename": "Tool"
				}
			]
		}`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
		query Gizmo {
			gizmos {
				id
				...GizmoDetails
			}
		}

		fragment GizmoDetails on Gizmo {
			... on Gadget {
				name
				owner
			}
		}`

		expectedJSON := `
		{
			"gizmos": [
				{
					"id": "GADGET1",
					"name": "Gadget #1",
					"owner": "Bob"
				},
				{
					"id": "GADGET2"
				}
			]
		}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("multiple implementations with empty fragment spreads before other fields", func(t *testing.T) {
		ddl := `
			interface Gizmo {
				id: ID!
				name: String!
			}

			type Gadget implements Gizmo {
				id: ID!
				name: String!
				owner: String!
			}

			type Tool implements Gizmo {
				id: ID!
				name: String!
				category: String!
			}

			type Query {
				gizmos: [Gizmo!]!
			}
		`

		result := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"id": "GADGET1",
					"name": "Gadget #1",
					"owner": "Bob",
					"__typename": "Gadget",
					"_bramble__typename": "Gadget"
				},
				{
					"id": "GADGET2",
					"name": "Gadget #2",
					"category": "Plastic",
					"__typename": "Tool",
					"_bramble__typename": "Tool"
				}
			]
		}`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
		query Gizmo {
			gizmos {
				...GizmoDetails
				id
			}
		}

		fragment GizmoDetails on Gizmo {
			... on Gadget {
				name
				owner
			}
		}`

		expectedJSON := `
		{
			"gizmos": [
				{
					"id": "GADGET1",
					"name": "Gadget #1",
					"owner": "Bob"
				},
				{
					"id": "GADGET2"
				}
			]
		}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("multiple implementations with multiple empty fragment spreads around other fields", func(t *testing.T) {
		ddl := `
			interface Gizmo {
				id: ID!
				name: String!
			}

			type Gadget implements Gizmo {
				id: ID!
				name: String!
				owner: String!
			}

			type Tool implements Gizmo {
				id: ID!
				name: String!
				category: String!
			}

			type Query {
				gizmos: [Gizmo!]!
			}
		`

		result := jsonToInterfaceMap(`{
			"gizmos": [
				{
					"id": "GADGET1",
					"name": "Gadget #1",
					"owner": "Bob",
					"__typename": "Gadget",
					"_bramble__typename": "Gadget"
				},
				{
					"id": "GADGET2",
					"name": "Gadget #2",
					"category": "Plastic",
					"__typename": "Tool",
					"_bramble__typename": "Tool"
				}
			]
		}`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
		query Gizmo {
			gizmos {
				...GizmoName
				id
				...GizmoDetails
			}
		}

		fragment GizmoName on Gizmo {
			... on Gadget {
				name
			}
		}

		fragment GizmoDetails on Gizmo {
			... on Gadget {
				owner
			}
		}`

		expectedJSON := `
		{
			"gizmos": [
				{
					"id": "GADGET1",
					"name": "Gadget #1",
					"owner": "Bob"
				},
				{
					"id": "GADGET2"
				}
			]
		}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})

	t.Run("multiple implementation fragment spreads (bottom fragment matches)", func(t *testing.T) {
		ddl := `
			interface Gizmo {
				id: ID!
				name: String!
			}

			type Owner {
				id: ID!
				fullName: String!
			}

			type Gadget implements Gizmo {
				id: ID!
				name: String!
				owner: Owner
			}

			type Tool implements Gizmo {
				id: ID!
				name: String!
				category: String!
			}

			type Query {
				gizmo: Gizmo!
			}
		`

		result := jsonToInterfaceMap(`{
			"gizmo": {
				"id": "TOOL1",
				"name": "Tool #1",
				"category": "Screwdriver",
				"_bramble__typename": "Tool",
				"__typename": "Tool"
			}
		}
	`)

		schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: ddl})

		query := `
		query Gizmo {
			gizmo {
				...GizmoDetails
				__typename
			}
		}

		fragment GizmoDetails on Gizmo {
			id
			name
			... on Gadget {
				id
				name
			}
			... on Tool {
				category
			}
		}`

		expectedJSON := `
		{
			"gizmo": {
				"id": "TOOL1",
				"name": "Tool #1",
				"category": "Screwdriver",
				"__typename": "Tool"
			}
		}`

		document := gqlparser.MustLoadQuery(schema, query)
		bodyJSON := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})
}
