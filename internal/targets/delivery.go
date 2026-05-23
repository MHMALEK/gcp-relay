package targets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"github.com/MHMALEK/gcp-relay/internal/config"
)

func Deliver(ctx context.Context, client *http.Client, target config.Target, event cloudevents.Envelope, bucket, objectName string) error {
	switch strings.ToLower(target.Type) {
	case "", "cloudevent", "cloud_function", "http":
		return deliverCloudEvent(ctx, client, target, event)
	default:
		return fmt.Errorf("unknown target type %q", target.Type)
	}
}

func deliverCloudEvent(ctx context.Context, client *http.Client, target config.Target, event cloudevents.Envelope) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}

	method := target.Method
	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, target.URL, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/cloudevents+json")
	req.Header.Set("Ce-Id", event.ID)
	req.Header.Set("Ce-Source", event.Source)
	req.Header.Set("Ce-Type", event.Type)
	req.Header.Set("Ce-Specversion", event.SpecVersion)
	req.Header.Set("Ce-Time", event.Time)

	return roundTrip(client, req)
}

func roundTrip(client *http.Client, req *http.Request) error {
	resp, err := client.Do(req)
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
