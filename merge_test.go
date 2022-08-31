package bramble

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMergeSingleSchema(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			type Service {
				name: String!
				version: String!
				schema: String!
			}

			interface Named {
				name: String!
			}

			type Gizmo implements Named {
				id: ID!
				name: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
				service: Service!
			}
		`,
		Expected: `
			interface Named {
				name: String!
			}

			type Gizmo implements Named {
				id: ID!
				name: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
			}
		`,
	}
	fixture.CheckSuccess(t)
}

func TestMergeTwoSchemasNoBoundaryTypes(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			interface Named {
				name: String!
			}

			"this is a Gizmo"
			type Gizmo implements Named {
				"this is an id"
				id: ID!
				"this is a name"
				name: String!
				old: Float! @deprecated(reason: "it's old")
			}

			type Query {
				gizmo(id: ID!): Gizmo!
			}
		`,
		Input2: `
			type Gimmick {
				id: ID!
				name: String!
			}

			type Query {
				gimmick(id: ID!): Gimmick!
			}
		`,
		Expected: `
			interface Named {
				name: String!
			}

			"this is a Gizmo"
			type Gizmo implements Named {
				"this is an id"
				id: ID!
				"this is a name"
				name: String!
				old: Float! @deprecated(reason: "it's old")
			}

			type Gimmick {
				id: ID!
				name: String!
			}

			type Query {
				gimmick(id: ID!): Gimmick!
				gizmo(id: ID!): Gizmo!
			}
		`,
	}
	fixture.CheckSuccess(t)
}

func TestMergeTwoSchemasWithCollidingInterface(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			interface Named {
				name: String!
			}

			type Gizmo implements Named {
				name: String!
				foo: Float!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
			}
		`,
		Input2: `
			interface Named {
				name: String!
			}

			type Gimmick implements Named {
				name: String!
				bar: Float!
			}

			type Query {
				gimmick(id: ID!): Gimmick!
			}
		`,
		Error: "conflicting interface: Named (interfaces may not span multiple services)",
	}
	fixture.CheckError(t)
}

func TestMergeTwoSchemasWithBoundaryTypes(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			directive @boundary on OBJECT
			interface Node { id: ID! }
			interface Named { name: String! }

			"foo"
			type Gizmo implements Node & Named @boundary {
				id: ID!
				name: String!
			}

			type Service {
				name: String!
				version: String!
				schema: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
				service: Service!
			}
		`,
		Input2: `
			directive @boundary on OBJECT
			interface Node { id: ID! }

			"bar"
			type Gizmo implements Node @boundary {
				id: ID!
				"this is a size"
				size: Float!
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
		`,
		Expected: `
			directive @boundary on OBJECT
			interface Named { name: String! }

			"""
			bar

			foo
			"""
			type Gizmo implements Named @boundary {
				id: ID!
				"this is a size"
				size: Float!
				name: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
			}
		`,
	}
	fixture.CheckSuccess(t)
}

func TestMergeBoundaryAndNamespace(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			directive @boundary on OBJECT
			directive @namespace on OBJECT

			type Service {
				name: String!
				version: String!
				schema: String!
			}

			type AnimalsQuery @namespace {
				someField: String!
			}

			type Query {
				animals: AnimalsQuery!
				service: Service!
			}
		`,
		Input2: `
			directive @boundary on OBJECT
			directive @namespace on OBJECT

			type AnimalsQuery @boundary {
				someOtherField: [String!]!
			}
		`,
		Error: `conflicting object directives, merged objects "AnimalsQuery" should both be boundary or namespaces`,
	}
	fixture.CheckError(t)
}

func TestTwoSchemaWithSharedNamespaces(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			directive @boundary on OBJECT
			directive @namespace on OBJECT
			interface Node { id: ID! }

			type Service {
				name: String!
				version: String!
				schema: String!
			}

			type Cat implements Node @boundary {
				id: ID!
				name: String!
			}

			type AnimalsQuery @namespace {
				cats: CatsQuery!
			}

			type CatsQuery @namespace{
				allCats: [Cat!]!
			}

			type AnimalsMutation @namespace {
				addAnimal(name: String!): Boolean!
			}

			type Query {
				animals: AnimalsQuery!
				service: Service!
			}

			type Mutation {
				animals: AnimalsMutation!
			}
		`,
		Input2: `
			directive @boundary on OBJECT
			directive @namespace on OBJECT
			interface Node { id: ID! }

			type Cat implements Node @boundary {
				id: ID!
			}

			type Service {
				name: String!
				version: String!
				schema: String!
			}

			type AnimalsQuery @namespace {
				species: [String!]!
				cats: CatsQuery!
			}

			type CatsQuery @namespace {
				searchCat(name: String!): Cat
			}

			type AnimalsMutation @namespace {
				addMoreLegs(name: String!): Int!
			}

			type Query {
				animals: AnimalsQuery!
				node(id: ID!): Node
				service: Service!
			}
		`,
		Expected: `
			directive @boundary on OBJECT
			directive @namespace on OBJECT

			type Cat @boundary {
				id: ID!
				name: String!
			}

			type AnimalsQuery @namespace {
				species: [String!]!
				cats: CatsQuery!
			}

			type CatsQuery @namespace {
				searchCat(name: String!): Cat
				allCats: [Cat!]!
			}

			type AnimalsMutation @namespace {
				addMoreLegs(name: String!): Int!
				addAnimal(name: String!): Boolean!
			}

			type Query {
				animals: AnimalsQuery!
			}

			type Mutation {
				animals: AnimalsMutation!
			}
		`,
	}
	fixture.CheckSuccess(t)
}

