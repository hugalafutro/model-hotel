package frontdesk

import (
	"net/http"
	"strings"
	"testing"
)

// The push-failure record is in-memory. These tests cover the event-log fallback
// that survives a Front Desk restart, mirroring the hold tests in
// autosync_hold_test.go. The restart is simulated by resetting the in-memory maps
// while the store keeps its events.

func resetFailureState(f *hashFleet) {
	f.srv.syncIncompleteMu.Lock()
	f.srv.lastSyncFailure = make(map[string]string)
	f.srv.failureLogChecked = make(map[string]bool)
	f.srv.syncIncompleteMu.Unlock()
}

func refuseThenRestart(t *testing.T) *hashFleet {
	t.Helper()
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.dryDiff = driftDiff
		r.realImportCode = http.StatusBadRequest
	})
	f.tick(t)
	if n := countEvents(t, f.store, "config.sync_failed"); n != 1 {
		t.Fatalf("config.sync_failed after the refusal = %d, want 1", n)
	}
	resetFailureState(f)
	return f
}

func recoveredEvents(t *testing.T, f *hashFleet) []Event {
	t.Helper()
	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_recovered"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return evs
}

// TestAutoSync_MeasuredConvergenceRecoversLoggedFailureAcrossRestart: the other
// clearing site. The member is found holding the primary's config before any
// push, so the recovery comes from the measurement, not from a push that
// succeeded.
func TestAutoSync_MeasuredConvergenceRecoversLoggedFailureAcrossRestart(t *testing.T) {
	f := refuseThenRestart(t)
	f.replica.mu.Lock()
	f.replica.versionHash = "hash-B"
	f.replica.mu.Unlock()
	f.tick(t)
	evs := recoveredEvents(t, f)
	if len(evs) != 1 || evs[0].MemberID != f.replicaM.ID || !strings.Contains(evs[0].Message, "holds the primary's config again") {
		t.Fatalf("config.sync_recovered = %+v, want one from the converged measurement", evs)
	}
	f.tick(t)
	if n := len(recoveredEvents(t, f)); n != 1 {
		t.Errorf("config.sync_recovered after another converged pass = %d, want still 1", n)
	}
}

// TestAutoSync_StillFailingAfterRestartDoesNotRealert: a member refusing for the
// same cause across a restart is not news; config.sync_failed stays at one.
func TestAutoSync_StillFailingAfterRestartDoesNotRealert(t *testing.T) {
	f := refuseThenRestart(t)
	f.tick(t)
	if n := countEvents(t, f.store, "config.sync_failed"); n != 1 {
		t.Fatalf("config.sync_failed after the restart's first refused pass = %d, want still 1", n)
	}
	f.replica.mu.Lock()
	f.replica.realImportCode = 0
	f.replica.appliedHash = "hash-B"
	f.replica.mu.Unlock()
	f.tick(t)
	if n := len(recoveredEvents(t, f)); n != 1 {
		t.Errorf("config.sync_recovered once the push succeeds = %d, want 1", n)
	}
}

// TestAutoSync_UnconfirmedFailureIsNotRecoveredAcrossRestart: a timed-out push is
// logged as config.sync_failed at info with unconfirmed=true and is never
// remembered as a failure, so the log fallback must not turn it into one.
func TestAutoSync_UnconfirmedFailureIsNotRecoveredAcrossRestart(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) { r.versionHash = "hash-B" })
	if _, err := f.store.InsertEvent(t.Context(), Event{
		Type: "config.sync_failed", Severity: "info", Source: "frontdesk",
		Message: "replica did not answer the config push in time", MemberID: f.replicaM.ID,
		Metadata: map[string]any{"error": "timed out", "timed_out": true, "unconfirmed": true},
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	f.tick(t)
	if n := len(recoveredEvents(t, f)); n != 0 {
		t.Errorf("config.sync_recovered after an unconfirmed push = %d, want 0", n)
	}
}

// TestAutoSync_FailureLogReadErrorIsNotMemoised: a store error reads as "no open
// failure" for that pass but gives the claim back, so the next pass retries.
func TestAutoSync_FailureLogReadErrorIsNotMemoised(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.db.Close(); err != nil {
		t.Fatalf("close store db: %v", err)
	}
	m := &Member{ID: "m1", Name: "m1"}
	if srv.failedPerLog(t.Context(), m) {
		t.Error("failedPerLog = true on a store read error, want false")
	}
	srv.syncIncompleteMu.Lock()
	checked := srv.failureLogChecked[m.ID]
	srv.syncIncompleteMu.Unlock()
	if checked {
		t.Error("a failed log read was memoised as checked; it must retry on the next pass")
	}
}

// TestAutoSync_PromotionClosesLoggedFailure: a member promoted to primary with an
// open config.sync_failed in the log gets it closed, so a later demotion and
// restart cannot read it back as a failure to recover from.
func TestAutoSync_PromotionClosesLoggedFailure(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) { r.versionHash = "hash-B" })
	if _, err := f.store.InsertEvent(t.Context(), Event{
		Type: "config.sync_failed", Severity: "warning", Source: "frontdesk",
		Message: "Failed to sync config to primary: could not reach this member", MemberID: f.primaryM.ID,
		Metadata: map[string]any{"error": "could not reach this member", "timed_out": false, "unconfirmed": false},
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	f.tick(t)
	evs := recoveredEvents(t, f)
	if len(evs) != 1 || evs[0].MemberID != f.primaryM.ID || !strings.Contains(evs[0].Message, "now the primary") {
		t.Fatalf("config.sync_recovered for the promoted member = %+v, want exactly one", evs)
	}
}
