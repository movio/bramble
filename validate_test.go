package bramble

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

type testSchema struct {
	t      *testing.T
	schema *ast.Schema
}

func withSchema(t *testing.T, schema string) *testSchema {
	t.Helper()
	return &testSchema{
		t:      t,
		schema: gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: schema}),
	}
}

func (t *testSchema) assertValid(f func(*ast.Schema) error) {
	t.t.Helper()
	assert.NoError(t.t, f(t.schema))
}

func (t *testSchema) assertInvalid(err string, f func(*ast.Schema) error) {
	t.t.Helper()
	e := f(t.schema)
	assert.Error(t.t, e)
	if e != nil {
		assert.Equal(t.t, err, e.Error())
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
		directive @boundary(incorrect: String) on FIELD
		type Filler @boundary {
			id: ID!
		}
		`).assertInvalid("@boundary directive may not take arguments", validateBoundaryObjects)
	})
	t.Run("@boundary is checked if it is used", func(t *testing.T) {
		withSchema(t, `
		directive @boundary(incorrect: String) on FIELD
		type Filler @boundary {
			id: ID!
		}
		`).assertInvalid("@boundary directive may not take arguments", ValidateSchema)
	})
}

func TestNamespaceDirectiveRequirements(t *testing.T) {
	t.Run("invalid @namespace directive", func(t *testing.T) {
		withSchema(t, `
		directive @namespace(incorrect: String) on OBJECT

		type NamespaceQuery @namespace {
			someField: String!
		}
		`).assertInvalid("@namespace directive may not take arguments", ValidateSchema)
	})
	t.Run("invalid namespace type name (query)", func(t *testing.T) {
		withSchema(t, `
		directive @namespace on OBJECT

		type Namespace @namespace {
			someField: String!
		}

		type Query {
			ns: Namespace!
		}
		`).assertInvalid(`type "Namespace" is used as a query namespace but doesn't have the "Query" suffix`, ValidateSchema)
	})
	t.Run("invalid namespace type name (mutation)", func(t *testing.T) {
		withSchema(t, `
		directive @namespace on OBJECT

		type Namespace @namespace {
			someField: String!
		}

		type Mutation {
			ns: Namespace!
		}
		`).assertInvalid(`type "Namespace" is used as a mutation namespace but doesn't have the "Mutation" suffix`, ValidateSchema)
	})
	t.Run("invalid subnamespace type name (query)", func(t *testing.T) {
		withSchema(t, `
		directive @namespace on OBJECT

		type Subnamespace @namespace {
			someField: String!
		}

		type NamespaceQuery @namespace {
			sns: Subnamespace!
		}

		type Query {
			ns: NamespaceQuery!
		}
		`).assertInvalid(`type "Subnamespace" is used as a query namespace but doesn't have the "Query" suffix`, ValidateSchema)
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

func TestNamingConventions(t *testing.T) {
	t.Run("object fields mut be camelCase", func(t *testing.T) {
		withSchema(t, `
		type Gizmo {
			shoe_size: String!
		}
		`).assertInvalid("field 'Gizmo.shoe_size' isn't camelCase", validateNamingConventions)
	})
	t.Run("input object fields mut be camelCase", func(t *testing.T) {
		withSchema(t, `
		input GizmoInput {
			ROOF_HEIGHT: Float!
		}
		`).assertInvalid("field 'GizmoInput.ROOF_HEIGHT' isn't camelCase", validateNamingConventions)
	})
	t.Run("interface object fields mut be camelCase", func(t *testing.T) {
		withSchema(t, `
		interface Gizmolike {
			Parent: Gizmolike!
		}
		`).assertInvalid("field 'Gizmolike.Parent' isn't camelCase", validateNamingConventions)
	})
	t.Run("object field arguments and mut be camelCase", func(t *testing.T) {
		withSchema(t, `
		type Gizmo {
			shoeSize(FOO_BAR: Float!): String!
		}
		`).assertInvalid("argument 'FOO_BAR' of field 'Gizmo.shoeSize' isn't camelCase", validateNamingConventions)
	})
	t.Run("interface object field arguments mut be camelCase", func(t *testing.T) {
		withSchema(t, `
		interface Gizmolike {
			parent(FOO_BAR: Float!): Gizmolike!
		}
		`).assertInvalid("argument 'FOO_BAR' of field 'Gizmolike.parent' isn't camelCase", validateNamingConventions)
	})
	t.Run("enum values must be ALL_CAPS", func(t *testing.T) {
		withSchema(t, `
		enum Color {
			DARK_GREEN
			DARK_YELLOW
			dark_red
		}
		`).assertInvalid("enum value 'Color.dark_red' isn't ALL_CAPS", validateNamingConventions)
	})
	t.Run("object types must be PascalCase", func(t *testing.T) {
		withSchema(t, `
		type GIZMO_THING {
			id: ID!
		}
		`).assertInvalid("type 'GIZMO_THING' isn't PascalCase", validateNamingConventions)
	})
	t.Run("interface types must be PascalCase", func(t *testing.T) {
		withSchema(t, `
		interface GIZMO_LIKE {
			id: ID!
		}
		`).assertInvalid("type 'GIZMO_LIKE' isn't PascalCase", validateNamingConventions)
	})
	t.Run("input types must be PascalCase", func(t *testing.T) {
		withSchema(t, `
		input GIZMO_INPUT {
			id: ID!
		}
		`).assertInvalid("type 'GIZMO_INPUT' isn't PascalCase", validateNamingConventions)
	})
	t.Run("enum types must be PascalCase", func(t *testing.T) {
		withSchema(t, `
		enum DARK_COLORS {
			RED
			BLUE
		}
		`).assertInvalid("enum type 'DARK_COLORS' isn't PascalCase", validateNamingConventions)
	})
	t.Run("union types must be PascalCase", func(t *testing.T) {
		withSchema(t, `
		type BigBoat { name: String! }
		type BigCar { name: String! }
		union BIG_THING = BigBoat | BigCar
		`).assertInvalid("union type 'BIG_THING' isn't PascalCase", validateNamingConventions)
	})
	t.Run("called by validateSchema", func(t *testing.T) {
		withSchema(t, `
		directive @boundary on OBJECT
		interface Node {
			id: ID!
		}
		type Gizmo {
			shoe_size: Float!
		}
		type Service {
			name: String!
			version: String!
			schema: String!
		}
		type Query {
			gizmo: Gizmo!
			node(id: ID!): Node
			service: Service!
		}
		`).assertInvalid("field 'Gizmo.shoe_size' isn't camelCase", ValidateSchema)
	})
	t.Run("naming conventions should be followed", func(t *testing.T) {
		withSchema(t, `
		type BigBoat { name: String! }
		type BigCar { name: String! }
		union BigThing = BigBoat | BigCar

		enum RainbowColor { RED, DARK_RED, BLUE, DARK_BLUE }

		type Gizmo @deprecated(reason: "no gizmos") {
			id: ID!
			someField: String!
		}

		input GizmoInput @deprecated(reason: "no gizmo inputs!") {
			searchString: String @deprecated(reason: "no searching!")
		}

		interface GizmoLike {
			tags: [String!]!
		}

		type Query {
			gizmo(someIdentifier: ID!): Gizmo
		}
		`).assertValid(validateNamingConventions)
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
		}`).assertInvalid("schema will become invalid after merge operation: merged schema:3: Undefined type Service.", validateSchemaValidAfterMerge)
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
		`).assertInvalid("missing boundary queries for the following types: [Bar]", validateBoundaryFields)
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
		`).assertInvalid(`invalid boundary query "foo": return type should be a non-null array of nullable elements`, validateBoundaryQueries)
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
