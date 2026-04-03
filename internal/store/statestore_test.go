package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"broker/internal/domain"
)

var postgresDSN string

func TestMain(m *testing.M) {
	ctr := startPostgres()

	code := m.Run()

	if ctr != nil {
		ctr.Terminate(context.Background())
	}
	os.Exit(code)
}

func startPostgres() *postgres.PostgresContainer {
	// testcontainers-go panics if it can't find the Docker socket at
	// /var/run/docker.sock. On macOS with Colima/Rancher/Lima the socket
	// lives elsewhere. Detect it from `docker context inspect` so the test
	// works without manual env setup.
	if os.Getenv("DOCKER_HOST") == "" {
		if out, err := exec.Command("docker", "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output(); err == nil {
			if host := strings.TrimSpace(string(out)); host != "" {
				os.Setenv("DOCKER_HOST", host)
			}
		}
	}

	// Ryuk (the testcontainers cleanup sidecar) tries to mount the Docker
	// socket into itself. This fails on Colima/Lima because the host path
	// doesn't exist inside the VM. We handle cleanup ourselves, so disable it.
	if os.Getenv("TESTCONTAINERS_RYUK_DISABLED") == "" {
		os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	}

	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("broker_test"),
		postgres.WithUsername("broker"),
		postgres.WithPassword("broker"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to start postgres container: %v\n", err)
		os.Exit(1)
	}

	host, err := ctr.Host(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to get postgres host: %v\n", err)
		os.Exit(1)
	}
	port, err := ctr.MappedPort(ctx, "5432/tcp")
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to get postgres port: %v\n", err)
		os.Exit(1)
	}

	postgresDSN = fmt.Sprintf("postgres://broker:broker@%s:%s/broker_test?sslmode=disable", host, port.Port())

	// Colima/Lima port forwarding can lag behind the container readiness.
	// Verify we can actually connect before declaring the DSN ready.
	for range 20 {
		db, err := sql.Open("pgx", postgresDSN)
		if err == nil {
			if err = db.Ping(); err == nil {
				db.Close()
				return ctr
			}
			db.Close()
		}
		time.Sleep(250 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "FATAL: postgres container started but not reachable at %s\n", postgresDSN)
	os.Exit(1)
	return nil
}

type storeFactory struct {
	name  string
	setup func(t *testing.T) StateStore
}

func backends(t *testing.T) []storeFactory {
	t.Helper()

	return []storeFactory{
		{
			name: "sqlite",
			setup: func(t *testing.T) StateStore {
				t.Helper()
				s, err := NewSQLite(filepath.Join(t.TempDir(), "test.db"))
				if err != nil {
					t.Fatalf("NewSQLite: %v", err)
				}
				t.Cleanup(func() { s.Close() })
				return s
			},
		},
		{
			name: "postgres",
			setup: func(t *testing.T) StateStore {
				t.Helper()
				s, err := NewPostgres(postgresDSN)
				if err != nil {
					t.Fatalf("NewPostgres: %v", err)
				}
				truncate(t, s.db)
				t.Cleanup(func() { s.Close() })
				return s
			},
		},
	}
}

func truncate(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, table := range []string{"jobs", "clusters"} {
		if _, err := db.Exec("DELETE FROM " + table); err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

func mustEqual(t *testing.T, label string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s mismatch:\n  got:  %+v\n  want: %+v", label, got, want)
	}
}

// --- Cluster tests ---

func TestStateStore_CreateAndGetCluster(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			now := time.Now().UTC().Truncate(time.Second)
			cluster := &domain.Cluster{
				ID:       "c-1",
				Name:     "train-cluster",
				Status:   domain.ClusterStatusInit,
				Cloud:    domain.CloudAWS,
				Region:   "us-east-1",
				Zone:     "us-east-1a",
				NumNodes: 4,
				HeadIP:   "10.0.0.5",
				UserID:   "user-abc",
				Resources: &domain.Resources{
					Cloud:        domain.CloudAWS,
					Accelerators: "A100:8",
					InstanceType: "p4d.24xlarge",
					UseSpot:      true,
					DiskSizeGB:   500,
					Ports:        []string{"8080", "443"},
				},
				LaunchedAt:      now,
				AutostopMinutes: 60,
				WorkdirID:       "wd-123",
			}

			if err := s.CreateCluster(cluster); err != nil {
				t.Fatalf("CreateCluster: %v", err)
			}

			t.Run("given a created cluster, when GetCluster by name, then full struct is returned", func(t *testing.T) {
				got, err := s.GetCluster("train-cluster")
				if err != nil {
					t.Fatalf("GetCluster: %v", err)
				}
				if got == nil {
					t.Fatal("GetCluster returned nil")
				}
				mustEqual(t, "cluster", got, cluster)
			})

			t.Run("given a created cluster, when GetClusterByID, then full struct is returned", func(t *testing.T) {
				got, err := s.GetClusterByID("c-1")
				if err != nil {
					t.Fatalf("GetClusterByID: %v", err)
				}
				if got == nil {
					t.Fatal("GetClusterByID returned nil")
				}
				mustEqual(t, "cluster", got, cluster)
			})
		})
	}
}

