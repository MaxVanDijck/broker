package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var validWorkdirID = regexp.MustCompile("^[a-zA-Z0-9_-]+$")

// Workdir storage. Tarballs are stored on disk at ~/.broker/workdirs/{id}.tar.gz.
// The CLI uploads them before launching a job. The agent downloads them
// before executing.

func (s *Server) workdirPath(id string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".broker", "workdirs", id+".tar.gz"), nil
}

func (s *Server) handleWorkdirUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/workdir/")
	if id == "" {
		http.Error(w, "missing workdir id", http.StatusBadRequest)
		return
	}
	if !validWorkdirID.MatchString(id) {
		http.Error(w, "invalid workdir id", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 512<<20)

	path, err := s.workdirPath(id)
	if err != nil {
		s.logger.Error("failed to resolve workdir path", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		s.logger.Error("failed to create workdir directory", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	f, err := os.Create(path)
	if err != nil {
		s.logger.Error("failed to create workdir file", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	n, err := io.Copy(f, r.Body)
	if err != nil {
		os.Remove(path)
		s.logger.Error("failed to write workdir file", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if err := f.Sync(); err != nil {
		s.logger.Error("failed to sync workdir file", "error", err)
	}

	s.logger.Info("workdir uploaded", "id", id, "size_bytes", n)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":"%s","size":%d}`, id, n)
}

func (s *Server) handleWorkdirDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/v1/workdir/")
	if id == "" {
		http.Error(w, "missing workdir id", http.StatusBadRequest)
		return
	}
	if !validWorkdirID.MatchString(id) {
		http.Error(w, "invalid workdir id", http.StatusBadRequest)
		return
	}

	path, err := s.workdirPath(id)
	if err != nil {
		s.logger.Error("failed to resolve workdir path", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "workdir not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		s.logger.Error("failed to stat workdir file", "id", id, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	if _, err := io.Copy(w, f); err != nil {
		s.logger.Error("failed to send workdir", "id", id, "error", err)
	}
}

func (s *Server) handleWorkdir(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleWorkdirUpload(w, r)
	case http.MethodGet:
		s.handleWorkdirDownload(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
