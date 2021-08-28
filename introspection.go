package bramble

import (
	"context"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

type SchemaStatus int

const (
	SchemaStatusUnreachable SchemaStatus = iota
	SchemaStatusError
	SchemaStatusOK
)

type Service interface {
	Name() string
	Version() string
	URL() string
	SchemaSource() string
	Schema() *ast.Schema
	Status() SchemaStatus
	Update() (updated bool, err error)
	Err() error
}

// NewService returns a new Service.
func NewService(serviceURL string) Service {
	s := &serviceImplementation{
		serviceURL: serviceURL,
		client:     NewClient(WithUserAgent(GenerateUserAgent("update"))),
	}
	return s
}

type serviceImplementation struct {
	serviceURL   string
	name         string
	version      string
	schemaSource string
	err          error
	schema       *ast.Schema
	status       SchemaStatus
	client       *GraphQLClient
}

func (s *serviceImplementation) Name() string {
	return s.name
}

func (s *serviceImplementation) Version() string {
	return s.version
}

func (s *serviceImplementation) URL() string {
	return s.serviceURL
}

func (s *serviceImplementation) SchemaSource() string {
	return s.schemaSource
}

func (s *serviceImplementation) Schema() *ast.Schema {
	return s.schema
}

func (s *serviceImplementation) Status() SchemaStatus {
	return s.status
}

func (s *serviceImplementation) Err() error {
	return s.err
}

func (s *serviceImplementation) Update() (bool, error) {
	s.err = nil
	req := NewRequest("{ service { name, version, schema} }")
	response := struct {
		Service struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Schema  string `json:"schema"`
		} `json:"service"`
	}{}
	if err := s.client.Request(context.Background(), s.serviceURL, req, &response); err != nil {
		s.status = SchemaStatusUnreachable
		return false, err
	}
	updated := response.Service.Schema != s.schemaSource
	s.name = response.Service.Name
	s.version = response.Service.Version
	s.schemaSource = response.Service.Schema

	schema, err := gqlparser.LoadSchema(&ast.Source{Name: s.serviceURL, Input: response.Service.Schema})
	if err != nil {
		s.status = SchemaStatusError
		return false, err
	}
	s.schema = schema

	if err := ValidateSchema(s.schema); err != nil {
		s.status = SchemaStatusError
		return updated, err
	}
	s.status = SchemaStatusOK
	return updated, nil
}
