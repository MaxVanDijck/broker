package entity

import (
	"time"
	"uuid"

)

type GpuMetric struct {
	Timestamp   time.Time
	NodeID      uuid.UUID
	GpuIndex    int32
	Utilization float64
	MemoryUsed  int64
	Temperature float64
}
