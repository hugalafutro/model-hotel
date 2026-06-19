package totp

import (
	"testing"
	"time"
)

func TestThrottle_AllowsUntilThreshold(t *testing.T) {
	th := NewThrottle(3, 10*time.Millisecond, time.Second)
	// First maxFailures failures must not lock (count does not yet exceed).
	for i := 0; i < 3; i++ {
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
