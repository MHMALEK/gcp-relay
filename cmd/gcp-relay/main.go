package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/MHMALEK/gcp-relay/internal/cli"
	"github.com/MHMALEK/gcp-relay/internal/config"
	"github.com/MHMALEK/gcp-relay/internal/history"
	"github.com/MHMALEK/gcp-relay/internal/router"
	"github.com/MHMALEK/gcp-relay/internal/server"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "up", "down", "init", "demo", "validate", "help", "-h", "--help":
			os.Exit(cli.Run(os.Args[1:]))
		}
	}
	os.Exit(runServe(os.Args[1:]))
}

func runServe(args []string) int {
	if len(args) > 0 && args[0] == "serve" {
		args = args[1:]
	}

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
	flag.CommandLine.Parse(args)

	logger := log.New(os.Stdout, "gcp-relay ", log.LstdFlags|log.Lmsgprefix)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}

	store := history.NewStore(200)
	r := router.New(cfg, logger, store)
	srv := server.New(r, store, logger)

	addr := fmt.Sprintf(":%s", *port)
	logger.Printf("listening on %s project=%s functions=%d notifications=%d inspector=http://localhost:%s/ui/", addr, cfg.ProjectID, len(cfg.Functions), len(cfg.Notifications), *port)

	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		logger.Fatalf("server stopped: %v", err)
	}
	return 0
}
