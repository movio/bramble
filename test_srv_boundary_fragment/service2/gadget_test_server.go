package testsrv2

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

type gizmoWithGadgetResolver struct {
	IDField string
	Gadget  *gadgetResolver
}

func (u gizmoWithGadgetResolver) ID() graphql.ID {
	return graphql.ID(u.IDField)
}

type gadgetServiceResolver struct {
	serviceField service
}

func (g *gadgetServiceResolver) Service() service {
	return g.serviceField
}

func (g *gadgetServiceResolver) BoundaryGizmo(args struct{ ID string }) (*gizmoWithGadgetResolver, error) {
	gadget, ok := gadgetMap[args.ID]
	if !ok {
		return nil, errors.New("gadget not found")
	}
	return &gizmoWithGadgetResolver{
		IDField: args.ID,
		Gadget:  &gadgetResolver{gadget},
	}, nil
}

func (g *gadgetServiceResolver) Gizmo(args struct{ ID string }) (*gizmoWithGadgetResolver, error) {
	gadget, ok := gadgetMap[args.ID]
	if !ok {
		return nil, errors.New("gadget not found")
	}
	return &gizmoWithGadgetResolver{
		IDField: args.ID,
		Gadget:  &gadgetResolver{gadget},
	}, nil
}

func (g *gadgetServiceResolver) Gadgets() ([]*gadgetResolver, error) {
	return []*gadgetResolver{{
		gadget: jetpackMap["JETPACK1"],
	}, {
		gadget: invisibleCarMap["AM1"],
	}}, nil
}

func (g *gadgetServiceResolver) BoundaryJetpack(args struct{ ID string }) (*jetpack, error) {
	jetpack, ok := jetpackMap[args.ID]
	if !ok {
		return nil, errors.New("jetpack not found")
	}
	return jetpack, nil
}

func (g *gadgetServiceResolver) BoundaryInvisibleCar(args struct{ ID string }) (*invisibleCar, error) {
	invisibleCar, ok := invisibleCarMap[args.ID]
	if !ok {
		return nil, errors.New("invisible car not found")
	}
	return invisibleCar, nil
}

type gadget interface {
	ID() graphql.ID
	Name() string
}

type gadgetResolver struct {
	gadget
}

var gadgetMap = map[string]gadget{
	"GIZMO1": &jetpack{
		IDField:    "JETPACK1",
		NameField:  "Jetpack #1",
		RangeField: "500km",
	},
	"GIZMO2": &invisibleCar{
		IDField:      "AM1",
		NameField:    "Vanquish",
		CloakedField: true,
	},
}

var jetpackMap = map[string]*jetpack{
	"JETPACK1": {
		IDField:    "JETPACK1",
		NameField:  "Jetpack #1",
		RangeField: "500km",
	},
}

var invisibleCarMap = map[string]*invisibleCar{
	"AM1": {
		IDField:      "AM1",
		NameField:    "Vanquish",
		CloakedField: true,
	},
}

type jetpack struct {
	IDField    string
	NameField  string
	RangeField string
}

func (r *gadgetResolver) ToJetpack() (*jetpack, bool) {
	jetpack, ok := r.gadget.(*jetpack)
	return jetpack, ok
}

func (j jetpack) ID() graphql.ID {
	return graphql.ID(j.IDField)
}

func (j jetpack) Name() string {
	return j.NameField
}

func (j jetpack) Range() string {
	return j.RangeField
}

type invisibleCar struct {
	IDField      string
	NameField    string
	CloakedField bool
}

func (r *gadgetResolver) ToInvisibleCar() (*invisibleCar, bool) {
	invisibleCar, ok := r.gadget.(*invisibleCar)
	return invisibleCar, ok
}

func (j invisibleCar) ID() graphql.ID {
	return graphql.ID(j.IDField)
}

func (j invisibleCar) Name() string {
	return j.NameField
}

func (j invisibleCar) Cloaked() bool {
	return j.CloakedField
}

func NewGadgetService() *httptest.Server {
	s := `
	directive @boundary on OBJECT | FIELD_DEFINITION

	type Query {
		service: Service!
		gizmo(id: ID!): Gizmo
		gadgets: [Gadget!]!
		boundaryGizmo(id: ID!): Gizmo @boundary
		boundaryJetpack(id: ID!): Jetpack @boundary
		boundaryInvisibleCar(id: ID!): InvisibleCar @boundary
	}

	type Gizmo @boundary {
		id: ID!
		gadget: Gadget
	}

	interface Gadget {
		id: ID!
		name: String!
	}

	type Jetpack implements Gadget @boundary {
		id: ID!
		name: String!
		range: String!
	}

	type InvisibleCar implements Gadget @boundary {
		id: ID!
		name: String!
		cloaked: Boolean!
	}

	type Service {
		name: String!
		version: String!
		schema: String!
	}`

	schema := graphql.MustParseSchema(s, &gadgetServiceResolver{
		serviceField: service{
			Name:    "gadget-service",
			Version: "v0.0.1",
			Schema:  s,
		},
	}, graphql.UseFieldResolvers())

	return httptest.NewServer(&relay.Handler{Schema: schema})
}