func TestStateStore_GetClusterNotFound(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			t.Run("given no clusters, when GetCluster, then nil is returned without error", func(t *testing.T) {
				got, err := s.GetCluster("ghost")
				if err != nil {
					t.Fatalf("GetCluster: %v", err)
				}
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
			})

			t.Run("given no clusters, when GetClusterByID, then nil is returned without error", func(t *testing.T) {
				got, err := s.GetClusterByID("ghost-id")
				if err != nil {
					t.Fatalf("GetClusterByID: %v", err)
				}
				if got != nil {
					t.Errorf("expected nil, got %+v", got)
				}
			})
		})
	}
}

func TestStateStore_GetClusterSkipsTerminated(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			terminated := &domain.Cluster{ID: "c-old", Name: "my-cluster", Status: domain.ClusterStatusTerminated}
			if err := s.CreateCluster(terminated); err != nil {
				t.Fatalf("CreateCluster (terminated): %v", err)
			}

			t.Run("given only a terminated cluster with that name, when GetCluster, then nil is returned", func(t *testing.T) {
				got, err := s.GetCluster("my-cluster")
				if err != nil {
					t.Fatalf("GetCluster: %v", err)
				}
				if got != nil {
					t.Errorf("expected nil for terminated cluster, got %+v", got)
				}
			})

			t.Run("given a terminated and an active cluster with the same name, when GetCluster, then the active one is returned", func(t *testing.T) {
				active := &domain.Cluster{ID: "c-new", Name: "my-cluster", Status: domain.ClusterStatusUp}
				if err := s.CreateCluster(active); err != nil {
					t.Fatalf("CreateCluster (active): %v", err)
				}

				got, err := s.GetCluster("my-cluster")
				if err != nil {
					t.Fatalf("GetCluster: %v", err)
				}
				if got == nil {
					t.Fatal("expected active cluster, got nil")
				}
				mustEqual(t, "cluster ID", got.ID, "c-new")
				mustEqual(t, "cluster status", got.Status, domain.ClusterStatusUp)
			})
		})
	}
}

func TestStateStore_GetClusterSkipsTerminating(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			terminating := &domain.Cluster{ID: "c-dying", Name: "dying-cluster", Status: domain.ClusterStatusTerminating}
			if err := s.CreateCluster(terminating); err != nil {
				t.Fatalf("CreateCluster: %v", err)
			}

			got, err := s.GetCluster("dying-cluster")
			if err != nil {
				t.Fatalf("GetCluster: %v", err)
			}
			if got != nil {
				t.Errorf("expected nil for terminating cluster, got %+v", got)
			}
		})
	}
}

func TestStateStore_DuplicateClusterName(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			c1 := &domain.Cluster{ID: "c-1", Name: "same-name", Status: domain.ClusterStatusUp}
			c2 := &domain.Cluster{ID: "c-2", Name: "same-name", Status: domain.ClusterStatusInit}

			if err := s.CreateCluster(c1); err != nil {
				t.Fatalf("CreateCluster c1: %v", err)
			}

			t.Run("given a cluster exists, when creating another with the same name but different ID, then it succeeds", func(t *testing.T) {
				if err := s.CreateCluster(c2); err != nil {
					t.Fatalf("CreateCluster c2: %v", err)
				}
			})
		})
	}
}

