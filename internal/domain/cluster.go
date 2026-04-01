package domain

import "time"

type ClusterStatus string

const (
	ClusterStatusInit        ClusterStatus = "INIT"
	ClusterStatusUp          ClusterStatus = "UP"
	ClusterStatusStopped     ClusterStatus = "STOPPED"
	ClusterStatusTerminating ClusterStatus = "TERMINATING"
	ClusterStatusTerminated  ClusterStatus = "TERMINATED"
)

type Cluster struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Status          ClusterStatus `json:"status"`
	Cloud           CloudProvider `json:"cloud"`
	Region          string        `json:"region"`
	Zone            string        `json:"zone"`
	NumNodes        int           `json:"num_nodes"`
	HeadIP          string        `json:"head_ip,omitempty"`
	WorkdirID       string        `json:"workdir_id,omitempty"`
	Resources       *Resources    `json:"resources,omitempty"`
	UserID          string        `json:"user_id"`
	LaunchedAt      time.Time     `json:"launched_at"`
	AutostopMinutes int           `json:"autostop_minutes,omitempty"`
}
