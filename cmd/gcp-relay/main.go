package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/MHMALEK/gcp-relay/internal/config"
	"github.com/MHMALEK/gcp-relay/internal/router"
	"github.com/MHMALEK/gcp-relay/internal/server"
)

func main() {
	defaultConfig := os.Getenv("GCP_RELAY_CONFIG")
	if defaultConfig == "" {
		defaultConfig = "config/triggers.example.yaml"
	}
	defaultPort := os.Getenv("GCP_RELAY_PORT")
	if defaultPort == "" {
		defaultPort = "8099"
	}

	configPath := flag.String("config", defaultConfig, "path to triggers yaml")
	port := flag.String("port", defaultPort, "listen port")
	flag.Parse()

	logger := log.New(os.Stdout, "gcp-relay ", log.LstdFlags|log.Lmsgprefix)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}

	r := router.New(cfg, logger)
	srv := server.New(r, logger)

	addr := fmt.Sprintf(":%s", *port)
	logger.Printf("listening on %s project=%s triggers=%d", addr, cfg.ProjectID, len(cfg.Triggers))

	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		logger.Fatalf("server stopped: %v", err)
	}
}
