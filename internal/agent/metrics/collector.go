package metrics

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Snapshot struct {
	CPUPercent    float64
	MemoryPercent float64
	DiskUsedBytes int64
	GPUs          []GPUMetrics
}

type GPUMetrics struct {
	Index       int32
	Utilization float64
	MemoryUsed  int64
	Temperature float64
}

type Collector struct {
	logger    *slog.Logger
	prevIdle  uint64
	prevTotal uint64
}

func NewCollector(logger *slog.Logger) *Collector {
	return &Collector{logger: logger}
}

func (c *Collector) Collect() (*Snapshot, error) {
	snap := &Snapshot{}

	cpu, err := c.collectCPU()
	if err != nil {
		c.logger.Warn("failed to collect cpu metrics, using fallback", "error", err)
		cpu = c.collectCPUFallback()
	}
	snap.CPUPercent = cpu

	mem, err := c.collectMemory()
	if err != nil {
		c.logger.Warn("failed to collect memory metrics, using fallback", "error", err)
		mem = c.collectMemoryFallback()
	}
	snap.MemoryPercent = mem

	disk, err := c.collectDisk()
	if err != nil {
		c.logger.Warn("failed to collect disk metrics", "error", err)
	}
	snap.DiskUsedBytes = disk

	gpus, err := c.collectGPU()
	if err != nil {
		c.logger.Debug("gpu metrics unavailable", "error", err)
	}
	snap.GPUs = gpus

	return snap, nil
}

func (c *Collector) collectCPU() (float64, error) {
	if runtime.GOOS != "linux" {
		return 0, fmt.Errorf("proc/stat not available on %s", runtime.GOOS)
	}

	idle, total, err := readProcStat()
	if err != nil {
		return 0, err
	}

	if c.prevTotal == 0 {
		c.prevIdle = idle
		c.prevTotal = total
		time.Sleep(100 * time.Millisecond)
		idle, total, err = readProcStat()
		if err != nil {
			return 0, err
		}
	}

	deltaIdle := idle - c.prevIdle
	deltaTotal := total - c.prevTotal

	c.prevIdle = idle
	c.prevTotal = total

	if deltaTotal == 0 {
		return 0, nil
	}

	return (1.0 - float64(deltaIdle)/float64(deltaTotal)) * 100.0, nil
}

func readProcStat() (idle, total uint64, err error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0, 0, fmt.Errorf("unexpected /proc/stat format: %s", line)
		}

		var sum uint64
		for _, field := range fields[1:] {
			val, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return 0, 0, fmt.Errorf("parse /proc/stat field %q: %w", field, err)
			}
			sum += val
		}

		idleVal, err := strconv.ParseUint(fields[4], 10, 64)
		if err != nil {
			return 0, 0, fmt.Errorf("parse idle field: %w", err)
		}

		return idleVal, sum, nil
	}

	return 0, 0, fmt.Errorf("/proc/stat: cpu line not found")
}

func (c *Collector) collectCPUFallback() float64 {
	numCPU := runtime.NumCPU()
	before := new(runtime.MemStats)
	runtime.ReadMemStats(before)
	start := time.Now()

	time.Sleep(50 * time.Millisecond)

	after := new(runtime.MemStats)
	runtime.ReadMemStats(after)
	elapsed := time.Since(start)

	gcTime := time.Duration(after.PauseTotalNs-before.PauseTotalNs) * time.Nanosecond
	if elapsed == 0 {
		return 0
	}
	percent := (float64(gcTime) / float64(elapsed)) / float64(numCPU) * 100.0
	if percent > 100 {
		percent = 100
	}
	return percent
}

func (c *Collector) collectMemory() (float64, error) {
	if runtime.GOOS != "linux" {
		return 0, fmt.Errorf("proc/meminfo not available on %s", runtime.GOOS)
	}

	return readProcMeminfo()
}

func readProcMeminfo() (float64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var memTotal, memAvailable uint64
	var foundTotal, foundAvailable bool

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "MemTotal:") {
			val, err := parseMemInfoLine(line)
			if err != nil {
				return 0, err
			}
			memTotal = val
			foundTotal = true
		} else if strings.HasPrefix(line, "MemAvailable:") {
			val, err := parseMemInfoLine(line)
			if err != nil {
				return 0, err
			}
			memAvailable = val
			foundAvailable = true
		}

		if foundTotal && foundAvailable {
			break
		}
	}

	if !foundTotal || !foundAvailable {
		return 0, fmt.Errorf("/proc/meminfo: missing MemTotal or MemAvailable")
	}

	if memTotal == 0 {
		return 0, nil
	}

	used := memTotal - memAvailable
	return float64(used) / float64(memTotal) * 100.0, nil
}

func parseMemInfoLine(line string) (uint64, error) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0, fmt.Errorf("unexpected meminfo line: %s", line)
	}
	val, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse meminfo value %q: %w", fields[1], err)
	}
	return val * 1024, nil
}

func (c *Collector) collectMemoryFallback() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	if m.Sys == 0 {
		return 0
	}
	return float64(m.Alloc) / float64(m.Sys) * 100.0
}

func (c *Collector) collectDisk() (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, fmt.Errorf("statfs /: %w", err)
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bfree * uint64(stat.Bsize)
	used := total - free

	return int64(used), nil
}

func (c *Collector) collectGPU() ([]GPUMetrics, error) {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil, nil
	}

	out, err := exec.Command(path,
		"--query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("nvidia-smi: %w", err)
	}

	var gpus []GPUMetrics
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) < 5 {
			c.logger.Warn("unexpected nvidia-smi output", "line", line)
			continue
		}

		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}

		idx, err := strconv.ParseInt(fields[0], 10, 32)
		if err != nil {
			continue
		}
		util, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			continue
		}
		memUsedMiB, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			continue
		}
		temp, err := strconv.ParseFloat(fields[4], 64)
		if err != nil {
			continue
		}

		gpus = append(gpus, GPUMetrics{
			Index:       int32(idx),
			Utilization: util,
			MemoryUsed:  int64(memUsedMiB * 1024 * 1024),
			Temperature: temp,
		})
	}

	return gpus, nil
}
