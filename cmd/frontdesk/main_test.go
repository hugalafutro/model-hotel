package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// TestNewRelyingPartyEnforcesHTTPS pins the HTTPS-only ingress guarantee: a
// plain-http PUBLIC_ORIGIN is refused so a misconfigured deploy fails loudly,
// while loopback http (a secure context for WebAuthn) stays allowed for local use.
func TestNewRelyingPartyEnforcesHTTPS(t *testing.T) {
	cases := []struct {
		origin string
		ok     bool
	}{
		{"https://frontdesk.example.com", true},
		{"https://frontdesk.example.com:8443", true},
		{"http://frontdesk.example.com", false}, // plain http is rejected
		{"http://localhost:8090", true},         // loopback http allowed
		{"http://127.0.0.1:8090", true},
		{"http://[::1]:8090", true},
		{"ftp://frontdesk.example.com", false},
		{"", false},
		{"https://", false}, // no host
	}
	for _, c := range cases {
		_, err := newRelyingParty(c.origin)
		if c.ok && err != nil {
			t.Errorf("newRelyingParty(%q) = %v, want success", c.origin, err)
		}
		if !c.ok && err == nil {
			t.Errorf("newRelyingParty(%q) = nil, want an error", c.origin)
		}
	}
}

// recordingHandler captures log records so the boot-time warning can be
// asserted without parsing stdout.
type recordingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

// A short FRONTDESK_MASTER_KEY is warned about at boot, like the main server's
// MASTER_KEY; a generator-length key is silent. Warn only: the process must
// still start, since rotating the key would orphan everything encrypted.
func TestWarnWeakMasterKey(t *testing.T) {
	rec := &recordingHandler{}
	prev := slog.Default()
	debuglog.SetHandler(rec)
	t.Cleanup(func() { slog.SetDefault(prev) })

	warnWeakMasterKey("hunter2")
	if n := len(rec.records); n != 1 {
		t.Fatalf("short key: %d records, want 1 warning", n)
	}
	r := rec.records[0]
	if r.Level != slog.LevelWarn || !strings.Contains(r.Message, "FRONTDESK_MASTER_KEY is shorter than recommended") {
		t.Errorf("short key: got %v %q", r.Level, r.Message)
	}
	if strings.Contains(r.Message, "hunter2") {
		t.Error("the warning must not echo the key")
	}

	rec.records = nil
	warnWeakMasterKey(strings.Repeat("k", config.RecommendedMasterKeyLength))
	if len(rec.records) != 0 {
		t.Errorf("strong key: unexpected log %q", rec.records[0].Message)
	}
}

// fakeSessionCleaner records sweeps and can fail on demand.
type fakeSessionCleaner struct {
	mu      sync.Mutex
	calls   int
	removed int64
	err     error
}

func (f *fakeSessionCleaner) CleanupExpiredSessions(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.removed, f.err
}

func (f *fakeSessionCleaner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// TestWebauthnSessionCleanupLoop_SweepsImmediately covers the gap this exists to
// close. The gateway prunes expired WebAuthn sessions hourly; Front Desk had the
// store method and an accessor documented as being "used by callers wiring
// background cleanup of expired sessions", and nothing ever called it. Front
// Desk's OIDC login start is unauthenticated and writes a session row per
// request, so the table only ever grew.
//
// The sweep runs once at startup rather than waiting a full interval, because
// any Front Desk upgrading into this fix already carries a backlog that a
// tick-first loop would leave in place until the next hour.
func TestWebauthnSessionCleanupLoop_SweepsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &fakeSessionCleaner{removed: 3}

	done := make(chan struct{})
	go func() {
		webauthnSessionCleanupLoop(ctx, c)
		close(done)
	}()

	waitForSweeps(t, c, 1)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup loop did not return after context cancel")
	}
}

// TestWebauthnSessionCleanupLoop_SurvivesStoreError keeps one failed sweep from
// ending the loop: a transient SQLite error must not silently disable cleanup
// for the rest of the process's life, which is the failure mode that would
// recreate the unbounded growth this fixes.
//
// It waits for SEVERAL sweeps on a shortened interval. Waiting for one would
// pass against a loop that bailed out on its first error, which is exactly the
// implementation the test is supposed to reject.
func TestWebauthnSessionCleanupLoop_SurvivesStoreError(t *testing.T) {
	restore := sessionCleanupInterval
	sessionCleanupInterval = 5 * time.Millisecond
	t.Cleanup(func() { sessionCleanupInterval = restore })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := &fakeSessionCleaner{err: errors.New("database is locked")}

	done := make(chan struct{})
	go func() {
		webauthnSessionCleanupLoop(ctx, c)
		close(done)
	}()

	waitForSweeps(t, c, 3)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup loop did not return after a failing sweep")
	}
}

// TestWebauthnSessionCleanupLoop_ShutdownIsNotAFault pins the quiet half.
// Shutdown cancels the context while the loop is registered on the server's
// background group, so a sweep racing the drain gets its query cancelled. That
// is the ordinary way this loop ends and it must not be reported as a failure,
// or every clean shutdown would log an error.
func TestWebauthnSessionCleanupLoop_ShutdownIsNotAFault(t *testing.T) {
	logs := &recordingHandler{}
	prev := slog.Default()
	debuglog.SetHandler(logs)
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // shutdown has already begun when the sweep runs

	done := make(chan struct{})
	go func() {
		webauthnSessionCleanupLoop(ctx, &fakeSessionCleaner{err: context.Canceled})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("loop did not return once the context was cancelled")
	}
	logs.mu.Lock()
	defer logs.mu.Unlock()
	for _, r := range logs.records {
		if r.Level >= slog.LevelError {
			t.Errorf("clean shutdown logged an error: %q", r.Message)
		}
	}
}

func waitForSweeps(t *testing.T, c *fakeSessionCleaner, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c.count() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("cleanup ran %d times, want at least %d", c.count(), want)
}

// TestGeneratedTokenNeverReachesTheStructuredLogger is the whole point of
// printing the token to stdout directly.
//
// debuglog's handler fans out to every configured channel: the JSON stdout
// handler, where the token becomes an indexed queryable field, and the OTLP
// exporter, which ships it to whatever log store the operator runs and retains
// it there. A one-time login credential must not be persisted in log
// infrastructure. The gateway has always printed its own token straight to
// stdout for this reason; Front Desk logged it as a structured attribute.
func TestGeneratedTokenNeverReachesTheStructuredLogger(t *testing.T) {
	const token = "fd-secret-token-do-not-log"

	logs := &recordingHandler{}
	prev := slog.Default()
	debuglog.SetHandler(logs)
	t.Cleanup(func() { slog.SetDefault(prev) })

	var out strings.Builder
	announceGeneratedToken(&out, token)

	if !strings.Contains(out.String(), token) {
		t.Errorf("the token must still reach stdout so the operator can capture it; got %q", out.String())
	}

	logs.mu.Lock()
	defer logs.mu.Unlock()
	for _, r := range logs.records {
		if strings.Contains(r.Message, token) {
			t.Errorf("token leaked into a log message: %q", r.Message)
		}
		r.Attrs(func(a slog.Attr) bool {
			if strings.Contains(a.Value.String(), token) {
				t.Errorf("token leaked into log attribute %q", a.Key)
			}
			return true
		})
	}
	if len(logs.records) == 0 {
		t.Error("expected a (token-free) log line noting that a token was generated")
	}
}
