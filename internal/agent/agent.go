package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"broker/internal/agent/executor"
	"broker/internal/agent/metrics"
	"broker/internal/agent/sshd"
	"broker/internal/tunnel"
	pb "broker/proto/agentpb"
)

type Config struct {
	ServerURL          string
	Token              string
	ClusterName        string
	NodeID             string
	SSHPort            int
	Mode               string        // "host" or "run"
	SelfTerminateAfter time.Duration // dead man's switch timeout (0 = disabled)
}

type Agent struct {
	cfg       Config
	logger    *slog.Logger
	tun       *tunnel.Tunnel
	exec      *executor.Executor
	ssh       *sshd.Server
	watchdog  *Watchdog
	collector *metrics.Collector
	sshRelays *sshRelays
}

func New(cfg Config, logger *slog.Logger) *Agent {
	if cfg.NodeID == "" {
		hostname, _ := os.Hostname()
		cfg.NodeID = fmt.Sprintf("%s-%s", hostname, uuid.New().String()[:8])
	}
	if cfg.SSHPort == 0 {
		cfg.SSHPort = 2222
	}

	a := &Agent{
		cfg:       cfg,
		logger:    logger,
		ssh:       sshd.New(logger.With("component", "sshd"), cfg.SSHPort),
		collector: metrics.NewCollector(logger.With("component", "metrics")),
		sshRelays: newSSHRelays(),
	}

	a.exec = executor.New(
		logger.With("component", "executor"),
		a.sendLogBatch,
		cfg.ServerURL,
	)

	if cfg.SelfTerminateAfter > 0 {
		a.watchdog = NewWatchdog(
			logger.With("component", "watchdog"),
			cfg.SelfTerminateAfter,
		)
	}

	return a
}

func (a *Agent) Run(ctx context.Context) error {
	errCh := make(chan error, 3)

	go func() {
		errCh <- a.ssh.Serve()
	}()

	go func() {
		errCh <- a.connectLoop(ctx)
	}()

	if a.watchdog != nil {
		go a.watchdog.Run(ctx)
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Agent) connectLoop(ctx context.Context) error {
	backoff := time.Second

	for {
		err := a.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		a.logger.Error("tunnel disconnected, reconnecting", "error", err, "backoff", backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}

		backoff = min(backoff*2, 30*time.Second)
	}
}

func (a *Agent) connect(ctx context.Context) error {
	url := fmt.Sprintf("%s/agent/v1/connect", a.cfg.ServerURL)
	a.logger.Info("connecting to server", "url", url)

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return fmt.Errorf("dial server: %w", err)
	}
	conn.SetReadLimit(4 << 20) // 4MB

	a.tun = tunnel.New(conn, a.logger)
	defer a.tun.Close()

	if err := a.register(ctx); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	heartbeatInterval := 15 * time.Second
	go a.heartbeatLoop(ctx, heartbeatInterval)

	return a.messageLoop(ctx)
}

func (a *Agent) register(ctx context.Context) error {
	info := a.collectNodeInfo()

	if err := a.tun.Send(ctx, &pb.Envelope{
		Payload: &pb.Envelope_Register{
			Register: &pb.Register{
				NodeId:      a.cfg.NodeID,
				ClusterName: a.cfg.ClusterName,
				Token:       a.cfg.Token,
				NodeInfo:    info,
			},
		},
	}); err != nil {
		return err
	}

	env, err := a.tun.Recv(ctx)
	if err != nil {
		return fmt.Errorf("read register ack: %w", err)
	}

	ack := env.GetRegisterAck()
	if ack == nil {
		return fmt.Errorf("expected register ack, got %T", env.Payload)
	}
	if !ack.Accepted {
		return fmt.Errorf("registration rejected: %s", ack.Error)
	}

	a.logger.Info("registered with server", "node_id", a.cfg.NodeID)

	if a.watchdog != nil {
		a.watchdog.Arm()
		a.watchdog.Touch()
	}

	return nil
}

