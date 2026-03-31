package server

import (
	"sync"

	"broker/internal/store"
)

// LogBus is a simple in-memory pub/sub for streaming log lines to active
// subscribers. Each subscriber receives log entries on a buffered channel
// keyed by job ID.
type LogBus struct {
	mu   sync.RWMutex
	subs map[string]map[chan store.LogEntry]struct{}
}

func NewLogBus() *LogBus {
	return &LogBus{
		subs: make(map[string]map[chan store.LogEntry]struct{}),
	}
}

// Subscribe returns a channel that will receive log entries for the given job.
// The caller must call Unsubscribe when done to avoid leaking the channel.
func (b *LogBus) Subscribe(jobID string) chan store.LogEntry {
	ch := make(chan store.LogEntry, 128)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.subs[jobID] == nil {
		b.subs[jobID] = make(map[chan store.LogEntry]struct{})
	}
	b.subs[jobID][ch] = struct{}{}
	return ch
}

// Unsubscribe removes a subscriber channel and closes it.
func (b *LogBus) Unsubscribe(jobID string, ch chan store.LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if listeners, ok := b.subs[jobID]; ok {
		delete(listeners, ch)
		if len(listeners) == 0 {
			delete(b.subs, jobID)
		}
	}
	close(ch)
}

// Publish sends log entries to all active subscribers for the given job.
// Slow consumers that have a full buffer will have messages dropped.
func (b *LogBus) Publish(jobID string, entries []store.LogEntry) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	listeners := b.subs[jobID]
	if len(listeners) == 0 {
		return
	}
	for _, entry := range entries {
		for ch := range listeners {
			select {
			case ch <- entry:
			default:
				// drop if subscriber is too slow
			}
		}
	}
}
