package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
)

type Container struct {
	ID     string
	Name   string
	Image  string
	Status string
}

type RunConfig struct {
	Name    string
	Image   string
	Command []string
	Env     map[string]string
	Ports   []string
	GPUIDs  []string
	Network string
	Workdir string
	Remove  bool
}

type Manager struct {
	logger *slog.Logger
}

func NewManager(logger *slog.Logger) *Manager {
	return &Manager{logger: logger}
}

func (m *Manager) Pull(ctx context.Context, image string) error {
	m.logger.Info("pulling image", "image", image)
	cmd := exec.CommandContext(ctx, "docker", "pull", image)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker pull %s: %s: %w", image, string(output), err)
	}
	return nil
}

func (m *Manager) Run(ctx context.Context, cfg *RunConfig) (string, error) {
	args := []string{"run", "-d"}

	if cfg.Name != "" {
		args = append(args, "--name", cfg.Name)
	}
	if cfg.Remove {
		args = append(args, "--rm")
	}
	if cfg.Network != "" {
		args = append(args, "--network", cfg.Network)
	} else {
		args = append(args, "--network", "host")
	}
	if cfg.Workdir != "" {
		args = append(args, "-w", cfg.Workdir)
	}

	for k, v := range cfg.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}
	for _, p := range cfg.Ports {
		args = append(args, "-p", p)
	}

	if len(cfg.GPUIDs) > 0 {
		args = append(args, "--gpus", fmt.Sprintf(`"device=%s"`, strings.Join(cfg.GPUIDs, ",")))
	}

	args = append(args, cfg.Image)
	args = append(args, cfg.Command...)

	m.logger.Info("starting container", "image", cfg.Image, "name", cfg.Name)
	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker run: %s: %w", string(output), err)
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

func (m *Manager) Stop(ctx context.Context, containerID string, timeoutSecs int) error {
	m.logger.Info("stopping container", "id", containerID)
	cmd := exec.CommandContext(ctx, "docker", "stop", "-t", fmt.Sprintf("%d", timeoutSecs), containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop %s: %s: %w", containerID, string(output), err)
	}
	return nil
}

func (m *Manager) Remove(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm %s: %s: %w", containerID, string(output), err)
	}
	return nil
}

func (m *Manager) Logs(ctx context.Context, containerID string, follow bool) (*exec.Cmd, error) {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, containerID)

	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd, nil
}

func (m *Manager) Inspect(ctx context.Context, containerID string) (*Container, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", containerID, "--format", "{{json .}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker inspect: %w", err)
	}

	var raw struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		Config struct {
			Image string `json:"Image"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parse inspect: %w", err)
	}

	return &Container{
		ID:     raw.ID[:12],
		Name:   strings.TrimPrefix(raw.Name, "/"),
		Image:  raw.Config.Image,
		Status: raw.State.Status,
	}, nil
}

func (m *Manager) Available() bool {
	return exec.Command("docker", "info").Run() == nil
}
