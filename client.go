package bramble

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	opentracing "github.com/opentracing/opentracing-go"
)

// GraphQLClient is a GraphQL client.
type GraphQLClient interface {
	Request(ctx context.Context, url string, request *Request, out interface{}) error
	UseMaxResponseSize(maxResponseSize int64)
	UseUserAgent(userAgent string)
	UseTracer(tracer opentracing.Tracer)
	UseTimeout(timeout time.Duration)
}

// DefaultGraphQLClient is the default implementation of GraphQLClient
type DefaultGraphQLClient struct {
	HTTPClient      *http.Client
	MaxResponseSize int64
	UserAgent       string
	Tracer          opentracing.Tracer
}
// ClientOpt is a function used to set a GraphQL client option
type ClientOpt func(GraphQLClient)

type defaultClientFactory func() GraphQLClient


func defaultNewClient() GraphQLClient {
	return &DefaultGraphQLClient{
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		MaxResponseSize: 1024 * 1024,
	}

}

var newClient = defaultNewClient

func OverrideDefaultNewClient(newClientFunc defaultClientFactory){
	newClient = newClientFunc
}

// NewClient creates a new GraphQLClient from the given options.
func NewClient(opts ...ClientOpt) GraphQLClient {
	c := newClient()
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WithMaxResponseSize sets the max allowed response size. The client will only
// read up to maxResponseSize and that size is exceeded an an error will be
// returned.
func WithMaxResponseSize(maxResponseSize int64) ClientOpt {
	return func(s GraphQLClient) {
		s.UseMaxResponseSize(maxResponseSize)
	}
}

// WithUserAgent set the user agent used by the client.
func WithUserAgent(userAgent string) ClientOpt {
	return func(s GraphQLClient) {
		s.UseUserAgent(userAgent)
	}
}

// Request executes a GraphQL request.
func (c *DefaultGraphQLClient) Request(ctx context.Context, url string, request *Request, out interface{}) error {
	var buf bytes.Buffer
	err := json.NewEncoder(&buf).Encode(request)
	if err != nil {
		return fmt.Errorf("unable to encode request body: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("unable to create request: %w", err)
	}

	if request.Headers != nil {
		httpReq.Header = request.Headers.Clone()
	}

	httpReq.Header.Set("Content-Type", "application/json; charset=utf-8")
	httpReq.Header.Set("Accept", "application/json; charset=utf-8")

	if c.UserAgent != "" {
		httpReq.Header.Set("User-Agent", c.UserAgent)
	}

	if c.Tracer != nil {
		span := opentracing.SpanFromContext(ctx)
		if span != nil {
			c.Tracer.Inject(
				span.Context(),
				opentracing.HTTPHeaders,
				opentracing.HTTPHeadersCarrier(httpReq.Header))
		}
	}

	res, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("error during request: %w", err)
	}
	defer res.Body.Close()

	maxResponseSize := c.MaxResponseSize
	if maxResponseSize == 0 {
		maxResponseSize = math.MaxInt64
	}

	limitReader := io.LimitedReader{
		R: res.Body,
		N: maxResponseSize,
	}

	graphqlResponse := Response{
		Data: out,
	}

	err = json.NewDecoder(&limitReader).Decode(&graphqlResponse)
	if err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			if limitReader.N == 0 {
				return fmt.Errorf("response exceeded maximum size of %d bytes", maxResponseSize)
			}
		}
		return fmt.Errorf("error decoding response: %w", err)
	}

	if len(graphqlResponse.Errors) > 0 {
		return graphqlResponse.Errors
	}

	return nil
}


func (c *DefaultGraphQLClient) UseMaxResponseSize(maxResponseSize int64) {
	c.MaxResponseSize = maxResponseSize
}

func (c *DefaultGraphQLClient) UseUserAgent(userAgent string) {
	c.UserAgent = userAgent
}

func (c *DefaultGraphQLClient) UseTracer(tracer opentracing.Tracer) {
	c.Tracer = tracer
}

func (c *DefaultGraphQLClient) UseTimeout(timeout time.Duration) {
	c.HTTPClient.Timeout = timeout
}

// Request is a GraphQL request.
type Request struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName,omitempty"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	Headers       http.Header            `json:"-"`
}

// NewRequest creates a new GraphQL requests from the provided body.
func NewRequest(body string) *Request {
	return &Request{
		Query: body,
	}
}

// Response is a GraphQL response
type Response struct {
	Errors GraphqlErrors `json:"errors"`
	Data   interface{}
}

// GraphqlErrors represents a list of GraphQL errors, as returned in a GraphQL
// response.
type GraphqlErrors []GraphqlError

// GraphqlError is a single GraphQL error
type GraphqlError struct {
	Message    string                 `json:"message"`
	Extensions map[string]interface{} `json:"extensions"`
}

// Error returns a string representation of the error list
func (e GraphqlErrors) Error() string {
	var errs []string
	for _, err := range e {
		errs = append(errs, err.Message)
	}
	return strings.Join(errs, ",")
}

func GenerateUserAgent(operation string) string {
	return fmt.Sprintf("Bramble/%s (%s)", Version, operation)
}
