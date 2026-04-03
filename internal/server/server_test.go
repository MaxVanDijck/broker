package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/coder/websocket"

	"broker/internal/domain"
	"broker/internal/provider"
	"broker/internal/store"
	"broker/internal/tunnel"
	agentpb "broker/proto/agentpb"
	brokerpb "broker/proto/brokerpb"
	"broker/proto/brokerpb/brokerpbconnect"
)

type testEnv struct {
	server     *Server
	httpServer *httptest.Server
	client     brokerpbconnect.BrokerServiceClient
	store      *store.SQLStore
}

func setupTestEnv(t *testing.T) *testEnv {
	t.Helper()

	db, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	logger := slog.Default()
	registry := provider.NewRegistry()

	srv := New(db, nil, registry, logger)

	mux := http.NewServeMux()
	path, handler := brokerpbconnect.NewBrokerServiceHandler(srv)
	mux.Handle(path, handler)
	mux.Handle("/agent/v1/connect", srv.Tunnel)

	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)

	rpcClient := brokerpbconnect.NewBrokerServiceClient(http.DefaultClient, hs.URL)

	return &testEnv{
		server:     srv,
		httpServer: hs,
		client:     rpcClient,
		store:      db,
	}
}

func connectAgent(t *testing.T, env *testEnv, nodeID, clusterName string) *tunnel.Tunnel {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(env.httpServer.URL, "http") + "/agent/v1/connect"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}

	tun := tunnel.New(conn, slog.Default())
	t.Cleanup(func() { tun.Close() })

	if err := tun.Send(ctx, &agentpb.Envelope{
		Payload: &agentpb.Envelope_Register{
			Register: &agentpb.Register{
				NodeId:      nodeID,
				ClusterName: clusterName,
				NodeInfo: &agentpb.NodeInfo{
					Hostname: "test-host",
					Os:       "linux",
					Arch:     "amd64",
				},
			},
		},
	}); err != nil {
		t.Fatalf("send register: %v", err)
	}

	ack, err := tun.Recv(ctx)
	if err != nil {
		t.Fatalf("recv register ack: %v", err)
	}
	regAck := ack.GetRegisterAck()
	if regAck == nil || !regAck.Accepted {
		t.Fatalf("registration not accepted: %v", ack)
	}

	return tun
}

