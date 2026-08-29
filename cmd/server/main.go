package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"broker/internal/config"
)

func main() {
	ctx := context.Background()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.InfoContext(ctx, "starting server")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// TODO(max): initialise state store
	logger.InfoContext(ctx, "initialising state store")
	

	// TODO(max): initialise analystics store

	// TODO(max): initialise cloud providers

	// TODO(max): initialise gin server
}

// TODO(max): implement StateStore contract
func initStateStore(cfg *config.Config, logger *slog.Logger) (store.StateStore, error) {
	// TODO(max): fix DSN in config
	switch cfg.State.Backend {
		case config.StateStorePostgres:
			logger.Info("using postgresql state store")
		    // TODO(max): implement Postgres state Store
			return store.NewPostgres(cfg.State.DSN), nil
		case config.StateStoreSQLite:
			logger.Info("using sqlite state store")
		    // TODO(max): implement sqlite state store
			return store.NewSQLite(cfg.State.DSN), nil
	}
}

// TODO(max): implement Analytics store contract
func initAnalyticsStore(cfg *config.Config, dataDir string, logger *slog.Logger) (store.AnalyticsStore, error) {
	// TODO(max): fix DSN in config
	switch cfg.Analytics.Backend {
		case config.AnalyticsStoreClickHouse:
			logger.Info("using clickhouse analytics store")
		    // TODO(max): implement Postgres analytics Store
			return store.NewClickhouse(cfg.State.DSN), nil
		case config.AnalyticsStoreSQLite:
			logger.Info("using clickhouse analytics store")
		    // TODO(max): implement Postgres analytics Store
			return store.NewSQLite(cfg.State.DSN), nil
	}
}
