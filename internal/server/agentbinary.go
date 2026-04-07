package server

import (
	"embed"
	"net/http"
)

//go:embed agentbin/*
var agentBinFS embed.FS

var agentBinaryData []byte

func init() {
	data, err := agentBinFS.ReadFile("agentbin/broker-agent")
	if err != nil {
		return
	}
	agentBinaryData = data
}

func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if agentBinaryData == nil {
		s.logger.Error("agent binary not embedded (build with 'make build-server')")
		http.Error(w, "agent binary not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=broker-agent")
	w.Write(agentBinaryData)
}
