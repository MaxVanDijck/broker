package server

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// agentBinaryCache holds a pre-built linux/amd64 agent binary in memory.
// Built lazily on first request. This is a temporary bootstrap mechanism --
// replace with custom AMIs once a Packer pipeline exists. If an instance
// cannot retrieve this binary, the agent will not start and the node will
// have no self-termination (dead man's switch), leaving an orphaned instance
// running indefinitely.
var agentBinaryCache struct {
	once sync.Once
	data []byte
	err  error
}

func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agentBinaryCache.once.Do(func() {
		agentBinaryCache.data, agentBinaryCache.err = buildAgentBinary(s)
	})

	if agentBinaryCache.err != nil {
		s.logger.Error("agent binary build failed", "error", agentBinaryCache.err)
		http.Error(w, "agent binary not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=broker-agent")
	w.Write(agentBinaryCache.data)
}

func buildAgentBinary(s *Server) ([]byte, error) {
	s.logger.Info("building linux/amd64 agent binary (this may take a moment)")

	tmpDir, err := os.MkdirTemp("", "broker-agent-build-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	outPath := filepath.Join(tmpDir, "broker-agent")

	// Find the module root by looking for go.mod relative to the current executable
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	moduleRoot := findModuleRoot(filepath.Dir(self))
	if moduleRoot == "" {
		moduleRoot = "."
	}

	cmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", outPath, "./cmd/broker-agent")
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("go build failed", "output", string(output))
		return nil, err
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		return nil, err
	}

	s.logger.Info("agent binary built", "size_mb", len(data)/(1024*1024))
	return data, nil
}

func findModuleRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
