// Package compose renders a docker-compose file from a gcp-relay config: the
// emulators (fake-gcs + Pub/Sub), the relay, and one Functions Framework
// runner service per source-based function.
package compose

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MHMALEK/gcp-relay/internal/config"
	"gopkg.in/yaml.v3"
)

// FirehoseTopic is the single topic fake-gcs publishes every bucket's object
// events to; the relay fans them out per its config.
const FirehoseTopic = "gcs-firehose"

// Images holds the container images used by the generated stack.
type Images struct {
	Relay         string
	PubSub        string
	GCS           string
	RuntimePython string
	RuntimeNode   string
	RuntimeGo     string
}

// DefaultImages returns the published image set, overridable via env vars
// (handy for local development against freshly built images).
func DefaultImages() Images {
	return Images{
		Relay:         envOr("GCP_RELAY_IMAGE", "ghcr.io/mhmalek/gcp-relay:latest"),
		PubSub:        envOr("GCP_RELAY_PUBSUB_IMAGE", "ghcr.io/mhmalek/gcp-relay-pubsub:latest"),
		GCS:           envOr("GCP_RELAY_GCS_IMAGE", "fsouza/fake-gcs-server:1.54.0"),
		RuntimePython: envOr("GCP_RELAY_RUNTIME_PYTHON_IMAGE", "ghcr.io/mhmalek/gcp-relay-runtime-python:latest"),
		RuntimeNode:   envOr("GCP_RELAY_RUNTIME_NODE_IMAGE", "ghcr.io/mhmalek/gcp-relay-runtime-node:latest"),
		RuntimeGo:     envOr("GCP_RELAY_RUNTIME_GO_IMAGE", "ghcr.io/mhmalek/gcp-relay-runtime-go:latest"),
	}
}

// Options configures compose generation.
type Options struct {
	Images     Images
	ConfigPath string // host path to the config, mounted read-only into the relay
	ProjectDir string // base dir for resolving relative function source paths
	StorageDir string // host dir for the fake-gcs filesystem backend
}

