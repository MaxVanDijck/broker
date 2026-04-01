package cli

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"broker/internal/client"
)

const defaultAddr = "http://localhost:8080"

func serverAddr() string {
	if addr := os.Getenv("BROKER_API_ADDR"); addr != "" {
		return addr
	}
	return defaultAddr
}

func brokerToken() string {
	return os.Getenv("BROKER_TOKEN")
}

func newClient() *client.Client {
	ensureServer()
	if !sshConfigInstalled() {
		installSSHConfig()
	}
	return client.New(serverAddr(), brokerToken())
}

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "broker",
		Short:         "Run AI workloads on any infrastructure",
		Long:          "broker is a fast, unified interface for launching and managing AI compute across clouds and Kubernetes.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		serveCmd(),
		launchCmd(),
		statusCmd(),
		stopCmd(),
		startCmd(),
		downCmd(),
		execCmd(),
		logsCmd(),
		cancelCmd(),
		sshCmd(),
		sshConfigCmd(),
		versionCmd(),
	)

	return root
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the broker version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("broker v0.1.0")
		},
	}
}

// ensureServer checks if a server is running and starts one as a detached
// background process if not. The server persists after the CLI exits.
func ensureServer() {
	addr := serverAddr()

	if serverIsUp(addr) {
		return
	}

	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".broker")
	os.MkdirAll(dataDir, 0o755)

	logFile := filepath.Join(dataDir, "server.log")

	// Start the server as a detached background process using our own
	// binary with the hidden `serve` subcommand.
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}

	self, err := os.Executable()
	if err != nil {
		f.Close()
		return
	}

	cmd := exec.Command(self, "serve")
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		f.Close()
		return
	}

	// Detach: don't wait for the child process
	cmd.Process.Release()
	f.Close()

	// Write PID for later cleanup
	pidFile := filepath.Join(dataDir, "server.pid")
	os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0o644)

	// Wait for server + agent to be ready
	for range 50 {
		time.Sleep(100 * time.Millisecond)
		if serverIsReady(addr) {
			return
		}
	}
}

func serverIsUp(addr string) bool {
	req, err := http.NewRequest(http.MethodGet, addr+"/healthz", nil)
	if err != nil {
		return false
	}
	if token := brokerToken(); token != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("broker:"+token)))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func serverIsReady(addr string) bool {
	req, err := http.NewRequest(http.MethodGet, addr+"/readyz", nil)
	if err != nil {
		return false
	}
	if token := brokerToken(); token != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("broker:"+token)))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}
