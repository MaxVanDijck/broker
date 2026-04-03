package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"broker/internal/agent"
	"broker/internal/config"
	"broker/internal/provider"
	awsprovider "broker/internal/provider/aws"
	"broker/internal/server"
	"broker/internal/store"
)

func serveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "serve",
		Short:  "Start the broker server (auto-managed, prefer broker-server for production)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			dataDir := filepath.Join(home, ".broker")
			os.MkdirAll(dataDir, 0o755)

			cfg, _ := config.Load()

			stateStore, err := store.NewSQLite(filepath.Join(dataDir, "broker.db"))
			if err != nil {
				return fmt.Errorf("state store: %w", err)
			}
			defer stateStore.Close()

			analyticsBackend := cfg.Analytics.Backend
			analyticsDSN := cfg.Analytics.DSN
			if analyticsDSN == "" {
				switch analyticsBackend {
				case "chdb":
					analyticsDSN = filepath.Join(dataDir, "chdb")
				case "sqlite":
					analyticsDSN = filepath.Join(dataDir, "broker.db")
				}
			}
			analyticsStore, err := store.NewAnalyticsStore(analyticsBackend, analyticsDSN)
			if err != nil {
				return fmt.Errorf("analytics store: %w", err)
			}
			defer analyticsStore.Close()

			registry := provider.NewRegistry()
			if cfg.APIServer.PublicURL != "" {
				registry.Register(awsprovider.New(logger.With("provider", "aws"), cfg.APIServer.PublicURL))
			}
			srv := server.New(stateStore, analyticsStore, registry, logger)

			port := cfg.APIServer.HTTPPort
			if port == 0 {
				port = 8080
			}

			errCh := make(chan error, 2)

			go func() {
				errCh <- srv.Serve(port)
			}()

			// Start a local agent for the "local" cluster
			go func() {
				time.Sleep(200 * time.Millisecond)

				a := agent.New(agent.Config{
					ServerURL:          fmt.Sprintf("ws://localhost:%d", port),
					ClusterName:        "local",
					NodeID:             "local-0",
					SSHPort:            2222,
					Mode:               "run",
					SelfTerminateAfter: 0,
				}, logger.With("component", "agent"))

				errCh <- a.Run(ctx)
			}()

			select {
			case err := <-errCh:
				return err
			case <-ctx.Done():
				return nil
			}
		},
	}

	return cmd
}
