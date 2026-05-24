package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the current triggers.yaml schema version.
// Configs without an explicit `version:` field are treated as this version
// for backward compatibility. Unknown versions are rejected.
const SchemaVersion = "v1"

type Config struct {
	Version   string    `yaml:"version"`
	ProjectID string    `yaml:"project_id"`
	Triggers  []Trigger `yaml:"triggers"`
}

type Trigger struct {
	Name    string            `yaml:"name"`
	Source  string            `yaml:"source"`
	Topic   string            `yaml:"topic"`
	Filters map[string]string `yaml:"filters"`
	Targets []Target          `yaml:"targets"`
}

type Target struct {
	Type   string `yaml:"type"`
	URL    string `yaml:"url"`
	Method string `yaml:"method"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if cfg.Version == "" {
		cfg.Version = SchemaVersion
	}
	if cfg.Version != SchemaVersion {
		return nil, fmt.Errorf("unsupported config version %q (this binary supports %q)", cfg.Version, SchemaVersion)
	}

	if cfg.ProjectID == "" {
		cfg.ProjectID = "local-project"
	}

	for i := range cfg.Triggers {
		for j := range cfg.Triggers[i].Targets {
			t := &cfg.Triggers[i].Targets[j]
			if t.Method == "" {
				t.Method = "POST"
			}
			if t.Type == "" {
				t.Type = "cloudevent"
			}
			t.URL = os.ExpandEnv(t.URL)
		}
	}

	return &cfg, nil
}

func (c *Config) TriggersForTopic(topic string) []Trigger {
	var out []Trigger
	for _, t := range c.Triggers {
		if t.Source == "pubsub" && t.Topic == topic {
			out = append(out, t)
		}
	}
	return out
}

func (c *Config) TriggersForGCS() []Trigger {
	var out []Trigger
	for _, t := range c.Triggers {
		if t.Source == "gcs" || t.Source == "pubsub" {
			out = append(out, t)
		}
	}
	return out
}

func (t Trigger) MatchesObject(objectName string, eventType string) bool {
	if !matchesEventType(t.Filters, eventType) {
		return false
	}
	if prefix, ok := t.Filters["object_prefix"]; ok && prefix != "" {
		return strings.HasPrefix(objectName, prefix)
	}
	return true
}

func matchesEventType(filters map[string]string, eventType string) bool {
	want, ok := filters["event_type"]
	if !ok || want == "" {
		return true
	}
	if want == "google.cloud.storage.object.v1.finalized" {
		return eventType == "OBJECT_FINALIZE" || eventType == want
	}
	return want == eventType
}
