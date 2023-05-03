package bramble

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/99designs/gqlgen/graphql"
	log "github.com/sirupsen/logrus"
)

// Plugin is a Bramble plugin. Plugins can be used to extend base Bramble functionalities.
type Plugin interface {
	// ID must return the plugin identifier (name). This is the id used to match
	// the plugin in the configuration.
	ID() string
	// Configure is called during initialization and every time the config is modified.
	// The pluginCfg argument is the raw json contained in the "config" key for that plugin.
	Configure(cfg *Config, pluginCfg json.RawMessage) error
	// Init is called once on initialization
	Init(schema *ExecutableSchema)
	SetupPublicMux(mux *http.ServeMux)
	SetupPrivateMux(mux *http.ServeMux)
	// Should return true and the query path if the plugin is a service that
	// should be federated by Bramble
	GraphqlQueryPath() (bool, string)
	ApplyMiddlewarePublicMux(http.Handler) http.Handler
	ApplyMiddlewarePrivateMux(http.Handler) http.Handler
	WrapGraphQLClientTransport(http.RoundTripper) http.RoundTripper

	InterceptRequest(ctx context.Context, operationName, rawQuery string, variables map[string]interface{})
	InterceptResponse(ctx context.Context, operationName, rawQuery string, variables map[string]interface{}, response *graphql.Response) *graphql.Response
}

// BasePlugin is an empty plugin. It can be embedded by any plugin as a way to avoid
// declaring unnecessary methods.
type BasePlugin struct{}

// Configure ...
func (p *BasePlugin) Configure(*Config, json.RawMessage) error {
	return nil
}

// Init ...
func (p *BasePlugin) Init(s *ExecutableSchema) {}

// SetupPublicMux ...
func (p *BasePlugin) SetupPublicMux(mux *http.ServeMux) {}

// SetupPrivateMux ...
func (p *BasePlugin) SetupPrivateMux(mux *http.ServeMux) {}

// GraphqlQueryPath ...
func (p *BasePlugin) GraphqlQueryPath() (bool, string) {
	return false, ""
}

// InterceptRequest is called before bramble starts executing a request.
// It can be used to inspect the unmarshalled GraphQL request bramble receives.
func (p *BasePlugin) InterceptRequest(ctx context.Context, operationName, rawQuery string, variables map[string]interface{}) {
}

// InterceptResponse is called after bramble has finished executing a request.
// It can be used to inspect and/or modify the response bramble will return.
func (p *BasePlugin) InterceptResponse(ctx context.Context, operationName, rawQuery string, variables map[string]interface{}, response *graphql.Response) *graphql.Response {
	return response
}

// ApplyMiddlewarePublicMux ...
func (p *BasePlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return h
}

// ApplyMiddlewarePrivateMux ...
func (p *BasePlugin) ApplyMiddlewarePrivateMux(h http.Handler) http.Handler {
	return h
}

// WrapGraphQLClientTransport wraps the http.RoundTripper used for GraphQL requests.
func (p *BasePlugin) WrapGraphQLClientTransport(transport http.RoundTripper) http.RoundTripper {
	return transport
}

var registeredPlugins = map[string]Plugin{}

// RegisterPlugin register a plugin so that it can be enabled via the configuration.
func RegisterPlugin(p Plugin) {
	if _, found := registeredPlugins[p.ID()]; found {
		log.Fatalf("plugin %q already registered", p.ID())
	}
	registeredPlugins[p.ID()] = p
}

// RegisteredPlugins returned the list of registered plugins.
func RegisteredPlugins() map[string]Plugin {
	return registeredPlugins
}
