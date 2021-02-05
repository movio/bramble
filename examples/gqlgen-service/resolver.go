package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/vektah/gqlparser/v2/formatter"
)

type Resolver struct{}

func (r *Resolver) Query() QueryResolver {
	return &queryResolver{r}
}

type queryResolver struct{ *Resolver }

const FOO_TYPENAME = "Foo"

func (r *queryResolver) Node(ctx context.Context, id string) (Node, error) {
	nodeType, nodeID, err := decodeID(id)
	if err != nil {
		return nil, err
	}

	switch nodeType {
	case FOO_TYPENAME:
		return r.Foo(ctx, nodeID)
	default:
		return nil, fmt.Errorf("unknown type %s", nodeType)
	}
}

func (r *queryResolver) Service(ctx context.Context) (*Service, error) {
	s := new(strings.Builder)
	f := formatter.NewFormatter(s)
	// parsedSchema is in the generated code
	f.FormatSchema(parsedSchema)

	service := Service{
		Name:    "gqlgen-service",
		Version: "0.1.0",
		Schema:  s.String(),
	}
	return &service, nil
}

func (r *queryResolver) Foo(ctx context.Context, id string) (*Foo, error) {
	foo := Foo{
		ID:     encodeID(FOO_TYPENAME, id),
		Gqlgen: true,
	}
	return &foo, nil
}

func (r *queryResolver) RandomFoo(ctx context.Context) (*Foo, error) {
	id := strconv.Itoa(rand.Intn(100))
	foo := Foo{
		ID:     encodeID(FOO_TYPENAME, id),
		Gqlgen: true,
	}
	return &foo, nil
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

func encodeID(typename, id string) string {
	return fmt.Sprintf("%s:%s", typename, id)
}
