package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/quota"
)

// TestDriftEligible_FutureStampIsNotEligible pins the negative-age guard at its
// call site: time.Sub returns a NEGATIVE duration for a future timestamp, which
// fails every "> maxAge" test, so a single future-dated snapshot would count as
// evidence forever. A snapshot at exactly maxAge is kept, as it always was.
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

	atMaxAge := ok
	atMaxAge.FetchedAt = now.Add(-maxAge)
	if !driftEligible(atMaxAge, maxAge, now) {
		t.Error("a snapshot at exactly maxAge should still be eligible (the bound is <=, not <)")
	}
}
