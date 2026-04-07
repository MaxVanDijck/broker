package domain

import "testing"

func TestResources_String(t *testing.T) {
	t.Run("given no fields set, when calling String, then '-' is returned", func(t *testing.T) {
		r := Resources{}
		if got := r.String(); got != "-" {
			t.Errorf("expected '-', got %q", got)
		}
	})

	t.Run("given accelerators, when calling String, then accelerators are returned", func(t *testing.T) {
		r := Resources{Cloud: CloudAWS, Accelerators: "A100:8"}
		if got := r.String(); got != "A100:8" {
			t.Errorf("expected 'A100:8', got %q", got)
		}
	})

	t.Run("given instance type only, when calling String, then instance type is returned", func(t *testing.T) {
		r := Resources{Cloud: CloudAWS, InstanceType: "p4d.24xlarge"}
		if got := r.String(); got != "p4d.24xlarge" {
			t.Errorf("expected 'p4d.24xlarge', got %q", got)
		}
	})

	t.Run("given both accelerators and instance type, when calling String, then accelerators take priority", func(t *testing.T) {
		r := Resources{InstanceType: "p4d.24xlarge", Accelerators: "A100:8"}
		if got := r.String(); got != "A100:8" {
			t.Errorf("expected 'A100:8', got %q", got)
		}
	})
}
