package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"broker/internal/domain"
	"broker/internal/provider"
	"broker/internal/store"
)

const defaultAutostopTimeout = 30 * time.Minute

type autostopEntry struct {
	lastActivity time.Time
	timeout      time.Duration
}

type AutostopManager struct {
	mu       sync.Mutex
	clusters map[string]*autostopEntry // cluster_id -> entry

	store    store.StateStore
	registry *provider.Registry
	events   *EventBus
	logger   *slog.Logger
}

func NewAutostopManager(s store.StateStore, r *provider.Registry, events *EventBus, logger *slog.Logger) *AutostopManager {
	return &AutostopManager{
		clusters: make(map[string]*autostopEntry),
		store:    s,
		registry: r,
		events:   events,
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
	m.Remove(clusterID)

	cluster, err := m.store.GetClusterByID(clusterID)
	if err != nil || cluster == nil {
		m.logger.Error("autostop: cluster not found for teardown", "cluster_id", clusterID, "error", err)
		return
	}

	if cluster.Status == domain.ClusterStatusTerminating || cluster.Status == domain.ClusterStatusTerminated {
		return
	}

	cluster.Status = domain.ClusterStatusTerminating
	m.store.UpdateCluster(cluster)
	m.events.Publish(Event{
		Type: "cluster_update",
		Data: map[string]string{
			"cluster_name": cluster.Name,
			"cluster_id":   cluster.ID,
			"status":       string(domain.ClusterStatusTerminating),
		},
	})

	clusterName := cluster.Name
	clusterCloud := cluster.Cloud
	go func() {
		if clusterCloud != "" {
			if prov, ok := m.registry.Get(clusterCloud); ok {
				c, _ := m.store.GetClusterByID(clusterID)
				if c != nil {
					if err := prov.Teardown(context.Background(), c); err != nil {
						m.logger.Error("autostop: cloud teardown failed", "cluster", clusterName, "error", err)
					}
				}
			}
		}

		if c, _ := m.store.GetClusterByID(clusterID); c != nil {
			c.Status = domain.ClusterStatusTerminated
			m.store.UpdateCluster(c)
		}
		m.events.Publish(Event{
			Type: "cluster_update",
			Data: map[string]string{
				"cluster_name": clusterName,
				"cluster_id":   clusterID,
				"status":       string(domain.ClusterStatusTerminated),
			},
		})
		m.logger.Info("autostop: cluster terminated", "cluster", clusterName)
	}()
}
