package server

import (
	"embed"
	"net/http"
)

//go:embed agentbin/*
var agentBinFS embed.FS

func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := agentBinFS.ReadFile("agentbin/broker-agent")
	if err != nil {
		s.logger.Error("agent binary not embedded (build with 'make build-server')")
		http.Error(w, "agent binary not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=broker-agent")
	w.Write(data)
}
