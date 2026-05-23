package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
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

	if cfg.ProjectID == "" {
		cfg.ProjectID = "local-project"
	}

	for i := range cfg.Triggers {
		for j := range cfg.Triggers[i].Targets {
			if cfg.Triggers[i].Targets[j].Method == "" {
				cfg.Triggers[i].Targets[j].Method = "POST"
			}
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
