package bramble

import (
	"context"
	"fmt"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// Service is a federated service.
type Service struct {
	ServiceURL   string
	Name         string
	Version      string
	SchemaSource string
	Schema       *ast.Schema
	Status       string

	tracer trace.Tracer
	client *GraphQLClient
}

// NewService returns a new Service.
func NewService(serviceURL string, opts ...ClientOpt) *Service {
	opts = append(opts, WithUserAgent(GenerateUserAgent("update")))
	s := &Service{
		ServiceURL: serviceURL,
		tracer:     otel.GetTracerProvider().Tracer(instrumentationName),
		client:     NewClientWithoutKeepAlive(opts...),
	}
	return s
}

// Update queries the service's schema, name and version and updates its status.
func (s *Service) Update(ctx context.Context) (bool, error) {
	req := NewRequest("query brambleServicePoll { service { name, version, schema} }").
		WithOperationName("brambleServicePoll")

	ctx, span := s.tracer.Start(ctx, "Federated Service Schema Update",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			semconv.GraphqlOperationTypeQuery,
			semconv.GraphqlOperationName(req.OperationName),
			semconv.GraphqlDocument(req.Query),
			attribute.String("graphql.federation.service", s.Name),
		),
	)

	defer span.End()

	response := struct {
		Service struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Schema  string `json:"schema"`
		} `json:"service"`
	}{}

	if err := s.client.Request(ctx, s.ServiceURL, req, &response); err != nil {
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
