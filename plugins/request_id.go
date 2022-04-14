package plugins

import (
	"net/http"

	"github.com/gofrs/uuid"
	"github.com/movio/bramble"
)

const BrambleRequestHeader = "X-Request-Id"

func init() {
	bramble.RegisterPlugin(&RequestIdentifierPlugin{})
}

type RequestIdentifierPlugin struct {
	bramble.BasePlugin
}

func (p *RequestIdentifierPlugin) ID() string {
	return "request-id"
}

func (p *RequestIdentifierPlugin) middleware(h http.Handler) http.HandlerFunc {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get(BrambleRequestHeader)
		if requestID == "" {
			requestID = uuid.Must(uuid.NewV4()).String()
		}

		ctx := r.Context()
		ctx = bramble.AddOutgoingRequestsHeaderToContext(ctx, BrambleRequestHeader, requestID)
		h.ServeHTTP(rw, r.WithContext(ctx))
	})
}

func (p *RequestIdentifierPlugin) ApplyMiddlewarePublicMux(h http.Handler) http.Handler {
	return p.middleware(h)
}

func (p *RequestIdentifierPlugin) ApplyMiddlewarePrivateMux(h http.Handler) http.Handler {
	return p.middleware(h)
}
