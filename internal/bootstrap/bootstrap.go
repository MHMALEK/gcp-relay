package bootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Options struct {
	ProjectID    string
	PubSubHost   string
	GCSHost      string
	RelayURL     string
	PushRelayURL string
	Topic        string
	Bucket       string
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