// Generate renders the docker-compose YAML for cfg.
func Generate(cfg *config.Config, opts Options) ([]byte, error) {
	if opts.Images == (Images{}) {
		opts.Images = DefaultImages()
	}
	if opts.ProjectDir == "" {
		opts.ProjectDir = "."
	}
	if opts.StorageDir == "" {
		opts.StorageDir = filepath.Join(opts.ProjectDir, ".gcp-relay", "storage")
	}

	configMount, err := absPath(opts.ProjectDir, opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	storageMount, err := absPath(opts.ProjectDir, opts.StorageDir)
	if err != nil {
		return nil, err
	}

	project := cfg.ProjectID
	if project == "" {
		project = "local-project"
	}

	f := file{Services: map[string]*service{}}

	f.Services["pubsub"] = &service{
		Image: opts.Images.PubSub,
		Ports: []string{"8085:8085"},
		Healthcheck: &healthcheck{
			Test:     []string{"CMD", "curl", "-sf", "http://localhost:8085"},
			Interval: "5s", Timeout: "3s", Retries: 15, StartPeriod: "20s",
		},
	}

	f.Services["gcs"] = &service{
		Image: opts.Images.GCS,
		Command: []string{
			"-scheme", "http",
			"-port", "4443",
			"-backend", "filesystem",
			"-filesystem-root", "/storage",
			"-public-host", "gcs:4443",
			"-event.pubsub-project-id", project,
			"-event.pubsub-topic", FirehoseTopic,
			"-event.list", "finalize,delete,metadataUpdate,archive",
		},
		Environment: map[string]string{"PUBSUB_EMULATOR_HOST": "pubsub:8085"},
		Ports:       []string{"4443:4443"},
		Volumes:     []string{storageMount + ":/storage"},
		DependsOn:   map[string]dep{"pubsub": {Condition: "service_healthy"}},
		Healthcheck: &healthcheck{
			Test:     []string{"CMD", "wget", "-q", "-O", "-", "http://localhost:4443/_internal/healthcheck"},
			Interval: "5s", Timeout: "3s", Retries: 10,
		},
	}

	f.Services["relay"] = &service{
		Image: opts.Images.Relay,
		Environment: map[string]string{
			"GCP_RELAY_CONFIG":         "/config/gcp-relay.yaml",
			"GCP_RELAY_PORT":           "8099",
			"GCP_RELAY_PROJECT":        project,
			"GCP_RELAY_FIREHOSE_TOPIC": FirehoseTopic,
			"GCP_RELAY_PUSH_URL":       "http://relay:8099",
			"PUBSUB_EMULATOR_HOST":     "pubsub:8085",
			"STORAGE_EMULATOR_HOST":    "http://gcs:4443",
		},
		Ports:   []string{"8099:8099"},
		Volumes: []string{configMount + ":/config/gcp-relay.yaml:ro"},
		DependsOn: map[string]dep{
			"pubsub": {Condition: "service_healthy"},
			"gcs":    {Condition: "service_healthy"},
		},
		Healthcheck: &healthcheck{
			Test:     []string{"CMD", "curl", "-sf", "http://localhost:8099/health"},
			Interval: "5s", Timeout: "3s", Retries: 10,
		},
	}

	for _, fn := range cfg.Functions {
		if fn.Source == "" {
			continue // already-running / external target; not managed by us
		}
		image, err := runtimeImage(opts.Images, fn.Runtime)
		if err != nil {
			return nil, fmt.Errorf("function %q: %w", fn.Name, err)
		}
		src, err := absPath(opts.ProjectDir, fn.Source)
		if err != nil {
			return nil, err
		}
		env := map[string]string{
			"FUNCTION_TARGET":         fn.EntryPoint,
			"FUNCTION_SIGNATURE_TYPE": signatureType(fn),
		}
		for k, v := range fn.Env {
			env[k] = v
		}
		svc := &service{
			Image:       image,
			WorkingDir:  "/workspace",
			Environment: env,
			Volumes:     []string{src + ":/workspace"},
			DependsOn:   map[string]dep{"relay": {Condition: "service_started"}},
		}
		if fn.Port != 0 {
			svc.Ports = []string{fmt.Sprintf("%d:8080", fn.Port)}
		}
		f.Services[fn.Name] = svc
	}

	return yaml.Marshal(f)
}

func runtimeImage(images Images, runtime string) (string, error) {
	switch {
	case strings.HasPrefix(runtime, "python"):
		return images.RuntimePython, nil
	case strings.HasPrefix(runtime, "nodejs"):
		return images.RuntimeNode, nil
	case strings.HasPrefix(runtime, "go"):
		return images.RuntimeGo, nil
	default:
		return "", fmt.Errorf("unsupported runtime %q", runtime)
	}
}

func signatureType(fn config.Function) string {
	if fn.Trigger.HTTP {
		return "http"
	}
	return "cloudevent"
}

func absPath(baseDir, p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Abs(filepath.Join(baseDir, p))
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// --- minimal docker-compose schema (deterministic YAML output) ---

type file struct {
	Services map[string]*service `yaml:"services"`
}

type service struct {
	Image       string            `yaml:"image,omitempty"`
	Command     []string          `yaml:"command,omitempty"`
	WorkingDir  string            `yaml:"working_dir,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	DependsOn   map[string]dep    `yaml:"depends_on,omitempty"`
	Healthcheck *healthcheck      `yaml:"healthcheck,omitempty"`
}

type dep struct {
	Condition string `yaml:"condition"`
}

type healthcheck struct {
	Test        []string `yaml:"test"`
	Interval    string   `yaml:"interval,omitempty"`
	Timeout     string   `yaml:"timeout,omitempty"`
	Retries     int      `yaml:"retries,omitempty"`
	StartPeriod string   `yaml:"start_period,omitempty"`
}
