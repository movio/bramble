package bramble

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Main runs the gateway. This function is exported so that it can be reused
// when building Bramble with custom plugins.
func Main() {
	ctx := context.Background()

	var configFiles arrayFlags
	flag.Var(&configFiles, "config", "Config file (can appear multiple times)")
	flag.Var(&configFiles, "conf", "deprecated, use -config instead")
	flag.Parse()

	log.SetFormatter(&log.JSONFormatter{TimestampFormat: time.RFC3339Nano})

	cfg, err := GetConfig(configFiles)
	if err != nil {
		log.WithError(err).Fatal("failed to get config")
	}
	go cfg.Watch()

	shutdown, err := InitTelemetry(ctx, cfg.Telemetry)
	if err != nil {
		log.WithError(err).Error("error creating telemetry")
	}

	defer func() {
		log.Info("flushing and shutting down telemetry")
		if err := shutdown(context.Background()); err != nil {
			log.WithError(err).Error("shutting down telemetry")
		}
	}()

	err = cfg.Init()
	if err != nil {
		log.WithError(err).Fatal("failed to configure")
	}

	log.WithField("config", cfg).Debug("configuration")

	gtw := NewGateway(cfg.executableSchema, cfg.plugins)
	RegisterMetrics()

	go gtw.UpdateSchemas(cfg.PollIntervalDuration)

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)

	go runHandler(ctx, &wg, "metrics", cfg.MetricAddress(), cfg.DefaultTimeouts, NewMetricsHandler())
	go runHandler(ctx, &wg, "private", cfg.PrivateAddress(), cfg.PrivateTimeouts, gtw.PrivateRouter())
	go runHandler(ctx, &wg, "public", cfg.GatewayAddress(), cfg.GatewayTimeouts, gtw.Router(cfg))

	wg.Wait()
}

func runHandler(ctx context.Context, wg *sync.WaitGroup, name, addr string, timeouts TimeoutConfig, handler http.Handler) {
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  timeouts.ReadTimeoutDuration,
		WriteTimeout: timeouts.WriteTimeoutDuration,
		IdleTimeout:  timeouts.IdleTimeoutDuration,
	}

	go func() {
		log.WithField("addr", addr).Infof("serving %s handler", name)
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			log.WithError(err).Fatal("server terminated unexpectedly")
		}
	}()

	<-ctx.Done()

	timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Infof("shutting down %s handler", name)
	err := srv.Shutdown(timeoutCtx)
	if err != nil {
		log.WithError(err).Error("error shutting down server")
	}
	log.Infof("shut down %s handler", name)
	wg.Done()
}