func (a *Agent) messageLoop(ctx context.Context) error {
	for {
		env, err := a.tun.Recv(ctx)
		if err != nil {
			return err
		}

		if a.watchdog != nil {
			a.watchdog.Touch()
		}

		switch p := env.Payload.(type) {
		case *pb.Envelope_SubmitJob:
			a.handleSubmitJob(ctx, p.SubmitJob)
		case *pb.Envelope_CancelJob:
			a.handleCancelJob(p.CancelJob)
		case *pb.Envelope_TerminateNode:
			a.handleTerminate(p.TerminateNode)
			return nil
		case *pb.Envelope_SshSessionData:
			a.handleSSHSessionData(ctx, p.SshSessionData)
		}
	}
}

func (a *Agent) handleSubmitJob(ctx context.Context, msg *pb.SubmitJob) {
	a.logger.Info("received job", "job_id", msg.JobId, "name", msg.Name)

	a.exec.Submit(ctx, msg, func(update *pb.JobUpdate) {
		a.logger.Info("job state changed", "job_id", update.JobId, "state", update.State)
		if err := a.tun.Send(ctx, &pb.Envelope{
			Payload: &pb.Envelope_JobUpdate{JobUpdate: update},
		}); err != nil {
			a.logger.Warn("failed to send job update", "job_id", update.JobId, "error", err)
		}
	})
}

func (a *Agent) handleCancelJob(msg *pb.CancelJob) {
	a.logger.Info("cancelling job", "job_id", msg.JobId)
	a.exec.Cancel(msg.JobId, msg.Force)
}

func (a *Agent) handleTerminate(msg *pb.TerminateNode) {
	a.logger.Info("terminate requested", "reason", msg.Reason, "grace_period", msg.GracePeriodSeconds)
	// TODO: graceful shutdown -- drain jobs, then exit
}

func (a *Agent) heartbeatLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			hb := &pb.Heartbeat{
				NodeId:        a.cfg.NodeID,
				TimestampUnix: time.Now().Unix(),
				RunningJobIds: a.exec.RunningJobIDs(),
			}

			snap, err := a.collector.Collect()
			if err != nil {
				a.logger.Warn("failed to collect metrics", "error", err)
			} else {
				hb.CpuPercent = snap.CPUPercent
				hb.MemoryPercent = snap.MemoryPercent
				hb.DiskUsedBytes = snap.DiskUsedBytes
				for _, g := range snap.GPUs {
					hb.GpuMetrics = append(hb.GpuMetrics, &pb.GPUMetrics{
						Index:              g.Index,
						UtilizationPercent: g.Utilization,
						MemoryUsedBytes:    g.MemoryUsed,
						TemperatureCelsius: g.Temperature,
					})
				}
			}

			if err := a.tun.Send(ctx, &pb.Envelope{
				Payload: &pb.Envelope_Heartbeat{Heartbeat: hb},
			}); err != nil {
				a.logger.Warn("failed to send heartbeat", "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (a *Agent) sendLogBatch(jobID string, batch *pb.LogBatch) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := a.tun.Send(ctx, &pb.Envelope{
		Payload: &pb.Envelope_LogBatch{LogBatch: batch},
	}); err != nil {
		a.logger.Warn("failed to send log batch", "job_id", jobID, "error", err)
	}
}

func (a *Agent) collectNodeInfo() *pb.NodeInfo {
	hostname, _ := os.Hostname()

	info := &pb.NodeInfo{
		Hostname:    hostname,
		Os:          runtime.GOOS,
		Arch:        runtime.GOARCH,
		Cpus:        int32(runtime.NumCPU()),
		MemoryBytes: totalMemoryBytes(),
		SshPort:     int32(a.cfg.SSHPort),
	}

	addrs, _ := net.InterfaceAddrs()
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
			info.PrivateIp = ipNet.IP.String()
			break
		}
	}

	return info
}

func totalMemoryBytes() int64 {
	// Try /proc/meminfo (Linux)
	data, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				var kb int64
				fmt.Sscanf(line, "MemTotal: %d kB", &kb)
				return kb * 1024
			}
		}
	}

	// macOS: use sysctl
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err == nil {
		var bytes int64
		fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &bytes)
		return bytes
	}

	return 0
}
