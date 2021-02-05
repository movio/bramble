//go:generate go run github.com/99designs/gqlgen
package main

import (
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

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
	http.Handle("/query", handler.GraphQL(NewExecutableSchema(Config{Resolvers: &Resolver{}})))

	log.Printf("example gqlgen-service running on http://localhost:%s/", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
