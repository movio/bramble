package plugins

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/movio/bramble"
	"github.com/rs/cors"
)

func init() {
	bramble.RegisterPlugin(&CorsPlugin{})
}

type CorsPlugin struct {
	bramble.BasePlugin
	config CorsPluginConfig
}

type CorsPluginConfig struct {
	AllowedOrigins   []string `json:"allowed-origins"`
	AllowedHeaders   []string `json:"allowed-headers"`
	AllowedMethods   []string `json:"allowed-methods"`
	AllowCredentials bool     `json:"allow-credentials"`
	ExposedHeaders   []string `json:"exposed-headers"`
	MaxAge           int      `json:"max-age"`
	Debug            bool     `json:"debug"`
}

func NewCorsPlugin(options CorsPluginConfig) *CorsPlugin {
	return &CorsPlugin{bramble.BasePlugin{}, options}
}

func (p *CorsPlugin) ID() string {
	return "cors"
}

func (p *CorsPlugin) Configure(cfg *bramble.Config, data json.RawMessage) error {
	return json.Unmarshal(data, &p.config)
}

func (p *CorsPlugin) middleware(h http.Handler) http.Handler {
	c := cors.New(cors.Options{
		AllowedOrigins:   p.config.AllowedOrigins,
		AllowedHeaders:   p.config.AllowedHeaders,
		AllowedMethods:   p.config.AllowedMethods,
		AllowCredentials: p.config.AllowCredentials,
		ExposedHeaders:   p.config.ExposedHeaders,
		MaxAge:           p.config.MaxAge,
		Debug:            p.config.Debug,
	})
	if p.config.Debug {
		c.Log = slog.NewLogLogger(slog.Default().Handler(), slog.LevelInfo)
	}
	return c.Handler(h)
}

func (p *CorsPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return p.middleware(h)
}

func (p *CorsPlugin) ApplyMiddlewarePrivateMux(h http.Handler) http.Handler {
	return p.middleware(h)
}
