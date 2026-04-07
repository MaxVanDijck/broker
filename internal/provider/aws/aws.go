package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"broker/internal/domain"
	"broker/internal/provider"
)

const (
	tagKeyCluster = "broker:cluster"
	tagKeyNodeID  = "broker:node-id"

	securityGroupName = "broker-agent"
	securityGroupDesc = "Broker agent security group - SSH and agent ports"

	DefaultRegion       = "us-east-1"
	defaultInstanceType = "t3.medium"
	defaultDiskSizeGB   = 100

	// Agent binary is downloaded from the broker server itself.
	// This is a temporary measure -- replace with custom AMIs.
	agentBinaryPath         = "/agent/v1/binary"
	selfTerminateAfterAgent = "30m"

	amiSSMParameterDefault = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
	amiSSMParameterGPU     = "/aws/service/deeplearning/ami/x86_64/base-oss-nvidia-driver-gpu-amazon-linux-2023/latest/ami-id"
)

var acceleratorInstanceTypes = map[string]string{
	"A100":   "p4d.24xlarge",
	"A100:1": "p4d.24xlarge",
	"A100:2": "p4d.24xlarge",
	"A100:4": "p4d.24xlarge",
	"A100:8": "p4d.24xlarge",
	"H100":   "p5.48xlarge",
	"H100:1": "p5.48xlarge",
	"H100:2": "p5.48xlarge",
	"H100:4": "p5.48xlarge",
	"H100:8": "p5.48xlarge",
	"T4":     "g4dn.xlarge",
	"T4:1":   "g4dn.xlarge",
	"T4:4":   "g4dn.12xlarge",
	"V100":   "p3.2xlarge",
	"V100:1": "p3.2xlarge",
	"V100:4": "p3.8xlarge",
	"V100:8": "p3.16xlarge",
	"L4":     "g6.xlarge",
	"L4:1":   "g6.xlarge",
	"L4:4":   "g6.12xlarge",
	"L4:8":   "g6.48xlarge",
	"A10G":   "g5.xlarge",
	"A10G:1": "g5.xlarge",
	"A10G:4": "g5.12xlarge",
	"A10G:8": "g5.48xlarge",
	"K80":    "p2.xlarge",
	"K80:1":  "p2.xlarge",
	"K80:8":  "p2.8xlarge",
	"K80:16": "p2.16xlarge",
}

type Provider struct {
	logger    *slog.Logger
	serverURL string
}

func New(logger *slog.Logger, serverURL string) *Provider {
	return &Provider{
		logger:    logger,
		serverURL: serverURL,
	}
}

func (p *Provider) Name() domain.CloudProvider {
	return domain.CloudAWS
}

// fallbackRegions is the ordered list of regions to try when capacity is
// unavailable. The user's preferred region (if any) is prepended at runtime.
var fallbackRegions = []string{
	"us-east-1",
	"us-west-2",
	"us-east-2",
	"eu-west-1",
	"ap-southeast-1",
}

