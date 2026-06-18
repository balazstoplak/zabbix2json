package main

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config holds runtime settings, resolved from YAML + env.
type Config struct {
	URL     string
	Token   string
	Listen  string
	Timeout time.Duration
	Version uint32
}

type rawConfig struct {
	URL     string `yaml:"url"`
	Token   string `yaml:"token"`
	Listen  string `yaml:"listen"`
	Timeout string `yaml:"timeout"`
	Version uint32 `yaml:"version"`
}

// Load reads YAML config (if path != ""), applies defaults, then env overrides.
func Load(path string) (*Config, error) {
	raw := rawConfig{}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(b, &raw); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	cfg := &Config{
		URL:     raw.URL,
		Token:   raw.Token,
		Listen:  raw.Listen,
		Version: raw.Version,
		Timeout: 10 * time.Second,
	}
	if raw.Timeout != "" {
		d, err := time.ParseDuration(raw.Timeout)
		if err != nil {
			return nil, fmt.Errorf("parse timeout %q: %w", raw.Timeout, err)
		}
		cfg.Timeout = d
	}
	if cfg.Listen == "" {
		cfg.Listen = ":8080"
	}
	if cfg.Version == 0 {
		cfg.Version = 20110619
	}

	if v := os.Getenv("ZABBIX_URL"); v != "" {
		cfg.URL = v
	}
	if v := os.Getenv("ZABBIX_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("ZABBIX_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("parse ZABBIX_TIMEOUT %q: %w", v, err)
		}
		cfg.Timeout = d
	}
	return cfg, nil
}
