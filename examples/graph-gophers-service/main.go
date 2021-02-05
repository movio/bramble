package main

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
)

var schema = `
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

type Foo implements Node @boundary {
  id: ID!
  graphGophers: Boolean!
}
`

func main() {
	resolver := newResolver()
	parsedSchema := graphql.MustParseSchema(schema, resolver, graphql.UseFieldResolvers())

	r := mux.NewRouter()
	r.Handle("/query", &relay.Handler{Schema: parsedSchema})

	http.ListenAndServe(":8080", r)
}
