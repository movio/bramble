# Debugging

## Debug headers

If the `X-Bramble-Debug` header is present Bramble will add the requested debug information to the response `extensions`.
One or multiple of the following options can be provided (white space separated):

- `variables`: input variables
- `query`: input query
- `plan`: the query plan, including services and subqueries
- `timing`: total execution time for the query (as a duration string, e.g. `12ms`)
- `trace-id`: the jaeger trace-id
- `all` (all of the above)

## Open tracing (Jaeger)

Tracing is a powerful way to understand exactly how your queries are executed and to troubleshoot slow queries.

### Enable tracing on Bramble

See the [open tracing plugin](plugins?id=open-tracing-jaeger).

### Add tracing to your services (optional)

Adding tracing to your individual services will add a lot more details to your traces.

1. Create a tracer, see the [Jaeger documentation](https://pkg.go.dev/github.com/uber/jaeger-client-go#NewTracer)

2. Add a tracing middleware to your HTTP endpoint.

```go
mux.Handle("/query", NewTracingMiddleware(tracer).Middleware(gqlserver))
```

<details>
<summary>
<strong>Example Go middleware</strong>
</summary>

```go
// TracingMiddleware is a middleware to add open tracing to incoming requests.
// It creates a span for each incoming requests, using the request context if
// present.
type TracingMiddleware struct {
	tracer opentracing.Tracer
}

// NewTracingMiddleware returns a new tracing middleware
func NewTracingMiddleware(tracer opentracing.Tracer) *TracingMiddleware {
	return &TracingMiddleware{
		tracer: tracer,
	}
}

// Middleware applies the tracing middleware to the handler
func (m *TracingMiddleware) Middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		spanContext, _ := m.tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))
		span := m.tracer.StartSpan("query", ext.RPCServerOption(spanContext))
		c := opentracing.ContextWithSpan(r.Context(), span)
		h.ServeHTTP(rw, r.WithContext(c))
		span.Finish()
	})
}
```

</details>

3. Add the tracer to the resolver

   - With graph-gophers

   ```go
   parsedSchema := graphql.MustParseSchema(schema, resolver, graphql.Tracer(trace.OpenTracingTracer{}))
   ```

   - With gqlgen

   ```go
   gqlserver.Use(support.NewGqlgenOpenTracing(tracer))
   ```
