package launcher

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/compose"
	"github.com/MHMALEK/gcp-relay/internal/config"
)

// writeFakeDocker creates a shell script that records every invocation's
// args into a log file, so tests can assert on what the launcher would have
// asked the docker daemon to do.
func writeFakeDocker(t *testing.T) (binPath, logPath string) {
	t.Helper()
	dir := t.TempDir()
	binPath = filepath.Join(dir, "docker")
	logPath = filepath.Join(dir, "log")
	script := "#!/bin/sh\necho \"$@\" >> " + logPath + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binPath, logPath
}

func TestDockerLauncherStartLaunchesEachSourceFunction(t *testing.T) {
	docker, logPath := writeFakeDocker(t)
	l := &DockerLauncher{
		Network:  "gcp-relay",
		HostRoot: "/host/project",
		Images:   compose.Images{RuntimePython: "py:test", RuntimeNode: "node:test"},
		Docker:   docker,
	}
	cfg := &config.Config{
		Functions: []config.Function{
			{
				Name: "py-fn", Runtime: "python312", Source: "./functions/py", EntryPoint: "hello",
				Trigger: config.FunctionTrigger{EventFilters: &config.EventFilters{Type: "google.cloud.storage.object.v1.finalized"}},
			},
			{
				Name: "node-fn", Runtime: "nodejs20", Source: "./functions/node", EntryPoint: "handler",
				Trigger: config.FunctionTrigger{HTTP: true},
			},
			// External (url) function — should be skipped
			{Name: "external", URL: "http://x:9090", Trigger: config.FunctionTrigger{HTTP: true}},
		},
	}

	if err := l.Start(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(logPath)
	got := string(data)

	// Python function: launched, cloudevent signature, source mount, image
	if !strings.Contains(got, "gcp-relay-fn-py-fn") {
		t.Errorf("python container name missing: %s", got)
	}
	if !strings.Contains(got, "/host/project/functions/py:/workspace") {
		t.Errorf("python host source mount wrong: %s", got)
	}
	if !strings.Contains(got, "py:test") {
		t.Errorf("python image missing: %s", got)
	}
	if !strings.Contains(got, "FUNCTION_SIGNATURE_TYPE=cloudevent") {
		t.Errorf("python signature type wrong: %s", got)
	}

	// Node function: http signature
	if !strings.Contains(got, "gcp-relay-fn-node-fn") {
		t.Errorf("node container name missing: %s", got)
	}
	if !strings.Contains(got, "FUNCTION_SIGNATURE_TYPE=http") {
		t.Errorf("node signature type wrong: %s", got)
	}

	// External (url) function: must NOT be launched
	if strings.Contains(got, "gcp-relay-fn-external") {
		t.Errorf("url-based function should not be launched: %s", got)
	}

	// Should also have tracked both for cleanup
	if len(l.running) != 2 {
		t.Errorf("expected 2 running containers tracked, got %v", l.running)
	}
}

func TestDockerLauncherStartRequiresHostRoot(t *testing.T) {
	l := &DockerLauncher{}
	err := l.Start(context.Background(), &config.Config{
		Functions: []config.Function{{Name: "x", Runtime: "python312", Source: "./x", EntryPoint: "h"}},
	})
	if err == nil || !strings.Contains(err.Error(), "HostRoot") {
		t.Fatalf("expected HostRoot error, got %v", err)
	}
}

func TestDockerLauncherUnsupportedRuntime(t *testing.T) {
	docker, _ := writeFakeDocker(t)
	l := &DockerLauncher{HostRoot: "/p", Images: compose.Images{}, Docker: docker}
	err := l.Start(context.Background(), &config.Config{
		Functions: []config.Function{{Name: "x", Runtime: "rust1", Source: "./x", EntryPoint: "h"}},
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported runtime") {
		t.Fatalf("expected unsupported runtime error, got %v", err)
	}
}

func TestDockerLauncherStopRemovesTrackedContainers(t *testing.T) {
	docker, logPath := writeFakeDocker(t)
	l := &DockerLauncher{Docker: docker}
	// Pretend Start() already ran
	l.running = []string{"gcp-relay-fn-a", "gcp-relay-fn-b"}

	if err := l.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(logPath)
	got := string(data)
	if c := strings.Count(got, "rm -f"); c != 2 {
		t.Errorf("expected 2 rm calls, got %d: %s", c, got)
	}
	if len(l.running) != 0 {
		t.Errorf("running list should be cleared, got %v", l.running)
	}
}
