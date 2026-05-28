package cloudevents

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Canonical CloudEvent type strings for Cloud Storage object events, matching
// what real GCP Eventarc delivers to a Cloud Function.
const (
	TypeObjectFinalized       = "google.cloud.storage.object.v1.finalized"
	TypeObjectDeleted         = "google.cloud.storage.object.v1.deleted"
	TypeObjectArchived        = "google.cloud.storage.object.v1.archived"
	TypeObjectMetadataUpdated = "google.cloud.storage.object.v1.metadataUpdated"

	TypeMessagePublished = "google.cloud.pubsub.topic.v1.messagePublished"

	SourceStoragePrefix = "//storage.googleapis.com/projects/_/buckets/"
	SourcePubSubPrefix  = "//pubsub.googleapis.com/projects/"
)

// Deprecated: kept for back-compat with older call sites.
const TypeStorageObjectFinalized = TypeObjectFinalized

// storageEventTypes maps the short notification names (as emitted by
// fake-gcs-server / used in gsutil notification configs) to canonical
// CloudEvent type strings.
var storageEventTypes = map[string]string{
	"OBJECT_FINALIZE":        TypeObjectFinalized,
	"OBJECT_DELETE":          TypeObjectDeleted,
	"OBJECT_ARCHIVE":         TypeObjectArchived,
	"OBJECT_METADATA_UPDATE": TypeObjectMetadataUpdated,
}

// canonicalSet is the set of valid canonical storage CloudEvent types.
var canonicalSet = map[string]bool{
	TypeObjectFinalized:       true,
	TypeObjectDeleted:         true,
	TypeObjectArchived:        true,
	TypeObjectMetadataUpdated: true,
}

// CanonicalStorageType normalizes a short notification name ("OBJECT_FINALIZE")
// or an already-canonical type to its canonical CloudEvent type. Returns ""
// for an empty or unknown input.
func CanonicalStorageType(eventType string) string {
	if eventType == "" {
		return ""
	}
	if canonicalSet[eventType] {
		return eventType
	}
	return storageEventTypes[eventType]
}

// IsCanonicalStorageType reports whether t is a known canonical storage type.
func IsCanonicalStorageType(t string) bool {
	return canonicalSet[t]
}

// IsShortStorageEventType reports whether t is a known short notification name.
func IsShortStorageEventType(t string) bool {
	_, ok := storageEventTypes[t]
	return ok
}

