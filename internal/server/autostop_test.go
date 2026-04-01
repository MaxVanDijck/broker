package server

import (
	"log/slog"
	"testing"
	"time"

	"broker/internal/domain"
	"broker/internal/provider"
	"broker/internal/store"
)

func newTestAutostopManager(t *testing.T) (*AutostopManager, *store.SQLiteStore) {
	t.Helper()
	db, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	registry := provider.NewRegistry()
	events := NewEventBus(slog.Default())
	logger := slog.Default()

	m := NewAutostopManager(db, registry, events, logger)
	return m, db
}

func TestAutostopManager_Touch(t *testing.T) {
	t.Run("given a tracked cluster, when touched, then last activity is updated", func(t *testing.T) {
		m, _ := newTestAutostopManager(t)
		m.SetTimeout("c-1", 10*time.Minute)

		before := m.clusters["c-1"].lastActivity
		time.Sleep(1 * time.Millisecond)
		m.Touch("c-1")
		after := m.clusters["c-1"].lastActivity

		if !after.After(before) {
			t.Error("expected lastActivity to be updated after Touch")
		}
	})

	t.Run("given an untracked cluster, when touched, then nothing happens", func(t *testing.T) {
		m, _ := newTestAutostopManager(t)
		m.Touch("nonexistent")
	})
}

func TestAutostopManager_SetTimeout(t *testing.T) {
	t.Run("given no existing entry, when setting timeout, then entry is created", func(t *testing.T) {
		m, _ := newTestAutostopManager(t)
		m.SetTimeout("c-a", 15*time.Minute)

		m.mu.Lock()
		entry, ok := m.clusters["c-a"]
		m.mu.Unlock()

		if !ok {
			t.Fatal("expected cluster c-a to be tracked")
		}
		if entry.timeout != 15*time.Minute {
			t.Errorf("expected timeout 15m, got %v", entry.timeout)
		}
	})

	t.Run("given existing entry, when setting timeout, then timeout is updated", func(t *testing.T) {
		m, _ := newTestAutostopManager(t)
		m.SetTimeout("c-a", 15*time.Minute)
		m.SetTimeout("c-a", 45*time.Minute)

		m.mu.Lock()
		entry := m.clusters["c-a"]
		m.mu.Unlock()

		if entry.timeout != 45*time.Minute {
			t.Errorf("expected timeout 45m, got %v", entry.timeout)
		}
	})
}

func TestAutostopManager_Remove(t *testing.T) {
	t.Run("given a tracked cluster, when removed, then it is no longer tracked", func(t *testing.T) {
		m, _ := newTestAutostopManager(t)
		m.SetTimeout("c-a", 10*time.Minute)
		m.Remove("c-a")

		m.mu.Lock()
		_, ok := m.clusters["c-a"]
		m.mu.Unlock()

		if ok {
			t.Error("expected cluster c-a to be removed")
		}
	})
}

func TestAutostopManager_Check_IdleClusterTeardown(t *testing.T) {
	t.Run("given an idle cluster with no running jobs, when check runs, then cluster is terminated", func(t *testing.T) {
		m, db := newTestAutostopManager(t)

		err := db.CreateCluster(&domain.Cluster{
			ID:     "c-1",
			Name:   "idle-cluster",
			Status: domain.ClusterStatusUp,
			UserID: "default",
		})
		if err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		m.SetTimeout("c-1", 1*time.Millisecond)
		m.mu.Lock()
		m.clusters["c-1"].lastActivity = time.Now().Add(-1 * time.Hour)
		m.mu.Unlock()

		m.check()

		time.Sleep(50 * time.Millisecond)

		cluster, err := db.GetClusterByID("c-1")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if cluster == nil {
			t.Fatal("expected cluster to exist")
		}
		if cluster.Status != domain.ClusterStatusTerminated {
			t.Errorf("expected status TERMINATED, got %s", cluster.Status)
		}

		m.mu.Lock()
		_, tracked := m.clusters["c-1"]
		m.mu.Unlock()
		if tracked {
			t.Error("expected cluster to be removed from autostop tracking")
		}
	})
}

