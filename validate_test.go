package bramble

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

type testSchema struct {
	*testing.T
	schema *ast.Schema
}

func withSchema(t *testing.T, schema string) *testSchema {
	t.Helper()
	return &testSchema{
		T:      t,
		schema: gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: schema}),
	}
}

func (t *testSchema) assertValid(f func(*ast.Schema) error) {
	t.Helper()
	assert.NoError(t.T, f(t.schema))
}

func (t *testSchema) assertInvalid(err string, f func(*ast.Schema) error) {
	t.Helper()
	e := f(t.schema)
	assert.Error(t.T, e)
	if e != nil {
		assert.Equal(t.T, err, e.Error())
	}
}

func TestSchemaIsValid(t *testing.T) {
	withSchema(t, `
	directive @boundary on OBJECT
	interface Node {
		id: ID!
	}
	type Service {
		name: String!
		version: String!
		schema: String!
	}
	type Query {
		node(id: ID!): Node
		service: Service!
	}
	`).assertValid(ValidateSchema)
}

func TestBoundaryDirectiveRequirements(t *testing.T) {
	// check @boundary directive matches requirements
	t.Run("@boundary missing", func(t *testing.T) {
		withSchema(t, `
		type Filler {
			other: String
		}
		`).assertInvalid("@boundary directive not found", validateBoundaryDirective)
	})
	t.Run("@boundary present", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT
		`).assertValid(validateBoundaryDirective)
	})
	t.Run("@boundary on OBJECT", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on FIELD
		`).assertInvalid("@boundary directive should have location OBJECT", validateBoundaryDirective)
	})
	t.Run("@boundary has one location", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on FIELD | OBJECT
		`).assertInvalid("@boundary directive should have locations OBJECT | FIELD_DEFINITION", validateBoundaryDirective)
	})
	t.Run("@boundary has no arguments", func(t *testing.T) {
		withSchema(t, `
		directive @boundary(id: String) on OBJECT
		`).assertInvalid("@boundary directive may not take arguments", validateBoundaryDirective)
	})
	// @boundary does not need to be present
	t.Run("@boundary not required", func(t *testing.T) {
		withSchema(t, `
		type Filler {
			other: String
		}
		`).assertValid(validateBoundaryObjects)
	})
	t.Run("@boundary is checked if it is used", func(t *testing.T) {
		withSchema(t, `
		directive @boundary(incorrect: String) on OBJECT
		type Filler @boundary {
			id: ID!
		}
		`).assertInvalid("@boundary directive may not take arguments", validateBoundaryObjects)
	})
	t.Run("@boundary is checked if it is used", func(t *testing.T) {
		withSchema(t, `
		directive @boundary(incorrect: String) on OBJECT
		type Filler @boundary {
			id: ID!
		}
		`).assertInvalid("@boundary directive may not take arguments", ValidateSchema)
	})
}

func TestNamespaceDirectiveRequirements(t *testing.T) {
	t.Run("valid namespaces", func(t *testing.T) {
		withSchema(t, `
		directive @namespace on OBJECT

		type SubNamespace @namespace {
			someField: String!
		}

		type RootNamespace @namespace {
			sub: SubNamespace!
		}

		type Query {
			root: RootNamespace!
		}
		`).assertValid(validateNamespaceObjects)
	})
	t.Run("invalid @namespace directive", func(t *testing.T) {
		withSchema(t, `
		directive @namespace(incorrect: String) on OBJECT

		type NamespaceQuery @namespace {
			someField: String!
		}
		`).assertInvalid("@namespace directive may not take arguments", ValidateSchema)
	})
	t.Run("nullable namespace field", func(t *testing.T) {
		withSchema(t, `
		directive @namespace on OBJECT

		type NamespaceQuery @namespace {
			someField: String!
		}

		type Query {
			ns: NamespaceQuery
		}
		`).assertInvalid("namespace return type should be non nullable on Query.ns", ValidateSchema)
	})
	t.Run("invalid namespace ascendence", func(t *testing.T) {
		withSchema(t, `
		directive @namespace on OBJECT

		type NamespaceQuery @namespace {
			someField: String!
		}

		type SomeObject {
			someField: String!
			ns: NamespaceQuery!
		}

		type Query {
			someObject: SomeObject!
		}
		`).assertInvalid(`type "NamespaceQuery" (namespace type) is used for field "ns" in non-namespace object "SomeObject"`, ValidateSchema)
	})
}

func TestNodeInterface(t *testing.T) {
	t.Run("Node interface missing", func(t *testing.T) {
		withSchema(t, "").assertInvalid("the Node interface was not found", validateNodeInterface)
	})
	t.Run("Node is not interface", func(t *testing.T) {
		withSchema(t, `
		type Node {
			id: ID!
		}`).assertInvalid("the Node type must be an interface", validateNodeInterface)
	})
	t.Run("Node interface has extra fields", func(t *testing.T) {
		withSchema(t, `
		interface Node {
			id: ID!
			extra: String
		}`).assertInvalid("the Node interface should have exactly one field", validateNodeInterface)
	})
	t.Run("Node interface has incorrect field", func(t *testing.T) {
		withSchema(t, `
		interface Node {
			incorrect: String
		}`).assertInvalid("the Node interface should have a field called 'id'", validateNodeInterface)
	})
	t.Run("Node interface has incorrect type", func(t *testing.T) {
		withSchema(t, `
		interface Node {
			id: String
		}`).assertInvalid("the Node interface should have a field called 'id' of type 'ID!'", validateNodeInterface)
	})
	t.Run("Node interface is correct", func(t *testing.T) {
		withSchema(t, `
		interface Node {
			id: ID!
		}`).assertValid(validateNodeInterface)
	})
}

func TestNodeQuery(t *testing.T) {
	t.Run("query type missing", func(t *testing.T) {
		withSchema(t, "").assertInvalid("the schema is missing a Query type", validateNodeQuery)
	})
	t.Run("node query missing", func(t *testing.T) {
		withSchema(t, `
		type Query {
			other: String
		}
		`).assertInvalid("the Query type is missing the 'node' field", validateNodeQuery)
	})
	t.Run("query with no arguments", func(t *testing.T) {
		withSchema(t, `
		type Query {
			node: ID!
		}
		`).assertInvalid("the 'node' field of Query must take a single argument", validateNodeQuery)
	})
	t.Run("query with wrong argument name", func(t *testing.T) {
		withSchema(t, `
		type Query {
			node(incorrect: ID!): ID!
		}
		`).assertInvalid("the 'node' field of Query must take a single argument called 'id'", validateNodeQuery)
	})
	t.Run("query with extra argument", func(t *testing.T) {
		withSchema(t, `
		type Query {
			node(id: ID!, incorrect: String): ID!
		}
		`).assertInvalid("the 'node' field of Query must take a single argument", validateNodeQuery)
	})
	t.Run("query with wrong argument type", func(t *testing.T) {
		withSchema(t, `
		type Query {
			node(id: String): ID!
		}
		`).assertInvalid("the 'node' field of Query must take a single argument of type 'ID!'", validateNodeQuery)
	})
	t.Run("query with wrong type", func(t *testing.T) {
		withSchema(t, `
		type Query {
			node(id: ID!): ID!
		}
		`).assertInvalid("the 'node' field of Query must be of type 'Node'", validateNodeQuery)
	})
	t.Run("query is correct", func(t *testing.T) {
		withSchema(t, `
		interface Node {
			id: ID!
		}
		type Query {
			node(id: ID!): Node
		}
		`).assertValid(validateNodeQuery)
	})
	t.Run("Query is checked if @boundary is used", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT
		type Query {
			node(id: ID!): ID!
		}
		interface Node {
			id: ID!
		}
		type Gizmo implements Node @boundary {
			id: ID!
		}`).assertInvalid("the 'node' field of Query must be of type 'Node'", validateBoundaryObjects)
	})
}

