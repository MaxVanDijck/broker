package agent

import (
	"context"
	"fmt"
	"net"
	"sync"

	pb "broker/proto/agentpb"
)

// sshRelays manages active SSH proxy sessions. Each session is a TCP
// connection to the agent's local SSH server (localhost:sshPort), with
// data relayed through the WebSocket tunnel to the server.
type sshRelays struct {
	mu    sync.RWMutex
	conns map[string]net.Conn // session_id -> TCP connection to local sshd
}

func newSSHRelays() *sshRelays {
	return &sshRelays{conns: make(map[string]net.Conn)}
}

func (r *sshRelays) get(id string) (net.Conn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.conns[id]
	return c, ok
}

func (r *sshRelays) add(id string, conn net.Conn) {
	r.mu.Lock()
	r.conns[id] = conn
	r.mu.Unlock()
}

func (r *sshRelays) remove(id string) {
	r.mu.Lock()
	if c, ok := r.conns[id]; ok {
		c.Close()
		delete(r.conns, id)
	}
	r.mu.Unlock()
}

func (a *Agent) handleSSHSessionData(ctx context.Context, msg *pb.SSHSessionData) {
	sessionID := msg.SessionId

	if msg.Closed {
		a.sshRelays.remove(sessionID)
		return
	}

	conn, exists := a.sshRelays.get(sessionID)
	if !exists {
		// New session: dial the local SSH server
		var err error
		conn, err = net.Dial("tcp", fmt.Sprintf("localhost:%d", a.cfg.SSHPort))
		if err != nil {
			a.logger.Error("ssh relay: failed to dial local sshd", "session_id", sessionID, "error", err)
			a.tun.Send(ctx, &pb.Envelope{
				Payload: &pb.Envelope_SshSession{SshSession: &pb.SSHSession{
					SessionId: sessionID,
					Closed:    true,
				}},
			})
			return
		}

		a.sshRelays.add(sessionID, conn)
		a.logger.Info("ssh relay: session started", "session_id", sessionID)

		// Read from local SSH server, send back through tunnel.
		// Use context.Background so this goroutine doesn't die if the
		// message loop context is cancelled -- SSH sessions outlive
		// individual message dispatches.
		go func() {
			sendCtx := context.Background()
			buf := make([]byte, 32*1024)
			for {
				n, err := conn.Read(buf)
				if n > 0 {
					data := make([]byte, n)
					copy(data, buf[:n])
					if sendErr := a.tun.Send(sendCtx, &pb.Envelope{
						Payload: &pb.Envelope_SshSession{SshSession: &pb.SSHSession{
							SessionId: sessionID,
							Data:      data,
						}},
					}); sendErr != nil {
						a.sshRelays.remove(sessionID)
						return
					}
				}
				if err != nil {
					a.sshRelays.remove(sessionID)
					a.tun.Send(sendCtx, &pb.Envelope{
						Payload: &pb.Envelope_SshSession{SshSession: &pb.SSHSession{
							SessionId: sessionID,
							Closed:    true,
						}},
					})
					return
				}
			}
		}()
	}

	// Forward data from tunnel to local SSH server
	if len(msg.Data) > 0 {
		if _, err := conn.Write(msg.Data); err != nil {
			a.logger.Warn("ssh relay: write to local sshd failed", "session_id", sessionID, "error", err)
			a.sshRelays.remove(sessionID)
			a.tun.Send(ctx, &pb.Envelope{
				Payload: &pb.Envelope_SshSession{SshSession: &pb.SSHSession{
					SessionId: sessionID,
					Closed:    true,
				}},
			})
		}
	}
}
