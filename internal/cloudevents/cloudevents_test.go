package cloudevents_test

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
)

func TestCanonicalStorageType(t *testing.T) {
	cases := map[string]string{
		"OBJECT_FINALIZE":        cloudevents.TypeObjectFinalized,
		"OBJECT_DELETE":          cloudevents.TypeObjectDeleted,
		"OBJECT_ARCHIVE":         cloudevents.TypeObjectArchived,
		"OBJECT_METADATA_UPDATE": cloudevents.TypeObjectMetadataUpdated,
		// already-canonical passes through
		cloudevents.TypeObjectFinalized: cloudevents.TypeObjectFinalized,
		// unknown / empty
		"NOPE": "",
		"":     "",
	}
	for in, want := range cases {
		if got := cloudevents.CanonicalStorageType(in); got != want {
			t.Errorf("CanonicalStorageType(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParsePubSubPushFromAttributes(t *testing.T) {
	body := []byte(`{
		"message": {
			"messageId": "123",
			"attributes": {
				"eventType": "OBJECT_DELETE",
				"bucketId": "demo-bucket",
				"objectId": "uploads/file.csv"
			}
		}
	}`)

	got := cloudevents.ParsePubSubPush(body)
	if !got.OK {
		t.Fatal("expected ok")
	}
	if got.Bucket != "demo-bucket" || got.Name != "uploads/file.csv" || got.EventType != "OBJECT_DELETE" {
		t.Fatalf("unexpected parse: %+v", got)
	}
	if got.MessageID != "123" {
		t.Fatalf("messageId=%q", got.MessageID)
	}
}

func TestParsePubSubPushFromData(t *testing.T) {
	obj := `{"bucket":"b","name":"o.txt","size":"42","generation":"17","contentType":"text/plain"}`
	body, _ := json.Marshal(map[string]any{
		"message": map[string]any{
			"data":       base64.StdEncoding.EncodeToString([]byte(obj)),
			"attributes": map[string]string{"eventType": "OBJECT_FINALIZE"},
		},
	})

	got := cloudevents.ParsePubSubPush(body)
	if !got.OK {
		t.Fatal("expected ok")
	}
	if got.Bucket != "b" || got.Name != "o.txt" {
		t.Fatalf("bucket/name: %+v", got)
	}
	if got.Data.Size != "42" || got.Data.Generation != "17" {
		t.Fatalf("data int64 fields not preserved as strings: %+v", got.Data)
	}
}

func TestNewStorageObjectEvent(t *testing.T) {
	obj := cloudevents.StorageObjectData{Size: "42", Generation: "17"}
	event := cloudevents.NewStorageObjectEvent(cloudevents.TypeObjectFinalized, "b", "o.txt", obj, "", "")

	if event.Type != cloudevents.TypeObjectFinalized {
		t.Fatalf("type=%q", event.Type)
	}
	if event.Subject != "objects/o.txt" {
		t.Fatalf("subject=%q", event.Subject)
	}
	if event.Source != cloudevents.SourceStoragePrefix+"b" {
		t.Fatalf("source=%q", event.Source)
	}

	var data cloudevents.StorageObjectData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.Bucket != "b" || data.Name != "o.txt" {
		t.Fatalf("data=%+v", data)
	}
	// int64 fields must serialize as JSON strings
	if !jsonHasStringField(t, event.Data, "size", "42") {
		t.Fatal("size must serialize as a JSON string")
	}
}

func TestNewMessagePublishedEvent(t *testing.T) {
	event := cloudevents.NewMessagePublishedEvent("proj", "orders", []byte("hello"), map[string]string{"k": "v"}, "m1", "")
	if event.Type != cloudevents.TypeMessagePublished {
		t.Fatalf("type=%q", event.Type)
	}
	if event.Source != cloudevents.SourcePubSubPrefix+"proj/topics/orders" {
		t.Fatalf("source=%q", event.Source)
	}
	var data cloudevents.MessagePublishedData
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatal(err)
	}
	decoded, _ := base64.StdEncoding.DecodeString(data.Message.Data)
	if string(decoded) != "hello" {
		t.Fatalf("payload=%q", decoded)
	}
	if data.Message.Attributes["k"] != "v" {
		t.Fatalf("attributes=%+v", data.Message.Attributes)
	}
}

func jsonHasStringField(t *testing.T, raw json.RawMessage, field, want string) bool {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return string(m[field]) == `"`+want+`"`
}
