package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/movio/bramble"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
)

func init() {
	bramble.RegisterPlugin(&OpenTracingPlugin{})
}

type OpenTracingPlugin struct {
	bramble.BasePlugin
	tracer opentracing.Tracer
}

func (p *OpenTracingPlugin) ID() string {
	return "open-tracing"
}

func (p *OpenTracingPlugin) Configure(cfg *bramble.Config, pluginCfg json.RawMessage) error {
	jaegerConfig := jaegercfg.Configuration{
		ServiceName: "bramble",
		Sampler: &jaegercfg.SamplerConfig{
			Type:  "remote",
			Param: 1,
		},
	}

	jaegerCfg, err := jaegerConfig.FromEnv()
	if err != nil {
		return fmt.Errorf("could not get Jaeger config from env: %w", err)
	}

	p.tracer, _, err = jaegerCfg.NewTracer()
	return err
}

func (p *OpenTracingPlugin) Init(s *bramble.ExecutableSchema) {
	s.Tracer = p.tracer
	s.GraphqlClient.UseTracer(p.tracer)
}

func (p *OpenTracingPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		// do not trace healthcheck
		if strings.HasPrefix(r.Header.Get("user-agent"), "Bramble") {
			h.ServeHTTP(rw, r)
			return
		}

		spanContext, _ := p.tracer.Extract(opentracing.HTTPHeaders, opentracing.HTTPHeadersCarrier(r.Header))
		span := p.tracer.StartSpan("query", ext.RPCServerOption(spanContext))
		c := opentracing.ContextWithSpan(r.Context(), span)
		bramble.AddFields(r.Context(), bramble.EventFields{
			"trace-id": traceIDFromContext(c),
		})
		r = r.WithContext(c)
		h.ServeHTTP(rw, r)
		span.Finish()
	})
}

// traceIDFromContext returns the Jaeger's trace ID if a span exists in the
// current context
func traceIDFromContext(ctx context.Context) string {
	span := opentracing.SpanFromContext(ctx)
	if span == nil {
		return ""
	}
	jaegerContext, ok := span.Context().(jaeger.SpanContext)
	if !ok {
		return ""
	}
	return jaegerContext.TraceID().String()
}
