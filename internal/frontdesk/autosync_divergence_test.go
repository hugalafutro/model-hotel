package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Auto-sync tests for DIVERGENCE: flagging a member whose hash never matches,
// the unreadable-hash clock, the recovery edge, and the message wording.

// TestAutoSync_MemberVersionUnreadableIsNotSkipped: an unread hash proves nothing,
// so a member whose config-version endpoint errors falls through to the dry-run
// path, which reports an unreachable or erroring member properly.
func TestAutoSync_MemberVersionUnreadableIsNotSkipped(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.versionCode = http.StatusInternalServerError // its own hash cannot be read
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if got := replica.dryRunCount(); got == 0 {
		t.Error("dry-runs = 0: a member whose hash could not be read must still be evaluated")
	}
	if got := replica.realSyncCount(); got != 1 {
		t.Errorf("real imports = %d, want 1: an unreadable hash is not a converged member", got)
	}
}

// TestAutoSync_FailedVerifyStampKeepsFlag: a converged measurement whose stamp
// write fails must keep the flag, so the next converged pass retries the stamp
// instead of losing it forever.
func TestAutoSync_FailedVerifyStampKeepsFlag(t *testing.T) {
	srv, store := newTestServer(t)
	replica := newStubAutoMember(t, "rtoken")
	replica.versionHash = "hash-B"
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	srv.markUnconfirmedPush(rm.ID, "hash-B")
	// Delete the row so the in-memory member still resolves for the hash read
	// while SetMemberLastSync affects zero rows and errors (the same seam as
	// TestConfigSyncStampFailureFailsResult).
	if err := store.DeleteMember(t.Context(), rm.ID); err != nil {
		t.Fatalf("delete member: %v", err)
	}

	converged, measured, _ := srv.measureMember(t.Context(), t.Context(), rm, "rtoken", "hash-B", nil)
	if !converged || !measured {
		t.Fatalf("converged=%v measured=%v, want both true", converged, measured)
	}
	if !srv.hasUnconfirmedPush(rm.ID, "hash-B") {
		t.Error("the flag was dropped on a failed stamp write; the next converged pass has nothing left to retry")
	}
}

