package bramble

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryPlanA(t *testing.T) {
	PlanTestFixture1.Check(t, "{ movies { id title } }", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id title _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": null
		  }
		]
	  }
	`)
}

func TestQueryPlanAB1(t *testing.T) {
	PlanTestFixture1.Check(t, "{movies {id compTitles(limit: 42) { id }}}", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": [
			  {
				"ServiceURL": "B",
				"ParentType": "Movie",
				"SelectionSet": "{ compTitles(limit: 42) { id _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": ["movies"],
				"Then": null
			  }
			]
		  }
		]
	  }
	`)
}

func TestQueryPlanAB2(t *testing.T) {
	PlanTestFixture1.Check(t, "{ movies { id compTitles(limit: 42) { id compTitles(limit: 666) { id } } } }", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": [
			  {
				"ServiceURL": "B",
				"ParentType": "Movie",
				"SelectionSet": "{ compTitles(limit: 42) { id compTitles(limit: 666) { id _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": ["movies"],
				"Then": null
			  }
			]
		  }
		]
	  }
	`)
}

func TestQueryPlanABA1(t *testing.T) {
	PlanTestFixture1.Check(t, "{movies {id compTitles(limit: 42) { id title }}}", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": [
			  {
				"ServiceURL": "B",
				"ParentType": "Movie",
				"SelectionSet": "{ compTitles(limit: 42) { id _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": ["movies"],
				"Then": [
				  {
					"ServiceURL": "A",
					"ParentType": "Movie",
					"SelectionSet": "{ title _bramble_id: id _bramble__typename: __typename }",
					"InsertionPoint": ["movies", "compTitles"],
					"Then": null
				  }
				]
			  }
			]
		  }
		]
	  }
	`)
}

func TestQueryPlanABA2(t *testing.T) {
	PlanTestFixture1.Check(t, "{movies {id compTitles(limit: 42) { id title compTitles(limit: 666) { id title } }}}", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": [
			  {
				"ServiceURL": "B",
				"ParentType": "Movie",
				"SelectionSet": "{ compTitles(limit: 42) { id compTitles(limit: 666) { id _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": ["movies"],
				"Then": [
				  {
					"ServiceURL": "A",
					"ParentType": "Movie",
					"SelectionSet": "{ title _bramble_id: id _bramble__typename: __typename }",
					"InsertionPoint": ["movies", "compTitles", "compTitles"],
					"Then": null
				  },
				  {
					"ServiceURL": "A",
					"ParentType": "Movie",
					"SelectionSet": "{ title _bramble_id: id _bramble__typename: __typename }",
					"InsertionPoint": ["movies", "compTitles"],
					"Then": null
				  }
				]
			  }
			]
		  }
		]
	  }
	`)
}

func TestQueryPlanAC(t *testing.T) {
	PlanTestFixture1.Check(t, "{movies {id title} transactions{id gross}}", `
      {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id title _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": null
		  },
		  {
			"ServiceURL": "C",
			"ParentType": "Query",
			"SelectionSet": "{ transactions { id gross } }",
			"InsertionPoint": null,
			"Then": null
		  }
		]
	  }
	`)
}

func TestQueryPlanWithAliases(t *testing.T) {
	PlanTestFixture1.Check(t, "{ a1: movies { a2: id a3: title a4: compTitles(limit: 42) { a5: id } } }", `
      {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ a1: movies { a2: id a3: title _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": [
			  {
				"ServiceURL": "B",
				"ParentType": "Movie",
				"SelectionSet": "{ a4: compTitles(limit: 42) { a5: id _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": ["a1"],
				"Then": null
			  }
			]
		  }
		]
	  }
	`)
}

func TestQueryPlanWithTypename(t *testing.T) {
	PlanTestFixture1.Check(t, "{__typename movies {id title __typename} transactions{id gross __typename}}", `
		  {
			"RootSteps": [
			  {
				"ServiceURL": "A",
				"ParentType": "Query",
				"SelectionSet": "{ movies { id title __typename _bramble_id: id _bramble__typename: __typename } }",
				"InsertionPoint": null,
				"Then": null
			  },
			  {
				"ServiceURL": "C",
				"ParentType": "Query",
				"SelectionSet": "{ transactions { id gross __typename } }",
				"InsertionPoint": null,
				"Then": null
			  },
			  {
				"ServiceURL": "__bramble",
				"ParentType": "Query",
				"SelectionSet": "{ __typename }",
				"InsertionPoint": null,
				"Then": null
			  }
			]
		  }
		`)
}

func TestQueryPlanNestedNoBoundaryType(t *testing.T) {
	PlanTestFixture2.Check(t, "{ gizmos { id name gadgets { id name gimmicks { id name } } } }", `
      {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ gizmos { id name gadgets { id name gimmicks { id name } } } }",
			"InsertionPoint": null,
			"Then": null
		  }
		]
	  }
	`)
}

func TestQueryPlanOptionalArgument(t *testing.T) {
	PlanTestFixture1.Check(t, "{ movies { id title(language: French) } }", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id title(language: French) _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": null
		  }
		]
	  }
	`)
}

