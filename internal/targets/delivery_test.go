package targets_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"github.com/MHMALEK/gcp-relay/internal/config"
	"github.com/MHMALEK/gcp-relay/internal/targets"
)

func TestDeliverCloudEvent(t *testing.T) {
	var got cloudevents.Envelope
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	event := cloudevents.NewStorageObjectFinalized("b", "o.txt")
	err := targets.Deliver(context.Background(), srv.Client(), config.Target{
		Type: "cloudevent",
		URL:  srv.URL,
	}, event, "b", "o.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != cloudevents.TypeStorageObjectFinalized {
		t.Fatalf("type=%q", got.Type)
	}
}

func TestDeliverAirflow(t *testing.T) {
	var dagID string
	var conf map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dagID = r.URL.Path
		var body struct {
			Conf map[string]string `json:"conf"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		conf = body.Conf
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	err := targets.Deliver(context.Background(), srv.Client(), config.Target{
		Type:  "airflow",
		URL:   srv.URL,
		DagID: "ingest_dag",
		Auth:  "admin:admin",
	}, cloudevents.Envelope{}, "demo-bucket", "file.csv")
	if err != nil {
		t.Fatal(err)
	}
	if dagID != "/api/v1/dags/ingest_dag/dagRuns" {
		t.Fatalf("path=%q", dagID)
	}
	if conf["bucket"] != "demo-bucket" || conf["name"] != "file.csv" {
		t.Fatalf("conf=%v", conf)
	}
}
