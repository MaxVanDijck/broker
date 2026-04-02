package server

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"broker/internal/domain"
	"broker/internal/provider"
	"broker/internal/store"
)

// mockProvider records calls to Teardown and can simulate launch behavior.
type mockProvider struct {
	mu            sync.Mutex
	teardownCalls []*domain.Cluster
	launchFn      func(ctx context.Context, cluster *domain.Cluster, task *domain.TaskSpec) ([]provider.NodeInfo, error)
	teardownFn    func(ctx context.Context, cluster *domain.Cluster) error
}

func (m *mockProvider) Name() domain.CloudProvider {
	return domain.CloudAWS
}

func (m *mockProvider) Launch(ctx context.Context, cluster *domain.Cluster, task *domain.TaskSpec) ([]provider.NodeInfo, error) {
	if m.launchFn != nil {
		return m.launchFn(ctx, cluster, task)
	}
	return nil, nil
}

func (m *mockProvider) Stop(_ context.Context, _ *domain.Cluster) error {
	return nil
}

func (m *mockProvider) Start(_ context.Context, _ *domain.Cluster) error {
	return nil
}

func (m *mockProvider) Teardown(ctx context.Context, cluster *domain.Cluster) error {
	m.mu.Lock()
	m.teardownCalls = append(m.teardownCalls, cluster)
	m.mu.Unlock()
	if m.teardownFn != nil {
		return m.teardownFn(ctx, cluster)
	}
	return nil
}

func (m *mockProvider) Status(_ context.Context, _ *domain.Cluster) (domain.ClusterStatus, error) {
	return domain.ClusterStatusUp, nil
}

func (m *mockProvider) TeardownCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.teardownCalls)
}

func TestProvisionWatchdog_TerminatesWhenNoAgentRegisters(t *testing.T) {
	t.Run("given a provisioned cluster with no agent, when the watchdog timeout expires, then the cluster is deleted", func(t *testing.T) {
		dbPath := t.TempDir() + "/watchdog.db"
		db, err := store.NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("failed to create sqlite store: %v", err)
		}
		t.Cleanup(func() { db.Close() })

		mock := &mockProvider{}
		registry := provider.NewRegistry()
		registry.Register(mock)
		events := NewEventBus(slog.Default())
		logger := slog.Default()

		srv := New(db, nil, registry, logger)
		srv.events = events

		cluster := &domain.Cluster{
			ID:     "watchdog-c-1",
			Name:   "watchdog-cluster",
			Status: domain.ClusterStatusInit,
			Cloud:  domain.CloudAWS,
			UserID: "default",
		}
		if err := db.CreateCluster(cluster); err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		timeout := 2 * time.Second
		go srv.watchProvisionedCluster(cluster.ID, mock, cluster, timeout)

		// Wait for timeout + some buffer for the watchdog tick interval
		// The watchdog ticks every 30s, but the deadline channel fires after timeout.
		// Since timeout is 2s, the deadline fires first, which triggers teardown.
		time.Sleep(timeout + 1*time.Second)

		// Verify the cluster was deleted from the store
		c, err := db.GetClusterByID("watchdog-c-1")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if c != nil {
			t.Error("expected cluster to be deleted after watchdog timeout")
		}

		// Verify Teardown was called on the provider
		if mock.TeardownCallCount() != 1 {
			t.Errorf("expected 1 teardown call, got %d", mock.TeardownCallCount())
		}
	})
}