func (p *Provider) Launch(ctx context.Context, cluster *domain.Cluster, task *domain.TaskSpec) ([]provider.NodeInfo, error) {
	regions := buildRegionList(cluster, task)

	var lastErr error
	for _, region := range regions {
		nodes, err := p.launchInRegion(ctx, cluster, task, region)
		if err == nil {
			cluster.Region = region
			return nodes, nil
		}
		if isCapacityError(err) {
			p.logger.Warn("region capacity unavailable, trying next region",
				"region", region,
				"error", err,
			)
			lastErr = err
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("all regions exhausted: %w", lastErr)
}

func (p *Provider) launchInRegion(ctx context.Context, cluster *domain.Cluster, task *domain.TaskSpec, region string) ([]provider.NodeInfo, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)
	ssmClient := ssm.NewFromConfig(cfg)

	sgID, err := p.ensureSecurityGroup(ctx, ec2Client, cluster.Name)
	if err != nil {
		return nil, fmt.Errorf("ensure security group: %w", err)
	}

	instanceType := resolveInstanceType(task)
	needsGPU := hasGPU(task)

	amiID, err := p.resolveAMI(ctx, ssmClient, needsGPU)
	if err != nil {
		return nil, fmt.Errorf("resolve AMI: %w", err)
	}
	diskSize := resolveDiskSize(task)
	numNodes := cluster.NumNodes
	if numNodes <= 0 {
		numNodes = 1
	}

	p.logger.Info("launching ec2 instances",
		"cluster", cluster.Name,
		"region", region,
		"ami", amiID,
		"instance_type", instanceType,
		"num_nodes", numNodes,
		"disk_gb", diskSize,
	)

	var nodes []provider.NodeInfo
	for i := range numNodes {
		nodeID := fmt.Sprintf("%s-node-%d", cluster.Name, i)

		userData := generateUserData(p.serverURL, cluster.Name, nodeID, cluster.ID)

		runInput := &ec2.RunInstancesInput{
			ImageId:          aws.String(amiID),
			InstanceType:     ec2types.InstanceType(instanceType),
			MinCount:         aws.Int32(1),
			MaxCount:         aws.Int32(1),
			SecurityGroupIds: []string{sgID},
			UserData:         aws.String(base64.StdEncoding.EncodeToString([]byte(userData))),
			TagSpecifications: []ec2types.TagSpecification{
				{
					ResourceType: ec2types.ResourceTypeInstance,
					Tags: []ec2types.Tag{
						{Key: aws.String("Name"), Value: aws.String(nodeID)},
						{Key: aws.String(tagKeyCluster), Value: aws.String(cluster.Name)},
						{Key: aws.String(tagKeyNodeID), Value: aws.String(nodeID)},
					},
				},
			},
			BlockDeviceMappings: []ec2types.BlockDeviceMapping{
				{
					DeviceName: aws.String("/dev/xvda"),
					Ebs: &ec2types.EbsBlockDevice{
						VolumeSize: aws.Int32(int32(diskSize)),
						VolumeType: ec2types.VolumeTypeGp3,
					},
				},
			},
		}

		if task.Resources != nil && task.Resources.UseSpot {
			runInput.InstanceMarketOptions = &ec2types.InstanceMarketOptionsRequest{
				MarketType: ec2types.MarketTypeSpot,
			}
		}

		result, err := ec2Client.RunInstances(ctx, runInput)
		if err != nil {
			return nodes, fmt.Errorf("run instance %s: %w", nodeID, err)
		}

		if len(result.Instances) == 0 {
			return nodes, fmt.Errorf("no instances returned for %s", nodeID)
		}

		instanceID := aws.ToString(result.Instances[0].InstanceId)
		p.logger.Info("instance created, waiting for running state", "instance_id", instanceID, "node_id", nodeID)

		waiter := ec2.NewInstanceRunningWaiter(ec2Client)
		err = waiter.Wait(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceID},
		}, 5*time.Minute)
		if err != nil {
			return nodes, fmt.Errorf("wait for instance %s running: %w", instanceID, err)
		}

		descResult, err := ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceID},
		})
		if err != nil {
			return nodes, fmt.Errorf("describe instance %s: %w", instanceID, err)
		}

		node := provider.NodeInfo{
			InstanceID:   instanceID,
			Status:       "running",
			Region:       region,
			InstanceType: string(instanceType),
		}

		if len(descResult.Reservations) > 0 && len(descResult.Reservations[0].Instances) > 0 {
			inst := descResult.Reservations[0].Instances[0]
			node.PublicIP = aws.ToString(inst.PublicIpAddress)
			node.PrivateIP = aws.ToString(inst.PrivateIpAddress)
		}

		p.logger.Info("instance running", "instance_id", instanceID, "public_ip", node.PublicIP, "node_id", nodeID)
		nodes = append(nodes, node)
	}

	return nodes, nil
}

