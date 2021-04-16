package bramble

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

type PlanTestFixture struct {
	Schema     string
	Locations  map[string]string
	IsBoundary map[string]bool
}

var PlanTestFixture1 = &PlanTestFixture{
	Schema: `
	directive @boundary on OBJECT | FIELD_DEFINITION

	enum Language {
		French
		English
		Italian
	}

	type Movie @boundary {
		id: ID!
		title(language: Language): String!
		compTitles(limit: Int!): [Movie!]!
	}

	type Transaction @boundary {
		id: ID!
		gross: Float!
	}

	type Query {
		movies: [Movie!]!
		transactions: [Transaction!]!
	}`,

	Locations: map[string]string{
		"Query.movies":       "A",
		"Query.transactions": "C",
		"Movie.title":        "A",
		"Movie.compTitles":   "B",
		"Transaction.id":     "C",
		"Transaction.gross":  "C",
	},

	IsBoundary: map[string]bool{
		"Movie":       true,
		"Transaction": false,
	},
}

var PlanTestFixture2 = &PlanTestFixture{
	Schema: `
	type Gizmo {
		id: ID!
		name: String!
		gadgets: [Gadget!]!
	}

	type Gadget {
		id: ID!
		name: String!
		gimmicks: [Gimmick!]!
	}

	type Gimmick {
		id: ID!
		name: String!
	}

	type Query {
		gizmos: [Gizmo!]!
	}`,

	Locations: map[string]string{
		"Query.gizmos":    "A",
		"Gizmo.id":        "A",
		"Gizmo.name":      "A",
		"Gizmo.gadgets":   "A",
		"Gadget.id":       "A",
		"Gadget.name":     "A",
		"Gadget.gimmicks": "A",
		"Gimmick.id":      "A",
		"Gimmick.name":    "A",
	},

	IsBoundary: map[string]bool{
		"Gizmo":   false,
		"Gadget":  false,
		"Gimmick": false,
	},
}

// nolint
var PlanTestFixture3 = &PlanTestFixture{
	Schema: `
	directive @boundary on OBJECT

	interface Node {
		id: ID!
	}

	interface Animal {
		weight: Float!
		name: String!
	}

	type Lion implements Animal & Node @boundary {
		id: ID!
		weight: Float!
		name: String!
		maneColor: String!
	}

	type Snake implements Animal & Node @boundary {
		id: ID!
		weight: Float!
		name: String!
		venomous: Boolean!
	}

	type Query {
		animals: [Animal]!
	}
	`,

	Locations: map[string]string{
		"Query.animals":  "A",
		"Lion.weight":    "A",
		"Lion.name":      "A",
		"Lion.maneColor": "A",
		"Snake.weight":   "B",
		"Snake.name":     "B",
		"Snake.venomous": "B",
	},

	IsBoundary: map[string]bool{
		"Lion":  true,
		"Snake": true,
	},
}

var PlanTestFixture4 = &PlanTestFixture{
	Schema: `
		directive @boundary on OBJECT

		interface Node {
			id: ID!
		}

		type Dog { name: String! }
		type Cat { name: String! }
		type Snake { name: String! }
		union Animal = Dog | Cat | Snake

		type Query {
			animals: [Animal]!
		}
	`,

	Locations: map[string]string{
		"Query.animals": "A",
		"Dog.name":      "A",
		"Cat.name":      "A",
		"Snake.name":    "A",
	},

	IsBoundary: map[string]bool{},
}

var PlanTestFixture5 = &PlanTestFixture{
	Schema: `
	directive @boundary on OBJECT

	interface Node {
		id: ID!
	}

	type Query {
		foo: FooQuery!
	}

	type FooQuery {
		foos(cursor: ID, limit: Int): FooPage
	}

	type FooPage {
		cursor: ID
		page: [Foo!]!
	}

	type Foo implements Node @boundary {
		id: ID!
		name: String!
		size: Float
	}
	`,

	Locations: map[string]string{
		"Query.foo":      "A",
		"FooQuery.foos":  "A",
		"FooPage.cursor": "A",
		"FooPage.page":   "A",
		"Foo.name":       "A",
		"Foo.size":       "B",
	},

	IsBoundary: map[string]bool{
		"Foo": true,
	},
}

func (f *PlanTestFixture) Check(t *testing.T, query, expectedJSON string) {
	t.Helper()
	schema := gqlparser.MustLoadSchema(&ast.Source{Name: "fixture", Input: f.Schema})
	operation := gqlparser.MustLoadQuery(schema, query)
	require.Len(t, operation.Operations, 1, "bad test: query must be a single operation")
	actual, err := Plan(&PlanningContext{operation.Operations[0], schema, f.Locations, f.IsBoundary, map[string]*Service{
		"A": {Name: "A", ServiceURL: "A"},
		"B": {Name: "B", ServiceURL: "B"},
		"C": {Name: "C", ServiceURL: "C"},
	}})
	require.NoError(t, err)
	actual.SortSteps()
	assert.JSONEq(t, expectedJSON, jsonMustMarshal(actual))
}

type ByServiceURL []*QueryPlanStep

func (a ByServiceURL) Len() int           { return len(a) }
func (a ByServiceURL) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByServiceURL) Less(i, j int) bool { return a[i].ServiceURL < a[j].ServiceURL }

func (p *QueryPlan) SortSteps() {
	sort.Sort(ByServiceURL(p.RootSteps))
	for _, s := range p.RootSteps {
		s.SortSteps()
	}
}

func (s *QueryPlanStep) SortSteps() {
	sort.Sort(ByServiceURL(s.Then))
	for _, s := range s.Then {
		s.SortSteps()
	}
}

func jsonMustMarshal(data interface{}) string {
	buf, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	return string(buf)
}
