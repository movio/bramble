package bramble

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
	"github.com/vektah/gqlparser/v2/gqlerror"
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

		assert.JSONEq(t, `
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

		assert.JSONEq(t, `
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
		assert.JSONEq(t, `
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
		assert.JSONEq(t, `
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
		assert.JSONEq(t, `
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
		assert.JSONEq(t, `
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

	t.Run("type referenced only through an interface", func(t *testing.T) {
		query := gqlparser.MustLoadQuery(es.MergedSchema, `{
			__type(name: "Cast") {
				kind
				name
			}
		}`)
		ctx := testContextWithoutVariables(query.Operations[0])
		resp := es.ExecuteQuery(ctx)

		assert.JSONEq(t, `
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
		assert.ElementsMatch(t, expected.Schema.Directives, actual.Schema.Directives)
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
		assert.JSONEq(t, `
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

func TestQueryExecutionWithSingleService(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `type Movie {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie!
				}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1",
								"title": "Test title"
							}
						}
					}`))
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "Test title"
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleServices(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1",
								"title": "Test title"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie @boundary {
					id: ID!
					release: Int
				}

				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"id": "1",
								"release": 2007
							}
						}
					}
					`))
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
				release
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "Test title",
				"release": 2007
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionNamespaceAndFragmentSpread(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `
				directive @namespace on OBJECT
				type Foo {
					id: ID!
				}

				type MyNamespace @namespace {
					foo: Foo!
				}

				type Query {
					ns: MyNamespace!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"ns": {
							"foo": {
								"id": "1"
							}
							}
						}
					}
					`))
				}),
			},
			{
				schema: `
				directive @namespace on OBJECT
				interface Person { name: String! }

				type Movie {
					title: String!
				}

				type Director implements Person {
					name: String!
					movies: [Movie!]
				}

				type MyNamespace @namespace {
					somePerson: Person!
				}

				type Query {
					ns: MyNamespace!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"ns": {
							"somePerson": {
								"name": "Luc Besson",
								"movies": [
									{"title": "The Big Blue"}
								]
							}
							}
						}
					}
					`))
				}),
			},
		},
		query: `{
			ns {
			somePerson {
				... on Director {
					name
					movies {
						title
					}
				}
			}
			foo {
				id
			}
		}
		}`,
		expected: `{
			"ns": {
			"somePerson": {
				"name": "Luc Besson",
				"movies": [
					{"title": "The Big Blue"}
				]
			},
			"foo": {
				"id": "1"
			}
		}
		}`,
	}

	f.run(t)
}

func TestQueryExecutionWithTypename(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `type Movie {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie!
				}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1",
								"title": "Test title",
								"__typename": "Movie"
							}
						}
					}`))
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
				__typename
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "Test title",
				"__typename": "Movie"
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionWithTypenameAndNamespaces(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `
				directive @namespace on OBJECT

				type Movie {
					id: ID!
					title: String!
				}

				type Cinema {
					id: ID!
				}

				type MovieQuery @namespace {
					movies: [Movie!]!
				}

				type CinemaQuery @namespace {
					cinemas: [Cinema!]!
				}

				type Query {
					movie: MovieQuery!
					cinema: CinemaQuery!
				}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"movies": [
									{"__typename": "Movie", "id": "1"}
								]
							}
						}
					}`))
				}),
			},
		},
		query: `{
			__typename
			movie {
				__typename
				movies {
					__typename
					id
				}
			}
			cinema {
				__typename
			}
		}`,
		expected: `{
			"__typename": "Query",
			"movie": {
				"__typename": "MovieQuery",
				"movies": [
					{"__typename": "Movie", "id": "1"}
				]
			},
			"cinema": {
				"__typename": "CinemaQuery"
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionWithMultipleNodeQueries(t *testing.T) {
	schema1 := `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movies: [Movie!]!
				}`
	schema2 := `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie implements Node @boundary {
					id: ID!
					release: Int
				}

				type Query {
					node(id: ID!): Node!
	}`

	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: schema1,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movies": [
							{ "id": "1", "title": "Test title 1" },
							{ "id": "2", "title": "Test title 2" },
							{ "id": "3", "title": "Test title 3" }
							]
						}
					}
					`))
				}),
			},
			{
				schema: schema2,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var q map[string]string
					json.NewDecoder(r.Body).Decode(&q)
					assertQueriesEqual(t, schema2, `{
						_0: node(id: "1") { ... on Movie { _id: id release } }
						_1: node(id: "2") { ... on Movie { _id: id release } }
						_2: node(id: "3") { ... on Movie { _id: id release } }
					}`, q["query"])
					w.Write([]byte(`{
						"data": {
							"_0": { "id": "1", "release": 2007 },
							"_1": { "id": "2", "release": 2008 },
							"_2": { "id": "3", "release": 2009 }
						}
					}
					`))
				}),
			},
		},
		query: `{
			movies {
				id
				title
				release
			}
		}`,
		expected: `{
			"movies": [
				{
					"id": "1",
					"title": "Test title 1",
					"release": 2007
				},
				{
					"id": "2",
					"title": "Test title 2",
					"release": 2008
				},
				{
					"id": "3",
					"title": "Test title 3",
					"release": 2009
				}
			]
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleServicesWithArray(t *testing.T) {
	schema1 := `directive @boundary on OBJECT
	interface Node { id: ID! }

	type Movie implements Node @boundary {
		id: ID!
		title: String
	}

	type Query {
		node(id: ID!): Node
		movie(id: ID!): Movie!
	}`

	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: schema1,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var req map[string]string
					json.NewDecoder(r.Body).Decode(&req)
					query := gqlparser.MustLoadQuery(gqlparser.MustLoadSchema(&ast.Source{Input: schema1}), req["query"])
					var ids []string
					for _, s := range query.Operations[0].SelectionSet {
						ids = append(ids, s.(*ast.Field).Arguments[0].Value.Raw)
					}
					if query.Operations[0].SelectionSet[0].(*ast.Field).Name == "node" {
						var res string
						for i, id := range ids {
							if i != 0 {
								res += ","
							}
							res += fmt.Sprintf(`
								"_%d": {
									"id": "%s",
									"title": "title %s"
								}`, i, id, id)
						}
						w.Write([]byte(fmt.Sprintf(`{ "data": { %s } }`, res)))
					} else {
						w.Write([]byte(fmt.Sprintf(`{
							"data": {
								"movie": {
									"id": "%s",
									"title": "title %s"
								}
							}
						}`, ids[0], ids[0])))
					}
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie implements Node @boundary {
					id: ID!
					compTitles: [Movie]
				}

				type Query {
					node(id: ID!): Node
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"id": "1",
								"compTitles": [
									{
										"id": "2",
										"compTitles": [
											{ "id": "3" },
											{ "id": "4" }
										]
									},
									{
										"id": "3",
										"compTitles": [
											{ "id": "4" },
											{ "id": "5" }
										]
									}
								]
							}
						}
					}
					`))
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
				compTitles {
					id
					title
					compTitles {
						id
						title
					}
				}
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "title 1",
				"compTitles": [
					{
						"id": "2",
						"title": "title 2",
						"compTitles": [
							{
								"id": "3",
								"title": "title 3"
							},
							{
								"id": "4",
								"title": "title 4"
							}
						]
					},
					{
						"id": "3",
						"title": "title 3",
						"compTitles": [
							{
								"id": "4",
								"title": "title 4"
							},
							{
								"id": "5",
								"title": "title 5"
							}
						]
					}
				]
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleServicesWithEmptyArray(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie implements Node @boundary {
					id: ID!
				}

				type Query {
					movies: [Movie!]!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
							"data": {
								"movies": []
							}
						}`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie implements Node @boundary {
					id: ID!
					title: String
				}

				type Query {
					node(id: ID!): Node
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					t.Fatal("service should not be called on empty array")
				}),
			},
		},
		query: `{
			movies {
				id
				title
			}
		}`,
		expected: `{
			"movies": []
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleServicesWithNestedArrays(t *testing.T) {
	schema1 := `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie implements Node @boundary {
					id: ID!
					title: String
				}

				type Query {
					node(id: ID!): Node
					movie(id: ID!): Movie!
			}`
	services := []testService{
		{
			schema: schema1,
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]string
				json.NewDecoder(r.Body).Decode(&req)
				query := gqlparser.MustLoadQuery(gqlparser.MustLoadSchema(&ast.Source{Input: schema1}), req["query"])
				var ids []string
				for _, s := range query.Operations[0].SelectionSet {
					ids = append(ids, s.(*ast.Field).Arguments[0].Value.Raw)
				}
				if query.Operations[0].SelectionSet[0].(*ast.Field).Name == "node" {
					var res string
					for i, id := range ids {
						if i != 0 {
							res += ","
						}
						res += fmt.Sprintf(`
								"_%d": {
									"id": "%s",
									"title": "title %s"
								}`, i, id, id)
					}
					w.Write([]byte(fmt.Sprintf(`{ "data": { %s } }`, res)))
				} else {
					w.Write([]byte(fmt.Sprintf(`{
							"data": {
								"movie": {
									"id": "%s",
									"title": "title %s"
								}
							}
						}`, ids[0], ids[0])))
				}
			}),
		},
		{
			schema: `directive @boundary on OBJECT
			interface Node { id: ID! }

			type Movie implements Node @boundary {
				id: ID!
				compTitles: [[Movie]]
			}

			type Query {
				node(id: ID!): Node
			}`,
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{
					"data": {
						"_0": {
							"id": "1",
							"compTitles": [[
								{
									"id": "2"
								},
								{
									"id": "3"
								}
							]]
						}
					}
				}`))
			}),
		},
	}

	f := &queryExecutionFixture{
		services: services,
		query: `{
		movie(id: "1") {
			id
			title
			compTitles {
				id
				title
			}
		}
	}`,
		expected: `{
		"movie": {
			"id": "1",
			"title": "title 1",
			"compTitles": [[
				{
					"id": "2",
					"title": "title 2"
				},
				{
					"id": "3",
					"title": "title 3"
				}
			]]
		}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionEmptyNodeResponse(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1",
								"title": "Test title"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie @boundary {
					id: ID!
					release: Int
				}

				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": null
						}
					}
					`))
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
				release
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "Test title",
				"release": null
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionWithNullResponse(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
				}

				type Query {
					movies: [Movie!]
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movies": null
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					assert.Fail(t, "handler should not be called")
				}),
			},
		},
		query: `{
			movies {
				id
				title
			}
		}`,
		expected: `{
			"movies": null
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionWithNullResponseAndSubBoundaryType(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					compTitles: [Movie!]
				}

				type Query {
					movies: [Movie!]
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movies": null
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					assert.Fail(t, "handler should not be called")
				}),
			},
		},
		query: `{
			movies {
				id
				title
				compTitles {
					title
				}
			}
		}`,
		expected: `{
			"movies": null
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionWithInputObject(t *testing.T) {
	schema1 := `directive @boundary on OBJECT
		type Movie @boundary {
			id: ID!
			title: String
			otherMovie(arg: MovieInput): Movie
		}

		input MovieInput {
			id: ID!
			title: String
		}

		type Query {
			movie(in: MovieInput): Movie!
	}`
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: schema1,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var q map[string]string
					json.NewDecoder(r.Body).Decode(&q)
					assertQueriesEqual(t, schema1, `{
						movie(in: {id: "1", title: "title"}) {
							id
							title
							otherMovie(arg: {id: "2", title: "another title"}) {
								title
							}
						}
					}`, q["query"])
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1",
								"title": "Test title",
								"otherMovie": {
									"title": "another title"
								}
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie @boundary {
					id: ID!
					release: Int
				}

				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"id": "1",
								"release": 2007
							}
						}
					}
					`))
				}),
			},
		},
		query: `{
			movie(in: {id: "1", title: "title"}) {
				id
				title
				release
				otherMovie(arg: {id: "2", title: "another title"}) {
					title
				}
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "Test title",
				"release": 2007,
				"otherMovie": {
					"title": "another title"
				}
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleObjects(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1",
								"title": "Test title"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }

				type Movie @boundary {
					id: ID!
					release: Int
				}

				type Query {
					node(id: ID!): Node!
					movies: [Movie!]
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := ioutil.ReadAll(r.Body)
					if strings.Contains(string(body), "movies") {
						w.Write([]byte(`{
							"data": {
								"movies": [
									{ "id": "1", "release": 2007 },
									{ "id": "2", "release": 2018 }
								]
							}
						}
						`))
					} else {
						w.Write([]byte(`{
							"data": {
								"_0": {
									"id": "1",
									"release": 2007
								}
							}
						}
						`))
					}
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
				release
			}

			movies {
				id
				release
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "Test title",
				"release": 2007
			},
			"movies": [
				{"id": "1", "release": 2007},
				{"id": "2", "release": 2018}
			]
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleServicesWithSkipTrueDirectives(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}
				type Query {
					movie(id: ID!): Movie!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }
				type Gizmo {
					foo: String!
					bar: String!
				}
				type Movie @boundary {
					id: ID!
					gizmo: Gizmo
				}
				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					panic("should not be called")
				}),
			},
		},
		query: `query q($skipTitle: Boolean!, $skipGizmo: Boolean!) {
			movie(id: "1") {
				id
				title @skip(if: $skipTitle)
				gizmo @skip(if: $skipGizmo) {
					foo
					bar
				}
			}
		}`,
		variables: map[string]interface{}{
			"skipTitle": true,
			"skipGizmo": true,
		},
		expected: `{
			"movie": {
				"id": "1"
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleServicesWithSkipFalseDirectives(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}
				type Query {
					movie(id: ID!): Movie!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }
				type Gizmo {
					foo: String!
					bar: String!
				}
				type Movie @boundary {
					id: ID!
					gizmo: Gizmo
				}
				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"id": "1",
								"title": "no soup for you",
								"gizmo": {
									"foo": "a foo",
									"bar": "a bar"
								}
							}
						}
					}
					`))
				}),
			},
		},
		query: `query q($skipTitle: Boolean!, $skipGizmo: Boolean!) {
			movie(id: "1") {
				id
				title @skip(if: $skipTitle)
				gizmo @skip(if: $skipGizmo) {
					foo
					bar
				}
			}
		}`,
		variables: map[string]interface{}{
			"skipTitle": false,
			"skipGizmo": false,
		},
		expected: `{
			"movie": {
				"id": "1",
				"title": "no soup for you",
				"gizmo": {
					"foo": "a foo",
					"bar": "a bar"
				}
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleServicesWithIncludeFalseDirectives(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}
				type Query {
					movie(id: ID!): Movie!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }
				type Gizmo {
					foo: String!
					bar: String!
				}
				type Movie @boundary {
					id: ID!
					gizmo: Gizmo
				}
				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					panic("should not be called")
				}),
			},
		},
		query: `query q($includeTitle: Boolean!, $includeGizmo: Boolean!) {
			movie(id: "1") {
				id
				title @include(if: $includeTitle)
				gizmo @include(if: $includeGizmo) {
					foo
					bar
				}
			}
		}`,
		variables: map[string]interface{}{
			"includeTitle": false,
			"includeGizmo": false,
		},
		expected: `{
			"movie": {
				"id": "1"
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionMultipleServicesWithIncludeTrueDirectives(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}
				type Query {
					movie(id: ID!): Movie!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }
				type Gizmo {
					foo: String!
					bar: String!
				}
				type Movie @boundary {
					id: ID!
					gizmo: Gizmo
				}
				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"id": "1",
								"title": "yada yada yada",
								"gizmo": {
									"foo": "a foo",
									"bar": "a bar"
								}
							}
						}
					}
					`))
				}),
			},
		},
		query: `query q($includeTitle: Boolean!, $includeGizmo: Boolean!) {
			movie(id: "1") {
				id
				title @include(if: $includeTitle)
				gizmo @include(if: $includeGizmo) {
					foo
					bar
				}
			}
		}`,
		variables: map[string]interface{}{
			"includeTitle": true,
			"includeGizmo": true,
		},
		expected: `{
			"movie": {
				"id": "1",
				"title": "yada yada yada",
				"gizmo": {
					"foo": "a foo",
					"bar": "a bar"
				}
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestMutationExecution(t *testing.T) {
	schema1 := `directive @boundary on OBJECT
				type Movie @boundary {
					id: ID!
					title: String
				}
				type Query {
					movie(id: ID!): Movie!
				}
				type Mutation {
					updateTitle(id: ID!, title: String): Movie
				}`
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: schema1,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var q map[string]string
					json.NewDecoder(r.Body).Decode(&q)
					assertQueriesEqual(t, schema1, `mutation { updateTitle(id: "2", title: "New title") { _id: id title } }`, q["query"])

					w.Write([]byte(`{
						"data": {
							"updateTitle": {
								"id": "2",
								"title": "New title"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT
				interface Node { id: ID! }
				type Movie @boundary {
					id: ID!
					release: Int
				}
				type Query {
					node(id: ID!): Node!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"id": "2",
								"release": 2007
							}
						}
					}
					`))
				}),
			},
		},
		query: `mutation {
			updateTitle(id: "2", title: "New title") {
				title
				release
			}
		}`,
		expected: `{
			"updateTitle": {
				"title": "New title",
				"release": 2007
			}
		}`,
	}

	f.checkSuccess(t)
}
func TestQueryExecutionWithUnions(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `
				interface Node { id: ID! }
				directive @boundary on OBJECT

				type Dog { name: String! age: Int }
				type Cat { name: String! age: Int }
				type Snake { name: String! age: Int }
				union Animal = Dog | Cat | Snake

				type Person @boundary {
					id: ID!
					pet: Animal
				}

				type Query {
					node(id: ID!): Node
					animals: [Animal]!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := ioutil.ReadAll(r.Body)
					if strings.Contains(string(b), "animals") {
						w.Write([]byte(`{
							"data": {
								"foo": [
									{ "name": "fido", "age": 4 },
									{ "name": "felix", "age": 2 },
									{ "age": 20, "name": "ka" }
								]
							}
						}
						`))
					} else {
						w.Write([]byte(`{
							"data": {
								"_0": {
									"_id": "2",
									"pet": {
										"name": "felix",
										"age": 2
									}
								}
							}
						}
						`))
					}
				}),
			},
			{
				schema: `directive @boundary on OBJECT

				type Person @boundary {
					id: ID!
					name: String!
				}

				type Query {
					person(id: ID!): Person
				}`,

				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"person": {
								"_id": "2",
								"name": "Bob"
							}
						}
					}
					`))
				}),
			},
		},
		query: `{
			person(id: "2") {
				name
				pet {
					... on Dog { name, age }
					... on Cat { name, age }
					... on Snake { age, name }
				}
			}
			foo: animals {
				... on Dog { name, age }
				... on Cat { name, age }
				... on Snake { age, name }
			}
		}`,
		expected: `{
			"person": {
				"name": "Bob",
				"pet": {
					"name": "felix",
					"age": 2
				}
			},
			"foo": [
				{ "name": "fido", "age": 4 },
				{ "name": "felix", "age": 2 },
				{ "age": 20, "name": "ka" }
			]
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryExecutionWithNamespaces(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `
					directive @boundary on OBJECT
					directive @namespace on OBJECT
					interface Node { id: ID! }

					type Cat implements Node @boundary {
						id: ID!
						name: String!
					}

					type AnimalsQuery @namespace {
						cats: CatsQuery!
					}

					type CatsQuery @namespace {
						allCats: [Cat!]!
					}

					type Query {
						animals: AnimalsQuery!
						node(id: ID!): Node!
					}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := ioutil.ReadAll(r.Body)

					if strings.Contains(string(b), "node") {
						w.Write([]byte(`{
							"data": {
								"_0": {
									"name": "Felix"
								}
							}
						}
						`))
					} else {
						w.Write([]byte(`{
							"data": {
								"animals": {
									"cats": {
										"allCats": [
											{ "name": "Felix" },
											{ "name": "Tigrou" }
										]
									}
								}
							}
						}
						`))
					}
				}),
			},
			{
				schema: `
					directive @boundary on OBJECT
					directive @namespace on OBJECT
					interface Node { id: ID! }

					type Cat implements Node @boundary {
						id: ID!
					}

					type AnimalsQuery @namespace {
						species: [String!]!
						cats: CatsQuery!
					}

					type CatsQuery @namespace {
						searchCat(name: String!): Cat
					}

					type Query {
						animals: AnimalsQuery!
					}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"animals": {
								"cats": {
									"searchCat": {
										"id": "CA7"
									}
								}
							}
						}
					}
					`))
				}),
			},
		},
		query: `{ animals {
			cats {
				allCats {
					name
				}
				searchCat(name: "Felix") {
					id
					name
				}
			}
		}}`,
		expected: `{
			"animals": {
				"cats": {
					"allCats": [
						{ "name": "Felix" },
						{ "name": "Tigrou" }
					],
					"searchCat": { "id": "CA7", "name": "Felix" }
				}
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestDebugExtensions(t *testing.T) {
	called := false
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `
				type Query {
					q(id: ID!): String!
				}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.Write([]byte(`{
						"data": {
							"q": "hi"
						}
					}`))
				}),
			},
		},
		debug: &DebugInfo{
			Variables: true,
			Query:     true,
			Plan:      true,
		},
		query: `{
			q(id: "1")
		}`,
		expected: `{
			"q": "hi"
		}`,
	}

	f.checkSuccess(t)
	assert.True(t, called)
	assert.NotNil(t, f.resp.Extensions["variables"])
}

