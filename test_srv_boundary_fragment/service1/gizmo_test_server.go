package testsrv1

import (
	"errors"
	"net/http/httptest"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
)

type service struct {
	Name    string
	Version string
	Schema  string
}

var jetpackMap = map[string]*jetpack{
	"JETPACK1": {
		IDField:          "JETPACK1",
		DescriptionField: "Jetpack #1 Description",
	},
}

var invisibleCarMap = map[string]*invisibleCar{
	"AM1": {
		IDField:          "AM1",
		PerformanceField: 100,
	},
}

type gizmoServiceResolver struct {
	serviceField service
}

func (g *gizmoServiceResolver) Service() service {
	return g.serviceField
}

func (g *gizmoServiceResolver) BoundaryJetpack(args struct{ ID string }) (*jetpack, error) {
	jetpack, ok := jetpackMap[args.ID]
	if !ok {
		return nil, errors.New("jetpack not found")
	}
	return jetpack, nil
}

func (g *gizmoServiceResolver) BoundaryInvisibleCar(args struct{ ID string }) (*invisibleCar, error) {
	invisibleCar, ok := invisibleCarMap[args.ID]
	if !ok {
		return nil, errors.New("invisible car not found")
	}
	return invisibleCar, nil
}

type invisibleCar struct {
	IDField          string
	PerformanceField int32
}

func (j invisibleCar) ID() graphql.ID {
	return graphql.ID(j.IDField)
}

func (j invisibleCar) Performance() int32 {
	return j.PerformanceField
}

type jetpack struct {
	IDField          string
	DescriptionField string
}

func (j jetpack) ID() graphql.ID {
	return graphql.ID(j.IDField)
}

func (j jetpack) Description() string {
	return j.DescriptionField
}

func NewGizmoService() *httptest.Server {
	s := `
	directive @boundary on OBJECT | FIELD_DEFINITION

	type Query {
		service: Service!

		boundaryJetpack(id: ID!): Jetpack @boundary
		boundaryInvisibleCar(id: ID!): InvisibleCar @boundary
	}

	type Jetpack @boundary {
		id: ID!
		description: String!
	}

	type InvisibleCar @boundary {
		id: ID!
		performance: Int!
	}

	type Service {
		name: String!
		version: String!
		schema: String!
	}`

	schema := graphql.MustParseSchema(s, &gizmoServiceResolver{
		serviceField: service{
			Name:    "gizmo-service",
			Version: "v0.0.1",
			Schema:  s,
		},
	}, graphql.UseFieldResolvers())

	return httptest.NewServer(&relay.Handler{Schema: schema})
}
