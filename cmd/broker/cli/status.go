package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	brokerpb "broker/proto/brokerpb"
)

func statusCmd() *cobra.Command {
	var refresh bool

	cmd := &cobra.Command{
		Use:   "status [clusters...]",
		Short: "Show cluster status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient()
			resp, err := c.Broker.Status(context.Background(), connect.NewRequest(&brokerpb.StatusRequest{
				ClusterNames: args,
				Refresh:      refresh,
			}))
			if err != nil {
				return fmt.Errorf("status failed: %w", err)
			}

			if len(resp.Msg.Clusters) == 0 {
				fmt.Println("No clusters.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tSTATUS\tCLOUD\tREGION\tRESOURCES\tNODES\tLAUNCHED")
			for _, cl := range resp.Msg.Clusters {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
					cl.Name, cl.Status, cl.Cloud, cl.Region,
					cl.Resources, cl.NumNodes, cl.LaunchedAt,
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().BoolVarP(&refresh, "refresh", "r", false, "Refresh cluster status from cloud")

	return cmd
}
