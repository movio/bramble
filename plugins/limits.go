package plugins

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/movio/bramble"
)

func init() {
	bramble.RegisterPlugin(&LimitsPlugin{})
}

type LimitsPlugin struct {
	bramble.BasePlugin
	config LimitsPluginConfig
}

type LimitsPluginConfig struct {
	MaxRequestBytes     int64  `json:"max-request-bytes"`
	MaxResponseTime     string `json:"max-response-time"`
	maxResponseDuration time.Duration
}

func NewLimitsPlugin(options LimitsPluginConfig) *LimitsPlugin {
	return &LimitsPlugin{bramble.BasePlugin{}, options}
}

func (p *LimitsPlugin) ID() string {
	return "limits"
}

func (p *LimitsPlugin) Init(es *bramble.ExecutableSchema) {
	es.GraphqlClient.HTTPClient.Timeout = p.config.maxResponseDuration
}

func (p *LimitsPlugin) Configure(cfg *bramble.Config, data json.RawMessage) error {
	err := json.Unmarshal(data, &p.config)
	if err != nil {
		return err
	}

	if p.config.MaxRequestBytes == 0 {
		return fmt.Errorf("MaxRequestBytes is undefined")
	}

	if p.config.MaxResponseTime == "" {
		return fmt.Errorf("MaxResponseTime is undefined")
	}

	p.config.maxResponseDuration, err = time.ParseDuration(p.config.MaxResponseTime)
	if err != nil {
		return fmt.Errorf("invalid duration: %w", err)
	}

	return nil
}

func (p *LimitsPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, p.config.MaxRequestBytes)
		h.ServeHTTP(w, r)
	})
	return handler
}
