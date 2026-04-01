package server

import (
	"context"
	"net/http"
	"strings"
	"sync"

	ws "github.com/coder/websocket"
	"github.com/google/uuid"

	pb "broker/proto/agentpb"
)

// sshSessions tracks active SSH proxy sessions. When a CLI client connects
// to /api/v1/clusters/{name}/ssh, the server creates a session, relays data
// between the client WebSocket and the agent's tunnel using SSHSessionData
// (server->agent) and SSHSession (agent->server) envelope messages.
type sshSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*sshSession // session_id -> session
}

type sshSession struct {
	id     string
	conn   *ws.Conn
	ctx    context.Context
	cancel context.CancelFunc
}

func newSSHSessionManager() *sshSessionManager {
	return &sshSessionManager{sessions: make(map[string]*sshSession)}
}

func (m *sshSessionManager) add(s *sshSession) {
	m.mu.Lock()
	m.sessions[s.id] = s
	m.mu.Unlock()
}

func (m *sshSessionManager) remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

func (m *sshSessionManager) get(id string) (*sshSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	return s, ok
}

// handleSSHProxy is the HTTP handler for /api/v1/clusters/{name}/ssh.
// It upgrades to WebSocket and relays SSH traffic through the agent tunnel.
func (s *Server) handleSSHProxy(w http.ResponseWriter, r *http.Request) {
	// Extract cluster name from URL: /api/v1/clusters/{name}/ssh
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[1] != "ssh" || parts[0] == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	clusterName := parts[0]

	ac, ok := s.Tunnel.GetAgentByCluster(clusterName)
	if !ok {
		http.Error(w, "no agent connected for cluster", http.StatusServiceUnavailable)
		return
	}

	conn, err := ws.Accept(w, r, &ws.AcceptOptions{})
	if err != nil {
		s.logger.Error("ssh proxy: websocket accept failed", "error", err)
		return
	}

	sessionID := uuid.New().String()[:8]
	ctx, cancel := context.WithCancel(r.Context())

	sess := &sshSession{
		id:     sessionID,
		conn:   conn,
		ctx:    ctx,
		cancel: cancel,
	}
	s.sshSessions.add(sess)
	if ac.ClusterID != "" {
		s.autostop.Touch(ac.ClusterID)
	}

	s.logger.Info("ssh proxy session started", "session_id", sessionID, "cluster", clusterName, "node", ac.NodeID)

	defer func() {
		s.sshSessions.remove(sessionID)
		cancel()
		conn.Close(ws.StatusNormalClosure, "session ended")
		s.logger.Info("ssh proxy session ended", "session_id", sessionID)

		// Tell the agent to close the SSH connection
		ac.Tunnel.Send(context.Background(), &pb.Envelope{
			Payload: &pb.Envelope_SshSessionData{SshSessionData: &pb.SSHSessionData{
				SessionId: sessionID,
				Closed:    true,
			}},
		})
	}()

	// Tell the agent to open an SSH connection for this session
	if err := ac.Tunnel.Send(ctx, &pb.Envelope{
		Payload: &pb.Envelope_SshSessionData{SshSessionData: &pb.SSHSessionData{
			SessionId: sessionID,
		}},
	}); err != nil {
		s.logger.Error("ssh proxy: failed to send session open", "error", err)
		return
	}

	// Read from client WebSocket, forward to agent via tunnel.
	// Touch autostop on every message -- the session is active as long as
	// data flows (keystrokes, output, file transfers).
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			s.logger.Info("ssh proxy: client read ended", "session_id", sessionID, "error", err)
			return
		}

		if ac.ClusterID != "" {
			s.autostop.Touch(ac.ClusterID)
		}

		if err := ac.Tunnel.Send(ctx, &pb.Envelope{
			Payload: &pb.Envelope_SshSessionData{SshSessionData: &pb.SSHSessionData{
				SessionId: sessionID,
				Data:      data,
			}},
		}); err != nil {
			s.logger.Info("ssh proxy: tunnel send failed", "session_id", sessionID, "error", err)
			return
		}
	}
}

// onSSHSessionData is called by the tunnel handler when the agent sends
// SSH session data back. It forwards the data to the client WebSocket.
func (s *Server) onSSHSessionData(sessionID string, data []byte, closed bool) {
	sess, ok := s.sshSessions.get(sessionID)
	if !ok {
		return
	}

	if closed {
		sess.cancel()
		return
	}

	if len(data) > 0 {
		if err := sess.conn.Write(sess.ctx, ws.MessageBinary, data); err != nil {
			sess.cancel()
		}
	}
}