func TestQueryWithBoundaryFields(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie
					_movie(id: ID!): Movie @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movie": {
								"id": "1",
								"title": "Test title"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					release: Int
				}

				type Query {
					movie(id: ID!): Movie @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"id": "1",
								"release": 2007
							}
						}
					}
					`))
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
				release
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "Test title",
				"release": 2007
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryWithArrayBoundaryFields(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					randomMovies: [Movie!]!
					movie(id: ID!): Movie @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"randomMovies": [
								{
									"id": "1",
									"title": "Movie 1"
								},
								{
									"id": "2",
									"title": "Movie 2"
								},
								{
									"id": "3",
									"title": "Movie 3"
								}
							]
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					release: Int
				}

				type Query {
					movies(ids: [ID!]): [Movie]! @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_result": [
								{
									"id": "1",
									"release": 2007
								},
								{
									"id": "2",
									"release": 2008
								},
								{
									"id": "3",
									"release": 2009
								}
							]
						}
					}
					`))
				}),
			},
		},
		query: `{
			randomMovies {
				id
				title
				release
			}
		}`,
		expected: `{
			"randomMovies": [
				{
					"id": "1",
					"title": "Movie 1",
					"release": 2007
				},
				{
					"id": "2",
					"title": "Movie 2",
					"release": 2008
				},
				{
					"id": "3",
					"title": "Movie 3",
					"release": 2009
				}
			]
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryWithArrayBoundaryFieldsAndMultipleChildrenSteps(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					randomMovie: Movie!
					movies(ids: [ID!]!): [Movie]! @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := io.ReadAll(r.Body)
					if strings.Contains(string(b), "randomMovie") {
						w.Write([]byte(`{
						"data": {
							"randomMovie": {
									"id": "1",
									"title": "Movie 1"
							}
						}
					}
					`))
					} else {
						w.Write([]byte(`{
						"data": {
							"_result": [
								{ "id": 2, "title": "Movie 2" },
								{ "id": 3, "title": "Movie 3" },
								{ "id": 4, "title": "Movie 4" }
							]
						}
					}
					`))
					}
				}),
			},
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					compTitles: [Movie!]!
				}

				type Query {
					movies(ids: [ID!]): [Movie]! @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_result": [
								{
									"_id": "1",
									"compTitles": [
										{"id": "2"},
										{"id": "3"},
										{"id": "4"}
									]
								}
							]
						}
					}
					`))
				}),
			},
		},
		query: `{
			randomMovie {
				id
				title
				compTitles {
					id
					title
				}
			}
		}`,
		expected: `{
			"randomMovie":
				{
					"id": "1",
					"title": "Movie 1",
					"compTitles": [
						{ "id": 2, "title": "Movie 2" },
						{ "id": 3, "title": "Movie 3" },
						{ "id": 4, "title": "Movie 4" }
					]
				}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryError(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `type Movie {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie!
				}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"errors": [
							{
								"message": "Movie does not exist",
								"path": ["movie"],
								"extensions": {
									"code": "NOT_FOUND"
								}
							}
						]
					}`))
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
			}
		}`,
		errors: gqlerror.List{
			&gqlerror.Error{
				Message: "Movie does not exist",
				Path:    ast.Path{ast.PathName("movie")},
				Locations: []gqlerror.Location{
					{Line: 2, Column: 4},
				},
				Extensions: map[string]interface{}{
					"code":         "NOT_FOUND",
					"selectionSet": `{ movie(id: "1") { id title } }`,
					"serviceName":  "",
				},
			},
			&gqlerror.Error{
				Message: `got a null response for non-nullable field "movie"`,
			},
		},
	}

	f.run(t)
}

