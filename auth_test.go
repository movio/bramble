package bramble

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

func TestUnmarshalRoles(t *testing.T) {
	var roles = map[string]OperationPermissions{
		"nested_role": {
			AllowedRootQueryFields: AllowedFields{
				AllowedSubfields: map[string]AllowedFields{
					"one": {
						AllowAll: true,
					},
					"two": {
						AllowedSubfields: map[string]AllowedFields{
							"three": {
								AllowAll: true,
							},
						},
					},
				},
			},
			AllowedRootMutationFields: AllowedFields{
				AllowAll: true,
			},
			AllowedRootSubscriptionFields: AllowedFields{
				AllowAll: false,
			},
		},
	}

	var newroles map[string]OperationPermissions
	byts, err := json.Marshal(roles)
	require.NoError(t, err)
	err = json.Unmarshal(byts, &newroles)
	require.NoError(t, err)
	require.Equal(t, roles, newroles)
}

func TestFilterAuthorizedFields(t *testing.T) {
	schemaStr := `
	type Movie {
		id: ID!
		title: String
		compTitles: [Movie]
	}

	type Query {
		movies: [Movie!]
	}
	`

	schema := gqlparser.MustLoadSchema(&ast.Source{Input: schemaStr})

	t.Run("root query authorized", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(schema, `query { movies {
		id
		title
		compTitles {
			id
		}
		} }`)
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowAll: true},
		}
		errs := perms.FilterAuthorizedFields(query.Operations[0])
		assert.Len(t, errs, 0)

		assertSelectionSetsEqual(t, schema, strToSelectionSet(schema, `{
		movies {
			id
			title
			compTitles {
				id
			}
		}
	}`), query.Operations[0].SelectionSet)
	})

	t.Run("partial authorization", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(schema, `query { movies {
		__typename
		id
		title
		compTitles {
			id
		}
		} }`)
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"movies": {
					AllowedSubfields: map[string]AllowedFields{
						"id":    {},
						"title": {},
					},
				},
			},
			},
		}
		errs := perms.FilterAuthorizedFields(query.Operations[0])
		require.Len(t, errs, 1)
		assert.Equal(t, errs[0].Message, "user do not have permission to access field query.movies.compTitles")

		assertSelectionSetsEqual(t, schema, strToSelectionSet(schema, `{
		movies {
			__typename
			id
			title
		}
		}`), query.Operations[0].SelectionSet)
	})

	t.Run("unauthorized", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(schema, `query { movies {
		id
		title
		compTitles {
			id
		}
		} }`)
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{},
		}
		errs := perms.FilterAuthorizedFields(query.Operations[0])
		assert.Len(t, errs, 1)
		assert.Len(t, query.Operations[0].SelectionSet, 0)
	})
}

func TestFilterSchema(t *testing.T) {
	schemaStr := `
	scalar Year

	union MovieOrCinema = Movie | Cinema

	type Cinema {
		id: ID!
		name: String!
	}

	type Movie {
		id: ID!
		title: String
		release: Year
		compTitles: [Movie]
	}

	input SubMovieSearch {
		genre: String
	}

	input MovieSearch {
		title: String
		subfields: SubMovieSearch
	}

	type Query {
		movies: [Movie!]
		movieByTitle(title: String!): Movie
		movieSearch(in: MovieSearch!): [Movie!]
		somethingRandom: MovieOrCinema!
	}
	`

	schema := gqlparser.MustLoadSchema(&ast.Source{Input: schemaStr})

	t.Run("field selection", func(t *testing.T) {
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"movies": {
					AllowedSubfields: map[string]AllowedFields{
						"id":      {},
						"title":   {},
						"release": {},
					},
				},
			},
			},
		}
		filteredSchema := perms.FilterSchema(schema)
		assert.Equal(t, loadAndFormatSchema(`
			scalar Year

			type Movie {
				id: ID!
				title: String
				release: Year
			}

			type Query {
				movies: [Movie!]
			}
		`), formatSchema(filteredSchema))
	})

	t.Run("same type, multiple paths, different allowed fields", func(t *testing.T) {
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"movies": {
					AllowedSubfields: map[string]AllowedFields{
						"release": {},
					},
				},
				"movieByTitle": {
					AllowedSubfields: map[string]AllowedFields{
						"title": {},
					},
				},
			},
			},
		}
		filteredSchema := perms.FilterSchema(schema)
		assert.Equal(t, loadAndFormatSchema(`
			scalar Year

			type Movie {
				release: Year
				title: String
			}

			type Query {
				movies: [Movie!]
				movieByTitle(title: String!): Movie
			}
		`), formatSchema(filteredSchema))
	})

	t.Run("allow all", func(t *testing.T) {
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"movies": {
					AllowAll: true,
				},
			},
			},
		}
		filteredSchema := perms.FilterSchema(schema)
		assert.Equal(t, loadAndFormatSchema(`
			scalar Year

			type Movie {
				id: ID!
				title: String
				release: Year
				compTitles: [Movie]
			}

			type Query {
				movies: [Movie!]
			}
		`), formatSchema(filteredSchema))
	})

	t.Run(`input type`, func(t *testing.T) {
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"movieSearch": {
					AllowAll: true,
				},
			},
			},
		}
		filteredSchema := perms.FilterSchema(schema)
		assert.Equal(t, loadAndFormatSchema(`
			scalar Year

			type Movie {
				id: ID!
				title: String
				release: Year
				compTitles: [Movie]
			}

			input SubMovieSearch {
				genre: String
			}

			input MovieSearch {
				title: String
				subfields: SubMovieSearch
			}

			type Query {
				movieSearch(in: MovieSearch!): [Movie!]
			}
		`), formatSchema(filteredSchema))
	})

	t.Run(`union`, func(t *testing.T) {
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"somethingRandom": {
					AllowAll: true,
				},
			},
			},
		}
		filteredSchema := perms.FilterSchema(schema)
		assert.Equal(t, loadAndFormatSchema(`
			scalar Year

			union MovieOrCinema = Movie | Cinema

			type Cinema {
				id: ID!
				name: String!
			}

			type Movie {
				id: ID!
				title: String
				release: Year
				compTitles: [Movie]
			}

			type Query {
				somethingRandom: MovieOrCinema!
			}
		`), formatSchema(filteredSchema))
	})
}

func strToSelectionSet(schema *ast.Schema, query string) ast.SelectionSet {
	return gqlparser.MustLoadQuery(schema, query).Operations[0].SelectionSet
}

func assertSelectionSetsEqual(t *testing.T, schema *ast.Schema, expected, actual ast.SelectionSet) {
	expectedStr := formatSelectionSet(testContextWithoutVariables(nil), schema, expected)
	actualStr := formatSelectionSet(testContextWithoutVariables(nil), schema, actual)

	assert.Equal(t, expectedStr, actualStr)
}