func TestUnions(t *testing.T) {
	t.Run("Unions are supported", func(t *testing.T) {
		withSchema(t, `
			type Dog { name: String! }
			type Cat { name: String! }
			type Snake { name: String! }
			union Animal = Dog | Cat | Snake

			directive @boundary on OBJECT
			interface Node {
				id: ID!
			}
			type Service {
				name: String!
				version: String!
				schema: String!
			}
			type Query {
				node(id: ID!): Node
				service: Service!
			}
		`).assertValid(ValidateSchema)
	})
}

func TestServiceObject(t *testing.T) {
	t.Run("Service is required", func(t *testing.T) {
		withSchema(t, "").assertInvalid("the Service object was not found", validateServiceObject)
	})
	t.Run("Service is an object", func(t *testing.T) {
		withSchema(t, `
		interface Service {
			incorrect: String
		}
		`).assertInvalid("the Service type must be an object", validateServiceObject)
	})
	t.Run("Service has 3 fields", func(t *testing.T) {
		withSchema(t, `
		type Service {
			incorrect: String
		}
		`).assertInvalid("the Service object should have exactly 3 fields", validateServiceObject)
	})
	t.Run("Service has correct 3 fields", func(t *testing.T) {
		withSchema(t, `
		type Service {
			incorrect: String
			other: String
			wrong: String
		}
		`).assertInvalid("the Service object should not have a field called incorrect", validateServiceObject)
	})
	t.Run("Service has a name field", func(t *testing.T) {
		withSchema(t, `
		type Service {
			name: String
			version: String!
			schema: String!
		}
		`).assertInvalid("the Service object should have a field called 'name' of type 'String!'", validateServiceObject)
	})
	t.Run("Service has a version field", func(t *testing.T) {
		withSchema(t, `
		type Service {
			name: String!
			version: String
			schema: String!
		}
		`).assertInvalid("the Service object should have a field called 'version' of type 'String!'", validateServiceObject)
	})
	t.Run("Service has a schema field", func(t *testing.T) {
		withSchema(t, `
		type Service {
			name: String!
			version: String!
			schema: String
		}
		`).assertInvalid("the Service object should have a field called 'schema' of type 'String!'", validateServiceObject)
	})
	t.Run("Service is correct", func(t *testing.T) {
		withSchema(t, `
		type Service {
			name: String!
			version: String!
			schema: String!
		}
		`).assertValid(validateServiceObject)
	})
	t.Run("Service is checked", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT
		interface Service {
			name: String
		}
		type Query {
			service: Service!
		}
		`).assertInvalid("the Service type must be an object", ValidateSchema)
	})
}

func TestServiceQuery(t *testing.T) {
	t.Run("query type missing", func(t *testing.T) {
		withSchema(t, "").assertInvalid("the schema is missing a Query type", validateServiceQuery)
	})
	t.Run("service is required", func(t *testing.T) {
		withSchema(t, `
		type Query {
			q: String
		}
		`).assertInvalid("the Query type is missing the 'service' field", validateServiceQuery)
	})
	t.Run("service takes no arguments", func(t *testing.T) {
		withSchema(t, `
		type Query {
			service(name: String): String
		}
		`).assertInvalid("the 'service' field of Query must take no arguments", validateServiceQuery)
	})
	t.Run("service returns Service object", func(t *testing.T) {
		withSchema(t, `
		type Query {
			service: String
		}
		`).assertInvalid("the 'service' field of Query must be of type 'Service!'", validateServiceQuery)
	})
	t.Run("service is correct", func(t *testing.T) {
		withSchema(t, `
		type Service {
			name: String
		}
		type Query {
			service: Service!
		}
		`).assertValid(validateServiceQuery)
	})
	t.Run("service is checked", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT
		type Service {
			name: String!
			version: String!
			schema: String!
		}
		type Query {
			q: String
		}
		`).assertInvalid("the Query type is missing the 'service' field", ValidateSchema)
	})
}

