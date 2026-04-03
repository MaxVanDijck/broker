package server

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	ws "github.com/coder/websocket"

	"broker/internal/provider"
	"broker/internal/store"
	"broker/internal/tunnel"
	agentpb "broker/proto/agentpb"
)

// startEchoTCPServer starts a TCP server that echoes back any data it receives,
// prefixed with "echo:". Returns the listener and its address.
func startEchoTCPServer(t *testing.T) (net.Listener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					response := append([]byte("echo:"), buf[:n]...)
					c.Write(response)
				}
			}(conn)
		}
	}()

	return ln, ln.Addr().String()
}

func TestSSHProxy_EndToEnd(t *testing.T) {
	t.Run("given a connected agent with ssh relay, when a client connects to ssh proxy, then data round-trips through the tunnel", func(t *testing.T) {
		// Start a local TCP echo server (simulates sshd)
		_, echoAddr := startEchoTCPServer(t)
		echoHost, echoPort, _ := net.SplitHostPort(echoAddr)
		_ = echoHost

		dbPath := t.TempDir() + "/sshproxy.db"
		db, err := store.NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("failed to create sqlite store: %v", err)
		}
		t.Cleanup(func() { db.Close() })

		logger := slog.Default()
		registry := provider.NewRegistry()
		srv := New(db, nil, registry, logger)

		mux := http.NewServeMux()
		mux.Handle("/agent/v1/connect", srv.Tunnel)
		mux.HandleFunc("/api/v1/clusters/", srv.handleClusterSubroutes)

		hs := httptest.NewServer(mux)
		t.Cleanup(hs.Close)

		// Connect the agent via WebSocket
		agentWSURL := "ws" + strings.TrimPrefix(hs.URL, "http") + "/agent/v1/connect"
		agentCtx, agentCancel := context.WithCancel(context.Background())
		t.Cleanup(agentCancel)

		agentConn, _, err := ws.Dial(agentCtx, agentWSURL, nil)
		if err != nil {
			t.Fatalf("agent websocket dial: %v", err)
		}
		agentConn.SetReadLimit(4 << 20)
		agentTun := tunnel.New(agentConn, logger)
		t.Cleanup(func() { agentTun.Close() })

		// Register the agent
		regCtx, regCancel := context.WithTimeout(agentCtx, 5*time.Second)
		defer regCancel()
		if err := agentTun.Send(regCtx, &agentpb.Envelope{
			Payload: &agentpb.Envelope_Register{
				Register: &agentpb.Register{
					NodeId:      "ssh-node-1",
					ClusterName: "ssh-cluster",
					NodeInfo: &agentpb.NodeInfo{
						Hostname: "ssh-test-host",
						Os:       "linux",
						Arch:     "amd64",
						SshPort:  0, // We'll use our echo server port instead
					},
				},
			},
		}); err != nil {
			t.Fatalf("send register: %v", err)
		}

		ack, err := agentTun.Recv(regCtx)
		if err != nil {
			t.Fatalf("recv register ack: %v", err)
		}
		if regAck := ack.GetRegisterAck(); regAck == nil || !regAck.Accepted {
			t.Fatalf("registration not accepted")
		}

		time.Sleep(100 * time.Millisecond)

		// Connect a client to the SSH proxy endpoint
		clientWSURL := "ws" + strings.TrimPrefix(hs.URL, "http") + "/api/v1/clusters/ssh-cluster/ssh"
		clientCtx, clientCancel := context.WithCancel(context.Background())
		t.Cleanup(clientCancel)

		clientConn, _, err := ws.Dial(clientCtx, clientWSURL, nil)
		if err != nil {
			t.Fatalf("client websocket dial: %v", err)
		}
		t.Cleanup(func() { clientConn.Close(ws.StatusNormalClosure, "done") })

		// The server sends an SSHSessionData to the agent to open a session.
		// The agent should receive this and connect to local sshd.
		openCtx, openCancel := context.WithTimeout(agentCtx, 5*time.Second)
		defer openCancel()
		openEnv, err := agentTun.Recv(openCtx)
		if err != nil {
			t.Fatalf("agent recv session open: %v", err)
		}
		sshData := openEnv.GetSshSessionData()
		if sshData == nil {
			t.Fatalf("expected SSHSessionData, got %T", openEnv.Payload)
		}
		sessionID := sshData.SessionId
		if sessionID == "" {
			t.Fatal("expected non-empty session ID")
		}

		// Simulate the agent: connect to our echo TCP server instead of real sshd
		localConn, err := net.Dial("tcp", echoAddr)
		if err != nil {
			t.Fatalf("dial echo server: %v", err)
		}
		t.Cleanup(func() { localConn.Close() })

		// Start a goroutine to read from the local connection and relay back through the tunnel
		var agentWg sync.WaitGroup
		agentWg.Add(1)
		go func() {
			defer agentWg.Done()
			buf := make([]byte, 32*1024)
			for {
				n, err := localConn.Read(buf)
				if n > 0 {
					data := make([]byte, n)
					copy(data, buf[:n])
					if sendErr := agentTun.Send(agentCtx, &agentpb.Envelope{
						Payload: &agentpb.Envelope_SshSession{SshSession: &agentpb.SSHSession{
							SessionId: sessionID,
							Data:      data,
						}},
					}); sendErr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}()

		// Start another goroutine to read from the agent tunnel and forward to local conn
		agentWg.Add(1)
		go func() {
			defer agentWg.Done()
			for {
				env, err := agentTun.Recv(agentCtx)
				if err != nil {
					return
				}
				if sd := env.GetSshSessionData(); sd != nil {
					if sd.Closed {
						localConn.Close()
						return
					}
					if len(sd.Data) > 0 {
						localConn.Write(sd.Data)
					}
				}
			}
		}()

		// Client sends data through the SSH proxy
		testData := []byte("hello ssh tunnel")
		if err := clientConn.Write(clientCtx, ws.MessageBinary, testData); err != nil {
			t.Fatalf("client write: %v", err)
		}

		// Client should receive echoed data back
		readCtx, readCancel := context.WithTimeout(clientCtx, 5*time.Second)
		defer readCancel()
		_, received, err := clientConn.Read(readCtx)
		if err != nil {
			t.Fatalf("client read: %v", err)
		}

		expected := "echo:" + string(testData)
		if string(received) != expected {
			t.Errorf("expected %q, got %q", expected, string(received))
		}

		// Send another message to verify the session stays open
		testData2 := []byte("second message")
		if err := clientConn.Write(clientCtx, ws.MessageBinary, testData2); err != nil {
			t.Fatalf("client write 2: %v", err)
		}

		readCtx2, readCancel2 := context.WithTimeout(clientCtx, 5*time.Second)
		defer readCancel2()
		_, received2, err := clientConn.Read(readCtx2)
		if err != nil {
			t.Fatalf("client read 2: %v", err)
		}

		expected2 := "echo:" + string(testData2)
		if string(received2) != expected2 {
			t.Errorf("expected %q, got %q", expected2, string(received2))
		}

		// Verify session exists in the session manager
		_, exists := srv.sshSessions.get(sessionID)
		if !exists {
			t.Error("expected SSH session to exist in session manager")
		}

		// Disconnect the client
		clientConn.Close(ws.StatusNormalClosure, "done")
		agentCancel()

		time.Sleep(200 * time.Millisecond)

		// Verify session was cleaned up
		_, exists = srv.sshSessions.get(sessionID)
		if exists {
			t.Error("expected SSH session to be removed after client disconnect")
		}

		_ = echoPort
	})
}

func TestSSHProxy_NoAgentReturnsServiceUnavailable(t *testing.T) {
	t.Run("given no agent connected, when a client connects to ssh proxy, then 503 is returned", func(t *testing.T) {
		dbPath := t.TempDir() + "/sshproxy-noagent.db"
		db, err := store.NewSQLite(dbPath)
		if err != nil {
			t.Fatalf("failed to create sqlite store: %v", err)
		}
		t.Cleanup(func() { db.Close() })

		logger := slog.Default()
		registry := provider.NewRegistry()
		srv := New(db, nil, registry, logger)

		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/clusters/", srv.handleClusterSubroutes)

		hs := httptest.NewServer(mux)
		t.Cleanup(hs.Close)

		// Try to connect to SSH proxy for a cluster with no agent
		resp, err := http.Get(hs.URL + "/api/v1/clusters/nonexistent/ssh")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", resp.StatusCode)
		}
	})
}
