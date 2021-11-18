# Federation Specification

## Federation Syntax

### `service` root Query field

Bramble requires each federated service to expose a root field `service` of type `Service`, as follows:

```graphql
type Service {
  name: String!
  version: String!
  schema: String!
}

type Query {
  service: Service!
}
```

- the `name` field should be a unique, machine-readable name for the service (it will appear in logs)
- the `version` field should identify the version of the running service (it will appear in logs)
- the `schema` field should contain the full GraphQL schema (i.e. type system definition language) of the service as a string

An example query / response might be:

```graphql
{
  service {
    name
    version
    schema
  }
}
```

```json
{
  "data": {
    "service": {
      "name": "service-a",
      "version": "v0.0.1",
      "schema": "directive @boundary on OBJECT | FIELD_DEFINITION\ntype Movie @boundary {\n\tid: ID!\n\ttitle: String!\n\tgenre: MovieGenre!\n}\nenum MovieGenre {\n\tACTION @deprecated(reason: \"testing, yo\")\n\tCOMEDY\n\tHORROR\n\tDRAMA\n\tANIMATION\n\tADVENTURE\n\tSCIENCE_FICTION\n}\ntype Query {\n\tservice: Service!\n\tmovie(id: ID!): Movie! @boundary\n\tmovies: [Movie!]!\n}\ntype Service {\n\tname: String!\n\tversion: String!\n\tschema: String!\n}\n"
    }
  }
}
```

### Boundary Directive

A service may optionally define the `boundary` directive, and use it in object definitions.

```graphql
directive @boundary on OBJECT | FIELD_DEFINITION

type Gizmo @boundary {
  id: ID!
  size: Float!
}
```

Additionally, a service that defines objects with a `@boundary` directive _must_ implement boundary queries for all boundary objects, as follows:

```graphql
type Query {
  gizmo(id: ID!): Gizmo @boundary
}
```

### Namespace Directive

The `namespace` directive allows services to share a type for the means of namespacing.
Namespace types do not resolve to any service, they live only on the gateway.

Multiple namespace types can declare a field with the same name as long as the following conditions are respected:

- the field's type is also a namespace
- the field doesn't take any argument
- the field is non nullable

Types with the `namespace` directive _must_ end with either `Query`, `Mutation` or `Subscription` depending on where they are used.
As a consequence a namespace type can only be used for one kind of operation.

`Query`, `Mutation` and `Subscription` are implicit namespace types.

- Service A

```graphql
directive @namespace on OBJECT

type MyNamespaceQuery @namespace {
  subnamespace: SubNamespaceQuery!
}

type SubNamespaceQuery @namespace {
  someField: String!
}

type Query {
  myNamespace: MyNamespaceQuery!
}
```

- Service B

```graphql
directive @namespace on OBJECT

type MyNamespaceQuery @namespace {
  subnamespace: SubNamespaceQuery!
}

type SubNamespaceQuery @namespace {
  someOtherField: String!
}

type Query {
  myNamespace: MyNamespaceQuery!
}
```

### Restriction on `schema`

Bramble currently does not support the `schema` construct to rename the `Query`, `Mutation`, and `Subscription` root types.

### Restriction on `Subscription`

Bramble currently does not support `subscription` operations.

### Federation Syntax FAQ

- **Q**: _Is it possible to use the `@boundary` directive on other type definitions like unions, interfaces, and input objects?_

  **A**: Not at this time.

- **Q**: _Does bramble support custom scalars?_

  **A**: Yes, services can define custom scalars. Bramble will forward the value as is.

- **Q**: _Does bramble support custom directives?_

  **A**: Not at this time.

- **Q**: _Is it possible for a type defined in one service to implement an interface defined in another service?_

  **A**: No, this would require additional syntax that Bramble doesn't currently have.

- **Q**: _Is it possible to use the `extend` syntax on a type defined in another service?_

  **A**: No, this would require additional syntax that Bramble doesn't currently have.

# Federation Semantics

The federation semantics is specified in two steps. First, we define how the federated service schemas are merged into a single schema, then we define how fields in the merged schema are resolved.

## Schema Merge

The schema merge operation is a binary, associative, and commutative operation. To merge more than two schemas, the merge operation is repeatedly performed until a single schema is obtained.

### Scalars

The merged schema contains the standard scalars (`Int`, `Float`, `String`, `ID`, and `Boolean`) as well as custom scalars defined in federated services.

### Directives

Since Bramble currently doesn't support custom directives in federated services, the merged schema's directives are the standard `@skip`, `@include`, `@deprecated`, as well as `@boundary`.

### Interfaces, Unions, Input Objects, and Enums

The merged schema contains all interfaces, unions, input objects, and enums defined in federated services. Their definitions are unchanged. None of their names may overlap or the merge operation will fail.

### Non boundary Objects

Object definitions that do not have the `@boundary` directive are merged in the same way as interfaces, unions, input objects, and enums.

### Boundary Objects

Object definitions that have the `@boundary` directive and that have the same name are merged as a single object definition in the merged schema. This object definition merge is a binary operation that is associative and commutative. The resulting object definition `M` from the merge of the object definitions `A` and `B` is defined as follows:

1. the name of `M` is the same as that of `A` and `B`
1. its description contains both `A` and `B`'s descriptions, separated with a blank line
1. it has the `@boundary` directive and only that directive
1. it implements all of `A` and `B`'s interfaces
1. it has an `id` field of type `ID!`, the name of which [may be customised](/configuration)
1. it has all of `A` and `B`'s fields, none of which may overlap (except for the `id` field)
1. its copied fields from `A` and `B` are not modified (type, arguments, description, etc.)

### Namespace Objects

Similar to boundary objects, object definitions that have the `@namespace` directive and that have the same name are merged as a single object definition.
The resulting object definition `M` from the merge of the object definitions `A` and `B` is defined as follows:

1. the name of `M` is the same as that of `A` and `B`
1. its description contains both `A` and `B`'s descriptions, separated with a blank line
1. it has the `@namespace` directive and only that directive
1. it implements all of `A` and `B`'s interfaces
1. it has all of `A` and `B`'s fields. Fields may overlap if:

- they have the same type and that type is also a namespace
- they do not accept any argument
- they are non nullable

## Field Resolution

Bramble's field resolution semantics is quite easy to define, thanks to its simple design. From the section above you can see that the following is true:

> **With the exception of namespaces and the `id` field of boundary objects, every field in the merged schema is defined in exactly one federated service.**

Because all fields in the graph are mutually exclusive (with the exception of boundary `id` fields which are mutually consistent), every field in the merged schema has exactly one resolver. Therefore, the semantics of resolving fields among merged schemas follows normal GraphQL patterns. Field resolvers are simply distributed among services, and the gateway handles routing field requests to their appropraite resolver locations.

All boundary object types across services must resolve an `id` field (or an [alternate key field name](/configuration) used across the graph). The resolved values of these key fields must be consistent across services, and will be used to cross-reference portions of a merged object.
