package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// attrCaptureHandler keeps each record as "msg k=v k=v" so a test can read an
// attribute value off a line, which msgCaptureHandler (message only) cannot.
type attrCaptureHandler struct {
	mu    sync.Mutex
	lines []string
}

func (c *attrCaptureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (c *attrCaptureHandler) Handle(_ context.Context, r slog.Record) error {
	line := r.Message
	r.Attrs(func(a slog.Attr) bool {
		line += fmt.Sprintf(" %s=%v", a.Key, a.Value.Any())
		return true
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, line)
	return nil
}

func (c *attrCaptureHandler) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *attrCaptureHandler) WithGroup(string) slog.Handler      { return c }

func (c *attrCaptureHandler) find(msg string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, l := range c.lines {
		if strings.HasPrefix(l, msg) {
			return l
		}
	}
	return ""
}

// A bucket stamps lastUsed on admission, before the refusal is noted, so an
// identity refused once and then silent reaches eviction with a lastUsed older
// than its throttledAt. The summary must end the episode at the last refusal,
// never report a negative duration.
func TestEndIfThrottled_LastRefusalWinsOverOlderAdmission(t *testing.T) {
	h := &attrCaptureHandler{}
	debuglog.SetHandler(h)
	t.Cleanup(func() { debuglog.Init() })

	s := &throttleState{}
	c := throttleLogCtx{prefix: "ratelimit", label: "key", id: "k"}
	admitted := time.Now()
	s.noteRejected(c) // throttledAt = lastRejectedAt = now, after admitted

	s.endIfThrottled(c, admitted, "idle")

	line := h.find("ratelimit: throttling ended")
	if line == "" {
		t.Fatal("expected one 'throttling ended' line")
	}
	if strings.Contains(line, "duration=-") {
		t.Errorf("episode summary reports a negative duration: %s", line)
	}
}

// Ending an episode closes it: a request still holding the evicted entry must
// not log the same episode ended a second time on its next admission.
func TestEndIfThrottled_ClosesTheEpisode(t *testing.T) {
	h := &msgCaptureHandler{}
	debuglog.SetHandler(h)
	t.Cleanup(func() { debuglog.Init() })

	s := &throttleState{}
	c := throttleLogCtx{prefix: "ratelimit", label: "key", id: "k"}
	s.noteRejected(c)
	s.endIfThrottled(c, time.Now(), "idle")
	s.endIfThrottled(c, time.Now(), "idle")
	s.noteAllowed(c)

	if got := h.count("ratelimit: throttling ended"); got != 1 {
		t.Errorf("expected exactly one 'throttling ended', got %d", got)
	}
	if s.throttled.Load() {
		t.Error("episode must be closed after endIfThrottled")
	}
}

// A cap change publishes a fresh entry around the same bucket and throttle
// state, so the lock-free readers of tpm (headers, log line) never race the
// write, and the budget the bucket holds carries over instead of refilling.
func TestTPMEntry_WithCapKeepsBucketAndThrottle(t *testing.T) {
	e := newTPMEntry(60)
	e.limiter.ReserveN(time.Now(), 60) // spend the whole minute
	e.throttle.noteRejected(e.throttleCtx("key", "k"))

	n := e.withCap(120)

	if n == e {
		t.Fatal("withCap must publish a new entry, not mutate the old one")
	}
	if n.limiter != e.limiter || n.throttle != e.throttle {
		t.Error("withCap must keep the same bucket and throttle state")
	}
	if n.tpm != 120 || e.tpm != 60 {
		t.Errorf("tpm: new=%d old=%d, want 120 and 60", n.tpm, e.tpm)
	}
	if n.limiter.Burst() != 120 || n.limiter.Limit() != tpmRate(120) {
		t.Errorf("bucket not re-rated: burst=%d limit=%v", n.limiter.Burst(), n.limiter.Limit())
	}
	if n.limiter.Tokens() > 1 { // refill between the reserve and this read is microtokens
		t.Errorf("a cap change must not refill a spent budget, got %v tokens", n.limiter.Tokens())
	}
	if !n.throttle.throttled.Load() {
		t.Error("open episode must survive the cap change")
	}
}

// Under -race: a cap change racing the 429 path's unlocked reads of tpm.
func TestTPMLimiter_CapChangeDoesNotRaceRejection(t *testing.T) {
	l, _ := newTestTPMLimiter(t)
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			l.getEntry(t.Context(), "k", 100+i)
		}()
		go func() {
			defer wg.Done()
			e := l.getEntry(t.Context(), "k", 100)
			_ = e.headers()
			_ = e.throttleCtx("key", "k")
		}()
	}
	wg.Wait()
}