// TestAutoSyncStale pins the drift rule: off + a designated primary + no (or a
// stale) recorded sync is stale; an enabled loop, an absent primary, or a recent
// sync is not.
func TestAutoSyncStale(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name     string
		cfg      AutoSyncConfig
		lastSync time.Time
		haveSync bool
		want     bool
	}{
		{"enabled is never stale", AutoSyncConfig{Enabled: true, PrimaryID: "m1"}, time.Time{}, false, false},
		{"no primary is never stale", AutoSyncConfig{Enabled: false, PrimaryID: ""}, time.Time{}, false, false},
		{"off, primary, never synced", AutoSyncConfig{Enabled: false, PrimaryID: "m1"}, time.Time{}, false, true},
		{"off, primary, synced recently", AutoSyncConfig{Enabled: false, PrimaryID: "m1"}, now.Add(-time.Hour), true, false},
		{"off, primary, synced long ago", AutoSyncConfig{Enabled: false, PrimaryID: "m1"}, now.Add(-25 * time.Hour), true, true},
		{"off, primary, exactly at threshold", AutoSyncConfig{Enabled: false, PrimaryID: "m1"}, now.Add(-24 * time.Hour), true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoSyncStale(tc.cfg, tc.lastSync, tc.haveSync, now); got != tc.want {
				t.Errorf("autoSyncStale = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGetAutoSyncReportsStale: the read endpoint folds in the computed staleness
// so a device that only polls it (Bellhop's background monitor) can raise its own
// notification.
func TestGetAutoSyncReportsStale(t *testing.T) {
	srv, store := newTestServer(t)
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "tok")

	read := func() autoSyncStatus {
		t.Helper()
		rec := do(t, srv, http.MethodGet, "/api/fleet/autosync", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("get autosync = %d (%s)", rec.Code, rec.Body.String())
		}
		var got autoSyncStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return got
	}

	// Off with a designated primary and no sync ever recorded: stale.
	if err := store.SetAutoSync(t.Context(), false, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	if got := read(); !got.Stale || got.Enabled || got.PrimaryID != pm.ID {
		t.Errorf("disabled+never-synced = %+v, want stale", got)
	}

	// Enabling clears it.
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	if got := read(); got.Stale || !got.Enabled {
		t.Errorf("enabled = %+v, want not stale", got)
	}
}

// TestAutoSyncPrimaryVersionUnreadable: if the primary's version endpoint errors,
// there is nothing to converge on, so the loop propagates nothing.
func TestAutoSyncPrimaryVersionUnreadable(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionCode = http.StatusInternalServerError
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)

	if got := srv.autoSyncOnce(t.Context(), ""); got != "" {
		t.Errorf("unreadable version returned %q, want empty", got)
	}
	if replica.didRealSync() {
		t.Error("a member was written to without the primary's hash being readable")
	}
	if memberVerified(srv, rm.ID) {
		t.Error("a member was verified against a primary hash that could not be read")
	}
}

// TestAutoSyncPrimaryExportUnreadable: a primary whose export fails at the apply
// stage leaves the fleet untouched. The export is read lazily, so this is also
// the case that proves a failed read aborts the pass rather than being retried
// once per member.
func TestAutoSyncPrimaryExportUnreadable(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	primary.exportCode = http.StatusInternalServerError
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	other := newStubAutoMember(t, "otoken")
	other.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	store.CreateMember(t.Context(), "other", other.srv.URL, "otoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev") // else the version gate holds both members first

	srv.autoSyncOnce(t.Context(), "hash-B") // settled: reach the apply stage

	if replica.didBackup() || replica.didRealSync() || other.didRealSync() {
		t.Error("a member was touched despite the primary export failing")
	}
	if memberVerified(srv, rm.ID) {
		t.Error("a member was verified in sync on a pass that never read the primary's config")
	}
	if got := primary.exportCount(); got != 1 {
		t.Errorf("primary exports attempted = %d, want 1: a failed read aborts the pass rather than being retried per member", got)
	}
}

// TestAutoSyncReEnableConvergesDriftedReplica is the activation-gap fix (Greptile
// P1): a replica that drifted while sync was off is brought back in line when the
// operator re-enables auto-sync, even though the primary's config never changed.
func TestAutoSyncReEnableConvergesDriftedReplica(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	before, _ := store.GetAutoSync(t.Context())

	// Operator re-applies the setup through the API; this must re-arm the loop.
	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync", `{"enabled":true,"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("put autosync = %d (%s)", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.Gen == before.Gen {
		t.Fatalf("rearm generation = %d after re-enable, want it bumped", cfg.Gen)
	}

	// A settled pass now converges the drifted replica without the primary changing.
	alignFleetVersions(t, srv, store, "dev")
	prev := srv.autoSyncOnce(t.Context(), "")
	srv.autoSyncOnce(t.Context(), prev)
	if !replica.didRealSync() {
		t.Error("re-enabling auto-sync did not converge a replica that drifted while off")
	}
}

// TestAutoSyncStaleImportIsBenign: when a member's commit fence refuses an import
// as stale (a newer generation already won), Front Desk treats it as a benign
// supersede: the member is not stamped as converged, the applied hash is not
// recorded, and no failure event is emitted. The superseding pass is left to
// converge it.
func TestAutoSyncStaleImportIsBenign(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.staleImport = true // the member's commit fence refuses the push

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	ch := srv.bus.Subscribe()
	defer srv.bus.Unsubscribe(ch)

	srv.forceAutoSyncNow(t.Context())

	if replica.didBackup() {
		t.Error("a refused import must not have been preceded by a Front Desk snapshot")
	}
	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt != nil {
		t.Error("a stale-refused member must not have its last-sync marker stamped")
	}
	if memberVerified(srv, rm.ID) {
		t.Error("a member that refused the push as stale was recorded verified in sync")
	}
	if sawSyncFailed(ch) {
		t.Error("a benign stale fence refusal must not emit a config.sync_failed event")
	}
}

// TestAutoSync_FailedRecoveredEmitIsNotMemoisedAsClosed: when the
// config.sync_recovered insert fails, the persisted log still ends with the
// member held, so the memoised log verdict must be dropped: the next pass then
// re-reads the log, finds the hold still open, and retries the event (the same
// path TestAutoSync_ClosesHoldAcrossRestart proves emits). The in-memory hold
// stays cleared either way; the fleet state is already right.
func TestAutoSync_FailedRecoveredEmitIsNotMemoisedAsClosed(t *testing.T) {
	srv, store := newTestServer(t)
	m := &Member{ID: "m1", Name: "m1"}
	srv.syncHeldMu.Lock()
	srv.syncHeld[m.ID] = "dev@old"
	srv.holdLogChecked[m.ID] = true
	srv.syncHeldMu.Unlock()

	if err := store.db.Close(); err != nil {
		t.Fatalf("close store db: %v", err)
	}
	srv.closeSyncHold(t.Context(), m, "m1 is no longer held for sync: its app version matches the primary's again")

	srv.syncHeldMu.Lock()
	checked := srv.holdLogChecked[m.ID]
	_, held := srv.syncHeld[m.ID]
	srv.syncHeldMu.Unlock()
	if checked {
		t.Error("a failed config.sync_recovered persist stayed memoised as closed; the next pass must re-read the log and retry")
	}
	if held {
		t.Error("the in-memory hold must stay cleared even when the closing event fails to persist")
	}
}

// TestAutoSync_TimedOutPushIsRateLimitedAndFlagged: a member whose import runs
// longer than the relay's deadline answers nothing, so Front Desk cannot know
// whether it committed. It must be treated as having received the config all the
// same. Otherwise the next tick pushes again, restarting the member-side model
// discovery that made it slow in the first place, forever, and the member is
// never flagged because it never reads as having had its chance.
func TestAutoSync_TimedOutPushIsRateLimitedAndFlagged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted" // and it never adopts the primary's
		r.dryDiff = driftDiff
		r.onImport = func(reqCtx context.Context) bool {
			// Outlast the relay deadline, then abandon the import without
			// committing: the member is still working when Front Desk gives up.
			select {
			case <-reqCtx.Done():
			case <-time.After(5 * time.Second):
			}
			return false
		}
	})
	f.srv.syncClient = newProbeClient(150 * time.Millisecond)

	f.tick(t) // pushes; the member never answers in time
	if got := f.replica.realSyncCount(); got != 0 {
		t.Fatalf("real imports committed = %d, want 0: the import was abandoned mid-flight", got)
	}
	pushes := f.replica.dryRunCount()
	if pushes == 0 {
		t.Fatal("dry-runs = 0: the pass never reached the push path")
	}

	f.tick(t) // measures: the hash still differs, and the member has had its chance
	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Error("a member that timed out and still does not match was not flagged; the badge would stay green")
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d, want 1", n)
	}

	for range 3 {
		f.tick(t)
	}
	if got := f.replica.dryRunCount(); got != pushes {
		t.Errorf("push attempts = %d after three further ticks, want %d: a timed-out push must be rate-limited like any other",
			got, pushes)
	}
}

