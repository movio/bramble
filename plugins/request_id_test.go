package plugins

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/movio/bramble"
	"github.com/stretchr/testify/assert"
)

func TestReqestIdHeader(t *testing.T) {
	p := RequestIdentifierPlugin{}

	t.Run("request id is added to outgoing context when not provided", func(t *testing.T) {
		called := false
		handler := p.ApplyMiddlewarePublicMux(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			headers := bramble.GetOutgoingRequestHeadersFromContext(r.Context())
			reqID := headers.Get(BrambleRequestHeader)
			assert.NotEmpty(t, reqID)
			id, err := uuid.FromString(reqID)
			assert.NoError(t, err)
			assert.True(t, id.Version() == uuid.V4)
		}))
		req := httptest.NewRequest(http.MethodPost, "/query", nil)
		handler.ServeHTTP(httptest.NewRecorder(), req)
		assert.True(t, called)
	})
	t.Run("request id is passed to outgoing context when provided", func(t *testing.T) {
		called := false
		handler := p.ApplyMiddlewarePublicMux(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			headers := bramble.GetOutgoingRequestHeadersFromContext(r.Context())
			reqID := headers.Get(BrambleRequestHeader)
			assert.NotEmpty(t, reqID)
			id, err := uuid.FromString(reqID)
			assert.NoError(t, err)
			assert.True(t, id.Version() == uuid.V4)
		}))
		req := httptest.NewRequest(http.MethodPost, "/query", nil)
		req.Header.Add(BrambleRequestHeader, uuid.Must(uuid.NewV4()).String())
		handler.ServeHTTP(httptest.NewRecorder(), req)
		assert.True(t, called)
	})
	t.Run("request id is reformatted when parseable as a UUID", func(t *testing.T) {
		called := false
		reqID := uuid.Must(uuid.NewV4()).String()
		handler := p.ApplyMiddlewarePublicMux(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			headers := bramble.GetOutgoingRequestHeadersFromContext(r.Context())
			id := headers.Get(BrambleRequestHeader)
			assert.Equal(t, reqID, id)
		}))
		req := httptest.NewRequest(http.MethodPost, "/query", nil)
		req.Header.Add(BrambleRequestHeader, strings.ReplaceAll(reqID, "-", ""))
		handler.ServeHTTP(httptest.NewRecorder(), req)
		assert.True(t, called)
	})
}
