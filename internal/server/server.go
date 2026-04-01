package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"

	"broker/internal/dashboard"
	"broker/internal/domain"
	"broker/internal/provider"
	"broker/internal/store"
	agentpb "broker/proto/agentpb"
	brokerpb "broker/proto/brokerpb"
	"broker/proto/brokerpb/brokerpbconnect"

	"github.com/google/uuid"
)

type Server struct {
	brokerpbconnect.UnimplementedBrokerServiceHandler
	store       store.StateStore
	analytics   store.AnalyticsStore
	registry    *provider.Registry
	logger      *slog.Logger
	Tunnel      *TunnelHandler
	logbus      *LogBus
	sshSessions *sshSessionManager
	events      *EventBus
}

func New(s store.StateStore, a store.AnalyticsStore, r *provider.Registry, logger *slog.Logger) *Server {
	srv := &Server{
		store:       s,
		analytics:   a,
		registry:    r,
		logger:      logger,
		Tunnel:      NewTunnelHandler(logger.With("component", "tunnel")),
		logbus:      NewLogBus(),
		sshSessions: newSSHSessionManager(),
		events:      NewEventBus(logger.With("component", "events")),
	}

	srv.Tunnel.SetCallbacks(
		srv.onAgentRegister,
		srv.onAgentHeartbeat,
		srv.onAgentLogBatch,
		srv.onAgentJobUpdate,
		srv.onSSHSessionData,
		srv.onAgentDisconnect,
	)

	return srv
}

func (s *Server) Serve(port int) error {
	mux := http.NewServeMux()

	// ConnectRPC API (serves gRPC, gRPC-web, and Connect protocols on the same handler)
	path, handler := brokerpbconnect.NewBrokerServiceHandler(s)
	mux.Handle(path, handler)

	// Agent WebSocket tunnel
	mux.Handle("/agent/v1/connect", s.Tunnel)

	// Agent binary download. Serves the broker-agent binary so EC2 instances
	// can bootstrap without a pre-baked AMI. This is a temporary measure --
	// replace with custom AMIs once a Packer pipeline exists. If an instance
	// cannot retrieve this binary, the agent will not start and the node will
	// have no self-termination (dead man's switch), leaving an orphaned
	// instance running indefinitely.
	mux.HandleFunc("/agent/v1/binary", s.handleAgentBinary)

	// SSE event stream
	mux.HandleFunc("/api/v1/events", s.handleSSE)

	// REST API
	mux.HandleFunc("/api/v1/jobs", s.handleJobsAPI)
	mux.HandleFunc("/api/v1/workdir/", s.handleWorkdir)
	mux.HandleFunc("/api/v1/ssh-setup", s.handleSSHSetup)
	mux.HandleFunc("/api/v1/clusters/", s.handleClusterOrSSHProxy)

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Readiness check (returns 200 only when at least one agent is connected)
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if len(s.Tunnel.ListAgents()) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("no agents"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	// Dashboard (embedded SPA, serves as fallback for all other routes)
	mux.Handle("/", dashboard.Handler())

	hs := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	s.logger.Info("broker server starting", "port", port, "protocols", "connect,grpc,grpc-web,websocket")
	return hs.ListenAndServe()
}

// watchProvisionedCluster terminates cloud instances if no agent registers
// within the given timeout. This prevents orphaned instances when the agent
// binary download fails, the UserData script errors, or the instance cannot
// reach the server.
func (s *Server) watchProvisionedCluster(clusterName string, prov provider.Provider, cluster *domain.Cluster, timeout time.Duration) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if _, ok := s.Tunnel.GetAgentByCluster(clusterName); ok {
				s.logger.Info("agent registered, provision watchdog cancelled", "cluster", clusterName)
				return
			}
		case <-deadline:
			if _, ok := s.Tunnel.GetAgentByCluster(clusterName); ok {
				return
			}

			s.logger.Error("no agent registered within timeout, terminating instances",
				"cluster", clusterName,
				"timeout", timeout,
			)

			if err := prov.Teardown(context.Background(), cluster); err != nil {
				s.logger.Error("failed to teardown orphaned cluster", "cluster", clusterName, "error", err)
			}

			s.store.DeleteCluster(clusterName)
			return
		}
	}
}

