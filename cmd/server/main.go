package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"broker/internal/config"
	"broker/internal/state"
	"broker/internal/analytics"
)

func main() {
	ctx := context.Background()
	defer ctx.Done()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	logger.InfoContext(ctx, "starting server")

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger.InfoContext(ctx, "initialising state store")
	stateStore, err := initStateStore(cfg, logger)
	if err != nil {
		logger.Error("failed to initialise state store", "error", err)
		os.Exit(1)
	}
    defer func() {
        err = stateStore.Close()
		logger.ErrorContext(ctx, "failed to close state store", "error", err)
    }()

	logger.InfoContext(ctx, "initialising analytics store")
	analyticsStore, err := initAnalyticsStore(cfg, logger)
	if err != nil {
		logger.Error("failed to initialise analytics store", "error", err)
		os.Exit(1)
	}
    defer func() {
        err = analyticsStore.Close()
		logger.ErrorContext(ctx, "failed to close analytics store", "error", err)
    }()

	// TODO(max): initialise cloud providers

	// TODO(max): initialise gin server
}

// TODO(max): implement StateStore contract
func initStateStore(cfg *config.Config, logger *slog.Logger) (state.Store, error) {
	// TODO(max): fix DSN in config
	switch cfg.State.Backend {
		case config.StateStorePostgres:
			logger.Info("using postgresql state store")
		    // TODO(max): pass a DSN (data source name)
			return state.NewPostgresStore(), nil
		case config.StateStoreSQLite:
			logger.Info("using sqlite state store")
		    // TODO(max): pass a DSN (data source name)
			return state.NewSqliteStore(), nil
		default:
			logger.Warn("falling back to sqlite state store")
		    // TODO(max): pass a DSN (data source name)
			return state.NewSqliteStore(), nil
	}
}

// TODO(max): implement Analytics store contract
func initAnalyticsStore(cfg *config.Config, logger *slog.Logger) (analytics.Store, error) {
	// TODO(max): fix DSN in config
	switch cfg.Analytics.Backend {
		case config.AnalyticsStoreClickHouse:
			logger.Info("using clickhouse analytics store")
		    // TODO(max): pass a DSN (data source name)
			return analytics.NewClickhouseStore(), nil
		case config.AnalyticsStoreSQLite:
			logger.Info("using sqlite analytics store")
		    // TODO(max): pass a DSN (data source name)
			return analytics.NewSqliteStore(), nil
		default:
			logger.Warn("falling back to sqlite analytics store")
		    // TODO(max): pass a DSN (data source name)
			return analytics.NewSqliteStore(), nil
	}
}
