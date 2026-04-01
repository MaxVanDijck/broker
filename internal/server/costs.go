package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"broker/internal/domain"
	"broker/internal/provider/aws"
	"broker/internal/store"
)

func (s *Server) runCostTracker(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recordCosts(ctx)
		}
	}
}

func (s *Server) recordCosts(ctx context.Context) {
	if s.analytics == nil {
		return
	}

	clusters, err := s.store.ListClusters()
	if err != nil {
		s.logger.Error("cost tracker: failed to list clusters", "error", err)
		return
	}

	now := time.Now().UTC()

	for _, c := range clusters {
		if c.Status != domain.ClusterStatusUp {
			continue
		}

		if c.Resources == nil {
			continue
		}

		instanceType := resolveClusterInstanceType(c)
		if instanceType == "" {
			continue
		}

		hourlyPrice, ok := aws.OnDemandPricing[instanceType]
		if !ok {
			continue
		}

		if c.Resources.UseSpot {
			hourlyPrice *= 0.35
		}

		perMinuteCost := hourlyPrice / 60.0

		// Persist resolved instance type back to cluster so the costs API can use it too
		if c.Resources.InstanceType == "" {
			c.Resources.InstanceType = instanceType
			s.store.UpdateCluster(c)
		}

		event := store.CostEvent{
			Timestamp:    now,
			ClusterID:    c.ID,
			Cloud:        string(c.Cloud),
			Region:       c.Region,
			InstanceType: instanceType,
			HourlyCost:   perMinuteCost,
			IsSpot:       c.Resources.UseSpot,
		}

		if err := s.analytics.InsertCostEvent(ctx, event); err != nil {
			s.logger.Error("cost tracker: failed to insert cost event",
				"cluster", c.Name,
				"error", err,
			)
		}
	}
}

type costClusterJSON struct {
	ClusterName  string  `json:"cluster_name"`
	ClusterID    string  `json:"cluster_id"`
	HourlyRate   float64 `json:"hourly_rate"`
	TotalCost    float64 `json:"total_cost"`
	IsSpot       bool    `json:"is_spot"`
	InstanceType string  `json:"instance_type"`
	Status       string  `json:"status"`
}

type costSummaryResponse struct {
	Clusters   []costClusterJSON `json:"clusters"`
	Total      float64           `json:"total"`
	Disclaimer string            `json:"disclaimer"`
}

// resolveClusterInstanceType returns the EC2 instance type for a cluster,
// resolving from accelerators if the user didn't specify one directly.
func resolveClusterInstanceType(c *domain.Cluster) string {
	if c.Resources == nil {
		return ""
	}
	if c.Resources.InstanceType != "" {
		return c.Resources.InstanceType
	}
	if c.Resources.Accelerators != "" {
		if it, ok := aws.MapAcceleratorToInstanceType(c.Resources.Accelerators); ok {
			return it
		}
	}
	return ""
}

func (s *Server) handleCostsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	clusters, err := s.store.ListClusters()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	allTime := store.TimeRange{
		From: time.Time{},
		To:   time.Now().Add(time.Minute),
	}

	var result []costClusterJSON
	var grandTotal float64

	for _, c := range clusters {
		instanceType := resolveClusterInstanceType(c)
		if instanceType == "" {
			continue
		}

		hourlyRate, ok := aws.OnDemandPricing[instanceType]
		if !ok {
			continue
		}

		isSpot := c.Resources != nil && c.Resources.UseSpot
		if isSpot {
			hourlyRate *= 0.35
		}

		var totalCost float64
		if s.analytics != nil {
			totalCost, _ = s.analytics.TotalCost(r.Context(), c.ID, allTime)
		}

		grandTotal += totalCost
		result = append(result, costClusterJSON{
			ClusterName:  c.Name,
			ClusterID:    c.ID,
			HourlyRate:   hourlyRate,
			TotalCost:    totalCost,
			IsSpot:       isSpot,
			InstanceType: instanceType,
			Status:       string(c.Status),
		})
	}

	if result == nil {
		result = []costClusterJSON{}
	}

	resp := costSummaryResponse{
		Clusters:   result,
		Total:      grandTotal,
		Disclaimer: "Costs are estimates based on on-demand pricing. Actual AWS billing may differ.",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type clusterCostEventsResponse struct {
	ClusterName string          `json:"cluster_name"`
	Events      []costEventJSON `json:"events"`
	TotalCost   float64         `json:"total_cost"`
}

type costEventJSON struct {
	Timestamp    time.Time `json:"timestamp"`
	InstanceType string    `json:"instance_type"`
	HourlyCost   float64   `json:"hourly_cost"`
	IsSpot       bool      `json:"is_spot"`
}

func (s *Server) handleClusterCostsAPI(w http.ResponseWriter, r *http.Request, clusterName string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cluster, err := s.resolveCluster(clusterName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.Error(w, fmt.Sprintf("cluster %q not found", clusterName), http.StatusNotFound)
		return
	}

	now := time.Now()
	tr := store.TimeRange{
		From: cluster.LaunchedAt,
		To:   now.Add(time.Minute),
	}

	if fromStr := r.URL.Query().Get("from"); fromStr != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, fromStr); parseErr == nil {
			tr.From = parsed
		}
	}
	if toStr := r.URL.Query().Get("to"); toStr != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, toStr); parseErr == nil {
			tr.To = parsed
		}
	}

	var events []store.CostEvent
	var totalCost float64
	if s.analytics != nil {
		events, err = s.analytics.QueryCosts(r.Context(), cluster.ID, tr)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		totalCost, _ = s.analytics.TotalCost(r.Context(), cluster.ID, tr)
	}

	jsonEvents := make([]costEventJSON, 0, len(events))
	for _, e := range events {
		jsonEvents = append(jsonEvents, costEventJSON{
			Timestamp:    e.Timestamp,
			InstanceType: e.InstanceType,
			HourlyCost:   e.HourlyCost,
			IsSpot:       e.IsSpot,
		})
	}

	resp := clusterCostEventsResponse{
		ClusterName: clusterName,
		Events:      jsonEvents,
		TotalCost:   totalCost,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

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
	json.NewEncoder(w).Encode(item)
}

func (s *Server) handleClusterDeleteAPI(w http.ResponseWriter, _ *http.Request, cluster *domain.Cluster) {
	s.logger.Info("tearing down cluster via API", "name", cluster.Name, "id", cluster.ID)
	s.teardownCluster(cluster)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":     cluster.ID,
		"name":   cluster.Name,
		"status": string(domain.ClusterStatusTerminating),
	})
}

func (s *Server) handleClusterOrSSHProxyOrCosts(w http.ResponseWriter, r *http.Request) {
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

	// Resolve the path segment as either a cluster UUID or name.
	cluster, err := s.resolveCluster(nameOrID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if cluster == nil {
		http.Error(w, fmt.Sprintf("cluster %q not found", nameOrID), http.StatusNotFound)
		return
	}

	// DELETE /api/v1/clusters/{id} - teardown by UUID
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
