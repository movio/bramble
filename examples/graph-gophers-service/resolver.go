package main

import (
	_ "embed"
	"net/http"

	"github.com/go-faker/faker/v4"
	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
)

var name = "graph-gophers-service"
var version = "0.1.0"

//go:embed schema.graphql
var schema string

func newResolver() http.Handler {
	return &relay.Handler{
		Schema: graphql.MustParseSchema(schema, &resolver{
			Service: service{
				Name:    name,
				Version: version,
				Schema:  schema,
			},
			emails: make(map[graphql.ID]string),
		}, graphql.UseFieldResolvers())}
}

type service struct {
	Name    string
	Version string
	Schema  string
}

type gizmo struct {
	ID    graphql.ID
	Email string
}

type resolver struct {
	Service service
	emails  map[graphql.ID]string
}

func (r *resolver) fetchEmailById(id graphql.ID) string {
	email, ok := r.emails[id]
	if !ok {
		email = faker.Email()
		r.emails[id] = email
	}
	return email
}

func (r *resolver) Gizmos(args struct {
	IDs []graphql.ID
}) ([]*gizmo, error) {
	gizmos := []*gizmo{}
	for _, id := range args.IDs {
		gizmos = append(gizmos, &gizmo{
			ID:    id,
			Email: r.fetchEmailById(id),
		})
	}
	return gizmos, nil
}
