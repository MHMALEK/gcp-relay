package config_test

import (
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
}
