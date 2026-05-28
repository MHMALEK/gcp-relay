package router_test

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"github.com/MHMALEK/gcp-relay/internal/config"
	"github.com/MHMALEK/gcp-relay/internal/history"
	"github.com/MHMALEK/gcp-relay/internal/router"
)

type stubPublisher struct {
	mu    sync.Mutex
	calls []pubCall
}

type pubCall struct {
	project string
	topic   string
	data    []byte
	attrs   map[string]string
}

func (s *stubPublisher) Publish(_ context.Context, project, topic string, data []byte, attrs map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, pubCall{project, topic, data, attrs})
	return nil
}

func newRouter(t *testing.T, cfg *config.Config) (*router.Router, *stubPublisher) {
	t.Helper()
	t.Setenv("GCP_RELAY_FIREHOSE_TOPIC", "gcs-firehose")
	r := router.New(cfg, log.New(io.Discard, "", 0), history.NewStore(10))
	pub := &stubPublisher{}
	r.SetPublisher(pub)
	return r, pub
}

// captured captures the binary-mode CloudEvent the router delivers: envelope
// metadata from Ce-* headers, body as the raw data payload.
type captured struct {
	Type    string
	Subject string
	Body    []byte
}

func captureServer(t *testing.T, got *captured) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Type = r.Header.Get("Ce-Type")
		got.Subject = r.Header.Get("Ce-Subject")
		got.Body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
}

func firehosePush(eventType, bucket, object string) []byte {
	body, _ := json.Marshal(map[string]any{
		"message": map[string]any{
			"messageId": "m1",
			"attributes": map[string]string{
				"eventType": eventType,
				"bucketId":  bucket,
				"objectId":  object,
			},
		},
	})
	return body
}

func TestStorageEventRoutesToFunctionAndNotification(t *testing.T) {
	var got captured
	srv := captureServer(t, &got)
	defer srv.Close()

	cfg := &config.Config{
		ProjectID: "test",
		Functions: []config.Function{{
			Name: "fn",
			URL:  srv.URL,
			Trigger: config.FunctionTrigger{EventFilters: &config.EventFilters{
				Type:   cloudevents.TypeObjectFinalized,
				Bucket: "raw",
			}},
		}},
		Notifications: []config.Notification{{
			Bucket:     "raw",
			Topic:      "mirror",
			EventTypes: []string{"OBJECT_FINALIZE"},
		}},
	}
	r, pub := newRouter(t, cfg)

	rec, err := r.HandlePubSubPush(context.Background(), "gcs-firehose", firehosePush("OBJECT_FINALIZE", "raw", "incoming/x.csv"))
	if err != nil {
		t.Fatal(err)
	}

	if got.Type != cloudevents.TypeObjectFinalized || got.Subject != "objects/incoming/x.csv" {
		t.Fatalf("function received unexpected event: type=%q subject=%q", got.Type, got.Subject)
	}
	if len(pub.calls) != 1 || pub.calls[0].topic != "mirror" {
		t.Fatalf("expected one notification publish to mirror, got %+v", pub.calls)
	}
	// record should show two deliveries (function + notification), both delivered
	if len(rec.Deliveries) != 2 {
		t.Fatalf("deliveries=%+v", rec.Deliveries)
	}
}

func TestStorageEventNoMatchIsNoOp(t *testing.T) {
	cfg := &config.Config{ProjectID: "test"}
	r, pub := newRouter(t, cfg)

	_, err := r.HandlePubSubPush(context.Background(), "gcs-firehose", firehosePush("OBJECT_FINALIZE", "raw", "x"))
	if err != nil {
		t.Fatalf("unmatched event should be a no-op, got %v", err)
	}
	if len(pub.calls) != 0 {
		t.Fatalf("expected no publishes, got %+v", pub.calls)
	}
}

func TestEventTypeFiltering(t *testing.T) {
	var got captured
	srv := captureServer(t, &got)
	defer srv.Close()

	cfg := &config.Config{
		ProjectID: "test",
		Functions: []config.Function{{
			Name:    "fn",
			URL:     srv.URL,
			Trigger: config.FunctionTrigger{EventFilters: &config.EventFilters{Type: cloudevents.TypeObjectDeleted}},
		}},
	}
	r, _ := newRouter(t, cfg)

	// finalize should NOT match a deleted-only function
	r.HandlePubSubPush(context.Background(), "gcs-firehose", firehosePush("OBJECT_FINALIZE", "raw", "x"))
	if got.Type != "" {
		t.Fatalf("function should not have been called for finalize, got %q", got.Type)
	}

	// delete should match and carry the canonical deleted type
	r.HandlePubSubPush(context.Background(), "gcs-firehose", firehosePush("OBJECT_DELETE", "raw", "x"))
	if got.Type != cloudevents.TypeObjectDeleted {
		t.Fatalf("expected deleted event, got %q", got.Type)
	}
}

func TestTopicTriggeredFunction(t *testing.T) {
	var got captured
	srv := captureServer(t, &got)
	defer srv.Close()

	cfg := &config.Config{
		ProjectID: "test",
		Functions: []config.Function{{
			Name:    "worker",
			URL:     srv.URL,
			Trigger: config.FunctionTrigger{Topic: "orders"},
		}},
	}
	r, _ := newRouter(t, cfg)

	push, _ := json.Marshal(map[string]any{
		"message": map[string]any{"messageId": "m9", "data": "aGVsbG8="}, // "hello"
	})
	if _, err := r.HandlePubSubPush(context.Background(), "orders", push); err != nil {
		t.Fatal(err)
	}
	if got.Type != cloudevents.TypeMessagePublished {
		t.Fatalf("expected messagePublished, got %q", got.Type)
	}
}
