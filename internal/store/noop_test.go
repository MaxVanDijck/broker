package store

import (
	"context"
	"testing"
	"time"
)

func TestNoopAnalyticsStore(t *testing.T) {
	ctx := context.Background()
	s := NewNoopAnalytics()
	tr := TimeRange{From: time.Now().Add(-time.Hour), To: time.Now()}

	t.Run("given a noop store, when inserting logs, then no error is returned", func(t *testing.T) {
		err := s.InsertLogs(ctx, []LogEntry{{JobID: "j-1", Line: []byte("hello")}})
		if err != nil {
			t.Fatalf("InsertLogs: %v", err)
		}
	})

	t.Run("given a noop store, when querying logs, then nil is returned", func(t *testing.T) {
		logs, err := s.QueryLogs(ctx, "j-1", tr, 100)
		if err != nil {
			t.Fatalf("QueryLogs: %v", err)
		}
		if logs != nil {
			t.Errorf("expected nil logs, got %v", logs)
		}
	})

	t.Run("given a noop store, when inserting metrics, then no error is returned", func(t *testing.T) {
		err := s.InsertMetrics(ctx, []MetricPoint{{NodeID: "n-1", CPUPercent: 50}})
		if err != nil {
			t.Fatalf("InsertMetrics: %v", err)
		}
	})

	t.Run("given a noop store, when querying metrics, then nil is returned", func(t *testing.T) {
		metrics, err := s.QueryMetrics(ctx, "n-1", tr)
		if err != nil {
			t.Fatalf("QueryMetrics: %v", err)
		}
		if metrics != nil {
			t.Errorf("expected nil metrics, got %v", metrics)
		}
	})

	t.Run("given a noop store, when inserting cost event, then no error is returned", func(t *testing.T) {
		err := s.InsertCostEvent(ctx, CostEvent{ClusterID: "c-1", HourlyCost: 1.5})
		if err != nil {
			t.Fatalf("InsertCostEvent: %v", err)
		}
	})

	t.Run("given a noop store, when querying costs, then nil is returned", func(t *testing.T) {
		costs, err := s.QueryCosts(ctx, "c-1", tr)
		if err != nil {
			t.Fatalf("QueryCosts: %v", err)
		}
		if costs != nil {
			t.Errorf("expected nil costs, got %v", costs)
		}
	})

	t.Run("given a noop store, when querying total cost, then zero is returned", func(t *testing.T) {
		total, err := s.TotalCost(ctx, "c-1", tr)
		if err != nil {
			t.Fatalf("TotalCost: %v", err)
		}
		if total != 0 {
			t.Errorf("expected 0 total cost, got %f", total)
		}
	})

	t.Run("given a noop store, when closing, then no error is returned", func(t *testing.T) {
		err := s.Close()
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
}
