package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	"github.com/99designs/gqlgen/graphql/handler"
)

var name = "gqlgen-service"
var version = "0.1.0"

//go:embed schema.graphql
var schema string

func newResolver() http.Handler {
	c := Config{
		Resolvers: &Resolver{
			gizmos: generateGizmos(),
			service: Service{
				Name:    name,
				Version: version,
				Schema:  schema,
			},
		},
		Directives: DirectiveRoot{
			// Support the @boundary directive as a no-op
			Boundary: func(ctx context.Context, obj interface{}, next graphql.Resolver) (res interface{}, err error) {
				return next(ctx)
			},
		},
	}
	return handler.NewDefaultServer(NewExecutableSchema(c))
}

type Resolver struct {
	gizmos  map[string]*Gizmo
	service Service
}

func (r *Resolver) Query() QueryResolver {
	return r
}

func (r *Resolver) Service(ctx context.Context) (*Service, error) {
	return &r.service, nil
}

func (r *Resolver) Gizmo(ctx context.Context, id string) (*Gizmo, error) {
	if gizmo, ok := r.gizmos[id]; ok {
		return gizmo, nil
	}
	return nil, fmt.Errorf("no gizmo found with id: %s", id)
}

func (r *Resolver) RandomGizmo(ctx context.Context) (*Gizmo, error) {
	for _, gizmo := range r.gizmos {
		return gizmo, nil
	}
	return nil, fmt.Errorf("failed to find a gizmo")
}
