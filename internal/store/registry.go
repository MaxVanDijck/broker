package store

import "fmt"

type AnalyticsFactory func(dsn string) (AnalyticsStore, error)

var analyticsBackends = map[string]AnalyticsFactory{}

func RegisterAnalyticsBackend(name string, factory AnalyticsFactory) {
	analyticsBackends[name] = factory
}

func NewAnalyticsStore(backend, dsn string) (AnalyticsStore, error) {
	if backend == "" || backend == "noop" {
		return NewNoopAnalytics(), nil
	}

	factory, ok := analyticsBackends[backend]
	if !ok {
		return nil, fmt.Errorf("unknown analytics backend: %q (available: clickhouse, or build with -tags chdb for embedded)", backend)
	}
	return factory(dsn)
}

func init() {
	RegisterAnalyticsBackend("clickhouse", func(dsn string) (AnalyticsStore, error) {
		return NewClickHouse(dsn)
	})
	RegisterAnalyticsBackend("sqlite", func(dsn string) (AnalyticsStore, error) {
		return NewSQLiteAnalytics(dsn)
	})
}
