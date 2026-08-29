package analytics

import (
    entity "broker/internal/analytics/entity"
    "uuid"
)

type sqliteStore struct {
    // TODO(max): conn, etc
}

func NewSqliteStore() Store {
    return sqliteStore{}
}


func (s sqliteStore) InsertGpuMetrics(points []entity.GpuMetric) error {
    // TODO(max): implement
    return nil
}
func (s sqliteStore) QueryGpuMetrics(nodeID uuid.UUID) ([]entity.GpuMetric, error) {
    // TODO(max): implement
    return make([]entity.GpuMetric, 0), nil
}

func (s sqliteStore) Close() error {
    // TODO(max): implement
    return nil
}
