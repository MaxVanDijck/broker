package cli

import (
	"context"
	"fmt"
	"os"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"broker/internal/domain"
	brokerpb "broker/proto/brokerpb"
)

func launchCmd() *cobra.Command {
	var (
		clusterName string
		gpus        string
		cloud       string
		detach      bool
	)

	cmd := &cobra.Command{
		Use:   "launch [yaml-or-command]",
		Short: "Launch a cluster or task",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			task, err := resolveTask(args)
			if err != nil {
				return err
			}

			if gpus != "" && task.Resources != nil {
				task.Resources.Accelerators = gpus
			}
			if cloud != "" && task.Resources != nil {
				task.Resources.Cloud = domain.CloudProvider(cloud)
			}

			c := newClient()
			resp, err := c.Broker.Launch(context.Background(), connect.NewRequest(&brokerpb.LaunchRequest{
				ClusterName: clusterName,
				Task:        taskToProto(task),
			}))
			if err != nil {
				return fmt.Errorf("launch failed: %w", err)
			}

			fmt.Printf("Cluster %s launched\n", resp.Msg.ClusterName)
			if resp.Msg.HeadIp != "" {
				fmt.Printf("Head node: %s\n", resp.Msg.HeadIp)
			}

			_ = detach
			return nil
		},
	}

	cmd.Flags().StringVarP(&clusterName, "cluster", "c", "", "Cluster name")
	cmd.Flags().StringVar(&gpus, "gpus", "", "GPU type and count (e.g. A100:4)")
	cmd.Flags().StringVar(&cloud, "cloud", "", "Cloud provider")
	cmd.Flags().BoolVarP(&detach, "detach-run", "d", false, "Detach after job submission")

	return cmd
}

func resolveTask(args []string) (*domain.TaskSpec, error) {
	task := &domain.TaskSpec{Resources: &domain.Resources{}}

	if len(args) == 0 {
		return task, nil
	}

	if _, err := os.Stat(args[0]); err == nil {
		data, err := os.ReadFile(args[0])
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", args[0], err)
		}
		if err := yaml.Unmarshal(data, task); err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", args[0], err)
		}
		if task.Resources == nil {
			task.Resources = &domain.Resources{}
		}
		return task, nil
	}

	run := ""
	for i, a := range args {
		if i > 0 {
			run += " "
		}
		run += a
	}
	task.Run = run
	return task, nil
}

func taskToProto(t *domain.TaskSpec) *brokerpb.TaskSpec {
	p := &brokerpb.TaskSpec{
		Name:     t.Name,
		Workdir:  t.Workdir,
		NumNodes: int32(t.NumNodes),
		Envs:     t.Envs,
		Setup:    t.Setup,
		Run:      t.Run,
	}
	if t.Resources != nil {
		p.Resources = &brokerpb.Resources{
			Cloud:        string(t.Resources.Cloud),
			Region:       t.Resources.Region,
			Zone:         t.Resources.Zone,
			Accelerators: t.Resources.Accelerators,
			Cpus:         t.Resources.CPUs,
			Memory:       t.Resources.Memory,
			InstanceType: t.Resources.InstanceType,
			UseSpot:      t.Resources.UseSpot,
			DiskSizeGb:   int32(t.Resources.DiskSizeGB),
			ImageId:      t.Resources.ImageID,
		}
		for _, port := range t.Resources.Ports {
			p.Resources.Ports = append(p.Resources.Ports, port)
		}
	}
	return p
}