func TestStateStore_DuplicateClusterID(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			c1 := &domain.Cluster{ID: "c-dup", Name: "cluster-a"}
			if err := s.CreateCluster(c1); err != nil {
				t.Fatalf("CreateCluster c1: %v", err)
			}

			t.Run("given a cluster exists, when creating with the same ID, then an error is returned", func(t *testing.T) {
				c2 := &domain.Cluster{ID: "c-dup", Name: "cluster-b"}
				if err := s.CreateCluster(c2); err == nil {
					t.Fatal("expected error for duplicate cluster ID, got nil")
				}
			})
		})
	}
}

func TestStateStore_ListClusters(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			t.Run("given no clusters, when listing, then empty slice is returned", func(t *testing.T) {
				clusters, err := s.ListClusters()
				if err != nil {
					t.Fatalf("ListClusters: %v", err)
				}
				if len(clusters) != 0 {
					t.Errorf("expected 0, got %d", len(clusters))
				}
			})

			t.Run("given multiple clusters, when listing, then all are returned including terminated", func(t *testing.T) {
				for _, c := range []*domain.Cluster{
					{ID: "c-a", Name: "alpha", Status: domain.ClusterStatusUp},
					{ID: "c-b", Name: "beta", Status: domain.ClusterStatusTerminated},
					{ID: "c-c", Name: "gamma", Status: domain.ClusterStatusInit},
				} {
					if err := s.CreateCluster(c); err != nil {
						t.Fatalf("CreateCluster %s: %v", c.ID, err)
					}
				}

				clusters, err := s.ListClusters()
				if err != nil {
					t.Fatalf("ListClusters: %v", err)
				}
				if len(clusters) != 3 {
					t.Fatalf("expected 3, got %d", len(clusters))
				}

				found := map[string]bool{}
				for _, c := range clusters {
					found[c.ID] = true
				}
				for _, id := range []string{"c-a", "c-b", "c-c"} {
					if !found[id] {
						t.Errorf("expected %s in list", id)
					}
				}
			})
		})
	}
}

func TestStateStore_UpdateCluster(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			original := &domain.Cluster{
				ID:       "c-1",
				Name:     "update-me",
				Status:   domain.ClusterStatusInit,
				Cloud:    domain.CloudAWS,
				NumNodes: 1,
				UserID:   "user-1",
			}
			if err := s.CreateCluster(original); err != nil {
				t.Fatalf("CreateCluster: %v", err)
			}

			original.Status = domain.ClusterStatusUp
			original.HeadIP = "10.128.0.2"
			original.NumNodes = 4
			original.Resources = &domain.Resources{InstanceType: "p4d.24xlarge", UseSpot: true}

			if err := s.UpdateCluster(original); err != nil {
				t.Fatalf("UpdateCluster: %v", err)
			}

			t.Run("given an updated cluster, when reading it back, then all fields reflect the update", func(t *testing.T) {
				got, err := s.GetClusterByID("c-1")
				if err != nil {
					t.Fatalf("GetClusterByID: %v", err)
				}
				mustEqual(t, "cluster", got, original)
			})
		})
	}
}

// --- Job tests ---

func TestStateStore_CreateAndGetJob(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			now := time.Now().UTC().Truncate(time.Second)
			started := now.Add(10 * time.Second)
			ended := now.Add(5 * time.Minute)

			job := &domain.Job{
				ID:          "j-1",
				ClusterID:   "c-1",
				ClusterName: "train-cluster",
				Name:        "training-run",
				Status:      domain.JobStatusSucceeded,
				UserID:      "user-abc",
				SubmittedAt: now,
				StartedAt:   &started,
				EndedAt:     &ended,
			}

			if err := s.CreateJob(job); err != nil {
				t.Fatalf("CreateJob: %v", err)
			}

			t.Run("given a created job, when GetJob, then full struct is returned", func(t *testing.T) {
				got, err := s.GetJob("j-1")
				if err != nil {
					t.Fatalf("GetJob: %v", err)
				}
				if got == nil {
					t.Fatal("GetJob returned nil")
				}
				mustEqual(t, "job", got, job)
			})
		})
	}
}

func TestStateStore_GetJobNotFound(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			got, err := s.GetJob("ghost")
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			if got != nil {
				t.Errorf("expected nil, got %+v", got)
			}
		})
	}
}