// buildRegionList returns the ordered list of regions to attempt. If the user
// specified a region it comes first, followed by the remaining fallback regions
// (deduped). If no region was specified, fallbackRegions is returned as-is.
func buildRegionList(cluster *domain.Cluster, task *domain.TaskSpec) []string {
	preferred := preferredRegion(cluster, task)

	regions := []string{preferred}
	for _, r := range fallbackRegions {
		if r != preferred {
			regions = append(regions, r)
		}
	}
	return regions
}

func preferredRegion(cluster *domain.Cluster, task *domain.TaskSpec) string {
	if cluster.Region != "" {
		return cluster.Region
	}
	if task != nil && task.Resources != nil && task.Resources.Region != "" {
		return task.Resources.Region
	}
	return DefaultRegion
}

// isCapacityError returns true for AWS errors that indicate the region/AZ
// cannot fulfil the request and a different region should be tried.
func isCapacityError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "InsufficientInstanceCapacity") ||
		strings.Contains(msg, "InstanceLimitExceeded") ||
		strings.Contains(msg, "Unsupported")
}

func (p *Provider) Stop(ctx context.Context, cluster *domain.Cluster) error {
	region := preferredRegion(cluster, nil)
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)
	instanceIDs, err := p.findClusterInstances(ctx, ec2Client, cluster.Name, runningFilter())
	if err != nil {
		return fmt.Errorf("find cluster instances: %w", err)
	}

	if len(instanceIDs) == 0 {
		p.logger.Info("no running instances found to stop", "cluster", cluster.Name)
		return nil
	}

	p.logger.Info("stopping instances", "cluster", cluster.Name, "count", len(instanceIDs))
	_, err = ec2Client.StopInstances(ctx, &ec2.StopInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return fmt.Errorf("stop instances: %w", err)
	}

	return nil
}

func (p *Provider) Start(ctx context.Context, cluster *domain.Cluster) error {
	region := preferredRegion(cluster, nil)
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)
	instanceIDs, err := p.findClusterInstances(ctx, ec2Client, cluster.Name, stoppedFilter())
	if err != nil {
		return fmt.Errorf("find cluster instances: %w", err)
	}

	if len(instanceIDs) == 0 {
		p.logger.Info("no stopped instances found to start", "cluster", cluster.Name)
		return nil
	}

	p.logger.Info("starting instances", "cluster", cluster.Name, "count", len(instanceIDs))
	_, err = ec2Client.StartInstances(ctx, &ec2.StartInstancesInput{
		InstanceIds: instanceIDs,
	})
	if err != nil {
		return fmt.Errorf("start instances: %w", err)
	}

	return nil
}

func (p *Provider) Teardown(ctx context.Context, cluster *domain.Cluster) error {
	region := preferredRegion(cluster, nil)
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}

	ec2Client := ec2.NewFromConfig(cfg)

	instanceIDs, err := p.findClusterInstances(ctx, ec2Client, cluster.Name, nonTerminatedFilter())
	if err != nil {
		return fmt.Errorf("find cluster instances: %w", err)
	}

	if len(instanceIDs) > 0 {
		p.logger.Info("terminating instances", "cluster", cluster.Name, "count", len(instanceIDs))
		_, err = ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
			InstanceIds: instanceIDs,
		})
		if err != nil {
			return fmt.Errorf("terminate instances: %w", err)
		}

		waiter := ec2.NewInstanceTerminatedWaiter(ec2Client)
		err = waiter.Wait(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: instanceIDs,
		}, 5*time.Minute)
		if err != nil {
			p.logger.Warn("timeout waiting for instance termination", "cluster", cluster.Name, "error", err)
		}
	}

	if err := p.deleteSecurityGroup(ctx, ec2Client, cluster.Name); err != nil {
		p.logger.Warn("failed to delete security group", "cluster", cluster.Name, "error", err)
	}

	return nil
}

