package store

import (
	"context"
	"time"

	"broker/internal/domain"
)

// StateStore handles OLTP state: clusters, jobs, users.
// Backed by SQLite (local) or PostgreSQL (production).
type StateStore interface {
	CreateCluster(c *domain.Cluster) error
	GetCluster(name string) (*domain.Cluster, error)
	GetClusterByID(id string) (*domain.Cluster, error)
	ListClusters() ([]*domain.Cluster, error)
	UpdateCluster(c *domain.Cluster) error
	DeleteCluster(id string) error

	CreateJob(j *domain.Job) error
	GetJob(id string) (*domain.Job, error)
	ListJobs(clusterID string) ([]*domain.Job, error)
	ListAllJobs() ([]*domain.Job, error)
	UpdateJob(j *domain.Job) error
}

// LogEntry is a single log line from a job.
type LogEntry struct {
	Timestamp time.Time
	JobID     string
	NodeID    string
	Stream    string // "stdout" or "stderr"
	Line      []byte
}

// MetricPoint is a single metric sample from a node.
type MetricPoint struct {
	Timestamp      time.Time
	NodeID         string
	ClusterID      string
	CPUPercent     float64
	MemoryPercent  float64
	DiskUsedBytes  int64
	GPUIndex       int32
	GPUUtilization float64
	GPUMemoryUsed  int64
	GPUTemperature float64
}

// CostEvent is a billing event for a cluster.
type CostEvent struct {
	Timestamp    time.Time
	ClusterID    string
	Cloud        string
	Region       string
	InstanceType string
	NodeID       string
	HourlyCost   float64
	IsSpot       bool
}

// TimeRange defines a query window.
type TimeRange struct {
	From time.Time
	To   time.Time
}

// AnalyticsStore handles append-only analytical data: logs, metrics, costs.
// Backed by chdb (local) or ClickHouse (production).
type AnalyticsStore interface {
	// Logs
	InsertLogs(ctx context.Context, entries []LogEntry) error
	QueryLogs(ctx context.Context, jobID string, tr TimeRange, limit int) ([]LogEntry, error)

	// Metrics
	InsertMetrics(ctx context.Context, points []MetricPoint) error
	QueryMetrics(ctx context.Context, nodeID string, tr TimeRange) ([]MetricPoint, error)
	QueryMetricsByCluster(ctx context.Context, clusterID string, tr TimeRange) ([]MetricPoint, error)

	// Costs
	InsertCostEvent(ctx context.Context, event CostEvent) error
	QueryCosts(ctx context.Context, clusterID string, tr TimeRange) ([]CostEvent, error)
	TotalCost(ctx context.Context, clusterID string, tr TimeRange) (float64, error)

	Close() error
}
