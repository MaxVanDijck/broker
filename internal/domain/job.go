package domain

import "time"

type JobStatus string

const (
	JobStatusPending   JobStatus = "PENDING"
	JobStatusRunning   JobStatus = "RUNNING"
	JobStatusSucceeded JobStatus = "SUCCEEDED"
	JobStatusFailed    JobStatus = "FAILED"
	JobStatusCancelled JobStatus = "CANCELLED"
)

func (s JobStatus) IsTerminal() bool {
	return s == JobStatusSucceeded || s == JobStatusFailed || s == JobStatusCancelled
}

type Job struct {
	ID          string     `json:"id"`
	ClusterID   string     `json:"cluster_id"`
	ClusterName string     `json:"cluster_name"`
	Name        string     `json:"name,omitempty"`
	Status      JobStatus  `json:"status"`
	UserID      string     `json:"user_id"`
	SubmittedAt time.Time  `json:"submitted_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
}
