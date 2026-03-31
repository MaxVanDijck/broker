package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	agentpb "broker/proto/agentpb"
)

func setupTunnelPair(t *testing.T) (server *Tunnel, client *Tunnel) {
	t.Helper()

	logger := slog.Default()
	serverConnCh := make(chan *websocket.Conn, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("websocket.Accept: %v", err)
			return
		}
		serverConnCh <- conn
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	clientConn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("websocket.Dial: %v", err)
	}

	var serverConn *websocket.Conn
	select {
	case serverConn = <-serverConnCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server connection")
	}

	server = New(serverConn, logger)
	client = New(clientConn, logger)
	t.Cleanup(func() {
		server.Close()
		client.Close()
	})
	return server, client
}

func TestTunnel_SendRecv(t *testing.T) {
	t.Run("given a tunnel pair, when client sends a heartbeat, then server receives it", func(t *testing.T) {
		server, client := setupTunnelPair(t)
		ctx := context.Background()

		sent := &agentpb.Envelope{
			Payload: &agentpb.Envelope_Heartbeat{
				Heartbeat: &agentpb.Heartbeat{
					NodeId:        "node-1",
					TimestampUnix: 1234567890,
					CpuPercent:    42.5,
				},
			},
		}

		if err := client.Send(ctx, sent); err != nil {
			t.Fatalf("Send: %v", err)
		}

		got, err := server.Recv(ctx)
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}

		hb := got.GetHeartbeat()
		if hb == nil {
			t.Fatal("expected heartbeat payload, got nil")
		}
		if hb.NodeId != "node-1" {
			t.Errorf("expected node_id node-1, got %s", hb.NodeId)
		}
		if hb.CpuPercent != 42.5 {
			t.Errorf("expected cpu_percent 42.5, got %f", hb.CpuPercent)
		}
	})

	t.Run("given a tunnel pair, when server sends a submit_job, then client receives it", func(t *testing.T) {
		server, client := setupTunnelPair(t)
		ctx := context.Background()

		sent := &agentpb.Envelope{
			Payload: &agentpb.Envelope_SubmitJob{
				SubmitJob: &agentpb.SubmitJob{
					JobId:     "j-42",
					RunScript: "echo hello",
				},
			},
		}

		if err := server.Send(ctx, sent); err != nil {
			t.Fatalf("Send: %v", err)
		}

		got, err := client.Recv(ctx)
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}

		sj := got.GetSubmitJob()
		if sj == nil {
			t.Fatal("expected submit_job payload, got nil")
		}
		if sj.JobId != "j-42" {
			t.Errorf("expected job_id j-42, got %s", sj.JobId)
		}
		if sj.RunScript != "echo hello" {
			t.Errorf("expected run_script 'echo hello', got %s", sj.RunScript)
		}
	})
}

