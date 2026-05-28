// Package gohello is a sample Cloud Function that logs every GCS object
// finalize event delivered to it via gcp-relay. Registration happens in
// init() per the Functions Framework Go convention; gcp-relay's go runtime
// container generates a tiny main that imports this package for side
// effects and starts the framework.
package gohello

import (
	"context"
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/GoogleCloudPlatform/functions-framework-go/functions"
)

func init() {
	functions.CloudEvent("hello", HelloHandler)
}

// storageObjectData mirrors the subset of google.events.cloud.storage.v1.StorageObjectData
// fields the example logs.
type storageObjectData struct {
	Bucket string `json:"bucket"`
	Name   string `json:"name"`
	Size   string `json:"size"`
}

// HelloHandler is the registered CloudEvent function entry point.
func HelloHandler(_ context.Context, e cloudevents.Event) error {
	var data storageObjectData
	if err := e.DataAs(&data); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	fmt.Printf("gcp-relay: received %s for gs://%s/%s (size=%s)\n",
		e.Type(), data.Bucket, data.Name, data.Size)
	return nil
}
