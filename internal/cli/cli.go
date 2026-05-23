package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MHMALEK/gcp-relay/internal/bootstrap"
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
		return runCompose("down")
	case "init":
		return runInit()
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
  gcp-relay up [--build]        Start docker compose stack and bootstrap
  gcp-relay down                Stop docker compose stack
  gcp-relay init                Bootstrap Pub/Sub topic, subscription, bucket
  gcp-relay demo                Upload demo object to local GCS

Environment:
  GCP_RELAY_CONFIG, GCP_RELAY_PORT, PUBSUB_EMULATOR_HOST, STORAGE_EMULATOR_HOST`)
}

func projectRoot() (string, error) {
	if root := os.Getenv("GCP_RELAY_ROOT"); root != "" {
		return root, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cwd, nil
		}
		dir = parent
	}
}

func runCompose(args ...string) int {
	root, err := projectRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "project root: %v\n", err)
		return 1
	}
	cmd := exec.Command("docker", append([]string{"compose"}, args...)...)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return 1
	}
	return 0
}

func runUp(args []string) int {
	build := false
	for _, a := range args {
		if a == "--build" {
			build = true
		}
	}
	composeArgs := []string{"up", "-d"}
	if build {
		composeArgs = append(composeArgs, "--build")
	}
	if code := runCompose(composeArgs...); code != 0 {
		return code
	}

	opts := bootstrap.DefaultOptions()
	fmt.Println("Waiting for relay...")
	if err := bootstrap.WaitForRelay(opts.RelayURL, 90*time.Second); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Println("Bootstrapping Pub/Sub + GCS notifications...")
	fmt.Printf("  push endpoint: %s/hooks/pubsub/%s\n", strings.TrimRight(opts.PushRelayURL, "/"), opts.Topic)
	if err := bootstrap.Run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "bootstrap: %v\n", err)
		return 1
	}
	fmt.Println("gcp-relay is ready")
	fmt.Println("  Inspector:  http://localhost:8099/ui/")
	fmt.Println("  Relay API:  http://localhost:8099/events")
	fmt.Println("  GCS:        http://localhost:4443")
	fmt.Println("  Demo:       gcp-relay demo")
	return 0
}

func runInit() int {
	opts := bootstrap.DefaultOptions()
	if err := bootstrap.Run(opts); err != nil {
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
	fmt.Println("Uploaded gs://demo-bucket/uploads/hello.txt")
	fmt.Println("Check relay logs or http://localhost:8099/ui/")
	return 0
}
