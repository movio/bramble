package plugins

import (
	"encoding/json"
	"net/http"

	"github.com/movio/bramble"
)

func init() {
	bramble.RegisterPlugin(&HeadersPlugin{})
}

type HeadersPlugin struct {
	bramble.BasePlugin
	config HeadersPluginConfig
}

type HeadersPluginConfig struct {
	AllowedHeaders []string `json:"allowed-headers"`
}

func NewHeadersPlugin(options HeadersPluginConfig) *HeadersPlugin {
	return &HeadersPlugin{bramble.BasePlugin{}, options}
}

func (p *HeadersPlugin) ID() string {
	return "headers"
}

func (p *HeadersPlugin) Configure(cfg *bramble.Config, data json.RawMessage) error {
	return json.Unmarshal(data, &p.config)
}

func (p *HeadersPlugin) middleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		for _, header := range p.config.AllowedHeaders {
			if value := r.Header.Get(header); value != "" {
				ctx = bramble.AddOutgoingRequestsHeaderToContext(ctx, header, value)
			}
		}
		h.ServeHTTP(rw, r.WithContext(ctx))
	})
}

func (p *HeadersPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return p.middleware(h)
}

func (p *HeadersPlugin) ApplyMiddlewarePrivateMux(h http.Handler) http.Handler {
	return p.middleware(h)
}
