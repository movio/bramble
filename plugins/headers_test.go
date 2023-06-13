package plugins

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/movio/bramble"
	"github.com/stretchr/testify/assert"
)

func TestHeaders(t *testing.T) {
	p := NewHeadersPlugin(HeadersPluginConfig{
		AllowedHeaders: []string{"X-Fun-Header"},
	})

	t.Run("unknown header is not in context", func(t *testing.T) {
		called := false
		handler := p.ApplyMiddlewarePublicMux(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			headers := bramble.GetOutgoingRequestHeadersFromContext(r.Context())
			assert.Empty(t, headers.Get("X-Bad-Header"))
		}))
		req := httptest.NewRequest(http.MethodPost, "/query", nil)
		req.Header.Add("X-Bad-Header", "bad")
		handler.ServeHTTP(httptest.NewRecorder(), req)
		assert.True(t, called)
	})
	t.Run("allowed header is in context", func(t *testing.T) {
		called := false
		handler := p.ApplyMiddlewarePublicMux(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			headers := bramble.GetOutgoingRequestHeadersFromContext(r.Context())
			assert.Equal(t, headers.Get("X-Fun-Header"), "funtime")
		}))
		req := httptest.NewRequest(http.MethodPost, "/query", nil)
		req.Header.Add("X-Fun-Header", "funtime")
		handler.ServeHTTP(httptest.NewRecorder(), req)
		assert.True(t, called)
	})
}
