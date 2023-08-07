package main

import (
	"context"
	_ "embed"
	"net/http"
	"time"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
)

var name = "slow-service"
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
	ID graphql.ID
}

func (g *gizmo) Delay(ctx context.Context, args struct {
	Duration string
}) (*string, error) {
	duration, err := time.ParseDuration(args.Duration)
	if err != nil {
		return nil, err
	}
	t := time.Now()
	time.Sleep(duration)
	dur := time.Since(t).String()
	return &dur, nil
}

type resolver struct {
	Service service
	emails  map[graphql.ID]string
}

func (r *resolver) Gizmos(args struct {
	IDs []graphql.ID
}) ([]*gizmo, error) {
	gizmos := []*gizmo{}
	for _, id := range args.IDs {
		gizmos = append(gizmos, &gizmo{
			ID: id,
		})
	}
	return gizmos, nil
}