func TestAutostopManager_Check_ActiveJobsPreventTeardown(t *testing.T) {
	t.Run("given an idle cluster with running jobs, when check runs, then cluster is not terminated", func(t *testing.T) {
		m, db := newTestAutostopManager(t)

		err := db.CreateCluster(&domain.Cluster{
			ID:     "c-1",
			Name:   "busy-cluster",
			Status: domain.ClusterStatusUp,
			UserID: "default",
		})
		if err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		err = db.CreateJob(&domain.Job{
			ID:          "j-1",
			ClusterID:   "c-1",
			ClusterName: "busy-cluster",
			Status:      domain.JobStatusRunning,
			UserID:      "default",
			SubmittedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("create job: %v", err)
		}

		m.SetTimeout("c-1", 1*time.Millisecond)
		m.mu.Lock()
		m.clusters["c-1"].lastActivity = time.Now().Add(-1 * time.Hour)
		m.mu.Unlock()

		m.check()

		cluster, err := db.GetClusterByID("c-1")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if cluster.Status != domain.ClusterStatusUp {
			t.Errorf("expected status UP (not torn down), got %s", cluster.Status)
		}

		m.mu.Lock()
		_, tracked := m.clusters["c-1"]
		m.mu.Unlock()
		if !tracked {
			t.Error("expected cluster to still be tracked")
		}
	})
}

func TestAutostopManager_Check_NotYetIdleCluster(t *testing.T) {
	t.Run("given a recently active cluster, when check runs, then cluster is not terminated", func(t *testing.T) {
		m, db := newTestAutostopManager(t)

		err := db.CreateCluster(&domain.Cluster{
			ID:     "c-1",
			Name:   "fresh-cluster",
			Status: domain.ClusterStatusUp,
			UserID: "default",
		})
		if err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		m.SetTimeout("c-1", 30*time.Minute)

		m.check()

		cluster, err := db.GetClusterByID("c-1")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if cluster.Status != domain.ClusterStatusUp {
			t.Errorf("expected status UP, got %s", cluster.Status)
		}
	})
}

func TestAutostopManager_Check_ZeroTimeoutSkipped(t *testing.T) {
	t.Run("given a cluster with zero timeout, when check runs, then cluster is not terminated", func(t *testing.T) {
		m, db := newTestAutostopManager(t)

		err := db.CreateCluster(&domain.Cluster{
			ID:     "c-1",
			Name:   "no-autostop",
			Status: domain.ClusterStatusUp,
			UserID: "default",
		})
		if err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		m.SetTimeout("c-1", 0)
		m.mu.Lock()
		m.clusters["c-1"].lastActivity = time.Now().Add(-1 * time.Hour)
		m.mu.Unlock()

		m.check()

		cluster, err := db.GetClusterByID("c-1")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if cluster.Status != domain.ClusterStatusUp {
			t.Errorf("expected status UP, got %s", cluster.Status)
		}
	})
}

func TestAutostopManager_Check_AlreadyTerminatingSkipped(t *testing.T) {
	t.Run("given an already terminating cluster, when check runs, then no duplicate teardown occurs", func(t *testing.T) {
		m, db := newTestAutostopManager(t)

		err := db.CreateCluster(&domain.Cluster{
			ID:     "c-1",
			Name:   "terminating-cluster",
			Status: domain.ClusterStatusTerminating,
			UserID: "default",
		})
		if err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		m.SetTimeout("c-1", 1*time.Millisecond)
		m.mu.Lock()
		m.clusters["c-1"].lastActivity = time.Now().Add(-1 * time.Hour)
		m.mu.Unlock()

		m.check()

		time.Sleep(50 * time.Millisecond)

		cluster, err := db.GetClusterByID("c-1")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if cluster.Status != domain.ClusterStatusTerminating {
			t.Errorf("expected status TERMINATING (unchanged), got %s", cluster.Status)
		}
	})
}
