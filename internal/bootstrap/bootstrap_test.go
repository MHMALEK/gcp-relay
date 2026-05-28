package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/config"
)

func recordingServer(t *testing.T, log *[]string, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*log = append(*log, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
}

func TestRunFromConfig(t *testing.T) {
	var mu sync.Mutex
	var pubsubLog, gcsLog []string

	pubsub := recordingServer(t, &pubsubLog, &mu)
	defer pubsub.Close()
	gcs := recordingServer(t, &gcsLog, &mu)
	defer gcs.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "seed.csv"), []byte("a,b,c\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		ProjectID: "demo",
		Buckets: []config.Bucket{{
			Name:       "raw",
			Versioning: true,
			Seed:       []config.SeedObject{{Object: "incoming/seed.csv", From: "seed.csv"}},
		}},
		PubSub: config.PubSub{
			Topics:        []config.Topic{{Name: "orders"}},
			Subscriptions: []config.Subscription{{Name: "orders-worker", Topic: "orders", PushEndpoint: "http://worker:8080"}},
		},
		Notifications: []config.Notification{{Bucket: "raw", Topic: "raw-events", EventTypes: []string{"OBJECT_FINALIZE"}}},
		Functions: []config.Function{{
			Name: "fanout", URL: "http://x", Trigger: config.FunctionTrigger{Topic: "orders"},
		}},
	}

	opts := Options{
		ProjectID:    "demo",
		PubSubHost:   pubsub.URL,
		GCSHost:      gcs.URL,
		PushRelayURL: "http://relay:8099",
		Topic:        "gcs-firehose",
		ProjectDir:   dir,
	}
	if err := RunFromConfig(cfg, opts); err != nil {
		t.Fatal(err)
	}

	want := []string{
		"PUT /v1/projects/demo/topics/gcs-firehose",
		"PUT /v1/projects/demo/topics/orders",
		"PUT /v1/projects/demo/topics/raw-events",
		"PUT /v1/projects/demo/subscriptions/gcs-relay-firehose",
		"PUT /v1/projects/demo/subscriptions/relay-orders",
		"PUT /v1/projects/demo/subscriptions/orders-worker",
	}
	for _, w := range want {
		if !slices.Contains(pubsubLog, w) {
			t.Errorf("missing pubsub call %q\ngot: %v", w, pubsubLog)
		}
	}

	if !slices.Contains(gcsLog, "POST /storage/v1/b") {
		t.Errorf("missing bucket create\ngot: %v", gcsLog)
	}
	if !slices.Contains(gcsLog, "POST /upload/storage/v1/b/raw/o") {
		t.Errorf("missing seed upload\ngot: %v", gcsLog)
	}
}
