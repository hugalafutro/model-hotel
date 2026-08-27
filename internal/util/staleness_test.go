package util

import (
	"testing"
	"time"
)

func TestTrustedAge(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name    string
		at      time.Time
		wantAge time.Duration
		wantOK  bool
	}{
		{"past is measured as-is", now.Add(-90 * time.Second), 90 * time.Second, true},
		{"the present is zero", now, 0, true},
		{"a long-past stamp keeps its full age", now.Add(-72 * time.Hour), 72 * time.Hour, true},
		// Any future stamp is refused. There is no tolerance band: every caller
		// reads a stamp its own host wrote, with a writer that rounds downward, so
		// a future value can only mean the clock stepped back.
		{"a nanosecond ahead is untrusted", now.Add(time.Nanosecond), 0, false},
		{"a few seconds ahead is untrusted", now.Add(3 * time.Second), 0, false},
		{"far future is untrusted", now.Add(9 * 365 * 24 * time.Hour), 0, false},
		// The zero Time is not a measurement. Callers that distinguish "never
		// happened" from "happened long ago" must check IsZero themselves; this
		// only refuses to invent an age of ~2026 years for it.
		{"the zero time is untrusted", time.Time{}, 0, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			age, ok := TrustedAge(now, c.at)
			if ok != c.wantOK {
				t.Fatalf("TrustedAge ok = %v, want %v", ok, c.wantOK)
			}
			if age != c.wantAge {
				t.Errorf("TrustedAge age = %v, want %v", age, c.wantAge)
			}
		})
	}
}

// TestTrustedAgeNeverReturnsNegative is the whole point of the helper: every
// caller compares the age against a threshold, and a negative duration is below
// every positive one, so an unguarded future stamp reads as permanently fresh.
func TestTrustedAgeNeverReturnsNegative(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	for _, ahead := range []time.Duration{
		time.Nanosecond, time.Second, time.Minute, time.Hour,
		24 * time.Hour, 365 * 24 * time.Hour,
	} {
		age, ok := TrustedAge(now, now.Add(ahead))
		if age < 0 {
			t.Errorf("stamp %v ahead produced a negative age %v", ahead, age)
		}
		if ok {
			t.Errorf("stamp %v ahead reported as trusted", ahead)
		}
	}
}

// TestTrustedAgeFreshWindowIsExactlyTheCallersWindow pins what refusing every
// future stamp actually buys, stated precisely.
//
// Hold a stamp fixed and sweep now across it. A caller's "trusted and inside my
// window" verdict is false, then true, then false again: before the stamp's own
// instant it is untrusted, and after the window it has aged out. That shape is
// inherent - once the clock catches up, a stamp written before a backwards step
// is indistinguishable from one genuinely written just now - so the guard does
// NOT eliminate the interval during which a stepped clock reads as fresh.
//
// What it does is stop that interval being WIDER than the caller asked for. A
// tolerance band of T reports age zero for a stamp up to T ahead, so the true
// interval becomes [stamp-T, stamp+window): every caller's freshness window
// silently grows by T. That mattered concretely - the tolerance first written
// here was 2 minutes while the window it guarded (fleetManagedTTL) is 90
// seconds, so the widening was larger than the window itself.
//
// This test fails if any tolerance is reintroduced.
func TestTrustedAgeFreshWindowIsExactlyTheCallersWindow(t *testing.T) {
	stamp := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

	for _, window := range []time.Duration{90 * time.Second, 5 * time.Minute, 24 * time.Hour} {
		fresh := func(now time.Time) bool {
			age, ok := TrustedAge(now, stamp)
			return ok && age < window
		}

		// Sweep from well before the stamp to well past the window, at a
		// resolution fine enough to place both edges exactly.
		step := time.Second
		var first, last time.Time
		var seen bool
		for offset := -10 * time.Minute; offset <= window+10*time.Minute; offset += step {
			at := stamp.Add(offset)
			if !fresh(at) {
				continue
			}
			if !seen {
				first, seen = at, true
			}
			last = at
		}
		if !seen {
			t.Fatalf("window %v: never fresh", window)
		}
		// The first fresh instant must be the stamp itself, not earlier: an
		// earlier one is a tolerance band admitting future stamps.
		if !first.Equal(stamp) {
			t.Errorf("window %v: first fresh at %v, want the stamp itself %v (a future stamp was accepted)",
				window, first, stamp)
		}
		if got := last.Sub(first) + step; got != window {
			t.Errorf("window %v: fresh interval spans %v, want exactly the window", window, got)
		}
	}
}

// TestMonotonicReadingIsWhatExemptsInProcessChecks documents why the Traefik
// staleness checks in internal/frontdesk/poller_versions.go are deliberately
// NOT converted to TrustedAge, and pins the stdlib property they rest on. The
// companion test that the poller's own fields really do keep that reading lives
// in internal/frontdesk (TestTraefikStalenessInputsKeepMonotonicReadings) -
// this one only establishes the rule.
func TestMonotonicReadingIsWhatExemptsInProcessChecks(t *testing.T) {
	captured := time.Now()

	// Round(0) strips the monotonic reading and is the documented way to ask
	// whether one is present. The comparison is struct identity on purpose:
	// Equal compares wall clocks, which agree by construction here, so it would
	// be true whether or not a monotonic reading exists - the very distinction
	// under test.
	if captured == captured.Round(0) { //nolint:staticcheck // QF1009: identity, not temporal equality
		t.Fatal("time.Now() carried no monotonic reading; the exemption does not hold")
	}
	if !captured.Equal(captured.Round(0)) {
		t.Error("Round(0) must preserve the wall clock, only drop the monotonic reading")
	}

	// The trap: the conversions that look purely cosmetic drop it. A site that
	// stores time.Now().UTC() has already lost the protection, which is why the
	// exemption is stated per-site rather than "anything from time.Now()".
	for name, stripped := range map[string]time.Time{
		"UTC":      captured.UTC(),
		"Round":    captured.Round(time.Second),
		"Truncate": captured.Truncate(time.Second),
	} {
		if stripped != stripped.Round(0) {
			t.Errorf("%s() unexpectedly kept the monotonic reading", name)
		}
	}

	// In-process durations stay ordered: the poller's situation, where both
	// operands come from time.Now() in the same process.
	if got := time.Since(captured); got < 0 {
		t.Errorf("in-process Sub went negative (%v)", got)
	}

	// Once the reading is gone, ordering is whatever the wall clock says, and a
	// stamp that ended up ahead of "now" is exactly the swept bug.
	wallNow := captured.Round(0)
	if age, ok := TrustedAge(wallNow, wallNow.Add(time.Second)); ok || age != 0 {
		t.Errorf("TrustedAge = (%v, %v), want (0, false)", age, ok)
	}
}
