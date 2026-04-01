package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"broker/internal/domain"
	"broker/internal/workdir"
	brokerpb "broker/proto/brokerpb"
)

func launchCmd() *cobra.Command {
	var (
		clusterName  string
		gpus         string
		cloud        string
		workdirFlag  string
		detach       bool
		autostopFlag time.Duration
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
			if workdirFlag != "" {
				task.Workdir = workdirFlag
			}

			c := newClient()

			// Upload workdir if specified
			var workdirID string
			if task.Workdir != "" {
				var err error
				workdirID, err = uploadWorkdir(task.Workdir)
				if err != nil {
					return fmt.Errorf("upload workdir: %w", err)
				}
			}

			pbTask := taskToProto(task)
			pbTask.Workdir = workdirID

			autostopMinutes := int32(autostopFlag.Minutes())
			resp, err := c.Broker.Launch(context.Background(), connect.NewRequest(&brokerpb.LaunchRequest{
				ClusterName:           clusterName,
				Task:                  pbTask,
				IdleMinutesToAutostop: autostopMinutes,
			}))
			if err != nil {
				return fmt.Errorf("launch failed: %w", err)
			}

			if resp.Msg.InstanceType != "" && resp.Msg.HourlyPrice > 0 {
				region := resp.Msg.Region
				if region == "" {
					region = "us-east-1"
				}
				spot := ""
				if task.Resources != nil && task.Resources.UseSpot {
					spot = " spot"
				}
				fmt.Printf("Cluster %s launched on %s%s ($%.2f/hr) in %s\n",
					resp.Msg.ClusterName, resp.Msg.InstanceType, spot, resp.Msg.HourlyPrice, region)
			} else {
				fmt.Printf("Cluster %s launched\n", resp.Msg.ClusterName)
			}
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
	cmd.Flags().StringVarP(&workdirFlag, "workdir", "w", "", "Working directory to upload")
	cmd.Flags().BoolVarP(&detach, "detach-run", "d", false, "Detach after job submission")
	cmd.Flags().DurationVar(&autostopFlag, "autostop", 30*time.Minute, "Idle duration before auto-teardown (0 to disable)")

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

func uploadWorkdir(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(absDir)
	if err != nil {
		return "", fmt.Errorf("workdir %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %s is not a directory", dir)
	}

	archivePath, err := workdir.Archive(absDir)
	if err != nil {
		return "", fmt.Errorf("archive: %w", err)
	}
	defer os.Remove(archivePath)

	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	stat, _ := f.Stat()
	fmt.Printf("Uploading workdir %s (%d KB)...\n", filepath.Base(absDir), stat.Size()/1024)

	id := uuid.New().String()[:8]
	url := fmt.Sprintf("%s/api/v1/workdir/%s", serverAddr(), id)

	resp, err := http.Post(url, "application/gzip", f)
	if err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upload failed: %s", resp.Status)
	}

	return id, nil
}