// TestAutoSync_DriftAfterConvergenceIsCorrected: a member edited directly, after
// the fleet converged and the primary settled, is the drift nothing else can
// catch. The primary's hash never moves, so only a pass that keeps measuring the
// members sees it at all. The pass measures it, pushes the primary's config back
// over it, and reports the re-sync in the event log.
//
// It is not flagged on that tick, and that is the no-flap guard doing its job: a
// member that converged has no retry timer, so it is both re-pushed at once and
// treated as one nobody has given this config yet. Drift Front Desk fixes inside a
// tick therefore raises no warning; drift it cannot fix is flagged on the next
// pass (TestAutoSync_DriftThatDoesNotCorrectIsFlagged).
func TestAutoSync_DriftAfterConvergenceIsCorrected(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-stale"
		r.appliedHash = "hash-B" // it genuinely holds the primary's config afterwards
		r.dryDiff = driftDiff
	})

	f.tick(t) // pushes
	f.tick(t) // verifies: the fleet is converged and the primary has settled
	if !f.verified() {
		t.Fatal("member not verified before the drift")
	}
	reads := f.replica.versionReadCount()

	// The operator edits a synced setting on the member itself, so its config
	// diverges from the primary's while the primary's own hash stays put.
	f.replica.setVersionHash("hash-drifted")

	f.tick(t)

	if got := f.replica.versionReadCount(); got <= reads {
		t.Fatalf("member hash reads = %d, want more than the %d before the drift: a converged fleet must keep "+
			"measuring its members or local drift is invisible", got, reads)
	}
	if got := f.replica.realSyncCount(); got != 2 {
		t.Fatalf("real imports = %d, want 2: the drifted member is given the primary's config again", got)
	}
	if n := countEventsOfType(t, f.store, "config.auto_synced"); n != 2 {
		t.Errorf("config.auto_synced events = %d, want 2: the corrective re-sync is reported too", n)
	}

	f.tick(t) // verifies the correction

	if !f.verified() {
		t.Error("member not re-verified once it held the primary's config again")
	}
	if len(f.srv.incompleteSnapshot()) != 0 {
		t.Errorf("incompleteSnapshot = %v after drift was corrected, want empty", f.srv.incompleteSnapshot())
	}
	for _, typ := range []string{"config.sync_incomplete", "config.sync_recovered"} {
		if n := countEventsOfType(t, f.store, typ); n != 0 {
			t.Errorf("%s events = %d for drift corrected within a tick, want 0", typ, n)
		}
	}
}

// TestAutoSync_DriftThatDoesNotCorrectIsFlagged: the warning is reserved for drift
// Front Desk cannot fix. A member that drifts after converging and then refuses to
// take the primary's config back is pushed on the pass that measures the drift and
// flagged on the pass after it, which is the same "it has had its chance" rule
// that governs a member which never converged in the first place.
func TestAutoSync_DriftThatDoesNotCorrectIsFlagged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-stale"
		r.appliedHash = "hash-B"
		r.dryDiff = driftDiff
	})

	f.tick(t) // pushes
	f.tick(t) // verifies: converged

	// The member drifts and stops adopting what it is given, so no push can bring
	// it back.
	f.replica.setAppliedHash("")
	f.replica.setVersionHash("hash-drifted")

	f.tick(t) // measures the drift and re-pushes; nobody is flagged yet
	if got := f.replica.realSyncCount(); got != 2 {
		t.Fatalf("real imports = %d, want 2 on the pass that measured the drift", got)
	}
	if f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Error("member flagged on the pass that first measured the drift; want the re-push to be given its chance")
	}

	f.tick(t) // it still does not hold the config: now it is a measured failure

	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Fatal("the drifted member is not flagged; the fleet badge would stay green while it serves another config")
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d, want 1 for the drifted member", n)
	}
	state, reasons, _, err := f.srv.fleetStateNow(t.Context())
	if err != nil {
		t.Fatalf("fleetStateNow: %v", err)
	}
	if state == FleetOK {
		t.Errorf("fleet state = %q %v while a member holds a different config, want degraded", state, reasons)
	}
}

