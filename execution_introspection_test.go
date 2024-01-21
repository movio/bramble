package bramble

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestIntrospectionQuery(t *testing.T) {
	schema := `
	union MovieOrCinema = Movie | Cinema
	interface Person { name: String! }

	type Cast implements Person {
		name: String!
	}

	"""
	A bit like a film
	"""
	type Movie {
		id: ID!
		title: String @deprecated(reason: "Use something else")
		genres: [MovieGenre!]!
	}

	enum MovieGenre {
		ACTION
		COMEDY
		HORROR @deprecated(reason: "too scary")
		DRAMA
		ANIMATION
		ADVENTURE
		SCIENCE_FICTION
	}

	type Cinema {
		id: ID!
		name: String!
	}

	type Query {
		movie(id: ID!): Movie!
		movies: [Movie!]!
		somethingRandom: MovieOrCinema
		somePerson: Person
	}`

	// Make sure schema merging doesn't break introspection
	mergedSchema, err := MergeSchemas(gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: schema}))
	require.NoError(t, err)

	es := ExecutableSchema{
		tracer:       noop.NewTracerProvider().Tracer("test"),
		MergedSchema: mergedSchema,
	}

	t.Run("basic type fields", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `{
			__type(name: "Movie") {
				kind
				name
				description
			}
		}`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)

		require.JSONEq(t, `
		{
			"__type": {
				"description": "A bit like a film",
				"kind": "OBJECT",
				"name": "Movie"
			}
		}
		`, string(resp.Data))
	})

	t.Run("basic aliased type fields", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `{
			movie: __type(name: "Movie") {
				type: kind
				n: name
				desc: description
			}
		}`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)

		require.JSONEq(t, `
		{
			"movie": {
				"desc": "A bit like a film",
				"type": "OBJECT",
				"n": "Movie"
			}
		}
		`, string(resp.Data))
	})

	t.Run("lists and non-nulls", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `{
		__type(name: "Movie") {
			fields(includeDeprecated: true) {
				name
				isDeprecated
				deprecationReason
				type {
					name
					kind
					ofType {
						name
						kind
						ofType {
							name
							kind
							ofType {
								name
							}
						}
					}
				}
			}
		}
	}`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)
		require.JSONEq(t, `
		{
			"__type": {
				"fields": [
				{
					"deprecationReason": null,
					"isDeprecated": false,
					"name": "id",
					"type": {
					"kind": "NON_NULL",
					"name": null,
					"ofType": {
						"kind": "SCALAR",
						"name": "ID",
						"ofType": null
					}
					}
				},
				{
					"deprecationReason": "Use something else",
					"isDeprecated": true,
					"name": "title",
					"type": {
					"kind": "SCALAR",
					"name": "String",
					"ofType": null
					}
				},
				{
					"deprecationReason": null,
					"isDeprecated": false,
					"name": "genres",
					"type": {
					"kind": "NON_NULL",
					"name": null,
					"ofType": {
						"kind": "LIST",
						"name": null,
						"ofType": {
						"kind": "NON_NULL",
						"name": null,
						"ofType": {
							"name": "MovieGenre"
						}
						}
					}
					}
				}
				]
			}
			}
	`, string(resp.Data))
	})

	t.Run("fragment", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `
		query {
			__type(name: "Movie") {
				...TypeInfo
			}
		}

		fragment TypeInfo on __Type {
			description
			kind
			name
		}
		`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)
		errsJSON, err := json.Marshal(resp.Errors)
		require.NoError(t, err)
		require.Nil(t, resp.Errors, fmt.Sprintf("errors: %s", errsJSON))
		require.JSONEq(t, `
		{
			"__type": {
				"description": "A bit like a film",
				"kind": "OBJECT",
				"name": "Movie"
			}
		}
		`, string(resp.Data))
	})

	t.Run("enum", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `
		{
			__type(name: "MovieGenre") {
				enumValues(includeDeprecated: true) {
					name
					isDeprecated
					deprecationReason
				}
			}
		}
		`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)
		require.JSONEq(t, `
		{
			"__type": {
				"enumValues": [
				{
					"deprecationReason": null,
					"isDeprecated": false,
					"name": "ACTION"
				},
				{
					"deprecationReason": null,
					"isDeprecated": false,
					"name": "COMEDY"
				},
				{
					"deprecationReason": "too scary",
					"isDeprecated": true,
					"name": "HORROR"
				},
				{
					"deprecationReason": null,
					"isDeprecated": false,
					"name": "DRAMA"
				},
				{
					"deprecationReason": null,
					"isDeprecated": false,
					"name": "ANIMATION"
				},
				{
					"deprecationReason": null,
					"isDeprecated": false,
					"name": "ADVENTURE"
				},
				{
					"deprecationReason": null,
					"isDeprecated": false,
					"name": "SCIENCE_FICTION"
				}
				]
			}
			}
		`, string(resp.Data))
	})

	t.Run("union", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `
		{
			__type(name: "MovieOrCinema") {
				possibleTypes {
					name
				}
			}
		}
		`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)
		require.JSONEq(t, `
		{
			"__type": {
				"possibleTypes": [
				{
					"name": "Movie"
				},
				{
					"name": "Cinema"
				}
				]
			}
			}
		`, string(resp.Data))
	})

	t.Run("interface", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `
		{
			__type(name: "Person") {
				possibleTypes {
					name
				}
			}
		}
		`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)
		require.JSONEq(t, `
		{
			"__type": {
				"possibleTypes": [
				{
					"name": "Cast"
				}
				]
			}
		}
		`, string(resp.Data))
	})

	t.Run("type referenced only through an interface", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `{
			__type(name: "Cast") {
				kind
				name
			}
		}`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)

		require.JSONEq(t, `
		{
			"__type": {
				"kind": "OBJECT",
				"name": "Cast"
			}
		}
		`, string(resp.Data))
	})

	t.Run("directive", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `
		{
			__schema {
				directives {
					name
					args {
						name
						type {
							name
						}
					}
				}
			}
		}
		`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)

		// directive order is random so we need to unmarshal and compare the elements
		type expectedType struct {
			Schema struct {
				Directives []struct {
					Name string
					Args []struct {
						Name string
						Type struct {
							Name string
						}
					}
				}
			} `json:"__schema"`
		}

		var actual expectedType
		err := json.Unmarshal([]byte(resp.Data), &actual)
		require.NoError(t, err)
		var expected expectedType
		err = json.Unmarshal([]byte(`
		{
			"__schema": {
			  "directives": [
				{
				  "name": "include",
				  "args": [
					{
					  "name": "if",
					  "type": {
						"name": null
					  }
					}
				  ]
				},
				{
				  "name": "skip",
				  "args": [
					{
					  "name": "if",
					  "type": {
						"name": null
					  }
					}
				  ]
				},
				{
				  "name": "deprecated",
				  "args": [
					{
					  "name": "reason",
					  "type": {
						"name": "String"
					  }
					}
				  ]
				}
			  ]
			}
		  }
		`), &expected)
		require.NoError(t, err)
		require.ElementsMatch(t, expected.Schema.Directives, actual.Schema.Directives)
	})

	t.Run("__schema", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `
		{
			__schema {
				queryType {
					name
				}
				mutationType {
					name
				}
				subscriptionType {
					name
				}
			}
		}
		`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)
		require.JSONEq(t, `
		{
			"__schema": {
				"queryType": {
					"name": "Query"
				},
				"mutationType": null,
				"subscriptionType": null
			}
			}
		`, string(resp.Data))
	})
}
