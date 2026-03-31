package store

import (
	"strings"
	"testing"
)

func TestNewAnalyticsStore(t *testing.T) {
	t.Run("given empty backend, when creating store, then noop store is returned", func(t *testing.T) {
		s, err := NewAnalyticsStore("", "")
		if err != nil {
			t.Fatalf("NewAnalyticsStore: %v", err)
		}
		if _, ok := s.(*NoopAnalyticsStore); !ok {
			t.Errorf("expected *NoopAnalyticsStore, got %T", s)
		}
	})

	t.Run("given noop backend, when creating store, then noop store is returned", func(t *testing.T) {
		s, err := NewAnalyticsStore("noop", "")
		if err != nil {
			t.Fatalf("NewAnalyticsStore: %v", err)
		}
		if _, ok := s.(*NoopAnalyticsStore); !ok {
			t.Errorf("expected *NoopAnalyticsStore, got %T", s)
		}
	})

	t.Run("given unknown backend, when creating store, then a descriptive error is returned", func(t *testing.T) {
		_, err := NewAnalyticsStore("redis", "localhost:6379")
		if err == nil {
			t.Fatal("expected error for unknown backend, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "redis") {
			t.Errorf("expected error to mention the unknown backend name 'redis', got: %s", errMsg)
		}
		if !strings.Contains(errMsg, "unknown") {
			t.Errorf("expected error to contain 'unknown', got: %s", errMsg)
		}
	})

	t.Run("given a registered mock backend, when creating store, then the factory is called", func(t *testing.T) {
		called := false
		RegisterAnalyticsBackend("mock-test", func(dsn string) (AnalyticsStore, error) {
			called = true
			if dsn != "test-dsn" {
				t.Errorf("expected dsn test-dsn, got %s", dsn)
			}
			return NewNoopAnalytics(), nil
		})

		s, err := NewAnalyticsStore("mock-test", "test-dsn")
		if err != nil {
			t.Fatalf("NewAnalyticsStore: %v", err)
		}
		if !called {
			t.Error("expected factory to be called")
		}
		if s == nil {
			t.Error("expected non-nil store")
		}
	})
}
