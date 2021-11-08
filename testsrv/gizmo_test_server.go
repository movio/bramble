package testsrv

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

type gizmo struct {
	IDField   string
	NameField string
}

func (u gizmo) ID() graphql.ID {
	return graphql.ID(u.IDField)
}

func (u gizmo) Name() string {
	return u.NameField
}

var gizmos = []*gizmo{
	{
		IDField:   "GIZMO1",
		NameField: "Gizmo #1",
	},
	{
		IDField:   "GIZMO2",
		NameField: "Gizmo #2",
	},
	{
		IDField:   "GIZMO3",
		NameField: "Gizmo #3",
	},
}

var gizmosMap = make(map[string]*gizmo)

func init() {
	for _, gizmo := range gizmos {
		gizmosMap[gizmo.IDField] = gizmo
	}
}

type gizmoServiceResolver struct {
	serviceField service
}

func (g *gizmoServiceResolver) Service() service {
	return g.serviceField
}

func (g *gizmoServiceResolver) Gizmo(args struct{ ID string }) (*gizmo, error) {
	gizmo, ok := gizmosMap[args.ID]
	if !ok {
		return nil, errors.New("not found")
	}
	return gizmo, nil
}

func (g *gizmoServiceResolver) BoundaryGizmo(args struct{ ID string }) (*gizmo, error) {
	gizmo, ok := gizmosMap[args.ID]
	if !ok {
		return nil, errors.New("not found")
	}
	return gizmo, nil
}

func NewGizmoService() *httptest.Server {
	s := `
	directive @boundary on OBJECT | FIELD_DEFINITION

	type Query {
		service: Service!
		gizmo(id: ID!): Gizmo!
		boundaryGizmo(id: ID!): Gizmo @boundary
	}

	type Gizmo @boundary {
		id: ID!
		name: String!
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
