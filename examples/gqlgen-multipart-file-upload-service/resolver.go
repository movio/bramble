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
	service Service
}

func (r *Resolver) Query() QueryResolver {
	return r
}

func (r *Resolver) Mutation() MutationResolver {
	return r
}

func (r *Resolver) Service(ctx context.Context) (*Service, error) {
	return &r.service, nil
}

func (r *Resolver) UploadGizmoFile(ctx context.Context, upload graphql.Upload) (string, error) {
	return fmt.Sprintf("%s: %d bytes %s", upload.Filename, upload.Size, upload.ContentType), nil
}
func (r *Resolver) UploadGadgetFile(ctx context.Context, input GadgetInput) (string, error) {
	upload := input.Upload
	return fmt.Sprintf("%s: %d bytes %s", upload.Filename, upload.Size, upload.ContentType), nil
}
