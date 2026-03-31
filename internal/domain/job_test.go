package domain

import "testing"

func TestJobStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		name     string
		status   JobStatus
		terminal bool
	}{
		{"given PENDING status, when checking IsTerminal, then false", JobStatusPending, false},
		{"given RUNNING status, when checking IsTerminal, then false", JobStatusRunning, false},
		{"given SUCCEEDED status, when checking IsTerminal, then true", JobStatusSucceeded, true},
		{"given FAILED status, when checking IsTerminal, then true", JobStatusFailed, true},
		{"given CANCELLED status, when checking IsTerminal, then true", JobStatusCancelled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.terminal {
				t.Errorf("JobStatus(%s).IsTerminal() = %v, want %v", tt.status, got, tt.terminal)
			}
		})
	}
}
