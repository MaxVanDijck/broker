package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"connectrpc.com/connect"

	"broker/internal/auth"
	"broker/internal/dashboard"
	"broker/internal/domain"
	"broker/internal/optimizer"
	"broker/internal/provider"
	"broker/internal/provider/aws"
	"broker/internal/store"
	agentpb "broker/proto/agentpb"
	brokerpb "broker/proto/brokerpb"
	"broker/proto/brokerpb/brokerpbconnect"

	"github.com/google/uuid"
)

const (
	shutdownTimeout = 30 * time.Second
	maxNumNodes     = 256
)

var validClusterName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,62}$`)

type Server struct {
	brokerpbconnect.UnimplementedBrokerServiceHandler
	store        store.StateStore
	analytics    store.AnalyticsStore
	registry     *provider.Registry
	logger       *slog.Logger
	Tunnel       *TunnelHandler
	logbus       *LogBus
	sshSessions  *sshSessionManager
	events       *EventBus
	autostop     *AutostopManager
	oidcVerifier *auth.Verifier

	// pendingJobs holds task specs for jobs that couldn't be dispatched
	// because no agent was connected yet (e.g. cloud instances still booting).
	// Dispatched when the agent registers.
	pendingMu   sync.Mutex
	pendingJobs map[string][]pendingJob // cluster_id -> jobs
}

type pendingJob struct {
	jobID string
	task  *brokerpb.TaskSpec
}

func New(s store.StateStore, a store.AnalyticsStore, r *provider.Registry, logger *slog.Logger, oidcCfg *auth.OIDCConfig) *Server {
	evBus := NewEventBus(logger.With("component", "events"))
	srv := &Server{
		store:       s,
		analytics:   a,
		registry:    r,
		logger:      logger,
		Tunnel:      NewTunnelHandler(logger.With("component", "tunnel")),
		logbus:      NewLogBus(),
		sshSessions: newSSHSessionManager(),
		events:      evBus,
		autostop:    NewAutostopManager(s, logger.With("component", "autostop")),
		pendingJobs: make(map[string][]pendingJob),
	}

	if oidcCfg != nil && oidcCfg.Enabled() {
		verifier, err := auth.NewVerifier(context.Background(), *oidcCfg, logger.With("component", "oidc"))
		if err != nil {
			logger.Warn("failed to initialize oidc verifier, continuing without oidc", "error", err)
		} else {
			srv.oidcVerifier = verifier
			logger.Info("oidc authentication enabled", "issuer", oidcCfg.Issuer)
		}
	}

	srv.autostop.onTeardown = func(cluster *domain.Cluster) {
		srv.logger.Info("autostop: tearing down cluster", "name", cluster.Name, "id", cluster.ID)
		srv.teardownCluster(cluster)
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

func (s *Server) Serve(ctx context.Context, port int) error {
	bgCtx, bgCancel := context.WithCancel(ctx)
	defer bgCancel()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("autostop manager panicked", "panic", r)
			}
		}()
		s.autostop.Run(bgCtx)
	}()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("cost tracker panicked", "panic", r)
			}
		}()
		s.runCostTracker(bgCtx)
	}()

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
	mux.HandleFunc("/api/v1/clusters", s.handleClustersListAPI)
	mux.HandleFunc("/api/v1/clusters/", s.handleClusterSubroutes)
	mux.HandleFunc("/api/v1/jobs", s.handleJobsAPI)
	mux.HandleFunc("/api/v1/costs", s.handleCostsAPI)
	mux.HandleFunc("/api/v1/workdir/", s.handleWorkdir)
	mux.HandleFunc("/api/v1/ssh-setup", s.handleSSHSetup)

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

	// Auth endpoints
	mux.HandleFunc("/auth/login", s.handleAuthLogin)
	mux.HandleFunc("/auth/callback", s.handleAuthCallback)
	mux.HandleFunc("/auth/userinfo", s.handleAuthUserinfo)

	// Dashboard (embedded SPA, serves as fallback for all other routes)
	mux.Handle("/", dashboard.Handler())

	hs := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           authMiddleware(s.oidcVerifier, mux),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()
		s.logger.Info("shutting down server")
		if err := hs.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("server shutdown error", "error", err)
		}
	}()

	s.logger.Info("broker server starting", "port", port, "protocols", "connect,grpc,grpc-web,websocket")
	return hs.ListenAndServe()
}

func (s *Server) hasAgentForCluster(clusterID, clusterName string) bool {
	if _, ok := s.Tunnel.GetAgentByClusterID(clusterID); ok {
		return true
	}
	if _, ok := s.Tunnel.GetAgentByCluster(clusterName); ok {
		return true
	}
	return false
}

// watchProvisionedCluster terminates cloud instances if no agent registers
// within the given timeout. This prevents orphaned instances when the agent
// binary download fails, the UserData script errors, or the instance cannot
// reach the server.
func (s *Server) watchProvisionedCluster(clusterID string, prov provider.Provider, cluster *domain.Cluster, timeout time.Duration) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if s.hasAgentForCluster(clusterID, cluster.Name) {
				s.logger.Info("agent registered, provision watchdog cancelled", "cluster", cluster.Name, "cluster_id", clusterID)
				return
			}
		case <-deadline:
			if s.hasAgentForCluster(clusterID, cluster.Name) {
				return
			}

			s.logger.Error("no agent registered within timeout, terminating instances",
				"cluster", cluster.Name,
				"cluster_id", clusterID,
				"timeout", timeout,
			)

			c, err := s.store.GetClusterByID(clusterID)
			if err != nil {
				s.logger.Error("failed to get cluster", "cluster_id", clusterID, "error", err)
			}
			if c != nil {
				s.teardownCluster(c)
			}
			return
		}
	}
}

// Agent tunnel callbacks

func (s *Server) onAgentRegister(ac *AgentConnection) {
	s.logger.Info("agent online", "node_id", ac.NodeID, "cluster", ac.ClusterName)

	cluster, err := s.resolveCluster(ac.ClusterName)
	if err != nil || cluster == nil {
		s.logger.Warn("agent registered for unknown cluster", "cluster", ac.ClusterName)
		return
	}
	ac.SetClusterID(cluster.ID)

	s.trySetClusterUp(cluster.ID)
	s.events.Publish(Event{
		Type: "node_online",
		Data: map[string]string{
			"node_id":      ac.NodeID,
			"cluster_name": ac.ClusterName,
			"cluster_id":   cluster.ID,
		},
	})

	// Dispatch any pending jobs that were queued before the agent connected
	s.pendingMu.Lock()
	jobs := s.pendingJobs[cluster.ID]
	delete(s.pendingJobs, cluster.ID)
	s.pendingMu.Unlock()

	for _, pj := range jobs {
		s.logger.Info("dispatching pending job", "job_id", pj.jobID, "cluster", ac.ClusterName)
		if err := s.dispatchJob(context.Background(), cluster.ID, pj.jobID, pj.task); err != nil {
			s.logger.Error("failed to dispatch pending job", "job_id", pj.jobID, "error", err)
		}
	}
}

func (s *Server) onAgentDisconnect(ac *AgentConnection) {
	s.logger.Info("agent offline", "node_id", ac.NodeID, "cluster", ac.ClusterName)
	s.events.Publish(Event{
		Type: "node_offline",
		Data: map[string]string{
			"node_id":      ac.NodeID,
			"cluster_name": ac.ClusterName,
			"cluster_id":   ac.GetClusterID(),
		},
	})
}

func (s *Server) trySetClusterUp(clusterID string) {
	cluster, err := s.store.GetClusterByID(clusterID)
	if err != nil || cluster == nil {
		return
	}
	if cluster.Status == domain.ClusterStatusInit || cluster.Status == domain.ClusterStatusStopped {
		cluster.Status = domain.ClusterStatusUp
		if err := s.store.UpdateCluster(cluster); err != nil {
			s.logger.Error("failed to update cluster", "cluster_id", cluster.ID, "error", err)
		}
		s.events.Publish(Event{
			Type: "cluster_update",
			Data: map[string]string{
				"cluster_name": cluster.Name,
				"cluster_id":   clusterID,
				"status":       string(domain.ClusterStatusUp),
			},
		})
	}
}

func (s *Server) onAgentHeartbeat(nodeID, clusterName string, hb *agentpb.Heartbeat) {
	s.logger.Debug("heartbeat", "node_id", nodeID, "jobs", hb.RunningJobIds)

	// Resolve cluster name to ID for metrics storage
	ac, ok := s.Tunnel.GetAgent(nodeID)
	clusterID := ""
	if ok && ac.GetClusterID() != "" {
		clusterID = ac.GetClusterID()
		s.trySetClusterUp(clusterID)
	}

	if s.analytics == nil {
		return
	}

	var points []store.MetricPoint
	base := store.MetricPoint{
		Timestamp:     time.Unix(hb.TimestampUnix, 0),
		NodeID:        nodeID,
		ClusterID:     clusterID,
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

func (s *Server) onAgentLogBatch(clusterName, jobID string, batch *agentpb.LogBatch) {
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
	if clusterName != "" {
		s.logbus.Publish("cluster:"+clusterName, entries)
	}
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

	s.autostop.Touch(job.ClusterID)

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
			"cluster_id":   job.ClusterID,
			"status":       string(job.Status),
		},
	})
}

func (s *Server) dispatchJob(ctx context.Context, clusterID string, jobID string, task *brokerpb.TaskSpec) error {
	s.autostop.Touch(clusterID)

	ac, ok := s.Tunnel.GetAgentByClusterID(clusterID)
	if !ok {
		// Agent may have registered before the cluster was created in the
		// store, so its ClusterID hasn't been set yet. Look up by cluster
		// name as a fallback and backfill the ID.
		cluster, err := s.store.GetClusterByID(clusterID)
		if err != nil {
			s.logger.Error("failed to get cluster", "cluster_id", clusterID, "error", err)
		}
		if cluster != nil {
			ac, ok = s.Tunnel.GetAgentByCluster(cluster.Name)
			if ok && ac.GetClusterID() == "" {
				ac.SetClusterID(clusterID)
			}
		}
	}
	if !ok {
		return fmt.Errorf("no agent connected for cluster %q", clusterID)
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

	if !validClusterName.MatchString(clusterName) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("invalid cluster name %q: must be 1-63 alphanumeric characters, dots, hyphens, or underscores", clusterName))
	}

	autostopMinutes := int(msg.IdleMinutesToAutostop)
	if autostopMinutes < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("autostop minutes must be non-negative"))
	}
	if autostopMinutes == 0 {
		autostopMinutes = 30
	}

	if msg.Task != nil && msg.Task.NumNodes > int32(maxNumNodes) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("num_nodes %d exceeds maximum %d", msg.Task.NumNodes, maxNumNodes))
	}

	if msg.Task != nil && msg.Task.Resources != nil && msg.Task.Resources.Cloud != "" {
		cloud := domain.CloudProvider(msg.Task.Resources.Cloud)
		if _, ok := s.registry.Get(cloud); !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("unknown cloud provider %q", msg.Task.Resources.Cloud))
		}
	}

	s.logger.Info("launching cluster", "name", clusterName)

	cluster := &domain.Cluster{
		ID:              uuid.New().String(),
		Name:            clusterName,
		Status:          domain.ClusterStatusInit,
		LaunchedAt:      time.Now().UTC(),
		UserID:          "default",
		AutostopMinutes: autostopMinutes,
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

	// If the user specified resources (GPUs, instance type, etc.) but no cloud,
	// default to the first registered cloud provider. This avoids silently
	// running on a local agent when the user clearly wants cloud resources.
	if cluster.Cloud == "" && cluster.Resources != nil {
		needsCloud := cluster.Resources.Accelerators != "" ||
			cluster.Resources.InstanceType != "" ||
			cluster.Resources.UseSpot
		if needsCloud {
			clouds := s.registry.List()
			if len(clouds) > 0 {
				cluster.Cloud = clouds[0]
				if cluster.Resources != nil {
					cluster.Resources.Cloud = clouds[0]
				}
				s.logger.Info("no cloud specified, defaulting to available provider", "cloud", clouds[0])
			}
		}
	}

	// Run the cost optimizer to select the cheapest instance type when the
	// user hasn't explicitly set one. The optimizer considers accelerators,
	// CPU, and memory requirements against the instance catalog and pricing.
	var selectedInstanceType string
	var selectedHourlyPrice float64

	if cluster.Resources != nil && cluster.Resources.InstanceType == "" {
		hasRequirements := cluster.Resources.Accelerators != "" ||
			cluster.Resources.CPUs != "" ||
			cluster.Resources.Memory != ""
		if hasRequirements {
			recs, err := optimizer.Optimize(optimizer.Requirements{
				Accelerators: cluster.Resources.Accelerators,
				CPUs:         cluster.Resources.CPUs,
				Memory:       cluster.Resources.Memory,
				UseSpot:      cluster.Resources.UseSpot,
			})
			if err != nil {
				s.logger.Warn("optimizer failed, falling back to default resolution", "error", err)
			} else if len(recs) > 0 {
				selectedInstanceType = recs[0].InstanceType
				selectedHourlyPrice = recs[0].HourlyPrice
				cluster.Resources.InstanceType = selectedInstanceType
				s.logger.Info("optimizer selected instance",
					"instance_type", selectedInstanceType,
					"hourly_price", selectedHourlyPrice,
					"cluster", clusterName,
				)
			}
		}
	}

	// If instance type is explicitly set, look up its price for the response.
	if cluster.Resources != nil && cluster.Resources.InstanceType != "" && selectedHourlyPrice == 0 {
		selectedInstanceType = cluster.Resources.InstanceType
		if price, ok := aws.OnDemandPricing[selectedInstanceType]; ok {
			selectedHourlyPrice = price
			if cluster.Resources.UseSpot {
				selectedHourlyPrice *= optimizer.SpotDiscount
			}
		}
	}

	existing, err := s.resolveCluster(clusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to check cluster: %w", err))
	}

	// resolveCluster returns only active clusters (not terminated), so if
	// existing is nil, we create a new one. Old terminated clusters with
	// the same name stay in the DB as history.
	if existing == nil {
		if err := s.store.CreateCluster(cluster); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to store cluster: %w", err))
		}
	} else {
		cluster = existing
	}

	s.logger.Info("cluster registered", "name", clusterName, "id", cluster.ID, "status", cluster.Status)

	if cluster.Cloud != "" && cluster.AutostopMinutes > 0 {
		timeout := time.Duration(cluster.AutostopMinutes) * time.Minute
		s.autostop.SetTimeout(cluster.ID, timeout)
		s.logger.Info("autostop configured", "cluster", clusterName, "timeout", timeout)
	}

	// If a cloud provider is specified and available, provision instances asynchronously.
	// The agent will connect back via WebSocket once the instance boots and the
	// UserData script starts it. Job dispatch happens when the agent registers.
	needsProvisioning := existing == nil || (existing != nil && existing.Status == domain.ClusterStatusInit)
	if cluster.Cloud != "" && needsProvisioning {
		prov, ok := s.registry.Get(cluster.Cloud)
		if ok {
			task := protoToTaskSpec(msg.Task)
			// Capture identifiers by value to avoid racing on the
			// cluster pointer after the handler returns.
			launchClusterID := cluster.ID
			launchClusterCloud := cluster.Cloud
			go func() {
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("provisioning panicked", "cluster", clusterName, "panic", r)
					}
				}()
				s.logger.Info("provisioning cloud instances", "cloud", launchClusterCloud, "cluster", clusterName)
				// Re-fetch from the store so the goroutine has its own copy.
				c, err := s.store.GetClusterByID(launchClusterID)
				if err != nil {
					s.logger.Error("failed to get cluster", "cluster_id", launchClusterID, "error", err)
				}
				if c == nil {
					s.logger.Error("cluster disappeared before provisioning", "cluster", clusterName)
					return
				}
				nodes, err := prov.Launch(context.Background(), c, task)
				if err != nil {
					s.logger.Error("cloud provisioning failed", "cluster", clusterName, "error", err)
					c.Status = domain.ClusterStatusTerminated
					if err := s.store.UpdateCluster(c); err != nil {
						s.logger.Error("failed to update cluster", "cluster_id", c.ID, "error", err)
					}
					s.events.Publish(Event{
						Type: "cluster_update",
						Data: map[string]string{
							"cluster_name": clusterName,
							"cluster_id":   launchClusterID,
							"status":       string(domain.ClusterStatusTerminated),
						},
					})
					return
				}
				if len(nodes) > 0 {
					c.HeadIP = nodes[0].PublicIP
					if nodes[0].Region != "" {
						c.Region = nodes[0].Region
					}
					if nodes[0].InstanceType != "" && c.Resources != nil {
						c.Resources.InstanceType = nodes[0].InstanceType
					}
					if err := s.store.UpdateCluster(c); err != nil {
						s.logger.Error("failed to update cluster", "cluster_id", c.ID, "error", err)
					}
				}
				s.logger.Info("cloud provisioning complete", "cluster", clusterName, "nodes", len(nodes))

				// Start a registration watchdog. If no agent registers for
				// this cluster within 30 minutes of provisioning, terminate
				// the instances. This prevents orphaned instances when the
				// agent binary download fails or the instance can't reach
				// the server.
				go s.watchProvisionedCluster(launchClusterID, prov, c, 30*time.Minute)
			}()
		} else {
			s.logger.Warn("no provider registered for cloud", "cloud", cluster.Cloud)
		}
	}

	jobID := uuid.New().String()[:8]
	job := &domain.Job{
		ID:          jobID,
		ClusterID:   cluster.ID,
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

	if err := s.dispatchJob(ctx, cluster.ID, jobID, msg.Task); err != nil {
		s.logger.Info("job queued, waiting for agent", "job_id", jobID, "cluster", clusterName)
		s.pendingMu.Lock()
		s.pendingJobs[cluster.ID] = append(s.pendingJobs[cluster.ID], pendingJob{jobID: jobID, task: msg.Task})
		s.pendingMu.Unlock()
	}

	region := cluster.Region
	if region == "" {
		region = "us-east-1"
	}

	return connect.NewResponse(&brokerpb.LaunchResponse{
		ClusterName:  clusterName,
		JobId:        jobID,
		HeadIp:       cluster.HeadIP,
		InstanceType: selectedInstanceType,
		HourlyPrice:  selectedHourlyPrice,
		Region:       region,
	}), nil
}

func (s *Server) Stop(ctx context.Context, req *connect.Request[brokerpb.ClusterRequest]) (*connect.Response[brokerpb.ClusterResponse], error) {
	cluster, err := s.resolveCluster(req.Msg.ClusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if cluster == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", req.Msg.ClusterName))
	}

	s.logger.Info("stopping cluster", "name", cluster.Name, "id", cluster.ID)

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
	cluster, err := s.resolveCluster(req.Msg.ClusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if cluster == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", req.Msg.ClusterName))
	}

	s.logger.Info("starting cluster", "name", cluster.Name, "id", cluster.ID)

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
	cluster, err := s.resolveCluster(req.Msg.ClusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if cluster == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", req.Msg.ClusterName))
	}

	s.logger.Info("tearing down cluster", "name", cluster.Name, "id", cluster.ID)
	s.teardownCluster(cluster)

	return connect.NewResponse(&brokerpb.ClusterResponse{
		ClusterName: cluster.Name,
		Status:      string(domain.ClusterStatusTerminating),
	}), nil
}

// teardownCluster transitions a cluster to TERMINATING, tears down cloud
// resources asynchronously, then marks it TERMINATED. All teardown paths
// (ConnectRPC Down, REST DELETE, autostop) funnel through here.
func (s *Server) teardownCluster(cluster *domain.Cluster) {
	s.autostop.Remove(cluster.ID)

	if cluster.Status == domain.ClusterStatusTerminating || cluster.Status == domain.ClusterStatusTerminated {
		return
	}

	cluster.Status = domain.ClusterStatusTerminating
	if err := s.store.UpdateCluster(cluster); err != nil {
		s.logger.Error("failed to update cluster", "cluster_id", cluster.ID, "error", err)
	}
	s.events.Publish(Event{
		Type: "cluster_update",
		Data: map[string]string{
			"cluster_name": cluster.Name,
			"cluster_id":   cluster.ID,
			"status":       string(domain.ClusterStatusTerminating),
		},
	})

	clusterID := cluster.ID
	clusterName := cluster.Name
	clusterCloud := cluster.Cloud
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("teardown goroutine panicked", "cluster", clusterName, "panic", r)
			}
		}()
		if clusterCloud != "" {
			if prov, ok := s.registry.Get(clusterCloud); ok {
				c, err := s.store.GetClusterByID(clusterID)
				if err != nil {
					s.logger.Error("failed to get cluster", "cluster_id", clusterID, "error", err)
				}
				if c != nil {
					if err := prov.Teardown(context.Background(), c); err != nil {
						s.logger.Error("cloud teardown failed", "cluster", clusterName, "error", err)
					}
				}
			}
		}

		c, err := s.store.GetClusterByID(clusterID)
		if err != nil {
			s.logger.Error("failed to get cluster", "cluster_id", clusterID, "error", err)
		}
		if c != nil {
			c.Status = domain.ClusterStatusTerminated
			if err := s.store.UpdateCluster(c); err != nil {
				s.logger.Error("failed to update cluster", "cluster_id", c.ID, "error", err)
			}
		}
		s.events.Publish(Event{
			Type: "cluster_update",
			Data: map[string]string{
				"cluster_name": clusterName,
				"cluster_id":   clusterID,
				"status":       string(domain.ClusterStatusTerminated),
			},
		})
		s.logger.Info("cluster terminated", "name", clusterName)
	}()
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
	cluster, err := s.resolveCluster(req.Msg.ClusterName)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if cluster == nil {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", req.Msg.ClusterName))
	}

	jobID := uuid.New().String()[:8]
	job := &domain.Job{
		ID:          jobID,
		ClusterID:   cluster.ID,
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

	if err := s.dispatchJob(ctx, cluster.ID, jobID, req.Msg.Task); err != nil {
		s.logger.Info("job queued, waiting for agent", "job_id", jobID, "cluster", cluster.Name)
		s.pendingMu.Lock()
		s.pendingJobs[cluster.ID] = append(s.pendingJobs[cluster.ID], pendingJob{jobID: jobID, task: req.Msg.Task})
		s.pendingMu.Unlock()
	}

	return connect.NewResponse(&brokerpb.ExecResponse{JobId: jobID}), nil
}

func formatLogLine(entry store.LogEntry) string {
	ts := entry.Timestamp.UTC().Format("15:04:05.000")
	return fmt.Sprintf("[%s] [%s] %s", ts, entry.Stream, string(entry.Line))
}

func (s *Server) Logs(ctx context.Context, req *connect.Request[brokerpb.LogsRequest], stream *connect.ServerStream[brokerpb.LogsResponse]) error {
	jobID := req.Msg.JobId
	clusterName := req.Msg.ClusterName
	follow := req.Msg.Follow
	s.logger.Info("streaming logs", "cluster", clusterName, "job_id", jobID, "follow", follow)

	if s.analytics != nil {
		tr := store.TimeRange{
			From: time.Time{},
			To:   time.Now().Add(time.Minute),
		}

		var jobIDs []string
		if jobID != "" {
			jobIDs = []string{jobID}
		} else if clusterName != "" {
			cluster, err := s.resolveCluster(clusterName)
			if err != nil {
				return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get cluster: %w", err))
			}
			if cluster != nil {
				jobs, err := s.store.ListJobs(cluster.ID)
				if err != nil {
					return connect.NewError(connect.CodeInternal, fmt.Errorf("failed to list jobs: %w", err))
				}
				for _, j := range jobs {
					jobIDs = append(jobIDs, j.ID)
				}
			}
		}

		for _, id := range jobIDs {
			existing, err := s.analytics.QueryLogs(ctx, id, tr, 10000)
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
	}

	if !follow {
		return nil
	}

	subscribeKey := jobID
	if subscribeKey == "" && clusterName != "" {
		subscribeKey = "cluster:" + clusterName
	}
	if subscribeKey == "" {
		return nil
	}

	ch := s.logbus.Subscribe(subscribeKey)
	defer s.logbus.Unsubscribe(subscribeKey, ch)

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
	clusterName := req.Msg.ClusterName
	s.logger.Info("cancelling jobs", "cluster", clusterName, "job_ids", req.Msg.JobIds, "all", req.Msg.All)

	if clusterName == "" && len(req.Msg.JobIds) == 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("cluster_name or job_ids required"))
	}

	var jobsToCancel []*domain.Job

	if len(req.Msg.JobIds) > 0 {
		for _, jobID := range req.Msg.JobIds {
			job, err := s.store.GetJob(jobID)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("failed to get job %s: %w", jobID, err))
			}
			if job == nil {
				return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("job %q not found", jobID))
			}
			jobsToCancel = append(jobsToCancel, job)
		}
	} else if clusterName != "" {
		cluster, err := s.resolveCluster(clusterName)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		if cluster == nil {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("cluster %q not found", clusterName))
		}
		jobs, err := s.store.ListJobs(cluster.ID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		for _, j := range jobs {
			if req.Msg.All || !j.Status.IsTerminal() {
				jobsToCancel = append(jobsToCancel, j)
			}
		}
	}

	for _, job := range jobsToCancel {
		if job.Status.IsTerminal() {
			continue
		}

		ac, ok := s.Tunnel.GetAgentByClusterID(job.ClusterID)
		if !ok {
			ac, ok = s.Tunnel.GetAgentByCluster(job.ClusterName)
		}

		if ok {
			if err := ac.Tunnel.Send(ctx, &agentpb.Envelope{
				Payload: &agentpb.Envelope_CancelJob{CancelJob: &agentpb.CancelJob{
					JobId: job.ID,
					Force: false,
				}},
			}); err != nil {
				s.logger.Error("failed to send cancel to agent", "job_id", job.ID, "error", err)
			}
		}

		now := time.Now().UTC()
		job.Status = domain.JobStatusCancelled
		job.EndedAt = &now
		if err := s.store.UpdateJob(job); err != nil {
			s.logger.Error("failed to update cancelled job", "job_id", job.ID, "error", err)
		}

		s.events.Publish(Event{
			Type: "job_update",
			Data: map[string]string{
				"job_id":       job.ID,
				"cluster_name": job.ClusterName,
				"cluster_id":   job.ClusterID,
				"status":       string(domain.JobStatusCancelled),
			},
		})
	}

	return connect.NewResponse(&brokerpb.CancelJobResponse{}), nil
}

// REST API handlers

type clusterListItemJSON struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Cloud        string `json:"cloud"`
	Region       string `json:"region"`
	Resources    string `json:"resources"`
	HeadIP       string `json:"head_ip"`
	NumNodes     int    `json:"num_nodes"`
	LaunchedAt   string `json:"launched_at"`
	InstanceType string `json:"instance_type,omitempty"`
	IsSpot       bool   `json:"is_spot,omitempty"`
}

func (s *Server) handleClustersListAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusters, err := s.store.ListClusters()
	if err != nil {
		s.logger.Error("failed to list clusters", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	items := make([]clusterListItemJSON, 0, len(clusters))
	for _, c := range clusters {
		item := clusterListItemJSON{
			ID:         c.ID,
			Name:       c.Name,
			Status:     string(c.Status),
			Cloud:      string(c.Cloud),
			Region:     c.Region,
			HeadIP:     c.HeadIP,
			NumNodes:   c.NumNodes,
			LaunchedAt: c.LaunchedAt.Format(time.RFC3339),
		}
		if c.Resources != nil {
			item.Resources = c.Resources.String()
			item.InstanceType = c.Resources.InstanceType
			item.IsSpot = c.Resources.UseSpot
		}
		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	// Error ignored: response already committed
	json.NewEncoder(w).Encode(map[string]interface{}{"clusters": items})
}

func (s *Server) handleJobsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusterName := r.URL.Query().Get("cluster")

	var jobs []*domain.Job
	var err error
	if clusterName != "" {
		cluster, cerr := s.resolveCluster(clusterName)
		if cerr != nil {
			s.logger.Error("failed to resolve cluster", "cluster", clusterName, "error", cerr)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		if cluster != nil {
			jobs, err = s.store.ListJobs(cluster.ID)
		}
	} else {
		jobs, err = s.store.ListAllJobs()
	}
	if err != nil {
		s.logger.Error("failed to list jobs", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if jobs == nil {
		jobs = []*domain.Job{}
	}

	w.Header().Set("Content-Type", "application/json")
	// Error ignored: response already committed
	json.NewEncoder(w).Encode(map[string]interface{}{"jobs": jobs})
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
	cluster, err := s.resolveCluster(clusterName)
	if err != nil {
		s.logger.Error("failed to resolve cluster", "cluster", clusterName, "error", err)
	}

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
	// Error ignored: response already committed
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
		// Error ignored: response already committed
		json.NewEncoder(w).Encode(metricsResponse{ClusterName: clusterName, Points: []metricsPointJSON{}})
		return
	}

	// Resolve cluster name or ID for metrics query
	var cluster *domain.Cluster
	clusterID := ""
	if s.store != nil {
		cluster, _ = s.resolveCluster(clusterName)
		if cluster != nil {
			clusterID = cluster.ID
		}
	}

	now := time.Now()
	tr := store.TimeRange{
		From: now.Add(-time.Hour),
		To:   now,
	}

	if cluster != nil && tr.From.Before(cluster.LaunchedAt) {
		tr.From = cluster.LaunchedAt
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

	// Ensure from is never before cluster launch, even after user-provided override
	if cluster != nil && tr.From.Before(cluster.LaunchedAt) {
		tr.From = cluster.LaunchedAt
	}

	var points []store.MetricPoint
	var queryErr error

	if nodeID := r.URL.Query().Get("node_id"); nodeID != "" {
		points, queryErr = s.analytics.QueryMetrics(r.Context(), nodeID, tr)
	} else if clusterID != "" {
		points, queryErr = s.analytics.QueryMetricsByCluster(r.Context(), clusterID, tr)
	}

	if queryErr != nil {
		s.logger.Error("failed to query metrics", "cluster", clusterName, "error", queryErr)
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
	// Error ignored: response already committed
	json.NewEncoder(w).Encode(resp)
}
