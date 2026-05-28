package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"github.com/MHMALEK/gcp-relay/internal/config"
	"github.com/MHMALEK/gcp-relay/internal/history"
	"github.com/MHMALEK/gcp-relay/internal/targets"
)

// Publisher publishes a message to a Pub/Sub topic (used for GCS bucket
// notification republishing).
type Publisher interface {
	Publish(ctx context.Context, project, topic string, data []byte, attrs map[string]string) error
}

type Router struct {
	cfg           *config.Config
	client        *http.Client
	log           *log.Logger
	history       *history.Store
	publisher     Publisher
	firehoseTopic string
	project       string
}

func New(cfg *config.Config, logger *log.Logger, store *history.Store) *Router {
	client := &http.Client{Timeout: 30 * time.Second}
	pubsubHost := envOr("PUBSUB_EMULATOR_HOST", "localhost:8085")
	firehose := envOr("GCP_RELAY_FIREHOSE_TOPIC", envOr("GCP_RELAY_GCS_TOPIC", "gcs-notifications"))
	return &Router{
		cfg:           cfg,
		client:        client,
		log:           logger,
		history:       store,
		publisher:     &httpPublisher{client: client, host: pubsubHost},
		firehoseTopic: firehose,
		project:       cfg.ProjectID,
	}
}

// SetPublisher overrides the Pub/Sub publisher (used in tests).
func (r *Router) SetPublisher(p Publisher) { r.publisher = p }

// DeliverGCS handles a manual GCS object-finalized event (POST /events/gcs).
func (r *Router) DeliverGCS(ctx context.Context, bucket, objectName string) (history.Record, error) {
	data := cloudevents.StorageObjectData{Bucket: bucket, Name: objectName}
	return r.deliverStorage(ctx, "gcs", "", bucket, objectName, "OBJECT_FINALIZE", data, "", "")
}

// HandlePubSubPush routes a Pub/Sub push. Pushes on the firehose topic are GCS
// object events (Eventarc + notification routing); pushes on any other topic
// are delivered as messagePublished CloudEvents to topic-triggered functions.
func (r *Router) HandlePubSubPush(ctx context.Context, topic string, body []byte) (history.Record, error) {
	if topic == r.firehoseTopic {
		parsed := cloudevents.ParsePubSubPush(body)
		if !parsed.OK {
			return history.Record{}, fmt.Errorf("unsupported gcs notification payload on firehose topic %q", topic)
		}
		return r.deliverStorage(ctx, "pubsub", topic, parsed.Bucket, parsed.Name, parsed.EventType, parsed.Data, parsed.MessageID, parsed.Time)
	}
	return r.deliverPubSub(ctx, topic, body)
}

// Replay re-delivers a previously recorded event.
func (r *Router) Replay(ctx context.Context, rec history.Record) (history.Record, error) {
	if rec.Event.Type == cloudevents.TypeMessagePublished {
		out := r.newRecord("replay", rec.Topic, "", "", cloudevents.TypeMessagePublished, rec.Event)
		delivered, lastErr := r.fanout(ctx, r.cfg.FunctionsForTopic(rec.Topic), rec.Event, &out)
		r.history.Add(out)
		return r.result(out, delivered, lastErr)
	}
	var data cloudevents.StorageObjectData
	_ = json.Unmarshal(rec.Event.Data, &data)
	shortType := rec.EventType
	if shortType == "" {
		shortType = "OBJECT_FINALIZE"
	}
	return r.deliverStorage(ctx, "replay", rec.Topic, rec.Bucket, rec.Object, shortType, data, "", "")
}

func (r *Router) deliverStorage(ctx context.Context, source, topic, bucket, name, shortType string, data cloudevents.StorageObjectData, id, eventTime string) (history.Record, error) {
	canonical := cloudevents.CanonicalStorageType(shortType)
	if canonical == "" {
		canonical = cloudevents.TypeObjectFinalized
	}
	event := cloudevents.NewStorageObjectEvent(canonical, bucket, name, data, id, eventTime)
	rec := r.newRecord(source, topic, bucket, name, shortType, event)

	// Eventarc role: deliver to functions whose event_filters match.
	delivered, lastErr := r.fanout(ctx, r.cfg.FunctionsForStorageEvent(bucket, name, canonical), event, &rec)

	// Notification role: republish to matching bucket-notification topics.
	for _, n := range r.cfg.NotificationsForStorageEvent(bucket, name, shortType) {
		d := history.Delivery{Trigger: "notification:" + n.Topic, TargetURL: "pubsub://" + n.Topic, TargetType: "pubsub"}
		payload := event.Data
		if n.PayloadFormat == "NONE" {
			payload = nil
		}
		attrs := storageAttrs(shortType, bucket, name, data, n.CustomAttributes)
		if err := r.publisher.Publish(ctx, r.project, n.Topic, payload, attrs); err != nil {
			d.Status = "error"
			d.Error = err.Error()
			lastErr = err
			r.log.Printf("notification topic=%s error=%v", n.Topic, err)
		} else {
			d.Status = "delivered"
			delivered++
			r.log.Printf("notification topic=%s bucket=%s object=%s status=published", n.Topic, bucket, name)
		}
		rec.Deliveries = append(rec.Deliveries, d)
	}

	r.history.Add(rec)
	return r.result(rec, delivered, lastErr)
}

