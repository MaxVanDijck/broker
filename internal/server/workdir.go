package server

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Workdir storage. Tarballs are stored on disk at ~/.broker/workdirs/{id}.tar.gz.
// The CLI uploads them before launching a job. The agent downloads them
// before executing.

func (s *Server) workdirPath(id string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".broker", "workdirs", id+".tar.gz")
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

	path := s.workdirPath(id)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	f, err := os.Create(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	n, err := io.Copy(f, r.Body)
	if err != nil {
		os.Remove(path)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	s.logger.Info("workdir uploaded", "id", id, "size_bytes", n)
	w.WriteHeader(http.StatusOK)
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

	path := s.workdirPath(id)
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "workdir not found", http.StatusNotFound)
		return
	}
	defer f.Close()

	stat, _ := f.Stat()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	io.Copy(w, f)
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
