package domain

import "testing"

func TestTaskSpec_Validate(t *testing.T) {
	t.Run("given zero num_nodes, when validating, then it defaults to 1", func(t *testing.T) {
		spec := &TaskSpec{}
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if spec.NumNodes != 1 {
			t.Errorf("expected NumNodes 1, got %d", spec.NumNodes)
		}
	})

	t.Run("given negative num_nodes, when validating, then it defaults to 1", func(t *testing.T) {
		spec := &TaskSpec{NumNodes: -5}
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if spec.NumNodes != 1 {
			t.Errorf("expected NumNodes 1, got %d", spec.NumNodes)
		}
	})

	t.Run("given positive num_nodes, when validating, then it is preserved", func(t *testing.T) {
		spec := &TaskSpec{NumNodes: 4}
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if spec.NumNodes != 4 {
			t.Errorf("expected NumNodes 4, got %d", spec.NumNodes)
		}
	})

	t.Run("given nil resources, when validating, then resources is initialized", func(t *testing.T) {
		spec := &TaskSpec{}
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if spec.Resources == nil {
			t.Error("expected Resources to be non-nil")
		}
	})

	t.Run("given existing resources, when validating, then resources is preserved", func(t *testing.T) {
		r := &Resources{Cloud: CloudAWS}
		spec := &TaskSpec{NumNodes: 2, Resources: r}
		if err := spec.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if spec.Resources.Cloud != CloudAWS {
			t.Errorf("expected cloud aws, got %s", spec.Resources.Cloud)
		}
	})

	t.Run("given valid spec, when validating, then nil error is returned", func(t *testing.T) {
		spec := &TaskSpec{NumNodes: 1, Resources: &Resources{}}
		if err := spec.Validate(); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})
}
