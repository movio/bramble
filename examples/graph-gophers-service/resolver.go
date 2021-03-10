package main

import (
	"github.com/graph-gophers/graphql-go"
)

type service struct {
	Name    string
	Version string
	Schema  string
}

type foo struct {
	ID           graphql.ID
	GraphGophers bool
}

type resolver struct {
	Service service
}

func newResolver() *resolver {
	return &resolver{
		Service: service{
			Name:    "graph-gophers-service",
			Version: "0.1",
			Schema:  schema,
		},
	}
}

func (r *resolver) Foo(args struct {
	ID graphql.ID
}) (*foo, error) {
	return &foo{
		ID:           args.ID,
		GraphGophers: true,
	}, nil
}
