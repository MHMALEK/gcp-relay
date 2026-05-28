package config_test

import (
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"github.com/MHMALEK/gcp-relay/internal/config"
)

func TestLoadExampleV2(t *testing.T) {
	cfg, err := config.Load("../../config/triggers.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "v2" {
		t.Fatalf("version=%q", cfg.Version)
	}
	if len(cfg.Functions) != 1 || cfg.Functions[0].Name != "echo-function" {
		t.Fatalf("functions=%+v", cfg.Functions)
	}
	if len(cfg.Notifications) != 1 {
		t.Fatalf("notifications=%+v", cfg.Notifications)
	}
}

func TestV1BackCompatNormalization(t *testing.T) {
	cfg, err := config.Parse([]byte(`version: v1
project_id: local-project
triggers:
  - name: gcs-finalize
    source: pubsub
    topic: gcs-notifications
    filters:
      event_type: google.cloud.storage.object.v1.finalized
      object_prefix: uploads/
    targets:
      - url: http://echo:8080
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Functions) != 1 {
		t.Fatalf("expected 1 normalized function, got %+v", cfg.Functions)
	}
	f := cfg.Functions[0]
	if f.URL != "http://echo:8080" {
		t.Fatalf("url=%q", f.URL)
	}
	if f.Trigger.EventFilters == nil || f.Trigger.EventFilters.Type != cloudevents.TypeObjectFinalized {
		t.Fatalf("event_filters=%+v", f.Trigger.EventFilters)
	}
	if f.Trigger.EventFilters.ObjectNamePrefix != "uploads/" {
		t.Fatalf("prefix=%q", f.Trigger.EventFilters.ObjectNamePrefix)
	}
}

func TestVersionAutoDetect(t *testing.T) {
	// no version + legacy triggers => v1
	cfg, err := config.Parse([]byte(`project_id: p
triggers:
  - name: t
    targets:
      - url: http://x/
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "v1" {
		t.Fatalf("expected v1 auto-detect, got %q", cfg.Version)
	}

	// no version + v2 fields => v2
	cfg2, err := config.Parse([]byte(`project_id: p
buckets:
  - name: b
`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.Version != "v2" {
		t.Fatalf("expected v2 auto-detect, got %q", cfg2.Version)
	}
}

func TestRejectsUnknownVersion(t *testing.T) {
	if _, err := config.Parse([]byte(`version: v999`)); err == nil {
		t.Fatal("expected error for unknown version")
	}
}

func TestValidationErrors(t *testing.T) {
	cases := map[string]string{
		"duplicate function name": `
functions:
  - name: dup
    url: http://a/
    trigger: { http: true }
  - name: dup
    url: http://b/
    trigger: { http: true }
`,
		"no trigger": `
functions:
  - name: f
    url: http://a/
`,
		"two triggers": `
functions:
  - name: f
    url: http://a/
    trigger:
      http: true
      topic: t
`,
		"bad event type": `
functions:
  - name: f
    url: http://a/
    trigger:
      event_filters:
        type: not.a.real.type
`,
		"bad runtime": `
functions:
  - name: f
    source: ./fn
    runtime: rust1
    trigger: { http: true }
`,
		"source or url required": `
functions:
  - name: f
    trigger: { http: true }
`,
		"bad notification event type": `
notifications:
  - bucket: b
    topic: t
    event_types: [OBJECT_NOPE]
`,
	}
	for name, body := range cases {
		if _, err := config.Parse([]byte(body)); err == nil {
			t.Errorf("%s: expected validation error", name)
		}
	}
}

func TestFunctionsForStorageEvent(t *testing.T) {
	cfg, err := config.Parse([]byte(`
functions:
  - name: any-bucket
    url: http://a/
    trigger:
      event_filters:
        type: google.cloud.storage.object.v1.finalized
  - name: scoped
    url: http://b/
    trigger:
      event_filters:
        type: google.cloud.storage.object.v1.finalized
        bucket: raw
        object_name_prefix: incoming/
  - name: deletes
    url: http://c/
    trigger:
      event_filters:
        type: google.cloud.storage.object.v1.deleted
`))
	if err != nil {
		t.Fatal(err)
	}

	got := names(cfg.FunctionsForStorageEvent("raw", "incoming/x.csv", cloudevents.TypeObjectFinalized))
	if !equal(got, []string{"any-bucket", "scoped"}) {
		t.Fatalf("finalized raw/incoming => %v", got)
	}

	got = names(cfg.FunctionsForStorageEvent("raw", "other/x.csv", cloudevents.TypeObjectFinalized))
	if !equal(got, []string{"any-bucket"}) {
		t.Fatalf("prefix should exclude scoped => %v", got)
	}

	got = names(cfg.FunctionsForStorageEvent("raw", "incoming/x.csv", cloudevents.TypeObjectDeleted))
	if !equal(got, []string{"deletes"}) {
		t.Fatalf("deleted => %v", got)
	}
}

func TestNotificationsForStorageEvent(t *testing.T) {
	cfg, err := config.Parse([]byte(`
notifications:
  - bucket: raw
    topic: t1
    event_types: [OBJECT_FINALIZE]
    object_name_prefix: incoming/
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.NotificationsForStorageEvent("raw", "incoming/a", "OBJECT_FINALIZE")) != 1 {
		t.Fatal("expected match")
	}
	if len(cfg.NotificationsForStorageEvent("raw", "other/a", "OBJECT_FINALIZE")) != 0 {
		t.Fatal("prefix should exclude")
	}
	if len(cfg.NotificationsForStorageEvent("raw", "incoming/a", "OBJECT_DELETE")) != 0 {
		t.Fatal("event type should exclude")
	}
}

func TestTargetURLDefaulting(t *testing.T) {
	f := config.Function{Name: "csv-processor"}
	if f.TargetURL() != "http://csv-processor:8080" {
		t.Fatalf("derived url=%q", f.TargetURL())
	}
	f.URL = "http://explicit:9000"
	if f.TargetURL() != "http://explicit:9000" {
		t.Fatalf("explicit url=%q", f.TargetURL())
	}
}

func names(fns []config.Function) []string {
	out := make([]string, len(fns))
	for i, f := range fns {
		out[i] = f.Name
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
