package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// hookRecorder captures WithResultHook callbacks across goroutines.
type hookRecorder struct {
	mu      sync.Mutex
	results []bool
}

func (h *hookRecorder) record(ok bool) {
	h.mu.Lock()
	h.results = append(h.results, ok)
	h.mu.Unlock()
}

func (h *hookRecorder) wait(t *testing.T, n int) []bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		got := len(h.results)
		h.mu.Unlock()
		if got >= n {
			h.mu.Lock()
			defer h.mu.Unlock()
			return append([]bool(nil), h.results...)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("expected %d hook calls", n)
	return nil
}

// TestResultHookObservesSuccess confirms the hook fires with ok=true after a
// successful POST to apprise-api.
func TestResultHookObservesSuccess(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	rec := &hookRecorder{}
	d := New(
		fakeCfg{cfg: enabledConfig(rs.URL, "tgram://tok/chat", "circuit_breaker.open")},
		rs.Client(),
		WithResultHook(rec.record),
	)

	if !d.handle(context.Background(), events.Event{Type: "circuit_breaker.open", Severity: "warning", Message: "x"}) {
		t.Fatal("expected handle to dispatch")
	}
	if got := rec.wait(t, 1); !got[0] {
		t.Errorf("hook result = false, want true")
	}
}

// TestResultHookObservesFailure confirms the hook fires with ok=false when
// apprise-api rejects the notification.
func TestResultHookObservesFailure(t *testing.T) {
	rs := newRecordingServer()
	defer rs.Close()
	rs.mu.Lock()
	rs.status = 500
	rs.mu.Unlock()

	rec := &hookRecorder{}
	d := New(
		fakeCfg{cfg: enabledConfig(rs.URL, "tgram://tok/chat", "circuit_breaker.open")},
		rs.Client(),
		WithResultHook(rec.record),
	)

	if !d.handle(context.Background(), events.Event{Type: "circuit_breaker.open", Severity: "warning", Message: "x"}) {
		t.Fatal("expected handle to dispatch")
	}
	if got := rec.wait(t, 1); got[0] {
		t.Errorf("hook result = true, want false")
	}
}
