package optimizer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"broker/internal/provider/aws"
)

type Requirements struct {
	Accelerators string // e.g. "A100:8", "T4:1", "H100:8"
	CPUs         string // e.g. "4", "16+"
	Memory       string // e.g. "32", "64+" (in GB)
	UseSpot      bool
}

type Recommendation struct {
	InstanceType string
	Region       string
	HourlyPrice  float64
	IsSpot       bool
	GPUCount     int
	GPUModel     string
	VCPUs        int
	MemoryGB     float64
}

const (
	// SpotDiscount is the estimated spot price as a fraction of on-demand.
	// 0.35 means spot costs ~35% of on-demand (i.e. ~65% savings).
	SpotDiscount = 0.35

	maxResults = 5
)

func Optimize(reqs Requirements) ([]Recommendation, error) {
	gpuModel, gpuCount, err := parseAccelerators(reqs.Accelerators)
	if err != nil {
		return nil, err
	}

	minCPUs, err := parseResourceRequirement(reqs.CPUs)
	if err != nil {
		return nil, fmt.Errorf("invalid cpus %q: %w", reqs.CPUs, err)
	}

	minMemoryGB, err := parseResourceRequirement(reqs.Memory)
	if err != nil {
		return nil, fmt.Errorf("invalid memory %q: %w", reqs.Memory, err)
	}
	minMemoryMB := minMemoryGB * 1024

	var candidates []Recommendation

	for _, spec := range aws.InstanceCatalog {
		if !matchesGPU(spec, gpuModel, gpuCount) {
			continue
		}

		if minCPUs > 0 && spec.VCPUs < minCPUs {
			continue
		}

		if minMemoryMB > 0 && spec.MemoryMB < minMemoryMB {
			continue
		}

		price, ok := aws.OnDemandPricing[spec.InstanceType]
		if !ok {
			continue
		}

		if reqs.UseSpot {
			price *= SpotDiscount
		}

		candidates = append(candidates, Recommendation{
			InstanceType: spec.InstanceType,
			Region:       "us-east-1",
			HourlyPrice:  price,
			IsSpot:       reqs.UseSpot,
			GPUCount:     spec.GPUCount,
			GPUModel:     spec.GPUModel,
			VCPUs:        spec.VCPUs,
			MemoryGB:     float64(spec.MemoryMB) / 1024.0,
		})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no instance type matches requirements: accelerators=%q cpus=%q memory=%q",
			reqs.Accelerators, reqs.CPUs, reqs.Memory)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].HourlyPrice < candidates[j].HourlyPrice
	})

	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}

	return candidates, nil
}

func parseAccelerators(s string) (model string, count int, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", 0, nil
	}

	parts := strings.SplitN(s, ":", 2)
	model = strings.ToUpper(strings.TrimSpace(parts[0]))

	if canonical, ok := aws.GPUModelAliases[model]; ok {
		model = canonical
	}

	count = 1
	if len(parts) == 2 {
		count, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return "", 0, fmt.Errorf("invalid gpu count in %q: %w", s, err)
		}
	}

	return model, count, nil
}

func parseResourceRequirement(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}

	s = strings.TrimSuffix(s, "+")
	s = strings.TrimSpace(s)

	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q: %w", s, err)
	}
	return v, nil
}

func matchesGPU(spec aws.InstanceSpec, requiredModel string, requiredCount int) bool {
	if requiredModel == "" {
		return true
	}

	if spec.GPUModel == "" || spec.GPUCount == 0 {
		return false
	}

	if !strings.EqualFold(spec.GPUModel, requiredModel) {
		return false
	}

	return spec.GPUCount >= requiredCount
}
