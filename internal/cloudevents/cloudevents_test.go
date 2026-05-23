package cloudevents_test

import (
	"encoding/json"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
)

func TestParsePubSubPushAttributes(t *testing.T) {
	body := []byte(`{
		"message": {
			"attributes": {
				"eventType": "OBJECT_FINALIZE",
				"bucketId": "demo-bucket",
				"objectId": "uploads/file.csv"
			}
		}
	}`)

	bucket, name, eventType, ok := cloudevents.ParsePubSubPush(body)
	if !ok {
		t.Fatal("expected ok")
	}
	if bucket != "demo-bucket" || name != "uploads/file.csv" || eventType != "OBJECT_FINALIZE" {
		t.Fatalf("unexpected parse: bucket=%q name=%q type=%q", bucket, name, eventType)
	}
}

func TestNewStorageObjectFinalized(t *testing.T) {
	event := cloudevents.NewStorageObjectFinalized("b", "o.txt")
	if event.Type != cloudevents.TypeStorageObjectFinalized {
		t.Fatalf("type=%q", event.Type)
	}

	var data cloudevents.GCSEvent
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Bucket != "b" || data.Name != "o.txt" {
		t.Fatalf("data=%+v", data)
	}
}

func TestMatchesFilter(t *testing.T) {
	filters := map[string]string{
		"event_type": cloudevents.TypeStorageObjectFinalized,
	}
	if !cloudevents.MatchesFilter(filters, "OBJECT_FINALIZE") {
		t.Fatal("expected OBJECT_FINALIZE to match finalized filter")
	}
}
