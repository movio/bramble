package bramble

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestHonorsPermissions(t *testing.T) {
	schema := `
	type Cinema {
		id: ID!
		name: String!
	}

	type Query {
		cinema(id: ID!): Cinema!
	}`

	mergedSchema, err := MergeSchemas(gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: schema}))
	require.NoError(t, err)

	es := ExecutableSchema{
		tracer:       noop.NewTracerProvider().Tracer("test"),
		MergedSchema: mergedSchema,
	}

	query := gqlparser.MustLoadQuery(es.MergedSchema, `{
		cinema(id: "Cinema") {
			name
		}
	}`)
	ctx := testContextWithNoPermissions(query.Operations[0])
	resp := es.ExecuteQuery(ctx)

	permissionsError := &gqlerror.Error{
		Message: "query.cinema access disallowed",
	}

	require.Contains(t, resp.Errors, permissionsError)
	require.Nil(t, resp.Data)
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
					"code":          "NOT_FOUND",
					"selectionSet":  `{ movie(id: "1") { id title } }`,
					"selectionPath": ast.Path{ast.PathName("movie")},
					"serviceName":   "",
				},
			},
			&gqlerror.Error{
				Message: `got a null response for non-nullable field "movie"`,
				Path:    ast.Path{ast.PathName("movie")},
			},
		},
	}

	es := f.setup(t)
	f.run(t, es, func(t *testing.T, resp *graphql.Response) {
		require.Equal(t, len(f.errors), len(resp.Errors))
		for i := range f.errors {
			assert.Error(t, resp.Errors[i])
			assert.Equal(t, f.errors[i].Message, resp.Errors[i].Message, "error message did not match")
			assert.Equal(t, f.errors[i].Path, resp.Errors[i].Path, "error path did not match")
			assert.Equal(t, f.errors[i].Locations, resp.Errors[i].Locations, "error locations did not match")
			assert.Equal(t, f.errors[i].Extensions, resp.Errors[i].Extensions, "error extensions did not match")
		}
	})
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
			snapshots: [Snapshot!]!
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
							"gizmos": [{ "_bramble_id": "GIZMO1", "id": "GIZMO1", "_bramble__typename": "Gizmo" }],
							"_bramble__typename": "GizmoImplementation"
						}
					}
				}`))
			} else if strings.Contains(string(body), "GADGET1") {
				w.Write([]byte(`
				{
					"data": {
						"snapshot": {
							"id": "100",
							"name": "foo",
							"gadgets": [{ "_bramble_id": "GADGET1", "id": "GADGET1", "_bramble__typename": "Gadget" }],
							"_bramble__typename": "GadgetImplementation"
						}
					}
				}`))
			} else {
				w.Write([]byte(`
				{
					"data": {
						"snapshots": [
							{
								"id": "100",
								"name": "foo",
								"gadgets": [{ "_bramble_id": "GADGET1", "id": "GADGET1", "_bramble__typename": "Gadget" }],
								"_bramble__typename": "GadgetImplementation"
							},
							{
								"id": "100",
								"name": "foo",
								"gizmos": [{ "_bramble_id": "GIZMO1", "id": "GIZMO1", "_bramble__typename": "Gizmo" }],
								"_bramble__typename": "GizmoImplementation"
							}
						]
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
							"_bramble__typename": "Gizmo",
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
								"_bramble__typename": "Gadget",
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

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
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

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
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

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
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

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
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

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
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

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
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

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
	})

	t.Run("with multiple top level fragment spreads (gadget implementation)", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query Foo {
				snapshot(id: "GADGET1") {
					id
					name
					... GadgetFragment
					... GizmoFragment
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

			fragment GizmoFragment on GizmoImplementation {
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

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
	})

	t.Run("with nested abstract fragment spreads", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{serviceA, serviceB},
			query: `
			query Foo {
				snapshots {
					...SnapshotFragment
				}
			}

			fragment SnapshotFragment on Snapshot {
				id
				name
				... on GadgetImplementation {
					gadgets {
						id
						name
					}
				}
			}`,
			expected: `
			{
				"snapshots": [
					{
						"id": "100",
						"name": "foo",
						"gadgets": [
							{
								"id": "GADGET1",
								"name": "Gadget #1"
							}
						]
					},
					{
						"id": "100",
						"name": "foo"
					}
				]
			}`,
		}

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
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
								"_bramble__typename": "Movie",
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
								"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
}

func TestQueryExecutionServiceTimeout(t *testing.T) {
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
								"_bramble__typename": "Movie",
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
					slowField: String
				}

				type Query {
					movie(id: ID!): Movie! @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					time.Sleep(20 * time.Millisecond)

					response := jsonToInterfaceMap(`{
						"data": {
							"_0": {
								"_bramble_id": "1",
								"_bramble__typename": "Movie",
								"id": "1",
								"release": 2007,
								"slowField": "very slow field"
							}
						}
					}
					`)
					if err := json.NewEncoder(w).Encode(response); err != nil {
						t.Errorf("Unexpected error %s", err)
					}
				}),
			},
		},
		query: `{
			movie(id: "1") {
				id
				title
				slowField
			}
		}`,
		expected: `{
			"movie": {
				"id": "1",
				"title": "Test title",
				"slowField": null
			}
		}`,
		errors: gqlerror.List{
			&gqlerror.Error{
				Message: "downstream request timed out",
				Path:    ast.Path{ast.PathName("movie")},
				Locations: []gqlerror.Location{
					{Line: 5, Column: 5},
				},
				Extensions: map[string]interface{}{
					"selectionSet": "{ slowField _bramble_id: id _bramble__typename: __typename }",
				},
			},
		},
	}

	es := f.setup(t)
	es.GraphqlClient.HTTPClient.Timeout = 10 * time.Millisecond

	f.run(t, es, func(t *testing.T, resp *graphql.Response) {
		jsonEqWithOrder(t, f.expected, string(resp.Data))

		assert.Equal(t, len(f.errors), len(resp.Errors))

		for i := range f.errors {
			// We want to unwrap the error to check the underlying error
			// type of the error returned by the client. This way we are
			// able to check if the error is a timeout error.
			respErr := resp.Errors[i].Unwrap()

			assert.Error(t, respErr, "expected error to be non-nil")
			assert.True(t, os.IsTimeout(respErr), "expected timeout error, got %T", respErr)
			assert.Equal(t, f.errors[i].Message, resp.Errors[i].Message, "error message did not match")
			assert.Equal(t, f.errors[i].Path, resp.Errors[i].Path, "error path did not match")
			assert.Equal(t, f.errors[i].Locations, resp.Errors[i].Locations, "error locations did not match")
			assert.Equal(t, f.errors[i].Extensions, resp.Errors[i].Extensions, "error extensions did not match")
		}

	})
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

	es := f.setup(t)
	f.run(t, es, func(t *testing.T, resp *graphql.Response) {
		jsonEqWithOrder(t, f.expected, string(resp.Data))
	})
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
									"_bramble__typename": "Movie",
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
								{ "_bramble_id": "2", "_bramble__typename": "Movie", "id": "2", "title": "Movie 2" },
								{ "_bramble_id": "3", "_bramble__typename": "Movie", "id": "3", "title": "Movie 3" },
								{ "_bramble_id": "4", "_bramble__typename": "Movie", "id": "4", "title": "Movie 4" }
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
									"_bramble__typename": "Movie",
									"compTitles": [
										{"_bramble_id": "2", "_bramble__typename": "Movie", "id": "2"},
										{"_bramble_id": "3", "_bramble__typename": "Movie", "id": "3"},
										{"_bramble_id": "4", "_bramble__typename": "Movie", "id": "4"}
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
								"_bramble__typename": "Namespace",
								"movies": [
									{
										"_bramble_id": "MOVIE1",
										"_bramble__typename": "Movie",
										"id": "MOVIE1",
										"title": "Movie #1",
										"director": { "_bramble_id": "DIRECTOR1", "_bramble__typename": "Person", "id": "DIRECTOR1" }
									},
									{
										"_bramble_id": "MOVIE2",
										"_bramble__typename": "Movie",
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
									"_bramble__typename": "Person",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
}

func TestNestingNullableBoundaryTypes(t *testing.T) {
	t.Run("nested boundary types are all null", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{
				{
					schema: `directive @boundary on OBJECT | FIELD_DEFINITION
						type Gizmo @boundary {
							id: ID!
						}
						type Query {
							tastyGizmos: [Gizmo!]!
							gizmo(ids: [ID!]!): [Gizmo]! @boundary
						}`,
					handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Write([]byte(`
							{
								"data": {
									"tastyGizmos": [
										{
											"_bramble_id": "beehasknees",
											"_bramble__typename": "Gizmo",
											"id": "beehasknees"
										},
										{
											"_bramble_id": "umlaut",
											"_bramble__typename": "Gizmo",
											"id": "umlaut"
										},
										{
											"_bramble_id": "probanana",
											"_bramble__typename": "Gizmo",
											"id": "probanana"
										}
									]
								}
							}
						`))
					}),
				},
				{
					schema: `directive @boundary on OBJECT | FIELD_DEFINITION
						type Gizmo @boundary {
							id: ID!
							wizzle: Wizzle
						}
						type Wizzle @boundary {
							id: ID!
						}
						type Query {
							wizzles(ids: [ID!]): [Wizzle]! @boundary
							gizmo(ids: [ID!]): [Gizmo]! @boundary
						}`,
					handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Write([]byte(`{
						"data": {
							"_result": [null, null, null]
						}
					}`))
					}),
				},
				{
					schema: `directive @boundary on OBJECT | FIELD_DEFINITION
						type Wizzle @boundary {
							id: ID!
							bazingaFactor: Int
						}
						type Query {
							wizzles(ids: [ID!]): [Wizzle]! @boundary
						}`,
					handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Write([]byte(`should not be called...`))
					}),
				},
			},

			query: `{
				tastyGizmos {
					id
					wizzle {
						id
						bazingaFactor
					}
				}
			}`,
			expected: `{
				"tastyGizmos": [
					{
						"id": "beehasknees",
						"wizzle": null
					},
					{
						"id": "umlaut",
						"wizzle": null
					},
					{
						"id": "probanana",
						"wizzle": null
					}
				]
			}`,
		}

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
	})

	t.Run("nested boundary types sometimes null", func(t *testing.T) {
		f := &queryExecutionFixture{
			services: []testService{
				{
					schema: `directive @boundary on OBJECT | FIELD_DEFINITION
						type Gizmo @boundary {
							id: ID!
						}
						type Query {
							tastyGizmos: [Gizmo!]!
							gizmo(ids: [ID!]!): [Gizmo]! @boundary
						}`,
					handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Write([]byte(`
							{
								"data": {
									"tastyGizmos": [
										{
											"_bramble_id": "beehasknees",
											"_bramble__typename": "Gizmo",
											"id": "beehasknees"
										},
										{
											"_bramble_id": "umlaut",
											"_bramble__typename": "Gizmo",
											"id": "umlaut"
										},
										{
											"_bramble_id": "probanana",
											"_bramble__typename": "Gizmo",
											"id": "probanana"
										}
									]
								}
							}
						`))
					}),
				},
				{
					schema: `directive @boundary on OBJECT | FIELD_DEFINITION
						type Gizmo @boundary {
							id: ID!
							wizzle: Wizzle
						}
						type Wizzle @boundary {
							id: ID!
						}
						type Query {
							wizzles(ids: [ID!]): [Wizzle]! @boundary
							gizmos(ids: [ID!]): [Gizmo]! @boundary
						}`,
					handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Write([]byte(`{
						"data": {
							"_result": [
								null,
								{
									"_bramble_id": "umlaut",
									"_bramble__typename": "Gizmo",
									"id": "umlaut",
									"wizzle": null
								},
								{
									"_bramble_id": "probanana",
									"_bramble__typename": "Gizmo",
									"id": "probanana",
									"wizzle": {
										"_bramble_id": "bananawizzle",
										"_bramble__typename": "Wizzle",
										"id": "bananawizzle"
									}
								}
							]
						}
					}`))
					}),
				},
				{
					schema: `directive @boundary on OBJECT | FIELD_DEFINITION
						type Wizzle @boundary {
							id: ID!
							bazingaFactor: Int
						}
						type Query {
							wizzles(ids: [ID!]): [Wizzle]! @boundary
						}`,
					handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Write([]byte(`{
						"data": {
							"_result": [
								{
									"_bramble_id": "bananawizzle",
									"_bramble__typename": "Wizzle",
									"id": "bananawizzle",
									"bazingaFactor": 4
								}
							]
						}
					}`))
					}),
				},
			},

			query: `{
				tastyGizmos {
					id
					wizzle {
						id
						bazingaFactor
					}
				}
			}`,
			expected: `{
				"tastyGizmos": [
					{
						"id": "beehasknees",
						"wizzle": null
					},
					{
						"id": "umlaut",
						"wizzle": null
					},
					{
						"id": "probanana",
						"wizzle": {
							"id": "bananawizzle",
							"bazingaFactor": 4
						}
					}
				]
			}`,
		}

		es := f.setup(t)
		f.run(t, es, f.checkSuccess())
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
								{ "_bramble_id": "1", "_bramble__typename": "Movie", "id": "1", "title": "Test title 1" },
								{ "_bramble_id": "2", "_bramble__typename": "Movie", "id": "2", "title": "Test title 2" },
								{ "_bramble_id": "3", "_bramble__typename": "Movie", "id": "3", "title": "Test title 3" }
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
							"_0": { "_bramble_id": "1", "_bramble__typename": "Movie", "id": "1", "release": 2007 },
							"_1": { "_bramble_id": "2", "_bramble__typename": "Movie", "id": "2", "release": 2008 },
							"_2": { "_bramble_id": "3", "_bramble__typename": "Movie", "id": "3", "release": 2009 }
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
									"_bramble__typename": "Movie",
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
									"_bramble__typename": "Movie",
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
								"_bramble__typename": "Movie",
								"id": "1",
								"compTitles": [
									{
										"_bramble_id": "2",
										"_bramble__typename": "Movie",
										"id": "2",
										"compTitles": [
											{ "_bramble_id": "3", "_bramble__typename": "Movie", "id": "3" },
											{ "_bramble_id": "4", "_bramble__typename": "Movie", "id": "4" }
										]
									},
									{
										"_bramble_id": "3",
										"_bramble__typename": "Movie",
										"id": "3",
										"compTitles": [
											{ "_bramble_id": "4", "_bramble__typename": "Movie", "id": "4" },
											{ "_bramble_id": "5", "_bramble__typename": "Movie", "id": "5" }
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
									"_bramble__typename": "Movie",
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
									"_bramble__typename": "Movie",
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
							"_bramble__typename": "Movie",
							"id": "1",
							"compTitles": [[
								{
									"_bramble_id": "2",
									"_bramble__typename": "Movie",
									"id": "2"
								},
								{
									"_bramble_id": "3",
									"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
								"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
								_bramble__typename: __typename
							}
							_bramble_id: id
							_bramble__typename: __typename
						}
					}`, q["query"])
					w.Write([]byte(`{
						"data": {
							"movie": {
								"_bramble_id": "1",
								"_bramble__typename": "Movie",
								"id": "1",
								"title": "Test title",
								"otherMovie": {
									"_bramble_id": "2",
									"_bramble__typename": "Movie",
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
								"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
								"_bramble__typename": "Movie",
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
					body, _ := io.ReadAll(r.Body)
					if strings.Contains(string(body), "movies") {
						w.Write([]byte(`{
							"data": {
								"movies": [
									{ "_bramble_id": "1", "_bramble__typename": "Movie", "id": "1", "release": 2007 },
									{ "_bramble_id": "2", "_bramble__typename": "Movie", "id": "2", "release": 2018 }
								]
							}
						}
						`))
					} else {
						w.Write([]byte(`{
							"data": {
								"_0": {
									"_bramble_id": "1",
									"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
								"_bramble__typename": "Movie",
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
								"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
								"_bramble__typename": "Movie",
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
								"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
					assertQueriesEqual(t, schema1, `mutation { updateTitle(id: "2", title: "New title") { title _bramble_id: id _bramble__typename: __typename } }`, q["query"])

					w.Write([]byte(`{
						"data": {
							"updateTitle": {
								"_bramble_id": "2",
								"_bramble__typename": "Movie",
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
								"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
					b, _ := io.ReadAll(r.Body)
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
									"_bramble__typename": "Person",
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
								"_bramble__typename": "Person",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
					b, _ := io.ReadAll(r.Body)

					if strings.Contains(string(b), "CA7") {
						w.Write([]byte(`{
							"data": {
								"_0": {
									"_bramble_id": "CA7",
									"_bramble__typename": "Cat",
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
										"_bramble__typename": "Cat",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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

	es := f.setup(t)
	f.run(t, es, func(t *testing.T, resp *graphql.Response) {
		assert.True(t, called)
		assert.NotNil(t, resp.Extensions["variables"])
		require.Empty(t, resp.Errors)
		jsonEqWithOrder(t, f.expected, string(resp.Data))
	})
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
								"_bramble__typename": "Movie",
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
								"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
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
									"_bramble__typename": "Movie",
									"id": "1",
									"title": "Movie 1"
								},
								{
									"_bramble_id": "2",
									"_bramble__typename": "Movie",
									"id": "2",
									"title": "Movie 2"
								},
								{
									"_bramble_id": "3",
									"_bramble__typename": "Movie",
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
									"_bramble__typename": "Movie",
									"id": "1",
									"release": 2007
								},
								{
									"_bramble_id": "2",
									"_bramble__typename": "Movie",
									"id": "2",
									"release": 2008
								},
								{
									"_bramble_id": "3",
									"_bramble__typename": "Movie",
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

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
}

func TestQueryWithAbstractType(t *testing.T) {
	f := &queryExecutionFixture{
		services: []testService{
			{
				schema: `
				directive @boundary on OBJECT | FIELD_DEFINITION

				interface Foo {
				  id: ID!
				}

				type Bar implements Foo {
				  id: ID!
				  bar: String!
				}

				type Baz implements Foo @boundary {
				  id: ID!
				}

				type Query {
				  foos: [Foo!]!
				  baz(id: ID!): Baz @boundary
				}`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(`{
						"data": {
							"foos": [
								{
									"_bramble_id": "1",
									"_bramble__typename": "Baz",
									"id": "1"
								},
								{
									"_bramble__typename": "Bar",
									"id": "2",
									"bar": "bar"
								}
							]
						}
					}
					`))
				}),
			},
			{
				schema: `
					directive @boundary on OBJECT | FIELD_DEFINITION

					type Baz @boundary {
						id: ID!
						baz: String!
					}

					type Query {
						baz(id: ID!): Baz @boundary
					}
				`,
				handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(`{
						"data": {
							"_0": {
								"_bramble_id": "1",
								"_bramble__typename": "Baz",
								"id": "1",
								"baz": "baz"
							}
						}
					}
					`))
				}),
			},
		},
		query: `{
			foos {
				id
				... on Baz {
					baz
				}
			}
		}`,
		expected: `{
			"foos": [
				{
					"id": "1",
					"baz": "baz"
				},
				{
					"id": "2"
				}
			]
		}`,
	}

	es := f.setup(t)
	f.run(t, es, f.checkSuccess())
}

func TestMergeWithNull(t *testing.T) {
	nullMap := make(map[string]interface{})
	dataMap := map[string]interface{}{
		"data": "foo",
	}

	require.NoError(t, json.Unmarshal([]byte(`null`), &nullMap))

	merged, err := mergeExecutionResults([]executionResult{
		{
			Data: nullMap,
		},
		{
			Data: dataMap,
		},
	})

	require.NoError(t, err)
	require.Equal(t, dataMap, merged)
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

	executableSchema := f.setup(t)

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

	executableSchema.UpdateSchema(context.TODO(), false)

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
	debug        *DebugInfo
	errors       gqlerror.List
}

func (f *queryExecutionFixture) setup(t *testing.T) *ExecutableSchema {
	var services []*Service
	var schemas []*ast.Schema

	for _, s := range f.services {
		serv := httptest.NewServer(s.handler)
		t.Cleanup(serv.Close)

		schema := gqlparser.MustLoadSchema(&ast.Source{Input: s.schema})
		service := NewService(serv.URL)
		service.Schema = schema
		service.SchemaSource = s.schema
		services = append(services, service)

		schemas = append(schemas, schema)
	}

	merged, err := MergeSchemas(schemas...)
	require.NoError(t, err, "failed to merge schemas before testrun")

	f.mergedSchema = merged

	es := NewExecutableSchema(nil, 50, nil, services...)
	es.MergedSchema = merged
	es.BoundaryQueries = buildBoundaryFieldsMap(services...)
	es.Locations = buildFieldURLMap(services...)
	es.IsBoundary = buildIsBoundaryMap(services...)

	return es
}

type assertFunc func(t *testing.T, resp *graphql.Response)

func (f *queryExecutionFixture) run(t *testing.T, es *ExecutableSchema, assertFunc assertFunc) {
	query := gqlparser.MustLoadQuery(f.mergedSchema, f.query)
	vars := f.variables
	if vars == nil {
		vars = map[string]interface{}{}
	}
	ctx := testContextWithVariables(vars, query.Operations[0])
	if f.debug != nil {
		ctx = context.WithValue(ctx, DebugKey, *f.debug)
	}
	resp := es.ExecuteQuery(ctx)
	resp.Extensions = graphql.GetExtensions(ctx)

	// Remove serviceUrl from extensions to make tests deterministic
	for i := range resp.Errors {
		delete(resp.Errors[i].Extensions, "serviceUrl")
	}

	assertFunc(t, resp)
}

func (f *queryExecutionFixture) checkSuccess() assertFunc {
	return func(t *testing.T, resp *graphql.Response) {
		require.Empty(t, resp.Errors, "expected no errors, got %v", resp.Errors)
		jsonEqWithOrder(t, f.expected, string(resp.Data))
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
	t.Helper()
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
	t.Helper()
	s := gqlparser.MustLoadSchema(&ast.Source{Input: schema})

	var expectedBuf bytes.Buffer
	formatter.NewFormatter(&expectedBuf).FormatQueryDocument(gqlparser.MustLoadQuery(s, expected))
	var actualBuf bytes.Buffer
	formatter.NewFormatter(&actualBuf).FormatQueryDocument(gqlparser.MustLoadQuery(s, actual))

	return assert.Equal(t, expectedBuf.String(), actualBuf.String(), "queries are not equal")
}

func testContextWithoutVariables(op *ast.OperationDefinition) context.Context {
	if op == nil {
		op = &ast.OperationDefinition{}
	}

	return AddPermissionsToContext(graphql.WithOperationContext(context.Background(), &graphql.OperationContext{
		OperationName: op.Name,
		Variables:     map[string]interface{}{},
		Operation:     op,
	}), OperationPermissions{
		AllowedRootQueryFields:        AllowedFields{AllowAll: true},
		AllowedRootMutationFields:     AllowedFields{AllowAll: true},
		AllowedRootSubscriptionFields: AllowedFields{AllowAll: true},
	})
}

func testContextWithNoPermissions(op *ast.OperationDefinition) context.Context {
	if op == nil {
		op = &ast.OperationDefinition{}
	}

	return AddPermissionsToContext(graphql.WithOperationContext(context.Background(), &graphql.OperationContext{
		OperationName: op.Name,
		Variables:     map[string]interface{}{},
		Operation:     op,
	}), OperationPermissions{
		AllowedRootQueryFields:        AllowedFields{},
		AllowedRootMutationFields:     AllowedFields{},
		AllowedRootSubscriptionFields: AllowedFields{},
	})
}

func testContextWithVariables(vars map[string]interface{}, op *ast.OperationDefinition) context.Context {
	if op == nil {
		op = &ast.OperationDefinition{}
	}

	return AddPermissionsToContext(graphql.WithResponseContext(graphql.WithOperationContext(context.Background(), &graphql.OperationContext{
		OperationName: op.Name,
		Variables:     vars,
		Operation:     op,
	}), graphql.DefaultErrorPresenter, graphql.DefaultRecover), OperationPermissions{
		AllowedRootQueryFields:        AllowedFields{AllowAll: true},
		AllowedRootMutationFields:     AllowedFields{AllowAll: true},
		AllowedRootSubscriptionFields: AllowedFields{AllowAll: true},
	})
}
