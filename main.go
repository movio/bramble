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
	var configFiles arrayFlags
	flag.Var(&configFiles, "conf", "Config file (can appear multiple times)")
	flag.Parse()

	log.SetFormatter(&log.JSONFormatter{TimestampFormat: time.RFC3339Nano})

	cfg, err := GetConfig(configFiles)
	if err != nil {
		log.WithError(err).Fatal("failed to get config")
	}
	go cfg.Watch()

	err = cfg.Init()
	if err != nil {
		log.WithError(err).Fatal("failed to configure")
	}

	log.WithField("config", cfg).Debug("configuration")

	gtw := NewGateway(cfg.executableSchema, cfg.plugins)
	RegisterMetrics()

	go gtw.UpdateSchemas(cfg.PollIntervalDuration)

	signalChan := make(chan os.Signal)
	signal.Notify(signalChan, os.Interrupt)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-signalChan
		log.Info("received shutdown signal")
		cancel()
	}()

	var wg sync.WaitGroup
	wg.Add(3)

	go runHandler(ctx, &wg, "metrics", cfg.MetricAddress(), NewMetricsHandler())
	go runHandler(ctx, &wg, "private", cfg.PrivateAddress(), gtw.PrivateRouter())
	go runHandler(ctx, &wg, "public", cfg.GatewayAddress(), gtw.Router())

	wg.Wait()
}

func runHandler(ctx context.Context, wg *sync.WaitGroup, name, addr string, handler http.Handler) {
	srv := &http.Server{
		Addr:    addr,
		Handler: handler,
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
