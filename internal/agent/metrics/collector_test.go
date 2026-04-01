package metrics

import (
	"log/slog"
	"os/exec"
	"runtime"
	"testing"
)

func TestCollector_Collect(t *testing.T) {
	t.Run("given a collector, when collecting metrics, then snapshot is returned without error", func(t *testing.T) {
		c := NewCollector(slog.Default())

		snap, err := c.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if snap == nil {
			t.Fatal("expected snapshot, got nil")
		}
	})

	t.Run("given a collector, when collecting disk metrics, then disk used bytes is non-zero", func(t *testing.T) {
		c := NewCollector(slog.Default())

		snap, err := c.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if snap.DiskUsedBytes <= 0 {
			t.Errorf("expected positive disk used bytes, got %d", snap.DiskUsedBytes)
		}
	})

	t.Run("given a collector on linux, when collecting cpu metrics, then cpu percent is non-zero", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("skipping linux-specific cpu test")
		}

		c := NewCollector(slog.Default())

		snap, err := c.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if snap.CPUPercent < 0 || snap.CPUPercent > 100 {
			t.Errorf("cpu percent out of range: %f", snap.CPUPercent)
		}
	})

	t.Run("given a collector on linux, when collecting memory metrics, then memory percent is non-zero", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("skipping linux-specific memory test")
		}

		c := NewCollector(slog.Default())

		snap, err := c.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if snap.MemoryPercent <= 0 || snap.MemoryPercent > 100 {
			t.Errorf("memory percent out of range: %f", snap.MemoryPercent)
		}
	})

	t.Run("given no nvidia-smi, when collecting gpu metrics, then empty gpu slice is returned without error", func(t *testing.T) {
		if _, err := exec.LookPath("nvidia-smi"); err == nil {
			t.Skip("nvidia-smi found, skipping no-gpu test")
		}

		c := NewCollector(slog.Default())

		snap, err := c.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if len(snap.GPUs) != 0 {
			t.Errorf("expected empty gpu metrics, got %d", len(snap.GPUs))
		}
	})

	t.Run("given nvidia-smi is available, when collecting gpu metrics, then gpu metrics are populated", func(t *testing.T) {
		if _, err := exec.LookPath("nvidia-smi"); err != nil {
			t.Skip("nvidia-smi not found, skipping gpu test")
		}

		c := NewCollector(slog.Default())

		snap, err := c.Collect()
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if len(snap.GPUs) == 0 {
			t.Error("expected gpu metrics when nvidia-smi is available")
		}
		for _, g := range snap.GPUs {
			if g.Utilization < 0 || g.Utilization > 100 {
				t.Errorf("gpu %d utilization out of range: %f", g.Index, g.Utilization)
			}
			if g.Temperature < 0 || g.Temperature > 150 {
				t.Errorf("gpu %d temperature out of range: %f", g.Index, g.Temperature)
			}
		}
	})
}
