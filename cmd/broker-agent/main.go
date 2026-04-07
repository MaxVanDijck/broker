package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"broker/internal/agent"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var (
		serverURL          string
		token              string
		clusterName        string
		nodeID             string
		sshPort            int
		selfTerminateAfter time.Duration
	)

	cmd := &cobra.Command{
		Use:   "broker-agent",
		Short: "Broker node agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if serverURL == "" {
				return fmt.Errorf("--server is required")
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: slog.LevelInfo,
			}))

			cfg := agent.Config{
				ServerURL:          serverURL,
				Token:              token,
				ClusterName:        clusterName,
				NodeID:             nodeID,
				SSHPort:            sshPort,
				SelfTerminateAfter: selfTerminateAfter,
			}

			a := agent.New(cfg, logger)

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			return a.Run(ctx)
		},
	}

	cmd.Flags().StringVar(&serverURL, "server", os.Getenv("BROKER_SERVER_URL"), "API server URL (e.g. ws://broker.example.com)")
	cmd.Flags().StringVar(&token, "token", os.Getenv("BROKER_TOKEN"), "Authentication token")
	cmd.Flags().StringVar(&clusterName, "cluster", os.Getenv("BROKER_CLUSTER"), "Cluster name this node belongs to")
	cmd.Flags().StringVar(&nodeID, "node-id", os.Getenv("BROKER_NODE_ID"), "Node ID (auto-generated if empty)")
	cmd.Flags().IntVar(&sshPort, "ssh-port", 2222, "Port for built-in SSH server")
	cmd.Flags().DurationVar(&selfTerminateAfter, "self-terminate-after", 30*time.Minute, "Terminate this node if server is unreachable for this duration (0 to disable)")

	return cmd
}
