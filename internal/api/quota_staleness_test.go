package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/quota"
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

// TestDriftEligible_FutureStampIsNotEligible pins the guard at its call site
// rather than only on the helper. driftEligible was the third site with this
// bug and was missed by the first pass at the fix, so a helper-only test would
// have gone on passing while the site itself stayed broken.
func TestDriftEligible_FutureStampIsNotEligible(t *testing.T) {
	now := time.Now()
	const maxAge = 15 * time.Minute
	ok := quota.Snapshot{HTTPStatus: http.StatusOK, Payload: []byte(`{"a":1}`)}

	fresh := ok
	fresh.FetchedAt = now.Add(-1 * time.Minute)
	if !driftEligible(fresh, maxAge, now) {
		t.Error("a recent snapshot should be drift-eligible")
	}

	stale := ok
	stale.FetchedAt = now.Add(-1 * time.Hour)
	if driftEligible(stale, maxAge, now) {
		t.Error("an old snapshot should not be drift-eligible")
	}

	future := ok
	future.FetchedAt = now.Add(48 * time.Hour)
	if driftEligible(future, maxAge, now) {
		t.Error("a future-dated snapshot is drift-eligible forever; the negative-age guard is missing at this site")
	}
}
