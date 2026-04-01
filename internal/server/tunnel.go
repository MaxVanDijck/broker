package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	ws "github.com/coder/websocket"

	"broker/internal/tunnel"
	pb "broker/proto/agentpb"
)

type AgentConnection struct {
	NodeID      string
	ClusterName string
	Tunnel      *tunnel.Tunnel
	Info        *pb.NodeInfo
	Cancel      context.CancelFunc
}

type TunnelHandler struct {
	logger *slog.Logger

	mu     sync.RWMutex
	agents map[string]*AgentConnection // node_id -> connection

	onRegister   func(conn *AgentConnection)
	onHeartbeat  func(nodeID, clusterName string, hb *pb.Heartbeat)
	onLogBatch   func(jobID string, batch *pb.LogBatch)
	onJobUpdate  func(jobID string, update *pb.JobUpdate)
	onSSHSession func(sessionID string, data []byte, closed bool)
	onDisconnect func(conn *AgentConnection)
}

func NewTunnelHandler(logger *slog.Logger) *TunnelHandler {
	return &TunnelHandler{
		logger: logger,
		agents: make(map[string]*AgentConnection),
	}
}

func (h *TunnelHandler) SetCallbacks(
	onRegister func(*AgentConnection),
	onHeartbeat func(string, string, *pb.Heartbeat),
	onLogBatch func(string, *pb.LogBatch),
	onJobUpdate func(string, *pb.JobUpdate),
	onSSHSession func(string, []byte, bool),
	onDisconnect func(*AgentConnection),
) {
	h.onRegister = onRegister
	h.onHeartbeat = onHeartbeat
	h.onLogBatch = onLogBatch
	h.onJobUpdate = onJobUpdate
	h.onSSHSession = onSSHSession
	h.onDisconnect = onDisconnect
}

func (h *TunnelHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := ws.Accept(w, r, &ws.AcceptOptions{})
	if err != nil {
		h.logger.Error("failed to accept websocket", "error", err)
		return
	}
	conn.SetReadLimit(4 << 20) // 4MB - SSH session data can be large (VS Code server download)

	ctx, cancel := context.WithCancel(r.Context())
	t := tunnel.New(conn, h.logger)

	// First message must be Register
	env, err := t.Recv(ctx)
	if err != nil {
		h.logger.Error("failed to read register message", "error", err)
		cancel()
		t.Close()
		return
	}

	reg := env.GetRegister()
	if reg == nil {
		h.logger.Error("first message must be register")
		cancel()
		t.Close()
		return
	}

	// TODO: validate token
	h.logger.Info("agent registered", "node_id", reg.NodeId, "cluster", reg.ClusterName)

	ac := &AgentConnection{
		NodeID:      reg.NodeId,
		ClusterName: reg.ClusterName,
		Tunnel:      t,
		Info:        reg.NodeInfo,
		Cancel:      cancel,
	}

	h.mu.Lock()
	h.agents[reg.NodeId] = ac
	h.mu.Unlock()

	if err := t.Send(ctx, &pb.Envelope{
		Payload: &pb.Envelope_RegisterAck{
			RegisterAck: &pb.RegisterAck{
				Accepted:                 true,
				HeartbeatIntervalSeconds: 15,
			},
		},
	}); err != nil {
		h.logger.Error("failed to send register ack", "error", err)
		cancel()
		t.Close()
		return
	}

	if h.onRegister != nil {
		h.onRegister(ac)
	}

	h.readLoop(ctx, ac)

	h.mu.Lock()
	delete(h.agents, reg.NodeId)
	h.mu.Unlock()

	if h.onDisconnect != nil {
		h.onDisconnect(ac)
	}

	cancel()
	t.Close()
	h.logger.Info("agent disconnected", "node_id", reg.NodeId)
}

func (h *TunnelHandler) readLoop(ctx context.Context, ac *AgentConnection) {
	for {
		env, err := ac.Tunnel.Recv(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			h.logger.Error("agent read error", "node_id", ac.NodeID, "error", err)
			return
		}

		switch p := env.Payload.(type) {
		case *pb.Envelope_Heartbeat:
			if h.onHeartbeat != nil {
				h.onHeartbeat(ac.NodeID, ac.ClusterName, p.Heartbeat)
			}
		case *pb.Envelope_LogBatch:
			if h.onLogBatch != nil {
				h.onLogBatch(p.LogBatch.JobId, p.LogBatch)
			}
		case *pb.Envelope_JobUpdate:
			if h.onJobUpdate != nil {
				h.onJobUpdate(p.JobUpdate.JobId, p.JobUpdate)
			}
		case *pb.Envelope_SshSession:
			if h.onSSHSession != nil {
				h.onSSHSession(p.SshSession.SessionId, p.SshSession.Data, p.SshSession.Closed)
			}
		default:
			h.logger.Warn("unknown message from agent", "node_id", ac.NodeID)
		}
	}
}

func (h *TunnelHandler) GetAgent(nodeID string) (*AgentConnection, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ac, ok := h.agents[nodeID]
	return ac, ok
}

func (h *TunnelHandler) GetAgentByCluster(clusterName string) (*AgentConnection, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ac := range h.agents {
		if ac.ClusterName == clusterName {
			return ac, true
		}
	}
	return nil, false
}

func (h *TunnelHandler) SendToAgent(ctx context.Context, nodeID string, env *pb.Envelope) error {
	ac, ok := h.GetAgent(nodeID)
	if !ok {
		return fmt.Errorf("agent %s not connected", nodeID)
	}
	return ac.Tunnel.Send(ctx, env)
}

func (h *TunnelHandler) ListAgents() []*AgentConnection {
	h.mu.RLock()
	defer h.mu.RUnlock()
	agents := make([]*AgentConnection, 0, len(h.agents))
	for _, ac := range h.agents {
		agents = append(agents, ac)
	}
	return agents
}
