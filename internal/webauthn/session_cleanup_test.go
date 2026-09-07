package webauthn

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// fakeSessionCleaner counts sweeps and can fail on demand.
type fakeSessionCleaner struct {
	mu      sync.Mutex
	sweeps  int
	removed int64
	err     error
}

func (f *fakeSessionCleaner) CleanupExpiredSessions(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sweeps++
	return f.removed, f.err
}

func (f *fakeSessionCleaner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.sweeps
}

// countingHandler records whether anything was logged at Error level.
type countingHandler struct {
	mu     sync.Mutex
	errors int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.Level == slog.LevelError {
		h.errors++
	}
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

func (h *countingHandler) errorCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.errors
}

// TestSessionCleanupLoop pins what the loop owes both binaries: it sweeps at
// startup rather than after a full interval (a process starting up may inherit
// a backlog), a store error is reported without ending the loop, and a
// cancellation is the shutdown path, not a fault.
func TestSessionCleanupLoop(t *testing.T) {
	tests := []struct {
		name      string
		cleaner   *fakeSessionCleaner
		wantError bool
	}{
		{"sweeps at startup", &fakeSessionCleaner{removed: 3}, false},
		{"store error is reported", &fakeSessionCleaner{err: errors.New("store gone")}, true},
		{"shutdown is not a fault", &fakeSessionCleaner{err: context.Canceled}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			logs := &countingHandler{}
			prev := slog.Default()
			slog.SetDefault(slog.New(logs))
			t.Cleanup(func() { slog.SetDefault(prev) })

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				SessionCleanupLoop(ctx, tc.cleaner, time.Hour)
				close(done)
			}()
			// The startup sweep runs before the first tick, so cancelling
			// straight away still leaves exactly one sweep behind.
			cancel()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("cleanup loop did not stop on cancellation")
			}

			if got := tc.cleaner.count(); got != 1 {
				t.Fatalf("sweeps = %d, want 1 (the startup sweep)", got)
			}
			if got := logs.errorCount() > 0; got != tc.wantError {
				t.Errorf("error logged = %v, want %v", got, tc.wantError)
			}
		})
	}
}

// TestSessionCleanupLoop_KeepsSweepingAfterAnError keeps one failed sweep from
// ending the loop: a transient store error must not silently disable cleanup
// for the rest of the process's life, which is the unbounded growth this loop
// exists to prevent. It waits for SEVERAL sweeps, since waiting for one would
// also pass against a loop that bailed out on its first error.
func TestSessionCleanupLoop_KeepsSweepingAfterAnError(t *testing.T) {
	prev := slog.Default()
	slog.SetDefault(slog.New(&countingHandler{}))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &fakeSessionCleaner{err: errors.New("database is locked")}

	done := make(chan struct{})
	go func() {
		SessionCleanupLoop(ctx, c, 5*time.Millisecond)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for c.count() < 3 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d sweeps after a failing one, want at least 3", c.count())
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup loop did not return after a failing sweep")
	}
}
