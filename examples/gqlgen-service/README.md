# Example gqlgen based service

This is an example service that exposes a very simple schema:

    type Gizmo {
        id: ID!
        name: String!
    }

Other example services will add other fields to the `Gizmo` object.

_Note: we have not added `gqlgen` related generated files to git; must `go generate .` before use_