// Agent tunnel callbacks

func (s *Server) onAgentRegister(ac *AgentConnection) {
	s.logger.Info("agent online", "node_id", ac.NodeID, "cluster", ac.ClusterName)
	s.trySetClusterUp(ac.ClusterName)
	s.events.Publish(Event{
		Type: "node_online",
		Data: map[string]string{
			"node_id":      ac.NodeID,
			"cluster_name": ac.ClusterName,
		},
	})
}

func (s *Server) onAgentDisconnect(ac *AgentConnection) {
	s.logger.Info("agent offline", "node_id", ac.NodeID, "cluster", ac.ClusterName)
	s.events.Publish(Event{
		Type: "node_offline",
		Data: map[string]string{
			"node_id":      ac.NodeID,
			"cluster_name": ac.ClusterName,
		},
	})
}

func (s *Server) trySetClusterUp(clusterName string) {
	cluster, err := s.store.GetCluster(clusterName)
	if err != nil || cluster == nil {
		return
	}
	if cluster.Status != domain.ClusterStatusUp {
		cluster.Status = domain.ClusterStatusUp
		s.store.UpdateCluster(cluster)
		s.events.Publish(Event{
			Type: "cluster_update",
			Data: map[string]string{
				"cluster_name": clusterName,
				"status":       string(domain.ClusterStatusUp),
			},
		})
	}
}

func (s *Server) onAgentHeartbeat(nodeID, clusterName string, hb *agentpb.Heartbeat) {
	s.logger.Debug("heartbeat", "node_id", nodeID, "jobs", hb.RunningJobIds)
	s.trySetClusterUp(clusterName)

	if s.analytics == nil {
		return
	}

	var points []store.MetricPoint
	base := store.MetricPoint{
		Timestamp:     time.Unix(hb.TimestampUnix, 0),
		NodeID:        nodeID,
		ClusterName:   clusterName,
		CPUPercent:    hb.CpuPercent,
		MemoryPercent: hb.MemoryPercent,
		DiskUsedBytes: hb.DiskUsedBytes,
	}

	if len(hb.GpuMetrics) == 0 {
		points = append(points, base)
	} else {
		for _, g := range hb.GpuMetrics {
			p := base
			p.GPUIndex = g.Index
			p.GPUUtilization = g.UtilizationPercent
			p.GPUMemoryUsed = g.MemoryUsedBytes
			p.GPUTemperature = g.TemperatureCelsius
			points = append(points, p)
		}
	}

	if err := s.analytics.InsertMetrics(context.Background(), points); err != nil {
		s.logger.Error("failed to insert metrics", "error", err)
	}
}

func (s *Server) onAgentLogBatch(jobID string, batch *agentpb.LogBatch) {
	entries := make([]store.LogEntry, 0, len(batch.Entries))
	for _, e := range batch.Entries {
		entries = append(entries, store.LogEntry{
			Timestamp: time.Unix(0, e.TimestampUnixNano),
			JobID:     jobID,
			Stream:    e.Stream,
			Line:      e.Data,
		})
	}

	if s.analytics != nil {
		if err := s.analytics.InsertLogs(context.Background(), entries); err != nil {
			s.logger.Error("failed to insert logs", "error", err)
		}
	}

	s.logbus.Publish(jobID, entries)
}

