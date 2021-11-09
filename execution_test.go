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

func TestQueryWithNamespace(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `
				directive @namespace on OBJECT

				type NamespacedMovie {
					id: ID!
					title: String
				}

				type NamespaceQuery @namespace {
					movie(id: ID!): NamespacedMovie!
				}

				type Query {
					namespace: NamespaceQuery!
				}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"namespace": {
								"movie": {
									"id": "1",
									"title": "Test title"
								}
							}
						}
					}`))
				}),
			},
		},
		query: `{
			namespace {
				movie(id: "1") {
					id
					title
				}
				__typename
			}
		}`,
		expected: `{
			"namespace": {
				"movie": {
					"id": "1",
					"title": "Test title"
				},
				"__typename": "NamespaceQuery"
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
				Path:    ast.Path{ast.PathName("movie")},
			},
		},
	}

	f.run(t)
}

func TestFederatedQueryFragmentSpreads(t *testing.T) {
	serviceA := testService{
		schema: `
		directive @boundary on OBJECT
		interface Snapshot {
			id: ID!
			name: String!
		}

		type Gizmo @boundary {
			id: ID!
		}

		type Gadget @boundary {
			id: ID!
		}

		type GizmoImplementation implements Snapshot {
			id: ID!
			name: String!
			gizmos: [Gizmo!]!
		}

		type GadgetImplementation implements Snapshot {
			id: ID!
			name: String!
			gadgets: [Gadget!]!
		}

		type Query {
			snapshot(id: ID!): Snapshot!
		}`,
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "GIZMO1") {
				w.Write([]byte(`
				{
					"data": {
						"snapshot": {
							"id": "100",
							"name": "foo",
							"gizmos": [{ "_bramble_id": "GIZMO1", "id": "GIZMO1" }],
							"_bramble__typename": "GizmoImplementation"
						}
					}
				}`))
			} else {
				w.Write([]byte(`
				{
					"data": {
						"snapshot": {
							"id": "100",
							"name": "foo",
							"gadgets": [{ "_bramble_id": "GADGET1", "id": "GADGET1" }],
							"_bramble__typename": "GadgetImplementation"
						}
					}
				}`))

			}
		}),
	}

	serviceB := testService{
		schema: `
		directive @boundary on OBJECT | FIELD_DEFINITION
		type Gizmo @boundary {
			id: ID!
			name: String!
		}

		type Agent {
			name: String!
			country: String!
		}

		type Gadget @boundary {
			id: ID!
			name: String!
			agents: [Agent!]!
		}

		type Query {
			gizmo(id: ID!): Gizmo @boundary
			gadgets(id: [ID!]!): [Gadget!]! @boundary
		}`,
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "GIZMO1") {
				w.Write([]byte(`
				{
					"data": {
						"_0": {
							"_bramble_id": "GIZMO1",
							"id": "GIZMO1",
							"name": "Gizmo #1"
						}
					}
				}`))
			} else {
				w.Write([]byte(`
				{
					"data": {
						"_result": [
							{
								"_bramble_id": "GADGET1",
								"id": "GADGET1",
								"name": "Gadget #1",
								"agents": [
									{
										"name": "James Bond",
										"country": "UK",
										"_bramble__typename": "Agent"
									}
								]
							}
						]
					}
				}`))
			}
		}),
	}

	t.Run("with inline fragment spread", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query Foo {
				snapshot(id: "GIZMO1") {
					id
					name
					... on GizmoImplementation {
						gizmos {
							id
							name
						}
					}
				}
			}`,
			expected: `
			{
				"snapshot": {
					"id": "100",
					"name": "foo",
					"gizmos": [{ "id": "GIZMO1", "name": "Gizmo #1" }]
				}
			}`,
		}

		f.checkSuccess(t)
	})

	t.Run("with overlap in field and fragment selection", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query Foo {
				snapshot(id: "GIZMO1") {
					id
					name
					... on GizmoImplementation {
						id
						name
						gizmos {
							id
							name
						}
					}
				}
			}`,
			expected: `
			{
				"snapshot": {
					"id": "100",
					"name": "foo",
					"gizmos": [{ "id": "GIZMO1", "name": "Gizmo #1" }]
				}
			}`,
		}

		f.checkSuccess(t)
	})

	t.Run("with non abstract fragment", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query Foo {
				snapshot(id: "GIZMO1") {
					... on Snapshot {
						name
					}
				}
			}`,
			expected: `
			{
				"snapshot": {
					"name": "foo"
				}
			}`,
		}

		f.checkSuccess(t)
	})

	t.Run("with named fragment spread", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query Foo {
				snapshot(id: "GIZMO1") {
					id
					name
					... NamedFragment
				}
			}

			fragment NamedFragment on GizmoImplementation {
				gizmos {
					id
					name
				}
			}`,
			expected: `
			{
				"snapshot": {
					"id": "100",
					"name": "foo",
					"gizmos": [{ "id": "GIZMO1", "name": "Gizmo #1" }]
				}
			}`,
		}

		f.checkSuccess(t)
	})

	t.Run("with nested fragment spread", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query Foo {
				snapshot(id: "GIZMO1") {
					... NamedFragment
				}
			}

			fragment NamedFragment on Snapshot {
				id
				name
				... on GizmoImplementation {
					gizmos {
						id
						name
				  	}
				}
			}`,
			expected: `
			{
				"snapshot": {
					"id": "100",
					"name": "foo",
					"gizmos": [{ "id": "GIZMO1", "name": "Gizmo #1" }]
				}
			}`,
		}

		f.checkSuccess(t)
	})

	t.Run("with multiple implementation fragment spreads (gizmo implementation)", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query {
				snapshot(id: "GIZMO1") {
					id
					... NamedFragment
				}
			}

			fragment NamedFragment on Snapshot {
				name
				... on GizmoImplementation {
					gizmos {
						id
						name
				  	}
				}
				... on GadgetImplementation {
					gadgets {
						id
						name
				  	}
				}
			}`,
			expected: `
			{
				"snapshot": {
					"id": "100",
					"name": "foo",
					"gizmos": [{ "id": "GIZMO1", "name": "Gizmo #1" }]
				}
			}`,
		}

		f.checkSuccess(t)
	})

	t.Run("with multiple implementation fragment spreads (gadget implementation)", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query Foo {
				snapshot(id: "GADGET1") {
					... NamedFragment
				}
			}

			fragment GadgetFragment on GadgetImplementation {
				gadgets {
					id
					name
					agents {
						name
						... on Agent {
							country
						}
					}
				}
			}

			fragment NamedFragment on Snapshot {
				id
				name
				... on GizmoImplementation {
					gizmos {
						id
						name
				  	}
				}
				... GadgetFragment
			}`,
			expected: `
			{
				"snapshot": {
					"id": "100",
					"name": "foo",
					"gadgets": [
						{
							"id": "GADGET1",
							"name": "Gadget #1",
							"agents": [
								{"name": "James Bond", "country": "UK"}
							]
						}
					]
				}
			}`,
		}

		f.checkSuccess(t)
	})

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
								"_bramble_id": "1",
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
					movie(id: ID!): Movie! @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"_bramble_id": "1",
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
									],
									"_bramble__typename": "Director"
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
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie! @boundary
				}`,
				handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					require.Fail(t, "handler should not be called")
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
									"_bramble_id": "1",
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
								{ "_bramble_id": "2", "id": "2", "title": "Movie 2" },
								{ "_bramble_id": "3", "id": "3", "title": "Movie 3" },
								{ "_bramble_id": "4", "id": "4", "title": "Movie 4" }
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
									"_bramble_id": "1",
									"compTitles": [
										{"_bramble_id": "2", "id": "2"},
										{"_bramble_id": "3", "id": "3"},
										{"_bramble_id": "4", "id": "4"}
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
						{ "id": "2", "title": "Movie 2" },
						{ "id": "3", "title": "Movie 3" },
						{ "id": "4", "title": "Movie 4" }
					]
				}
		}`,
	}

	f.checkSuccess(t)
}