func TestStateStore_DuplicateJobID(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			j1 := &domain.Job{ID: "j-dup", ClusterID: "c-1", Status: domain.JobStatusPending}
			if err := s.CreateJob(j1); err != nil {
				t.Fatalf("CreateJob j1: %v", err)
			}

			j2 := &domain.Job{ID: "j-dup", ClusterID: "c-2", Status: domain.JobStatusRunning}
			if err := s.CreateJob(j2); err == nil {
				t.Fatal("expected error for duplicate job ID, got nil")
			}
		})
	}
}

func TestStateStore_ListJobsByCluster(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			for _, j := range []*domain.Job{
				{ID: "j-a1", ClusterID: "c-alpha", Status: domain.JobStatusPending},
				{ID: "j-a2", ClusterID: "c-alpha", Status: domain.JobStatusRunning},
				{ID: "j-b1", ClusterID: "c-beta", Status: domain.JobStatusSucceeded},
			} {
				if err := s.CreateJob(j); err != nil {
					t.Fatalf("CreateJob %s: %v", j.ID, err)
				}
				time.Sleep(10 * time.Millisecond)
			}

			t.Run("given jobs in two clusters, when listing by cluster, then only that clusters jobs are returned", func(t *testing.T) {
				alpha, err := s.ListJobs("c-alpha")
				if err != nil {
					t.Fatalf("ListJobs c-alpha: %v", err)
				}
				if len(alpha) != 2 {
					t.Fatalf("expected 2 jobs for c-alpha, got %d", len(alpha))
				}
				for _, j := range alpha {
					mustEqual(t, "cluster_id", j.ClusterID, "c-alpha")
				}

				found := map[string]bool{}
				for _, j := range alpha {
					found[j.ID] = true
				}
				if !found["j-a1"] || !found["j-a2"] {
					t.Errorf("expected j-a1 and j-a2, got %v", found)
				}
			})

			t.Run("given no jobs for a cluster, when listing, then empty slice is returned", func(t *testing.T) {
				empty, err := s.ListJobs("c-nonexistent")
				if err != nil {
					t.Fatalf("ListJobs: %v", err)
				}
				if len(empty) != 0 {
					t.Errorf("expected 0, got %d", len(empty))
				}
			})
		})
	}
}

func TestStateStore_ListAllJobs(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			t.Run("given no jobs, when listing all, then empty slice is returned", func(t *testing.T) {
				jobs, err := s.ListAllJobs()
				if err != nil {
					t.Fatalf("ListAllJobs: %v", err)
				}
				if len(jobs) != 0 {
					t.Errorf("expected 0, got %d", len(jobs))
				}
			})

			t.Run("given jobs across clusters, when listing all, then all are returned", func(t *testing.T) {
				for _, j := range []*domain.Job{
					{ID: "j-1", ClusterID: "c-a", Status: domain.JobStatusPending},
					{ID: "j-2", ClusterID: "c-b", Status: domain.JobStatusRunning},
					{ID: "j-3", ClusterID: "c-a", Status: domain.JobStatusFailed},
				} {
					if err := s.CreateJob(j); err != nil {
						t.Fatalf("CreateJob %s: %v", j.ID, err)
					}
				}

				jobs, err := s.ListAllJobs()
				if err != nil {
					t.Fatalf("ListAllJobs: %v", err)
				}
				if len(jobs) != 3 {
					t.Fatalf("expected 3, got %d", len(jobs))
				}

				found := map[string]bool{}
				for _, j := range jobs {
					found[j.ID] = true
				}
				for _, id := range []string{"j-1", "j-2", "j-3"} {
					if !found[id] {
						t.Errorf("expected %s in list", id)
					}
				}
			})
		})
	}
}

func TestStateStore_UpdateJob(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			now := time.Now().UTC().Truncate(time.Second)
			job := &domain.Job{
				ID:          "j-1",
				ClusterID:   "c-1",
				ClusterName: "cluster-a",
				Name:        "my-job",
				Status:      domain.JobStatusPending,
				UserID:      "user-1",
				SubmittedAt: now,
			}
			if err := s.CreateJob(job); err != nil {
				t.Fatalf("CreateJob: %v", err)
			}

			started := now.Add(5 * time.Second)
			job.Status = domain.JobStatusRunning
			job.StartedAt = &started
			if err := s.UpdateJob(job); err != nil {
				t.Fatalf("UpdateJob: %v", err)
			}

			t.Run("given an updated job, when reading it back, then all fields reflect the update", func(t *testing.T) {
				got, err := s.GetJob("j-1")
				if err != nil {
					t.Fatalf("GetJob: %v", err)
				}
				mustEqual(t, "job", got, job)
			})

			t.Run("given a completed job, when updating again, then all fields including EndedAt are persisted", func(t *testing.T) {
				ended := now.Add(10 * time.Minute)
				job.Status = domain.JobStatusSucceeded
				job.EndedAt = &ended
				if err := s.UpdateJob(job); err != nil {
					t.Fatalf("UpdateJob: %v", err)
				}

				got, err := s.GetJob("j-1")
				if err != nil {
					t.Fatalf("GetJob: %v", err)
				}
				mustEqual(t, "job", got, job)
			})
		})
	}
}

