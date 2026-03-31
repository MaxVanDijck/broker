package domain

import "testing"

func TestResources_InfraString(t *testing.T) {
	t.Run("given no cloud set, when calling InfraString, then 'any' is returned", func(t *testing.T) {
		r := Resources{}
		if got := r.InfraString(); got != "any" {
			t.Errorf("expected 'any', got %q", got)
		}
	})

	t.Run("given cloud only, when calling InfraString, then cloud name is returned", func(t *testing.T) {
		r := Resources{Cloud: CloudAWS}
		if got := r.InfraString(); got != "aws" {
			t.Errorf("expected 'aws', got %q", got)
		}
	})

	t.Run("given cloud and region, when calling InfraString, then cloud/region is returned", func(t *testing.T) {
		r := Resources{Cloud: CloudGCP, Region: "us-central1"}
		if got := r.InfraString(); got != "gcp/us-central1" {
			t.Errorf("expected 'gcp/us-central1', got %q", got)
		}
	})

	t.Run("given cloud region and zone, when calling InfraString, then cloud/region/zone is returned", func(t *testing.T) {
		r := Resources{Cloud: CloudAzure, Region: "eastus", Zone: "1"}
		if got := r.InfraString(); got != "azure/eastus/1" {
			t.Errorf("expected 'azure/eastus/1', got %q", got)
		}
	})

	t.Run("given cloud and zone but no region, when calling InfraString, then cloud/zone is returned", func(t *testing.T) {
		r := Resources{Cloud: CloudAWS, Zone: "us-east-1a"}
		if got := r.InfraString(); got != "aws/us-east-1a" {
			t.Errorf("expected 'aws/us-east-1a', got %q", got)
		}
	})
}

func TestResources_String(t *testing.T) {
	t.Run("given no fields set, when calling String, then 'any' is returned", func(t *testing.T) {
		r := Resources{}
		if got := r.String(); got != "any" {
			t.Errorf("expected 'any', got %q", got)
		}
	})

	t.Run("given cloud and instance type, when calling String, then both are included", func(t *testing.T) {
		r := Resources{Cloud: CloudAWS, Region: "us-east-1", InstanceType: "p3.2xlarge"}
		expected := "aws/us-east-1, p3.2xlarge"
		if got := r.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("given cloud and accelerators, when calling String, then both are included", func(t *testing.T) {
		r := Resources{Cloud: CloudGCP, Accelerators: "A100:4"}
		expected := "gcp, A100:4"
		if got := r.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})

	t.Run("given cloud instance type and accelerators, when calling String, then all are included", func(t *testing.T) {
		r := Resources{Cloud: CloudAWS, InstanceType: "p4d.24xlarge", Accelerators: "A100:8"}
		expected := "aws, p4d.24xlarge, A100:8"
		if got := r.String(); got != expected {
			t.Errorf("expected %q, got %q", expected, got)
		}
	})
}
