package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MHMALEK/gcp-relay/internal/cloudevents"
	"gopkg.in/yaml.v3"
)

// SchemaVersion is the current config schema version. Configs without an
// explicit `version:` are auto-detected (legacy `triggers:` => v1, otherwise
// v2). Unknown versions are rejected.
const SchemaVersion = "v2"

// Config is the top-level gcp-relay configuration. It mirrors real GCP
// resources: buckets, Pub/Sub topics/subscriptions, GCS bucket notifications,
// and Cloud Functions.
type Config struct {
	Version       string         `yaml:"version"`
	ProjectID     string         `yaml:"project_id"`
	Buckets       []Bucket       `yaml:"buckets"`
	PubSub        PubSub         `yaml:"pubsub"`
	Notifications []Notification `yaml:"notifications"`
	Functions     []Function     `yaml:"functions"`

	// Legacy v1 schema. Parsed for back-compat then normalized into Functions.
	Triggers []Trigger `yaml:"triggers"`

	// baseDir is the directory the config was loaded from, for resolving
	// relative function source paths. Set by Load; not serialized.
	baseDir string `yaml:"-"`
}

// Bucket is a GCS bucket to create on bootstrap.
type Bucket struct {
	Name       string       `yaml:"name"`
	Location   string       `yaml:"location"`
	Versioning bool         `yaml:"versioning"`
	Seed       []SeedObject `yaml:"seed"`
}

// SeedObject preloads a local file into a bucket at startup.
type SeedObject struct {
	Object string `yaml:"object"`
	From   string `yaml:"from"`
}

// PubSub declares Pub/Sub topics and subscriptions.
type PubSub struct {
	Topics        []Topic        `yaml:"topics"`
	Subscriptions []Subscription `yaml:"subscriptions"`
}

// Topic is a Pub/Sub topic.
type Topic struct {
	Name string `yaml:"name"`
}

// Subscription is a Pub/Sub subscription. A non-empty PushEndpoint makes it a
// push subscription delivering to that URL.
type Subscription struct {
	Name         string `yaml:"name"`
	Topic        string `yaml:"topic"`
	PushEndpoint string `yaml:"push_endpoint"`
	Filter       string `yaml:"filter"`
}

// Notification mirrors a GCS bucket notification config
// (gsutil notification create): bucket object events => a Pub/Sub topic.
type Notification struct {
	Bucket           string            `yaml:"bucket"`
	Topic            string            `yaml:"topic"`
	EventTypes       []string          `yaml:"event_types"`
	ObjectNamePrefix string            `yaml:"object_name_prefix"`
	PayloadFormat    string            `yaml:"payload_format"`
	CustomAttributes map[string]string `yaml:"custom_attributes"`
}

// Function mirrors `gcloud functions deploy`: a function run from source with
// an event trigger.
type Function struct {
	Name       string            `yaml:"name"`
	Runtime    string            `yaml:"runtime"`
	Source     string            `yaml:"source"`
	EntryPoint string            `yaml:"entry_point"`
	Port       int               `yaml:"port"` // optional host port to publish
	URL        string            `yaml:"url"`  // explicit override for an already-running function
	Env        map[string]string `yaml:"env"`
	Trigger    FunctionTrigger   `yaml:"trigger"`
}

// FunctionTrigger is the function's event source. Exactly one of HTTP, Topic,
// or EventFilters must be set.
type FunctionTrigger struct {
	HTTP         bool          `yaml:"http"`
	Topic        string        `yaml:"topic"`
	EventFilters *EventFilters `yaml:"event_filters"`
}

// EventFilters mirrors `gcloud functions deploy --trigger-event-filters`.
type EventFilters struct {
	Type             string `yaml:"type"`
	Bucket           string `yaml:"bucket"`
	ObjectNamePrefix string `yaml:"object_name_prefix"`
}

// Trigger is the legacy v1 schema element.
type Trigger struct {
	Name    string            `yaml:"name"`
	Source  string            `yaml:"source"`
	Topic   string            `yaml:"topic"`
	Filters map[string]string `yaml:"filters"`
	Targets []Target          `yaml:"targets"`
}

// Target is a legacy v1 delivery target.
type Target struct {
	Type   string `yaml:"type"`
	URL    string `yaml:"url"`
	Method string `yaml:"method"`
}

