package server

import (
	"context"
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

// integrationEnv wraps a full server stack with real stores for integration tests.
type integrationEnv struct {
	server     *Server
	httpServer *httptest.Server
	client     brokerpbconnect.BrokerServiceClient
	store      *store.SQLStore
}

func setupIntegrationEnv(t *testing.T) *integrationEnv {
	t.Helper()

	dbPath := t.TempDir() + "/test.db"
	db, err := store.NewSQLite(dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	logger := slog.Default()
	registry := provider.NewRegistry()
	analytics := store.NewNoopAnalytics()

	srv := New(db, analytics, registry, logger, nil)

	mux := http.NewServeMux()
	path, handler := brokerpbconnect.NewBrokerServiceHandler(srv)
	mux.Handle(path, handler)
	mux.Handle("/agent/v1/connect", srv.Tunnel)

	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)

	rpcClient := brokerpbconnect.NewBrokerServiceClient(http.DefaultClient, hs.URL)

	return &integrationEnv{
		server:     srv,
		httpServer: hs,
		client:     rpcClient,
		store:      db,
	}
}

func connectIntegrationAgent(t *testing.T, env *integrationEnv, nodeID, clusterName string) *tunnel.Tunnel {
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

func TestIntegration_JobDispatchAndExecution(t *testing.T) {
	t.Run("given a connected agent, when a job is launched and the agent runs it, then status transitions PENDING to RUNNING to SUCCEEDED with logs", func(t *testing.T) {
		env := setupIntegrationEnv(t)
		ctx := context.Background()

		// Pre-create the cluster so the agent registration can resolve it
		err := env.store.CreateCluster(&domain.Cluster{
			ID:     "int-c-1",
			Name:   "int-cluster",
			Status: domain.ClusterStatusInit,
			UserID: "default",
		})
		if err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		// Connect agent after cluster exists so onAgentRegister can resolve it
		agentTun := connectIntegrationAgent(t, env, "int-node-1", "int-cluster")
		time.Sleep(100 * time.Millisecond)

		// Verify agent registration set cluster status to UP
		cluster, err := env.store.GetCluster("int-cluster")
		if err != nil {
			t.Fatalf("get cluster: %v", err)
		}
		if cluster.Status != domain.ClusterStatusUp {
			t.Errorf("expected cluster status UP after agent register, got %s", cluster.Status)
		}

		// Launch reuses the existing cluster + creates a job + dispatches to agent
		launchResp, err := env.client.Launch(ctx, connect.NewRequest(&brokerpb.LaunchRequest{
			ClusterName: "int-cluster",
			Task: &brokerpb.TaskSpec{
				Name:  "integration-job",
				Setup: "echo setting up",
				Run:   "echo hello world",
				Envs:  map[string]string{"MY_VAR": "test"},
			},
		}))
		if err != nil {
			t.Fatalf("launch: %v", err)
		}
		jobID := launchResp.Msg.JobId
		if jobID == "" {
			t.Fatal("expected non-empty job ID")
		}

		// Verify job was created in PENDING status
		job, err := env.store.GetJob(jobID)
		if err != nil {
			t.Fatalf("get job: %v", err)
		}
		if job.Status != domain.JobStatusPending {
			t.Errorf("expected initial job status PENDING, got %s", job.Status)
		}

		// Agent receives the SubmitJob
		recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
		defer recvCancel()
		envelope, err := agentTun.Recv(recvCtx)
		if err != nil {
			t.Fatalf("agent recv: %v", err)
		}
		submitJob := envelope.GetSubmitJob()
		if submitJob == nil {
			t.Fatalf("expected SubmitJob, got %T", envelope.Payload)
		}
		if submitJob.JobId != jobID {
			t.Errorf("expected job_id %s, got %s", jobID, submitJob.JobId)
		}
		if submitJob.Name != "integration-job" {
			t.Errorf("expected name 'integration-job', got %s", submitJob.Name)
		}
		if submitJob.SetupScript != "echo setting up" {
			t.Errorf("expected setup_script, got %s", submitJob.SetupScript)
		}
		if submitJob.RunScript != "echo hello world" {
			t.Errorf("expected run_script, got %s", submitJob.RunScript)
		}
		if v, ok := submitJob.Env["MY_VAR"]; !ok || v != "test" {
			t.Errorf("expected env MY_VAR=test, got %v", submitJob.Env)
		}

		// Simulate agent sending RUNNING update
		if err := agentTun.Send(ctx, &agentpb.Envelope{
			Payload: &agentpb.Envelope_JobUpdate{
				JobUpdate: &agentpb.JobUpdate{
					JobId: jobID,
					State: agentpb.JobState_JOB_STATE_RUNNING,
				},
			},
		}); err != nil {
			t.Fatalf("send RUNNING update: %v", err)
		}

		// Send log batch while job is running
		if err := agentTun.Send(ctx, &agentpb.Envelope{
			Payload: &agentpb.Envelope_LogBatch{
				LogBatch: &agentpb.LogBatch{
					JobId: jobID,
					Entries: []*agentpb.LogEntry{
						{
							TimestampUnixNano: time.Now().UnixNano(),
							Stream:            "stdout",
							Data:              []byte("hello world\n"),
						},
					},
				},
			},
		}); err != nil {
			t.Fatalf("send log batch: %v", err)
		}

		time.Sleep(150 * time.Millisecond)

		// Verify job transitioned to RUNNING
		job, err = env.store.GetJob(jobID)
		if err != nil {
			t.Fatalf("get job after RUNNING: %v", err)
		}
		if job.Status != domain.JobStatusRunning {
			t.Errorf("expected job status RUNNING, got %s", job.Status)
		}
		if job.StartedAt == nil {
			t.Error("expected StartedAt to be set after RUNNING")
		}

		// Simulate agent sending SUCCEEDED update
		if err := agentTun.Send(ctx, &agentpb.Envelope{
			Payload: &agentpb.Envelope_JobUpdate{
				JobUpdate: &agentpb.JobUpdate{
					JobId: jobID,
					State: agentpb.JobState_JOB_STATE_SUCCEEDED,
				},
			},
		}); err != nil {
			t.Fatalf("send SUCCEEDED update: %v", err)
		}

		time.Sleep(150 * time.Millisecond)

		// Verify job transitioned to SUCCEEDED
		job, err = env.store.GetJob(jobID)
		if err != nil {
			t.Fatalf("get job after SUCCEEDED: %v", err)
		}
		if job.Status != domain.JobStatusSucceeded {
			t.Errorf("expected job status SUCCEEDED, got %s", job.Status)
		}
		if job.EndedAt == nil {
			t.Error("expected EndedAt to be set after SUCCEEDED")
		}

		// Verify cluster status is still UP
		cluster, err = env.store.GetCluster("int-cluster")
		if err != nil {
			t.Fatalf("get cluster after job: %v", err)
		}
		if cluster.Status != domain.ClusterStatusUp {
			t.Errorf("expected cluster status UP, got %s", cluster.Status)
		}
	})
}

func TestIntegration_PendingJobDispatchedOnAgentConnect(t *testing.T) {
	t.Run("given a job submitted before agent connects, when agent registers, then pending job is dispatched", func(t *testing.T) {
		env := setupIntegrationEnv(t)
		ctx := context.Background()

		// Create cluster first, then submit job without any agent connected
		err := env.store.CreateCluster(&domain.Cluster{
			ID:     "pending-c-1",
			Name:   "pending-cluster",
			Status: domain.ClusterStatusInit,
			UserID: "default",
		})
		if err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		execResp, err := env.client.Exec(ctx, connect.NewRequest(&brokerpb.ExecRequest{
			ClusterName: "pending-cluster",
			Task: &brokerpb.TaskSpec{
				Name: "delayed-job",
				Run:  "echo delayed",
			},
		}))
		if err != nil {
			t.Fatalf("exec: %v", err)
		}
		jobID := execResp.Msg.JobId

		// Verify job is pending and queued
		job, _ := env.store.GetJob(jobID)
		if job.Status != domain.JobStatusPending {
			t.Errorf("expected PENDING, got %s", job.Status)
		}

		// Now connect the agent -- this should trigger pending job dispatch
		agentTun := connectIntegrationAgent(t, env, "pending-node", "pending-cluster")
		time.Sleep(200 * time.Millisecond)

		// Agent should receive the SubmitJob for the pending job
		recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
		defer recvCancel()
		envelope, err := agentTun.Recv(recvCtx)
		if err != nil {
			t.Fatalf("agent recv: %v", err)
		}
		submitJob := envelope.GetSubmitJob()
		if submitJob == nil {
			t.Fatalf("expected SubmitJob, got %T", envelope.Payload)
		}
		if submitJob.JobId != jobID {
			t.Errorf("expected pending job_id %s, got %s", jobID, submitJob.JobId)
		}
		if submitJob.Name != "delayed-job" {
			t.Errorf("expected name 'delayed-job', got %s", submitJob.Name)
		}
	})
}

func TestIntegration_MultipleJobsSequential(t *testing.T) {
	t.Run("given a connected agent, when multiple jobs are submitted, then each is dispatched and tracked independently", func(t *testing.T) {
		env := setupIntegrationEnv(t)
		ctx := context.Background()

		err := env.store.CreateCluster(&domain.Cluster{
			ID:     "multi-c-1",
			Name:   "multi-cluster",
			Status: domain.ClusterStatusUp,
			UserID: "default",
		})
		if err != nil {
			t.Fatalf("create cluster: %v", err)
		}

		agentTun := connectIntegrationAgent(t, env, "multi-node", "multi-cluster")
		time.Sleep(100 * time.Millisecond)

		// Submit two jobs
		resp1, err := env.client.Exec(ctx, connect.NewRequest(&brokerpb.ExecRequest{
			ClusterName: "multi-cluster",
			Task:        &brokerpb.TaskSpec{Name: "job-1", Run: "echo 1"},
		}))
		if err != nil {
			t.Fatalf("exec job 1: %v", err)
		}

		resp2, err := env.client.Exec(ctx, connect.NewRequest(&brokerpb.ExecRequest{
			ClusterName: "multi-cluster",
			Task:        &brokerpb.TaskSpec{Name: "job-2", Run: "echo 2"},
		}))
		if err != nil {
			t.Fatalf("exec job 2: %v", err)
		}

		// Drain both SubmitJob messages from agent
		recvCtx, recvCancel := context.WithTimeout(ctx, 5*time.Second)
		defer recvCancel()

		for range 2 {
			_, err := agentTun.Recv(recvCtx)
			if err != nil {
				t.Fatalf("agent recv: %v", err)
			}
		}

		// Mark job 1 as succeeded
		agentTun.Send(ctx, &agentpb.Envelope{
			Payload: &agentpb.Envelope_JobUpdate{
				JobUpdate: &agentpb.JobUpdate{JobId: resp1.Msg.JobId, State: agentpb.JobState_JOB_STATE_SUCCEEDED},
			},
		})

		// Mark job 2 as failed
		agentTun.Send(ctx, &agentpb.Envelope{
			Payload: &agentpb.Envelope_JobUpdate{
				JobUpdate: &agentpb.JobUpdate{JobId: resp2.Msg.JobId, State: agentpb.JobState_JOB_STATE_FAILED, ExitCode: 1},
			},
		})

		time.Sleep(200 * time.Millisecond)

		job1, _ := env.store.GetJob(resp1.Msg.JobId)
		job2, _ := env.store.GetJob(resp2.Msg.JobId)

		if job1.Status != domain.JobStatusSucceeded {
			t.Errorf("expected job 1 SUCCEEDED, got %s", job1.Status)
		}
		if job2.Status != domain.JobStatusFailed {
			t.Errorf("expected job 2 FAILED, got %s", job2.Status)
		}
		if job1.EndedAt == nil {
			t.Error("expected job 1 EndedAt to be set")
		}
		if job2.EndedAt == nil {
			t.Error("expected job 2 EndedAt to be set")
		}
	})
}
