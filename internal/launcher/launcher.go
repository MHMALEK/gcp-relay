// Package launcher manages the lifecycle of function containers when the
// relay runs alongside the docker daemon (via /var/run/docker.sock) rather
// than alongside the gcp-relay CLI.
//
// This is the "Model B" run path: users add only the relay service to their
// own docker-compose with the socket mounted, and the relay process itself
// launches one container per source-based function from the config — no
// `gcp-relay up` invocation needed.
package launcher

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/MHMALEK/gcp-relay/internal/compose"
	"github.com/MHMALEK/gcp-relay/internal/config"
)

// Launcher manages the lifecycle of the function containers declared in a
// gcp-relay config.
type Launcher interface {
	Start(ctx context.Context, cfg *config.Config) error
	Stop(ctx context.Context) error
}

// DockerLauncher shells out to the docker CLI to start function containers.
// The relay's image installs docker-cli; the user's compose mounts
// /var/run/docker.sock into it.
//
// Volume mounts are resolved against HostRoot — the *host* path of the
// user's project directory. This is the classic docker-out-of-docker
// sharp edge: the relay sees only its own filesystem, but the daemon
// creating sibling containers needs host paths.
type DockerLauncher struct {
	Network  string         // docker network the function containers join
	HostRoot string         // host path of the user's project (GCP_RELAY_HOST_ROOT)
	Images   compose.Images // runtime images per language
	Docker   string         // "docker" by default; overridable for tests
	Logger   *log.Logger    // optional; nil discards

	mu      sync.Mutex
	running []string
}

// NewDocker builds a DockerLauncher with sensible defaults.
func NewDocker(network, hostRoot string, images compose.Images, logger *log.Logger) *DockerLauncher {
	if network == "" {
		network = "gcp-relay"
	}
	return &DockerLauncher{
		Network: network, HostRoot: hostRoot, Images: images,
		Docker: "docker", Logger: logger,
	}
}

// Start launches one container per source-based function in cfg.
func (d *DockerLauncher) Start(ctx context.Context, cfg *config.Config) error {
	if d.HostRoot == "" {
		return fmt.Errorf("DockerLauncher: HostRoot is required (set GCP_RELAY_HOST_ROOT to your project's host path)")
	}
	for _, fn := range cfg.Functions {
		if fn.Source == "" {
			continue // url-based / external function; not ours to launch
		}
		if err := d.launch(ctx, fn); err != nil {
			return err
		}
	}
	return nil
}

// Stop removes every container Start has launched (best-effort).
func (d *DockerLauncher) Stop(ctx context.Context) error {
	d.mu.Lock()
	names := append([]string(nil), d.running...)
	d.running = d.running[:0]
	d.mu.Unlock()

	var lastErr error
	for _, name := range names {
		cmd := exec.CommandContext(ctx, d.Docker, "rm", "-f", name)
		if out, err := cmd.CombinedOutput(); err != nil {
			lastErr = fmt.Errorf("docker rm %s: %w: %s", name, err, strings.TrimSpace(string(out)))
			d.log(lastErr.Error())
		}
	}
	return lastErr
}

func (d *DockerLauncher) launch(ctx context.Context, fn config.Function) error {
	image, err := runtimeImage(d.Images, fn.Runtime)
	if err != nil {
		return fmt.Errorf("function %q: %w", fn.Name, err)
	}
	name := "gcp-relay-fn-" + fn.Name
	hostSource := filepath.Join(d.HostRoot, fn.Source)

	// Best-effort: remove any stale container with the same name from a prior run.
	_ = exec.CommandContext(ctx, d.Docker, "rm", "-f", name).Run()

	args := []string{
		"run", "-d",
		"--name", name,
		"--network", d.Network,
		"--network-alias", fn.Name,
		"-v", hostSource + ":/workspace",
		"-e", "FUNCTION_TARGET=" + fn.EntryPoint,
		"-e", "FUNCTION_SIGNATURE_TYPE=" + signatureType(fn),
	}
	if fn.Port != 0 {
		args = append(args, "-p", fmt.Sprintf("%d:8080", fn.Port))
	}
	for k, v := range fn.Env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, image)

	cmd := exec.CommandContext(ctx, d.Docker, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	d.mu.Lock()
	d.running = append(d.running, name)
	d.mu.Unlock()
	d.log("launched function=%s image=%s source=%s", fn.Name, image, hostSource)
	return nil
}

func (d *DockerLauncher) log(format string, args ...any) {
	if d.Logger != nil {
		d.Logger.Printf(format, args...)
	}
}

func runtimeImage(images compose.Images, runtime string) (string, error) {
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
