package main

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"log"
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
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "OK")
	})

	log.Printf("example graph-gophers-service running on http://localhost:%s/", port)
	http.ListenAndServe(":"+port, r)
}