func (p *Provider) ensureSecurityGroup(ctx context.Context, client *ec2.Client, clusterName string) (string, error) {
	sgName := securityGroupName + "-" + clusterName

	descResult, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("group-name"), Values: []string{sgName}},
		},
	})
	if err == nil && len(descResult.SecurityGroups) > 0 {
		sgID := aws.ToString(descResult.SecurityGroups[0].GroupId)
		p.logger.Info("using existing security group", "sg_id", sgID, "name", sgName)
		return sgID, nil
	}

	createResult, err := client.CreateSecurityGroup(ctx, &ec2.CreateSecurityGroupInput{
		GroupName:   aws.String(sgName),
		Description: aws.String(securityGroupDesc),
		TagSpecifications: []ec2types.TagSpecification{
			{
				ResourceType: ec2types.ResourceTypeSecurityGroup,
				Tags: []ec2types.Tag{
					{Key: aws.String(tagKeyCluster), Value: aws.String(clusterName)},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("create security group: %w", err)
	}

	sgID := aws.ToString(createResult.GroupId)
	p.logger.Info("created security group", "sg_id", sgID, "name", sgName)

	// No inbound rules needed: SSH goes through the broker tunnel and the
	// agent connects outbound to the server. Default VPC allows all
	// outbound traffic.

	return sgID, nil
}

func (p *Provider) deleteSecurityGroup(ctx context.Context, client *ec2.Client, clusterName string) error {
	sgName := securityGroupName + "-" + clusterName

	descResult, err := client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("group-name"), Values: []string{sgName}},
		},
	})
	if err != nil {
		return fmt.Errorf("describe security groups: %w", err)
	}

	if len(descResult.SecurityGroups) == 0 {
		return nil
	}

	sgID := aws.ToString(descResult.SecurityGroups[0].GroupId)
	_, err = client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
		GroupId: aws.String(sgID),
	})
	if err != nil {
		return fmt.Errorf("delete security group %s: %w", sgID, err)
	}

	p.logger.Info("deleted security group", "sg_id", sgID, "name", sgName)
	return nil
}

func (p *Provider) resolveAMI(ctx context.Context, client *ssm.Client, needsGPU bool) (string, error) {
	param := amiSSMParameterDefault
	if needsGPU {
		param = amiSSMParameterGPU
	}

	result, err := client.GetParameter(ctx, &ssm.GetParameterInput{
		Name: aws.String(param),
	})
	if err != nil {
		return "", fmt.Errorf("get AMI from SSM (param=%s): %w", param, err)
	}

	return aws.ToString(result.Parameter.Value), nil
}

func (p *Provider) findClusterInstances(ctx context.Context, client *ec2.Client, clusterName string, stateFilter ec2types.Filter) ([]string, error) {
	result, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: aws.String("tag:" + tagKeyCluster), Values: []string{clusterName}},
			stateFilter,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("describe instances: %w", err)
	}

	var ids []string
	for _, res := range result.Reservations {
		for _, inst := range res.Instances {
			ids = append(ids, aws.ToString(inst.InstanceId))
		}
	}

	return ids, nil
}

func resolveInstanceType(task *domain.TaskSpec) string {
	if task.Resources != nil && task.Resources.InstanceType != "" {
		return task.Resources.InstanceType
	}
	if task.Resources != nil && task.Resources.Accelerators != "" {
		if it, ok := MapAcceleratorToInstanceType(task.Resources.Accelerators); ok {
			return it
		}
	}
	// The server's Launch handler runs the optimizer before calling the
	// provider, so Resources.InstanceType is normally populated by the
	// time we get here. This fallback only fires for direct provider
	// calls outside the server path (tests, scripts, etc.).
	return defaultInstanceType
}

func hasGPU(task *domain.TaskSpec) bool {
	if task == nil || task.Resources == nil {
		return false
	}
	if task.Resources.Accelerators != "" {
		return true
	}
	// Check if the instance type is a known GPU instance
	it := task.Resources.InstanceType
	for _, prefix := range []string{"p2.", "p3.", "p4d.", "p4de.", "p5.", "g4dn.", "g5.", "g5g.", "g6.", "g6e.", "gr6."} {
		if strings.HasPrefix(it, prefix) {
			return true
		}
	}
	return false
}