// --- Full lifecycle tests ---

func TestStateStore_ClusterLifecycle(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			now := time.Now().UTC().Truncate(time.Second)
			cluster := &domain.Cluster{
				ID:         "c-lc",
				Name:       "lifecycle",
				Status:     domain.ClusterStatusInit,
				Cloud:      domain.CloudAWS,
				Region:     "us-west-2",
				NumNodes:   1,
				UserID:     "user-1",
				LaunchedAt: now,
			}

			// Create
			if err := s.CreateCluster(cluster); err != nil {
				t.Fatalf("CreateCluster: %v", err)
			}

			// Transition to UP
			cluster.Status = domain.ClusterStatusUp
			cluster.HeadIP = "10.0.0.1"
			if err := s.UpdateCluster(cluster); err != nil {
				t.Fatalf("UpdateCluster to UP: %v", err)
			}

			got, err := s.GetCluster("lifecycle")
			if err != nil {
				t.Fatalf("GetCluster after UP: %v", err)
			}
			mustEqual(t, "cluster after UP", got, cluster)

			// Transition to TERMINATING
			cluster.Status = domain.ClusterStatusTerminating
			if err := s.UpdateCluster(cluster); err != nil {
				t.Fatalf("UpdateCluster to TERMINATING: %v", err)
			}

			got, err = s.GetCluster("lifecycle")
			if err != nil {
				t.Fatalf("GetCluster after TERMINATING: %v", err)
			}
			if got != nil {
				t.Error("expected nil from GetCluster for TERMINATING cluster")
			}

			// GetClusterByID still returns it regardless of status
			got, err = s.GetClusterByID("c-lc")
			if err != nil {
				t.Fatalf("GetClusterByID after TERMINATING: %v", err)
			}
			mustEqual(t, "cluster by ID after TERMINATING", got, cluster)

			// Transition to TERMINATED
			cluster.Status = domain.ClusterStatusTerminated
			if err := s.UpdateCluster(cluster); err != nil {
				t.Fatalf("UpdateCluster to TERMINATED: %v", err)
			}

			got, err = s.GetCluster("lifecycle")
			if err != nil {
				t.Fatalf("GetCluster after TERMINATED: %v", err)
			}
			if got != nil {
				t.Error("expected nil from GetCluster for TERMINATED cluster")
			}

			// ListClusters still includes terminated clusters
			all, err := s.ListClusters()
			if err != nil {
				t.Fatalf("ListClusters: %v", err)
			}
			if len(all) != 1 {
				t.Fatalf("expected 1 cluster in list, got %d", len(all))
			}
			mustEqual(t, "listed cluster status", all[0].Status, domain.ClusterStatusTerminated)

			// Reuse name: create a new cluster with same name
			cluster2 := &domain.Cluster{
				ID:         "c-lc-2",
				Name:       "lifecycle",
				Status:     domain.ClusterStatusUp,
				LaunchedAt: now.Add(time.Hour),
			}
			if err := s.CreateCluster(cluster2); err != nil {
				t.Fatalf("CreateCluster (reuse name): %v", err)
			}

			got, err = s.GetCluster("lifecycle")
			if err != nil {
				t.Fatalf("GetCluster after reuse: %v", err)
			}
			if got == nil {
				t.Fatal("expected new cluster, got nil")
			}
			mustEqual(t, "reused cluster ID", got.ID, "c-lc-2")
		})
	}
}