func TestNoSources(t *testing.T) {
	_, err := MergeSchemas()
	assert.Error(t, err)
}

func TestConflictingBoundaryTypes(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			directive @boundary on OBJECT
			interface Node { id: ID! }

			type Gizmo implements Node @boundary {
				id: ID!
				name: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
			}
		`,
		Input2: `
			type Gizmo {
				id: ID!
				size: Float!
			}

			type Gimmick {
				id: ID!
				name: String!
			}

			type Query {
				gimmick(id: ID!): Gimmick!
			}
		`,
		Error: "conflicting non boundary type: Gizmo",
	}
	fixture.CheckError(t)
}

func TestBoundaryTypesOverlappingFields(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			directive @boundary on OBJECT
			interface Node { id: ID! }

			type Gizmo implements Node @boundary{
				id: ID!
				name: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
			}
		`,
		Input2: `
			directive @boundary on OBJECT
			interface Node { id: ID! }

			"foo"
			type Gizmo implements Node @boundary{
				id: ID!
				name: String!
				size: Float!
			}

			type Query {
				node(id: ID!): Node
			}
		`,
		Error: "overlapping fields Gizmo : name",
	}
	fixture.CheckError(t)
}

func TestBuildFieldURLMapSingleSchema(t *testing.T) {
	loc1 := "http://location1.com/query"
	fixture := BuildFieldURLMapFixture{
		Schema1: `
			interface Named {
				name: String!
			}

			type Gizmo implements Named {
				id: ID!
				name: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
			}
		`,
		Location1: loc1,
		Expected: FieldURLMap{
			"Query.gizmo": loc1,
			"Named.name":  loc1,
			"Gizmo.id":    loc1,
			"Gizmo.name":  loc1,
		},
	}
	fixture.Check(t)
}

func TestBuildFieldURLMapTwoSchemasNoBoundaryType(t *testing.T) {
	loc1 := "http://location1.com/query"
	loc2 := "http://location2.com/query"
	fixture := BuildFieldURLMapFixture{
		Schema1: `
			interface Named {
				name: String!
			}

			type Gizmo implements Named {
				id: ID!
				name: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
			}
		`,
		Location1: loc1,
		Schema2: `
			interface Sized {
				size: Float!
			}

			type Gimmick implements Sized {
				id: ID!
				size: Float!
			}

			type Query {
				gimmick(id: ID!): Gimmick!
			}
		`,
		Location2: loc2,
		Expected: FieldURLMap{
			"Query.gizmo":   loc1,
			"Query.gimmick": loc2,
			"Gizmo.id":      loc1,
			"Gizmo.name":    loc1,
			"Gimmick.id":    loc2,
			"Gimmick.size":  loc2,
			"Named.name":    loc1,
			"Sized.size":    loc2,
		},
	}
	fixture.Check(t)
}

func TestBuildFieldURLMapTwoSchemasWithBoundaryType(t *testing.T) {
	loc1 := "http://location1.com/query"
	loc2 := "http://location2.com/query"
	fixture := BuildFieldURLMapFixture{
		Schema1: `
			directive @boundary on OBJECT

			interface Named {
				name: String!
			}

			type Gizmo implements Named @boundary {
				id: ID!
				name: String!
			}

			type Service {
				name: String!
				version: String!
				schema: String!
			}

			type Query {
				gizmo(id: ID!): Gizmo!
				service: Service!
			}
		`,
		Location1: loc1,
		Schema2: `
			directive @boundary on OBJECT

			interface Sized {
				size: Float!
			}

			interface Node {
				id: ID!
			}

			type Gizmo implements Sized @boundary {
				id: ID!
				size: Float!
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
		`,
		Location2: loc2,
		Expected: FieldURLMap{
			"Query.gizmo": loc1,
			"Gizmo.name":  loc1,
			"Gizmo.size":  loc2,
			"Named.name":  loc1,
			"Sized.size":  loc2,
			"Node.id":     loc2,
		},
	}
	fixture.Check(t)
}

