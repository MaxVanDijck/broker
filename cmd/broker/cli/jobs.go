package cli

import (
	"context"
	"fmt"
	"io"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	brokerpb "broker/proto/brokerpb"
)

func execCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "exec CLUSTER [yaml-or-command...]",
		Short: "Execute a task on an existing cluster",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			task, err := resolveTask(args[1:])
			if err != nil {
				return err
			}

			c := newClient()
			resp, err := c.Broker.Exec(context.Background(), connect.NewRequest(&brokerpb.ExecRequest{
				ClusterName: clusterName,
				Task:        taskToProto(task),
			}))
			if err != nil {
				return fmt.Errorf("exec failed: %w", err)
			}

			fmt.Printf("Job %s submitted on %s\n", resp.Msg.JobId, clusterName)
			return nil
		},
	}

	return cmd
}

func logsCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs CLUSTER [JOB_ID]",
		Short: "Tail logs of a job",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]
			jobID := ""
			if len(args) > 1 {
				jobID = args[1]
			}

			c := newClient()
			stream, err := c.Broker.Logs(context.Background(), connect.NewRequest(&brokerpb.LogsRequest{
				ClusterName: clusterName,
				JobId:       jobID,
				Follow:      follow,
			}))
			if err != nil {
				return fmt.Errorf("logs failed: %w", err)
			}

			for stream.Receive() {
				fmt.Println(stream.Msg().Line)
			}
			if err := stream.Err(); err != nil && err != io.EOF {
				return err
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", true, "Follow log output")
	return cmd
}

func cancelCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "cancel CLUSTER [JOB_IDS...]",
		Short: "Cancel jobs on a cluster",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			c := newClient()
			_, err := c.Broker.CancelJob(context.Background(), connect.NewRequest(&brokerpb.CancelJobRequest{
				ClusterName: clusterName,
				JobIds:      args[1:],
				All:         all,
			}))
			if err != nil {
				return fmt.Errorf("cancel failed: %w", err)
			}

			fmt.Println("Jobs cancelled.")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Cancel all jobs")
	return cmd
}
