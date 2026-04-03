package domain

import "testing"

func TestTaskSpec_ApplyDefaults(t *testing.T) {
	t.Run("given zero num_nodes, when applying defaults, then it defaults to 1", func(t *testing.T) {
		spec := &TaskSpec{}
		spec.ApplyDefaults()
		if spec.NumNodes != 1 {
			t.Errorf("expected NumNodes 1, got %d", spec.NumNodes)
		}
	})

	t.Run("given negative num_nodes, when applying defaults, then it defaults to 1", func(t *testing.T) {
		spec := &TaskSpec{NumNodes: -5}
		spec.ApplyDefaults()
		if spec.NumNodes != 1 {
			t.Errorf("expected NumNodes 1, got %d", spec.NumNodes)
		}
	})

	t.Run("given positive num_nodes, when applying defaults, then it is preserved", func(t *testing.T) {
		spec := &TaskSpec{NumNodes: 4}
		spec.ApplyDefaults()
		if spec.NumNodes != 4 {
			t.Errorf("expected NumNodes 4, got %d", spec.NumNodes)
		}
	})

	t.Run("given nil resources, when applying defaults, then resources is initialized", func(t *testing.T) {
		spec := &TaskSpec{}
		spec.ApplyDefaults()
		if spec.Resources == nil {
			t.Error("expected Resources to be non-nil")
		}
	})

	t.Run("given existing resources, when applying defaults, then resources is preserved", func(t *testing.T) {
		r := &Resources{Cloud: CloudAWS}
		spec := &TaskSpec{NumNodes: 2, Resources: r}
		spec.ApplyDefaults()
		if spec.Resources.Cloud != CloudAWS {
			t.Errorf("expected cloud aws, got %s", spec.Resources.Cloud)
		}
	})
}
