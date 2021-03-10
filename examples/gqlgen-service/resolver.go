package main

import (
	"context"
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
		ID:     id,
		Gqlgen: true,
	}
	return &foo, nil
}

func (r *queryResolver) RandomFoo(ctx context.Context) (*Foo, error) {
	id := strconv.Itoa(rand.Intn(100))
	foo := Foo{
		ID:     id,
		Gqlgen: true,
	}
	return &foo, nil
}