func TestRootObjectNaming(t *testing.T) {
	t.Run("default schema definition is valid", func(t *testing.T) {
		withSchema(t, `
		type Gizmo {
			id: ID!
			name: String!
		}

		input GizmoInput {
			id: ID!
			name: String
		}

		type Query {
			gizmo(someIdentifier: ID!): Gizmo
		}
		type Mutation {
			updateGizmo(gizmo: GizmoInput!): Gizmo
		}`).assertValid(validateRootObjectNames)
	})
	t.Run("overriding query is not valid", func(t *testing.T) {
		withSchema(t, `
		type Gizmo {
			id: ID!
			name: String!
		}

		schema {
			query: QueryObj
		}

		type QueryObj {
			gizmo(someIdentifier: ID!): Gizmo
		}`).assertInvalid("the schema Query type can not be renamed to QueryObj", validateRootObjectNames)
	})
	t.Run("overriding mutation is not valid", func(t *testing.T) {
		withSchema(t, `
		type Gizmo {
			id: ID!
			name: String!
		}

		input GizmoInput {
			id: ID!
			name: String
		}

		schema {
			mutation: MutObj
		}

		type MutObj {
			updateGizmo(gizmo: GizmoInput!): Gizmo
		}`).assertInvalid("the schema Mutation type can not be renamed to MutObj", validateRootObjectNames)
	})
	t.Run("overriding subscription is not valid", func(t *testing.T) {
		withSchema(t, `
		type Gizmo {
			id: ID!
			name: String!
		}

		schema {
			subscription: SubObj
		}

		type SubObj {
			gizmos: Gizmo
		}`).assertInvalid("the schema Subscription type can not be renamed to SubObj", validateRootObjectNames)
	})
}

