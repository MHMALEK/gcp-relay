package router

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"github.com/MHMALEK/gcp-relay/internal/config"
	"github.com/MHMALEK/gcp-relay/internal/history"
	"github.com/MHMALEK/gcp-relay/internal/targets"
)

type Router struct {
	cfg     *config.Config
	client  *http.Client
	log     *log.Logger
	history *history.Store
}

func New(cfg *config.Config, logger *log.Logger, store *history.Store) *Router {
	return &Router{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		log:     logger,
		history: store,
	}
}

func (r *Router) DeliverGCS(ctx context.Context, bucket, objectName string) (history.Record, error) {
	return r.deliver(ctx, "gcs", "", bucket, objectName, cloudevents.TypeStorageObjectFinalized)
}

func (r *Router) HandlePubSubPush(ctx context.Context, topic string, body []byte) (history.Record, error) {
	bucket, objectName, eventType, ok := cloudevents.ParsePubSubPush(body)
	if !ok {
		return history.Record{}, fmt.Errorf("unsupported pubsub payload")
	}
	return r.deliver(ctx, "pubsub", topic, bucket, objectName, eventType)
}

func (r *Router) Replay(ctx context.Context, rec history.Record) (history.Record, error) {
	eventType := rec.EventType
	if eventType == "" {
		eventType = cloudevents.TypeStorageObjectFinalized
	}
	return r.deliver(ctx, "replay", rec.Topic, rec.Bucket, rec.Object, eventType)
}

func (r *Router) deliver(ctx context.Context, source, topic, bucket, objectName, eventType string) (history.Record, error) {
	event := cloudevents.NewStorageObjectFinalized(bucket, objectName)
	rec := history.Record{
		ID:        event.ID,
		Timestamp: time.Now().UTC(),
		Source:    source,
		Topic:     topic,
		Bucket:    bucket,
		Object:    objectName,
		EventType: eventType,
		Event:     event,
	}

	triggers := r.cfg.TriggersForGCS()
	if source == "pubsub" {
		if topicTriggers := r.cfg.TriggersForTopic(topic); len(topicTriggers) > 0 {
			triggers = topicTriggers
		}
	}

	var lastErr error
	delivered := 0
	for _, trigger := range triggers {
		if !trigger.MatchesObject(objectName, eventType) {
			continue
		}
		for _, target := range trigger.Targets {
			d := history.Delivery{
				Trigger:    trigger.Name,
				TargetURL:  targetURL(target),
				TargetType: target.Type,
			}
			if err := targets.Deliver(ctx, r.client, target, event, bucket, objectName); err != nil {
				d.Status = "error"
				d.Error = err.Error()
				lastErr = err
				r.log.Printf("trigger=%s target=%s error=%v", trigger.Name, d.TargetURL, err)
			} else {
				d.Status = "delivered"
				delivered++
				r.log.Printf("trigger=%s target=%s bucket=%s object=%s status=delivered", trigger.Name, d.TargetURL, bucket, objectName)
			}
			rec.Deliveries = append(rec.Deliveries, d)
		}
	}

	r.history.Add(rec)

	if delivered == 0 && lastErr != nil {
		return rec, lastErr
	}
	if delivered == 0 {
		return rec, fmt.Errorf("no matching targets for bucket=%s object=%s", bucket, objectName)
	}
	return rec, nil
}

func targetURL(t config.Target) string {
	return t.URL
}
