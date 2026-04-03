package server

import (
	"log/slog"
	"testing"
	"time"
)

func TestEventBus_SubscribeAndReceive(t *testing.T) {
	t.Run("given a subscriber, when an event is published, then the subscriber receives it", func(t *testing.T) {
		bus := NewEventBus(slog.Default())
		ch, unsub := bus.Subscribe()
		defer unsub()

		bus.Publish(Event{Type: "cluster_update", Data: map[string]string{"key": "some-data"}})

		select {
		case got := <-ch:
			if got.Type != "cluster_update" {
				t.Errorf("expected type cluster_update, got %s", got.Type)
			}
			if got.Data["key"] != "some-data" {
				t.Errorf("expected data key=some-data, got %v", got.Data)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for event")
		}
	})
}

func TestEventBus_UnsubscribeStopsDelivery(t *testing.T) {
	t.Run("given a subscriber that unsubscribes, when an event is published, then it is not received", func(t *testing.T) {
		bus := NewEventBus(slog.Default())
		ch, unsub := bus.Subscribe()
		unsub()

		bus.Publish(Event{Type: "test", Data: map[string]string{"msg": "should-not-arrive"}})

		select {
		case _, ok := <-ch:
			if ok {
				t.Fatal("expected channel to be closed after unsubscribe")
			}
		default:
		}
	})
}

func TestEventBus_MultipleSubscribers(t *testing.T) {
	t.Run("given multiple subscribers, when an event is published, then all subscribers receive it", func(t *testing.T) {
		bus := NewEventBus(slog.Default())

		ch1, unsub1 := bus.Subscribe()
		defer unsub1()
		ch2, unsub2 := bus.Subscribe()
		defer unsub2()
		ch3, unsub3 := bus.Subscribe()
		defer unsub3()

		bus.Publish(Event{Type: "broadcast", Data: map[string]string{"msg": "hello"}})

		for i, ch := range []chan Event{ch1, ch2, ch3} {
			select {
			case got := <-ch:
				if got.Type != "broadcast" {
					t.Errorf("subscriber %d: expected type broadcast, got %s", i, got.Type)
				}
			case <-time.After(time.Second):
				t.Fatalf("subscriber %d: timed out waiting for event", i)
			}
		}
	})
}

func TestEventBus_SlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	t.Run("given a slow subscriber with a full buffer, when publishing, then the publisher does not block", func(t *testing.T) {
		bus := NewEventBus(slog.Default())
		_, unsub := bus.Subscribe()
		defer unsub()

		done := make(chan struct{})
		go func() {
			for i := 0; i < 200; i++ {
				bus.Publish(Event{Type: "flood", Data: map[string]string{"i": "x"}})
			}
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("publisher blocked on slow subscriber")
		}
	})
}

func TestEventBus_PublishToNoSubscribersIsNoop(t *testing.T) {
	t.Run("given no subscribers, when publishing, then no panic occurs", func(t *testing.T) {
		bus := NewEventBus(slog.Default())
		bus.Publish(Event{Type: "ghost", Data: map[string]string{"msg": "nothing"}})
	})
}
