package bramble

import (
	"context"
	"flag"
	"fmt"
	log "log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"
)

// Main runs the gateway. This function is exported so that it can be reused
// when building Bramble with custom plugins.
func Main() {
	ctx := context.Background()

	var configFiles arrayFlags
	level := new(log.LevelVar)
	flag.Var(&configFiles, "config", "Config file (can appear multiple times)")
	flag.Var(&configFiles, "conf", "deprecated, use -config instead")
	flag.TextVar(level, "loglevel", level, "log level: debug, info, warn, error")
	flag.Parse()

	logger := log.New(log.NewJSONHandler(os.Stderr, &log.HandlerOptions{Level: level}))
	log.SetDefault(logger)

	cfg, err := GetConfig(configFiles)
	if err != nil {
		log.With("error", err).Error("failed to load config")
		os.Exit(1)
	}
	go cfg.Watch()

	shutdown, err := InitTelemetry(ctx, cfg.Telemetry)
	if err != nil {
		log.With("error", err).Error("failed initializing telemetry")
	}

	defer func() {
		log.Info("flushing and shutting down telemetry")
		if err := shutdown(context.Background()); err != nil {
			log.With("error", err).Error("shutting down telemetry")
		}
	}()

	err = cfg.Init()
	if err != nil {
		log.With("error", err).Error("failed to configure")
		os.Exit(1)
	}

	log.With("config", cfg).Debug("configuration")

	gtw := NewGateway(cfg.executableSchema, cfg.plugins)
	RegisterMetrics()

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)

	privateListener, err := net.Listen("tcp", cfg.PrivateAddress())
	if err != nil {
		log.With("error", err).Error("failed to bind private listener")
		os.Exit(1)
	}
	go runHandler(ctx, &wg, "private", privateListener, cfg.PrivateTimeouts, gtw.PrivateRouter())

	err = gtw.ExecutableSchema.UpdateSchema(ctx, true)
	if err != nil {
		log.With("error", err).Error("failed run first schema update")
		os.Exit(1)
	}

	go gtw.UpdateSchemas(cfg.PollIntervalDuration)

	metricsListener, err := net.Listen("tcp", cfg.MetricAddress())
	if err != nil {
		log.With("error", err).Error("failed to bind metrics listener")
		os.Exit(1)
	}
	publicListener, err := net.Listen("tcp", cfg.GatewayAddress())
	if err != nil {
		log.With("error", err).Error("failed to bind public listener")
		os.Exit(1)
	}

	go runHandler(ctx, &wg, "metrics", metricsListener, cfg.DefaultTimeouts, NewMetricsHandler())
	go runHandler(ctx, &wg, "public", publicListener, cfg.GatewayTimeouts, gtw.Router(cfg))

	wg.Wait()
}

func runHandler(ctx context.Context, wg *sync.WaitGroup, name string, ln net.Listener, timeouts TimeoutConfig, handler http.Handler) {
	srv := &http.Server{
		Handler:      handler,
		ReadTimeout:  timeouts.ReadTimeoutDuration,
		WriteTimeout: timeouts.WriteTimeoutDuration,
		IdleTimeout:  timeouts.IdleTimeoutDuration,
	}

	go func() {
		log.With("addr", ln.Addr().String()).Info(fmt.Sprintf("serving %s handler", name))
		if err := srv.Serve(ln); err != http.ErrServerClosed {
			log.With("error", err).Error("server terminated unexpectedly")
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Info(fmt.Sprintf("shutting down %s handler", name))
	err := srv.Shutdown(timeoutCtx)
	if err != nil {
		log.With("error", err).Error("failed shutting down server")
	}
	log.Info(fmt.Sprintf("shut down %s handler", name))
	wg.Done()
}
