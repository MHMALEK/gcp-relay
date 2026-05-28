package targets_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"github.com/MHMALEK/gcp-relay/internal/targets"
)

// TestDeliverCloudEvent verifies binary-mode CloudEvents delivery: envelope
// metadata in Ce-* headers, body is just the `data` payload (a
// StorageObjectData JSON in this case), matching real Eventarc.
func TestDeliverCloudEvent(t *testing.T) {
	var ceType, ceSubject, ceID, ceSource, ctype string
	var data cloudevents.StorageObjectData
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ceType = r.Header.Get("Ce-Type")
		ceSubject = r.Header.Get("Ce-Subject")
		ceID = r.Header.Get("Ce-Id")
		ceSource = r.Header.Get("Ce-Source")
		ctype = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &data)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	event := cloudevents.NewStorageObjectEvent(
		cloudevents.TypeObjectFinalized, "b", "o.txt",
		cloudevents.StorageObjectData{Size: "42"}, "id-1", "",
	)
	if err := targets.DeliverCloudEvent(context.Background(), srv.Client(), srv.URL, http.MethodPost, event); err != nil {
		t.Fatal(err)
	}

	if ceType != cloudevents.TypeObjectFinalized {
		t.Errorf("Ce-Type=%q", ceType)
	}
	if ceSubject != "objects/o.txt" {
		t.Errorf("Ce-Subject=%q", ceSubject)
	}
	if ceID != "id-1" {
		t.Errorf("Ce-Id=%q", ceID)
	}
	if ceSource != "//storage.googleapis.com/projects/_/buckets/b" {
		t.Errorf("Ce-Source=%q", ceSource)
	}
	if ctype != "application/json" {
		t.Errorf("Content-Type=%q", ctype)
	}
	if data.Bucket != "b" || data.Name != "o.txt" || data.Size != "42" {
		t.Errorf("body should be StorageObjectData directly, got %+v", data)
	}
}

func TestDeliverCloudEventErrorsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	event := cloudevents.NewStorageObjectEvent(cloudevents.TypeObjectFinalized, "b", "o.txt", cloudevents.StorageObjectData{}, "", "")
	if err := targets.DeliverCloudEvent(context.Background(), srv.Client(), srv.URL, http.MethodPost, event); err == nil {
		t.Fatal("expected error for 500 response")
	}
}
