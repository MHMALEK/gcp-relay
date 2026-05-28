package targets

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
)

// DeliverCloudEvent POSTs a CloudEvent in HTTP binary content mode, matching
// what real Eventarc delivers to Cloud Run / Cloud Functions 2nd gen: the
// body is just the `data` payload, with envelope metadata in `Ce-*` headers.
// Mixing binary and structured modes confuses some Functions Framework
// implementations (Node treats Ce-* presence as binary mode and reads the
// body as data — so a structured envelope ends up with `data` nested twice).
func DeliverCloudEvent(ctx context.Context, client *http.Client, url, method string, event cloudevents.Envelope) error {
	if method == "" {
		method = http.MethodPost
	}
	body := []byte(event.Data)

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	contentType := event.DataContentType
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Ce-Specversion", event.SpecVersion)
	req.Header.Set("Ce-Id", event.ID)
	req.Header.Set("Ce-Source", event.Source)
	req.Header.Set("Ce-Type", event.Type)
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