// TestAutoSync_MemberClaimingSuccessWhileDivergedIsFlagged is the headline: the
// member answers the import with incomplete=false, so its self-report says the
// config applied cleanly, and its own config hash still differs from the primary's.
// That is the production incident exactly. The hash decides, so the member is
// flagged and the fleet stays unconverged; trusting the self-report would have
// recorded the fleet hash and left the divergence invisible.
func TestAutoSync_MemberClaimingSuccessWhileDivergedIsFlagged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted" // and it never adopts the primary's
		r.dryDiff = driftDiff
		r.realImportBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,"incomplete":false,"diff":{}}`
	})

	f.tick(t) // pushes; the member claims a clean apply
	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("first pass: real imports = %d, want 1", got)
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 0 {
		t.Errorf("config.sync_incomplete events = %d on the pushing pass, want 0: a member is flagged only after it has been given the config", n)
	}

	f.tick(t) // verifies: the member's hash still differs
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d, want 1: a member that claims success while diverged is still flagged", n)
	}
	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Error("incompleteSnapshot does not hold the diverged member; the fleet badge would stay green")
	}
	if f.verified() {
		t.Error("a diverged member was recorded verified; the loop must keep retrying it")
	}
}

// TestAutoSync_MemberNotYetPushedIsNeverFlagged is the no-flap guard. Every
// ordinary config edit leaves every member momentarily diverged, so a member that
// has not been given the new config yet must not turn the fleet badge amber for a
// tick. It is pushed, and judged on the pass after.
func TestAutoSync_MemberNotYetPushedIsNeverFlagged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-stale"
		r.appliedHash = "hash-B"
		r.dryDiff = driftDiff
	})

	f.tick(t)

	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("real imports = %d, want 1: the member still needs the config", got)
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 0 {
		t.Errorf("config.sync_incomplete events = %d on the very first pass, want 0", n)
	}
	if len(f.srv.incompleteSnapshot()) != 0 {
		t.Errorf("incompleteSnapshot = %v after one pass, want empty: an unpushed member is not a failed one", f.srv.incompleteSnapshot())
	}
}

// TestAutoSync_UnreadableMemberHashIsNotFlagged: a hash that could not be read
// proves nothing either way. The member is held unconverged and re-checked on the
// next pass, but it is never flagged as diverged, because Front Desk has not
// measured a divergence.
func TestAutoSync_UnreadableMemberHashIsNotFlagged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError // its own hash cannot be read
		r.dryDiff = driftDiff
	})

	f.tick(t)
	f.tick(t)

	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 0 {
		t.Errorf("config.sync_incomplete events = %d for an unreadable hash, want 0", n)
	}
	if len(f.srv.incompleteSnapshot()) != 0 {
		t.Errorf("incompleteSnapshot = %v, want empty: an unread hash is not a measured divergence", f.srv.incompleteSnapshot())
	}
	if f.verified() {
		t.Error("a member whose hash could not be read was recorded verified")
	}
	// Nor may its verify heartbeat move: an unread hash is not a confirmation, and a
	// ticking "verified in sync" beside an unmeasurable member is the false comfort
	// this criterion exists to remove.
	if snap := f.srv.poller.Snapshot(); snap[f.replicaM.ID].AutoSyncVerifiedAt != nil {
		t.Error("member whose hash could not be read was stamped verified; want the heartbeat frozen")
	}
}

// TestAutoSync_EmptyDiffWithAnUnreadableHashIsLeftAlone: the other half of the
// rule. With an empty diff AND no hash to contradict it there is no evidence in
// either direction, so importing would be a guess. The member is left for the next
// pass rather than written to.
func TestAutoSync_EmptyDiffWithAnUnreadableHashIsLeftAlone(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError // no hash to read
		// The default dryDiff is empty.
	})

	f.tick(t)

	if got := f.replica.dryRunCount(); got == 0 {
		t.Fatal("dry-runs = 0: a member whose hash could not be read must still be evaluated")
	}
	if got := f.replica.realSyncCount(); got != 0 {
		t.Errorf("real imports = %d, want 0: nothing to write and nothing saying otherwise", got)
	}
	if f.verified() {
		t.Error("an unmeasured member was recorded verified")
	}
}

// TestAutoSync_RecoveryEdgeFiresOnceWhenTheHashMatches: the transition out is
// edge-triggered on the same criterion as the transition in. A member whose
// discovery finally catches up serves the primary's hash, and that one pass clears
// the state and emits config.sync_recovered exactly once.
func TestAutoSync_RecoveryEdgeFiresOnceWhenTheHashMatches(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.dryDiff = driftDiff
	})

	f.tick(t)
	f.tick(t)
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Fatalf("config.sync_incomplete events = %d, want 1 before recovery", n)
	}
	if n := countEventsOfType(t, f.store, "config.sync_recovered"); n != 0 {
		t.Errorf("config.sync_recovered events = %d while the member is still diverged, want 0", n)
	}

	f.replica.setVersionHash("hash-B") // the member now holds the primary's config
	f.tick(t)

	if n := countEventsOfType(t, f.store, "config.sync_recovered"); n != 1 {
		t.Fatalf("config.sync_recovered events = %d, want exactly 1 on the recovery edge", n)
	}
	if len(f.srv.incompleteSnapshot()) != 0 {
		t.Errorf("incompleteSnapshot = %v after recovery, want empty", f.srv.incompleteSnapshot())
	}
	if !f.verified() {
		t.Error("member not recorded verified once it matched the primary")
	}

	f.tick(t)
	if n := countEventsOfType(t, f.store, "config.sync_recovered"); n != 1 {
		t.Errorf("config.sync_recovered events = %d on a later quiet pass, want the original 1", n)
	}
}

// TestAutoSync_ConvergedMemberNeverFlaggedEmitsNoRecovery: recovery is an edge, not
// a level. A member that converges without ever having been flagged must stay
// quiet, or every healthy sync would toast a recovery.
func TestAutoSync_ConvergedMemberNeverFlaggedEmitsNoRecovery(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-stale"
		r.appliedHash = "hash-B"
		r.dryDiff = driftDiff
	})

	f.tick(t)
	f.tick(t)

	if n := countEventsOfType(t, f.store, "config.sync_recovered"); n != 0 {
		t.Errorf("config.sync_recovered events = %d for a member that was never flagged, want 0", n)
	}
}

// TestAutoSync_FlagNamesTheGroupsTheMemberReported: the member's own report is no
// longer what decides convergence, but it is still what makes the alert specific.
// The groups it named on its last import survive to the pass that flags it, so the
// operator is told which failover groups are missing rather than only that
// something differs.
func TestAutoSync_FlagNamesTheGroupsTheMemberReported(t *testing.T) {
	f := newIncompleteFleet(t)

	f.settledTick(t)
	f.settledTick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_incomplete events = %d, want 1", len(evs))
	}
	if evs[0].MemberID != f.replicaM.ID {
		t.Errorf("event member = %q, want %q", evs[0].MemberID, f.replicaM.ID)
	}
	const wantMsg = "replica applied the config but could not build 1 failover group(s): ds4flash"
	if evs[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", evs[0].Message, wantMsg)
	}
	meta, err := json.Marshal(evs[0].Metadata)
	if err != nil {
		t.Fatalf("marshal event metadata: %v", err)
	}
	if got, want := string(meta), `{"partial":[],"unapplied":["ds4flash"],"unapplied_models":[],"unreadable":false}`; got != want {
		t.Errorf("event metadata = %s, want %s", got, want)
	}
}

// TestAutoSync_FlagSaysBothWhenGroupsAreMissingAndShort: a member can be short of
// models in two ways at once, one group below the two-entry floor and another
// merely thinner than the primary's. One alert says both.
func TestAutoSync_FlagSaysBothWhenGroupsAreMissingAndShort(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.dryDiff = driftDiff
		r.realImportBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,` +
			`"incomplete":true,"unapplied":["ds4flash"],"partial":["testgroup"],"diff":{}}`
	})

	f.tick(t)
	f.tick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_incomplete events = %d, want 1", len(evs))
	}
	const wantMsg = "replica applied the config but could not build 1 failover group(s): ds4flash, " +
		"and built testgroup with fewer entries than the primary has"
	if evs[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", evs[0].Message, wantMsg)
	}
	meta, err := json.Marshal(evs[0].Metadata)
	if err != nil {
		t.Fatalf("marshal event metadata: %v", err)
	}
	if got, want := string(meta), `{"partial":["testgroup"],"unapplied":["ds4flash"],"unapplied_models":[],"unreadable":false}`; got != want {
		t.Errorf("event metadata = %s, want %s", got, want)
	}
}

