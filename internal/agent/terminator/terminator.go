package terminator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

type Cloud string

const (
	CloudAWS     Cloud = "aws"
	CloudGCP     Cloud = "gcp"
	CloudAzure   Cloud = "azure"
	CloudUnknown Cloud = "unknown"
)

type Terminator struct {
	logger *slog.Logger
	cloud  Cloud
}

func New(logger *slog.Logger) *Terminator {
	t := &Terminator{logger: logger}
	t.cloud = t.detectCloud()
	logger.Info("cloud detected for self-termination", "cloud", t.cloud)
	return t
}

func (t *Terminator) Terminate(ctx context.Context) error {
	t.logger.Info("self-terminating instance", "cloud", t.cloud)

	switch t.cloud {
	case CloudAWS:
		return t.terminateAWS(ctx)
	case CloudGCP:
		return t.terminateGCP(ctx)
	case CloudAzure:
		return t.terminateAzure(ctx)
	default:
		return t.terminateGeneric(ctx)
	}
}

func (t *Terminator) detectCloud() Cloud {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// AWS: check instance metadata v2
	if t.probeURL(ctx, "http://169.254.169.254/latest/meta-data/instance-id", map[string]string{
		"X-aws-ec2-metadata-token-ttl-seconds": "5",
	}) {
		return CloudAWS
	}

	// GCP: check metadata server
	if t.probeURL(ctx, "http://metadata.google.internal/computeMetadata/v1/instance/id", map[string]string{
		"Metadata-Flavor": "Google",
	}) {
		return CloudGCP
	}

	// Azure: check IMDS
	if t.probeURL(ctx, "http://169.254.169.254/metadata/instance?api-version=2021-02-01", map[string]string{
		"Metadata": "true",
	}) {
		return CloudAzure
	}

	return CloudUnknown
}

func (t *Terminator) probeURL(ctx context.Context, url string, headers map[string]string) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return false
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func (t *Terminator) terminateAWS(ctx context.Context) error {
	// Get IMDSv2 token
	tokenReq, _ := http.NewRequestWithContext(ctx, "PUT",
		"http://169.254.169.254/latest/api/token", nil)
	tokenReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "30")

	tokenResp, err := http.DefaultClient.Do(tokenReq)
	if err != nil {
		return fmt.Errorf("get imds token: %w", err)
	}
	tokenBody, _ := io.ReadAll(tokenResp.Body)
	tokenResp.Body.Close()
	token := strings.TrimSpace(string(tokenBody))

	// Get instance ID
	idReq, _ := http.NewRequestWithContext(ctx, "GET",
		"http://169.254.169.254/latest/meta-data/instance-id", nil)
	idReq.Header.Set("X-aws-ec2-metadata-token", token)

	idResp, err := http.DefaultClient.Do(idReq)
	if err != nil {
		return fmt.Errorf("get instance id: %w", err)
	}
	idBody, _ := io.ReadAll(idResp.Body)
	idResp.Body.Close()
	instanceID := strings.TrimSpace(string(idBody))

	// Get region
	regionReq, _ := http.NewRequestWithContext(ctx, "GET",
		"http://169.254.169.254/latest/meta-data/placement/region", nil)
	regionReq.Header.Set("X-aws-ec2-metadata-token", token)

	regionResp, err := http.DefaultClient.Do(regionReq)
	if err != nil {
		return fmt.Errorf("get region: %w", err)
	}
	regionBody, _ := io.ReadAll(regionResp.Body)
	regionResp.Body.Close()
	region := strings.TrimSpace(string(regionBody))

	t.logger.Info("terminating EC2 instance", "instance_id", instanceID, "region", region)

	cmd := exec.CommandContext(ctx, "aws", "ec2", "terminate-instances",
		"--instance-ids", instanceID,
		"--region", region)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("aws terminate: %s: %w", string(output), err)
	}
	return nil
}

func (t *Terminator) terminateGCP(ctx context.Context) error {
	// Get instance name and zone from metadata
	name, err := t.gcpMetadata(ctx, "instance/name")
	if err != nil {
		return fmt.Errorf("get instance name: %w", err)
	}
	zone, err := t.gcpMetadata(ctx, "instance/zone")
	if err != nil {
		return fmt.Errorf("get zone: %w", err)
	}
	// zone is like "projects/123/zones/us-central1-a", extract last part
	parts := strings.Split(zone, "/")
	zone = parts[len(parts)-1]

	t.logger.Info("terminating GCE instance", "name", name, "zone", zone)

	cmd := exec.CommandContext(ctx, "gcloud", "compute", "instances", "delete",
		name, "--zone", zone, "--quiet")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("gcloud delete: %s: %w", string(output), err)
	}
	return nil
}

func (t *Terminator) gcpMetadata(ctx context.Context, path string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET",
		"http://metadata.google.internal/computeMetadata/v1/"+path, nil)
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return strings.TrimSpace(string(body)), nil
}

func (t *Terminator) terminateAzure(ctx context.Context) error {
	t.logger.Info("azure self-termination not yet implemented, shutting down")
	return t.terminateGeneric(ctx)
}

func (t *Terminator) terminateGeneric(ctx context.Context) error {
	t.logger.Info("no cloud detected, attempting shutdown via system command")
	cmd := exec.CommandContext(ctx, "shutdown", "-h", "now")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("shutdown: %s: %w", string(output), err)
	}
	return nil
}