func (s *Server) onAgentJobUpdate(jobID string, update *agentpb.JobUpdate) {
	s.logger.Info("job update from agent", "job_id", jobID, "state", update.State)

	job, err := s.store.GetJob(jobID)
	if err != nil {
		s.logger.Error("failed to get job for update", "job_id", jobID, "error", err)
		return
	}
	if job == nil {
		s.logger.Warn("job update for unknown job", "job_id", jobID)
		return
	}

	now := time.Now().UTC()
	switch update.State {
	case agentpb.JobState_JOB_STATE_SETUP, agentpb.JobState_JOB_STATE_PULLING:
		job.Status = domain.JobStatusRunning
		if job.StartedAt == nil {
			job.StartedAt = &now
		}
	case agentpb.JobState_JOB_STATE_RUNNING:
		job.Status = domain.JobStatusRunning
		if job.StartedAt == nil {
			job.StartedAt = &now
		}
	case agentpb.JobState_JOB_STATE_SUCCEEDED:
		job.Status = domain.JobStatusSucceeded
		job.EndedAt = &now
	case agentpb.JobState_JOB_STATE_FAILED:
		job.Status = domain.JobStatusFailed
		job.EndedAt = &now
	case agentpb.JobState_JOB_STATE_CANCELLED:
		job.Status = domain.JobStatusCancelled
		job.EndedAt = &now
	}

	if err := s.store.UpdateJob(job); err != nil {
		s.logger.Error("failed to update job state", "job_id", jobID, "error", err)
	}

	s.events.Publish(Event{
		Type: "job_update",
		Data: map[string]string{
			"job_id":       jobID,
			"cluster_name": job.ClusterName,
			"status":       string(job.Status),
		},
	})
}

func (s *Server) dispatchJob(ctx context.Context, clusterName string, jobID string, task *brokerpb.TaskSpec) error {
	ac, ok := s.Tunnel.GetAgentByCluster(clusterName)
	if !ok {
		return fmt.Errorf("no agent connected for cluster %q", clusterName)
	}

	submit := &agentpb.SubmitJob{
		JobId: jobID,
	}
	if task != nil {
		submit.Name = task.Name
		submit.SetupScript = task.Setup
		submit.RunScript = task.Run
		submit.WorkdirId = task.Workdir
		submit.Env = task.Envs
	}

	env := &agentpb.Envelope{
		Payload: &agentpb.Envelope_SubmitJob{SubmitJob: submit},
	}
	return ac.Tunnel.Send(ctx, env)
}

func protoToTaskSpec(t *brokerpb.TaskSpec) *domain.TaskSpec {
	if t == nil {
		return &domain.TaskSpec{Resources: &domain.Resources{}}
	}
	task := &domain.TaskSpec{
		Name:     t.Name,
		Workdir:  t.Workdir,
		NumNodes: int(t.NumNodes),
		Envs:     t.Envs,
		Setup:    t.Setup,
		Run:      t.Run,
	}
	if t.Resources != nil {
		task.Resources = &domain.Resources{
			Cloud:        domain.CloudProvider(t.Resources.Cloud),
			Region:       t.Resources.Region,
			Zone:         t.Resources.Zone,
			Accelerators: t.Resources.Accelerators,
			CPUs:         t.Resources.Cpus,
			Memory:       t.Resources.Memory,
			InstanceType: t.Resources.InstanceType,
			UseSpot:      t.Resources.UseSpot,
			DiskSizeGB:   int(t.Resources.DiskSizeGb),
			ImageID:      t.Resources.ImageId,
		}
	} else {
		task.Resources = &domain.Resources{}
	}
	return task
}

// ConnectRPC handlers

