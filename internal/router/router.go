package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"github.com/MHMALEK/gcp-relay/internal/config"
)

type Router struct {
	cfg    *config.Config
	client *http.Client
	log    *log.Logger
}

func New(cfg *config.Config, logger *log.Logger) *Router {
	return &Router{
		cfg: cfg,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		log: logger,
	}
}

func (r *Router) DeliverGCS(ctx context.Context, bucket, objectName string) error {
	event := cloudevents.NewStorageObjectFinalized(bucket, objectName)
	triggers := r.cfg.TriggersForGCS()
	if len(triggers) == 0 {
		return fmt.Errorf("no triggers configured")
	}

	var lastErr error
	delivered := 0
	for _, trigger := range triggers {
		if !cloudevents.MatchesFilter(trigger.Filters, cloudevents.TypeStorageObjectFinalized) {
			continue
		}
		for _, target := range trigger.Targets {
			if err := r.postCloudEvent(ctx, target, event); err != nil {
				lastErr = err
				r.log.Printf("trigger=%s target=%s error=%v", trigger.Name, target.URL, err)
				continue
			}
			delivered++
			r.log.Printf("trigger=%s target=%s bucket=%s object=%s status=delivered", trigger.Name, target.URL, bucket, objectName)
		}
	}

	if delivered == 0 && lastErr != nil {
		return lastErr
	}
	if delivered == 0 {
		return fmt.Errorf("no matching targets for bucket=%s object=%s", bucket, objectName)
	}
	return nil
}

func (r *Router) HandlePubSubPush(ctx context.Context, topic string, body []byte) error {
	bucket, objectName, eventType, ok := cloudevents.ParsePubSubPush(body)
	if !ok {
		return fmt.Errorf("unsupported pubsub payload")
	}

	triggers := r.cfg.TriggersForTopic(topic)
	if len(triggers) == 0 {
		triggers = r.cfg.TriggersForGCS()
	}

	var lastErr error
	delivered := 0
	event := cloudevents.NewStorageObjectFinalized(bucket, objectName)

	for _, trigger := range triggers {
		if !cloudevents.MatchesFilter(trigger.Filters, eventType) {
			continue
		}
		for _, target := range trigger.Targets {
			if err := r.postCloudEvent(ctx, target, event); err != nil {
				lastErr = err
				r.log.Printf("trigger=%s topic=%s target=%s error=%v", trigger.Name, topic, target.URL, err)
				continue
			}
			delivered++
			r.log.Printf("trigger=%s topic=%s target=%s bucket=%s object=%s status=delivered", trigger.Name, topic, target.URL, bucket, objectName)
		}
	}

	if delivered == 0 && lastErr != nil {
		return lastErr
	}
	if delivered == 0 {
		return fmt.Errorf("no matching targets for topic=%s", topic)
	}
	return nil
}

func (r *Router) postCloudEvent(ctx context.Context, target config.Target, event cloudevents.Envelope) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, target.Method, target.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/cloudevents+json")
	req.Header.Set("Ce-Id", event.ID)
	req.Header.Set("Ce-Source", event.Source)
	req.Header.Set("Ce-Type", event.Type)
	req.Header.Set("Ce-Specversion", event.SpecVersion)
	req.Header.Set("Ce-Time", event.Time)

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("target returned %s", resp.Status)
	}
	return nil
}
