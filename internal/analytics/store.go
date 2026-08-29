package analytics

import (
	entity "broker/internal/analytics/entity"
	"uuid"
)

// AnalyticsStore handles gpu metrics, logs, etc
type Store interface {
	// TODO(max) pass context to methods
	// Metrics
	InsertGpuMetrics(points []entity.GpuMetric) error
	QueryGpuMetrics(nodeID uuid.UUID) ([]entity.GpuMetric, error)

	Close() error
}

