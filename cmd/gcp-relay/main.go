package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MHMALEK/gcp-relay/internal/bootstrap"
	"github.com/MHMALEK/gcp-relay/internal/cli"
	"github.com/MHMALEK/gcp-relay/internal/compose"
	"github.com/MHMALEK/gcp-relay/internal/config"
	"github.com/MHMALEK/gcp-relay/internal/history"
	"github.com/MHMALEK/gcp-relay/internal/launcher"
	"github.com/MHMALEK/gcp-relay/internal/router"
	"github.com/MHMALEK/gcp-relay/internal/server"
)

// version is injected at build time via -ldflags "-X main.version=vX.Y.Z".
var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "--version", "-v":
			fmt.Println(version)
			os.Exit(0)
		case "up", "down", "init", "demo", "validate", "plan", "logs", "help", "-h", "--help":
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
	logger.Printf("gcp-relay %s listening on %s project=%s functions=%d notifications=%d inspector=http://localhost:%s/ui/", version, addr, cfg.ProjectID, len(cfg.Functions), len(cfg.Notifications), *port)

	if os.Getenv("GCP_RELAY_AUTO_BOOTSTRAP") == "true" {
		go autoBootstrap(logger, cfg)
	}

	if os.Getenv("GCP_RELAY_LAUNCH_FUNCTIONS") == "true" {
		l := launcher.NewDocker(
			envOr("GCP_RELAY_NETWORK", "gcp-relay"),
			os.Getenv("GCP_RELAY_HOST_ROOT"),
			compose.DefaultImages(),
			logger,
		)
		if err := l.Start(context.Background(), cfg); err != nil {
			logger.Printf("launcher: %v", err)
		} else {
			// Best-effort cleanup on SIGTERM/SIGINT.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
			go func() {
				<-sigCh
				stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := l.Stop(stopCtx); err != nil {
					logger.Printf("launcher stop: %v", err)
				}
				os.Exit(0)
			}()
		}
	}

	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		logger.Fatalf("server stopped: %v", err)
	}
	return 0
}

// autoBootstrap provisions every resource declared in cfg against the
// emulators using in-network DNS names (pubsub:8085, gcs:4443). It retries
// with exponential backoff until the emulators are up or 2 minutes pass —
// enabling the static emulators-only compose to work without a separate
// `gcp-relay init` step.
func autoBootstrap(logger *log.Logger, cfg *config.Config) {
	opts := bootstrap.Options{
		ProjectID:    cfg.ProjectID,
		PubSubHost:   envOr("PUBSUB_EMULATOR_HOST", "pubsub:8085"),
		GCSHost:      envOr("STORAGE_EMULATOR_HOST", "http://gcs:4443"),
		PushRelayURL: envOr("GCP_RELAY_PUSH_URL", "http://relay:8099"),
		Topic:        envOr("GCP_RELAY_FIREHOSE_TOPIC", "gcs-firehose"),
		ProjectDir:   envOr("GCP_RELAY_PROJECT_DIR", "/config"),
	}
	deadline := time.Now().Add(120 * time.Second)
	backoff := time.Second
	var lastErr error
	for time.Now().Before(deadline) {
		if err := bootstrap.RunFromConfig(cfg, opts); err == nil {
			logger.Printf("auto-bootstrap complete project=%s firehose=%s", opts.ProjectID, opts.Topic)
			return
		} else {
			lastErr = err
			logger.Printf("auto-bootstrap retry in %s: %v", backoff, err)
		}
		time.Sleep(backoff)
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
	logger.Printf("auto-bootstrap gave up after 120s: %v", lastErr)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
