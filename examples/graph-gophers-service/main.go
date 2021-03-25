package main

import (
	_ "embed"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
)

//go:embed schema.graphql
var schema string

const defaultPort = "8080"

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	resolver := newResolver()
	parsedSchema := graphql.MustParseSchema(schema, resolver, graphql.UseFieldResolvers())

	r := mux.NewRouter()
	r.Handle("/query", &relay.Handler{Schema: parsedSchema})

	http.ListenAndServe(":"+port, r)
}
