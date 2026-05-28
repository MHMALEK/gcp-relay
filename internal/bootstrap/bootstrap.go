package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MHMALEK/gcp-relay/internal/config"
)

type Options struct {
	ProjectID    string
	PubSubHost   string
	GCSHost      string
	RelayURL     string
	PushRelayURL string
	Topic        string
	Bucket       string
	ProjectDir   string // base dir for resolving seed file paths
}

func DefaultOptions() Options {
	relayURL := envOr("GCP_RELAY_URL", "http://localhost:8099")
	pushURL := envOr("GCP_RELAY_PUSH_URL", "http://host.docker.internal:8099")
	return Options{
		ProjectID:    envOr("GCP_RELAY_PROJECT", "local-project"),
		PubSubHost:   envOr("PUBSUB_EMULATOR_HOST", "localhost:8085"),
		GCSHost:      envOr("STORAGE_EMULATOR_HOST", "http://localhost:4443"),
		RelayURL:     relayURL,
		PushRelayURL: pushURL,
		Topic:        envOr("GCP_RELAY_GCS_TOPIC", "gcs-notifications"),
		Bucket:       envOr("GCP_RELAY_DEMO_BUCKET", "demo-bucket"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func Run(opts Options) error {
	client := &http.Client{Timeout: 15 * time.Second}
	pubsubBase := pubsubURL(opts.PubSubHost)
	gcsBase := strings.TrimRight(opts.GCSHost, "/")

	if err := putJSON(client, fmt.Sprintf("%s/v1/projects/%s/topics/%s", pubsubBase, opts.ProjectID, opts.Topic), `{}`); err != nil {
		return fmt.Errorf("create topic: %w", err)
	}

	subURL := fmt.Sprintf("%s/v1/projects/%s/subscriptions/gcs-relay-push", pubsubBase, opts.ProjectID)
	deleteReq, _ := http.NewRequest(http.MethodDelete, subURL, nil)
	_ = do(client, deleteReq)

	subBody, _ := json.Marshal(map[string]any{
		"topic": fmt.Sprintf("projects/%s/topics/%s", opts.ProjectID, opts.Topic),
		"pushConfig": map[string]string{
			"pushEndpoint": fmt.Sprintf("%s/hooks/pubsub/%s", strings.TrimRight(opts.PushRelayURL, "/"), opts.Topic),
		},
	})
	if err := putJSON(client, subURL, string(subBody)); err != nil {
		return fmt.Errorf("create subscription: %w", err)
	}

	bucketBody, _ := json.Marshal(map[string]string{"name": opts.Bucket})
	if err := postJSON(client, gcsBase+"/storage/v1/b", string(bucketBody)); err != nil {
		return fmt.Errorf("create bucket: %w", err)
	}

	// Optional on newer fake-gcs builds; primary path is -event.pubsub-* flags in compose.
	notifyBody, _ := json.Marshal(map[string]any{
		"topic":          fmt.Sprintf("projects/%s/topics/%s", opts.ProjectID, opts.Topic),
		"payload_format": "JSON_API_V1",
		"event_types":    []string{"OBJECT_FINALIZE"},
	})
	_ = postJSON(client, fmt.Sprintf("%s/storage/v1/b/%s/notificationConfigs", gcsBase, opts.Bucket), string(notifyBody))

	return nil
}

// RunFromConfig provisions all resources declared in cfg against the
// emulators: the firehose topic + relay push subscription, declared Pub/Sub
// topics/subscriptions, notification topics, per-function topic subscriptions,
// and buckets (with versioning and seed objects). opts.Topic is the firehose
// topic name; opts.PushRelayURL is the relay's in-network base URL.
func RunFromConfig(cfg *config.Config, opts Options) error {
	client := &http.Client{Timeout: 15 * time.Second}
	pubsubBase := pubsubURL(opts.PubSubHost)
	gcsBase := strings.TrimRight(opts.GCSHost, "/")
	pushBase := strings.TrimRight(opts.PushRelayURL, "/")
	project := cfg.ProjectID
	if project == "" {
		project = "local-project"
	}

	// Create every topic referenced anywhere (firehose, declared, notification,
	// function triggers), deduplicated.
	topics := map[string]bool{}
	if opts.Topic != "" {
		topics[opts.Topic] = true
	}
	for _, t := range cfg.PubSub.Topics {
		topics[t.Name] = true
	}
	for _, n := range cfg.Notifications {
		topics[n.Topic] = true
	}
	for _, f := range cfg.Functions {
		if f.Trigger.Topic != "" {
			topics[f.Trigger.Topic] = true
		}
	}
	for name := range topics {
		if name == "" {
			continue
		}
		if err := putJSON(client, topicURL(pubsubBase, project, name), `{}`); err != nil {
			return fmt.Errorf("create topic %s: %w", name, err)
		}
	}

	// Firehose: deliver all GCS object events to the relay.
	if opts.Topic != "" {
		if err := createPushSub(client, pubsubBase, project, "gcs-relay-firehose", opts.Topic, pushBase+"/hooks/pubsub/"+opts.Topic); err != nil {
			return err
		}
	}

	// Topic-triggered functions: push the topic to the relay, which wraps it as
	// a messagePublished CloudEvent for the function.
	fnTopics := map[string]bool{}
	for _, f := range cfg.Functions {
		if f.Trigger.Topic != "" {
			fnTopics[f.Trigger.Topic] = true
		}
	}
	for topic := range fnTopics {
		if err := createPushSub(client, pubsubBase, project, "relay-"+topic, topic, pushBase+"/hooks/pubsub/"+topic); err != nil {
			return err
		}
	}

	// User-declared subscriptions (push to their own endpoint, or pull).
	for _, s := range cfg.PubSub.Subscriptions {
		if s.PushEndpoint != "" {
			if err := createPushSub(client, pubsubBase, project, s.Name, s.Topic, s.PushEndpoint); err != nil {
				return err
			}
		} else if err := createPullSub(client, pubsubBase, project, s.Name, s.Topic); err != nil {
			return err
		}
	}

	// Buckets + seed objects.
	for _, b := range cfg.Buckets {
		if err := ensureBucket(client, gcsBase, b); err != nil {
			return err
		}
		for _, sd := range b.Seed {
			if err := seedObject(gcsBase, b.Name, sd, opts.ProjectDir); err != nil {
				return fmt.Errorf("seed %s/%s: %w", b.Name, sd.Object, err)
			}
		}
	}
	return nil
}

func topicURL(base, project, topic string) string {
	return fmt.Sprintf("%s/v1/projects/%s/topics/%s", base, project, topic)
}

func createPushSub(client *http.Client, base, project, name, topic, endpoint string) error {
	subURL := fmt.Sprintf("%s/v1/projects/%s/subscriptions/%s", base, project, name)
	delReq, _ := http.NewRequest(http.MethodDelete, subURL, nil)
	_ = do(client, delReq)
	body, _ := json.Marshal(map[string]any{
		"topic":      fmt.Sprintf("projects/%s/topics/%s", project, topic),
		"pushConfig": map[string]string{"pushEndpoint": endpoint},
	})
	if err := putJSON(client, subURL, string(body)); err != nil {
		return fmt.Errorf("create push subscription %s: %w", name, err)
	}
	return nil
}

func createPullSub(client *http.Client, base, project, name, topic string) error {
	subURL := fmt.Sprintf("%s/v1/projects/%s/subscriptions/%s", base, project, name)
	body, _ := json.Marshal(map[string]any{
		"topic": fmt.Sprintf("projects/%s/topics/%s", project, topic),
	})
	if err := putJSON(client, subURL, string(body)); err != nil {
		return fmt.Errorf("create subscription %s: %w", name, err)
	}
	return nil
}

func ensureBucket(client *http.Client, gcsBase string, b config.Bucket) error {
	payload := map[string]any{"name": b.Name}
	if b.Versioning {
		payload["versioning"] = map[string]bool{"enabled": true}
	}
	body, _ := json.Marshal(payload)
	if err := postJSON(client, gcsBase+"/storage/v1/b", string(body)); err != nil {
		return fmt.Errorf("create bucket %s: %w", b.Name, err)
	}
	return nil
}

func seedObject(gcsBase, bucket string, sd config.SeedObject, projectDir string) error {
	path := sd.From
	if !filepath.IsAbs(path) && projectDir != "" {
		path = filepath.Join(projectDir, path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?uploadType=media&name=%s", gcsBase, bucket, sd.Object)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload returned %s", resp.Status)
	}
	return nil
}

func UploadDemo(opts Options, objectName, content string) error {
	if objectName == "" {
		objectName = "uploads/hello.txt"
	}
	if content == "" {
		content = "hello from gcp-relay demo"
	}
	gcsBase := strings.TrimRight(opts.GCSHost, "/")
	url := fmt.Sprintf("%s/upload/storage/v1/b/%s/o?uploadType=media&name=%s", gcsBase, opts.Bucket, objectName)
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(content))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("upload returned %s", resp.Status)
	}
	return nil
}

func WaitForRelay(relayURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := strings.TrimRight(relayURL, "/") + "/health"
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("relay not healthy at %s within %s", url, timeout)
}

func pubsubURL(host string) string {
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	return "http://" + host
}

func putJSON(client *http.Client, url, body string) error {
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return do(client, req)
}

func postJSON(client *http.Client, url, body string) error {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return do(client, req)
}

func do(client *http.Client, req *http.Request) error {
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 300 && resp.StatusCode != 409 {
		return fmt.Errorf("%s returned %s", req.URL, resp.Status)
	}
	return nil
}
