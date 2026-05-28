package compose

import (
	"strings"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/config"
	"gopkg.in/yaml.v3"
)

func testImages() Images {
	return Images{
		Relay:         "relay:test",
		PubSub:        "pubsub:test",
		GCS:           "gcs:test",
		RuntimePython: "py:test",
		RuntimeNode:   "node:test",
		RuntimeGo:     "go:test",
	}
}

func TestGenerate(t *testing.T) {
	cfg := &config.Config{
		ProjectID: "demo",
		Functions: []config.Function{
			{
				Name:       "csv-processor",
				Runtime:    "python312",
				Source:     "./functions/csv",
				EntryPoint: "process_csv",
				Trigger:    config.FunctionTrigger{EventFilters: &config.EventFilters{Type: "google.cloud.storage.object.v1.finalized"}},
			},
			{
				Name:    "external",
				URL:     "http://external:8080",
				Trigger: config.FunctionTrigger{EventFilters: &config.EventFilters{Type: "google.cloud.storage.object.v1.finalized"}},
			},
		},
	}

	out, err := Generate(cfg, Options{Images: testImages(), ConfigPath: "gcp-relay.yaml", ProjectDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	var f file
	if err := yaml.Unmarshal(out, &f); err != nil {
		t.Fatalf("generated invalid yaml: %v", err)
	}

	for _, name := range []string{"pubsub", "gcs", "relay", "csv-processor"} {
		if f.Services[name] == nil {
			t.Fatalf("missing service %q in:\n%s", name, out)
		}
	}
	if f.Services["external"] != nil {
		t.Fatal("url-based function must NOT get a runner service")
	}

	gcsCmd := strings.Join(f.Services["gcs"].Command, " ")
	if strings.Contains(gcsCmd, "-event.bucket") {
		t.Fatal("firehose mode must not pin a single -event.bucket")
	}
	if !strings.Contains(gcsCmd, FirehoseTopic) {
		t.Fatalf("gcs command missing firehose topic: %s", gcsCmd)
	}
	if !strings.Contains(gcsCmd, "finalize,delete,metadataUpdate,archive") {
		t.Fatalf("gcs command missing full event list: %s", gcsCmd)
	}

	py := f.Services["csv-processor"]
	if py.Image != "py:test" {
		t.Fatalf("python runtime image=%q", py.Image)
	}
	if py.Environment["FUNCTION_TARGET"] != "process_csv" {
		t.Fatalf("FUNCTION_TARGET=%q", py.Environment["FUNCTION_TARGET"])
	}
	if py.Environment["FUNCTION_SIGNATURE_TYPE"] != "cloudevent" {
		t.Fatalf("signature type=%q", py.Environment["FUNCTION_SIGNATURE_TYPE"])
	}
	if len(py.Volumes) != 1 || !strings.HasSuffix(py.Volumes[0], ":/workspace") {
		t.Fatalf("source volume=%v", py.Volumes)
	}

	relay := f.Services["relay"]
	if relay.Environment["GCP_RELAY_FIREHOSE_TOPIC"] != FirehoseTopic {
		t.Fatalf("relay firehose env=%q", relay.Environment["GCP_RELAY_FIREHOSE_TOPIC"])
	}
	if len(relay.Volumes) != 1 || !strings.HasSuffix(relay.Volumes[0], ":/config/gcp-relay.yaml:ro") {
		t.Fatalf("relay config volume=%v", relay.Volumes)
	}
}

func TestGenerateUnsupportedRuntime(t *testing.T) {
	cfg := &config.Config{
		Functions: []config.Function{{
			Name: "f", Runtime: "rust1", Source: "./f", EntryPoint: "h",
			Trigger: config.FunctionTrigger{HTTP: true},
		}},
	}
	if _, err := Generate(cfg, Options{Images: testImages(), ConfigPath: "c.yaml", ProjectDir: t.TempDir()}); err == nil {
		t.Fatal("expected error for unsupported runtime")
	}
}

func TestSignatureTypeHTTP(t *testing.T) {
	if got := signatureType(config.Function{Trigger: config.FunctionTrigger{HTTP: true}}); got != "http" {
		t.Fatalf("http signature=%q", got)
	}
	if got := signatureType(config.Function{Trigger: config.FunctionTrigger{Topic: "t"}}); got != "cloudevent" {
		t.Fatalf("topic signature=%q", got)
	}
}