func (s *Server) Launch(ctx context.Context, req *connect.Request[brokerpb.LaunchRequest]) (*connect.Response[brokerpb.LaunchResponse], error) {
	msg := req.Msg
	clusterName := msg.ClusterName
	if clusterName == "" {
		clusterName = fmt.Sprintf("broker-%s", uuid.New().String()[:8])
	}

	s.logger.Info("launching cluster", "name", clusterName)

	cluster := &domain.Cluster{
		ID:         uuid.New().String(),
		Name:       clusterName,
		Status:     domain.ClusterStatusInit,
		LaunchedAt: time.Now().UTC(),
		UserID:     "default",
	}

	if msg.Task != nil {
		cluster.NumNodes = int(msg.Task.NumNodes)
		cluster.WorkdirID = msg.Task.Workdir
		if msg.Task.Resources != nil {
			cluster.Cloud = domain.CloudProvider(msg.Task.Resources.Cloud)
			cluster.Region = msg.Task.Resources.Region
			cluster.Zone = msg.Task.Resources.Zone
			cluster.Resources = &domain.Resources{
				Cloud:        domain.CloudProvider(msg.Task.Resources.Cloud),
				Region:       msg.Task.Resources.Region,
				Zone:         msg.Task.Resources.Zone,
				Accelerators: msg.Task.Resources.Accelerators,
				CPUs:         msg.Task.Resources.Cpus,
				Memory:       msg.Task.Resources.Memory,
				InstanceType: msg.Task.Resources.InstanceType,
				UseSpot:      msg.Task.Resources.UseSpot,
				DiskSizeGB:   int(msg.Task.Resources.DiskSizeGb),
				ImageID:      msg.Task.Resources.ImageId,
			}
		}
	}

	if cluster.NumNodes <= 0 {
		cluster.NumNodes = 1
	}

	existing, err := s.store.GetCluster(clusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check cluster: %w", err))
	}

	if existing != nil && (existing.Status == domain.ClusterStatusTerminated || existing.Status == domain.ClusterStatusTerminating) {
		s.store.DeleteCluster(clusterName)
		existing = nil
	}

	if existing == nil {
		if err := s.store.CreateCluster(cluster); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to store cluster: %w", err))
		}
	} else {
		cluster = existing
	}

	s.logger.Info("cluster registered", "name", clusterName, "status", cluster.Status)

	// If a cloud provider is specified and available, provision instances asynchronously.
	// The agent will connect back via WebSocket once the instance boots and the
	// UserData script starts it. Job dispatch happens when the agent registers.
	if cluster.Cloud != "" && existing == nil {
		prov, ok := s.registry.Get(cluster.Cloud)
		if ok {
			task := protoToTaskSpec(msg.Task)
			go func() {
				s.logger.Info("provisioning cloud instances", "cloud", cluster.Cloud, "cluster", clusterName)
				nodes, err := prov.Launch(context.Background(), cluster, task)
				if err != nil {
					s.logger.Error("cloud provisioning failed", "cluster", clusterName, "error", err)
					return
				}
				if len(nodes) > 0 {
					cluster.HeadIP = nodes[0].PublicIP
					s.store.UpdateCluster(cluster)
				}
				s.logger.Info("cloud provisioning complete", "cluster", clusterName, "nodes", len(nodes))

				// Start a registration watchdog. If no agent registers for
				// this cluster within 30 minutes of provisioning, terminate
				// the instances. This prevents orphaned instances when the
				// agent binary download fails or the instance can't reach
				// the server.
				go s.watchProvisionedCluster(clusterName, prov, cluster, 30*time.Minute)
			}()
		} else {
			s.logger.Warn("no provider registered for cloud", "cloud", cluster.Cloud)
		}
	}

	jobID := uuid.New().String()[:8]
	job := &domain.Job{
		ID:          jobID,
		ClusterName: clusterName,
		Status:      domain.JobStatusPending,
		UserID:      "default",
		SubmittedAt: time.Now().UTC(),
	}
	if msg.Task != nil {
		job.Name = msg.Task.Name
	}

	if err := s.store.CreateJob(job); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to store job: %w", err))
	}

	if err := s.dispatchJob(ctx, clusterName, jobID, msg.Task); err != nil {
		s.logger.Warn("failed to dispatch job to agent", "job_id", jobID, "error", err)
	}

	return connect.NewResponse(&brokerpb.LaunchResponse{
		ClusterName: clusterName,
		JobId:       jobID,
		HeadIp:      cluster.HeadIP,
	}), nil
}

func (s *Server) Stop(ctx context.Context, req *connect.Request[brokerpb.ClusterRequest]) (*connect.Response[brokerpb.ClusterResponse], error) {
	cluster, err := s.store.GetCluster(req.Msg.ClusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if cluster == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", req.Msg.ClusterName))
	}

	s.logger.Info("stopping cluster", "name", req.Msg.ClusterName)

	if cluster.Cloud != "" {
		if prov, ok := s.registry.Get(cluster.Cloud); ok {
			if err := prov.Stop(ctx, cluster); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("cloud stop failed: %w", err))
			}
		}
	}

	cluster.Status = domain.ClusterStatusStopped
	if err := s.store.UpdateCluster(cluster); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&brokerpb.ClusterResponse{
		ClusterName: cluster.Name,
		Status:      string(cluster.Status),
	}), nil
}

