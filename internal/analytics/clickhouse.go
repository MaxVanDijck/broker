package analytics

import (
    entity "broker/internal/analytics/entity"
    "uuid"
)

type clickhouseStore struct {
    // TODO(max): conn, etc
}

func NewClickhouseStore() Store {
    return clickhouseStore{}
}


func (s clickhouseStore) InsertGpuMetrics(points []entity.GpuMetric) error {
    // TODO(max): implement
    return nil
}
func (s clickhouseStore) QueryGpuMetrics(nodeID uuid.UUID) ([]entity.GpuMetric, error) {
    // TODO(max): implement
    return make([]entity.GpuMetric, 0), nil
}

func (s clickhouseStore) Close() error {
    // TODO(max): implement
    return nil
}
