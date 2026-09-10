package totp

import (
	"strings"
	"testing"
	"time"
)

// TestThrottle_KeysAreBounded pins what keeps an unauthenticated flood from
// holding the heap: callers key on values that arrive from the network, and the
// map holds an entry until a later failure sweeps it, so the stored key has to
// cost the same whatever was passed.
func TestThrottle_KeysAreBounded(t *testing.T) {
	th := NewThrottle(1, time.Second, time.Minute)
	huge := "user:" + strings.Repeat("a", 1<<20)

	th.RecordFailure(huge)
	th.RecordFailure(huge)

	th.mu.Lock()
	defer th.mu.Unlock()
	if len(th.entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(th.entries))
	}
	for k := range th.entries {
		if len(k) != 32 {
			t.Errorf("stored key is %d bytes, want the 32 byte digest", len(k))
		}
	}
}

// The digest must not cost accuracy: a key still throttles itself and leaves
// every other key alone.
func TestThrottle_HashedKeysStayDistinct(t *testing.T) {
	th := NewThrottle(1, time.Minute, time.Minute)
	th.RecordFailure("a")
	th.RecordFailure("a")

	if ok, _ := th.Allowed("a"); ok {
		t.Error("the failing key should be locked out")
	}
	if ok, _ := th.Allowed("b"); !ok {
		t.Error("a different key must be unaffected")
	}
	th.RecordSuccess("a")
	if ok, _ := th.Allowed("a"); !ok {
		t.Error("success should clear the key's backoff")
	}
}
