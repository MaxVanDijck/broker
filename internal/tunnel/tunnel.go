package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	agentpb "broker/proto/agentpb"
)

// Tunnel is a bidirectional protobuf message channel over a WebSocket.
// Used by both server (to talk to agents) and agent (to talk to server).
type Tunnel struct {
	conn   *websocket.Conn
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

func New(conn *websocket.Conn, logger *slog.Logger) *Tunnel {
	return &Tunnel{
		conn:   conn,
		logger: logger,
	}
}

func (t *Tunnel) Send(ctx context.Context, env *agentpb.Envelope) error {
	data, err := proto.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("tunnel closed")
	}

	return t.conn.Write(ctx, websocket.MessageBinary, data)
}

func (t *Tunnel) Recv(ctx context.Context) (*agentpb.Envelope, error) {
	_, data, err := t.conn.Read(ctx)
	if err != nil {
		return nil, err
	}

	var env agentpb.Envelope
	if err := proto.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("unmarshal envelope: %w", err)
	}

	return &env, nil
}

func (t *Tunnel) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	return t.conn.Close(websocket.StatusNormalClosure, "closing")
}
