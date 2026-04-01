package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestAnalyticsStore(t *testing.T) *SQLiteAnalyticsStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "analytics_test.db")
	s, err := NewSQLiteAnalytics(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteAnalytics: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteAnalyticsStore_InsertAndQueryMetricsByNodeID(t *testing.T) {
	t.Run("given inserted metrics, when queried by node_id and time range, then matching data is returned", func(t *testing.T) {
		s := newTestAnalyticsStore(t)
		ctx := context.Background()

		baseTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

		points := []MetricPoint{
			{
				Timestamp:      baseTime,
				NodeID:         "node-1",
				ClusterID:      "cluster-a",
				CPUPercent:     42.5,
				MemoryPercent:  60.0,
				DiskUsedBytes:  1024 * 1024 * 100,
				GPUIndex:       0,
				GPUUtilization: 95.0,
				GPUMemoryUsed:  8 * 1024 * 1024 * 1024,
				GPUTemperature: 72.0,
			},
			{
				Timestamp:     baseTime.Add(10 * time.Second),
				NodeID:        "node-1",
				ClusterID:     "cluster-a",
				CPUPercent:    55.0,
				MemoryPercent: 65.0,
				DiskUsedBytes: 1024 * 1024 * 200,
			},
		}

		if err := s.InsertMetrics(ctx, points); err != nil {
			t.Fatalf("InsertMetrics: %v", err)
		}

		tr := TimeRange{
			From: baseTime.Add(-1 * time.Minute),
			To:   baseTime.Add(1 * time.Minute),
		}
		got, err := s.QueryMetrics(ctx, "node-1", tr)
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 metric points, got %d", len(got))
		}
		if got[0].CPUPercent != 42.5 {
			t.Errorf("expected cpu_percent 42.5, got %f", got[0].CPUPercent)
		}
		if got[0].GPUUtilization != 95.0 {
			t.Errorf("expected gpu_utilization 95.0, got %f", got[0].GPUUtilization)
		}
		if got[1].CPUPercent != 55.0 {
			t.Errorf("expected cpu_percent 55.0 for second point, got %f", got[1].CPUPercent)
		}
		if got[0].NodeID != "node-1" {
			t.Errorf("expected node_id node-1, got %s", got[0].NodeID)
		}
		if got[0].ClusterID != "cluster-a" {
			t.Errorf("expected cluster_id cluster-a, got %s", got[0].ClusterID)
		}
	})
}

func TestSQLiteAnalyticsStore_QueryMetricsByClusterID(t *testing.T) {
	t.Run("given metrics for multiple nodes in a cluster, when queried by cluster_id, then all nodes metrics are returned", func(t *testing.T) {
		s := newTestAnalyticsStore(t)
		ctx := context.Background()

		baseTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

		points := []MetricPoint{
			{Timestamp: baseTime, NodeID: "node-1", ClusterID: "cluster-a", CPUPercent: 40.0},
			{Timestamp: baseTime.Add(1 * time.Second), NodeID: "node-2", ClusterID: "cluster-a", CPUPercent: 60.0},
			{Timestamp: baseTime.Add(2 * time.Second), NodeID: "node-3", ClusterID: "cluster-b", CPUPercent: 80.0},
		}

		if err := s.InsertMetrics(ctx, points); err != nil {
			t.Fatalf("InsertMetrics: %v", err)
		}

		tr := TimeRange{
			From: baseTime.Add(-1 * time.Minute),
			To:   baseTime.Add(1 * time.Minute),
		}

		got, err := s.QueryMetricsByCluster(ctx, "cluster-a", tr)
		if err != nil {
			t.Fatalf("QueryMetricsByCluster: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 metric points for cluster-a, got %d", len(got))
		}

		nodeIDs := map[string]bool{}
		for _, p := range got {
			nodeIDs[p.NodeID] = true
			if p.ClusterID != "cluster-a" {
				t.Errorf("expected cluster_id cluster-a, got %s", p.ClusterID)
			}
		}
		if !nodeIDs["node-1"] || !nodeIDs["node-2"] {
			t.Errorf("expected node-1 and node-2 in results, got %v", nodeIDs)
		}

		gotB, err := s.QueryMetricsByCluster(ctx, "cluster-b", tr)
		if err != nil {
			t.Fatalf("QueryMetricsByCluster(cluster-b): %v", err)
		}
		if len(gotB) != 1 {
			t.Fatalf("expected 1 metric point for cluster-b, got %d", len(gotB))
		}
		if gotB[0].NodeID != "node-3" {
			t.Errorf("expected node-3, got %s", gotB[0].NodeID)
		}
	})
}