// TestAutoSync_FlagWithoutNamesReadsAsAPlainDivergence: a divergence the member
// did not explain (it named no groups, or it claimed a clean apply) has no group
// list to report, so the alert must say the member does not match rather than
// degrade into a count of zero groups.
func TestAutoSync_FlagWithoutNamesReadsAsAPlainDivergence(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.dryDiff = driftDiff
	})

	f.tick(t)
	f.tick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_incomplete events = %d, want 1", len(evs))
	}
	const wantMsg = "replica applied the config but does not match the primary's config"
	if evs[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", evs[0].Message, wantMsg)
	}
	// The names ride the metadata as a list either way, never as null, so consumers
	// see one shape.
	meta, err := json.Marshal(evs[0].Metadata)
	if err != nil {
		t.Fatalf("marshal event metadata: %v", err)
	}
	if got, want := string(meta), `{"partial":[],"unapplied":[],"unapplied_models":[],"unreadable":false}`; got != want {
		t.Errorf("event metadata = %s, want %s", got, want)
	}
}

// TestDivergenceMessageCaps locks the four alert shapes divergenceMessage renders
// (unapplied only, partial only, both, neither) and the boundary of its per-clause
// name cap: a list at the cap prints every name with no truncation marker, and a
// list one over the cap keeps the true total in the leading count while showing
// only the capped names plus a trailing "and N more". Both clauses are pushed past
// the cap together in the last case, so one clause's truncation is checked
// independently of the other's.
func TestDivergenceMessageCaps(t *testing.T) {
	if divergenceMessageMaxNames != 5 {
		t.Fatalf("divergenceMessageMaxNames = %d; the cases below are written for 5", divergenceMessageMaxNames)
	}

	atCapUnapplied := divergenceCapNames("u", divergenceMessageMaxNames)
	overCapUnapplied := divergenceCapNames("u", divergenceMessageMaxNames+1)
	atCapPartial := divergenceCapNames("p", divergenceMessageMaxNames)
	overCapPartial := divergenceCapNames("p", divergenceMessageMaxNames+1)

	cases := []struct {
		name                                string
		unapplied, partial, unappliedModels []string
		want                                string
	}{
		{
			name:      "unapplied only",
			unapplied: []string{"ds4flash"},
			want:      "replica applied the config but could not build 1 failover group(s): ds4flash",
		},
		{
			name:    "partial only",
			partial: []string{"testgroup"},
			want:    "replica applied the config but built testgroup with fewer entries than the primary has",
		},
		{
			name:      "unapplied and partial together",
			unapplied: []string{"ds4flash"},
			partial:   []string{"testgroup"},
			want: "replica applied the config but could not build 1 failover group(s): ds4flash, " +
				"and built testgroup with fewer entries than the primary has",
		},
		{
			name: "none of the three",
			want: "replica applied the config but does not match the primary's config",
		},
		{
			name:            "unapplied models only",
			unappliedModels: []string{"openai/gpt-5"},
			want: "replica applied the config but does not hold openai/gpt-5, " +
				"which the primary has switched off",
		},
		{
			name:            "unapplied models over the cap: capped names, truncation marker",
			unappliedModels: divergenceCapNames("openai/m", divergenceMessageMaxNames+1),
			want: "replica applied the config but does not hold openai/m1, openai/m2, openai/m3, " +
				"openai/m4, openai/m5, and 1 more, which the primary has switched off",
		},
		{
			name:            "all three: the clauses join in severity order",
			unapplied:       []string{"ds4flash"},
			partial:         []string{"testgroup"},
			unappliedModels: []string{"openai/gpt-5"},
			want: "replica applied the config but could not build 1 failover group(s): ds4flash, " +
				"and built testgroup with fewer entries than the primary has, " +
				"and does not hold openai/gpt-5, which the primary has switched off",
		},
		{
			name:      "unapplied exactly at the cap: every name shown, no truncation marker",
			unapplied: atCapUnapplied,
			want: "replica applied the config but could not build 5 failover group(s): " +
				"u1, u2, u3, u4, u5",
		},
		{
			name:      "unapplied one over the cap: capped names, true total in the count",
			unapplied: overCapUnapplied,
			want: "replica applied the config but could not build 6 failover group(s): " +
				"u1, u2, u3, u4, u5, and 1 more",
		},
		{
			name:    "partial exactly at the cap: every name shown, no truncation marker",
			partial: atCapPartial,
			want: "replica applied the config but built p1, p2, p3, p4, p5 " +
				"with fewer entries than the primary has",
		},
		{
			name:    "partial one over the cap: capped names, truncation marker",
			partial: overCapPartial,
			want: "replica applied the config but built p1, p2, p3, p4, p5, and 1 more " +
				"with fewer entries than the primary has",
		},
		{
			name:      "both over the cap: each clause caps independently",
			unapplied: overCapUnapplied,
			partial:   overCapPartial,
			want: "replica applied the config but could not build 6 failover group(s): " +
				"u1, u2, u3, u4, u5, and 1 more, and built p1, p2, p3, p4, p5, and 1 more " +
				"with fewer entries than the primary has",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := divergenceMessage("replica", tc.unapplied, tc.partial, tc.unappliedModels); got != tc.want {
				t.Errorf("divergenceMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAutoSyncStaleTier(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	off := AutoSyncConfig{Enabled: false, PrimaryID: "p1"}
	cases := []struct {
		name     string
		cfg      AutoSyncConfig
		lastSync time.Time
		haveSync bool
		want     int
	}{
		{"enabled is never stale", AutoSyncConfig{Enabled: true, PrimaryID: "p1"}, now.Add(-100 * time.Hour), true, 0},
		{"no primary is never stale", AutoSyncConfig{}, now.Add(-100 * time.Hour), true, 0},
		{"fresh sync", off, now.Add(-1 * time.Hour), true, 0},
		{"never synced caps at tier 1", off, time.Time{}, false, 1},
		{"just over a day", off, now.Add(-25 * time.Hour), true, 1},
		{"just under three days", off, now.Add(-71 * time.Hour), true, 1},
		{"over three days", off, now.Add(-73 * time.Hour), true, 2},
		// LastRunAt comes back from SQLite as time.Unix(0, at).UTC() (store_fleet.go),
		// so it carries no monotonic reading and a backwards step of Front Desk's own
		// clock leaves it dated ahead of now. A raw subtraction then goes negative,
		// which is under both thresholds, so a fleet that stopped syncing would report
		// tier 0 - perfectly fresh - for as long as the step lasts. That silences the
		// config.autosync_stale watchdog and the degraded/faulty fleet state at once,
		// which is the whole point of the tier.
		{"a second ahead cannot vouch for freshness", off, now.Add(time.Second), true, 1},
		{"years ahead cannot vouch for freshness", off, now.Add(9 * 365 * 24 * time.Hour), true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoSyncStaleTier(tc.cfg, tc.lastSync, tc.haveSync, now); got != tc.want {
				t.Errorf("tier = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestAutoSync_OneUnreadableHashDoesNotFlagTheMember: a single failed hash read
// proves nothing. A member restarting, or busy with an import, answers again on
// the next tick, so flagging on the first failure would turn every routine blip
// amber and alert on it.
func TestAutoSync_OneUnreadableHashDoesNotFlagTheMember(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError // its hash cannot be read
		r.dryDiff = driftDiff
	})

	f.tick(t)

	if memberDiverged(f.srv, f.replicaM.ID) {
		t.Error("a member was flagged on its first unreadable hash read; one failure proves nothing")
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 0 {
		t.Errorf("config.sync_incomplete events = %d after one failed read, want 0", n)
	}
	if unreadableSince(f.srv, f.replicaM.ID).IsZero() {
		t.Error("the unreadable clock was not started, so the member could never be flagged")
	}
}

// TestAutoSync_PersistentlyUnreadableHashFlagsTheMember: a member whose config
// hash never reads can never be shown to hold the primary's config. Before this it
// was measured in neither direction and so stayed green and silent forever, which
// is the state that hid a broken member for four hours. Past the threshold it
// carries the same badge and alert as a measured divergence, and the event says
// unknown rather than wrong.
func TestAutoSync_PersistentlyUnreadableHashFlagsTheMember(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError
		r.dryDiff = driftDiff
	})
	// The clock has been running since before the threshold: this stands in for the
	// ticks that already failed, without waiting them out.
	f.tick(t)
	seedUnreadableSince(f.srv, f.replicaM.ID, time.Now().Add(-unreadableHashThreshold-time.Minute))

	f.tick(t)

	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Fatal("a member whose hash has never read was not flagged; the fleet badge would stay green")
	}
	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_incomplete events = %d, want exactly 1 on the transition in", len(evs))
	}
	if got := evs[0].Metadata["unreadable"]; got != true {
		t.Errorf("event metadata unreadable = %v, want true: this divergence was not measured, it was unmeasurable", got)
	}
	if got, _ := evs[0].Metadata["error"].(string); got == "" {
		t.Error("event metadata carries no read failure, so the operator cannot tell an unreachable endpoint from a member too old to serve it")
	}
	// Still edge-triggered: the member is re-read every pass and must not re-alert.
	for range 3 {
		f.tick(t)
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d across further ticks, want the original 1", n)
	}
}

// TestAutoSync_UnreadableClockClearsOnASuccessfulRead: the threshold is about a
// hash that never reads, not one that reads intermittently. A member answering
// once restarts the clock from zero, so a flaky endpoint that keeps answering is
// never flagged on accumulated failures.
func TestAutoSync_UnreadableClockClearsOnASuccessfulRead(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError
		r.dryDiff = driftDiff
	})
	f.tick(t)
	seedUnreadableSince(f.srv, f.replicaM.ID, time.Now().Add(-unreadableHashThreshold-time.Minute))

	// The member answers again, with a hash that still differs: readable, so not
	// unmeasurable, whatever it says.
	f.replica.mu.Lock()
	f.replica.versionCode = http.StatusOK
	f.replica.versionHash = "hash-drifted"
	f.replica.mu.Unlock()

	f.tick(t)

	if got := unreadableSince(f.srv, f.replicaM.ID); !got.IsZero() {
		t.Errorf("unreadable clock = %v after a successful read, want zero", got)
	}
	// It may well be flagged as diverged on its own merits by now; what must not
	// happen is being reported unmeasurable while it is answering.
	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	for _, ev := range evs {
		if ev.Metadata["unreadable"] == true {
			t.Error("a member that answered was reported unmeasurable")
		}
	}
}

// TestRecordUnreadableHash: the clock starts on the first failure and only reports
// the threshold crossed once it has actually elapsed.
func TestRecordUnreadableHash(t *testing.T) {
	srv, _ := newTestServer(t)
	start := time.Now()
	const readErr = "member config-version returned 500"

	if srv.recordUnreadableHash("m1", readErr, start) {
		t.Error("the first failed read reported the threshold crossed; it only starts the clock")
	}
	if srv.recordUnreadableHash("m1", readErr, start.Add(unreadableHashThreshold-time.Second)) {
		t.Error("the threshold was reported crossed a second early")
	}
	if !srv.recordUnreadableHash("m1", readErr, start.Add(unreadableHashThreshold)) {
		t.Error("the threshold was not reported crossed once it elapsed")
	}

	srv.clearUnreadableHash("m1")
	if srv.recordUnreadableHash("m1", readErr, start.Add(2*unreadableHashThreshold)) {
		t.Error("a cleared clock did not restart from zero, so an intermittent member accumulates toward the flag")
	}
}

// TestClearUnreadableHashKeepsTheRestOfTheState: a readable hash ends only the
// unmeasurable condition. The member's retry timer and the group names it reported
// describe a measured divergence and must survive, or a member whose hash blinks
// would have its re-push rate limit reset and its alert stripped of detail.
func TestClearUnreadableHashKeepsTheRestOfTheState(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.recordSyncAttempt("m1", []string{"ds4flash"}, []string{"cheap"}, []string{"openai/gpt-5"})
	if srv.recordUnreadableHash("m1", "boom", time.Now()) {
		t.Fatal("the first failed read reported the threshold crossed")
	}

	srv.clearUnreadableHash("m1")

	srv.syncIncompleteMu.Lock()
	st := srv.syncIncomplete["m1"]
	srv.syncIncompleteMu.Unlock()
	if !st.unreadableSince.IsZero() || st.lastReadErr != "" {
		t.Error("the unreadable clock survived a successful read")
	}
	if st.lastAttempt.IsZero() {
		t.Error("the retry timer was cleared, so the member would be re-pushed every tick")
	}
	if len(st.lastUnapplied) != 1 || len(st.lastPartial) != 1 || len(st.lastUnappliedModels) != 1 {
		t.Errorf("reported names = %v/%v/%v, want all three kept for the alert",
			st.lastUnapplied, st.lastPartial, st.lastUnappliedModels)
	}
}

// TestUnmeasuredMessage: the line an operator reads when a member's config hash
// cannot be read. It has to say unknown rather than wrong, and name the read
// failure when there is one, so an unreachable endpoint reads differently from a
// member too old to serve it.
func TestUnmeasuredMessage(t *testing.T) {
	const prefix = "beta cannot be measured: Front Desk cannot read its config hash"
	withCause := unmeasuredMessage("beta", "member config-version returned 404")
	if !strings.HasPrefix(withCause, prefix) || !strings.Contains(withCause, "returned 404") {
		t.Errorf("unmeasuredMessage() = %q, want it to name the read failure", withCause)
	}
	// A read that failed with nothing to quote still has to produce a sentence,
	// not a dangling empty parenthetical.
	noCause := unmeasuredMessage("beta", "")
	if !strings.HasPrefix(noCause, prefix) {
		t.Errorf("unmeasuredMessage() = %q, want the same reading without a cause", noCause)
	}
	if strings.Contains(noCause, "()") {
		t.Errorf("unmeasuredMessage() = %q, want no empty parenthetical", noCause)
	}
	for _, msg := range []string{withCause, noCause} {
		if !strings.Contains(msg, "unknown") {
			t.Errorf("message = %q; an unmeasured member is unknown, not known-wrong", msg)
		}
	}
}

// TestAutoSync_UpButUnsyncableMemberIsStillFlagged: the case that made the "has
// had its chance" guard wrong. A member that is up and version-matched, but whose
// hash read AND import both fail, is never successfully pushed to, so such a guard
// would never let it be flagged. Every one of those failure paths only logs, so it
// would sit healthy-looking behind a green fleet holding unknown config, which is
// the exact shape of the incident this check exists to end.
func TestAutoSync_UpButUnsyncableMemberIsStillFlagged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError // cannot be measured
		r.importCode = http.StatusInternalServerError  // and cannot be written to
		r.dryDiff = driftDiff
	})
	f.tick(t)
	if f.replica.realSyncCount() != 0 {
		t.Fatal("the member accepted an import; this test needs one that accepts none")
	}
	seedUnreadableSince(f.srv, f.replicaM.ID, time.Now().Add(-unreadableHashThreshold-time.Minute))

	f.tick(t)

	if !memberDiverged(f.srv, f.replicaM.ID) {
		t.Fatal("a healthy-looking member that can be neither measured nor written to was not flagged; the fleet would read ok")
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d, want 1", n)
	}
}

