package cloudevents

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	TypeStorageObjectFinalized = "google.cloud.storage.object.v1.finalized"
	SourceStoragePrefix        = "//storage.googleapis.com/projects/_/buckets/"
)

type GCSEvent struct {
	Bucket string `json:"bucket"`
	Name   string `json:"name"`
}

type Envelope struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Time            string          `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	Data            json.RawMessage `json:"data"`
}

func NewStorageObjectFinalized(bucket, objectName string) Envelope {
	data, _ := json.Marshal(GCSEvent{
		Bucket: bucket,
		Name:   objectName,
	})

	return Envelope{
		SpecVersion:     "1.0",
		ID:              fmt.Sprintf("gcp-relay-%d", time.Now().UnixNano()),
		Source:          SourceStoragePrefix + bucket,
		Type:            TypeStorageObjectFinalized,
		Time:            time.Now().UTC().Format(time.RFC3339Nano),
		DataContentType: "application/json",
		Data:            data,
	}
}

// ParsePubSubPush extracts a GCS notification from a Pub/Sub push envelope.
func ParsePubSubPush(body []byte) (bucket, objectName, eventType string, ok bool) {
	var push struct {
		Message struct {
			Data       string            `json:"data"`
			Attributes map[string]string `json:"attributes"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &push); err != nil {
		return "", "", "", false
	}

	if push.Message.Attributes["eventType"] != "" {
		eventType = push.Message.Attributes["eventType"]
	}
	if push.Message.Attributes["objectId"] != "" {
		objectName = push.Message.Attributes["objectId"]
	}
	if push.Message.Attributes["bucketId"] != "" {
		bucket = push.Message.Attributes["bucketId"]
	}
	if bucket != "" && objectName != "" {
		if eventType == "" {
			eventType = "OBJECT_FINALIZE"
		}
		return bucket, objectName, eventType, true
	}

	if push.Message.Data == "" {
		return "", "", "", false
	}

	var gcs struct {
		Bucket     string `json:"bucket"`
		Name       string `json:"name"`
		EventType  string `json:"eventType"`
		ObjectName string `json:"object_name"`
	}
	raw := []byte(push.Message.Data)
	if err := json.Unmarshal(raw, &gcs); err != nil {
		return "", "", "", false
	}

	bucket = gcs.Bucket
	objectName = gcs.Name
	if objectName == "" {
		objectName = gcs.ObjectName
	}
	eventType = gcs.EventType
	if eventType == "" {
		eventType = "OBJECT_FINALIZE"
	}

	return bucket, objectName, eventType, bucket != "" && objectName != ""
}

func MatchesFilter(filters map[string]string, eventType string) bool {
	if len(filters) == 0 {
		return true
	}
	for key, want := range filters {
		switch key {
		case "event_type":
			if want == TypeStorageObjectFinalized && (eventType == "OBJECT_FINALIZE" || eventType == TypeStorageObjectFinalized) {
				continue
			}
			if want != eventType {
				return false
			}
		default:
			return false
		}
	}
	return true
}
