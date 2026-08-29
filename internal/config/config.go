package config

import (
	"os"

	"go.yaml.in/yaml/v4"
)

type Config struct {
	State     StateConfig     `yaml:"state,omitempty"`
	Analytics AnalyticsConfig `yaml:"analytics,omitempty"`
}

type StateStore string
const (
	StateStoreSQLite   StateStore = "sqlite"
	StateStorePostgres StateStore = "postgres"
)
type StateConfig struct {
	Backend StateStore `yaml:"backend,omitempty"` // "sqlite" (default) or "postgres"
	DSN     string     `yaml:"dsn,omitempty"`     // postgres connection string; for sqlite, path to db file
}

type AnalyticsStore string
const (
	AnalyticsStoreSQLite     AnalyticsStore = "sqlite"
	AnalyticsStoreClickHouse AnalyticsStore = "clickhouse"
)
type AnalyticsConfig struct {
	Backend AnalyticsStore `yaml:"backend,omitempty"` // "sqlite" (default), "chdb", or "clickhouse"
	DSN     string `yaml:"dsn,omitempty"`     // clickhouse connection string; for sqlite, path to db file
}

func DefaultConfig() *Config {
	return &Config{
		State: StateConfig{
			Backend: StateStoreSQLite,
		},
		Analytics: AnalyticsConfig{
			Backend: AnalyticsStoreSQLite,
		},
	}
	// TODO(max): Add data path for persistence (sqlite / chdb)
}

func Load() (*Config, error) {
	path := os.Getenv("BROKER_CONFIG")
	if path == "" {
		return DefaultConfig(), nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Load(data, &config, yaml.WithKnownFields())

	if err != nil {
		return nil, err
	}

	return &config, nil
}
