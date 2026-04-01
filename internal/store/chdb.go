//go:build chdb

package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chdb-io/chdb-go/chdb"
)

func init() {
	RegisterAnalyticsBackend("chdb", func(dsn string) (AnalyticsStore, error) {
		return NewChDB(dsn)
	})
}

type ChDBStore struct {
	session *chdb.Session
}

func NewChDB(dataDir string) (*ChDBStore, error) {
	session, err := chdb.NewSession(dataDir)
	if err != nil {
		return nil, fmt.Errorf("chdb session: %w", err)
	}

	s := &ChDBStore{session: session}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ChDBStore) migrate() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS logs (
			timestamp DateTime64(9),
			job_id String,
			node_id String,
			stream String,
			line String
		) ENGINE = MergeTree()
		ORDER BY (job_id, timestamp)`,

		`CREATE TABLE IF NOT EXISTS metrics (
			timestamp DateTime64(3),
			node_id String,
			cluster_id String,
			cpu_percent Float64,
			memory_percent Float64,
			disk_used_bytes Int64,
			gpu_index Int32,
			gpu_utilization Float64,
			gpu_memory_used Int64,
			gpu_temperature Float64
		) ENGINE = MergeTree()
		ORDER BY (node_id, timestamp)`,

		`CREATE TABLE IF NOT EXISTS costs (
			timestamp DateTime64(3),
			cluster_id String,
			cloud String,
			region String,
			instance_type String,
			node_id String,
			hourly_cost Float64,
			is_spot UInt8
		) ENGINE = MergeTree()
		ORDER BY (cluster_id, timestamp)`,
	}

	for _, q := range queries {
		if _, err := s.session.Query(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func (s *ChDBStore) InsertLogs(ctx context.Context, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("INSERT INTO logs (timestamp, job_id, node_id, stream, line) VALUES ")

	for i, e := range entries {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "(%d, '%s', '%s', '%s', '%s')",
			e.Timestamp.UnixNano(),
			escape(e.JobID),
			escape(e.NodeID),
			escape(e.Stream),
			escape(string(e.Line)),
		)
	}

	_, err := s.session.Query(b.String())
	return err
}

func (s *ChDBStore) QueryLogs(ctx context.Context, jobID string, tr TimeRange, limit int) ([]LogEntry, error) {
	q := fmt.Sprintf(
		`SELECT timestamp, job_id, node_id, stream, line FROM logs
		 WHERE job_id = '%s' AND timestamp >= %d AND timestamp <= %d
		 ORDER BY timestamp ASC LIMIT %d`,
		escape(jobID),
		tr.From.UnixNano(),
		tr.To.UnixNano(),
		limit,
	)

	result, err := s.session.Query(q)
	if err != nil {
		return nil, err
	}

	var entries []LogEntry
	for _, line := range strings.Split(result.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 5)
		if len(parts) < 5 {
			continue
		}
		ts, _ := time.Parse(time.RFC3339Nano, parts[0])
		entries = append(entries, LogEntry{
			Timestamp: ts,
			JobID:     parts[1],
			NodeID:    parts[2],
			Stream:    parts[3],
			Line:      []byte(parts[4]),
		})
	}
	return entries, nil
}

func (s *ChDBStore) InsertMetrics(ctx context.Context, points []MetricPoint) error {
	if len(points) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("INSERT INTO metrics VALUES ")

	for i, p := range points {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "(%d, '%s', '%s', %f, %f, %d, %d, %f, %d, %f)",
			p.Timestamp.UnixMilli(),
			escape(p.NodeID),
			escape(p.ClusterID),
			p.CPUPercent,
			p.MemoryPercent,
			p.DiskUsedBytes,
			p.GPUIndex,
			p.GPUUtilization,
			p.GPUMemoryUsed,
			p.GPUTemperature,
		)
	}

	_, err := s.session.Query(b.String())
	return err
}

func (s *ChDBStore) QueryMetrics(ctx context.Context, nodeID string, tr TimeRange) ([]MetricPoint, error) {
	return nil, nil
}

func (s *ChDBStore) QueryMetricsByCluster(ctx context.Context, clusterID string, tr TimeRange) ([]MetricPoint, error) {
	q := fmt.Sprintf(
		`SELECT timestamp, node_id, cluster_id, cpu_percent, memory_percent,
		        disk_used_bytes, gpu_index, gpu_utilization, gpu_memory_used, gpu_temperature
		 FROM metrics
		 WHERE cluster_id = '%s' AND timestamp >= %d AND timestamp <= %d
		 ORDER BY timestamp ASC`,
		escape(clusterID),
		tr.From.UnixMilli(),
		tr.To.UnixMilli(),
	)

	result, err := s.session.Query(q)
	if err != nil {
		return nil, err
	}

	var points []MetricPoint
	for _, line := range strings.Split(result.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 10)
		if len(parts) < 10 {
			continue
		}
		ts, _ := time.Parse(time.RFC3339Nano, parts[0])
		var p MetricPoint
		p.Timestamp = ts
		p.NodeID = parts[1]
		p.ClusterID = parts[2]
		fmt.Sscanf(parts[3], "%f", &p.CPUPercent)
		fmt.Sscanf(parts[4], "%f", &p.MemoryPercent)
		fmt.Sscanf(parts[5], "%d", &p.DiskUsedBytes)
		fmt.Sscanf(parts[6], "%d", &p.GPUIndex)
		fmt.Sscanf(parts[7], "%f", &p.GPUUtilization)
		fmt.Sscanf(parts[8], "%d", &p.GPUMemoryUsed)
		fmt.Sscanf(parts[9], "%f", &p.GPUTemperature)
		points = append(points, p)
	}
	return points, nil
}

func (s *ChDBStore) InsertCostEvent(ctx context.Context, event CostEvent) error {
	q := fmt.Sprintf(
		`INSERT INTO costs VALUES (%d, '%s', '%s', '%s', '%s', '%s', %f, %d)`,
		event.Timestamp.UnixMilli(),
		escape(event.ClusterID),
		escape(event.Cloud),
		escape(event.Region),
		escape(event.InstanceType),
		escape(event.NodeID),
		event.HourlyCost,
		boolToInt(event.IsSpot),
	)

	_, err := s.session.Query(q)
	return err
}

func (s *ChDBStore) QueryCosts(ctx context.Context, clusterID string, tr TimeRange) ([]CostEvent, error) {
	return nil, nil
}

func (s *ChDBStore) TotalCost(ctx context.Context, clusterID string, tr TimeRange) (float64, error) {
	q := fmt.Sprintf(
		`SELECT sum(hourly_cost) FROM costs
		 WHERE cluster_id = '%s' AND timestamp >= %d AND timestamp <= %d`,
		escape(clusterID),
		tr.From.UnixMilli(),
		tr.To.UnixMilli(),
	)

	result, err := s.session.Query(q)
	if err != nil {
		return 0, err
	}

	val := strings.TrimSpace(result.String())
	if val == "" {
		return 0, nil
	}
	var total float64
	fmt.Sscanf(val, "%f", &total)
	return total, nil
}

func (s *ChDBStore) Close() error {
	s.session.Cleanup()
	return nil
}