func TestTunnel_BidirectionalExchange(t *testing.T) {
	t.Run("given a tunnel pair, when server sends submit_job and client sends heartbeat, then both sides receive correct typed messages", func(t *testing.T) {
		server, client := setupTunnelPair(t)
		ctx := context.Background()

		submitJobEnv := &agentpb.Envelope{
			Payload: &agentpb.Envelope_SubmitJob{
				SubmitJob: &agentpb.SubmitJob{
					JobId:       "j-99",
					RunScript:   "python train.py",
					SetupScript: "pip install torch",
					Env:         map[string]string{"CUDA_VISIBLE_DEVICES": "0,1"},
				},
			},
		}

		heartbeatEnv := &agentpb.Envelope{
			Payload: &agentpb.Envelope_Heartbeat{
				Heartbeat: &agentpb.Heartbeat{
					NodeId:        "node-42",
					TimestampUnix: 1700000000,
					CpuPercent:    85.3,
					MemoryPercent: 62.1,
					RunningJobIds: []string{"j-99"},
				},
			},
		}

		errCh := make(chan error, 2)

		go func() {
			errCh <- server.Send(ctx, submitJobEnv)
		}()
		go func() {
			errCh <- client.Send(ctx, heartbeatEnv)
		}()

		for range 2 {
			if err := <-errCh; err != nil {
				t.Fatalf("Send: %v", err)
			}
		}

		clientRecv, err := client.Recv(ctx)
		if err != nil {
			t.Fatalf("client Recv: %v", err)
		}
		sj := clientRecv.GetSubmitJob()
		if sj == nil {
			t.Fatal("client expected submit_job payload, got nil")
		}
		if sj.JobId != "j-99" {
			t.Errorf("expected job_id j-99, got %s", sj.JobId)
		}
		if sj.RunScript != "python train.py" {
			t.Errorf("expected run_script 'python train.py', got %s", sj.RunScript)
		}
		if sj.SetupScript != "pip install torch" {
			t.Errorf("expected setup_script 'pip install torch', got %s", sj.SetupScript)
		}
		if v, ok := sj.Env["CUDA_VISIBLE_DEVICES"]; !ok || v != "0,1" {
			t.Errorf("expected env CUDA_VISIBLE_DEVICES=0,1, got %v", sj.Env)
		}

		serverRecv, err := server.Recv(ctx)
		if err != nil {
			t.Fatalf("server Recv: %v", err)
		}
		hb := serverRecv.GetHeartbeat()
		if hb == nil {
			t.Fatal("server expected heartbeat payload, got nil")
		}
		if hb.NodeId != "node-42" {
			t.Errorf("expected node_id node-42, got %s", hb.NodeId)
		}
		if hb.CpuPercent != 85.3 {
			t.Errorf("expected cpu_percent 85.3, got %f", hb.CpuPercent)
		}
		if hb.MemoryPercent != 62.1 {
			t.Errorf("expected memory_percent 62.1, got %f", hb.MemoryPercent)
		}
		if len(hb.RunningJobIds) != 1 || hb.RunningJobIds[0] != "j-99" {
			t.Errorf("expected running_job_ids [j-99], got %v", hb.RunningJobIds)
		}
	})
}

func TestTunnel_ConcurrentSends(t *testing.T) {
	t.Run("given a tunnel pair, when multiple goroutines send concurrently, then all messages are received without corruption", func(t *testing.T) {
		server, client := setupTunnelPair(t)
		ctx := context.Background()

		const numSenders = 10
		errCh := make(chan error, numSenders)

		for i := range numSenders {
			go func(idx int) {
				env := &agentpb.Envelope{
					Payload: &agentpb.Envelope_Heartbeat{
						Heartbeat: &agentpb.Heartbeat{
							NodeId:        fmt.Sprintf("node-%d", idx),
							TimestampUnix: int64(idx),
							CpuPercent:    float64(idx),
						},
					},
				}
				errCh <- client.Send(ctx, env)
			}(i)
		}

		for range numSenders {
			if err := <-errCh; err != nil {
				t.Fatalf("concurrent Send: %v", err)
			}
		}

		seen := make(map[string]bool)
		for range numSenders {
			env, err := server.Recv(ctx)
			if err != nil {
				t.Fatalf("Recv: %v", err)
			}
			hb := env.GetHeartbeat()
			if hb == nil {
				t.Fatal("expected heartbeat payload, got nil")
			}
			if seen[hb.NodeId] {
				t.Errorf("duplicate node_id: %s", hb.NodeId)
			}
			seen[hb.NodeId] = true
		}

		if len(seen) != numSenders {
			t.Errorf("expected %d unique messages, got %d", numSenders, len(seen))
		}
	})
}

func TestTunnel_Close(t *testing.T) {
	t.Run("given a tunnel, when closed, then send returns an error", func(t *testing.T) {
		server, client := setupTunnelPair(t)
		ctx := context.Background()

		// drain server side so the close handshake can complete
		go func() { server.Recv(ctx) }()

		if err := client.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		env := &agentpb.Envelope{
			Payload: &agentpb.Envelope_Heartbeat{
				Heartbeat: &agentpb.Heartbeat{NodeId: "node-1"},
			},
		}
		if err := client.Send(ctx, env); err == nil {
			t.Error("expected error sending on closed tunnel, got nil")
		}
	})

	t.Run("given a tunnel, when closed twice, then second close returns nil", func(t *testing.T) {
		server, client := setupTunnelPair(t)
		ctx := context.Background()

		// drain server side so the close handshake can complete
		go func() { server.Recv(ctx) }()

		if err := client.Close(); err != nil {
			t.Fatalf("first Close: %v", err)
		}
		if err := client.Close(); err != nil {
			t.Errorf("second Close should return nil, got %v", err)
		}
	})
}
