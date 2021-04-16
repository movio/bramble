package bramble

import (
	"context"
	"encoding/json"
	"net/http"

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
	ModifyExtensions(ctx context.Context, e *QueryExecution, extensions map[string]interface{}) error
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

// ApplyMiddlewarePublicMux ...
func (p *BasePlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return h
}

// ApplyMiddlewarePrivateMux ...
func (p *BasePlugin) ApplyMiddlewarePrivateMux(h http.Handler) http.Handler {
	return h
}

// ModifyExtensions ...
func (p *BasePlugin) ModifyExtensions(ctx context.Context, e *QueryExecution, extensions map[string]interface{}) error {
	return nil
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
