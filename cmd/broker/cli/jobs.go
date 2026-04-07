package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"text/tabwriter"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	brokerpb "broker/proto/brokerpb"
)

func jobsCmd() *cobra.Command {
	var cluster string

	cmd := &cobra.Command{
		Use:   "jobs",
		Short: "List jobs",
		RunE: func(cmd *cobra.Command, args []string) error {
			ensureServer()

			url := serverAddr() + "/api/v1/jobs"
			if cluster != "" {
				url += "?cluster=" + cluster
			}

			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			setAuthHeader(req)

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return fmt.Errorf("failed to list jobs: %w", err)
			}
			defer resp.Body.Close()

			var result struct {
				Jobs []struct {
					ID          string `json:"id"`
					ClusterName string `json:"cluster_name"`
					Name        string `json:"name"`
					Status      string `json:"status"`
					SubmittedAt string `json:"submitted_at"`
				} `json:"jobs"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tCLUSTER\tNAME\tSTATUS\tSUBMITTED")
			for _, j := range result.Jobs {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					j.ID, j.ClusterName, j.Name, j.Status, j.SubmittedAt)
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().StringVarP(&cluster, "cluster", "c", "", "Filter by cluster name")
	return cmd
}

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

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
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
