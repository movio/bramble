package bramble

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/felixge/httpsnoop"
	"github.com/prometheus/client_golang/prometheus"
)

type middleware func(http.Handler) http.Handler

// DebugKey is used to request debug info from the context
const DebugKey contextKey = "debug"

const (
	debugHeader = "X-Bramble-Debug"
)

// DebugInfo contains the requested debug info for a query
type DebugInfo struct {
	Variables bool
	Query     bool
	Plan      bool
	Timing    bool
	TraceID   bool
}

func debugMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := DebugInfo{}
		for _, field := range strings.Fields(r.Header.Get(debugHeader)) {
			switch field {
			case "all":
				info.Variables = true
				info.Plan = true
				info.Query = true
				info.Timing = true
				info.TraceID = true
			case "query":
				info.Query = true
			case "variables":
				info.Variables = true
			case "plan":
				info.Plan = true
			case "timing":
				info.Timing = true
			case "traceid":
				info.TraceID = true
			}
		}

		ctx := context.WithValue(r.Context(), DebugKey, info)
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

func monitoringMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, event := startEvent(r.Context(), "request")
		if !strings.HasPrefix(r.Header.Get("user-agent"), "Bramble") {
			defer event.finish()
		}

		if host := r.Header.Get("X-Forwarded-Host"); host != "" {
			event.addField("forwarded_host", host)
		}

		var buf bytes.Buffer
		_, err := io.Copy(&buf, r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(&buf)

		r = r.WithContext(ctx)

		addRequestBody(event, r, buf)

		m := httpsnoop.CaptureMetrics(h, w, r)

		event.addFields(EventFields{
			"response.status": m.Code,
			"request.path":    r.URL.Path,
			"response.size":   m.Written,
		})

		promHTTPRequestCounter.With(prometheus.Labels{
			"code": fmt.Sprintf("%dXX", m.Code/100),
		}).Inc()
		promHTTPRequestSizes.With(prometheus.Labels{}).Observe(float64(buf.Len()))
		promHTTPResponseSizes.With(prometheus.Labels{}).Observe(float64(m.Written))
		promHTTPResponseDurations.With(prometheus.Labels{}).Observe(m.Duration.Seconds())
	})
}

func addRequestBody(e *event, r *http.Request, buf bytes.Buffer) {
	contentType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	e.addField("request.content-type", contentType)

	if r.Method != http.MethodHead && r.Method != http.MethodGet {
		switch {
		case contentType == "application/json":
			var payload interface{}
			if err := json.Unmarshal(buf.Bytes(), &payload); err == nil {
				e.addField("request.body", &payload)
			} else {
				e.addField("request.body", buf.String())
				e.addField("request.error", err)
			}
		case contentType == "multipart/form-data":
			e.addField("request.body", fmt.Sprintf("%d bytes", len(buf.Bytes())))
		default:
			e.addField("request.body", buf.String())
		}
	} else {
		e.addField("request.body", buf.String())
	}
}

func applyMiddleware(h http.Handler, mws ...middleware) http.Handler {
	for _, mw := range mws {
		h = mw(h)
	}
	return h
}
