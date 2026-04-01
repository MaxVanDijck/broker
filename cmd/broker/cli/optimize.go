package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"broker/internal/optimizer"
)

func optimizeCmd() *cobra.Command {
	var (
		gpus   string
		cpus   string
		memory string
		spot   bool
	)

	cmd := &cobra.Command{
		Use:   "optimize",
		Short: "Show cheapest instance types for given requirements",
		Long:  "Queries the instance catalog and pricing data to find the cheapest AWS instance types matching the given resource requirements.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if gpus == "" && cpus == "" && memory == "" {
				return fmt.Errorf("at least one requirement must be specified (--gpus, --cpus, or --memory)")
			}

			recs, err := optimizer.Optimize(optimizer.Requirements{
				Accelerators: gpus,
				CPUs:         cpus,
				Memory:       memory,
				UseSpot:      spot,
			})
			if err != nil {
				return err
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "INSTANCE TYPE\tGPU\tVCPUS\tMEMORY\tPRICE/HR")
			for _, r := range recs {
				gpu := "-"
				if r.GPUModel != "" {
					gpu = fmt.Sprintf("%s x%d", r.GPUModel, r.GPUCount)
				}
				priceLabel := fmt.Sprintf("$%.2f", r.HourlyPrice)
				if r.IsSpot {
					priceLabel += " (spot)"
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%.0f GB\t%s\n",
					r.InstanceType, gpu, r.VCPUs, r.MemoryGB, priceLabel)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&gpus, "gpus", "", "GPU type and count (e.g. T4:1, A100:8)")
	cmd.Flags().StringVar(&cpus, "cpus", "", "Minimum vCPUs (e.g. 4, 16+)")
	cmd.Flags().StringVar(&memory, "memory", "", "Minimum memory in GB (e.g. 32, 64+)")
	cmd.Flags().BoolVar(&spot, "spot", false, "Show spot pricing")

	return cmd
}