func TestMergeSupportsValidUnions(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			type Dog { name: String! }
			type Cat { name: String! }
			type Snake { name: String! }
			union Animal = Dog | Cat | Snake

			type Query {
				animals: [Animal]!
			}
		`,
		Input2: `
			type Circle { area: Float! }
			type Triangle { area: Float! }
			type Square { area: Float! }
			union Shape = Circle | Triangle | Square

			type Query {
				shapes: [Shape]!
			}
		`,
		Expected: `
			type Dog { name: String! }
			type Cat { name: String! }
			type Snake { name: String! }
			union Animal = Dog | Cat | Snake

			type Circle { area: Float! }
			type Triangle { area: Float! }
			type Square { area: Float! }
			union Shape = Circle | Triangle | Square

			type Query {
				shapes: [Shape]!
				animals: [Animal]!
			}
		`,
	}
	fixture.CheckSuccess(t)
}

func TestHandlesMutationServiceFieldMap(t *testing.T) {
	loc1 := "http://location1.com/query"
	loc2 := "http://location2.com/query"
	fixture := BuildFieldURLMapFixture{
		Schema1: `
		type Mutation {
				addGizmo(name: String!): ID!
		}`,
		Location1: loc1,
		Schema2: `
        type Mutation {
			addWidget(name: String): ID!
        }`,
		Location2: loc2,
		Expected: FieldURLMap{
			"Mutation.addGizmo":  loc1,
			"Mutation.addWidget": loc2,
		},
	}
	fixture.Check(t)
}

func TestHandlesMutationServices(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
		type Mutation {
				addGizmo(name: String!): ID!
		}`,
		Input2: `
		type Mutation {
				addWidget(name: String!): ID!
		}`,
		Expected: `
			type Mutation {
				addWidget(name: String!): ID!
				addGizmo(name: String!): ID!
			}
		`,
	}
	fixture.CheckSuccess(t)
}

func TestMergeHandlesUnionConflict(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			type Dog1 { name: String! }
			type Cat1 { name: String! }
			type Snake1 { name: String! }
			union Animal = Dog1 | Cat1 | Snake1

			type Query {
				animals: [Animal]!
			}
		`,
		Input2: `
			type Dog2 { name: String! }
			type Cat2 { name: String! }
			type Snake2 { name: String! }
			union Animal = Dog2 | Cat2 | Snake2

			type Query {
				foo: String!
			}
		`,
		Error: "conflicting non boundary type: Animal",
	}
	fixture.CheckError(t)
}

func TestMergeTwoSchemasWithCustomRootTypes(t *testing.T) {
	t.Skip("not supported at this time")
	fixture := MergeTestFixture{
		Input1: `
			directive @boundary on OBJECT
			interface Node { id: ID! }
			interface Named { name: String! }

			type Gizmo implements Node & Named @boundary {
				id: ID!
				name: String!
			}

			type Service {
				name: String!
				version: String!
				schema: String!
			}

			schema {
				query: QueryObj
			}

			type QueryObj {
				gizmo(id: ID!): Gizmo!
				node(id: ID!): Node
				service: Service!
			}
		`,
		Input2: `
			directive @boundary on OBJECT
			interface Node { id: ID! }

			type Gizmo implements Node @boundary {
				id: ID!
				size: Float!
			}

			type Service {
				name: String!
				version: String!
				schema: String!
			}

			type Query {
				gimmick(id: ID!): String
				node(id: ID!): Node
				service: Service!
			}
			`,
		Error: "renaming root types is currently unsupported",
		// If it worked this would be the expected schema
		Expected: `
			directive @boundary on OBJECT
			interface Named { name: String! }

			type Gizmo implements Named @boundary {
				id: ID!
				size: Float!
				name: String!
			}

			type Query {
				gimmick(id: ID!): String
				gizmo(id: ID!): Gizmo!
			}
		`,
	}
	fixture.CheckError(t)
}

func TestRejectsConflictingMutations(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
            type Mutation {
				addGizmo(name: String!): ID!
            }

		`,
		Input2: `
            type Mutation {
				addGizmo(name: String!): ID!
            }
		`,
		Error: "overlapping namespace fields Mutation : addGizmo",
	}
	fixture.CheckError(t)
}

