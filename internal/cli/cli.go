package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MHMALEK/gcp-relay/internal/bootstrap"
	"github.com/MHMALEK/gcp-relay/internal/compose"
	"github.com/MHMALEK/gcp-relay/internal/config"
)

func Run(args []string) int {
	if len(args) == 0 {
		printUsage()
		return 1
	}

	switch args[0] {
	case "up":
		return runUp(args[1:])
	case "down":
		return runDown(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "init":
		return runInit(args[1:])
	case "demo":
		return runDemo()
	case "help", "-h", "--help":
		printUsage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		printUsage()
		return 1
	}
}

func printUsage() {
	fmt.Println(`gcp-relay — local GCP event pipeline emulator

Usage:
  gcp-relay serve [--config path] [--port 8099]
  gcp-relay up [--config path] [--build]   Generate compose, start stack, bootstrap
  gcp-relay down [--config path]           Stop the generated stack
  gcp-relay validate [--config path]       Validate the config (incl. function sources)
  gcp-relay init [--config path]           Bootstrap against an already-running stack
  gcp-relay demo                           Upload a demo object to local GCS

Config resolution (when --config is omitted):
  $GCP_RELAY_CONFIG -> ./gcp-relay.yaml -> config/triggers.example.yaml

Environment:
  GCP_RELAY_CONFIG, PUBSUB_EMULATOR_HOST, STORAGE_EMULATOR_HOST,
  GCP_RELAY_IMAGE, GCP_RELAY_PUBSUB_IMAGE, GCP_RELAY_GCS_IMAGE,
  GCP_RELAY_RUNTIME_PYTHON_IMAGE, GCP_RELAY_RUNTIME_NODE_IMAGE, GCP_RELAY_RUNTIME_GO_IMAGE`)
}

func runUp(args []string) int {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	configFlag := fs.String("config", "", "path to gcp-relay config")
	build := fs.Bool("build", false, "build images before starting")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	cfg, cfgPath, dir, ok := loadConfig(*configFlag)
	if !ok {
		return 1
	}
	if err := cfg.ValidateSources(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	genPath, err := writeCompose(cfg, cfgPath, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate compose: %v\n", err)
		return 1
	}
	fmt.Printf("Generated %s\n", genPath)

	composeArgs := []string{"compose", "-f", genPath, "up", "-d"}
	if *build {
		composeArgs = append(composeArgs, "--build")
	}
	if code := dockerCompose(dir, composeArgs...); code != 0 {
		return code
	}

	opts := bootstrapOptions(cfg, dir)
	fmt.Println("Waiting for relay...")
	if err := bootstrap.WaitForRelay(opts.RelayURL, 90*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Println("Bootstrapping buckets, topics, subscriptions...")
	if err := bootstrap.RunFromConfig(cfg, opts); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
		return 1
	}

	fmt.Println("gcp-relay is ready")
	fmt.Println("  Inspector:  http://localhost:8099/ui/")
	fmt.Println("  Relay API:  http://localhost:8099/events")
	fmt.Println("  GCS:        http://localhost:4443")
	return 0
}

func runDown(args []string) int {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	configFlag := fs.String("config", "", "path to gcp-relay config")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfgPath := resolveConfigPath(*configFlag)
	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	dir := filepath.Dir(abs)
	genPath := filepath.Join(dir, ".gcp-relay", "docker-compose.generated.yml")
	if _, err := os.Stat(genPath); err != nil {
		fmt.Fprintln(os.Stderr, "no generated compose found; run `gcp-relay up` first")
		return 1
	}
	return dockerCompose(dir, "compose", "-f", genPath, "down")
}

func runValidate(args []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	configFlag := fs.String("config", "", "path to gcp-relay config")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfg, cfgPath, _, ok := loadConfig(*configFlag)
	if !ok {
		return 1
	}
	if err := cfg.ValidateSources(); err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	fmt.Printf("ok: %s (version=%s functions=%d notifications=%d buckets=%d)\n",
		cfgPath, cfg.Version, len(cfg.Functions), len(cfg.Notifications), len(cfg.Buckets))
	return 0
}

func runInit(args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configFlag := fs.String("config", "", "path to gcp-relay config")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfg, _, dir, ok := loadConfig(*configFlag)
	if !ok {
		return 1
	}
	if err := bootstrap.RunFromConfig(cfg, bootstrapOptions(cfg, dir)); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
		return 1
	}
	fmt.Println("Bootstrap complete")
	return 0
}

func runDemo() int {
	opts := bootstrap.DefaultOptions()
	if err := bootstrap.UploadDemo(opts, "uploads/hello.txt", "hello from gcp-relay demo"); err != nil {
		fmt.Fprintf(os.Stderr, "demo upload: %v\n", err)
		return 1
	}
	fmt.Printf("Uploaded gs://%s/uploads/hello.txt\n", opts.Bucket)
	fmt.Println("Check the inspector at http://localhost:8099/ui/")
	return 0
}

// loadConfig resolves, loads, and returns the config plus its absolute path and
// directory. On error it prints and returns ok=false.
func loadConfig(configFlag string) (cfg *config.Config, cfgPath, dir string, ok bool) {
	cfgPath = resolveConfigPath(configFlag)
	abs, err := filepath.Abs(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return nil, "", "", false
	}
	cfg, err = config.Load(abs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		return nil, "", "", false
	}
	return cfg, abs, filepath.Dir(abs), true
}

func bootstrapOptions(cfg *config.Config, dir string) bootstrap.Options {
	return bootstrap.Options{
		ProjectID:    cfg.ProjectID,
		PubSubHost:   envOr("PUBSUB_EMULATOR_HOST", "localhost:8085"),
		GCSHost:      envOr("STORAGE_EMULATOR_HOST", "http://localhost:4443"),
		RelayURL:     envOr("GCP_RELAY_URL", "http://localhost:8099"),
		PushRelayURL: envOr("GCP_RELAY_PUSH_URL", "http://relay:8099"),
		Topic:        compose.FirehoseTopic,
		ProjectDir:   dir,
	}
}

func writeCompose(cfg *config.Config, cfgPath, dir string) (string, error) {
	out, err := compose.Generate(cfg, compose.Options{ConfigPath: cfgPath, ProjectDir: dir})
	if err != nil {
		return "", err
	}
	genDir := filepath.Join(dir, ".gcp-relay")
	if err := os.MkdirAll(filepath.Join(genDir, "storage"), 0o755); err != nil {
		return "", err
	}
	genPath := filepath.Join(genDir, "docker-compose.generated.yml")
	if err := os.WriteFile(genPath, out, 0o644); err != nil {
		return "", err
	}
	return genPath, nil
}

func resolveConfigPath(configFlag string) string {
	if configFlag != "" {
		return configFlag
	}
	if v := os.Getenv("GCP_RELAY_CONFIG"); v != "" {
		return v
	}
	for _, c := range []string{"gcp-relay.yaml", "gcp-relay.yml"} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return "config/triggers.example.yaml"
}

func dockerCompose(dir string, args ...string) int {
	cmd := exec.Command("docker", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return 1
	}
	return 0
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