func (s *Server) Start(ctx context.Context, req *connect.Request[brokerpb.ClusterRequest]) (*connect.Response[brokerpb.ClusterResponse], error) {
	cluster, err := s.store.GetCluster(req.Msg.ClusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if cluster == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", req.Msg.ClusterName))
	}

	s.logger.Info("starting cluster", "name", req.Msg.ClusterName)

	if cluster.Cloud != "" {
		if prov, ok := s.registry.Get(cluster.Cloud); ok {
			if err := prov.Start(ctx, cluster); err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("cloud start failed: %w", err))
			}
		}
	}

	cluster.Status = domain.ClusterStatusUp
	if err := s.store.UpdateCluster(cluster); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&brokerpb.ClusterResponse{
		ClusterName: cluster.Name,
		Status:      string(cluster.Status),
	}), nil
}

func (s *Server) Down(ctx context.Context, req *connect.Request[brokerpb.ClusterRequest]) (*connect.Response[brokerpb.ClusterResponse], error) {
	cluster, err := s.store.GetCluster(req.Msg.ClusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if cluster == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", req.Msg.ClusterName))
	}

	s.logger.Info("tearing down cluster", "name", req.Msg.ClusterName)

	cluster.Status = domain.ClusterStatusTerminating
	s.store.UpdateCluster(cluster)
	s.events.Publish(Event{
		Type: "cluster_update",
		Data: map[string]string{
			"cluster_name": cluster.Name,
			"status":       string(domain.ClusterStatusTerminating),
		},
	})

	// Teardown cloud resources asynchronously so the CLI/dashboard gets
	// immediate feedback. The cluster transitions TERMINATING -> TERMINATED.
	go func() {
		if cluster.Cloud != "" {
			if prov, ok := s.registry.Get(cluster.Cloud); ok {
				if err := prov.Teardown(context.Background(), cluster); err != nil {
					s.logger.Error("cloud teardown failed", "cluster", cluster.Name, "error", err)
				}
			}
		}

		cluster.Status = domain.ClusterStatusTerminated
		s.store.UpdateCluster(cluster)
		s.events.Publish(Event{
			Type: "cluster_update",
			Data: map[string]string{
				"cluster_name": cluster.Name,
				"status":       string(domain.ClusterStatusTerminated),
			},
		})
		s.logger.Info("cluster terminated", "name", cluster.Name)
	}()

	return connect.NewResponse(&brokerpb.ClusterResponse{
		ClusterName: cluster.Name,
		Status:      string(domain.ClusterStatusTerminating),
	}), nil
}

func (s *Server) Status(ctx context.Context, req *connect.Request[brokerpb.StatusRequest]) (*connect.Response[brokerpb.StatusResponse], error) {
	clusters, err := s.store.ListClusters()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	resp := &brokerpb.StatusResponse{}
	for _, c := range clusters {
		info := &brokerpb.ClusterInfo{
			Name:       c.Name,
			Status:     string(c.Status),
			Cloud:      string(c.Cloud),
			Region:     c.Region,
			HeadIp:     c.HeadIP,
			NumNodes:   int32(c.NumNodes),
			LaunchedAt: c.LaunchedAt.Format(time.RFC3339),
		}
		if c.Resources != nil {
			info.Resources = c.Resources.String()
		}
		resp.Clusters = append(resp.Clusters, info)
	}

	return connect.NewResponse(resp), nil
}

func (s *Server) Exec(ctx context.Context, req *connect.Request[brokerpb.ExecRequest]) (*connect.Response[brokerpb.ExecResponse], error) {
	cluster, err := s.store.GetCluster(req.Msg.ClusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if cluster == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", req.Msg.ClusterName))
	}

	jobID := uuid.New().String()[:8]
	job := &domain.Job{
		ID:          jobID,
		ClusterName: cluster.Name,
		Status:      domain.JobStatusPending,
		UserID:      "default",
		SubmittedAt: time.Now().UTC(),
	}
	if req.Msg.Task != nil {
		job.Name = req.Msg.Task.Name
	}

	if err := s.store.CreateJob(job); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	s.logger.Info("job submitted", "job_id", jobID, "cluster", cluster.Name)

	if err := s.dispatchJob(ctx, cluster.Name, jobID, req.Msg.Task); err != nil {
		s.logger.Warn("failed to dispatch job to agent", "job_id", jobID, "error", err)
	}

	return connect.NewResponse(&brokerpb.ExecResponse{JobId: jobID}), nil
}

