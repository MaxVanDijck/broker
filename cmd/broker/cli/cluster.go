package cli

import (
	"context"
	"fmt"
	"os"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	brokerpb "broker/proto/brokerpb"
)

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop CLUSTER",
		Short: "Stop a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			resp, err := c.Broker.Stop(context.Background(), connect.NewRequest(&brokerpb.ClusterRequest{ClusterName: args[0]}))
			if err != nil {
				return fmt.Errorf("stop failed: %w", err)
			}
			fmt.Printf("Cluster %s: %s\n", resp.Msg.ClusterName, resp.Msg.Status)
			return nil
		},
	}
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start CLUSTER",
		Short: "Start a stopped cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			resp, err := c.Broker.Start(context.Background(), connect.NewRequest(&brokerpb.ClusterRequest{ClusterName: args[0]}))
			if err != nil {
				return fmt.Errorf("start failed: %w", err)
			}
			fmt.Printf("Cluster %s: %s\n", resp.Msg.ClusterName, resp.Msg.Status)
			return nil
		},
	}
}

func downCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "down CLUSTER",
		Short: "Tear down a cluster",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !isTerminal() {
					return fmt.Errorf("refusing to tear down cluster without --yes flag when stdin is not a terminal")
				}
				fmt.Printf("Tearing down cluster %s. This will delete all resources. Continue? [y/N] ", args[0])
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			c := newClient()
			resp, err := c.Broker.Down(context.Background(), connect.NewRequest(&brokerpb.ClusterRequest{ClusterName: args[0]}))
			if err != nil {
				return fmt.Errorf("down failed: %w", err)
			}
			fmt.Printf("Cluster %s: %s\n", resp.Msg.ClusterName, resp.Msg.Status)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}