func TestQueryPlanInlineFragment(t *testing.T) {
	query := `{
		movies {
			... on Movie {
				id
				title(language: French)
			}
		}
	}`
	plan := `{
		"RootSteps": [
			{
				"ServiceURL": "A",
				"ParentType": "Query",
				"SelectionSet": "{ movies { ... on Movie { id title(language: French) _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename } }",
				"InsertionPoint": null,
				"Then": null
			}
		]
	}`
	PlanTestFixture1.Check(t, query, plan)
}

func TestQueryPlanInlineFragmentDoesNotDuplicateTypename(t *testing.T) {
	query := `{
		movies {
			... on Movie {
				__typename
				id
				title(language: French)
			}
		}
	}`
	plan := `{
		"RootSteps": [
			{
				"ServiceURL": "A",
				"ParentType": "Query",
				"SelectionSet": "{ movies { ... on Movie { __typename id title(language: French) _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename } }",
				"InsertionPoint": null,
				"Then": null
			}
		]
	}`
	PlanTestFixture1.Check(t, query, plan)
}

func TestQueryPlanInlineFragmentPlan(t *testing.T) {
	query := `{
		movies {
			... on Movie {
				id
				title(language: French)
				compTitles(limit: 42) {
					id
				}
			}
		}
	}`
	plan := `{
		"RootSteps": [
			{
				"ServiceURL": "A",
				"ParentType": "Query",
				"SelectionSet": "{ movies { ... on Movie { id title(language: French) _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename } }",
				"InsertionPoint": null,
				"Then": [
					{
						"ServiceURL": "B",
						"ParentType": "Movie",
						"SelectionSet": "{ compTitles(limit: 42) { id _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
						"InsertionPoint": ["movies"],
						"Then": null
					}
				]
			}
		]
	}`
	PlanTestFixture1.Check(t, query, plan)
}

func TestQueryPlanFragmentSpread1(t *testing.T) {
	query := `
	fragment Frag on Movie {
		id
		title(language: French)
	}
	{
		movies {
			...Frag
		}
	}`
	plan := `{
		"RootSteps": [
			{
				"ServiceURL": "A",
				"ParentType": "Query",
				"SelectionSet": "{ movies { ... on Movie { id title(language: French) _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename } }",
				"InsertionPoint": null,
				"Then": null
			}
		]
	}`

	PlanTestFixture1.Check(t, query, plan)
}

func TestQueryPlanFragmentSpread2(t *testing.T) {
	query := `
	fragment Frag on Query {
		movies {
			id
			title(language: French)
		}
	}
	{
		...Frag
	}`
	plan := `{
		"RootSteps": [
			{
				"ServiceURL": "A",
				"ParentType": "Query",
				"SelectionSet": "{ movies { id title(language: French) _bramble_id: id _bramble__typename: __typename } }",
				"InsertionPoint": null,
				"Then": null
			}
		]
	}`
	PlanTestFixture1.Check(t, query, plan)
}