func TestQueryWithBoundaryFieldsAndNullsAboveInsertionPoint(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION
				directive @namespace on OBJECT

				type Movie @boundary {
					id: ID!
					title: String
					director: Person
				}

				type Person @boundary {
					id: ID!
				}

				type Namespace @namespace {
					movies: [Movie!]!
				}

				type Query {
					ns: Namespace!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					response := jsonToInterfaceMap(`{
						"data": {
							"ns": {
								"movies": [
									{
										"_bramble_id": "MOVIE1",
										"id": "MOVIE1",
										"title": "Movie #1",
										"director": { "_bramble_id": "DIRECTOR1", "id": "DIRECTOR1" }
									},
									{
										"_bramble_id": "MOVIE2",
										"id": "MOVIE2",
										"title": "Movie #2",
										"director": null
									}
								]
							}
						}
					}
					`)
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Error(err)
					}
				}),
			},
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Person @boundary {
					id: ID!
					name: String!
				}

				type Query {
					person(id: ID!): Person @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
							"data": {
								"_0": {
									"_bramble_id": "DIRECTOR1",
									"name": "David Fincher"
								}
							}
						}`))
				}),
			},
		},
		query: `{
			ns {
				movies {
					id
					title
					director {
						id
						name
					}
				}
			}
		}`,
		expected: `{
			"ns": {
				"movies": [
					{
						"id": "MOVIE1",
						"title": "Movie #1",
						"director": {
							"id": "DIRECTOR1",
							"name": "David Fincher"
						}
					},
					{
						"id": "MOVIE2",
						"title": "Movie #2",
						"director": null
					}
				]
			}
		}`,
	}

	f.checkSuccess(t)
}

func TestExtractBoundaryIDs(t *testing.T) {
	dataJSON := `{
		"gizmos": [
			{
				"_bramble_id": "1",
				"name": "Gizmo 1",
				"owner": {
					"_bramble_id": "1"
				}
			},
			{
				"_bramble_id": "2",
				"name": "Gizmo 2",
				"owner": {
					"_bramble_id": "1"
				}
			},
			{
				"_bramble_id": "3",
				"name": "Gizmo 3",
				"owner": {
					"_bramble_id": "2"
				}
			},
			{
				"_bramble_id": "4",
				"name": "Gizmo 4",
				"owner": {
					"_bramble_id": "5"
				}
			}
		]
	}`
	data := map[string]interface{}{}
	expected := []string{"1", "1", "2", "5"}
	insertionPoint := []string{"gizmos", "owner"}
	require.NoError(t, json.Unmarshal([]byte(dataJSON), &data))
	result, err := extractBoundaryIDs(data, insertionPoint)
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
	boundaryField := BoundaryField{Field: "getOwners", Array: true}
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
	expected := []string{`{ _result: getOwners(ids: ["1", "2", "3"]) { _bramble_id: id name } }`}
	ctx := testContextWithoutVariables(nil)
	docs, err := buildBoundaryQueryDocuments(ctx, schema, step, ids, boundaryField, 1)
	require.NoError(t, err)
	require.Equal(t, expected, docs)
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
	boundaryField := BoundaryField{Field: "getOwner", Array: false}
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
	expected := []string{`{ _0: getOwner(id: "1") { _bramble_id: id name } _1: getOwner(id: "2") { _bramble_id: id name } _2: getOwner(id: "3") { _bramble_id: id name } }`}
	ctx := testContextWithoutVariables(nil)
	docs, err := buildBoundaryQueryDocuments(ctx, schema, step, ids, boundaryField, 10)
	require.NoError(t, err)
	require.Equal(t, expected, docs)
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
	boundaryField := BoundaryField{Field: "getOwner", Array: false}
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
	expected := []string{`{ _0: getOwner(id: "1") { _bramble_id: id name } _1: getOwner(id: "2") { _bramble_id: id name } }`, `{ _2: getOwner(id: "3") { _bramble_id: id name } }`}
	ctx := testContextWithoutVariables(nil)
	docs, err := buildBoundaryQueryDocuments(ctx, schema, step, ids, boundaryField, 2)
	require.NoError(t, err)
	require.Equal(t, expected, docs)
}

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
				"gadgets": [
					{"_bramble_id": "GADGET1", "owner": { "_bramble_id": "OWNER1" }},
					{"_bramble_id": "GADGET3", "owner": { "_bramble_id": "OWNER3" }},
					{"_bramble_id": "GADGET2", "owner": null}
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
						"owner": {
							"_bramble_id": "OWNER1",
							"name": "008"
						}
					},
					{
						"_bramble_id": "GADGET3",
						"owner": {
							"_bramble_id": "OWNER3"
						}
					},
					{
						"_bramble_id": "GADGET2",
						"owner": null
					}
				],
				"_bramble_id": "1"
			}
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})

	t.Run("merges nested mid-level array", func(t *testing.T) {
		inputMapA := jsonToInterfaceMap(`{
			"gizmo": {
				"_bramble_id": "1",
				"gadgets": [
					[
						{"_bramble_id": "GADGET1", "owner": { "_bramble_id": "OWNER1" }},
						{"_bramble_id": "GADGET3", "owner": { "_bramble_id": "OWNER3" }}
					],
					[
						{"_bramble_id": "GADGET2", "owner": null}
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
							"owner": {
								"_bramble_id": "OWNER1",
								"name": "008"
							}
						},
						{
							"_bramble_id": "GADGET3",
							"owner": {
								"_bramble_id": "OWNER3"
							}
						}
					],
					[
						{
							"_bramble_id": "GADGET2",
							"owner": null
						}
					]
				],
				"_bramble_id": "1"
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
					"_bramble_id": "1"
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
						"_bramble_id": "4"
					}
				},
				{
					"id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5"
					}
				},
				{
					"id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6"
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
				"name": "Owner A"
			},
			{
				"_bramble_id": "5",
				"name": "Owner B"
			},
			{
				"_bramble_id": "6",
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
						"name": "Owner A"
					}
				},
				{
					"id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"name": "Owner B"
					}
				},
				{
					"id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
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
						"_bramble_id": "4"
					}
				},
				{
					"id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5"
					}
				},
				{
					"id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6"
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
				"name": "Owner A"
			},
			{
				"_bramble_id": "5",
				"name": "Owner B"
			},
			{
				"_bramble_id": "6",
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
						"name": "Owner A"
					}
				},
				{
					"id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"name": "Owner B"
					}
				},
				{
					"id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
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
					"color": "RED",
					"owner": {
						"_bramble_id": "4"
					}
				},
				{
					"_bramble_id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5"
					}
				},
				{
					"_bramble_id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6"
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
				"name": "Owner A"
			},
			{
				"_bramble_id": "5",
				"name": "Owner B"
			},
			{
				"_bramble_id": "6",
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
					"color": "RED",
					"owner": {
						"_bramble_id": "4",
						"name": "Owner A"
					}
				},
				{
					"_bramble_id": "2",
					"color": "GREEN",
					"owner": {
						"_bramble_id": "5",
						"name": "Owner B"
					}
				},
				{
					"_bramble_id": "3",
					"color": "BLUE",
					"owner": {
						"_bramble_id": "6",
						"name": "Owner C"
					}
				}
			]
		}`)

		require.NoError(t, err)
		require.Equal(t, expected, mergedMap)
	})
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

		filtered, err := unionAndTrimSelectionSet("", schema, selectionSet)
		require.NoError(t, err)
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

		filtered, err := unionAndTrimSelectionSet("", schema, selectionSet)
		require.NoError(t, err)
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

		filtered, err := unionAndTrimSelectionSet("GizmoImplementation", schema, initialSelectionSet)
		require.NoError(t, err)
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

		filtered, err := unionAndTrimSelectionSet("GizmoImplementation", schema, initialSelectionSet)
		require.NoError(t, err)
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

		filtered, err := unionAndTrimSelectionSet("GizmoImplementation", schema, initialSelectionSet)
		require.NoError(t, err)
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

		filtered, err := unionAndTrimSelectionSet("Gadget", schema, initialSelectionSet)
		require.NoError(t, err)
		require.Equal(t, formatSelectionSetSingleLine(ctx, schema, expected), formatSelectionSetSingleLine(ctx, schema, filtered))
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
		bodyJSON, err := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
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
		bodyJSON, err := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
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
		bodyJSON, err := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
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
		bodyJSON, err := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
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
		bodyJSON, err := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
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
		bodyJSON, err := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
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
		bodyJSON, err := formatResponseData(schema, document.Operations[0].SelectionSet, result)
		require.NoError(t, err)
		require.JSONEq(t, expectedJSON, string(bodyJSON))
	})
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

func TestQueryExecutionWithMultipleBoundaryQueries(t *testing.T) {
	schema1 := `directive @boundary on OBJECT | FIELD_DEFINITION
				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movies: [Movie!]!
				}`
	schema2 := `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					release: Int
				}

				type Query {
					movie(id: ID!): Movie @boundary
				}`

	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: schema1,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"movies": [
								{ "_bramble_id": "1", "id": "1", "title": "Test title 1" },
								{ "_bramble_id": "2", "id": "2", "title": "Test title 2" },
								{ "_bramble_id": "3", "id": "3", "title": "Test title 3" }
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
					w.Write([]byte(`{
						"data": {
							"_0": { "_bramble_id": "1", "id": "1", "release": 2007 },
							"_1": { "_bramble_id": "2", "id": "2", "release": 2008 },
							"_2": { "_bramble_id": "3", "id": "3", "release": 2009 }
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
	schema1 := `directive @boundary on OBJECT | FIELD_DEFINITION

	type Movie @boundary {
		id: ID!
		title: String
	}

	type Query {
		_movie(id: ID!): Movie @boundary
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
					if query.Operations[0].SelectionSet[0].(*ast.Field).Name == "_movie" {
						var res string
						for i, id := range ids {
							if i != 0 {
								res += ","
							}
							res += fmt.Sprintf(`
								"_%d": {
									"_bramble_id": "%s",
									"id": "%s",
									"title": "title %s"
								}`, i, id, id, id)
						}
						w.Write([]byte(fmt.Sprintf(`{ "data": { %s } }`, res)))
					} else {
						w.Write([]byte(fmt.Sprintf(`{
							"data": {
								"movie": {
									"_bramble_id": "%s",
									"id": "%s",
									"title": "title %s"
								}
							}
						}`, ids[0], ids[0], ids[0])))
					}
				}),
			},
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					compTitles: [Movie]
				}

				type Query {
					movie(id: ID!): Movie @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"_bramble_id": "1",
								"id": "1",
								"compTitles": [
									{
										"_bramble_id": "2",
										"id": "2",
										"compTitles": [
											{ "_bramble_id": "3", "id": "3" },
											{ "_bramble_id": "4", "id": "4" }
										]
									},
									{
										"_bramble_id": "3",
										"id": "3",
										"compTitles": [
											{ "_bramble_id": "4", "id": "4" },
											{ "_bramble_id": "5", "id": "5" }
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
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
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
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie @boundary
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
	schema1 := `directive @boundary on OBJECT | FIELD_DEFINITION

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					_movie(id: ID!): Movie @boundary
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
				if query.Operations[0].SelectionSet[0].(*ast.Field).Name == "_movie" {
					var res string
					for i, id := range ids {
						if i != 0 {
							res += ","
						}
						res += fmt.Sprintf(`
								"_%d": {
									"_bramble_id": "%s",
									"id": "%s",
									"title": "title %s"
								}`, i, id, id, id)
					}
					w.Write([]byte(fmt.Sprintf(`{ "data": { %s } }`, res)))
				} else {
					w.Write([]byte(fmt.Sprintf(`{
							"data": {
								"movie": {
									"_bramble_id": "%s",
									"id": "%s",
									"title": "title %s"
								}
							}
						}`, ids[0], ids[0], ids[0])))
				}
			}),
		},
		{
			schema: `directive @boundary on OBJECT | FIELD_DEFINITION

			type Movie @boundary {
				id: ID!
				compTitles: [[Movie]]
			}

			type Query {
				movie(id: ID!): Movie @boundary
			}`,
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{
					"data": {
						"_0": {
							"_bramble_id": "1",
							"id": "1",
							"compTitles": [[
								{
									"_bramble_id": "2",
									"id": "2"
								},
								{
									"_bramble_id": "3",
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

func TestQueryExecutionEmptyBoundaryResponse(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION
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
								"_bramble_id": "1",
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
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION
				interface Node { id: ID! }

				type Movie @boundary {
					id: ID!
					title: String
				}

				type Query {
					movie(id: ID!): Movie @boundary
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
	schema1 := `directive @boundary on OBJECT | FIELD_DEFINITION
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
								_bramble_id: id
							}
							_bramble_id: id
						}
					}`, q["query"])
					w.Write([]byte(`{
						"data": {
							"movie": {
								"_bramble_id": "1",
								"id": "1",
								"title": "Test title",
								"otherMovie": {
									"_bramble_id": "2",
									"title": "another title"
								}
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
					movie(id: ID!): Movie! @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"_bramble_id": "1",
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
								"_bramble_id": "1",
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
					movie(id: ID!): Movie! @boundary
					movies: [Movie!]
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					body, _ := ioutil.ReadAll(r.Body)
					if strings.Contains(string(body), "movies") {
						w.Write([]byte(`{
							"data": {
								"movies": [
									{ "_bramble_id": "1", "id": "1", "release": 2007 },
									{ "_bramble_id": "2", "id": "2", "release": 2018 }
								]
							}
						}
						`))
					} else {
						w.Write([]byte(`{
							"data": {
								"_0": {
									"_bramble_id": "1",
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
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION
				type Gizmo {
					foo: String!
					bar: String!
				}
				type Movie @boundary {
					id: ID!
					gizmo: Gizmo
				}
				type Query {
					movie(id: ID!): Movie @boundary
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
								"_bramble_id": "1",
								"id": "1"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION
				type Gizmo {
					foo: String!
					bar: String!
				}
				type Movie @boundary {
					id: ID!
					gizmo: Gizmo
				}
				type Query {
					movie(id: ID!): Movie @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"_bramble_id": "1",
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
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION
				type Gizmo {
					foo: String!
					bar: String!
				}
				type Movie @boundary {
					id: ID!
					gizmo: Gizmo
				}
				type Query {
					movie(id: ID!): Movie @boundary
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
								"_bramble_id": "1",
								"id": "1"
							}
						}
					}
					`))
				}),
			},
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION
				type Gizmo {
					foo: String!
					bar: String!
				}
				type Movie @boundary {
					id: ID!
					gizmo: Gizmo
				}
				type Query {
					movie(id: ID!): Movie @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(`{
						"data": {
							"_0": {
								"_bramble_id": "1",
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
					assertQueriesEqual(t, schema1, `mutation { updateTitle(id: "2", title: "New title") { title _bramble_id: id } }`, q["query"])

					w.Write([]byte(`{
						"data": {
							"updateTitle": {
								"_bramble_id": "2",
								"id": "2",
								"title": "New title"
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
								"_bramble_id": "2",
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
				directive @boundary on OBJECT | FIELD_DEFINITION

				type Dog { name: String! age: Int }
				type Cat { name: String! age: Int }
				type Snake { name: String! age: Int }
				union Animal = Dog | Cat | Snake

				type Person @boundary {
					id: ID!
					pet: Animal
				}

				type Query {
					animal(id: ID!): Animal
					person(id: ID!): Person @boundary
					animals: [Animal]!
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := ioutil.ReadAll(r.Body)
					if strings.Contains(string(b), "animals") {
						w.Write([]byte(`{
							"data": {
								"foo": [
									{ "name": "fido", "age": 4, "_bramble__typename": "Dog" },
									{ "name": "felix", "age": 2, "_bramble__typename": "Cat" },
									{ "age": 20, "name": "ka", "_bramble__typename": "Snake" }
								]
							}
						}
						`))
					} else {
						w.Write([]byte(`{
							"data": {
								"_0": {
									"_bramble_id": "2",
									"pet": {
										"name": "felix",
										"age": 2,
										"_bramble__typename": "Cat"
									}
								}
							}
						}
						`))
					}
				}),
			},
			{
				schema: `directive @boundary on OBJECT | FIELD_DEFINITION

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
								"_bramble_id": "2",
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
					directive @boundary on OBJECT | FIELD_DEFINITION
					directive @namespace on OBJECT

					type Cat @boundary {
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
						cat(id: ID!): Cat @boundary
					}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					b, _ := ioutil.ReadAll(r.Body)

					if strings.Contains(string(b), "CA7") {
						w.Write([]byte(`{
							"data": {
								"_0": {
									"_bramble_id": "CA7",
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
					directive @boundary on OBJECT | FIELD_DEFINITION
					directive @namespace on OBJECT

					type Cat @boundary {
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
										"_bramble_id": "CA7",
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
								"_bramble_id": "1",
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
								"_bramble_id": "1",
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

func TestQuerySelectionSetFragmentMismatchesWithResponse(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{{
			schema: `
				interface Transport {
					speed: Int!
				}

				type Bicycle implements Transport {
					speed: Int!
					dropbars: Boolean!
				}

				type Plane implements Transport {
					speed: Int!
					winglength: Int!
				}

				type Query {
					selectedTransport: Transport!
				}`,
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`{
					"data": {
						"selectedTransport": {
							"speed": 30
						}
					}
        }`))
			}),
		}},
		query: `query {
			selectedTransport {
				speed
				... on Plane {
					__typename
					winglength
				}
			}
    	}`,
		expected: `{
			"selectedTransport": {
				"speed": 30
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
									"_bramble_id": "1",
									"id": "1",
									"title": "Movie 1"
								},
								{
									"_bramble_id": "2",
									"id": "2",
									"title": "Movie 2"
								},
								{
									"_bramble_id": "3",
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
									"_bramble_id": "1",
									"id": "1",
									"release": 2007
								},
								{
									"_bramble_id": "2",
									"id": "2",
									"release": 2008
								},
								{
									"_bramble_id": "3",
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

func TestSchemaUpdate_serviceError(t *testing.T) {
	schemaA := `directive @boundary on OBJECT
				type Service {
					name: String!
					version: String!
					schema: String!
				}

				type Gizmo {
					name: String!
				}

				type Query {
					service: Service!
				}`

	schemaB := `directive @boundary on OBJECT
				type Service {
					name: String!
					version: String!
					schema: String!
				}

				type Gadget {
					name: String!
				}

				type Query {
					service: Service!
				}`
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: schemaA,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					http.Error(w, "", http.StatusInternalServerError)
				}),
			},
			{
				schema: schemaB,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Write([]byte(fmt.Sprintf(`{
						"data": {
							"service": {
								"name": "serviceB",
								"version": "v0.0.1",
								"schema": %q
							}
						}
					}
					`, schemaB)))
				}),
			},
		},
	}

	executableSchema, cleanup := f.setup(t)
	defer cleanup()

	foundGizmo, foundGadget := false, false

	for typeName := range executableSchema.MergedSchema.Types {
		if typeName == "Gizmo" {
			foundGizmo = true
		}
		if typeName == "Gadget" {
			foundGadget = true
		}
	}

	if !foundGizmo || !foundGadget {
		t.Error("expected both Gadget and Gizmo in schema")
	}

	executableSchema.UpdateSchema(false)

	for _, service := range executableSchema.Services {
		if service.Name == "serviceA" {
			require.Equal(t, "", service.SchemaSource)
		}
	}

	for typeName := range executableSchema.MergedSchema.Types {
		if typeName == "Gizmo" {
			t.Error("expected Gizmo to be dropped from schema")
		}
	}
}

type testService struct {
	schema  string
	handler http.Handler
}

type queryExecutionFixture struct {
	services     []testService
	variables    map[string]interface{}
	mergedSchema *ast.Schema
	query        string
	expected     string
	resp         *graphql.Response
	debug        *DebugInfo
	errors       gqlerror.List
}

func (f *queryExecutionFixture) checkSuccess(t *testing.T) {
	f.run(t)

	require.Empty(t, f.resp.Errors)
	jsonEqWithOrder(t, f.expected, string(f.resp.Data))
}

func (f *queryExecutionFixture) setup(t *testing.T) (*ExecutableSchema, func()) {
	var services []*Service
	var schemas []*ast.Schema
	var serverCloses []func()

	for _, s := range f.services {
		serv := httptest.NewServer(s.handler)
		serverCloses = append(serverCloses, serv.Close)

		schema := gqlparser.MustLoadSchema(&ast.Source{Input: s.schema})
		service := NewService(serv.URL)
		service.Schema = schema
		service.SchemaSource = s.schema
		services = append(services, service)

		schemas = append(schemas, schema)
	}

	merged, err := MergeSchemas(schemas...)
	require.NoError(t, err)

	f.mergedSchema = merged

	es := newExecutableSchema(nil, 50, nil, services...)
	es.MergedSchema = merged
	es.BoundaryQueries = buildBoundaryFieldsMap(services...)
	es.Locations = buildFieldURLMap(services...)
	es.IsBoundary = buildIsBoundaryMap(services...)

	return es, func() {
		for _, close := range serverCloses {
			close()
		}
	}
}

func (f *queryExecutionFixture) run(t *testing.T) {
	es, cleanup := f.setup(t)
	defer cleanup()
	query := gqlparser.MustLoadQuery(f.mergedSchema, f.query)
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
		require.Empty(t, f.resp.Errors)
		jsonEqWithOrder(t, f.expected, string(f.resp.Data))
	} else {
		require.Equal(t, len(f.errors), len(f.resp.Errors))
		for i := range f.errors {
			delete(f.resp.Errors[i].Extensions, "serviceUrl")
			require.Equal(t, *f.errors[i], *f.resp.Errors[i])
		}
	}
}

func jsonToInterfaceMap(jsonString string) map[string]interface{} {
	var outputMap map[string]interface{}
	err := json.Unmarshal([]byte(jsonString), &outputMap)
	if err != nil {
		panic(err)
	}

	return outputMap
}

func jsonToInterfaceSlice(jsonString string) []interface{} {
	var outputSlice []interface{}
	err := json.Unmarshal([]byte(jsonString), &outputSlice)
	if err != nil {
		panic(err)
	}

	return outputSlice
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
