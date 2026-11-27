package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/movio/bramble"
)

const defaultPort = "8080"

func main() {
	port := os.Getenv("BRAMBLE_PORT")
	if port == "" {
		port = defaultPort
	}

	configFile := os.Getenv("BRAMBLE_CONFIG")
	if configFile == "" {
		configFile = "config.json"
	}

	cfg, err := bramble.GetConfig([]string{configFile})
	if err != nil {
		log.Fatalf(fmt.Errorf("failed to load config files: %w", err).Error())
	}
	go cfg.Watch()

	err = cfg.Init()
	if err != nil {
		log.Fatalf(fmt.Errorf("failed to load initialize config: %w", err).Error())
	}

	gateway := bramble.FromConfig(cfg)

	mux := http.NewServeMux()
	mux.Handle("/", gateway.Router())

	log.Fatal(http.ListenAndServe(":"+port, mux))
}