func MapAcceleratorToInstanceType(accelerator string) (string, bool) {
	if it, ok := acceleratorInstanceTypes[accelerator]; ok {
		return it, true
	}

	name := strings.SplitN(accelerator, ":", 2)[0]
	name = strings.ToUpper(strings.TrimSpace(name))
	if it, ok := acceleratorInstanceTypes[name]; ok {
		return it, true
	}

	return "", false
}

func resolveDiskSize(task *domain.TaskSpec) int {
	if task.Resources != nil && task.Resources.DiskSizeGB > 0 {
		return task.Resources.DiskSizeGB
	}
	return defaultDiskSizeGB
}

func shellEscape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func generateUserData(serverURL, clusterName, nodeID, token string) string {
	// Normalize the server URL for both HTTP (binary download) and WebSocket (agent).
	// Accept any scheme in config and derive the correct one for each use.
	httpBase := serverURL
	httpBase = strings.Replace(httpBase, "wss://", "https://", 1)
	httpBase = strings.Replace(httpBase, "ws://", "http://", 1)
	if !strings.HasPrefix(httpBase, "http") {
		httpBase = "https://" + httpBase
	}
	binaryURL := httpBase + agentBinaryPath

	wsBase := serverURL
	wsBase = strings.Replace(wsBase, "https://", "wss://", 1)
	wsBase = strings.Replace(wsBase, "http://", "ws://", 1)
	if !strings.HasPrefix(wsBase, "ws") {
		wsBase = "wss://" + wsBase
	}

	escapedBinaryURL := shellEscape(binaryURL)
	escapedWsBase := shellEscape(wsBase)
	escapedCluster := shellEscape(clusterName)
	escapedToken := shellEscape(token)
	escapedNodeID := shellEscape(nodeID)

	curlAuth := ""
	if token != "" {
		curlAuth = fmt.Sprintf(` -u "broker:%s"`, shellEscape(token))
	}

	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail

# Temporary bootstrap: download agent binary from the broker server.
# Replace with a custom AMI once a Packer pipeline exists. If this
# download fails, the server's provision watchdog will terminate this
# instance after 30 minutes.

MAX_RETRIES=10
for i in $(seq 1 $MAX_RETRIES); do
  curl -fsSL%s -o /usr/local/bin/broker-agent %s && break
  echo "download attempt $i failed, retrying in 10s..."
  sleep 10
done
chmod +x /usr/local/bin/broker-agent

cat > /etc/systemd/system/broker-agent.service <<'UNIT'
[Unit]
Description=Broker Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/broker-agent \
  --server %s \
  --cluster %s \
  --token %s \
  --node-id %s \
  --self-terminate-after %s
Restart=always
RestartSec=5
Environment=HOME=/root

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now broker-agent.service
`, curlAuth, escapedBinaryURL, escapedWsBase, escapedCluster, escapedToken, escapedNodeID, selfTerminateAfterAgent)
}

func nonTerminatedFilter() ec2types.Filter {
	return ec2types.Filter{
		Name: aws.String("instance-state-name"),
		Values: []string{
			string(ec2types.InstanceStateNamePending),
			string(ec2types.InstanceStateNameRunning),
			string(ec2types.InstanceStateNameStopping),
			string(ec2types.InstanceStateNameStopped),
		},
	}
}

func runningFilter() ec2types.Filter {
	return ec2types.Filter{
		Name: aws.String("instance-state-name"),
		Values: []string{
			string(ec2types.InstanceStateNameRunning),
		},
	}
}

func stoppedFilter() ec2types.Filter {
	return ec2types.Filter{
		Name: aws.String("instance-state-name"),
		Values: []string{
			string(ec2types.InstanceStateNameStopped),
		},
	}
}
