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

func TestAllowedFieldsMarshallingOrder(t *testing.T) {
	fields := AllowedFields{
		AllowedSubfields: map[string]AllowedFields{
			"a": {AllowAll: true},
			"b": {AllowAll: true},
			"c": {AllowAll: true},
			"d": {AllowAll: true},
		},
	}

	b1, err := json.Marshal(fields)
	require.NoError(t, err)
	b2, err := json.Marshal(fields)
	require.NoError(t, err)

	assert.Equal(t, b1, b2)
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

	t.Run("fragment spread", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(schema, `query {
			movies {
				... MovieFragment
			}
		}

		fragment MovieFragment on Movie {
			id
			title
			compTitles {
				id
			}
		}
		`)
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
		assert.Len(t, errs, 1)
		expectedQuery := gqlparser.MustLoadQuery(schema, `query {
			movies {
				... MovieFragment
			}
		}

		fragment MovieFragment on Movie {
			id
			title
		}
		`)
		assertSelectionSetsEqual(t, schema, expectedQuery.Operations[0].SelectionSet, query.Operations[0].SelectionSet)
		assertSelectionSetsEqual(t, schema, expectedQuery.Fragments[0].SelectionSet, query.Fragments[0].SelectionSet)
	})

	t.Run("inline fragment", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(schema, `query {
			movies {
				... on Movie {
					id
					title
					compTitles {
						id
					}
				}
			}
		}`)
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
			... on Movie {
				id
				title
			}
		}
		}`), query.Operations[0].SelectionSet)
	})
}

func TestFilterSchema(t *testing.T) {
	schemaStr := `
	scalar Year

	interface Person {
		name: String!
		animals: [Animal!]
	}

	interface Animal { name: String! }

	type Toy {
		name: String!
	}

	type Dog implements Animal {
		name: String!
		toys: [Toy!]
	}

	type Cast implements Person {
		id: ID!
		name: String!
		animals: [Animal!]
	}

	type Director implements Person {
		id: ID!
		name: String!
		movies: [Movie!]
		animals: [Animal!]
	}

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
		someone: Person!
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

	t.Run(`union, allow all`, func(t *testing.T) {
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

	t.Run(`union`, func(t *testing.T) {
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"somethingRandom": {
					AllowedSubfields: map[string]AllowedFields{
						"id": {},
					},
				},
			},
			},
		}
		filteredSchema := perms.FilterSchema(schema)
		assert.Equal(t, loadAndFormatSchema(`
			union MovieOrCinema = Movie | Cinema

			type Cinema {
				id: ID!
			}

			type Movie {
				id: ID!
			}

			type Query {
				somethingRandom: MovieOrCinema!
			}
		`), formatSchema(filteredSchema))
	})

	t.Run(`interface`, func(t *testing.T) {
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"someone": {
					AllowedSubfields: map[string]AllowedFields{
						"name": {},
					},
				},
			},
			},
		}
		filteredSchema := perms.FilterSchema(schema)
		assert.Equal(t, loadAndFormatSchema(`
			interface Person { name: String!  }

			type Cast implements Person {
				name: String!
			}

			type Director implements Person {
				name: String!
			}

			type Query {
				someone: Person!
			}
		`), formatSchema(filteredSchema))
	})

	t.Run(`interface, allow all`, func(t *testing.T) {
		perms := OperationPermissions{
			AllowedRootQueryFields: AllowedFields{AllowedSubfields: map[string]AllowedFields{
				"someone": {
					AllowAll: true,
				},
			},
			},
		}
		filteredSchema := perms.FilterSchema(schema)
		assert.Equal(t, loadAndFormatSchema(`
			scalar Year

			interface Person {
				name: String!
				animals: [Animal!]
			}

			interface Animal { name: String! }

			type Toy {
				name: String!
			}

			type Dog implements Animal {
				name: String!
				toys: [Toy!]
			}

			type Cast implements Person {
				id: ID!
				name: String!
				animals: [Animal!]
			}

			type Director implements Person {
				id: ID!
				name: String!
				movies: [Movie!]
				animals: [Animal!]
			}

			type Movie {
				id: ID!
				title: String
				release: Year
				compTitles: [Movie]
			}

			type Query {
				someone: Person!
			}
		`), formatSchema(filteredSchema))
	})
}

func TestMergePermissions(t *testing.T) {
	permsStr := []string{
		`{ "query": {"movie": ["title"]}, "mutation": {"movie": ["updateTitle"]} }`,
		`{ "query": {"movie": ["releaseDate"]}, "mutation": {"movie": ["delete"]} }`,
		`{ "mutation": {"movie": ["add"]} }`,
	}
	expectedStr := `{
		"query": {"movie": ["title", "releaseDate"]},
		"mutation": {"movie": ["updateTitle", "delete", "add"]},
		"subscription": []
	}`

	var perms []OperationPermissions
	for _, p := range permsStr {
		var perm OperationPermissions
		err := json.Unmarshal([]byte(p), &perm)
		require.NoError(t, err)
		perms = append(perms, perm)
	}

	res := MergePermissions(perms...)
	var expected OperationPermissions
	err := json.Unmarshal([]byte(expectedStr), &expected)
	require.NoError(t, err)
	assert.EqualValues(t, expected, res)
}

func TestMergeAllowedFields(t *testing.T) {
	tts := []struct {
		name     string
		in       []string
		expected string
	}{
		{
			name: "basic merge",
			in: []string{
				`{"movie": ["title", "releaseDate"]}`,
				`{"movie": ["cast", "length"]}`,
			},
			expected: `{"movie": ["title", "releaseDate", "cast", "length"]}`,
		},
		{
			name: "recursive merge",
			in: []string{
				`{"movie": {"cast": ["lastName"] }}`,
				`{"movie": {"title": "*", "cast": ["firstName"]}}`,
			},
			expected: `{"movie": {"title": "*", "cast": ["firstName", "lastName"]}}`,
		},
		{
			name: "allow all takes precedence",
			in: []string{
				`{"movie": {"cast": "*" }}`,
				`{"movie": {"title": "*", "cast": ["firstName"]}}`,
			},
			expected: `{"movie": {"title": "*", "cast": "*" }}`,
		},
		{
			name: "overlapping fields",
			in: []string{
				`{"movie": ["title", "releaseDate"]}`,
				`{"movie": ["title", "releaseDate", "cast", "length"]}`,
			},
			expected: `{"movie": ["title", "releaseDate", "cast", "length"]}`,
		},
	}

	for _, tt := range tts {
		t.Run(tt.name, func(t *testing.T) {
			var perms []AllowedFields
			for _, str := range tt.in {
				var af AllowedFields
				err := json.Unmarshal([]byte(str), &af)
				require.NoError(t, err)
				perms = append(perms, af)
			}

			res := MergeAllowedFields(perms...)

			var expected AllowedFields
			err := json.Unmarshal([]byte(tt.expected), &expected)
			require.NoError(t, err)
			assert.EqualValues(t, expected, res)
		})
	}
}

func strToSelectionSet(schema *ast.Schema, query string) ast.SelectionSet {
	return gqlparser.MustLoadQuery(schema, query).Operations[0].SelectionSet
}

func assertSelectionSetsEqual(t *testing.T, schema *ast.Schema, expected, actual ast.SelectionSet) {
	t.Helper()
	expectedStr := formatSelectionSet(testContextWithoutVariables(nil), schema, expected)
	actualStr := formatSelectionSet(testContextWithoutVariables(nil), schema, actual)

	assert.Equal(t, expectedStr, actualStr)
}
