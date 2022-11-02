package bramble

import (
	"context"
	"fmt"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// Service is a federated service.
type Service struct {
	ServiceURL   string
	Name         string
	Version      string
	SchemaSource string
	Schema       *ast.Schema
	Status       string

	client *GraphQLClient
}

// NewService returns a new Service.
func NewService(serviceURL string) *Service {
	s := &Service{
		ServiceURL: serviceURL,
		client:     NewClientWithoutKeepAlive(WithUserAgent(GenerateUserAgent("update"))),
	}
	return s
}

// Update queries the service's schema, name and version and updates its status.
func (s *Service) Update() (bool, error) {
	req := NewRequest("query brambleServicePoll { service { name, version, schema} }").
		WithOperationName("brambleServicePoll")
	response := struct {
		Service struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Schema  string `json:"schema"`
		} `json:"service"`
	}{}

	if err := s.client.Request(context.Background(), s.ServiceURL, req, &response); err != nil {
		s.SchemaSource = ""
		s.Status = "Unreachable"
		return false, err
	}

	updated := response.Service.Schema != s.SchemaSource

	s.Name = response.Service.Name
	s.Version = response.Service.Version
	s.SchemaSource = response.Service.Schema

	schema, err := gqlparser.LoadSchema(&ast.Source{Name: s.ServiceURL, Input: response.Service.Schema})
	if err != nil {
		s.Status = "Schema error"
		return false, err
	}
	s.Schema = schema

	if err := ValidateSchema(s.Schema); err != nil {
		s.Status = fmt.Sprintf("Invalid (%s)", err)
		return updated, err
	}

	s.Status = "OK"
	return updated, nil
}