func TestQueryPlanCompleteDeepTraversal(t *testing.T) {
	query := `
	{
		shop1 {
			name
			products {
				name
				collection {
					name
				}
			}
		}
	}`
	plan := `{
		"RootSteps": [
			{
				"ServiceURL": "A",
				"ParentType": "Query",
				"SelectionSet": "{ shop1 { name products { _bramble_id: id _bramble__typename: __typename } } }",
				"InsertionPoint": null,
				"Then": [
					{
					"ServiceURL": "B",
					"ParentType": "Product",
					"SelectionSet": "{ name collection { _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
					"InsertionPoint": ["shop1", "products"],
					"Then": [
							{
							"ServiceURL": "C",
							"ParentType": "Collection",
							"SelectionSet": "{ name _bramble_id: id _bramble__typename: __typename }",
							"InsertionPoint": ["shop1", "products", "collection"],
							"Then": null
							}
						]
					}
				]
			}
		]
	}`
	PlanTestFixture6.Check(t, query, plan)
}

func TestQueryPlanMergeInsertionPointSteps(t *testing.T) {
	query := `
	{
		shop1 {
			products {
				name
			}
			products {
				name
			}
		}
	}`
	plan := `{
		"RootSteps": [
			{
				"ServiceURL": "A",
				"ParentType": "Query",
				"SelectionSet": "{ shop1 { products { _bramble_id: id _bramble__typename: __typename } products { _bramble_id: id _bramble__typename: __typename } } }",
				"InsertionPoint": null,
				"Then": [
					{
					"ServiceURL": "B",
					"ParentType": "Product",
					"SelectionSet": "{ name _bramble_id: id _bramble__typename: __typename name _bramble_id: id _bramble__typename: __typename }",
					"InsertionPoint": ["shop1", "products"],
					"Then": null
					}
				]
			}
		]
	}`
	PlanTestFixture6.Check(t, query, plan)
}

func TestQueryPlanExpandAbstractTypesWithPossibleBoundaryIds(t *testing.T) {
	query := `
	{
		animals {
			name
		}
	}`
	rootFieldSelections := []string{
		"name",
		"... on Lion { _bramble_id: id }",
		"... on Snake { _bramble_id: id }",
		"_bramble__typename: __typename",
	}
	PlanTestFixture3.CheckUnorderedRootFieldSelections(t, query, rootFieldSelections)
}

func TestQueryPlanInlineFragmentSpreadOfInterface(t *testing.T) {
	query := `
	{
		animals {
			name
			... on Lion {
				maneColor
			}
			... on Snake {
				venomous
			}
		}
	}`
	rootFieldSelections := []string{
		"name",
		"... on Lion { _bramble_id: id }",
		"... on Snake { _bramble_id: id }",
		"... on Lion { maneColor _bramble_id: id _bramble__typename: __typename }",
		"... on Snake { _bramble_id: id _bramble__typename: __typename }",
		"_bramble__typename: __typename",
	}
	PlanTestFixture3.CheckUnorderedRootFieldSelections(t, query, rootFieldSelections)
}

func TestQueryPlanSkipDirective(t *testing.T) {
	PlanTestFixture1.Check(t, "{ movies { id title @skip(if: false) } }", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id title @skip(if: false) _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": null
		  }
		]
	  }
	`)
}

func TestQueryPlanIncludeDirective(t *testing.T) {
	PlanTestFixture1.Check(t, "{ movies { id title @include(if: true) } }", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id title @include(if: true) _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": null
		  }
		]
	  }
	`)
}

