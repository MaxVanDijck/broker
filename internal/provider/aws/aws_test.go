package aws

import (
	"strings"
	"testing"

	"broker/internal/domain"
)

func TestMapAcceleratorToInstanceType(t *testing.T) {
	t.Run("given A100 accelerator, when mapping, then p4d.24xlarge is returned", func(t *testing.T) {
		it, ok := MapAcceleratorToInstanceType("A100")
		if !ok {
			t.Fatal("expected mapping to exist")
		}
		if it != "p4d.24xlarge" {
			t.Errorf("expected p4d.24xlarge, got %s", it)
		}
	})

	t.Run("given A100:8 accelerator, when mapping, then p4d.24xlarge is returned", func(t *testing.T) {
		it, ok := MapAcceleratorToInstanceType("A100:8")
		if !ok {
			t.Fatal("expected mapping to exist")
		}
		if it != "p4d.24xlarge" {
			t.Errorf("expected p4d.24xlarge, got %s", it)
		}
	})

	t.Run("given H100 accelerator, when mapping, then p5.48xlarge is returned", func(t *testing.T) {
		it, ok := MapAcceleratorToInstanceType("H100")
		if !ok {
			t.Fatal("expected mapping to exist")
		}
		if it != "p5.48xlarge" {
			t.Errorf("expected p5.48xlarge, got %s", it)
		}
	})

	t.Run("given T4 accelerator, when mapping, then g4dn.xlarge is returned", func(t *testing.T) {
		it, ok := MapAcceleratorToInstanceType("T4")
		if !ok {
			t.Fatal("expected mapping to exist")
		}
		if it != "g4dn.xlarge" {
			t.Errorf("expected g4dn.xlarge, got %s", it)
		}
	})

	t.Run("given V100:4 accelerator, when mapping, then p3.8xlarge is returned", func(t *testing.T) {
		it, ok := MapAcceleratorToInstanceType("V100:4")
		if !ok {
			t.Fatal("expected mapping to exist")
		}
		if it != "p3.8xlarge" {
			t.Errorf("expected p3.8xlarge, got %s", it)
		}
	})

	t.Run("given L4:8 accelerator, when mapping, then g6.48xlarge is returned", func(t *testing.T) {
		it, ok := MapAcceleratorToInstanceType("L4:8")
		if !ok {
			t.Fatal("expected mapping to exist")
		}
		if it != "g6.48xlarge" {
			t.Errorf("expected g6.48xlarge, got %s", it)
		}
	})

	t.Run("given A10G accelerator, when mapping, then g5.xlarge is returned", func(t *testing.T) {
		it, ok := MapAcceleratorToInstanceType("A10G")
		if !ok {
			t.Fatal("expected mapping to exist")
		}
		if it != "g5.xlarge" {
			t.Errorf("expected g5.xlarge, got %s", it)
		}
	})

	t.Run("given unknown accelerator, when mapping, then false is returned", func(t *testing.T) {
		_, ok := MapAcceleratorToInstanceType("TPUv5")
		if ok {
			t.Error("expected no mapping for unknown accelerator")
		}
	})

	t.Run("given lowercase accelerator name with count, when mapping, then correct instance type is returned via base name fallback", func(t *testing.T) {
		it, ok := MapAcceleratorToInstanceType("a100:2")
		if !ok {
			t.Fatal("expected mapping to exist via case-insensitive fallback")
		}
		if it != "p4d.24xlarge" {
			t.Errorf("expected p4d.24xlarge, got %s", it)
		}
	})
}

func TestResolveInstanceType(t *testing.T) {
	t.Run("given explicit instance type, when resolving, then it is used directly", func(t *testing.T) {
		task := &domain.TaskSpec{
			Resources: &domain.Resources{InstanceType: "c5.4xlarge"},
		}
		got := resolveInstanceType(task)
		if got != "c5.4xlarge" {
			t.Errorf("expected c5.4xlarge, got %s", got)
		}
	})

	t.Run("given accelerator but no instance type, when resolving, then accelerator mapping is used", func(t *testing.T) {
		task := &domain.TaskSpec{
			Resources: &domain.Resources{Accelerators: "H100:8"},
		}
		got := resolveInstanceType(task)
		if got != "p5.48xlarge" {
			t.Errorf("expected p5.48xlarge, got %s", got)
		}
	})

	t.Run("given no resources, when resolving, then default instance type is returned", func(t *testing.T) {
		task := &domain.TaskSpec{Resources: &domain.Resources{}}
		got := resolveInstanceType(task)
		if got != defaultInstanceType {
			t.Errorf("expected %s, got %s", defaultInstanceType, got)
		}
	})

	t.Run("given nil resources, when resolving, then default instance type is returned", func(t *testing.T) {
		task := &domain.TaskSpec{}
		got := resolveInstanceType(task)
		if got != defaultInstanceType {
			t.Errorf("expected %s, got %s", defaultInstanceType, got)
		}
	})
}

