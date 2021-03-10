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

type Gizmo implements Node @boundary {
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
1. it has an `id: ID!` field
1. it has all of `A` and `B`'s fields, none of which may overlap (except for `id: ID!`)
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

> **With the exception of namespaces and the `id` field in objects with the `@boundary` directive, every field in the merged schema is defined in exactly one federated service.**

As a consequence of the statement above, with the exception of the `id` field in objects with the `@boundary` directive, every field in the merged schema has exactly one resolver. Therefore, with the exception of the `id` fields in objects with the `@boundary` directive, the semantics of resolving fields in the merged schema is identical to that of a normal GraphQL schema. The resolvers are distributed among different services, but that is an implementation concern, that does not affect the resolution semantics. Of course, this semantics definition doesn't explain _how_ Bramble executes operations and is able to invoke remote resolvers; this is covered in the _"Algorithm Definitions"_ section.

Finally, we need to define the resolution semantics of `id` fields in objects with the `@boundary` directive. First note that any service that defines the `@boundary` directive, must have a resolver for the `id` field. Also, in any query document, all such `id` fields will have a _parent field_ (i.e. it cannot be a root field). As observed before, that parent field's resolver is located in exactly one service, and that service must necessarily define the `@boundary` directive. The resolution semantics of the `id` fields in objects with the `@boundary` directive is the resolution semantics of the resolver for that `id` field in that service.
