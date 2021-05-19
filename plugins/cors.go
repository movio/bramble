package plugins

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/movio/bramble"
	"github.com/rs/cors"
	"github.com/sirupsen/logrus"
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
	AllowCredentials bool     `json:"allow-credentials"`
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
		AllowCredentials: p.config.AllowCredentials,
		MaxAge:           p.config.MaxAge,
		Debug:            p.config.Debug,
	})
	if p.config.Debug {
		c.Log = log.New(logrus.StandardLogger().Writer(), "cors:", log.Lshortfile)
	}
	return c.Handler(h)
}

func (p *CorsPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return p.middleware(h)
}

func (p *CorsPlugin) ApplyMiddlewarePrivateMux(h http.Handler) http.Handler {
	return p.middleware(h)
}
