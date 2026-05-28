package targets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
)

// DeliverCloudEvent POSTs a CloudEvent to url in structured JSON content mode,
// also setting the binary-mode Ce-* headers so any Functions Framework target
// (which accepts either) can parse it.
func DeliverCloudEvent(ctx context.Context, client *http.Client, url, method string, event cloudevents.Envelope) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if method == "" {
		method = http.MethodPost
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/cloudevents+json")
	req.Header.Set("Ce-Id", event.ID)
	req.Header.Set("Ce-Source", event.Source)
	req.Header.Set("Ce-Type", event.Type)
	req.Header.Set("Ce-Specversion", event.SpecVersion)
	req.Header.Set("Ce-Time", event.Time)
	if event.Subject != "" {
		req.Header.Set("Ce-Subject", event.Subject)
	}

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
