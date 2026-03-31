package store

import "context"

// NoopAnalyticsStore discards all writes and returns empty results.
// Used when no analytics backend is configured or available.
type NoopAnalyticsStore struct{}

func NewNoopAnalytics() *NoopAnalyticsStore { return &NoopAnalyticsStore{} }

func (s *NoopAnalyticsStore) InsertLogs(_ context.Context, _ []LogEntry) error { return nil }
func (s *NoopAnalyticsStore) QueryLogs(_ context.Context, _ string, _ TimeRange, _ int) ([]LogEntry, error) {
	return nil, nil
}
func (s *NoopAnalyticsStore) InsertMetrics(_ context.Context, _ []MetricPoint) error { return nil }
func (s *NoopAnalyticsStore) QueryMetrics(_ context.Context, _ string, _ TimeRange) ([]MetricPoint, error) {
	return nil, nil
}
func (s *NoopAnalyticsStore) QueryMetricsByCluster(_ context.Context, _ string, _ TimeRange) ([]MetricPoint, error) {
	return nil, nil
}
func (s *NoopAnalyticsStore) InsertCostEvent(_ context.Context, _ CostEvent) error { return nil }
func (s *NoopAnalyticsStore) QueryCosts(_ context.Context, _ string, _ TimeRange) ([]CostEvent, error) {
	return nil, nil
}
func (s *NoopAnalyticsStore) TotalCost(_ context.Context, _ string, _ TimeRange) (float64, error) {
	return 0, nil
}
func (s *NoopAnalyticsStore) Close() error { return nil }

var _ AnalyticsStore = (*NoopAnalyticsStore)(nil)
