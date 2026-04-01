package optimizer

import (
	"testing"
)

func TestOptimize_GPURequirements(t *testing.T) {
	t.Run("given T4:1 requirement, when optimizing, then cheapest T4 instance is first", func(t *testing.T) {
		recs, err := Optimize(Requirements{Accelerators: "T4:1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recs) == 0 {
			t.Fatal("expected at least one recommendation")
		}
		if recs[0].GPUModel != "T4" {
			t.Errorf("expected GPU model T4, got %s", recs[0].GPUModel)
		}
		if recs[0].GPUCount < 1 {
			t.Errorf("expected at least 1 GPU, got %d", recs[0].GPUCount)
		}
		if recs[0].InstanceType != "g4dn.xlarge" {
			t.Errorf("expected g4dn.xlarge as cheapest T4 instance, got %s", recs[0].InstanceType)
		}
	})

	t.Run("given T4:4 requirement, when optimizing, then only instances with 4+ T4 GPUs are returned", func(t *testing.T) {
		recs, err := Optimize(Requirements{Accelerators: "T4:4"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recs) == 0 {
			t.Fatal("expected at least one recommendation")
		}
		for _, r := range recs {
			if r.GPUCount < 4 {
				t.Errorf("expected at least 4 GPUs, got %d for %s", r.GPUCount, r.InstanceType)
			}
			if r.GPUModel != "T4" {
				t.Errorf("expected GPU model T4, got %s for %s", r.GPUModel, r.InstanceType)
			}
		}
	})

	t.Run("given H100:8 requirement, when optimizing, then p5.48xlarge is returned", func(t *testing.T) {
		recs, err := Optimize(Requirements{Accelerators: "H100:8"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recs) == 0 {
			t.Fatal("expected at least one recommendation")
		}
		if recs[0].InstanceType != "p5.48xlarge" {
			t.Errorf("expected p5.48xlarge, got %s", recs[0].InstanceType)
		}
	})

	t.Run("given A100:8 requirement, when optimizing, then p4d.24xlarge is first", func(t *testing.T) {
		recs, err := Optimize(Requirements{Accelerators: "A100:8"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recs) == 0 {
			t.Fatal("expected at least one recommendation")
		}
		if recs[0].InstanceType != "p4d.24xlarge" {
			t.Errorf("expected p4d.24xlarge, got %s", recs[0].InstanceType)
		}
	})
}

func TestOptimize_CPUAndMemoryRequirements(t *testing.T) {
	t.Run("given T4:1 with 16+ cpus, when optimizing, then instances have at least 16 vcpus", func(t *testing.T) {
		recs, err := Optimize(Requirements{Accelerators: "T4:1", CPUs: "16+"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recs) == 0 {
			t.Fatal("expected at least one recommendation")
		}
		for _, r := range recs {
			if r.VCPUs < 16 {
				t.Errorf("expected at least 16 vcpus, got %d for %s", r.VCPUs, r.InstanceType)
			}
		}
	})

	t.Run("given T4:1 with 64+ memory, when optimizing, then instances have at least 64 GB", func(t *testing.T) {
		recs, err := Optimize(Requirements{Accelerators: "T4:1", Memory: "64+"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recs) == 0 {
			t.Fatal("expected at least one recommendation")
		}
		for _, r := range recs {
			if r.MemoryGB < 64 {
				t.Errorf("expected at least 64 GB memory, got %.0f GB for %s", r.MemoryGB, r.InstanceType)
			}
		}
	})
}

func TestOptimize_SpotPricing(t *testing.T) {
	t.Run("given T4:1 with spot, when optimizing, then prices are spot-discounted", func(t *testing.T) {
		onDemand, err := Optimize(Requirements{Accelerators: "T4:1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		spot, err := Optimize(Requirements{Accelerators: "T4:1", UseSpot: true})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(onDemand) == 0 || len(spot) == 0 {
			t.Fatal("expected recommendations for both on-demand and spot")
		}

		if !spot[0].IsSpot {
			t.Error("expected spot recommendation to have IsSpot=true")
		}

		if spot[0].HourlyPrice >= onDemand[0].HourlyPrice {
			t.Errorf("expected spot price ($%.4f) to be less than on-demand ($%.4f)",
				spot[0].HourlyPrice, onDemand[0].HourlyPrice)
		}
	})
}

func TestOptimize_NoMatch(t *testing.T) {
	t.Run("given unknown GPU, when optimizing, then error is returned", func(t *testing.T) {
		_, err := Optimize(Requirements{Accelerators: "TPUv5:1"})
		if err == nil {
			t.Fatal("expected error for unknown GPU type")
		}
	})
}

func TestOptimize_NoGPU(t *testing.T) {
	t.Run("given only cpu requirements, when optimizing, then non-GPU instances are included", func(t *testing.T) {
		recs, err := Optimize(Requirements{CPUs: "2", Memory: "4"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recs) == 0 {
			t.Fatal("expected at least one recommendation")
		}
		if recs[0].HourlyPrice <= 0 {
			t.Errorf("expected positive price, got %f", recs[0].HourlyPrice)
		}
	})
}

func TestOptimize_MaxResults(t *testing.T) {
	t.Run("given broad requirements, when optimizing, then at most 5 results are returned", func(t *testing.T) {
		recs, err := Optimize(Requirements{Accelerators: "T4:1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(recs) > 5 {
			t.Errorf("expected at most 5 recommendations, got %d", len(recs))
		}
	})
}

func TestOptimize_SortedByPrice(t *testing.T) {
	t.Run("given T4:1, when optimizing, then results are sorted by price ascending", func(t *testing.T) {
		recs, err := Optimize(Requirements{Accelerators: "T4:1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		for i := 1; i < len(recs); i++ {
			if recs[i].HourlyPrice < recs[i-1].HourlyPrice {
				t.Errorf("results not sorted: %s ($%.4f) < %s ($%.4f)",
					recs[i].InstanceType, recs[i].HourlyPrice,
					recs[i-1].InstanceType, recs[i-1].HourlyPrice)
			}
		}
	})
}

func TestParseAccelerators(t *testing.T) {
	t.Run("given empty string, when parsing, then zero values", func(t *testing.T) {
		model, count, err := parseAccelerators("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model != "" || count != 0 {
			t.Errorf("expected empty/zero, got model=%q count=%d", model, count)
		}
	})

	t.Run("given T4 without count, when parsing, then count defaults to 1", func(t *testing.T) {
		model, count, err := parseAccelerators("T4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model != "T4" || count != 1 {
			t.Errorf("expected T4/1, got %s/%d", model, count)
		}
	})

	t.Run("given A100:8, when parsing, then model and count are extracted", func(t *testing.T) {
		model, count, err := parseAccelerators("A100:8")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model != "A100" || count != 8 {
			t.Errorf("expected A100/8, got %s/%d", model, count)
		}
	})

	t.Run("given lowercase a10g:4, when parsing, then model is uppercased", func(t *testing.T) {
		model, count, err := parseAccelerators("a10g:4")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if model != "A10G" || count != 4 {
			t.Errorf("expected A10G/4, got %s/%d", model, count)
		}
	})

	t.Run("given invalid count, when parsing, then error is returned", func(t *testing.T) {
		_, _, err := parseAccelerators("T4:abc")
		if err == nil {
			t.Fatal("expected error for invalid count")
		}
	})
}

func TestParseResourceRequirement(t *testing.T) {
	t.Run("given empty string, when parsing, then zero", func(t *testing.T) {
		v, err := parseResourceRequirement("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != 0 {
			t.Errorf("expected 0, got %d", v)
		}
	})

	t.Run("given 16, when parsing, then 16", func(t *testing.T) {
		v, err := parseResourceRequirement("16")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != 16 {
			t.Errorf("expected 16, got %d", v)
		}
	})

	t.Run("given 32+, when parsing, then 32", func(t *testing.T) {
		v, err := parseResourceRequirement("32+")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != 32 {
			t.Errorf("expected 32, got %d", v)
		}
	})
}
