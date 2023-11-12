package bramble

import (
	"net/http"
	"net/http/pprof"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// promInvalidSchema is a gauge representing the current status of remote services schemas
	promInvalidSchema = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "invalid_schema",
		Help: "A gauge representing the current status of remote services schemas",
	})

	promServiceUpdateErrorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "service_update_error_total",
			Help: "A counter indicating how many times services have failed to update",
		},
		[]string{
			"service",
		},
	)

	promServiceTimeoutErrorCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "service_timeout_error_total",
			Help: "A counter indicating how many times services have timed out",
		},
		[]string{
			"service",
		},
	)

	promServiceUpdateErrorGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_update_error",
			Help: "A gauge indicating what services are failing to update",
		},
		[]string{
			"service",
		},
	)

	// promHTTPInFlightGauge is a gauge of requests currently being served by the wrapped handler
	promHTTPInFlightGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_in_flight_requests",
		Help: "A gauge of requests currently being served",
	})

	// promHTTPRequestCounter is a counter for requests to the wrapped handler
	promHTTPRequestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_api_requests_total",
			Help: "A counter for served requests",
		},
		[]string{"code"},
	)

	// promHTTPResponseDurations is a histogram of request latencies
	promHTTPResponseDurations = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_response_duration_seconds",
			Help:    "A histogram of request latencies",
			Buckets: prometheus.DefBuckets,
		},
		[]string{},
	)

	// promHTTPRequestSizes is a histogram of request sizes for requests
	promHTTPRequestSizes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_size_bytes",
			Help:    "A histogram of request sizes for requests",
			Buckets: prometheus.ExponentialBuckets(128, 2, 10),
		},
		[]string{},
	)

	// promHTTPResponseSizes is a histogram of response sizes for responses.
	promHTTPResponseSizes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_response_size_bytes",
			Help:    "A histogram of response sizes for responses",
			Buckets: prometheus.ExponentialBuckets(1024, 2, 10),
		},
		[]string{},
	)
)

// RegisterMetrics register the prometheus metrics.
func RegisterMetrics() {
	prometheus.MustRegister(promInvalidSchema)
	prometheus.MustRegister(promServiceTimeoutErrorCounter)
	prometheus.MustRegister(promServiceUpdateErrorCounter)
	prometheus.MustRegister(promServiceUpdateErrorGauge)
	prometheus.MustRegister(promHTTPInFlightGauge)
	prometheus.MustRegister(promHTTPRequestCounter)
	prometheus.MustRegister(promHTTPResponseDurations)
	prometheus.MustRegister(promHTTPRequestSizes)
	prometheus.MustRegister(promHTTPResponseSizes)
}

// NewMetricsHandler returns a new Prometheus metrics handler.
func NewMetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/debug/pprof/", pprof.Index)

	return mux
}
