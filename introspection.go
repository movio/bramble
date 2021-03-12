package bramble

import (
	"context"
	"fmt"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
)

// Service ...
type Service struct {
	ServiceURL   string
	Name         string
	Version      string
	SchemaSource string
	Schema       *ast.Schema
	Status       string
}

func NewService(serviceURL string) *Service {
	return &Service{
		ServiceURL: serviceURL,
	}
}

func (s *Service) Update() (bool, error) {
	req := NewRequest("{ service { name, version, schema} }")
	response := struct {
		Service struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Schema  string `json:"schema"`
		} `json:"service"`
	}{}

	client := NewClient()
	if err := client.Request(context.Background(), s.ServiceURL, req, &response); err != nil {
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
