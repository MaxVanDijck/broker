package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"broker/internal/domain"
)

func (s *Server) resolveCluster(nameOrID string) (*domain.Cluster, error) {
	c, err := s.store.GetClusterByID(nameOrID)
	if err != nil {
		return nil, err
	}
	if c != nil {
		return c, nil
	}
	return s.store.GetCluster(nameOrID)
}

func (s *Server) handleClusterInfoAPI(w http.ResponseWriter, _ *http.Request, cluster *domain.Cluster) {
	item := clusterListItemJSON{
		ID:         cluster.ID,
		Name:       cluster.Name,
		Status:     string(cluster.Status),
		Cloud:      string(cluster.Cloud),
		Region:     cluster.Region,
		HeadIP:     cluster.HeadIP,
		NumNodes:   cluster.NumNodes,
		LaunchedAt: cluster.LaunchedAt.Format(time.RFC3339),
	}
	if cluster.Resources != nil {
		item.Resources = cluster.Resources.String()
		item.InstanceType = cluster.Resources.InstanceType
		item.IsSpot = cluster.Resources.UseSpot
	}
	w.Header().Set("Content-Type", "application/json")
	// Error ignored: response already committed
	json.NewEncoder(w).Encode(item)
}

func (s *Server) handleClusterDeleteAPI(w http.ResponseWriter, _ *http.Request, cluster *domain.Cluster) {
	s.logger.Info("tearing down cluster via API", "name", cluster.Name, "id", cluster.ID)
	s.teardownCluster(cluster)

	w.Header().Set("Content-Type", "application/json")
	// Error ignored: response already committed
	json.NewEncoder(w).Encode(map[string]string{
		"id":     cluster.ID,
		"name":   cluster.Name,
		"status": string(domain.ClusterStatusTerminating),
	})
}

func (s *Server) handleClusterSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/clusters/")
	if path == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	parts := strings.SplitN(path, "/", 2)
	nameOrID := parts[0]
	subResource := ""
	if len(parts) == 2 {
		subResource = parts[1]
	}

	if subResource == "ssh" {
		s.handleSSHProxy(w, r)
		return
	}

	cluster, err := s.resolveCluster(nameOrID)
	if err != nil {
		s.logger.Error("failed to resolve cluster", "cluster", nameOrID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.Error(w, fmt.Sprintf("cluster %q not found", nameOrID), http.StatusNotFound)
		return
	}

	if r.Method == http.MethodDelete && subResource == "" {
		s.handleClusterDeleteAPI(w, r, cluster)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch subResource {
	case "":
		s.handleClusterInfoAPI(w, r, cluster)
	case "nodes":
		s.handleNodesAPI(w, r, cluster.Name)
	case "metrics":
		s.handleMetricsAPI(w, r, cluster.Name)
	case "costs":
		s.handleClusterCostsAPI(w, r, cluster.Name)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}