func TestStateStore_JobLifecycle(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			now := time.Now().UTC().Truncate(time.Second)
			job := &domain.Job{
				ID:          "j-lc",
				ClusterID:   "c-1",
				ClusterName: "cluster-a",
				Name:        "training",
				Status:      domain.JobStatusPending,
				UserID:      "user-1",
				SubmittedAt: now,
			}

			if err := s.CreateJob(job); err != nil {
				t.Fatalf("CreateJob: %v", err)
			}

			// Pending -> Running
			started := now.Add(5 * time.Second)
			job.Status = domain.JobStatusRunning
			job.StartedAt = &started
			if err := s.UpdateJob(job); err != nil {
				t.Fatalf("UpdateJob to RUNNING: %v", err)
			}

			got, err := s.GetJob("j-lc")
			if err != nil {
				t.Fatalf("GetJob after RUNNING: %v", err)
			}
			mustEqual(t, "job after RUNNING", got, job)

			// Running -> Succeeded
			ended := now.Add(10 * time.Minute)
			job.Status = domain.JobStatusSucceeded
			job.EndedAt = &ended
			if err := s.UpdateJob(job); err != nil {
				t.Fatalf("UpdateJob to SUCCEEDED: %v", err)
			}

			got, err = s.GetJob("j-lc")
			if err != nil {
				t.Fatalf("GetJob after SUCCEEDED: %v", err)
			}
			mustEqual(t, "job after SUCCEEDED", got, job)

			// Verify it appears in ListJobs and ListAllJobs
			jobs, err := s.ListJobs("c-1")
			if err != nil {
				t.Fatalf("ListJobs: %v", err)
			}
			if len(jobs) != 1 {
				t.Fatalf("expected 1 job, got %d", len(jobs))
			}
			mustEqual(t, "listed job", jobs[0], job)

			all, err := s.ListAllJobs()
			if err != nil {
				t.Fatalf("ListAllJobs: %v", err)
			}
			if len(all) != 1 {
				t.Fatalf("expected 1 job, got %d", len(all))
			}
			mustEqual(t, "all jobs[0]", all[0], job)
		})
	}
}

// --- Roundtrip tests ---

func TestStateStore_ClusterRoundtripPreservesAllFields(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			now := time.Now().UTC().Truncate(time.Second)
			cluster := &domain.Cluster{
				ID:       "c-rt",
				Name:     "roundtrip",
				Status:   domain.ClusterStatusUp,
				Cloud:    domain.CloudAWS,
				Region:   "ap-southeast-1",
				Zone:     "ap-southeast-1b",
				NumNodes: 8,
				HeadIP:   "172.16.0.1",
				UserID:   "user-xyz",
				Resources: &domain.Resources{
					Cloud:        domain.CloudAWS,
					Region:       "ap-southeast-1",
					Zone:         "ap-southeast-1b",
					Accelerators: "H100:8",
					CPUs:         "96",
					Memory:       "1024",
					InstanceType: "p5.48xlarge",
					UseSpot:      false,
					DiskSizeGB:   2000,
					Ports:        []string{"22", "8080", "6443"},
					ImageID:      "ami-12345678",
				},
				LaunchedAt:      now,
				AutostopMinutes: 120,
				WorkdirID:       "wd-abc",
			}

			if err := s.CreateCluster(cluster); err != nil {
				t.Fatalf("CreateCluster: %v", err)
			}

			got, err := s.GetClusterByID("c-rt")
			if err != nil {
				t.Fatalf("GetClusterByID: %v", err)
			}
			if got == nil {
				t.Fatal("got nil")
			}

			mustEqual(t, "ID", got.ID, cluster.ID)
			mustEqual(t, "Name", got.Name, cluster.Name)
			mustEqual(t, "Status", got.Status, cluster.Status)
			mustEqual(t, "Cloud", got.Cloud, cluster.Cloud)
			mustEqual(t, "Region", got.Region, cluster.Region)
			mustEqual(t, "Zone", got.Zone, cluster.Zone)
			mustEqual(t, "NumNodes", got.NumNodes, cluster.NumNodes)
			mustEqual(t, "HeadIP", got.HeadIP, cluster.HeadIP)
			mustEqual(t, "UserID", got.UserID, cluster.UserID)
			mustEqual(t, "LaunchedAt", got.LaunchedAt, cluster.LaunchedAt)
			mustEqual(t, "AutostopMinutes", got.AutostopMinutes, cluster.AutostopMinutes)
			mustEqual(t, "WorkdirID", got.WorkdirID, cluster.WorkdirID)
			mustEqual(t, "Resources.Cloud", got.Resources.Cloud, cluster.Resources.Cloud)
			mustEqual(t, "Resources.Region", got.Resources.Region, cluster.Resources.Region)
			mustEqual(t, "Resources.Zone", got.Resources.Zone, cluster.Resources.Zone)
			mustEqual(t, "Resources.Accelerators", got.Resources.Accelerators, cluster.Resources.Accelerators)
			mustEqual(t, "Resources.CPUs", got.Resources.CPUs, cluster.Resources.CPUs)
			mustEqual(t, "Resources.Memory", got.Resources.Memory, cluster.Resources.Memory)
			mustEqual(t, "Resources.InstanceType", got.Resources.InstanceType, cluster.Resources.InstanceType)
			mustEqual(t, "Resources.UseSpot", got.Resources.UseSpot, cluster.Resources.UseSpot)
			mustEqual(t, "Resources.DiskSizeGB", got.Resources.DiskSizeGB, cluster.Resources.DiskSizeGB)
			mustEqual(t, "Resources.Ports", got.Resources.Ports, cluster.Resources.Ports)
			mustEqual(t, "Resources.ImageID", got.Resources.ImageID, cluster.Resources.ImageID)
		})
	}
}

