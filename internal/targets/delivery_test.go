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

func TestDeliverCloudEvent(t *testing.T) {
	var got cloudevents.Envelope
	var subject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject = r.Header.Get("Ce-Subject")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	event := cloudevents.NewStorageObjectEvent(cloudevents.TypeObjectFinalized, "b", "o.txt", cloudevents.StorageObjectData{}, "", "")
	if err := targets.DeliverCloudEvent(context.Background(), srv.Client(), srv.URL, http.MethodPost, event); err != nil {
		t.Fatal(err)
	}
	if got.Type != cloudevents.TypeObjectFinalized {
		t.Fatalf("type=%q", got.Type)
	}
	if subject != "objects/o.txt" {
		t.Fatalf("Ce-Subject=%q", subject)
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
