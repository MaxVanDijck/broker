package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	APIServer APIServerConfig `yaml:"api_server,omitempty"`
	OIDC      OIDCConfig      `yaml:"oidc,omitempty"`
	State     StateConfig     `yaml:"state,omitempty"`
	Analytics AnalyticsConfig `yaml:"analytics,omitempty"`
}

type OIDCConfig struct {
	Issuer       string   `yaml:"issuer,omitempty"`
	ClientID     string   `yaml:"client_id,omitempty"`
	ClientSecret string   `yaml:"client_secret,omitempty"`
	Audience     string   `yaml:"audience,omitempty"`
	Scopes       []string `yaml:"scopes,omitempty"`
	RedirectURL  string   `yaml:"redirect_url,omitempty"`
}

type APIServerConfig struct {
	HTTPPort  int    `yaml:"http_port,omitempty"`
	PublicURL string `yaml:"public_url,omitempty"` // URL agents use to connect back (e.g. wss://broker.example.com)
}

type StateConfig struct {
	Backend string `yaml:"backend,omitempty"` // "sqlite" (default) or "postgres"
	DSN     string `yaml:"dsn,omitempty"`     // postgres connection string; for sqlite, path to db file
}

type AnalyticsConfig struct {
	Backend string `yaml:"backend,omitempty"` // "sqlite" (default), "chdb", or "clickhouse"
	DSN     string `yaml:"dsn,omitempty"`     // clickhouse connection string; for sqlite, path to db file
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
			Backend: "sqlite",
		},
	}
}

func Load() (*Config, error) {
	cfg := DefaultConfig()

	path := os.Getenv("BROKER_CONFIG_FILE")
	if path == "" {
		dataDir := os.Getenv("BROKER_DATA_DIR")
		if dataDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return cfg, nil
			}
			dataDir = filepath.Join(home, ".broker")
		}
		path = filepath.Join(dataDir, "config.yaml")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, nil
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