func TestQueryPlanSkipAndIncludeDirective(t *testing.T) {
	PlanTestFixture1.Check(t, "{ movies { id title @skip(if: false) @include(if: true) } }", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id title @skip(if: false) @include(if: true) _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": null
		  }
		]
	  }
	`)
}

func TestQueryPlanSkipAndIncludeDirectiveInChildStep(t *testing.T) {
	PlanTestFixture1.Check(t, "{movies {id compTitles(limit: 42) { id @skip(if: false) @include(if: true) }}}", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { id _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": [
			  {
				"ServiceURL": "B",
				"ParentType": "Movie",
				"SelectionSet": "{ compTitles(limit: 42) { id @skip(if: false) @include(if: true) _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": ["movies"],
				"Then": null
			  }
			]
		  }
		]
	  }
	`)
}

func TestQueryPlanSupportsAliasing(t *testing.T) {
	PlanTestFixture1.Check(t, "{ foo: movies { id aliasTitle: title bar: compTitles(limit: 42) { id compTitleAliasTitle: title } } }", `
    {
      "RootSteps": [
        {
          "ServiceURL": "A",
          "ParentType": "Query",
          "SelectionSet": "{ foo: movies { id aliasTitle: title _bramble_id: id _bramble__typename: __typename } }",
          "InsertionPoint": null,
          "Then": [
            {
              "ServiceURL": "B",
              "ParentType": "Movie",
              "SelectionSet": "{ bar: compTitles(limit: 42) { id _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename }",
              "InsertionPoint": [
                "foo"
              ],
              "Then": [
                {
                  "ServiceURL": "A",
                  "ParentType": "Movie",
                  "SelectionSet": "{ compTitleAliasTitle: title _bramble_id: id _bramble__typename: __typename }",
                  "InsertionPoint": [
                    "foo",
                    "bar"
                  ],
                  "Then": null
                }
              ]
            }
          ]
        }
      ]
    }`)
}

func TestQueryPlanSupportsUnions(t *testing.T) {
	PlanTestFixture4.Check(t, "{ animals { ... on Dog { name } ... on Cat { name }  ... on Snake { name } } }", `
    {
      "RootSteps": [
        {
          "ServiceURL": "A",
          "ParentType": "Query",
          "SelectionSet": "{ animals { ... on Dog { name } ... on Cat { name } ... on Snake { name } _bramble__typename: __typename } }",
          "InsertionPoint": null,
          "Then": null
        }
      ]
    }`)
}

func TestQueryPlanSupportsMutations(t *testing.T) {
	f := &PlanTestFixture{
		Schema: `
		directive @boundary on OBJECT

		interface Node {
			id: ID!
		}

		type Movie implements Node @boundary {
			id: ID!
			title: String
			release: Int
		}

		type Query {
			movie(id: ID!): Movie
		}

		type Mutation {
			updateTitle(id: ID!, title: String): Movie
		}
		`,
		Locations: map[string]string{
			"Movie.title":          "A",
			"Movie.release":        "B",
			"Query.movie":          "A",
			"Mutation.updateTitle": "A",
		},
		IsBoundary: map[string]bool{
			"Movie": true,
		},
	}

	f.Check(t, `mutation { updateTitle(id: "2", title: "New title") { title release }}`, `
	{
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Mutation",
			"SelectionSet": "{ updateTitle(id: \"2\", title: \"New title\") { title _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": [
			  {
				"ServiceURL": "B",
				"ParentType": "Movie",
				"SelectionSet": "{ release _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": [
				  "updateTitle"
				],
				"Then": null
			  }
			]
		  }
		]
	  }
	`)
}

func TestQueryPlanWithPaginatedBoundaryType(t *testing.T) {
	PlanTestFixture5.Check(t, "{ foo { foos { cursor page { id name size } } } }", `
    {
      "RootSteps": [
        {
          "ServiceURL": "A",
          "ParentType": "Query",
          "SelectionSet": "{ foo { foos { cursor page { id name _bramble_id: id _bramble__typename: __typename } } } }",
          "InsertionPoint": null,
          "Then": [
			{
				"ServiceURL": "B",
				"ParentType": "Foo",
				"SelectionSet": "{ size _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": [ "foo", "foos", "page" ],
				"Then": null
			}
		  ]
		}
      ]
    }`)
}