func TestSQLiteAnalyticsStore_InsertAndQueryLogs(t *testing.T) {
	t.Run("given inserted logs, when queried by job_id, then logs are returned in timestamp order", func(t *testing.T) {
		s := newTestAnalyticsStore(t)
		ctx := context.Background()

		baseTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

		entries := []LogEntry{
			{Timestamp: baseTime.Add(2 * time.Second), JobID: "j-1", NodeID: "node-1", Stream: "stdout", Line: []byte("line three")},
			{Timestamp: baseTime, JobID: "j-1", NodeID: "node-1", Stream: "stdout", Line: []byte("line one")},
			{Timestamp: baseTime.Add(1 * time.Second), JobID: "j-1", NodeID: "node-1", Stream: "stderr", Line: []byte("line two")},
		}

		if err := s.InsertLogs(ctx, entries); err != nil {
			t.Fatalf("InsertLogs: %v", err)
		}

		tr := TimeRange{
			From: baseTime.Add(-1 * time.Minute),
			To:   baseTime.Add(1 * time.Minute),
		}
		got, err := s.QueryLogs(ctx, "j-1", tr, 100)
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 log entries, got %d", len(got))
		}

		if string(got[0].Line) != "line one" {
			t.Errorf("expected first line 'line one', got %q", string(got[0].Line))
		}
		if string(got[1].Line) != "line two" {
			t.Errorf("expected second line 'line two', got %q", string(got[1].Line))
		}
		if string(got[2].Line) != "line three" {
			t.Errorf("expected third line 'line three', got %q", string(got[2].Line))
		}

		if got[1].Stream != "stderr" {
			t.Errorf("expected stream stderr for second entry, got %s", got[1].Stream)
		}
		if got[0].JobID != "j-1" {
			t.Errorf("expected job_id j-1, got %s", got[0].JobID)
		}
	})
}

func TestSQLiteAnalyticsStore_LogsFilteredByJobID(t *testing.T) {
	t.Run("given logs for multiple jobs, when queried by job_id, then only matching logs are returned", func(t *testing.T) {
		s := newTestAnalyticsStore(t)
		ctx := context.Background()

		baseTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

		entries := []LogEntry{
			{Timestamp: baseTime, JobID: "j-1", NodeID: "node-1", Stream: "stdout", Line: []byte("job one output")},
			{Timestamp: baseTime.Add(1 * time.Second), JobID: "j-2", NodeID: "node-1", Stream: "stdout", Line: []byte("job two output")},
			{Timestamp: baseTime.Add(2 * time.Second), JobID: "j-1", NodeID: "node-1", Stream: "stdout", Line: []byte("job one more")},
		}

		if err := s.InsertLogs(ctx, entries); err != nil {
			t.Fatalf("InsertLogs: %v", err)
		}

		tr := TimeRange{From: baseTime.Add(-1 * time.Minute), To: baseTime.Add(1 * time.Minute)}

		got, err := s.QueryLogs(ctx, "j-1", tr, 100)
		if err != nil {
			t.Fatalf("QueryLogs(j-1): %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 log entries for j-1, got %d", len(got))
		}
		for _, e := range got {
			if e.JobID != "j-1" {
				t.Errorf("expected job_id j-1, got %s", e.JobID)
			}
		}
	})
}

func TestSQLiteAnalyticsStore_MetricsTimeRangeFiltering(t *testing.T) {
	t.Run("given metrics at different times, when queried with a narrow time range, then only matching metrics are returned", func(t *testing.T) {
		s := newTestAnalyticsStore(t)
		ctx := context.Background()

		t1 := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		t3 := time.Date(2025, 1, 1, 14, 0, 0, 0, time.UTC)

		points := []MetricPoint{
			{Timestamp: t1, NodeID: "node-1", ClusterID: "c-1", CPUPercent: 10.0},
			{Timestamp: t2, NodeID: "node-1", ClusterID: "c-1", CPUPercent: 50.0},
			{Timestamp: t3, NodeID: "node-1", ClusterID: "c-1", CPUPercent: 90.0},
		}

		if err := s.InsertMetrics(ctx, points); err != nil {
			t.Fatalf("InsertMetrics: %v", err)
		}

		narrowRange := TimeRange{
			From: t2.Add(-30 * time.Minute),
			To:   t2.Add(30 * time.Minute),
		}
		got, err := s.QueryMetrics(ctx, "node-1", narrowRange)
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 metric point in narrow range, got %d", len(got))
		}
		if got[0].CPUPercent != 50.0 {
			t.Errorf("expected cpu_percent 50.0, got %f", got[0].CPUPercent)
		}

		wideRange := TimeRange{
			From: t1.Add(-1 * time.Hour),
			To:   t3.Add(1 * time.Hour),
		}
		gotAll, err := s.QueryMetrics(ctx, "node-1", wideRange)
		if err != nil {
			t.Fatalf("QueryMetrics wide range: %v", err)
		}
		if len(gotAll) != 3 {
			t.Fatalf("expected 3 metric points in wide range, got %d", len(gotAll))
		}
	})
}

