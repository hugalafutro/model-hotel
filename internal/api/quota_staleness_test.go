package api

import (
	"testing"
	"time"
)

// TestSnapshotFreshness_FutureStampIsNeverFresh is the bug these guards exist
// for. time.Since and Sub return a NEGATIVE duration for a future timestamp,
// which satisfies every "< interval" test and fails every "> maxAge" test, so a
// single future-dated snapshot made a provider permanently fresh: its upstream
// poll was skipped forever and its stale quota kept counting as evidence.
//
// The same mistake was found and fixed once already in the fleet rate-limit
// divisor. These cases pin the repair at the two remaining sites.
func TestSnapshotFreshness_FutureStampIsNeverFresh(t *testing.T) {
	now := time.Now()

	for _, tc := range []struct {
		name       string
		fetchedAt  time.Time
		wantFresh  bool
		wantWithin bool
	}{
		{"just fetched", now, true, true},
		{"comfortably recent", now.Add(-1 * time.Minute), true, true},
		{"older than the window", now.Add(-30 * time.Minute), false, false},
		{"one second in the future", now.Add(1 * time.Second), false, false},
		{"far in the future", now.Add(48 * time.Hour), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := snapshotFresherThan(now, tc.fetchedAt, 5*time.Minute); got != tc.wantFresh {
				t.Errorf("snapshotFresherThan = %v, want %v", got, tc.wantFresh)
			}
			if got := snapshotWithinAge(now, tc.fetchedAt, 5*time.Minute); got != tc.wantWithin {
				t.Errorf("snapshotWithinAge = %v, want %v", got, tc.wantWithin)
			}
		})
	}
}

// TestSnapshotFreshness_BoundariesUnchanged keeps the guards from quietly
// shifting the windows they replaced: the dedup check was strictly-less-than and
// the evidence check kept a snapshot at exactly maxAge.
func TestSnapshotFreshness_BoundariesUnchanged(t *testing.T) {
	now := time.Now()
	const window = 5 * time.Minute
	atWindow := now.Add(-window)

	if snapshotFresherThan(now, atWindow, window) {
		t.Error("snapshotFresherThan at exactly the window = true, want false (was <, not <=)")
	}
	if !snapshotWithinAge(now, atWindow, window) {
		t.Error("snapshotWithinAge at exactly maxAge = false, want true (was >, so equal was kept)")
	}
}
