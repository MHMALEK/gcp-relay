package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/config"
)

func TestTriggerMatchesObjectPrefix(t *testing.T) {
	tr := config.Trigger{
		Filters: map[string]string{
			"event_type":    "google.cloud.storage.object.v1.finalized",
			"object_prefix": "uploads/",
		},
	}
	if !tr.MatchesObject("uploads/a.txt", "OBJECT_FINALIZE") {
		t.Fatal("expected prefix match")
	}
	if tr.MatchesObject("other/a.txt", "OBJECT_FINALIZE") {
		t.Fatal("expected prefix mismatch")
	}
}

func TestTargetDefaults(t *testing.T) {
	cfg, err := config.Load("../../config/triggers.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Triggers) == 0 {
		t.Fatal("expected triggers")
	}
	if cfg.Triggers[0].Targets[0].Type != "cloudevent" {
		t.Fatalf("type=%q", cfg.Triggers[0].Targets[0].Type)
	}
	if cfg.Version != config.SchemaVersion {
		t.Fatalf("version=%q want %q", cfg.Version, config.SchemaVersion)
	}
}

func TestLoadDefaultsVersionWhenMissing(t *testing.T) {
	path := writeTempConfig(t, `project_id: local-project
triggers:
  - name: t1
    source: pubsub
    topic: gcs-notifications
    targets:
      - url: http://example/
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != config.SchemaVersion {
		t.Fatalf("expected default version %q, got %q", config.SchemaVersion, cfg.Version)
	}
}

func TestLoadRejectsUnknownVersion(t *testing.T) {
	path := writeTempConfig(t, `version: v999
project_id: local-project
`)
	if _, err := config.Load(path); err == nil {
		t.Fatal("expected error for unknown version")
	}
}

func writeTempConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.yaml")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
