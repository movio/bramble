package plugins

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCors(t *testing.T) {
	p := NewCorsPlugin(CorsPluginConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowedHeaders:   []string{"X-My-Header"},
		AllowCredentials: true,
		MaxAge:           3600,
	})

	var handler http.Handler
	handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler = p.ApplyMiddlewarePublicMux(handler)

	req := httptest.NewRequest(http.MethodPost, "/query", nil)
	req.Header.Add("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, "https://example.com", rr.Header().Get("Access-Control-Allow-Origin"))

	req = httptest.NewRequest(http.MethodOptions, "/query", nil)
	req.Header.Add("Origin", "https://example.com")
	req.Header.Add("Access-Control-Request-Method", "POST")
	req.Header.Add("Access-Control-Request-Headers", "X-My-Header")
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, "X-My-Header", rr.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "3600", rr.Header().Get("Access-Control-Max-Age"))
}
