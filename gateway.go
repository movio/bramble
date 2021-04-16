package bramble

import (
	"net/http"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	log "github.com/sirupsen/logrus"
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
	for range time.Tick(interval) {
		err := g.ExecutableSchema.UpdateSchema(false)
		if err != nil {
			log.WithError(err).Error("error updating schemas")
		}
	}
}

// Router returns the public http handler
func (g *Gateway) Router() http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/query",
		applyMiddleware(
			handler.NewDefaultServer(g.ExecutableSchema),
			debugMiddleware,
		),
	)

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