// TestAutoSync_ReachableMemberWithAnUnreadableHashIsStillFlagged: the guard above
// must not reintroduce the silent hole. A member that is up and takes the config
// but cannot serve its hash has had its chance, so it is flagged as unmeasured.
func TestAutoSync_ReachableMemberWithAnUnreadableHashIsStillFlagged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError // hash unreadable
		r.dryDiff = driftDiff                          // but imports fine
	})
	f.tick(t) // pushes, so the member has had its chance
	if f.replica.realSyncCount() == 0 {
		t.Fatal("real imports = 0: the member never received the config, so the guard is not what is under test")
	}
	seedUnreadableSince(f.srv, f.replicaM.ID, time.Now().Add(-unreadableHashThreshold-time.Minute))

	f.tick(t)

	if !memberDiverged(f.srv, f.replicaM.ID) {
		t.Error("a member that took the config and cannot be measured was not flagged; that is the hole this closes")
	}
}

// TestUnreadableCauseKeepsMemberURLsOutOfAlerts: this cause is rendered into the
// config.sync_incomplete message, and the Apprise dispatcher sends that message as
// the notification body. A transport failure arrives as a *url.Error carrying the
// member's full URL, so without this a LAN hostname and port would start reaching
// the operator's notification provider as a side effect of this check.
func TestUnreadableCauseKeepsMemberURLsOutOfAlerts(t *testing.T) {
	const memberURL = "http://hotel-2.lan:8080/api/config/version"
	transport := &url.Error{
		Op:  "Get",
		URL: memberURL,
		Err: errors.New("dial tcp 10.0.0.7:8080: connect: connection refused"),
	}
	got := unreadableCause(transport)
	if strings.Contains(got, "hotel-2.lan") || strings.Contains(got, "10.0.0.7") {
		t.Errorf("cause = %q; it carries the member's address into the alert body", got)
	}
	if got == "" {
		t.Error("cause is empty; the operator is told nothing")
	}

	// A status-shaped failure names no address, so it rides through verbatim.
	const status = "member config-version returned 500"
	if unreadableCause(errors.New(status)) != status {
		t.Errorf("cause = %q, want the specific %q kept", unreadableCause(errors.New(status)), status)
	}
}