func formatLogLine(entry store.LogEntry) string {
	ts := entry.Timestamp.UTC().Format("15:04:05.000")
	return fmt.Sprintf("[%s] [%s] %s", ts, entry.Stream, string(entry.Line))
}

func (s *Server) Logs(ctx context.Context, req *connect.Request[brokerpb.LogsRequest], stream *connect.ServerStream[brokerpb.LogsResponse]) error {
	jobID := req.Msg.JobId
	follow := req.Msg.Follow
	s.logger.Info("streaming logs", "cluster", req.Msg.ClusterName, "job_id", jobID, "follow", follow)

	if s.analytics != nil {
		tr := store.TimeRange{
			From: time.Time{},
			To:   time.Now().Add(time.Minute),
		}
		existing, err := s.analytics.QueryLogs(ctx, jobID, tr, 10000)
		if err != nil {
			return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to query logs: %w", err))
		}
		for _, entry := range existing {
			line := formatLogLine(entry)
			if err := stream.Send(&brokerpb.LogsResponse{Line: line}); err != nil {
				return err
			}
		}
	}

	if !follow {
		return nil
	}

	ch := s.logbus.Subscribe(jobID)
	defer s.logbus.Unsubscribe(jobID, ch)

	for {
		select {
		case <-ctx.Done():
			return nil
		case entry, ok := <-ch:
			if !ok {
				return nil
			}
			line := formatLogLine(entry)
			if err := stream.Send(&brokerpb.LogsResponse{Line: line}); err != nil {
				return err
			}
		}
	}
}

func (s *Server) CancelJob(ctx context.Context, req *connect.Request[brokerpb.CancelJobRequest]) (*connect.Response[brokerpb.CancelJobResponse], error) {
	s.logger.Info("cancelling jobs", "cluster", req.Msg.ClusterName, "job_ids", req.Msg.JobIds)
	return connect.NewResponse(&brokerpb.CancelJobResponse{}), nil
}

// REST API handlers

func (s *Server) handleJobsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cluster := r.URL.Query().Get("cluster")

	var jobs []*domain.Job
	var err error
	if cluster != "" {
		jobs, err = s.store.ListJobs(cluster)
	} else {
		jobs, err = s.store.ListAllJobs()
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if jobs == nil {
		jobs = []*domain.Job{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"jobs": jobs})
}