type testService struct {
	schema  string
	handler http.Handler
}

type queryExecutionFixture struct {
	services  []testService
	variables map[string]interface{}
	query     string
	expected  string
	resp      *graphql.Response
	debug     *DebugInfo
	errors    gqlerror.List
}

func (f *queryExecutionFixture) checkSuccess(t *testing.T) {
	f.run(t)

	assert.Empty(t, f.resp.Errors)
	jsonEqWithOrder(t, f.expected, string(f.resp.Data))
}

func (f *queryExecutionFixture) run(t *testing.T) {
	var services []*Service
	var schemas []*ast.Schema

	for _, s := range f.services {
		serv := httptest.NewServer(s.handler)
		defer serv.Close()

		schema := gqlparser.MustLoadSchema(&ast.Source{Input: s.schema})
		services = append(services, &Service{
			ServiceURL: serv.URL,
			Schema:     schema,
		})

		schemas = append(schemas, schema)
	}

	merged, err := MergeSchemas(schemas...)
	require.NoError(t, err)

	es := newExecutableSchema(nil, 50, nil, services...)
	es.MergedSchema = merged
	es.BoundaryQueries = buildBoundaryQueriesMap(services...)
	es.Locations = buildFieldURLMap(services...)
	es.IsBoundary = buildIsBoundaryMap(services...)
	query := gqlparser.MustLoadQuery(merged, f.query)
	vars := f.variables
	if vars == nil {
		vars = map[string]interface{}{}
	}
	ctx := testContextWithVariables(vars, query.Operations[0])
	if f.debug != nil {
		ctx = context.WithValue(ctx, DebugKey, *f.debug)
	}
	f.resp = es.ExecuteQuery(ctx)
	f.resp.Extensions = graphql.GetExtensions(ctx)

	if len(f.errors) == 0 {
		assert.Empty(t, f.resp.Errors)
		jsonEqWithOrder(t, f.expected, string(f.resp.Data))
	} else {
		require.Equal(t, len(f.errors), len(f.resp.Errors))
		for i := range f.errors {
			delete(f.resp.Errors[i].Extensions, "serviceUrl")
			assert.Equal(t, *f.errors[i], *f.resp.Errors[i])
		}
	}
}

