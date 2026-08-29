package entity

import (
	"time"
	"uuid"
)

type NodeStatus string

const (
	NodeStatusInit        NodeStatus = "INIT"
	NodeStatusUp          NodeStatus = "UP"
	NodeStatusStopped     NodeStatus = "STOPPED"
	NodeStatusTerminating NodeStatus = "TERMINATING"
	NodeStatusTerminated  NodeStatus = "TERMINATED"
)

type Node struct {
	ID              uuid.UUID     `json:"id"`
	Name            string        `json:"name"`
	Status          NodeStatus    `json:"status"`
	// Cloud           CloudProvider `json:"cloud"` // TODO(max) add provider enum
	Region          string        `json:"region,omitempty"`
	Zone            string        `json:"zone,omitempty"`
	IP              string        `json:"head_ip,omitempty"`
	// Resources       *Resources    `json:"resources,omitempty"` // TODO(max) add resources field
	UserID          string        `json:"user_id"`
	LaunchedAt      time.Time     `json:"launched_at"`
	AutostopMinutes int           `json:"autostop_minutes,omitempty"`
}
