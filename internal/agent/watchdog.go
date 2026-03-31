package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"broker/internal/agent/terminator"
)

type Watchdog struct {
	logger     *slog.Logger
	timeout    time.Duration
	terminator *terminator.Terminator

	mu          sync.Mutex
	lastContact time.Time
	armed       bool
}

func NewWatchdog(logger *slog.Logger, timeout time.Duration) *Watchdog {
	return &Watchdog{
		logger:      logger,
		timeout:     timeout,
		terminator:  terminator.New(logger.With("component", "terminator")),
		lastContact: time.Now(),
	}
}

// Touch resets the dead man's switch. Call this on every successful
// server interaction (register ack, submit job, etc).
func (w *Watchdog) Touch() {
	w.mu.Lock()
	w.lastContact = time.Now()
	w.mu.Unlock()
}

// Arm enables the watchdog. Called after the first successful registration.
func (w *Watchdog) Arm() {
	w.mu.Lock()
	w.armed = true
	w.lastContact = time.Now()
	w.mu.Unlock()
	w.logger.Info("watchdog armed", "timeout", w.timeout)
}

// Run monitors connectivity and terminates the node if the server is
// unreachable for longer than the configured timeout.
func (w *Watchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.check(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (w *Watchdog) check(ctx context.Context) {
	w.mu.Lock()
	armed := w.armed
	since := time.Since(w.lastContact)
	w.mu.Unlock()

	if !armed {
		return
	}

	if since < w.timeout {
		return
	}

	w.logger.Error("server unreachable, dead man's switch triggered",
		"last_contact", w.lastContact,
		"elapsed", since,
		"timeout", w.timeout,
	)

	if err := w.terminator.Terminate(ctx); err != nil {
		w.logger.Error("self-termination failed", "error", err)
	}
}
