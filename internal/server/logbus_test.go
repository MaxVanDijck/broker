package server

import (
	"sync"
	"testing"
	"time"

	"broker/internal/store"
)

func TestLogBus_PublishAndReceive(t *testing.T) {
	// Given a logbus with one subscriber for job "j-1"
	bus := NewLogBus()
	ch := bus.Subscribe("j-1")

	entry := store.LogEntry{
		Timestamp: time.Now(),
		JobID:     "j-1",
		Stream:    "stdout",
		Line:      []byte("hello world"),
	}

	// When a log entry is published
	bus.Publish("j-1", []store.LogEntry{entry})

	// Then the subscriber receives it
	select {
	case got := <-ch:
		if string(got.Line) != "hello world" {
			t.Fatalf("expected 'hello world', got %q", string(got.Line))
		}
		if got.JobID != "j-1" {
			t.Fatalf("expected job id 'j-1', got %q", got.JobID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for log entry")
	}

	bus.Unsubscribe("j-1", ch)
}

func TestLogBus_UnsubscribeStopsDelivery(t *testing.T) {
	// Given a logbus with one subscriber for job "j-2"
	bus := NewLogBus()
	ch := bus.Subscribe("j-2")

	// When the subscriber unsubscribes
	bus.Unsubscribe("j-2", ch)

	entry := store.LogEntry{
		Timestamp: time.Now(),
		JobID:     "j-2",
		Stream:    "stdout",
		Line:      []byte("should not arrive"),
	}

	// Then publishing does not deliver to the closed channel
	bus.Publish("j-2", []store.LogEntry{entry})

	// The channel should be closed and drained
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after unsubscribe")
		}
	default:
		// channel is closed and empty, this is expected
	}
}

func TestLogBus_MultipleSubscribers(t *testing.T) {
	// Given a logbus with three subscribers for job "j-3"
	bus := NewLogBus()
	ch1 := bus.Subscribe("j-3")
	ch2 := bus.Subscribe("j-3")
	ch3 := bus.Subscribe("j-3")

	entry := store.LogEntry{
		Timestamp: time.Now(),
		JobID:     "j-3",
		Stream:    "stderr",
		Line:      []byte("multi"),
	}

	// When a log entry is published
	bus.Publish("j-3", []store.LogEntry{entry})

	// Then all three subscribers receive it
	for i, ch := range []chan store.LogEntry{ch1, ch2, ch3} {
		select {
		case got := <-ch:
			if string(got.Line) != "multi" {
				t.Fatalf("subscriber %d: expected 'multi', got %q", i, string(got.Line))
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for log entry", i)
		}
	}

	bus.Unsubscribe("j-3", ch1)
	bus.Unsubscribe("j-3", ch2)
	bus.Unsubscribe("j-3", ch3)
}

func TestLogBus_PublishToUnknownJobIsNoop(t *testing.T) {
	// Given a logbus with no subscribers
	bus := NewLogBus()

	entry := store.LogEntry{
		Timestamp: time.Now(),
		JobID:     "j-unknown",
		Stream:    "stdout",
		Line:      []byte("no one listening"),
	}

	// When publishing to a job with no subscribers, then no panic occurs
	bus.Publish("j-unknown", []store.LogEntry{entry})
}

func TestLogBus_ConcurrentPublishSubscribe(t *testing.T) {
	// Given a logbus
	bus := NewLogBus()

	var wg sync.WaitGroup
	const numGoroutines = 10

	// When multiple goroutines subscribe, publish, and unsubscribe concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := bus.Subscribe("j-concurrent")
			defer bus.Unsubscribe("j-concurrent", ch)

			bus.Publish("j-concurrent", []store.LogEntry{{
				Timestamp: time.Now(),
				JobID:     "j-concurrent",
				Stream:    "stdout",
				Line:      []byte("concurrent"),
			}})

			// Drain any received messages
			for {
				select {
				case <-ch:
				default:
					return
				}
			}
		}()
	}

	// Then no race conditions or panics occur
	wg.Wait()
}