func TestSQLiteAnalyticsStore_LogsTimeRangeFiltering(t *testing.T) {
	t.Run("given logs at different times, when queried with a narrow time range, then only matching logs are returned", func(t *testing.T) {
		s := newTestAnalyticsStore(t)
		ctx := context.Background()

		t1 := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
		t3 := time.Date(2025, 1, 1, 14, 0, 0, 0, time.UTC)

		entries := []LogEntry{
			{Timestamp: t1, JobID: "j-1", NodeID: "n-1", Stream: "stdout", Line: []byte("early")},
			{Timestamp: t2, JobID: "j-1", NodeID: "n-1", Stream: "stdout", Line: []byte("middle")},
			{Timestamp: t3, JobID: "j-1", NodeID: "n-1", Stream: "stdout", Line: []byte("late")},
		}

		if err := s.InsertLogs(ctx, entries); err != nil {
			t.Fatalf("InsertLogs: %v", err)
		}

		narrowRange := TimeRange{
			From: t1.Add(-1 * time.Minute),
			To:   t1.Add(1 * time.Minute),
		}
		got, err := s.QueryLogs(ctx, "j-1", narrowRange, 100)
		if err != nil {
			t.Fatalf("QueryLogs narrow: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 log entry in narrow range, got %d", len(got))
		}
		if string(got[0].Line) != "early" {
			t.Errorf("expected 'early', got %q", string(got[0].Line))
		}
	})
}

func TestSQLiteAnalyticsStore_CostEvents(t *testing.T) {
	t.Run("given cost events, when queried and totaled, then correct values are returned", func(t *testing.T) {
		s := newTestAnalyticsStore(t)
		ctx := context.Background()

		baseTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

		if err := s.InsertCostEvent(ctx, CostEvent{
			Timestamp:    baseTime,
			ClusterID:    "c-1",
			Cloud:        "aws",
			Region:       "us-east-1",
			InstanceType: "p3.2xlarge",
			NodeID:       "node-1",
			HourlyCost:   3.06,
			IsSpot:       false,
		}); err != nil {
			t.Fatalf("InsertCostEvent: %v", err)
		}
		if err := s.InsertCostEvent(ctx, CostEvent{
			Timestamp:    baseTime.Add(1 * time.Hour),
			ClusterID:    "c-1",
			Cloud:        "aws",
			Region:       "us-east-1",
			InstanceType: "p3.2xlarge",
			NodeID:       "node-1",
			HourlyCost:   3.06,
			IsSpot:       true,
		}); err != nil {
			t.Fatalf("InsertCostEvent: %v", err)
		}

		tr := TimeRange{
			From: baseTime.Add(-1 * time.Hour),
			To:   baseTime.Add(2 * time.Hour),
		}

		costs, err := s.QueryCosts(ctx, "c-1", tr)
		if err != nil {
			t.Fatalf("QueryCosts: %v", err)
		}
		if len(costs) != 2 {
			t.Fatalf("expected 2 cost events, got %d", len(costs))
		}
		if !costs[1].IsSpot {
			t.Error("expected second cost event to be spot")
		}
		if costs[0].IsSpot {
			t.Error("expected first cost event to not be spot")
		}

		total, err := s.TotalCost(ctx, "c-1", tr)
		if err != nil {
			t.Fatalf("TotalCost: %v", err)
		}
		expectedTotal := 6.12
		if total < expectedTotal-0.01 || total > expectedTotal+0.01 {
			t.Errorf("expected total cost ~%.2f, got %.2f", expectedTotal, total)
		}
	})
}

func TestSQLiteAnalyticsStore_EmptyInsertIsNoop(t *testing.T) {
	t.Run("given empty slices, when inserting, then no error occurs", func(t *testing.T) {
		s := newTestAnalyticsStore(t)
		ctx := context.Background()

		if err := s.InsertMetrics(ctx, nil); err != nil {
			t.Fatalf("InsertMetrics(nil): %v", err)
		}
		if err := s.InsertMetrics(ctx, []MetricPoint{}); err != nil {
			t.Fatalf("InsertMetrics(empty): %v", err)
		}
		if err := s.InsertLogs(ctx, nil); err != nil {
			t.Fatalf("InsertLogs(nil): %v", err)
		}
		if err := s.InsertLogs(ctx, []LogEntry{}); err != nil {
			t.Fatalf("InsertLogs(empty): %v", err)
		}
	})
}