// jsonEqWithOrder checks that the JSON are equals, including the order of the
// fields
func jsonEqWithOrder(t *testing.T, expected, actual string) {
	d1 := json.NewDecoder(bytes.NewBufferString(expected))
	d2 := json.NewDecoder(bytes.NewBufferString(actual))

	if !assert.JSONEq(t, expected, actual) {
		return
	}

	for {
		t1, err1 := d1.Token()
		t2, err2 := d2.Token()

		if err1 != nil && err1 == err2 && err1 == io.EOF {
			if err1 == io.EOF && err1 == err2 {
				return
			}

			t.Errorf("error comparing JSONs: %s, %s", err1, err2)
			return
		}

		if t1 != t2 {
			t.Errorf("fields order is not equal, first differing fields are %q and %q\n", t1, t2)
			return
		}
	}
}

func assertQueriesEqual(t *testing.T, schema, expected, actual string) bool {
	s := gqlparser.MustLoadSchema(&ast.Source{Input: schema})

	var expectedBuf bytes.Buffer
	formatter.NewFormatter(&expectedBuf).FormatQueryDocument(gqlparser.MustLoadQuery(s, expected))
	var actualBuf bytes.Buffer
	formatter.NewFormatter(&actualBuf).FormatQueryDocument(gqlparser.MustLoadQuery(s, actual))

	return assert.Equal(t, expectedBuf.String(), actualBuf.String(), "queries are not equal")
}

func testContextWithoutVariables(op *ast.OperationDefinition) context.Context {
	return AddPermissionsToContext(graphql.WithOperationContext(context.Background(), &graphql.OperationContext{
		Variables: map[string]interface{}{},
		Operation: op,
	}), OperationPermissions{
		AllowedRootQueryFields:        AllowedFields{AllowAll: true},
		AllowedRootMutationFields:     AllowedFields{AllowAll: true},
		AllowedRootSubscriptionFields: AllowedFields{AllowAll: true},
	})
}

func testContextWithVariables(vars map[string]interface{}, op *ast.OperationDefinition) context.Context {
	return AddPermissionsToContext(graphql.WithResponseContext(graphql.WithOperationContext(context.Background(), &graphql.OperationContext{
		Variables: vars,
		Operation: op,
	}), graphql.DefaultErrorPresenter, graphql.DefaultRecover), OperationPermissions{
		AllowedRootQueryFields:        AllowedFields{AllowAll: true},
		AllowedRootMutationFields:     AllowedFields{AllowAll: true},
		AllowedRootSubscriptionFields: AllowedFields{AllowAll: true},
	})
}
