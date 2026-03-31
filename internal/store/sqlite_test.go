package store

import (
	"path/filepath"
	"testing"
	"time"

	"broker/internal/domain"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("NewSQLite: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteStore_CreateCluster(t *testing.T) {
	t.Run("given a new cluster, when created, then it can be retrieved", func(t *testing.T) {
		s := newTestStore(t)

		cluster := &domain.Cluster{
			ID:       "c-1",
			Name:     "test-cluster",
			Status:   domain.ClusterStatusInit,
			Cloud:    domain.CloudAWS,
			Region:   "us-east-1",
			NumNodes: 2,
			UserID:   "user-1",
		}

		if err := s.CreateCluster(cluster); err != nil {
			t.Fatalf("CreateCluster: %v", err)
		}

		got, err := s.GetCluster("test-cluster")
		if err != nil {
			t.Fatalf("GetCluster: %v", err)
		}
		if got == nil {
			t.Fatal("expected cluster, got nil")
		}
		if got.Name != "test-cluster" {
			t.Errorf("expected name test-cluster, got %s", got.Name)
		}
		if got.Cloud != domain.CloudAWS {
			t.Errorf("expected cloud aws, got %s", got.Cloud)
		}
		if got.NumNodes != 2 {
			t.Errorf("expected 2 nodes, got %d", got.NumNodes)
		}
	})

	t.Run("given a cluster exists, when creating with the same name, then it returns an error", func(t *testing.T) {
		s := newTestStore(t)

		cluster := &domain.Cluster{Name: "dup-cluster", ID: "c-1"}
		if err := s.CreateCluster(cluster); err != nil {
			t.Fatalf("first CreateCluster: %v", err)
		}

		dup := &domain.Cluster{Name: "dup-cluster", ID: "c-2"}
		if err := s.CreateCluster(dup); err == nil {
			t.Fatal("expected error for duplicate cluster name, got nil")
		}
	})
}

func TestSQLiteStore_GetCluster(t *testing.T) {
	t.Run("given no clusters exist, when getting a non-existent cluster, then it returns nil", func(t *testing.T) {
		s := newTestStore(t)

		got, err := s.GetCluster("nonexistent")
		if err != nil {
			t.Fatalf("GetCluster: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})
}

func TestSQLiteStore_ListClusters(t *testing.T) {
	t.Run("given multiple clusters, when listing, then all are returned", func(t *testing.T) {
		s := newTestStore(t)

		names := []string{"alpha", "beta", "gamma"}
		for _, name := range names {
			c := &domain.Cluster{Name: name, ID: name}
			if err := s.CreateCluster(c); err != nil {
				t.Fatalf("CreateCluster(%s): %v", name, err)
			}
		}

		clusters, err := s.ListClusters()
		if err != nil {
			t.Fatalf("ListClusters: %v", err)
		}
		if len(clusters) != 3 {
			t.Fatalf("expected 3 clusters, got %d", len(clusters))
		}

		found := make(map[string]bool)
		for _, c := range clusters {
			found[c.Name] = true
		}
		for _, name := range names {
			if !found[name] {
				t.Errorf("expected cluster %s in list, not found", name)
			}
		}
	})

	t.Run("given no clusters, when listing, then empty slice is returned", func(t *testing.T) {
		s := newTestStore(t)

		clusters, err := s.ListClusters()
		if err != nil {
			t.Fatalf("ListClusters: %v", err)
		}
		if len(clusters) != 0 {
			t.Errorf("expected 0 clusters, got %d", len(clusters))
		}
	})
}

func TestSQLiteStore_UpdateCluster(t *testing.T) {
	t.Run("given an existing cluster, when updated, then changes are persisted", func(t *testing.T) {
		s := newTestStore(t)

		cluster := &domain.Cluster{
			Name:   "update-me",
			ID:     "c-1",
			Status: domain.ClusterStatusInit,
		}
		if err := s.CreateCluster(cluster); err != nil {
			t.Fatalf("CreateCluster: %v", err)
		}

		cluster.Status = domain.ClusterStatusUp
		cluster.HeadIP = "10.0.0.1"
		if err := s.UpdateCluster(cluster); err != nil {
			t.Fatalf("UpdateCluster: %v", err)
		}

		got, err := s.GetCluster("update-me")
		if err != nil {
			t.Fatalf("GetCluster: %v", err)
		}
		if got.Status != domain.ClusterStatusUp {
			t.Errorf("expected status UP, got %s", got.Status)
		}
		if got.HeadIP != "10.0.0.1" {
			t.Errorf("expected head IP 10.0.0.1, got %s", got.HeadIP)
		}
	})
}

func TestSQLiteStore_DeleteCluster(t *testing.T) {
	t.Run("given an existing cluster, when deleted, then it is no longer retrievable", func(t *testing.T) {
		s := newTestStore(t)

		cluster := &domain.Cluster{Name: "delete-me", ID: "c-1"}
		if err := s.CreateCluster(cluster); err != nil {
			t.Fatalf("CreateCluster: %v", err)
		}

		if err := s.DeleteCluster("delete-me"); err != nil {
			t.Fatalf("DeleteCluster: %v", err)
		}

		got, err := s.GetCluster("delete-me")
		if err != nil {
			t.Fatalf("GetCluster: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil after delete, got %+v", got)
		}
	})

	t.Run("given no cluster exists, when deleting, then no error is returned", func(t *testing.T) {
		s := newTestStore(t)

		if err := s.DeleteCluster("nonexistent"); err != nil {
			t.Fatalf("DeleteCluster: %v", err)
		}
	})
}

func TestSQLiteStore_CreateJob(t *testing.T) {
	t.Run("given a new job, when created, then it can be retrieved by ID", func(t *testing.T) {
		s := newTestStore(t)

		now := time.Now().UTC().Truncate(time.Second)
		job := &domain.Job{
			ID:          "j-1",
			ClusterName: "cluster-a",
			Name:        "train",
			Status:      domain.JobStatusPending,
			UserID:      "user-1",
			SubmittedAt: now,
		}

		if err := s.CreateJob(job); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		got, err := s.GetJob("j-1")
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if got == nil {
			t.Fatal("expected job, got nil")
		}
		if got.Name != "train" {
			t.Errorf("expected name train, got %s", got.Name)
		}
		if got.ClusterName != "cluster-a" {
			t.Errorf("expected cluster cluster-a, got %s", got.ClusterName)
		}
		if got.Status != domain.JobStatusPending {
			t.Errorf("expected status PENDING, got %s", got.Status)
		}
	})
}

func TestSQLiteStore_GetJob(t *testing.T) {
	t.Run("given no jobs exist, when getting a non-existent job, then it returns nil", func(t *testing.T) {
		s := newTestStore(t)

		got, err := s.GetJob("nonexistent")
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %+v", got)
		}
	})
}

func TestSQLiteStore_ListJobs(t *testing.T) {
	t.Run("given jobs in multiple clusters, when listing by cluster, then only matching jobs are returned", func(t *testing.T) {
		s := newTestStore(t)

		for i, name := range []string{"j-1", "j-2", "j-3"} {
			cluster := "cluster-a"
			if i == 2 {
				cluster = "cluster-b"
			}
			job := &domain.Job{ID: name, ClusterName: cluster, Status: domain.JobStatusPending}
			if err := s.CreateJob(job); err != nil {
				t.Fatalf("CreateJob(%s): %v", name, err)
			}
			time.Sleep(10 * time.Millisecond)
		}

		jobs, err := s.ListJobs("cluster-a")
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(jobs) != 2 {
			t.Fatalf("expected 2 jobs for cluster-a, got %d", len(jobs))
		}

		jobsB, err := s.ListJobs("cluster-b")
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(jobsB) != 1 {
			t.Fatalf("expected 1 job for cluster-b, got %d", len(jobsB))
		}
	})

	t.Run("given no jobs for a cluster, when listing, then empty slice is returned", func(t *testing.T) {
		s := newTestStore(t)

		jobs, err := s.ListJobs("empty-cluster")
		if err != nil {
			t.Fatalf("ListJobs: %v", err)
		}
		if len(jobs) != 0 {
			t.Errorf("expected 0 jobs, got %d", len(jobs))
		}
	})
}

func TestSQLiteStore_UpdateJob(t *testing.T) {
	t.Run("given an existing job, when updated, then changes are persisted", func(t *testing.T) {
		s := newTestStore(t)

		job := &domain.Job{
			ID:          "j-1",
			ClusterName: "cluster-a",
			Status:      domain.JobStatusPending,
		}
		if err := s.CreateJob(job); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		now := time.Now().UTC().Truncate(time.Second)
		job.Status = domain.JobStatusRunning
		job.StartedAt = &now
		if err := s.UpdateJob(job); err != nil {
			t.Fatalf("UpdateJob: %v", err)
		}

		got, err := s.GetJob("j-1")
		if err != nil {
			t.Fatalf("GetJob: %v", err)
		}
		if got.Status != domain.JobStatusRunning {
			t.Errorf("expected status RUNNING, got %s", got.Status)
		}
		if got.StartedAt == nil {
			t.Fatal("expected StartedAt to be set")
		}
	})
}

func TestSQLiteStore_ClusterLifecycle(t *testing.T) {
	t.Run("given a store, when running full cluster lifecycle, then each step produces correct state", func(t *testing.T) {
		s := newTestStore(t)

		cluster := &domain.Cluster{
			ID:       "c-lifecycle",
			Name:     "lifecycle-cluster",
			Status:   domain.ClusterStatusInit,
			Cloud:    domain.CloudGCP,
			Region:   "us-central1",
			NumNodes: 4,
			UserID:   "user-1",
		}

		if err := s.CreateCluster(cluster); err != nil {
			t.Fatalf("CreateCluster: %v", err)
		}

		got, err := s.GetCluster("lifecycle-cluster")
		if err != nil {
			t.Fatalf("GetCluster after create: %v", err)
		}
		if got == nil {
			t.Fatal("expected cluster after create, got nil")
		}
		if got.ID != "c-lifecycle" {
			t.Errorf("expected ID c-lifecycle, got %s", got.ID)
		}
		if got.Cloud != domain.CloudGCP {
			t.Errorf("expected cloud gcp, got %s", got.Cloud)
		}
		if got.NumNodes != 4 {
			t.Errorf("expected 4 nodes, got %d", got.NumNodes)
		}

		cluster.Status = domain.ClusterStatusUp
		cluster.HeadIP = "10.128.0.2"
		if err := s.UpdateCluster(cluster); err != nil {
			t.Fatalf("UpdateCluster: %v", err)
		}

		got, err = s.GetCluster("lifecycle-cluster")
		if err != nil {
			t.Fatalf("GetCluster after update: %v", err)
		}
		if got.Status != domain.ClusterStatusUp {
			t.Errorf("expected status UP after update, got %s", got.Status)
		}
		if got.HeadIP != "10.128.0.2" {
			t.Errorf("expected head IP 10.128.0.2, got %s", got.HeadIP)
		}

		clusters, err := s.ListClusters()
		if err != nil {
			t.Fatalf("ListClusters: %v", err)
		}
		if len(clusters) != 1 {
			t.Fatalf("expected 1 cluster in list, got %d", len(clusters))
		}
		if clusters[0].Name != "lifecycle-cluster" {
			t.Errorf("expected lifecycle-cluster in list, got %s", clusters[0].Name)
		}

		if err := s.DeleteCluster("lifecycle-cluster"); err != nil {
			t.Fatalf("DeleteCluster: %v", err)
		}

		got, err = s.GetCluster("lifecycle-cluster")
		if err != nil {
			t.Fatalf("GetCluster after delete: %v", err)
		}
		if got != nil {
			t.Errorf("expected nil after delete, got %+v", got)
		}

		clusters, err = s.ListClusters()
		if err != nil {
			t.Fatalf("ListClusters after delete: %v", err)
		}
		if len(clusters) != 0 {
			t.Errorf("expected 0 clusters after delete, got %d", len(clusters))
		}
	})
}

func TestSQLiteStore_JobLifecycle(t *testing.T) {
	t.Run("given a store, when running full job lifecycle, then create list update all produce correct state", func(t *testing.T) {
		s := newTestStore(t)

		now := time.Now().UTC().Truncate(time.Second)
		job := &domain.Job{
			ID:          "j-lifecycle",
			ClusterName: "cluster-x",
			Name:        "training-run",
			Status:      domain.JobStatusPending,
			UserID:      "user-1",
			SubmittedAt: now,
		}

		if err := s.CreateJob(job); err != nil {
			t.Fatalf("CreateJob: %v", err)
		}

		jobs, err := s.ListJobs("cluster-x")
		if err != nil {
			t.Fatalf("ListJobs after create: %v", err)
		}
		if len(jobs) != 1 {
			t.Fatalf("expected 1 job, got %d", len(jobs))
		}
		if jobs[0].ID != "j-lifecycle" {
			t.Errorf("expected job ID j-lifecycle, got %s", jobs[0].ID)
		}
		if jobs[0].Name != "training-run" {
			t.Errorf("expected job name training-run, got %s", jobs[0].Name)
		}

		started := time.Now().UTC().Truncate(time.Second)
		job.Status = domain.JobStatusRunning
		job.StartedAt = &started
		if err := s.UpdateJob(job); err != nil {
			t.Fatalf("UpdateJob to running: %v", err)
		}

		got, err := s.GetJob("j-lifecycle")
		if err != nil {
			t.Fatalf("GetJob after update to running: %v", err)
		}
		if got.Status != domain.JobStatusRunning {
			t.Errorf("expected status RUNNING, got %s", got.Status)
		}
		if got.StartedAt == nil {
			t.Fatal("expected StartedAt to be set after update")
		}

		ended := time.Now().UTC().Truncate(time.Second)
		job.Status = domain.JobStatusSucceeded
		job.EndedAt = &ended
		if err := s.UpdateJob(job); err != nil {
			t.Fatalf("UpdateJob to succeeded: %v", err)
		}

		got, err = s.GetJob("j-lifecycle")
		if err != nil {
			t.Fatalf("GetJob after update to succeeded: %v", err)
		}
		if got.Status != domain.JobStatusSucceeded {
			t.Errorf("expected status SUCCEEDED, got %s", got.Status)
		}
		if got.EndedAt == nil {
			t.Fatal("expected EndedAt to be set after succeeded")
		}
	})
}

func TestSQLiteStore_JobClusterIsolation(t *testing.T) {
	t.Run("given jobs on two clusters, when listing each cluster, then only that clusters jobs are returned", func(t *testing.T) {
		s := newTestStore(t)

		clusterAJobs := []string{"a-1", "a-2", "a-3"}
		clusterBJobs := []string{"b-1", "b-2"}

		for _, id := range clusterAJobs {
			j := &domain.Job{ID: id, ClusterName: "cluster-alpha", Name: "job-" + id, Status: domain.JobStatusPending}
			if err := s.CreateJob(j); err != nil {
				t.Fatalf("CreateJob(%s): %v", id, err)
			}
			time.Sleep(5 * time.Millisecond)
		}
		for _, id := range clusterBJobs {
			j := &domain.Job{ID: id, ClusterName: "cluster-beta", Name: "job-" + id, Status: domain.JobStatusRunning}
			if err := s.CreateJob(j); err != nil {
				t.Fatalf("CreateJob(%s): %v", id, err)
			}
			time.Sleep(5 * time.Millisecond)
		}

		jobsA, err := s.ListJobs("cluster-alpha")
		if err != nil {
			t.Fatalf("ListJobs(cluster-alpha): %v", err)
		}
		if len(jobsA) != 3 {
			t.Fatalf("expected 3 jobs for cluster-alpha, got %d", len(jobsA))
		}
		for _, j := range jobsA {
			if j.ClusterName != "cluster-alpha" {
				t.Errorf("job %s has cluster %s, expected cluster-alpha", j.ID, j.ClusterName)
			}
		}

		jobsB, err := s.ListJobs("cluster-beta")
		if err != nil {
			t.Fatalf("ListJobs(cluster-beta): %v", err)
		}
		if len(jobsB) != 2 {
			t.Fatalf("expected 2 jobs for cluster-beta, got %d", len(jobsB))
		}
		for _, j := range jobsB {
			if j.ClusterName != "cluster-beta" {
				t.Errorf("job %s has cluster %s, expected cluster-beta", j.ID, j.ClusterName)
			}
			if j.Status != domain.JobStatusRunning {
				t.Errorf("job %s has status %s, expected RUNNING", j.ID, j.Status)
			}
		}

		jobsC, err := s.ListJobs("cluster-nonexistent")
		if err != nil {
			t.Fatalf("ListJobs(cluster-nonexistent): %v", err)
		}
		if len(jobsC) != 0 {
			t.Errorf("expected 0 jobs for nonexistent cluster, got %d", len(jobsC))
		}
	})
}