func TestSchemaValidAfterMerge(t *testing.T) {
	t.Run("invalid use of Servive type", func(t *testing.T) {
		withSchema(t, `
		type Service {
			name: String!
			version: String!
			schema: String!
		}

		type Query {
			service: Service!
		}

		type Mutation {
			service: Service!
		}`).assertInvalid("schema will become invalid after merge operation: merged schema:2: Undefined type Service.", validateSchemaValidAfterMerge)
	})

	t.Run("valid schema with empty Query type", func(t *testing.T) {
		withSchema(t, `
		type Service {
			name: String!
			version: String!
			schema: String!
		}

		type Query {
			service: Service!
		}

		type Mutation {
			a: String!
		}`).assertValid(validateSchemaValidAfterMerge)
	})
}

func TestSchemaValidateBoundaryFields(t *testing.T) {
	t.Run("valid boundary field", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Bar @boundary {
			id: ID!
		}

		type Query {
			foo(id: ID!): Foo @boundary
			barGetter(id: ID!): Bar @boundary
		}
		`).assertValid(validateBoundaryFields)
	})

	t.Run("missing boundary field", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Bar @boundary {
			id: ID!
		}

		type Query {
			foo(id: ID!): Foo @boundary
		}
		`).assertInvalid("missing boundary fields for the following types: [Bar]", validateBoundaryFields)
	})

	t.Run("boundary field for non-boundary type", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo {
			id: ID!
		}

		type Query {
			foo(id: ID!): Foo @boundary
		}
		`).assertInvalid(`declared boundary query for non-boundary type "Foo"`, validateBoundaryFields)
	})

	t.Run("valid boundary fields", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Bar @boundary {
			id: ID!
		}

		type Query {
			foo(id: ID!): Foo @boundary
			barGetter(id: ID!): Bar @boundary
		}
		`).assertValid(validateBoundaryFields)
	})

	t.Run("valid array boundary field", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Query {
			foo(ids: [ID!]!): [Foo]! @boundary
		}
		`).assertValid(validateBoundaryFields)
	})

	t.Run("invalid array boundary query", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Bar @boundary {
			id: ID!
		}

		type Query {
			foo(ids: [ID!]): [Foo!] @boundary
		}
		`).assertInvalid(`invalid boundary query "foo": boundary list query must accept an argument of type "[ID!]!"`, validateBoundaryQueries)
	})

	t.Run("non-nullable boundary query result", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Bar @boundary {
			id: ID!
		}

		type Query {
			foo(id: ID!): Foo! @boundary
		}
		`).assertInvalid(`invalid boundary query "foo": return type of boundary query should be nullable`, validateBoundaryQueries)
	})

	t.Run("don't allow duplicated boundary getter", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Query {
			foo(id: ID!): Foo @boundary
			severalFoos(ids: [ID!]!): [Foo]! @boundary
		}
		`).assertInvalid(`declared duplicate query for boundary type "Foo"`, validateBoundaryFields)
	})

	t.Run("requires at least one argument", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Query {
			foo: Foo @boundary
		}
		`).assertInvalid(`boundary field "foo" expects exactly one argument`, validateBoundaryFields)
	})

	t.Run("requires exactly one argument", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Query {
			foo(id: ID!, scope: String): Foo @boundary
		}
		`).assertInvalid(`boundary field "foo" expects exactly one argument`, validateBoundaryFields)
	})
}

func TestSchemaValidateBoundaryObjectsFormat(t *testing.T) {
	t.Run("valid boundary objects", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			id: ID!
		}

		type Bar @boundary {
			id: ID!
		}
		`).assertValid(validateBoundaryObjectsFormat)
	})

	t.Run("missing id field", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT | FIELD_DEFINITION

		type Foo @boundary {
			foo: String
		}
		`).assertInvalid(`missing "id: ID!" field in boundary type "Foo"`, validateBoundaryObjectsFormat)
	})
}
