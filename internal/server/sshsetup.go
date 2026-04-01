package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleSSHSetup installs the broker SSH config so that `ssh <cluster>.broker`
// and VS Code Remote SSH work without any manual setup. Called by the dashboard
// before opening a VS Code URI.
func (s *Server) handleSSHSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	brokerDir := filepath.Join(home, ".broker")
	os.MkdirAll(brokerDir, 0o755)

	brokerBin, err := findBrokerBinary()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	managedConfig := fmt.Sprintf("# broker-managed\nHost *.broker\n    StrictHostKeyChecking no\n    UserKnownHostsFile /dev/null\n    LogLevel ERROR\n    User root\n    ProxyCommand %s ssh --stdio --hostname-suffix .broker %%h\n", brokerBin)

	managedPath := filepath.Join(brokerDir, "ssh_config")
	if err := os.WriteFile(managedPath, []byte(managedConfig), 0o644); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(sshDir, 0o700)

	sshConfigPath := filepath.Join(sshDir, "config")
	existing, _ := os.ReadFile(sshConfigPath)

	if !strings.Contains(string(existing), managedPath) {
		updated := fmt.Sprintf("Include %s\n", managedPath) + string(existing)
		if err := os.WriteFile(sshConfigPath, []byte(updated), 0o644); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"ok":true}`))
}

func findBrokerBinary() (string, error) {
	// Check if broker CLI is on PATH
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		path := filepath.Join(dir, "broker")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// Fall back to the server binary's directory
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	candidate := filepath.Join(filepath.Dir(self), "broker")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	return "", fmt.Errorf("broker binary not found in PATH or next to server binary")
}
