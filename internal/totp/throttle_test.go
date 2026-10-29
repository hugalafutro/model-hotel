package totp

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/logtest"
)

// Admit lets a key under the limit through without touching the response, and
// answers a key in backoff itself: the caller's warning logged with its attrs,
// a 429 carrying the remaining lock as Retry-After.
func TestThrottle_AdmitRefusesALockedKeyWith429(t *testing.T) {
	logs := logtest.Capture(t)
	th := NewThrottle(1, time.Minute, time.Hour)
	th.RecordFailure("ip")

	rr := httptest.NewRecorder()
	if !th.Admit(rr, "ip", "totp: throttled", "ip", "10.0.0.1") {
		t.Fatal("Admit refused a key still under the limit")
	}
	if rr.Code != http.StatusOK || rr.Body.Len() != 0 || rr.Header().Get("Retry-After") != "" {
		t.Errorf("an admitted request must write nothing: status %d, body %q, Retry-After %q", rr.Code, rr.Body.String(), rr.Header().Get("Retry-After"))
	}
	if logs.Has("totp: throttled") {
		t.Error("an admitted request logged the throttle warning")
	}

	th.RecordFailure("ip") // exceeds the limit: locked for a minute
	rr = httptest.NewRecorder()
	if th.Admit(rr, "ip", "totp: throttled", "ip", "10.0.0.1") {
		t.Fatal("Admit let a locked key through")
	}
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rr.Code)
	}
	if secs, err := strconv.Atoi(rr.Header().Get("Retry-After")); err != nil || secs < 59 || secs > 60 {
		t.Errorf("Retry-After = %q, want the ~60s left on the lock", rr.Header().Get("Retry-After"))
	}
	rec, ok := logs.Find("totp: throttled")
	if !ok {
		t.Fatal("refusal did not log the caller's message")
	}
	if rec.Level != slog.LevelWarn || rec.Attrs["ip"] != "10.0.0.1" {
		t.Errorf("log = %+v, want Warn with ip=10.0.0.1", rec)
	}
	// Another key is unaffected.
	if !th.Admit(httptest.NewRecorder(), "other", "totp: throttled") {
		t.Error("Admit refused an unrelated key")
	}
}

// Past maxThrottleEntries a failure sweeps the map before recording itself:
// stale unlocked entries go, locked ones and the new failure stay.
func TestThrottle_RecordFailureSweepsWhenTheMapIsFull(t *testing.T) {
	// A long base delay: the lock the fresh failure takes must still hold when
	// it is checked below, however slowly the race detector runs this.
	th := NewThrottle(0, time.Minute, time.Hour)
	old := time.Now().Add(-time.Hour)
	for i := range maxThrottleEntries {
		th.entries["stale"+strconv.Itoa(i)] = &throttleEntry{lockedUntil: old, lastSeen: old}
	}
	th.entries["locked"] = &throttleEntry{lockedUntil: time.Now().Add(time.Hour), lastSeen: time.Now()}

	th.RecordFailure("fresh")

	if len(th.entries) != 2 {
		t.Fatalf("entries = %d, want the locked one and the fresh failure", len(th.entries))
	}
	if _, ok := th.entries["locked"]; !ok {
		t.Error("a locked entry was swept")
	}
	if ok, _ := th.Allowed("fresh"); ok {
		t.Error("the failure that triggered the sweep was not recorded")
	}
}

func TestThrottle_AllowsUntilThreshold(t *testing.T) {
	th := NewThrottle(3, 10*time.Millisecond, time.Second)
	// First maxFailures failures must not lock (count does not yet exceed).
	for i := range 3 {
		th.RecordFailure("ip")
		if ok, _ := th.Allowed("ip"); !ok {
			t.Fatalf("locked after %d failures, expected still allowed", i+1)
		}
	}
	// The next failure exceeds the threshold and must lock.
	th.RecordFailure("ip")
	if ok, retry := th.Allowed("ip"); ok || retry <= 0 {
		t.Fatalf("expected lock with positive retry after exceeding threshold, got ok=%v retry=%v", ok, retry)
	}
}

func TestThrottle_LockExpires(t *testing.T) {
	th := NewThrottle(0, 10*time.Millisecond, time.Second)
	th.RecordFailure("ip") // 1 > 0 -> locked for baseDelay
	if ok, _ := th.Allowed("ip"); ok {
		t.Fatal("expected immediate lock")
	}
	time.Sleep(20 * time.Millisecond)
	if ok, _ := th.Allowed("ip"); !ok {
		t.Fatal("expected lock to expire after baseDelay")
	}
}

func TestThrottle_SuccessResets(t *testing.T) {
	th := NewThrottle(0, time.Hour, time.Hour)
	th.RecordFailure("ip")
	if ok, _ := th.Allowed("ip"); ok {
		t.Fatal("expected lock")
	}
	th.RecordSuccess("ip")
	if ok, retry := th.Allowed("ip"); !ok || retry != 0 {
		t.Fatalf("expected reset after success, got ok=%v retry=%v", ok, retry)
	}
}

func TestThrottle_BackoffGrowsAndCaps(t *testing.T) {
	th := NewThrottle(0, time.Second, 4*time.Second)
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 4 * time.Second}, // capped
		{99, 4 * time.Second},
	}
	for _, c := range cases {
		if got := th.backoffFor(c.failures); got != c.want {
			t.Errorf("backoffFor(%d) = %v, want %v", c.failures, got, c.want)
		}
	}
}

func TestThrottle_KeysAreIndependent(t *testing.T) {
	th := NewThrottle(0, time.Hour, time.Hour)
	th.RecordFailure("attacker")
	if ok, _ := th.Allowed("attacker"); ok {
		t.Fatal("expected attacker key locked")
	}
	if ok, _ := th.Allowed("admin"); !ok {
		t.Fatal("admin key must not be affected by attacker's failures")
	}
}

func TestThrottle_SweepEvictsStale(t *testing.T) {
	th := NewThrottle(0, time.Millisecond, 10*time.Millisecond)
	now := time.Now()
	// Unlocked and last seen well beyond maxDelay -> swept.
	th.entries["stale"] = &throttleEntry{lockedUntil: now.Add(-time.Hour), lastSeen: now.Add(-time.Hour)}
	// Still locked -> kept.
	th.entries["locked"] = &throttleEntry{lockedUntil: now.Add(time.Hour), lastSeen: now}
	th.sweepLocked(now)
	if _, ok := th.entries["stale"]; ok {
		t.Error("expected stale entry to be swept")
	}
	if _, ok := th.entries["locked"]; !ok {
		t.Error("expected locked entry to be kept")
	}
}
