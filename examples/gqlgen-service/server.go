//go:generate go run github.com/99designs/gqlgen
package main

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/handler"
)

const defaultPort = "8080"

func init() {
	rand.Seed(time.Now().UnixNano())
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	http.Handle("/", handler.Playground("GraphQL playground", "/query"))
	c := Config{Resolvers: &Resolver{}}
	c.Directives.Boundary = func(ctx context.Context, obj interface{}, next graphql.Resolver) (res interface{}, err error) {
		return next(ctx)
	}
	http.Handle("/query", handler.GraphQL(NewExecutableSchema(c)))

	log.Printf("example gqlgen-service running on http://localhost:%s/", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