func TestMergeCustomScalars(t *testing.T) {
	fixture := MergeTestFixture{
		Input1:   `scalar MyCustomScalar`,
		Input2:   `scalar MyCustomScalar`,
		Expected: `scalar MyCustomScalar`,
	}
	fixture.CheckSuccess(t)
}

func TestMergeEmptyQuery(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			type Service {
				name: String!
				version: String!
				schema: String!
			}

            type Query {
				service: Service!
            }

			type Mutation {
				addGizmo(name: String!): ID!
			}
		`,
		Input2: `
			type Service {
				name: String!
				version: String!
				schema: String!
			}

            type Query {
				service: Service!
            }

			type Mutation {
				updateGizmo(name: String!): ID!
			}
		`,
		Expected: `type Mutation {
			updateGizmo(name: String!): ID!
			addGizmo(name: String!): ID!
		}`,
	}
	fixture.CheckSuccess(t)
}

func TestMergeRemovesCustomDirectives(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			interface Node { id: ID! }
			directive @boundary on OBJECT

			directive @myObjectDirective on OBJECT
			directive @myFieldDirective on FIELD | FIELD_DEFINITION

            type Query @myObjectDirective {
				name: String! @myFieldDirective @deprecated
            }

			type MyBoundaryType implements Node @boundary @myObjectDirective {
				id: ID! @myFieldDirective
				firstName: String @myFieldDirective
			}

			type ServiceAType {
				field: String @myFieldDirective
			}
		`,
		Input2: `
			interface Node { id: ID! }
			directive @boundary on OBJECT

			directive @myObjectDirective on OBJECT
			directive @myFieldDirective on FIELD | FIELD_DEFINITION

			type MyBoundaryType implements Node @boundary @myObjectDirective {
				id: ID! @myFieldDirective
				lastName: String @myFieldDirective
			}

			type ServiceBType {
				field: String @myFieldDirective
			}
		`,
		Expected: `
			directive @boundary on OBJECT

            type Query {
				name: String! @deprecated
            }

			type MyBoundaryType @boundary {
				id: ID!
				lastName: String
				firstName: String
			}

			type ServiceAType {
				field: String
			}

			type ServiceBType {
				field: String
			}
		`,
	}
	fixture.CheckSuccess(t)
}

func TestMergeWithAlternateId(t *testing.T) {
	IdFieldName = "gid"
	fixture := MergeTestFixture{
		Input1: `
			directive @boundary on OBJECT | FIELD_DEFINITION
			type Dog @boundary {
				gid: ID!
				name: String
			}
			type Query {
				dog(gid: ID!): Dog @boundary
				doggie: Dog
			}
		`,
		Input2: `
			directive @boundary on OBJECT | FIELD_DEFINITION
			type Dog @boundary {
				gid: ID!
				color: String
			}
			type Query {
				dogs(gids: [ID!]!): [Dog]! @boundary
			}
		`,
		Expected: `
			directive @boundary on OBJECT | FIELD_DEFINITION
			type Dog @boundary {
				gid: ID!
				color: String
				name: String
			}
			type Query {
				doggie: Dog
			}
		`,
	}
	fixture.CheckSuccess(t)
	IdFieldName = "id" // reset!
}

func TestMergePossibleTypes(t *testing.T) {
	fixture := MergeTestFixture{
		Input1: `
			directive @boundary on OBJECT | FIELD_DEFINITION

			interface Face {
				id: ID!
			}

			type Foo implements Face @boundary {
				id: ID!
				foo: Boolean!
			}

			type Bar implements Face {
				id: ID!
				bar: Boolean!
			}

			type Query  {
				faces: [Face!]!
				face(id: ID!): Face @boundary
				service: Service!
			}

			type Service {
				name: String!
				version: String!
				schema: String!
			}
		`,
		Input2: `
			directive @boundary on OBJECT | FIELD_DEFINITION

			type Foo @boundary {
				id: ID!
				superfoo: Boolean!
			}

			type Query  {
				face(id: ID!): Foo @boundary
				service: Service!
			}

			type Service {
				name: String!
				version: String!
				schema: String!
			}
	`,
		Expected: `
			directive @boundary on OBJECT | FIELD_DEFINITION

			interface Face {
				id: ID!
			}

			type Foo implements Face @boundary {
				id: ID!
				superfoo: Boolean!
				foo: Boolean!
			}

			type Bar implements Face {
				id: ID!
				bar: Boolean!
			}

			type Query  {
				faces: [Face!]!
			}
		`,
	}
	fixture.CheckSuccess(t)
}
