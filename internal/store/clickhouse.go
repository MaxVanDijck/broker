package store

import (
	"context"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type ClickHouseStore struct {
	conn driver.Conn
}

func NewClickHouse(dsn string) (*ClickHouseStore, error) {
	opts, err := clickhouse.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse clickhouse dsn: %w", err)
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}

	s := &ClickHouseStore{conn: conn}
	if err := s.migrate(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ClickHouseStore) migrate(ctx context.Context) error {
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
		if err := s.conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func (s *ClickHouseStore) InsertLogs(ctx context.Context, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO logs")
	if err != nil {
		return err
	}

	for _, e := range entries {
		if err := batch.Append(e.Timestamp, e.JobID, e.NodeID, e.Stream, string(e.Line)); err != nil {
			return err
		}
	}

	return batch.Send()
}

func (s *ClickHouseStore) QueryLogs(ctx context.Context, jobID string, tr TimeRange, limit int) ([]LogEntry, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT timestamp, job_id, node_id, stream, line FROM logs
		 WHERE job_id = $1 AND timestamp >= $2 AND timestamp <= $3
		 ORDER BY timestamp ASC LIMIT $4`,
		jobID, tr.From, tr.To, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var line string
		if err := rows.Scan(&e.Timestamp, &e.JobID, &e.NodeID, &e.Stream, &line); err != nil {
			return nil, err
		}
		e.Line = []byte(line)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *ClickHouseStore) InsertMetrics(ctx context.Context, points []MetricPoint) error {
	if len(points) == 0 {
		return nil
	}

	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO metrics")
	if err != nil {
		return err
	}

	for _, p := range points {
		if err := batch.Append(
			p.Timestamp, p.NodeID, p.ClusterID,
			p.CPUPercent, p.MemoryPercent, p.DiskUsedBytes,
			p.GPUIndex, p.GPUUtilization, p.GPUMemoryUsed, p.GPUTemperature,
		); err != nil {
			return err
		}
	}

	return batch.Send()
}

func (s *ClickHouseStore) QueryMetrics(ctx context.Context, nodeID string, tr TimeRange) ([]MetricPoint, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT timestamp, node_id, cluster_id, cpu_percent, memory_percent,
		        disk_used_bytes, gpu_index, gpu_utilization, gpu_memory_used, gpu_temperature
		 FROM metrics
		 WHERE node_id = $1 AND timestamp >= $2 AND timestamp <= $3
		 ORDER BY timestamp ASC`,
		nodeID, tr.From, tr.To,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []MetricPoint
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(
			&p.Timestamp, &p.NodeID, &p.ClusterID,
			&p.CPUPercent, &p.MemoryPercent, &p.DiskUsedBytes,
			&p.GPUIndex, &p.GPUUtilization, &p.GPUMemoryUsed, &p.GPUTemperature,
		); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *ClickHouseStore) QueryMetricsByCluster(ctx context.Context, clusterID string, tr TimeRange) ([]MetricPoint, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT timestamp, node_id, cluster_id, cpu_percent, memory_percent,
		        disk_used_bytes, gpu_index, gpu_utilization, gpu_memory_used, gpu_temperature
		 FROM metrics
		 WHERE cluster_id = $1 AND timestamp >= $2 AND timestamp <= $3
		 ORDER BY timestamp ASC`,
		clusterID, tr.From, tr.To,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []MetricPoint
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(
			&p.Timestamp, &p.NodeID, &p.ClusterID,
			&p.CPUPercent, &p.MemoryPercent, &p.DiskUsedBytes,
			&p.GPUIndex, &p.GPUUtilization, &p.GPUMemoryUsed, &p.GPUTemperature,
		); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *ClickHouseStore) InsertCostEvent(ctx context.Context, event CostEvent) error {
	return s.conn.Exec(ctx,
		"INSERT INTO costs VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
		event.Timestamp, event.ClusterID, event.Cloud, event.Region,
		event.InstanceType, event.NodeID, event.HourlyCost, boolToUint8(event.IsSpot),
	)
}

func (s *ClickHouseStore) QueryCosts(ctx context.Context, clusterID string, tr TimeRange) ([]CostEvent, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT timestamp, cluster_id, cloud, region, instance_type, node_id, hourly_cost, is_spot
		 FROM costs
		 WHERE cluster_id = $1 AND timestamp >= $2 AND timestamp <= $3
		 ORDER BY timestamp ASC`,
		clusterID, tr.From, tr.To,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []CostEvent
	for rows.Next() {
		var e CostEvent
		var isSpot uint8
		if err := rows.Scan(&e.Timestamp, &e.ClusterID, &e.Cloud, &e.Region,
			&e.InstanceType, &e.NodeID, &e.HourlyCost, &isSpot); err != nil {
			return nil, err
		}
		e.IsSpot = isSpot == 1
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *ClickHouseStore) TotalCost(ctx context.Context, clusterID string, tr TimeRange) (float64, error) {
	var total float64
	err := s.conn.QueryRow(ctx,
		`SELECT sum(hourly_cost) FROM costs
		 WHERE cluster_id = $1 AND timestamp >= $2 AND timestamp <= $3`,
		clusterID, tr.From, tr.To,
	).Scan(&total)
	return total, err
}

func (s *ClickHouseStore) Close() error {
	return s.conn.Close()
}

// compile-time checks
var _ AnalyticsStore = (*ClickHouseStore)(nil)
