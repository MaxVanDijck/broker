package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	APIServer APIServerConfig `yaml:"api_server,omitempty"`
	State     StateConfig     `yaml:"state,omitempty"`
	Analytics AnalyticsConfig `yaml:"analytics,omitempty"`
}

type APIServerConfig struct {
	HTTPPort  int    `yaml:"http_port,omitempty"`
	PublicURL string `yaml:"public_url,omitempty"` // URL agents use to connect back (e.g. wss://broker.example.com)
}

type StateConfig struct {
	Backend string `yaml:"backend,omitempty"` // "sqlite" (default) or "postgres"
	DSN     string `yaml:"dsn,omitempty"`     // postgres connection string
}

type AnalyticsConfig struct {
	Backend string `yaml:"backend,omitempty"` // "chdb" (default) or "clickhouse"
	DSN     string `yaml:"dsn,omitempty"`     // clickhouse connection string
}

func DefaultConfig() *Config {
	return &Config{
		APIServer: APIServerConfig{
			HTTPPort: 8080,
		},
		State: StateConfig{
			Backend: "sqlite",
		},
		Analytics: AnalyticsConfig{
			Backend: "noop",
		},
	}
}

func Load() (*Config, error) {
	cfg := DefaultConfig()

	home, err := os.UserHomeDir()
	if err != nil {
		return cfg, nil
	}

	path := filepath.Join(home, ".broker", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, nil
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
