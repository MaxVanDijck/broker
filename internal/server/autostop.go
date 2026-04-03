package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"broker/internal/domain"
	"broker/internal/store"
)

type autostopEntry struct {
	lastActivity time.Time
	timeout      time.Duration
}

type AutostopManager struct {
	mu       sync.Mutex
	clusters map[string]*autostopEntry // cluster_id -> entry

	store      store.StateStore
	logger     *slog.Logger
	onTeardown func(cluster *domain.Cluster) // called to initiate cluster teardown
}

func NewAutostopManager(s store.StateStore, logger *slog.Logger) *AutostopManager {
	return &AutostopManager{
		clusters: make(map[string]*autostopEntry),
		store:    s,
		logger:   logger,
	}
}

func (m *AutostopManager) Touch(clusterID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.clusters[clusterID]
	if !ok {
		return
	}
	entry.lastActivity = time.Now()
}

func (m *AutostopManager) SetTimeout(clusterID string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.clusters[clusterID]
	if ok {
		entry.timeout = d
	} else {
		m.clusters[clusterID] = &autostopEntry{
			lastActivity: time.Now(),
			timeout:      d,
		}
	}
}

func (m *AutostopManager) Remove(clusterID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.clusters, clusterID)
}

func (m *AutostopManager) Run(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.check()
		}
	}
}

func (m *AutostopManager) check() {
	now := time.Now()

	m.mu.Lock()
	candidates := make(map[string]autostopEntry)
	for id, entry := range m.clusters {
		if entry.timeout <= 0 {
			continue
		}
		if now.Sub(entry.lastActivity) >= entry.timeout {
			candidates[id] = *entry
		}
	}
	m.mu.Unlock()

	for id, entry := range candidates {
		if m.hasRunningJobs(id) {
			m.mu.Lock()
			if e, ok := m.clusters[id]; ok {
				e.lastActivity = now
			}
			m.mu.Unlock()
			continue
		}

		m.logger.Info("autostop: cluster idle, tearing down",
			"cluster_id", id,
			"idle_for", entry.timeout,
		)
		m.teardown(id)
	}
}

func (m *AutostopManager) hasRunningJobs(clusterID string) bool {
	jobs, err := m.store.ListJobs(clusterID)
	if err != nil {
		m.logger.Error("autostop: failed to list jobs", "cluster_id", clusterID, "error", err)
		return true
	}
	for _, j := range jobs {
		if j.Status == domain.JobStatusRunning || j.Status == domain.JobStatusPending {
			return true
		}
	}
	return false
}

func (m *AutostopManager) teardown(clusterID string) {
	cluster, err := m.store.GetClusterByID(clusterID)
	if err != nil || cluster == nil {
		m.logger.Error("autostop: cluster not found for teardown", "cluster_id", clusterID, "error", err)
		return
	}

	if cluster.Status == domain.ClusterStatusTerminating || cluster.Status == domain.ClusterStatusTerminated {
		return
	}

	if m.onTeardown != nil {
		m.onTeardown(cluster)
	}
}
