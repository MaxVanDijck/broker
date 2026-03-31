package domain

import "time"

type ServiceStatus string

const (
	ServiceStatusProvisioning ServiceStatus = "PROVISIONING"
	ServiceStatusReady        ServiceStatus = "READY"
	ServiceStatusFailed       ServiceStatus = "FAILED"
)

type Service struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Status    ServiceStatus `json:"status"`
	Endpoint  string        `json:"endpoint,omitempty"`
	Replicas  int           `json:"replicas"`
	UserID    string        `json:"user_id"`
	CreatedAt time.Time     `json:"created_at"`
}