func TestPreferredRegion(t *testing.T) {
	t.Run("given cluster region set, when resolving, then cluster region is used", func(t *testing.T) {
		cluster := &domain.Cluster{Region: "eu-west-1"}
		got := preferredRegion(cluster, nil)
		if got != "eu-west-1" {
			t.Errorf("expected eu-west-1, got %s", got)
		}
	})

	t.Run("given no cluster region but task region set, when resolving, then task region is used", func(t *testing.T) {
		cluster := &domain.Cluster{}
		task := &domain.TaskSpec{Resources: &domain.Resources{Region: "ap-southeast-1"}}
		got := preferredRegion(cluster, task)
		if got != "ap-southeast-1" {
			t.Errorf("expected ap-southeast-1, got %s", got)
		}
	})

	t.Run("given no region anywhere, when resolving, then default region is returned", func(t *testing.T) {
		cluster := &domain.Cluster{}
		task := &domain.TaskSpec{}
		got := preferredRegion(cluster, task)
		if got != DefaultRegion {
			t.Errorf("expected %s, got %s", DefaultRegion, got)
		}
	})
}

func TestResolveDiskSize(t *testing.T) {
	t.Run("given disk size in resources, when resolving, then it is used", func(t *testing.T) {
		task := &domain.TaskSpec{Resources: &domain.Resources{DiskSizeGB: 500}}
		got := resolveDiskSize(task)
		if got != 500 {
			t.Errorf("expected 500, got %d", got)
		}
	})

	t.Run("given zero disk size, when resolving, then default is returned", func(t *testing.T) {
		task := &domain.TaskSpec{Resources: &domain.Resources{}}
		got := resolveDiskSize(task)
		if got != defaultDiskSizeGB {
			t.Errorf("expected %d, got %d", defaultDiskSizeGB, got)
		}
	})

	t.Run("given nil resources, when resolving, then default is returned", func(t *testing.T) {
		task := &domain.TaskSpec{}
		got := resolveDiskSize(task)
		if got != defaultDiskSizeGB {
			t.Errorf("expected %d, got %d", defaultDiskSizeGB, got)
		}
	})
}

func TestGenerateUserData(t *testing.T) {
	t.Run("given valid parameters, when generating user data, then script contains all required flags", func(t *testing.T) {
		script := generateUserData("https://broker.example.com", "test-cluster", "test-node-0", "secret-token")

		if !strings.HasPrefix(script, "#!/bin/bash") {
			t.Error("expected script to start with shebang")
		}
		if !strings.Contains(script, "--server 'wss://broker.example.com'") {
			t.Error("expected script to contain --server flag with websocket URL")
		}
		if !strings.Contains(script, "--cluster 'test-cluster'") {
			t.Error("expected script to contain --cluster flag with cluster name")
		}
		if !strings.Contains(script, "--token 'secret-token'") {
			t.Error("expected script to contain --token flag with token")
		}
		if !strings.Contains(script, "--node-id 'test-node-0'") {
			t.Error("expected script to contain --node-id flag with node ID")
		}
		if !strings.Contains(script, "--self-terminate-after") {
			t.Error("expected script to contain --self-terminate-after flag")
		}
		if !strings.Contains(script, "broker-agent") {
			t.Error("expected script to reference broker-agent binary")
		}
		if !strings.Contains(script, "systemctl") {
			t.Error("expected script to use systemctl to manage the service")
		}
		if !strings.Contains(script, agentBinaryPath) {
			t.Errorf("expected script to contain agent binary path %s", agentBinaryPath)
		}
	})

	t.Run("given special characters in cluster name, when generating user data, then they are included", func(t *testing.T) {
		script := generateUserData("https://broker.example.com", "my-cluster-123", "node-0", "tok")
		if !strings.Contains(script, "my-cluster-123") {
			t.Error("expected script to contain the cluster name")
		}
	})
}

func TestProviderName(t *testing.T) {
	t.Run("given aws provider, when calling Name, then aws is returned", func(t *testing.T) {
		p := New(nil, "", "")
		if p.Name() != domain.CloudAWS {
			t.Errorf("expected %s, got %s", domain.CloudAWS, p.Name())
		}
	})
}