// Envelope is a CloudEvents 1.0 structured-mode JSON envelope.
type Envelope struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Subject         string          `json:"subject,omitempty"`
	Time            string          `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	Data            json.RawMessage `json:"data"`
}

// StorageObjectData mirrors google.events.cloud.storage.v1.StorageObjectData.
// Per the JSON API / proto int64 convention, size/generation/metageneration
// are serialized as strings.
type StorageObjectData struct {
	Kind                    string            `json:"kind,omitempty"`
	ID                      string            `json:"id,omitempty"`
	SelfLink                string            `json:"selfLink,omitempty"`
	Name                    string            `json:"name,omitempty"`
	Bucket                  string            `json:"bucket,omitempty"`
	Generation              string            `json:"generation,omitempty"`
	Metageneration          string            `json:"metageneration,omitempty"`
	ContentType             string            `json:"contentType,omitempty"`
	TimeCreated             string            `json:"timeCreated,omitempty"`
	Updated                 string            `json:"updated,omitempty"`
	StorageClass            string            `json:"storageClass,omitempty"`
	TimeStorageClassUpdated string            `json:"timeStorageClassUpdated,omitempty"`
	Size                    string            `json:"size,omitempty"`
	MD5Hash                 string            `json:"md5Hash,omitempty"`
	MediaLink               string            `json:"mediaLink,omitempty"`
	ContentEncoding         string            `json:"contentEncoding,omitempty"`
	ContentDisposition      string            `json:"contentDisposition,omitempty"`
	CacheControl            string            `json:"cacheControl,omitempty"`
	ContentLanguage         string            `json:"contentLanguage,omitempty"`
	Metadata                map[string]string `json:"metadata,omitempty"`
	CRC32C                  string            `json:"crc32c,omitempty"`
	ComponentCount          int               `json:"componentCount,omitempty"`
	Etag                    string            `json:"etag,omitempty"`
}

// NewStorageObjectEvent builds a CloudEvent for a Cloud Storage object event.
// canonicalType must be one of the TypeObject* constants. The data payload is
// filled out from obj, with bucket/name backfilled from the arguments.
func NewStorageObjectEvent(canonicalType, bucket, name string, obj StorageObjectData, id, eventTime string) Envelope {
	if obj.Bucket == "" {
		obj.Bucket = bucket
	}
	if obj.Name == "" {
		obj.Name = name
	}
	if obj.Kind == "" {
		obj.Kind = "storage#object"
	}
	data, _ := json.Marshal(obj)

	if id == "" {
		id = fmt.Sprintf("gcp-relay-%d", time.Now().UnixNano())
	}
	if eventTime == "" {
		eventTime = time.Now().UTC().Format(time.RFC3339Nano)
	}

	return Envelope{
		SpecVersion:     "1.0",
		ID:              id,
		Source:          SourceStoragePrefix + bucket,
		Type:            canonicalType,
		Subject:         "objects/" + name,
		Time:            eventTime,
		DataContentType: "application/json",
		Data:            data,
	}
}

// PubSubMessage is the inner message of a messagePublished CloudEvent payload.
type PubSubMessage struct {
	Data        string            `json:"data,omitempty"` // base64
	Attributes  map[string]string `json:"attributes,omitempty"`
	MessageID   string            `json:"messageId,omitempty"`
	PublishTime string            `json:"publishTime,omitempty"`
	OrderingKey string            `json:"orderingKey,omitempty"`
}

// MessagePublishedData mirrors the data payload of a
// google.cloud.pubsub.topic.v1.messagePublished CloudEvent.
type MessagePublishedData struct {
	Message      PubSubMessage `json:"message"`
	Subscription string        `json:"subscription,omitempty"`
}

// NewMessagePublishedEvent builds a CloudEvent for a Pub/Sub message delivered
// to a topic-triggered function. rawData is the decoded message payload; it is
// base64-encoded into the envelope as GCP delivers it.
func NewMessagePublishedEvent(project, topic string, rawData []byte, attrs map[string]string, messageID, publishTime string) Envelope {
	if messageID == "" {
		messageID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if publishTime == "" {
		publishTime = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload := MessagePublishedData{
		Message: PubSubMessage{
			Data:        base64.StdEncoding.EncodeToString(rawData),
			Attributes:  attrs,
			MessageID:   messageID,
			PublishTime: publishTime,
		},
	}
	data, _ := json.Marshal(payload)

	return Envelope{
		SpecVersion:     "1.0",
		ID:              messageID,
		Source:          fmt.Sprintf("%s%s/topics/%s", SourcePubSubPrefix, project, topic),
		Type:            TypeMessagePublished,
		Time:            publishTime,
		DataContentType: "application/json",
		Data:            data,
	}
}

// ParsedStorageEvent is the result of decoding a Pub/Sub push that carries a
// GCS object notification.
type ParsedStorageEvent struct {
	Bucket    string
	Name      string
	EventType string // short form, e.g. OBJECT_FINALIZE
	MessageID string
	Time      string
	Data      StorageObjectData
	OK        bool
}

// ParsePubSubPush decodes a Pub/Sub push envelope carrying a GCS object
// notification (as emitted by fake-gcs-server). It reads metadata from message
// attributes and the full object resource from the base64 message data.
func ParsePubSubPush(body []byte) ParsedStorageEvent {
	var push struct {
		Message struct {
			Data        string            `json:"data"`
			Attributes  map[string]string `json:"attributes"`
			MessageID   string            `json:"messageId"`
			PublishTime string            `json:"publishTime"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &push); err != nil {
		return ParsedStorageEvent{}
	}

	res := ParsedStorageEvent{
		EventType: push.Message.Attributes["eventType"],
		Name:      push.Message.Attributes["objectId"],
		Bucket:    push.Message.Attributes["bucketId"],
		MessageID: push.Message.MessageID,
		Time:      push.Message.PublishTime,
	}

	// The message data is the full storage#object resource (base64 may or may
	// not be applied depending on the publisher; handle both).
	if push.Message.Data != "" {
		raw := decodeMaybeBase64(push.Message.Data)
		var obj StorageObjectData
		if err := json.Unmarshal(raw, &obj); err == nil {
			res.Data = obj
			if res.Bucket == "" {
				res.Bucket = obj.Bucket
			}
			if res.Name == "" {
				res.Name = obj.Name
			}
		}
	}

	if res.EventType == "" {
		res.EventType = "OBJECT_FINALIZE"
	}
	res.OK = res.Bucket != "" && res.Name != ""
	return res
}

// ParsePubSubMessage decodes a Pub/Sub push envelope into its raw message data
// and attributes, without assuming a GCS payload. Used for topic-triggered
// functions.
func ParsePubSubMessage(body []byte) (data []byte, attrs map[string]string, messageID, publishTime string, ok bool) {
	var push struct {
		Message struct {
			Data        string            `json:"data"`
			Attributes  map[string]string `json:"attributes"`
			MessageID   string            `json:"messageId"`
			PublishTime string            `json:"publishTime"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &push); err != nil {
		return nil, nil, "", "", false
	}
	if push.Message.Data != "" {
		if decoded, err := base64.StdEncoding.DecodeString(push.Message.Data); err == nil {
			data = decoded
		} else {
			data = []byte(push.Message.Data)
		}
	}
	return data, push.Message.Attributes, push.Message.MessageID, push.Message.PublishTime, true
}

func decodeMaybeBase64(s string) []byte {
	if decoded, err := base64.StdEncoding.DecodeString(s); err == nil && json.Valid(decoded) {
		return decoded
	}
	return []byte(s)
}