// TestUnreadableCauseSeparatesTimeoutFromRefusal: both are transport failures
// whose address is stripped, but they mean different things to an operator: a
// member that is answering slowly is not a member that is refusing connections.
func TestUnreadableCauseSeparatesTimeoutFromRefusal(t *testing.T) {
	timeout := &url.Error{Op: "Get", URL: "http://hotel-2.lan:8080/x", Err: context.DeadlineExceeded}
	if got := unreadableCause(timeout); !strings.Contains(got, "in time") {
		t.Errorf("timeout cause = %q, want it to say the member did not answer in time", got)
	}
	refused := &url.Error{Op: "Get", URL: "http://hotel-2.lan:8080/x", Err: errors.New("connection refused")}
	if got := unreadableCause(refused); strings.Contains(got, "in time") {
		t.Errorf("refusal cause = %q, want it distinguished from a timeout", got)
	}
	for _, e := range []error{timeout, refused} {
		if strings.Contains(unreadableCause(e), "hotel-2.lan") {
			t.Errorf("cause %q leaks the member address", unreadableCause(e))
		}
	}
}

// TestAutoSync_UnmeasuredUpgradesToAMeasuredDivergence: the two divergence kinds
// share a badge but not a story. A member flagged unmeasurable that later answers
// with a hash that differs is no longer unknown, it is known wrong and its groups
// can be named, so the alert has to be re-emitted. Leaving the first one standing
// would keep telling the operator the member "cannot be measured ... unknown"
// after Front Desk had measured it, which is the same over-claiming this branch
// exists to remove, pointed the other way.
func TestAutoSync_UnmeasuredUpgradesToAMeasuredDivergence(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError
		r.dryDiff = driftDiff
	})
	f.tick(t) // pushed, so it has had its chance
	seedUnreadableSince(f.srv, f.replicaM.ID, time.Now().Add(-unreadableHashThreshold-time.Minute))
	f.tick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 || evs[0].Metadata["unreadable"] != true {
		t.Fatalf("events = %d, want exactly one unmeasurable alert: %+v", len(evs), evs)
	}

	// It answers again, with a hash that still differs: measured, not unknown.
	f.replica.mu.Lock()
	f.replica.versionCode = http.StatusOK
	f.replica.versionHash = "hash-drifted"
	f.replica.mu.Unlock()

	f.tick(t)

	evs, _, err = f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("events = %d, want a second one on the unmeasurable -> measured transition", len(evs))
	}
	// ListEvents is newest-first.
	if got := evs[0].Metadata["unreadable"]; got != false {
		t.Errorf("newest event unreadable = %v, want false: the member was measured this time", got)
	}
	if strings.Contains(evs[0].Message, "cannot be measured") {
		t.Errorf("newest message = %q; it still says unknown about a member that was measured", evs[0].Message)
	}

	// And it stays edge-triggered: no third alert while nothing changes.
	for range 3 {
		f.tick(t)
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 2 {
		t.Errorf("events = %d after further ticks, want the original 2", n)
	}
}
