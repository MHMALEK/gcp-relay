package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/config"
	"github.com/MHMALEK/gcp-relay/internal/history"
	"github.com/MHMALEK/gcp-relay/internal/router"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{Version: config.SchemaVersion, ProjectID: "test-project"}
	store := history.NewStore(10)
	r := router.New(cfg, log.New(io.Discard, "", 0), store)
	return New(r, store, log.New(io.Discard, "", 0))
}

func TestAdminBootstrapAgainstFakeAPIs(t *testing.T) {
	pubsubCalls := 0
	gcsCalls := 0

	pubsub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pubsubCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer pubsub.Close()

	gcs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gcsCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer gcs.Close()

	body, _ := json.Marshal(map[string]string{
		"project_id":  "test-project",
		"topic":       "test-topic",
		"bucket":      "test-bucket",
		"push_url":    "http://relay:8099",
		"pubsub_host": strings.TrimPrefix(pubsub.URL, "http://"),
		"gcs_host":    gcs.URL,
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newTestServer(t).Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if pubsubCalls == 0 {
		t.Fatal("expected pubsub emulator to be called")
	}
	if gcsCalls == 0 {
		t.Fatal("expected gcs emulator to be called")
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", resp)
	}
	if resp["topic"] != "test-topic" {
		t.Fatalf("expected topic echoed back, got %v", resp["topic"])
	}
}

func TestAdminBootstrapErrorBubblesUp(t *testing.T) {
	body, _ := json.Marshal(map[string]string{
		"pubsub_host": "127.0.0.1:1",
		"gcs_host":    "http://127.0.0.1:1",
	})

	req := httptest.NewRequest(http.MethodPost, "/admin/bootstrap", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	newTestServer(t).Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d body=%s", rec.Code, rec.Body.String())
	}
}
