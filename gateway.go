package bramble

import (
	"context"
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/handler/extension"
	"github.com/99designs/gqlgen/graphql/handler/transport"
	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Gateway contains the public and private routers
type Gateway struct {
	ExecutableSchema *ExecutableSchema

	plugins []Plugin
}

// NewGateway returns the graphql gateway server mux
func NewGateway(executableSchema *ExecutableSchema, plugins []Plugin) *Gateway {
	return &Gateway{
		ExecutableSchema: executableSchema,
		plugins:          plugins,
	}
}

// UpdateSchemas periodically updates the execute schema
func (g *Gateway) UpdateSchemas(interval time.Duration) {
	time.Sleep(interval)
	for range time.Tick(interval) {
		err := g.ExecutableSchema.UpdateSchema(context.Background(), false)
		if err != nil {
			log.WithError(err).Error("error updating schemas")
		}
	}
}

// Router returns the public http handler
func (g *Gateway) Router(cfg *Config) http.Handler {
	mux := http.NewServeMux()

	gatewayHandler := handler.New(g.ExecutableSchema)
	for _, plugin := range g.plugins {
		plugin.SetupGatewayHandler(gatewayHandler)
	}
	// Duplicated from `handler.NewDefaultServer` minus
	// the websocket transport and persisted query extension
	gatewayHandler.AddTransport(transport.Options{})
	gatewayHandler.AddTransport(transport.GET{})
	gatewayHandler.AddTransport(transport.POST{})
	gatewayHandler.AddTransport(transport.MultipartForm{
		MaxUploadSize: cfg.MaxFileUploadSize,
	})
	if !cfg.DisableIntrospection {
		gatewayHandler.Use(extension.Introspection{})
	}

	mux.Handle("/query", applyMiddleware(otelhttp.NewHandler(gatewayHandler, "/query"), debugMiddleware))

	for _, plugin := range g.plugins {
		plugin.SetupPublicMux(mux)
	}

	var result http.Handler = mux

	for i := len(g.plugins) - 1; i >= 0; i-- {
		result = g.plugins[i].ApplyMiddlewarePublicMux(result)
	}

	return applyMiddleware(result, monitoringMiddleware)
}

// PrivateRouter returns the private http handler
func (g *Gateway) PrivateRouter() http.Handler {
	mux := http.NewServeMux()

	for _, plugin := range g.plugins {
		plugin.SetupPrivateMux(mux)
	}

	var result http.Handler = mux
	for i := len(g.plugins) - 1; i >= 0; i-- {
		result = g.plugins[i].ApplyMiddlewarePrivateMux(result)
	}

	return result
}