func TestProvisionWatchdog_CancelledWhenAgentRegisters(t *testing.T) {
	t.Run("given a provisioned cluster, when an agent registers before timeout, then the watchdog cancels without teardown", func(t *testing.T) {
		dbPath := t.TempDir() + "/watchdog-cancel.db"
		db, err := store.NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("failed to create sqlite store: %v", err)
		}
		t.Cleanup(func() { db.Close() })

		mock := &mockProvider{}
		registry := provider.NewRegistry()
		registry.Register(mock)
		events := NewEventBus(slog.Default())
		logger := slog.Default()

		srv := New(db, nil, registry, logger)
		srv.events = events

		cluster := &domain.Cluster{
			ID:     "watchdog-c-2",
			Name:   "watchdog-cluster-2",
			Status: domain.ClusterStatusInit,
			Cloud:  domain.CloudAWS,
			UserID: "default",
		}
		if err := db.CreateCluster(cluster); err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		// Use a 5s timeout. The watchdog ticker fires every 30s, but the
		// deadline fires after 5s. We register the agent before the
		// deadline, so when the deadline fires it sees the agent and exits
		// cleanly without teardown.
		timeout := 5 * time.Second

		// Register the agent immediately before starting the watchdog
		srv.Tunnel.mu.Lock()
		ac := &AgentConnection{
			NodeID:      "watchdog-node",
			ClusterName: "watchdog-cluster-2",
		}
		ac.SetClusterID("watchdog-c-2")
		srv.Tunnel.agents["watchdog-node"] = ac
		srv.Tunnel.mu.Unlock()

		go srv.watchProvisionedCluster(cluster.ID, mock, cluster, timeout)

		// Wait for deadline to fire plus buffer
		time.Sleep(timeout + 2*time.Second)

		// Cluster should still exist
		c, err := db.GetClusterByID("watchdog-c-2")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if c == nil {
			t.Error("expected cluster to still exist (agent registered before timeout)")
		}

		// Teardown should NOT have been called
		if mock.TeardownCallCount() != 0 {
			t.Errorf("expected 0 teardown calls, got %d", mock.TeardownCallCount())
		}
	})
}

func TestAutostopWatchdog_IdleClusterTerminatedWithProvider(t *testing.T) {
	t.Run("given an idle cloud cluster with a mock provider, when autostop check triggers, then provider.Teardown is called", func(t *testing.T) {
		dbPath := t.TempDir() + "/autostop-provider.db"
		db, err := store.NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("failed to create sqlite store: %v", err)
		}
		t.Cleanup(func() { db.Close() })

		mock := &mockProvider{}
		registry := provider.NewRegistry()
		registry.Register(mock)
		events := NewEventBus(slog.Default())
		logger := slog.Default()

		m := NewAutostopManager(db, logger)

		srv := &Server{
			store:    db,
			registry: registry,
			logger:   logger,
			events:   events,
			autostop: m,
		}
		m.onTeardown = func(cluster *domain.Cluster) {
			srv.teardownCluster(cluster)
		}

		cluster := &domain.Cluster{
			ID:     "autostop-c-1",
			Name:   "autostop-cluster",
			Status: domain.ClusterStatusUp,
			Cloud:  domain.CloudAWS,
			UserID: "default",
		}
		if err := db.CreateCluster(cluster); err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		// Set a very short timeout and backdate the last activity
		m.SetTimeout("autostop-c-1", 1*time.Millisecond)
		m.mu.Lock()
		m.clusters["autostop-c-1"].lastActivity = time.Now().Add(-1 * time.Hour)
		m.mu.Unlock()

		// Trigger the check
		m.check()

		// Wait for the async teardown goroutine
		time.Sleep(200 * time.Millisecond)

		// Verify Teardown was called
		if mock.TeardownCallCount() != 1 {
			t.Errorf("expected 1 teardown call, got %d", mock.TeardownCallCount())
		}

		// Verify cluster status is TERMINATED
		c, err := db.GetClusterByID("autostop-c-1")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if c == nil {
			t.Fatal("expected cluster to exist")
		}
		if c.Status != domain.ClusterStatusTerminated {
			t.Errorf("expected status TERMINATED, got %s", c.Status)
		}

		// Verify cluster was removed from autostop tracking
		m.mu.Lock()
		_, tracked := m.clusters["autostop-c-1"]
		m.mu.Unlock()
		if tracked {
			t.Error("expected cluster to be removed from autostop tracking")
		}
	})
}
