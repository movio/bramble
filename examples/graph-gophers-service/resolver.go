package main

import (
	"errors"
	"strings"

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

// Interface types must have a "Too$TYPE" method for each of the type that
// implements them
type Node struct {
	ID graphql.ID

	// one way of achieving that is to store pointers to the different possible
	// types
	foo *foo
}

func (n *Node) ToFoo() (*foo, bool) {
	if n.foo != nil {
		return n.foo, true
	}

	return nil, false
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

func (r *resolver) Node(args struct {
	ID graphql.ID
}) (*Node, error) {
	nodeType, _, err := decodeID(string(args.ID))
	if err != nil {
		return nil, err
	}

	if nodeType == "Foo" {
		return &Node{
			ID: args.ID,
			foo: &foo{
				ID:           args.ID,
				GraphGophers: true,
			},
		}, nil
	}

	return nil, nil
}

func decodeID(id string) (string, string, error) {
	// The id format can be anything but must be global and should usually
	// contain the type and id. Here we use "type:id"
	parts := strings.Split(id, ":")
	if len(parts) != 2 {
		return "", "", errors.New("invalid id")
	}
	return parts[0], parts[1], nil
}
