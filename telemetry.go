package bramble

import (
	"context"
	"errors"
	"os"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.25.0"
)

// instrumentationName is used to identify the instrumentation in the
// OpenTelemetry collector. It maps to the attribute `otel.library.name`.
const instrumentationName string = "github.com/movio/bramble"

// TelemetryConfig is the configuration for OpenTelemetry tracing and metrics.
type TelemetryConfig struct {
	Enabled     bool   `json:"enabled"`      // Enabled enables OpenTelemetry tracing and metrics.
	Insecure    bool   `json:"insecure"`     // Insecure enables insecure communication with the OpenTelemetry collector.
	Endpoint    string `json:"endpoint"`     // Endpoint is the OpenTelemetry collector endpoint.
	ServiceName string `json:"service_name"` // ServiceName is the name of the service.
}

// TelemetryErrHandler is an error handler that logs errors.
type TelemetryErrHandler struct {
	log *logrus.Logger
}

// Handle implements otel.ErrorHandler.
func (e *TelemetryErrHandler) Handle(err error) {
	e.log.Error(err.Error())
}

// InitializesTelemetry initializes OpenTelemetry tracing and metrics. It
// returns a shutdown function that should be called when the application
// terminates.
func InitTelemetry(ctx context.Context, cfg TelemetryConfig) (func(context.Context) error, error) {
	endpoint := os.Getenv("BRAMBLE_OTEL_ENDPOINT")
	if endpoint != "" {
		cfg.Endpoint = endpoint
	}

	// If telemetry is disabled, return a no-op shutdown function. The standard
	// behaviour of the application will not be affected, since a
	// `NoopTracerProvider` is used by default.
	if !cfg.Enabled || cfg.Endpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	var flushAndShutdownFuncs []func(context.Context) error

	// flushAndShutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	flushAndShutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range flushAndShutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		flushAndShutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) error {
		return errors.Join(inErr, flushAndShutdown(ctx))
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = "bramble"
	}

	// Set up resource.
	res, err := resource.Merge(resource.Default(),
		resource.NewWithAttributes(semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(Version),
		))
	if err != nil {
		return nil, handleErr(err)
	}

	// Set up propagator.
	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)

	otel.SetTextMapPropagator(prop)

	errHandler := &TelemetryErrHandler{
		log: logrus.StandardLogger(),
	}

	otel.SetErrorHandler(errHandler)

	traceShutdown, err := setupOTelTraceProvider(ctx, cfg, res)
	if err != nil {
		return nil, handleErr(err)
	}

	flushAndShutdownFuncs = append(flushAndShutdownFuncs, traceShutdown...)

	meterShutdown, err := setupOTelMeterProvider(ctx, cfg, res)
	if err != nil {
		return nil, handleErr(err)
	}

	flushAndShutdownFuncs = append(flushAndShutdownFuncs, meterShutdown...)

	return flushAndShutdown, nil
}

func setupOTelTraceProvider(ctx context.Context, cfg TelemetryConfig, res *resource.Resource) ([]func(context.Context) error, error) {
	// Set up exporter.
	traceExp, err := newTraceExporter(ctx, cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, err
	}

	// Set up trace provider.
	tracerProvider, err := newTraceProvider(traceExp, res)
	if err != nil {
		return nil, err
	}

	var shutdownFuncs []func(context.Context) error
	shutdownFuncs = append(shutdownFuncs,
		tracerProvider.ForceFlush, // ForceFlush exports any traces that have not yet been exported.
		tracerProvider.Shutdown,   // Shutdown stops the export pipeline and returns the last error.
	)

	otel.SetTracerProvider(tracerProvider)
	return shutdownFuncs, nil
}

func newTraceExporter(ctx context.Context, endpoint string, insecure bool) (sdktrace.SpanExporter, error) {
	exporterOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}

	if insecure {
		exporterOpts = append(exporterOpts, otlptracegrpc.WithInsecure())
	}

	traceExporter, err := otlptracegrpc.New(ctx, exporterOpts...)
	if err != nil {
		return nil, err
	}

	return traceExporter, nil
}

func newTraceProvider(exp sdktrace.SpanExporter, res *resource.Resource) (*sdktrace.TracerProvider, error) {
	// ParentBased sampler is used to sample traces based on the parent span.
	// This is useful for sampling traces based on the sampling decision of the
	// upstream service. We follow the default sampling strategy of the
	// OpenTelemetry Sampler.
	parentSamplers := []sdktrace.ParentBasedSamplerOption{
		sdktrace.WithLocalParentSampled(sdktrace.AlwaysSample()),
		sdktrace.WithLocalParentNotSampled(sdktrace.NeverSample()),
		sdktrace.WithRemoteParentSampled(sdktrace.AlwaysSample()),
		sdktrace.WithRemoteParentNotSampled(sdktrace.NeverSample()),
	}

	traceProvider := sdktrace.NewTracerProvider(
		// By default we'll trace all requests if not parent trace is found.
		// Otherwise we follow the rules from above.
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample(), parentSamplers...)),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(sdktrace.NewBatchSpanProcessor(exp)),
	)

	return traceProvider, nil
}

func setupOTelMeterProvider(ctx context.Context, cfg TelemetryConfig, res *resource.Resource) ([]func(context.Context) error, error) {
	metricExp, err := newMetricExporter(ctx, cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, err
	}

	meterProvider, err := newMeterProvider(metricExp, res)
	if err != nil {
		return nil, err
	}

	var shutdownFuncs []func(context.Context) error
	shutdownFuncs = append(shutdownFuncs,
		meterProvider.ForceFlush, // ForceFlush exports any metrics that have not yet been exported.
		meterProvider.Shutdown,   // Shutdown stops the export pipeline and returns the last error.
	)

	otel.SetMeterProvider(meterProvider)
	return shutdownFuncs, nil
}

func newMetricExporter(ctx context.Context, endpoint string, insecure bool) (sdkmetric.Exporter, error) {
	exporterOpts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(endpoint),
	}

	if insecure {
		exporterOpts = append(exporterOpts, otlpmetricgrpc.WithInsecure())
	}

	metricExporter, err := otlpmetricgrpc.New(ctx, exporterOpts...)
	if err != nil {
		return nil, err
	}

	return metricExporter, nil
}

func newMeterProvider(exp sdkmetric.Exporter, res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp)),
	)

	return meterProvider, nil
}