func TestStateStore_JobRoundtripPreservesAllFields(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			now := time.Now().UTC().Truncate(time.Second)
			started := now.Add(3 * time.Second)
			ended := now.Add(7 * time.Minute)

			job := &domain.Job{
				ID:          "j-rt",
				ClusterID:   "c-rt",
				ClusterName: "roundtrip-cluster",
				Name:        "eval-run",
				Status:      domain.JobStatusFailed,
				UserID:      "user-xyz",
				SubmittedAt: now,
				StartedAt:   &started,
				EndedAt:     &ended,
			}

			if err := s.CreateJob(job); err != nil {
				t.Fatalf("CreateJob: %v", err)
			}

			got, err := s.GetJob("j-rt")
			if err != nil {
				t.Fatalf("GetJob: %v", err)
			}
			if got == nil {
				t.Fatal("got nil")
			}

			mustEqual(t, "ID", got.ID, job.ID)
			mustEqual(t, "ClusterID", got.ClusterID, job.ClusterID)
			mustEqual(t, "ClusterName", got.ClusterName, job.ClusterName)
			mustEqual(t, "Name", got.Name, job.Name)
			mustEqual(t, "Status", got.Status, job.Status)
			mustEqual(t, "UserID", got.UserID, job.UserID)
			mustEqual(t, "SubmittedAt", got.SubmittedAt, job.SubmittedAt)
			mustEqual(t, "StartedAt", got.StartedAt, job.StartedAt)
			mustEqual(t, "EndedAt", got.EndedAt, job.EndedAt)
		})
	}
}

func TestStateStore_NilOptionalFields(t *testing.T) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) {
			s := b.setup(t)

			t.Run("given a cluster with nil Resources, when round-tripped, then Resources remains nil", func(t *testing.T) {
				cluster := &domain.Cluster{ID: "c-nil", Name: "nil-resources", Status: domain.ClusterStatusInit}
				if err := s.CreateCluster(cluster); err != nil {
					t.Fatalf("CreateCluster: %v", err)
				}

				got, err := s.GetClusterByID("c-nil")
				if err != nil {
					t.Fatalf("GetClusterByID: %v", err)
				}
				if got.Resources != nil {
					t.Errorf("expected nil Resources, got %+v", got.Resources)
				}
			})

			t.Run("given a job with nil StartedAt and EndedAt, when round-tripped, then they remain nil", func(t *testing.T) {
				job := &domain.Job{ID: "j-nil", ClusterID: "c-1", Status: domain.JobStatusPending}
				if err := s.CreateJob(job); err != nil {
					t.Fatalf("CreateJob: %v", err)
				}

				got, err := s.GetJob("j-nil")
				if err != nil {
					t.Fatalf("GetJob: %v", err)
				}
				if got.StartedAt != nil {
					t.Errorf("expected nil StartedAt, got %v", got.StartedAt)
				}
				if got.EndedAt != nil {
					t.Errorf("expected nil EndedAt, got %v", got.EndedAt)
				}
			})
		})
	}
}