// Load reads, parses, normalizes, and validates a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}
	cfg.baseDir = filepath.Dir(path)
	return cfg, nil
}

// Parse parses and validates raw config bytes. baseDir is left empty (source
// path checks are skipped); use Load to also resolve relative source paths.
func Parse(data []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.resolveVersion(); err != nil {
		return nil, err
	}
	if cfg.ProjectID == "" {
		cfg.ProjectID = "local-project"
	}

	cfg.normalizeLegacy()
	cfg.applyDefaults()

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// baseDir is the directory the config was loaded from, used to resolve
// relative function source paths. Not serialized.
func (c *Config) resolveVersion() error {
	if c.Version == "" {
		if len(c.Triggers) > 0 && len(c.Functions) == 0 && len(c.Notifications) == 0 && len(c.Buckets) == 0 {
			c.Version = "v1"
		} else {
			c.Version = SchemaVersion
		}
		return nil
	}
	switch c.Version {
	case "v1", "v2":
		return nil
	default:
		return fmt.Errorf("unsupported config version %q (this binary supports v1, v2)", c.Version)
	}
}

// normalizeLegacy converts v1 triggers into equivalent v2 functions so the
// router only ever deals with the v2 model.
func (c *Config) normalizeLegacy() {
	for _, t := range c.Triggers {
		canonical := cloudevents.CanonicalStorageType(t.Filters["event_type"])
		if canonical == "" {
			// v1 emitted only finalize by default; preserve that behavior.
			canonical = cloudevents.TypeObjectFinalized
		}
		prefix := t.Filters["object_prefix"]
		for i, tg := range t.Targets {
			name := t.Name
			if len(t.Targets) > 1 {
				name = fmt.Sprintf("%s-%d", t.Name, i)
			}
			c.Functions = append(c.Functions, Function{
				Name: name,
				URL:  os.ExpandEnv(tg.URL),
				Trigger: FunctionTrigger{
					EventFilters: &EventFilters{
						Type:             canonical,
						ObjectNamePrefix: prefix,
					},
				},
			})
		}
	}
	c.Triggers = nil
}

func (c *Config) applyDefaults() {
	for i := range c.Functions {
		f := &c.Functions[i]
		f.URL = os.ExpandEnv(f.URL)
		if f.Trigger.EventFilters != nil {
			// Allow already-canonical or short event types.
			if canonical := cloudevents.CanonicalStorageType(f.Trigger.EventFilters.Type); canonical != "" {
				f.Trigger.EventFilters.Type = canonical
			}
		}
	}
	for i := range c.Notifications {
		n := &c.Notifications[i]
		if n.PayloadFormat == "" {
			n.PayloadFormat = "JSON_API_V1"
		}
		if len(n.EventTypes) == 0 {
			n.EventTypes = []string{"OBJECT_FINALIZE"}
		}
	}
}

var validRuntimePrefixes = []string{"python", "nodejs", "go"}

// Validate checks structural invariants. It does not touch the filesystem.
func (c *Config) Validate() error {
	seenBucket := map[string]bool{}
	for _, b := range c.Buckets {
		if b.Name == "" {
			return fmt.Errorf("bucket: name is required")
		}
		if seenBucket[b.Name] {
			return fmt.Errorf("bucket %q: duplicate name", b.Name)
		}
		seenBucket[b.Name] = true
	}

	topicNames := map[string]bool{}
	for _, t := range c.PubSub.Topics {
		if t.Name == "" {
			return fmt.Errorf("pubsub topic: name is required")
		}
		topicNames[t.Name] = true
	}
	for _, s := range c.PubSub.Subscriptions {
		if s.Name == "" || s.Topic == "" {
			return fmt.Errorf("pubsub subscription: name and topic are required")
		}
	}

	for _, n := range c.Notifications {
		if n.Bucket == "" || n.Topic == "" {
			return fmt.Errorf("notification: bucket and topic are required")
		}
		for _, et := range n.EventTypes {
			if !cloudevents.IsShortStorageEventType(et) {
				return fmt.Errorf("notification on %q: unknown event_type %q (want OBJECT_FINALIZE, OBJECT_DELETE, OBJECT_ARCHIVE, OBJECT_METADATA_UPDATE)", n.Bucket, et)
			}
		}
		if n.PayloadFormat != "JSON_API_V1" && n.PayloadFormat != "NONE" {
			return fmt.Errorf("notification on %q: invalid payload_format %q", n.Bucket, n.PayloadFormat)
		}
	}

	seenFn := map[string]bool{}
	seenPort := map[int]bool{}
	for _, f := range c.Functions {
		if f.Name == "" {
			return fmt.Errorf("function: name is required")
		}
		if seenFn[f.Name] {
			return fmt.Errorf("function %q: duplicate name", f.Name)
		}
		seenFn[f.Name] = true

		if err := validateTrigger(f); err != nil {
			return err
		}
		if f.Source == "" && f.URL == "" {
			return fmt.Errorf("function %q: one of source or url is required", f.Name)
		}
		if f.Source != "" && !validRuntime(f.Runtime) {
			return fmt.Errorf("function %q: runtime %q is not supported (want python*/nodejs*/go*)", f.Name, f.Runtime)
		}
		if f.Port != 0 {
			if seenPort[f.Port] {
				return fmt.Errorf("function %q: port %d already in use by another function", f.Name, f.Port)
			}
			seenPort[f.Port] = true
		}
	}
	return nil
}

func validateTrigger(f Function) error {
	t := f.Trigger
	set := 0
	if t.HTTP {
		set++
	}
	if t.Topic != "" {
		set++
	}
	if t.EventFilters != nil {
		set++
	}
	if set == 0 {
		return fmt.Errorf("function %q: a trigger is required (http, topic, or event_filters)", f.Name)
	}
	if set > 1 {
		return fmt.Errorf("function %q: exactly one trigger kind allowed (http, topic, or event_filters)", f.Name)
	}
	if t.EventFilters != nil {
		if !cloudevents.IsCanonicalStorageType(t.EventFilters.Type) {
			return fmt.Errorf("function %q: event_filters.type %q is not a storage event type", f.Name, t.EventFilters.Type)
		}
	}
	return nil
}

func validRuntime(rt string) bool {
	for _, p := range validRuntimePrefixes {
		if strings.HasPrefix(rt, p) {
			return true
		}
	}
	return false
}

// ValidateSources checks that each function's source directory exists,
// resolving relative paths against the config's directory.
func (c *Config) ValidateSources() error {
	for _, f := range c.Functions {
		if f.Source == "" {
			continue
		}
		p := f.Source
		if !filepath.IsAbs(p) && c.baseDir != "" {
			p = filepath.Join(c.baseDir, p)
		}
		info, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("function %q: source %q: %w", f.Name, f.Source, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("function %q: source %q is not a directory", f.Name, f.Source)
		}
	}
	return nil
}

// TargetURL is the in-network URL the relay delivers events to. An explicit
// url wins; otherwise it is derived from the function name (containers listen
// on 8080 internally regardless of the published host port).
func (f Function) TargetURL() string {
	if f.URL != "" {
		return f.URL
	}
	return fmt.Sprintf("http://%s:8080", f.Name)
}

// FunctionsForStorageEvent returns functions whose event_filters match a GCS
// object event. An empty filter bucket matches any bucket.
func (c *Config) FunctionsForStorageEvent(bucket, object, canonicalType string) []Function {
	var out []Function
	for _, f := range c.Functions {
		ef := f.Trigger.EventFilters
		if ef == nil {
			continue
		}
		if ef.Type != canonicalType {
			continue
		}
		if ef.Bucket != "" && ef.Bucket != bucket {
			continue
		}
		if ef.ObjectNamePrefix != "" && !strings.HasPrefix(object, ef.ObjectNamePrefix) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// FunctionsForTopic returns functions triggered by a Pub/Sub topic.
func (c *Config) FunctionsForTopic(topic string) []Function {
	var out []Function
	for _, f := range c.Functions {
		if f.Trigger.Topic == topic {
			out = append(out, f)
		}
	}
	return out
}

// NotificationsForStorageEvent returns notifications matching a GCS object
// event. shortType is the short form (e.g. OBJECT_FINALIZE).
func (c *Config) NotificationsForStorageEvent(bucket, object, shortType string) []Notification {
	var out []Notification
	for _, n := range c.Notifications {
		if n.Bucket != bucket {
			continue
		}
		if !slices.Contains(n.EventTypes, shortType) {
			continue
		}
		if n.ObjectNamePrefix != "" && !strings.HasPrefix(object, n.ObjectNamePrefix) {
			continue
		}
		out = append(out, n)
	}
	return out
}