func TestQueryPlanWithNestedNamespaces(t *testing.T) {
	fixture := &PlanTestFixture{
		Schema: `
	directive @boundary on OBJECT
	directive @namespace on OBJECT

	interface Node {
		id: ID!
	}

	type Mutation {
		firstLevel: FirstLevelMovieMutation!
	}

	type FirstLevelMovieMutation @namespace {
		secondLevel: SecondLevelMovieMutation!
	}

	type SecondLevelMovieMutation @namespace {
		movie: Movie!
	}

	type Movie implements Node @boundary {
		id: ID!
		title: String!
		releases: [Release!]!
		compTitles: [CompTitle!]!
	}

	type Release {
		id: ID!
		date: String!
	}

	type CompTitle @boundary {
		id: ID!
		score: Int!
	}
	`,

		Locations: map[string]string{
			"SecondLevelMovieMutation.movie": "A",
			"Movie.title":                    "A",
			"Release.date":                   "A",
			"CompTitle.score":                "B",
		},

		IsBoundary: map[string]bool{
			"Movie":     true,
			"Release":   true,
			"CompTitle": true,
		},
	}

	fixture.Check(t, `
	mutation {
		firstLevel {
			secondLevel {
				movie {
					id
					compTitles {
						id
						score
					}
					releases {
						date
					}
				}
			}
		}
	}
	`, `
	{
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Mutation",
			"SelectionSet": "{ firstLevel { secondLevel { movie { id compTitles { id _bramble_id: id _bramble__typename: __typename } releases { date _bramble_id: id _bramble__typename: __typename } _bramble_id: id _bramble__typename: __typename } } } }",
			"InsertionPoint": null,
			"Then": [
			  {
				"ServiceURL": "B",
				"ParentType": "CompTitle",
				"SelectionSet": "{ score _bramble_id: id _bramble__typename: __typename }",
				"InsertionPoint": [
				  "firstLevel",
				  "secondLevel",
				  "movie",
				  "compTitles"
				],
				"Then": null
			  }
			]
		  }
		]
	  }
	`)
}

func TestPrefersArrayBasedBoundaryLookups(t *testing.T) {
	boundaryFieldMap := make(BoundaryFieldsMap)
	boundaryFieldMap.RegisterField("service-a", "movie", "_movie", "id", true)
	boundaryFieldMap.RegisterField("service-a", "movie", "_movies", "ids", false)

	boundaryField, err := boundaryFieldMap.Field("service-a", "movie")
	require.NoError(t, err)
	require.True(t, boundaryField.Array)
}

func TestQueryPlanNoUnnessecaryID(t *testing.T) {
	PlanTestFixture1.Check(t, "{ movies { title } }", `
	  {
		"RootSteps": [
		  {
			"ServiceURL": "A",
			"ParentType": "Query",
			"SelectionSet": "{ movies { title _bramble_id: id _bramble__typename: __typename } }",
			"InsertionPoint": null,
			"Then": null
		  }
		]
	  }
	`)
}

func TestQueryPlanValidateReservedIdAlias(t *testing.T) {
	PlanTestFixture1.CheckError(t, "{ movies { _bramble_id: title } }")
}

func TestQueryPlanValidateReservedTypenameAlias(t *testing.T) {
	PlanTestFixture1.CheckError(t, "{ movies { _bramble__typename: title } }")
}

func TestExtractSelectionSetNilParentDefinition(t *testing.T) {
	query := `
	query {
		animals {
			... on Dog {
				name
				breed {
					name
					origin
				}
			}
		}
	}`
	PlanTestFixture7.CheckNilPointer(t, query)
}
