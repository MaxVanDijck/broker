package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"broker/internal/auth"
	"broker/internal/config"
	"broker/internal/provider"
	awsprovider "broker/internal/provider/aws"
	"broker/internal/server"
	"broker/internal/store"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	dataDir := os.Getenv("BROKER_DATA_DIR")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			logger.Error("failed to get home dir", "error", err)
			os.Exit(1)
		}
		dataDir = filepath.Join(home, ".broker")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		logger.Error("failed to create data dir", "error", err)
		os.Exit(1)
	}

	stateStore, err := initStateStore(cfg, dataDir, logger)
	if err != nil {
		logger.Error("failed to init state store", "error", err)
		os.Exit(1)
	}
	defer stateStore.Close()

	analyticsStore, err := initAnalyticsStore(cfg, dataDir, logger)
	if err != nil {
		logger.Error("failed to init analytics store", "error", err)
		os.Exit(1)
	}
	defer analyticsStore.Close()

	registry := initProviders(cfg, logger)

	oidcCfg := &auth.VerifierConfig{
		Issuer:       cfg.OIDC.Issuer,
		ClientID:     cfg.OIDC.ClientID,
		ClientSecret: cfg.OIDC.ClientSecret,
		Audience:     cfg.OIDC.Audience,
		Scopes:       cfg.OIDC.Scopes,
		RedirectURL:  cfg.OIDC.RedirectURL,
	}
	srv := server.New(stateStore, analyticsStore, registry, logger, oidcCfg)

	port := cfg.APIServer.HTTPPort
	if port == 0 {
		port = 8080
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	logger.Info("broker server starting", "port", port)
	if err := srv.Serve(ctx, port); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func initProviders(cfg *config.Config, logger *slog.Logger) *provider.Registry {
	registry := provider.NewRegistry()

	publicURL := cfg.APIServer.PublicURL
	if publicURL != "" {
		awsProvider := awsprovider.New(logger.With("provider", "aws"), publicURL)
		registry.Register(awsProvider)
		logger.Info("registered aws provider", "server_url", publicURL)
	}

	return registry
}

func initStateStore(cfg *config.Config, dataDir string, logger *slog.Logger) (store.StateStore, error) {
	switch cfg.State.Backend {
	case "postgres":
		logger.Info("using postgresql state store")
		return store.NewPostgres(cfg.State.DSN)
	default:
		logger.Info("using sqlite state store", "path", filepath.Join(dataDir, "broker.db"))
		return store.NewSQLite(filepath.Join(dataDir, "broker.db"))
	}
}

func initAnalyticsStore(cfg *config.Config, dataDir string, logger *slog.Logger) (store.AnalyticsStore, error) {
	dsn := cfg.Analytics.DSN
	if dsn == "" {
		switch cfg.Analytics.Backend {
		case "chdb":
			dsn = filepath.Join(dataDir, "chdb")
		case "sqlite":
			dsn = filepath.Join(dataDir, "broker.db")
		}
	}

	logger.Info("initializing analytics store", "backend", cfg.Analytics.Backend)
	return store.NewAnalyticsStore(cfg.Analytics.Backend, dsn)
}