func (s *Server) handleClusterOrSSHProxy(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[0] == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// SSH proxy uses WebSocket upgrade, not a normal GET
	if parts[1] == "ssh" {
		s.handleSSHProxy(w, r)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusterName := parts[0]

	switch parts[1] {
	case "nodes":
		s.handleNodesAPI(w, r, clusterName)
	case "metrics":
		s.handleMetricsAPI(w, r, clusterName)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// Nodes API

type nodeResponseJSON struct {
	NodeID      string            `json:"node_id"`
	Hostname    string            `json:"hostname"`
	PublicIP    string            `json:"public_ip"`
	PrivateIP   string            `json:"private_ip"`
	Status      string            `json:"status"`
	CPUs        int32             `json:"cpus"`
	MemoryBytes int64             `json:"memory_bytes"`
	GPUs        []gpuResponseJSON `json:"gpus"`
	SSHPort     int32             `json:"ssh_port"`
}

type gpuResponseJSON struct {
	Model       string `json:"model"`
	MemoryBytes int64  `json:"memory_bytes"`
}

type nodesResponse struct {
	Nodes     []nodeResponseJSON `json:"nodes"`
	WorkdirID string             `json:"workdir_id,omitempty"`
}

func (s *Server) handleNodesAPI(w http.ResponseWriter, _ *http.Request, clusterName string) {
	// Look up the cluster to get the provisioned public IP (the agent can't
	// detect its own public IP from inside a VPC).
	cluster, _ := s.store.GetCluster(clusterName)

	agents := s.Tunnel.ListAgents()
	nodes := make([]nodeResponseJSON, 0)
	for _, ac := range agents {
		if ac.ClusterName != clusterName {
			continue
		}
		node := nodeResponseJSON{
			NodeID: ac.NodeID,
			Status: "connected",
			GPUs:   make([]gpuResponseJSON, 0),
		}
		if ac.Info != nil {
			node.Hostname = ac.Info.Hostname
			node.PublicIP = ac.Info.PublicIp
			node.PrivateIP = ac.Info.PrivateIp
			node.CPUs = ac.Info.Cpus
			node.MemoryBytes = ac.Info.MemoryBytes
			node.SSHPort = ac.Info.SshPort
			for _, g := range ac.Info.Gpus {
				node.GPUs = append(node.GPUs, gpuResponseJSON{
					Model:       g.Model,
					MemoryBytes: g.MemoryBytes,
				})
			}
		}
		// If the agent didn't report a public IP, use the cluster's head IP
		// from the cloud provider (set during provisioning).
		if node.PublicIP == "" && cluster != nil && cluster.HeadIP != "" {
			node.PublicIP = cluster.HeadIP
		}
		nodes = append(nodes, node)
	}

	resp := nodesResponse{Nodes: nodes}
	if cluster != nil && cluster.WorkdirID != "" {
		resp.WorkdirID = cluster.WorkdirID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Metrics API

type metricsPointJSON struct {
	Timestamp      time.Time `json:"timestamp"`
	NodeID         string    `json:"node_id"`
	CPUPercent     float64   `json:"cpu_percent"`
	MemoryPercent  float64   `json:"memory_percent"`
	DiskUsedBytes  int64     `json:"disk_used_bytes"`
	GPUIndex       int32     `json:"gpu_index,omitempty"`
	GPUUtilization float64   `json:"gpu_utilization,omitempty"`
	GPUMemoryUsed  int64     `json:"gpu_memory_used,omitempty"`
	GPUTemperature float64   `json:"gpu_temperature,omitempty"`
}

type metricsResponse struct {
	ClusterName string             `json:"cluster_name"`
	Points      []metricsPointJSON `json:"points"`
}

func (s *Server) handleMetricsAPI(w http.ResponseWriter, r *http.Request, clusterName string) {
	if s.analytics == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(metricsResponse{ClusterName: clusterName, Points: []metricsPointJSON{}})
		return
	}

	now := time.Now()
	tr := store.TimeRange{
		From: now.Add(-time.Hour),
		To:   now,
	}

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if parsed, err := time.Parse(time.RFC3339, fromStr); err == nil {
			tr.From = parsed
		} else if ts, err := strconv.ParseInt(fromStr, 10, 64); err == nil {
			tr.From = time.Unix(ts, 0)
		}
	}

	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if parsed, err := time.Parse(time.RFC3339, toStr); err == nil {
			tr.To = parsed
		} else if ts, err := strconv.ParseInt(toStr, 10, 64); err == nil {
			tr.To = time.Unix(ts, 0)
		}
	}

	points, err := s.analytics.QueryMetricsByCluster(r.Context(), clusterName, tr)
	if err != nil {
		s.logger.Error("failed to query metrics", "cluster", clusterName, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := metricsResponse{
		ClusterName: clusterName,
		Points:      make([]metricsPointJSON, 0, len(points)),
	}

	for _, p := range points {
		resp.Points = append(resp.Points, metricsPointJSON{
			Timestamp:      p.Timestamp,
			NodeID:         p.NodeID,
			CPUPercent:     p.CPUPercent,
			MemoryPercent:  p.MemoryPercent,
			DiskUsedBytes:  p.DiskUsedBytes,
			GPUIndex:       p.GPUIndex,
			GPUUtilization: p.GPUUtilization,
			GPUMemoryUsed:  p.GPUMemoryUsed,
			GPUTemperature: p.GPUTemperature,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
