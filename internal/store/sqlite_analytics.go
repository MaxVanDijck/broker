package store

import (
	"context"
	"database/sql"
	"time"
)

type SQLiteAnalyticsStore struct {
	db *sql.DB
}

func NewSQLiteAnalytics(path string) (*SQLiteAnalyticsStore, error) {
	db, err := sql.Open("sqlite3", path+"?_journal_mode=WAL")
	if err != nil {
		return nil, err
	}

	s := &SQLiteAnalyticsStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteAnalyticsStore) migrate() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS metrics (
			timestamp INTEGER NOT NULL,
			node_id TEXT NOT NULL,
			cluster_id TEXT NOT NULL,
			cpu_percent REAL NOT NULL DEFAULT 0,
			memory_percent REAL NOT NULL DEFAULT 0,
			disk_used_bytes INTEGER NOT NULL DEFAULT 0,
			gpu_index INTEGER NOT NULL DEFAULT 0,
			gpu_utilization REAL NOT NULL DEFAULT 0,
			gpu_memory_used INTEGER NOT NULL DEFAULT 0,
			gpu_temperature REAL NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_metrics_cluster_ts ON metrics(cluster_id, timestamp);
		CREATE INDEX IF NOT EXISTS idx_metrics_node_ts ON metrics(node_id, timestamp);

		CREATE TABLE IF NOT EXISTS logs (
			timestamp INTEGER NOT NULL,
			job_id TEXT NOT NULL,
			node_id TEXT NOT NULL DEFAULT '',
			stream TEXT NOT NULL DEFAULT '',
			line TEXT NOT NULL DEFAULT ''
		);
		CREATE INDEX IF NOT EXISTS idx_logs_job_ts ON logs(job_id, timestamp);

		CREATE TABLE IF NOT EXISTS costs (
			timestamp INTEGER NOT NULL,
			cluster_id TEXT NOT NULL,
			cloud TEXT NOT NULL DEFAULT '',
			region TEXT NOT NULL DEFAULT '',
			instance_type TEXT NOT NULL DEFAULT '',
			node_id TEXT NOT NULL DEFAULT '',
			hourly_cost REAL NOT NULL DEFAULT 0,
			is_spot INTEGER NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_costs_cluster_ts ON costs(cluster_id, timestamp);
	`)
	return err
}

func (s *SQLiteAnalyticsStore) InsertLogs(ctx context.Context, entries []LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO logs (timestamp, job_id, node_id, stream, line) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx,
			e.Timestamp.UnixNano(), e.JobID, e.NodeID, e.Stream, string(e.Line),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteAnalyticsStore) QueryLogs(ctx context.Context, jobID string, tr TimeRange, limit int) ([]LogEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT timestamp, job_id, node_id, stream, line FROM logs
		 WHERE job_id = ? AND timestamp >= ? AND timestamp <= ?
		 ORDER BY timestamp ASC LIMIT ?`,
		jobID, tr.From.UnixNano(), tr.To.UnixNano(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		var tsNano int64
		var line string
		if err := rows.Scan(&tsNano, &e.JobID, &e.NodeID, &e.Stream, &line); err != nil {
			return nil, err
		}
		e.Timestamp = time.Unix(0, tsNano)
		e.Line = []byte(line)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (s *SQLiteAnalyticsStore) InsertMetrics(ctx context.Context, points []MetricPoint) error {
	if len(points) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO metrics (timestamp, node_id, cluster_id, cpu_percent, memory_percent,
		 disk_used_bytes, gpu_index, gpu_utilization, gpu_memory_used, gpu_temperature)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range points {
		if _, err := stmt.ExecContext(ctx,
			p.Timestamp.Unix(), p.NodeID, p.ClusterID,
			p.CPUPercent, p.MemoryPercent, p.DiskUsedBytes,
			p.GPUIndex, p.GPUUtilization, p.GPUMemoryUsed, p.GPUTemperature,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteAnalyticsStore) QueryMetrics(ctx context.Context, nodeID string, tr TimeRange) ([]MetricPoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT timestamp, node_id, cluster_id, cpu_percent, memory_percent,
		        disk_used_bytes, gpu_index, gpu_utilization, gpu_memory_used, gpu_temperature
		 FROM metrics
		 WHERE node_id = ? AND timestamp >= ? AND timestamp <= ?
		 ORDER BY timestamp ASC`,
		nodeID, tr.From.Unix(), tr.To.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMetricRows(rows)
}

func (s *SQLiteAnalyticsStore) QueryMetricsByCluster(ctx context.Context, clusterID string, tr TimeRange) ([]MetricPoint, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT timestamp, node_id, cluster_id, cpu_percent, memory_percent,
		        disk_used_bytes, gpu_index, gpu_utilization, gpu_memory_used, gpu_temperature
		 FROM metrics
		 WHERE cluster_id = ? AND timestamp >= ? AND timestamp <= ?
		 ORDER BY timestamp ASC`,
		clusterID, tr.From.Unix(), tr.To.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanMetricRows(rows)
}

func scanMetricRows(rows *sql.Rows) ([]MetricPoint, error) {
	var points []MetricPoint
	for rows.Next() {
		var p MetricPoint
		var ts int64
		if err := rows.Scan(
			&ts, &p.NodeID, &p.ClusterID,
			&p.CPUPercent, &p.MemoryPercent, &p.DiskUsedBytes,
			&p.GPUIndex, &p.GPUUtilization, &p.GPUMemoryUsed, &p.GPUTemperature,
		); err != nil {
			return nil, err
		}
		p.Timestamp = time.Unix(ts, 0)
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *SQLiteAnalyticsStore) InsertCostEvent(ctx context.Context, event CostEvent) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO costs (timestamp, cluster_id, cloud, region, instance_type, node_id, hourly_cost, is_spot)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Timestamp.Unix(), event.ClusterID, event.Cloud, event.Region,
		event.InstanceType, event.NodeID, event.HourlyCost, boolToInt(event.IsSpot),
	)
	return err
}

func (s *SQLiteAnalyticsStore) QueryCosts(ctx context.Context, clusterID string, tr TimeRange) ([]CostEvent, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT timestamp, cluster_id, cloud, region, instance_type, node_id, hourly_cost, is_spot
		 FROM costs
		 WHERE cluster_id = ? AND timestamp >= ? AND timestamp <= ?
		 ORDER BY timestamp ASC`,
		clusterID, tr.From.Unix(), tr.To.Unix(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []CostEvent
	for rows.Next() {
		var e CostEvent
		var ts int64
		var isSpot int
		if err := rows.Scan(&ts, &e.ClusterID, &e.Cloud, &e.Region,
			&e.InstanceType, &e.NodeID, &e.HourlyCost, &isSpot); err != nil {
			return nil, err
		}
		e.Timestamp = time.Unix(ts, 0)
		e.IsSpot = isSpot == 1
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *SQLiteAnalyticsStore) TotalCost(ctx context.Context, clusterID string, tr TimeRange) (float64, error) {
	var total sql.NullFloat64
	err := s.db.QueryRowContext(ctx,
		`SELECT sum(hourly_cost) FROM costs
		 WHERE cluster_id = ? AND timestamp >= ? AND timestamp <= ?`,
		clusterID, tr.From.Unix(), tr.To.Unix(),
	).Scan(&total)
	if err != nil {
		return 0, err
	}
	if !total.Valid {
		return 0, nil
	}
	return total.Float64, nil
}

func (s *SQLiteAnalyticsStore) Close() error {
	return s.db.Close()
}

var _ AnalyticsStore = (*SQLiteAnalyticsStore)(nil)