func (r *Router) deliverPubSub(ctx context.Context, topic string, body []byte) (history.Record, error) {
	data, attrs, msgID, pubTime, ok := cloudevents.ParsePubSubMessage(body)
	if !ok {
		return history.Record{}, fmt.Errorf("invalid pubsub push on topic %q", topic)
	}
	event := cloudevents.NewMessagePublishedEvent(r.project, topic, data, attrs, msgID, pubTime)
	rec := r.newRecord("pubsub", topic, "", "", cloudevents.TypeMessagePublished, event)

	delivered, lastErr := r.fanout(ctx, r.cfg.FunctionsForTopic(topic), event, &rec)

	r.history.Add(rec)
	return r.result(rec, delivered, lastErr)
}

// fanout delivers event to each function and records the outcome.
func (r *Router) fanout(ctx context.Context, fns []config.Function, event cloudevents.Envelope, rec *history.Record) (delivered int, lastErr error) {
	for _, fn := range fns {
		d := history.Delivery{Trigger: fn.Name, TargetURL: fn.TargetURL(), TargetType: "cloudevent"}
		if err := targets.DeliverCloudEvent(ctx, r.client, fn.TargetURL(), http.MethodPost, event); err != nil {
			d.Status = "error"
			d.Error = err.Error()
			lastErr = err
			r.log.Printf("function=%s target=%s error=%v", fn.Name, d.TargetURL, err)
		} else {
			d.Status = "delivered"
			delivered++
			r.log.Printf("function=%s target=%s type=%s status=delivered", fn.Name, d.TargetURL, event.Type)
		}
		rec.Deliveries = append(rec.Deliveries, d)
	}
	return delivered, lastErr
}

func (r *Router) newRecord(source, topic, bucket, object, eventType string, event cloudevents.Envelope) history.Record {
	return history.Record{
		ID:        event.ID,
		Timestamp: time.Now().UTC(),
		Source:    source,
		Topic:     topic,
		Bucket:    bucket,
		Object:    object,
		EventType: eventType,
		Event:     event,
	}
}

// result reports an error only when nothing was delivered and a delivery
// failed. An unmatched event (no functions/notifications) is a successful
// no-op — the firehose receives every upload, most of which match nothing.
func (r *Router) result(rec history.Record, delivered int, lastErr error) (history.Record, error) {
	if delivered == 0 && lastErr != nil {
		return rec, lastErr
	}
	return rec, nil
}

func storageAttrs(shortType, bucket, name string, data cloudevents.StorageObjectData, custom map[string]string) map[string]string {
	attrs := map[string]string{
		"eventType":     shortType,
		"bucketId":      bucket,
		"objectId":      name,
		"payloadFormat": "JSON_API_V1",
	}
	if data.Generation != "" {
		attrs["objectGeneration"] = data.Generation
	}
	for k, v := range custom {
		attrs[k] = v
	}
	return attrs
}

// httpPublisher publishes to the Pub/Sub emulator's REST API.
type httpPublisher struct {
	client *http.Client
	host   string
}

func (p *httpPublisher) Publish(ctx context.Context, project, topic string, data []byte, attrs map[string]string) error {
	msg := map[string]any{}
	if data != nil {
		msg["data"] = base64.StdEncoding.EncodeToString(data)
	}
	if len(attrs) > 0 {
		msg["attributes"] = attrs
	}
	body, _ := json.Marshal(map[string]any{"messages": []any{msg}})
	url := fmt.Sprintf("%s/v1/projects/%s/topics/%s:publish", pubsubBaseURL(p.host), project, topic)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("publish to %s returned %s", topic, resp.Status)
	}
	return nil
}

func pubsubBaseURL(host string) string {
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	return "http://" + host
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