func TestExecDispatchesToAgent(t *testing.T) {
	// Given a server with a connected agent for cluster "test-cluster"
	env := setupTestEnv(t)

	err := env.store.CreateCluster(&domain.Cluster{
		ID:     "c-1",
		Name:   "test-cluster",
		Status: domain.ClusterStatusUp,
		UserID: "default",
	})
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}

	agentTun := connectAgent(t, env, "node-1", "test-cluster")

	// Small delay to ensure the agent registration completes in the server's
	// readLoop goroutine before we issue the Exec call.
	time.Sleep(50 * time.Millisecond)

	// When a job is submitted via Exec
	ctx := context.Background()
	resp, err := env.client.Exec(ctx, connect.NewRequest(&brokerpb.ExecRequest{
		ClusterName: "test-cluster",
		Task: &brokerpb.TaskSpec{
			Name: "test-job",
			Run:  "echo hello",
		},
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	jobID := resp.Msg.JobId
	if jobID == "" {
		t.Fatal("expected non-empty job ID")
	}

	// Then the agent receives a SubmitJob envelope
	recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
	defer recvCancel()

	env2, err := agentTun.Recv(recvCtx)
	if err != nil {
		t.Fatalf("agent recv: %v", err)
	}

	submitJob := env2.GetSubmitJob()
	if submitJob == nil {
		t.Fatalf("expected SubmitJob, got %T", env2.Payload)
	}
	if submitJob.JobId != jobID {
		t.Errorf("expected job_id %s, got %s", jobID, submitJob.JobId)
	}
	if submitJob.RunScript != "echo hello" {
		t.Errorf("expected run_script 'echo hello', got %s", submitJob.RunScript)
	}
	if submitJob.Name != "test-job" {
		t.Errorf("expected name 'test-job', got %s", submitJob.Name)
	}

	// Then the job is stored in pending state
	job, err := env.store.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job == nil {
		t.Fatal("job not found in store")
	}
	if job.Status != domain.JobStatusPending {
		t.Errorf("expected job status PENDING, got %s", job.Status)
	}
	if job.ClusterID != "c-1" {
		t.Errorf("expected cluster_id c-1, got %s", job.ClusterID)
	}
}

func TestJobUpdateFlowsBackToServer(t *testing.T) {
	// Given a server with a connected agent and a dispatched job
	env := setupTestEnv(t)

	err := env.store.CreateCluster(&domain.Cluster{
		ID:     "c-1",
		Name:   "test-cluster",
		Status: domain.ClusterStatusUp,
		UserID: "default",
	})
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}

	agentTun := connectAgent(t, env, "node-1", "test-cluster")
	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	resp, err := env.client.Exec(ctx, connect.NewRequest(&brokerpb.ExecRequest{
		ClusterName: "test-cluster",
		Task: &brokerpb.TaskSpec{
			Run: "echo hello",
		},
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	jobID := resp.Msg.JobId

	// Drain the SubmitJob message that the agent receives
	recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
	defer recvCancel()
	_, err = agentTun.Recv(recvCtx)
	if err != nil {
		t.Fatalf("agent recv submit: %v", err)
	}

	// When the agent sends a RUNNING update
	if err := agentTun.Send(ctx, &agentpb.Envelope{
		Payload: &agentpb.Envelope_JobUpdate{
			JobUpdate: &agentpb.JobUpdate{
				JobId: jobID,
				State: agentpb.JobState_JOB_STATE_RUNNING,
			},
		},
	}); err != nil {
		t.Fatalf("send running update: %v", err)
	}

	// Allow time for the server to process the update
	time.Sleep(100 * time.Millisecond)

	// Then the job state is updated to RUNNING in the store
	job, err := env.store.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != domain.JobStatusRunning {
		t.Errorf("expected job status RUNNING, got %s", job.Status)
	}
	if job.StartedAt == nil {
		t.Error("expected StartedAt to be set")
	}

	// When the agent sends a SUCCEEDED update
	if err := agentTun.Send(ctx, &agentpb.Envelope{
		Payload: &agentpb.Envelope_JobUpdate{
			JobUpdate: &agentpb.JobUpdate{
				JobId: jobID,
				State: agentpb.JobState_JOB_STATE_SUCCEEDED,
			},
		},
	}); err != nil {
		t.Fatalf("send succeeded update: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Then the job state is updated to SUCCEEDED in the store
	job, err = env.store.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != domain.JobStatusSucceeded {
		t.Errorf("expected job status SUCCEEDED, got %s", job.Status)
	}
	if job.EndedAt == nil {
		t.Error("expected EndedAt to be set")
	}
}

func TestLaunchCreatesClusterAndJob(t *testing.T) {
	// Given a server with a connected agent for cluster "launch-test"
	env := setupTestEnv(t)

	agentTun := connectAgent(t, env, "node-launch", "launch-test")
	time.Sleep(50 * time.Millisecond)

	// When a launch request is made
	ctx := context.Background()
	resp, err := env.client.Launch(ctx, connect.NewRequest(&brokerpb.LaunchRequest{
		ClusterName: "launch-test",
		Task: &brokerpb.TaskSpec{
			Name:  "train",
			Setup: "pip install torch",
			Run:   "python train.py",
			Envs:  map[string]string{"CUDA_VISIBLE_DEVICES": "0"},
		},
	}))
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	if resp.Msg.ClusterName != "launch-test" {
		t.Errorf("expected cluster name 'launch-test', got %s", resp.Msg.ClusterName)
	}
	if resp.Msg.JobId == "" {
		t.Fatal("expected non-empty job ID in launch response")
	}

	// Then a cluster is created in the store
	cluster, err := env.store.GetCluster("launch-test")
	if err != nil {
		t.Fatalf("get cluster: %v", err)
	}
	if cluster == nil {
		t.Fatal("cluster not found in store")
	}
	if cluster.ID == "" {
		t.Fatal("expected non-empty cluster ID")
	}

	// Then a job is created in the store
	job, err := env.store.GetJob(resp.Msg.JobId)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job == nil {
		t.Fatal("job not found in store")
	}
	if job.Status != domain.JobStatusPending {
		t.Errorf("expected job status PENDING, got %s", job.Status)
	}
	if job.Name != "train" {
		t.Errorf("expected job name 'train', got %s", job.Name)
	}
	if job.ClusterID != cluster.ID {
		t.Errorf("expected job cluster_id %s, got %s", cluster.ID, job.ClusterID)
	}

	// Then the agent receives the SubmitJob
	recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
	defer recvCancel()

	env2, err := agentTun.Recv(recvCtx)
	if err != nil {
		t.Fatalf("agent recv: %v", err)
	}

	submitJob := env2.GetSubmitJob()
	if submitJob == nil {
		t.Fatalf("expected SubmitJob, got %T", env2.Payload)
	}
	if submitJob.JobId != resp.Msg.JobId {
		t.Errorf("expected job_id %s, got %s", resp.Msg.JobId, submitJob.JobId)
	}
	if submitJob.SetupScript != "pip install torch" {
		t.Errorf("expected setup_script 'pip install torch', got %s", submitJob.SetupScript)
	}
	if submitJob.RunScript != "python train.py" {
		t.Errorf("expected run_script 'python train.py', got %s", submitJob.RunScript)
	}
	if v, ok := submitJob.Env["CUDA_VISIBLE_DEVICES"]; !ok || v != "0" {
		t.Errorf("expected env CUDA_VISIBLE_DEVICES=0, got %v", submitJob.Env)
	}
}

func TestExecWithNoAgentStillCreatesJob(t *testing.T) {
	// Given a server with a cluster but no connected agent
	env := setupTestEnv(t)

	err := env.store.CreateCluster(&domain.Cluster{
		ID:     "c-1",
		Name:   "orphan-cluster",
		Status: domain.ClusterStatusUp,
		UserID: "default",
	})
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}

	// When a job is submitted via Exec
	ctx := context.Background()
	resp, err := env.client.Exec(ctx, connect.NewRequest(&brokerpb.ExecRequest{
		ClusterName: "orphan-cluster",
		Task: &brokerpb.TaskSpec{
			Run: "echo orphan",
		},
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}

	// Then the job is still created in the store (dispatch failure is non-fatal)
	job, err := env.store.GetJob(resp.Msg.JobId)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job == nil {
		t.Fatal("job not found in store despite no agent")
	}
	if job.Status != domain.JobStatusPending {
		t.Errorf("expected job status PENDING, got %s", job.Status)
	}
}

func TestExecWithNonexistentClusterReturnsNotFound(t *testing.T) {
	// Given a server with no clusters
	env := setupTestEnv(t)

	// When exec is called for a nonexistent cluster
	ctx := context.Background()
	_, err := env.client.Exec(ctx, connect.NewRequest(&brokerpb.ExecRequest{
		ClusterName: "does-not-exist",
		Task: &brokerpb.TaskSpec{
			Run: "echo fail",
		},
	}))

	// Then a not found error is returned
	if err == nil {
		t.Fatal("expected error for nonexistent cluster")
	}
	if connectErr := new(connect.Error); !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestJobFailedUpdateSetsEndedAt(t *testing.T) {
	// Given a server with a connected agent and a dispatched job
	env := setupTestEnv(t)

	err := env.store.CreateCluster(&domain.Cluster{
		ID:     "c-1",
		Name:   "fail-cluster",
		Status: domain.ClusterStatusUp,
		UserID: "default",
	})
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}

	agentTun := connectAgent(t, env, "node-fail", "fail-cluster")
	time.Sleep(50 * time.Millisecond)

	ctx := context.Background()
	resp, err := env.client.Exec(ctx, connect.NewRequest(&brokerpb.ExecRequest{
		ClusterName: "fail-cluster",
		Task:        &brokerpb.TaskSpec{Run: "exit 1"},
	}))
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	jobID := resp.Msg.JobId

	// Drain SubmitJob
	recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
	defer recvCancel()
	_, _ = agentTun.Recv(recvCtx)

	// When the agent sends a FAILED update
	if err := agentTun.Send(ctx, &agentpb.Envelope{
		Payload: &agentpb.Envelope_JobUpdate{
			JobUpdate: &agentpb.JobUpdate{
				JobId:    jobID,
				State:    agentpb.JobState_JOB_STATE_FAILED,
				ExitCode: 1,
				Error:    "exit status 1",
			},
		},
	}); err != nil {
		t.Fatalf("send failed update: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Then the job state is FAILED with EndedAt set
	job, err := env.store.GetJob(jobID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if job.Status != domain.JobStatusFailed {
		t.Errorf("expected job status FAILED, got %s", job.Status)
	}
	if job.EndedAt == nil {
		t.Error("expected EndedAt to be set for failed job")
	}
}

func TestDownTeardownFlow(t *testing.T) {
	// Given a server with a cluster in UP status
	env := setupTestEnv(t)

	err := env.store.CreateCluster(&domain.Cluster{
		ID:     "c-down",
		Name:   "down-cluster",
		Status: domain.ClusterStatusUp,
		UserID: "default",
	})
	if err != nil {
		t.Fatalf("create cluster: %v", err)
	}

	ctx := context.Background()

	// When Down is called for the cluster
	resp, err := env.client.Down(ctx, connect.NewRequest(&brokerpb.ClusterRequest{
		ClusterName: "down-cluster",
	}))
	if err != nil {
		t.Fatalf("down: %v", err)
	}

	// Then the response status is TERMINATING
	if resp.Msg.Status != string(domain.ClusterStatusTerminating) {
		t.Errorf("expected status TERMINATING, got %s", resp.Msg.Status)
	}
	if resp.Msg.ClusterName != "down-cluster" {
		t.Errorf("expected cluster name 'down-cluster', got %s", resp.Msg.ClusterName)
	}

	// Wait for the async teardown goroutine to complete (no cloud provider
	// is registered, so it just transitions TERMINATING -> TERMINATED)
	time.Sleep(200 * time.Millisecond)

	// Then the cluster status in the store is TERMINATED
	cluster, err := env.store.GetClusterByID("c-down")
	if err != nil {
		t.Fatalf("get cluster by id: %v", err)
	}
	if cluster == nil {
		t.Fatal("cluster not found by ID")
	}
	if cluster.Status != domain.ClusterStatusTerminated {
		t.Errorf("expected cluster status TERMINATED, got %s", cluster.Status)
	}

	// When Down is called again for the same cluster name
	_, err = env.client.Down(ctx, connect.NewRequest(&brokerpb.ClusterRequest{
		ClusterName: "down-cluster",
	}))

	// Then a NotFound error is returned (GetCluster skips terminated clusters)
	if err == nil {
		t.Fatal("expected error for terminated cluster")
	}
	if connectErr := new(connect.Error); !errors.As(err, &connectErr) || connectErr.Code() != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", err)
	}
}

func TestGetAgentByCluster(t *testing.T) {
	// Given a tunnel handler with two agents on different clusters
	env := setupTestEnv(t)

	_ = connectAgent(t, env, "node-a", "cluster-a")
	_ = connectAgent(t, env, "node-b", "cluster-b")
	time.Sleep(50 * time.Millisecond)

	// When looking up by cluster name
	ac, ok := env.server.Tunnel.GetAgentByCluster("cluster-a")
	if !ok {
		t.Fatal("expected to find agent for cluster-a")
	}
	if ac.NodeID != "node-a" {
		t.Errorf("expected node-a, got %s", ac.NodeID)
	}

	ac, ok = env.server.Tunnel.GetAgentByCluster("cluster-b")
	if !ok {
		t.Fatal("expected to find agent for cluster-b")
	}
	if ac.NodeID != "node-b" {
		t.Errorf("expected node-b, got %s", ac.NodeID)
	}

	// When looking up a nonexistent cluster
	_, ok = env.server.Tunnel.GetAgentByCluster("cluster-c")
	if ok {
		t.Error("expected no agent for cluster-c")
	}
}

type stubAnalyticsStore struct {
	store.NoopAnalyticsStore
	metrics []store.MetricPoint
}

func (s *stubAnalyticsStore) InsertMetrics(_ context.Context, points []store.MetricPoint) error {
	s.metrics = append(s.metrics, points...)
	return nil
}

func (s *stubAnalyticsStore) QueryMetricsByCluster(_ context.Context, clusterID string, tr store.TimeRange) ([]store.MetricPoint, error) {
	var result []store.MetricPoint
	for _, p := range s.metrics {
		if p.ClusterID == clusterID && !p.Timestamp.Before(tr.From) && !p.Timestamp.After(tr.To) {
			result = append(result, p)
		}
	}
	return result, nil
}

func TestHandleClusterMetrics(t *testing.T) {
	t.Run("given metrics exist, when querying by cluster, then matching metrics are returned as json", func(t *testing.T) {
		analytics := &stubAnalyticsStore{}
		now := time.Now().UTC().Truncate(time.Second)

		analytics.metrics = []store.MetricPoint{
			{
				Timestamp:     now.Add(-30 * time.Minute),
				NodeID:        "node-1",
				ClusterID:     "cid-1",
				CPUPercent:    42.5,
				MemoryPercent: 65.2,
				DiskUsedBytes: 1024 * 1024 * 100,
			},
			{
				Timestamp:     now.Add(-15 * time.Minute),
				NodeID:        "node-1",
				ClusterID:     "cid-1",
				CPUPercent:    55.0,
				MemoryPercent: 70.1,
				DiskUsedBytes: 1024 * 1024 * 110,
			},
			{
				Timestamp:     now.Add(-10 * time.Minute),
				NodeID:        "node-2",
				ClusterID:     "cid-2",
				CPUPercent:    20.0,
				MemoryPercent: 30.0,
				DiskUsedBytes: 1024 * 1024 * 50,
			},
		}

		// Create a state store with the cluster so the server can resolve name -> ID
		stateStore, err := store.NewSQLite(":memory:")
		if err != nil {
			t.Fatalf("create sqlite: %v", err)
		}
		defer stateStore.Close()
		stateStore.CreateCluster(&domain.Cluster{ID: "cid-1", Name: "test-cluster", Status: domain.ClusterStatusUp})

		srv := &Server{
			store:     stateStore,
			analytics: analytics,
			logger:    slog.Default(),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/clusters/", srv.handleClusterSubroutes)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		url := fmt.Sprintf("%s/api/v1/clusters/test-cluster/metrics", ts.URL)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body metricsResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode json: %v", err)
		}

		if body.ClusterName != "test-cluster" {
			t.Errorf("expected cluster_name test-cluster, got %s", body.ClusterName)
		}
		if len(body.Points) != 2 {
			t.Fatalf("expected 2 points, got %d", len(body.Points))
		}
		if body.Points[0].CPUPercent != 42.5 {
			t.Errorf("expected first point cpu 42.5, got %f", body.Points[0].CPUPercent)
		}
		if body.Points[1].CPUPercent != 55.0 {
			t.Errorf("expected second point cpu 55.0, got %f", body.Points[1].CPUPercent)
		}
	})

	t.Run("given no metrics exist, when querying, then empty points array is returned", func(t *testing.T) {
		analytics := &stubAnalyticsStore{}
		ss, _ := store.NewSQLite(":memory:")
		defer ss.Close()
		ss.CreateCluster(&domain.Cluster{ID: "empty-id", Name: "empty-cluster", Status: domain.ClusterStatusUp})

		srv := &Server{
			store:     ss,
			analytics: analytics,
			logger:    slog.Default(),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/clusters/", srv.handleClusterSubroutes)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		url := fmt.Sprintf("%s/api/v1/clusters/empty-cluster/metrics", ts.URL)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body metricsResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode json: %v", err)
		}

		if len(body.Points) != 0 {
			t.Errorf("expected 0 points, got %d", len(body.Points))
		}
	})

	t.Run("given metrics exist, when querying with from and to params, then only matching time range is returned", func(t *testing.T) {
		analytics := &stubAnalyticsStore{}
		now := time.Now().UTC().Truncate(time.Second)

		analytics.metrics = []store.MetricPoint{
			{
				Timestamp:  now.Add(-2 * time.Hour),
				NodeID:     "node-1",
				ClusterID:  "cid-1",
				CPUPercent: 10.0,
			},
			{
				Timestamp:  now.Add(-30 * time.Minute),
				NodeID:     "node-1",
				ClusterID:  "cid-1",
				CPUPercent: 50.0,
			},
		}

		stateStore, err := store.NewSQLite(":memory:")
		if err != nil {
			t.Fatalf("create sqlite: %v", err)
		}
		defer stateStore.Close()
		stateStore.CreateCluster(&domain.Cluster{ID: "cid-1", Name: "test-cluster", Status: domain.ClusterStatusUp})

		srv := &Server{
			store:     stateStore,
			analytics: analytics,
			logger:    slog.Default(),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/clusters/", srv.handleClusterSubroutes)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		from := now.Add(-1 * time.Hour).Unix()
		to := now.Unix()
		url := fmt.Sprintf("%s/api/v1/clusters/test-cluster/metrics?from=%d&to=%d", ts.URL, from, to)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body metricsResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode json: %v", err)
		}

		if len(body.Points) != 1 {
			t.Fatalf("expected 1 point within time range, got %d", len(body.Points))
		}
		if body.Points[0].CPUPercent != 50.0 {
			t.Errorf("expected cpu 50.0, got %f", body.Points[0].CPUPercent)
		}
	})

	t.Run("given invalid path, when requesting, then 404 is returned", func(t *testing.T) {
		analytics := &stubAnalyticsStore{}
		ss2, _ := store.NewSQLite(":memory:")
		defer ss2.Close()
		ss2.CreateCluster(&domain.Cluster{ID: "tc-id", Name: "test-cluster", Status: domain.ClusterStatusUp})

		srv := &Server{
			store:     ss2,
			analytics: analytics,
			logger:    slog.Default(),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/clusters/", srv.handleClusterSubroutes)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		url := fmt.Sprintf("%s/api/v1/clusters/test-cluster/invalid", ts.URL)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("given no analytics store, when requesting metrics, then empty points are returned", func(t *testing.T) {
		ss3, _ := store.NewSQLite(":memory:")
		defer ss3.Close()
		ss3.CreateCluster(&domain.Cluster{ID: "tc-id2", Name: "test-cluster", Status: domain.ClusterStatusUp})

		srv := &Server{
			store:     ss3,
			analytics: nil,
			logger:    slog.Default(),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/clusters/", srv.handleClusterSubroutes)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		url := fmt.Sprintf("%s/api/v1/clusters/test-cluster/metrics", ts.URL)
		resp, err := http.Get(url)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var body metricsResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("decode json: %v", err)
		}
		if len(body.Points) != 0 {
			t.Errorf("expected 0 points, got %d", len(body.Points))
		}
	})

	t.Run("given a post request, when hitting metrics endpoint, then 405 is returned", func(t *testing.T) {
		analytics := &stubAnalyticsStore{}
		ss4, _ := store.NewSQLite(":memory:")
		defer ss4.Close()
		ss4.CreateCluster(&domain.Cluster{ID: "tc-id3", Name: "test-cluster", Status: domain.ClusterStatusUp})

		srv := &Server{
			store:     ss4,
			analytics: analytics,
			logger:    slog.Default(),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/clusters/", srv.handleClusterSubroutes)
		ts := httptest.NewServer(mux)
		defer ts.Close()

		url := fmt.Sprintf("%s/api/v1/clusters/test-cluster/metrics", ts.URL)
		resp, err := http.Post(url, "application/json", nil)
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("expected 405, got %d", resp.StatusCode)
		}
	})
}
