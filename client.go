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

type GraphQLClient struct {
	HTTPClient      *http.Client
	MaxResponseSize int64
	Tracer          opentracing.Tracer
	UserAgent       string
}

type ClientOpt func(*GraphQLClient)

func NewClient(opts ...ClientOpt) *GraphQLClient {
	c := &GraphQLClient{
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
		MaxResponseSize: 1024 * 1024,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

func WithMaxResponseSize(maxResponseSize int64) ClientOpt {
	return func(s *GraphQLClient) {
		s.MaxResponseSize = maxResponseSize
	}
}

func WithUserAgent(userAgent string) ClientOpt {
	return func(s *GraphQLClient) {
		s.UserAgent = userAgent
	}
}

func (c *GraphQLClient) Request(ctx context.Context, url string, request *Request, out interface{}) error {
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

type Request struct {
	Query         string                 `json:"query"`
	OperationName string                 `json:"operationName,omitempty"`
	Variables     map[string]interface{} `json:"variables,omitempty"`
	Headers       http.Header            `json:"-"`
}

func NewRequest(body string) *Request {
	return &Request{
		Query: body,
	}
}

type Response struct {
	Errors GraphqlErrors `json:"errors"`
	Data   interface{}
}

type GraphqlErrors []GraphqlError

type GraphqlError struct {
	Message    string                 `json:"message"`
	Extensions map[string]interface{} `json:"extensions"`
}

func (e GraphqlErrors) Error() string {
	var errs []string
	for _, err := range e {
		errs = append(errs, err.Message)
	}
	return strings.Join(errs, ",")
}
