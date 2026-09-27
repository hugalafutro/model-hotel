package frontdesk

import (
	"net/http"
	"strings"
	"testing"
)

// Auto-sync tests for the sync HOLD: a member whose schema or build is skewed
// is held out of the pass and released when it catches up.

// TestAutoSync_MemberHoldingThisConfigIsSkipped: every member serves the same
// content hash of its syncable config, so a member reporting the primary's hash
// already holds exactly this config and is skipped outright, without even the
// dry-run. The dry-run cannot establish that (it keys on presence, so a matching
// member still reports every shared entity as updated), which is why the member's
// own hash is read first. A member that matches is genuinely converged, so the
// skip records it verified rather than leaving it unmeasured.
func TestAutoSync_MemberHoldingThisConfigIsSkipped(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.versionHash = "hash-B" // this member already holds the primary's config
	replica.dryDiff = driftDiff    // a presence-based diff would claim it needs syncing

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if got := replica.dryRunCount(); got != 0 {
		t.Errorf("dry-runs = %d, want 0: a member holding this config is skipped before the diff", got)
	}
	if got := replica.realSyncCount(); got != 0 {
		t.Errorf("real imports = %d, want 0: the member already holds this config", got)
	}
	if !memberVerified(srv, rm.ID) {
		t.Error("skipping a matching member left it unmeasured; it holds this config and must read as verified")
	}
	// Nothing was written, so the persisted stamp stays put; only the live
	// "verified in sync" heartbeat advances.
	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt != nil {
		t.Error("a skipped member had last_config_sync_at stamped; want untouched (no write happened)")
	}
	if snap := srv.poller.Snapshot(); snap[rm.ID].AutoSyncVerifiedAt == nil {
		t.Error("skipped member AutoSyncVerifiedAt = nil, want the verify heartbeat stamped")
	}
}

// TestAutoSyncHoldsVersionSkew: a member whose polled app version differs from
// the primary's is held (no push, fleet not converged), and
// config.sync_held is emitted once on the transition into held rather than on
// every pass. Once the versions align, the next pass syncs the member normally.
func TestAutoSyncHoldsVersionSkew(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // this member needs the new config

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")

	// Two held passes: the member is never pushed to, and the second pass must
	// not re-alert (edge-triggered hold).
	srv.forceAutoSyncNow(t.Context())
	srv.forceAutoSyncNow(t.Context())

	if replica.didBackup() || replica.didRealSync() {
		t.Fatal("version-skewed member was pushed to; want held")
	}
	if memberVerified(srv, rm.ID) {
		t.Error("a version-skewed member was recorded verified in sync; it was never measured")
	}
	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: "config.sync_held"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_held events = %d, want exactly 1 across repeated held passes", len(evs))
	}
	if evs[0].MemberID != rm.ID {
		t.Errorf("held event member = %q, want %q", evs[0].MemberID, rm.ID)
	}

	// Versions align: the hold clears on its own and the member syncs.
	setMemberVersion(srv, rm.ID, "v1.0.0")
	srv.forceAutoSyncNow(t.Context())
	if !replica.didRealSync() {
		t.Error("aligned member was not synced once the hold cleared")
	}
}

// TestAutoSync_EmitsRecoveredWhenHoldClears: leaving the held-for-skew state
// emits config.sync_recovered exactly once, so config.sync_held is never a
// member's last word once its versions realign (consumers that lead with the
// newest per-member event — Bellhop's member pill, the events feed — would
// otherwise show "held" forever against a live status of verified in sync).
func TestAutoSync_EmitsRecoveredWhenHoldClears(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")

	srv.forceAutoSyncNow(t.Context())
	if n := countEventsOfType(t, store, "config.sync_recovered"); n != 0 {
		t.Fatalf("config.sync_recovered events while still held = %d, want 0", n)
	}

	// Versions align: the same pass that resumes syncing closes the hold.
	setMemberVersion(srv, rm.ID, "v1.0.0")
	srv.forceAutoSyncNow(t.Context())
	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: "config.sync_recovered"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_recovered events = %d, want exactly 1 on the transition out of held", len(evs))
	}
	if evs[0].MemberID != rm.ID {
		t.Errorf("recovered event member = %q, want %q", evs[0].MemberID, rm.ID)
	}

	// Edge-triggered: further aligned passes stay quiet.
	srv.forceAutoSyncNow(t.Context())
	if n := countEventsOfType(t, store, "config.sync_recovered"); n != 1 {
		t.Errorf("config.sync_recovered events after another aligned pass = %d, want still 1", n)
	}
}

// TestAutoSync_ClosesHoldAcrossRestart: the hold set is in-memory, so a restart
// forgets a hold config.sync_held already announced. The first pass that finds
// the member's versions aligned must still emit config.sync_recovered, seeded
// from the persisted event log, or the warning dangles as the member's newest
// event forever.
func TestAutoSync_ClosesHoldAcrossRestart(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")
	srv.forceAutoSyncNow(t.Context()) // announces the hold

	// Simulate a restart: the in-memory hold state is gone, the event log is not.
	srv.syncHeldMu.Lock()
	srv.syncHeld = make(map[string]string)
	srv.holdLogChecked = make(map[string]bool)
	srv.syncHeldMu.Unlock()

	setMemberVersion(srv, rm.ID, "v1.0.0")
	srv.forceAutoSyncNow(t.Context())
	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: "config.sync_recovered"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 || evs[0].MemberID != rm.ID {
		t.Fatalf("config.sync_recovered after restart = %d events, want exactly 1 for the held member", len(evs))
	}

	// The log verdict is consumed: later aligned passes emit nothing more.
	srv.forceAutoSyncNow(t.Context())
	if n := countEventsOfType(t, store, "config.sync_recovered"); n != 1 {
		t.Errorf("config.sync_recovered events after another aligned pass = %d, want still 1", n)
	}
}

// TestAutoSync_HoldLogReadErrorIsNotMemoised: a store error while reconciling
// the persisted hold state reads as "not held" for that pass but is not
// remembered as resolved, so the next pass retries the read instead of
// treating a transient DB failure as a clean log.
func TestAutoSync_HoldLogReadErrorIsNotMemoised(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.db.Close(); err != nil {
		t.Fatalf("close store db: %v", err)
	}
	if srv.heldPerLog(t.Context(), &Member{ID: "m1", Name: "m1"}) {
		t.Error("heldPerLog = true on a store read error, want false")
	}
	srv.syncHeldMu.Lock()
	checked := srv.holdLogChecked["m1"]
	srv.syncHeldMu.Unlock()
	if checked {
		t.Error("a failed log read was memoised as checked; it must retry on the next pass")
	}
}

// TestAutoSync_StillHeldAfterRestartDoesNotRealert: a restart that comes back
// up with the member still skewed is continuing a hold the previous process
// already announced, not entering a new one, so config.sync_held is not
// duplicated after every restart.
func TestAutoSync_StillHeldAfterRestartDoesNotRealert(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")
	srv.forceAutoSyncNow(t.Context()) // announces the hold

	// Simulate a restart with the member still skewed.
	srv.syncHeldMu.Lock()
	srv.syncHeld = make(map[string]string)
	srv.holdLogChecked = make(map[string]bool)
	srv.syncHeldMu.Unlock()

	srv.forceAutoSyncNow(t.Context())
	if n := countEventsOfType(t, store, "config.sync_held"); n != 1 {
		t.Errorf("config.sync_held events after restart while still skewed = %d, want still 1", n)
	}
	if n := countEventsOfType(t, store, "config.sync_recovered"); n != 0 {
		t.Errorf("config.sync_recovered events while still skewed = %d, want 0", n)
	}

	// And the continued hold still closes normally once versions align.
	setMemberVersion(srv, rm.ID, "v1.0.0")
	srv.forceAutoSyncNow(t.Context())
	if n := countEventsOfType(t, store, "config.sync_recovered"); n != 1 {
		t.Errorf("config.sync_recovered once the continued hold cleared = %d, want 1", n)
	}
}

// TestAutoSync_PromotedPrimaryClosesItsHold: a held member the operator
// upgrades and promotes to primary is skipped as the sync source from then on,
// but the hold it carried must still close. Without that, config.sync_held
// stays the new primary's newest event forever: no pass would ever emit
// config.sync_recovered for the one member every pass skips.
func TestAutoSync_PromotedPrimaryClosesItsHold(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")
	srv.forceAutoSyncNow(t.Context()) // announces the hold on the replica

	// The operator promotes the held member to primary and the next pass runs.
	enableAutoSync(t, store, rm.ID)
	srv.forceAutoSyncNow(t.Context())

	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: "config.sync_recovered"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 || evs[0].MemberID != rm.ID {
		t.Fatalf("config.sync_recovered after promotion = %d events, want exactly 1 for the promoted member", len(evs))
	}
	// The message must tell the promotion story: sync was not "resumed" to a
	// member that is now the source and is never synced to.
	if !strings.Contains(evs[0].Message, "it is now the primary") {
		t.Errorf("promoted-primary recovered message = %q, want the promotion wording", evs[0].Message)
	}
	if replica.didRealSync() {
		t.Error("the promoted primary was pushed to; the source is never written to")
	}
}

// TestAutoSync_TokenlessMemberStillClosesItsHold: a held member whose admin
// token is cleared is skipped for measuring and pushing, but once the versions
// realign its hold must still close. The version verdict comes from the
// poller, not the sync token, so Front Desk knows the skew is over and must
// say so rather than leave config.sync_held dangling against a member the
// versions say is fine. The realignment is the primary's version moving onto
// the member's, because that is the direction reachable in production: the
// poller skips a tokenless member, so its own polled version stays frozen at
// the last read taken while it had a token.
func TestAutoSync_TokenlessMemberStillClosesItsHold(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")
	srv.forceAutoSyncNow(t.Context()) // announces the hold on the replica

	// The member's token is cleared while held, then the operator rolls the
	// primary back onto the member's (frozen) version.
	if err := store.SetMemberToken(t.Context(), rm.ID, ""); err != nil {
		t.Fatalf("SetMemberToken: %v", err)
	}
	setMemberVersion(srv, pm.ID, "v0.9.0")
	srv.forceAutoSyncNow(t.Context())

	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: "config.sync_recovered"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 || evs[0].MemberID != rm.ID {
		t.Fatalf("config.sync_recovered for the tokenless member = %d events, want exactly 1", len(evs))
	}
	// The message must not claim sync resumed: without a token there is
	// nothing to resume with, only a skew that ended.
	if strings.Contains(evs[0].Message, "Resumed sync") {
		t.Errorf("tokenless recovered message = %q, want no resumed-sync claim", evs[0].Message)
	}
	if replica.didRealSync() {
		t.Error("a tokenless member was pushed to")
	}
}

// TestAutoSync_VersionSkewedMemberIsHeldEvenWhenItsConfigMatches pins where the
// hash check sits: after the version gate, not before it. A member running a
// different app version is held even when it already holds this exact config, so
// the operator still sees the skew. Hoisting the hash check above the gate would
// skip this member silently: no hold, no config.sync_held alert, its heartbeat
// stamped as verified, and the fleet recorded converged with a skewed member in
// it.
func TestAutoSync_VersionSkewedMemberIsHeldEvenWhenItsConfigMatches(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.versionHash = "hash-B" // this member already holds the primary's config
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")

	srv.forceAutoSyncNow(t.Context())

	if n := countEventsOfType(t, store, "config.sync_held"); n != 1 {
		t.Errorf("config.sync_held events = %d, want 1: a skewed member is held even when its config already matches", n)
	}
	if replica.didRealSync() {
		t.Error("a held member was pushed to")
	}
	if snap := srv.poller.Snapshot(); snap[rm.ID].AutoSyncVerifiedAt != nil {
		t.Error("a held member was stamped verified in sync; the version gate must decide first")
	}
}

// TestAutoSync_MemberWithAnUnknownVersionIsHeldNotMeasured: the reason the
// unreadable-hash flag needs no "has had its chance" guard. A member Front Desk
// has never reached has no polled version, versionSkew fails closed on that, and
// the pass holds it before asking for its hash. So the case that guard would
// protect against cannot reach the flag, and adding it only silenced members that
// can.
func TestAutoSync_MemberWithAnUnknownVersionIsHeldNotMeasured(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionCode = http.StatusInternalServerError
		r.dryDiff = driftDiff
	})
	// Undo the fixture's version alignment for the replica alone: an unreached
	// member's polled version is empty.
	setMemberVersion(f.srv, f.replicaM.ID, "")

	f.tick(t)

	if got := f.replica.versionReadCount(); got != 0 {
		t.Errorf("member hash reads = %d, want 0: a version-skewed member is held before it is measured", got)
	}
	if !unreadableSince(f.srv, f.replicaM.ID).IsZero() {
		t.Error("an unreadable clock was started for a member that was never asked for its hash")
	}
	if memberDiverged(f.srv, f.replicaM.ID) {
		t.Error("a held member was flagged as diverged")
	}
}

// TestAutoSync_UnknownPrimaryBuildHoldsNobody: one blank version read of the
// primary is not skew on every replica. The pass pushes nothing (the gate still
// fails closed) but announces no hold and leaves the fleet state alone; on prod a
// single missed poll of the primary held all three replicas and degraded the
// fleet for fifteen seconds. Once the primary's build reads again the same
// members sync as if nothing happened.
func TestAutoSync_UnknownPrimaryBuildHoldsNobody(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) { r.dryDiff = driftDiff })
	setMemberVersion(f.srv, f.primaryM.ID, "")

	f.tick(t)

	if f.replica.didRealSync() {
		t.Fatal("a pass with an unknown primary build pushed to a replica; the gate must still fail closed")
	}
	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_held"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 0 {
		t.Fatalf("config.sync_held events = %d, want 0: an unread primary build is not skew on its replicas", len(evs))
	}

	setMemberVersion(f.srv, f.primaryM.ID, "dev")
	f.tick(t)
	if !f.replica.didRealSync() {
		t.Error("replica was not synced once the primary's build read again")
	}
}

// TestAutoSync_UnknownPrimaryBuildKeepsAnOpenHold: a hold taken against a known
// primary build neither closes nor re-announces while the primary's build cannot
// be read. The skew it recorded is still the last thing known; only a build that
// reads again, and matches, closes it.
func TestAutoSync_UnknownPrimaryBuildKeepsAnOpenHold(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) { r.dryDiff = driftDiff })
	setMemberVersion(f.srv, f.replicaM.ID, "v0.9.0")
	f.tick(t)
	count := func(typ string) int {
		t.Helper()
		evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: typ})
		if err != nil {
			t.Fatalf("ListEvents %s: %v", typ, err)
		}
		return len(evs)
	}
	if got := count("config.sync_held"); got != 1 {
		t.Fatalf("config.sync_held after the skewed pass = %d, want 1", got)
	}

	setMemberVersion(f.srv, f.primaryM.ID, "")
	f.tick(t)
	f.tick(t)

	if got := count("config.sync_held"); got != 1 {
		t.Errorf("config.sync_held after blank-primary passes = %d, want still 1 (neither re-announced nor cleared)", got)
	}
	if got := count("config.sync_recovered"); got != 0 {
		t.Errorf("config.sync_recovered after blank-primary passes = %d, want 0: an unread primary build clears nothing", got)
	}
	if f.replica.didRealSync() {
		t.Error("a held replica was pushed to while the primary's build was unknown")
	}

	// The build reads again and still differs: the hold stands, silently.
	setMemberVersion(f.srv, f.primaryM.ID, "dev")
	f.tick(t)
	if got := count("config.sync_held"); got != 1 {
		t.Errorf("config.sync_held once the primary reads again = %d, want 1", got)
	}
}

// TestAutoSync_UnknownPrimaryBuildIsColdThenLoud: a skipped pass is not a verdict,
// so the fleet-state inputs stay cold through the grace (a restart into a blank
// primary must not report "no holds" from an empty in-memory set), and a primary
// whose build stays unread past the grace gets the old fail-closed gate back:
// the pass runs, the replica is held and announced, and the inputs are warm.
func TestAutoSync_UnknownPrimaryBuildIsColdThenLoud(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) { r.dryDiff = driftDiff })
	setMemberVersion(f.srv, f.primaryM.ID, "")
	f.srv.autoSyncEvaluated.Store(false)
	held := func() int {
		t.Helper()
		evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_held"})
		if err != nil {
			t.Fatalf("ListEvents: %v", err)
		}
		return len(evs)
	}

	for i := 0; i < unknownPrimaryGracePasses; i++ {
		f.tick(t)
		if f.srv.autoSyncEvaluated.Load() {
			t.Fatalf("pass %d with an unread primary build marked the fleet inputs warm", i+1)
		}
		if got := held(); got != 0 {
			t.Fatalf("pass %d with an unread primary build announced %d holds, want 0", i+1, got)
		}
	}

	f.tick(t)
	if !f.srv.autoSyncEvaluated.Load() {
		t.Error("the pass past the grace ran without marking the fleet inputs warm")
	}
	if got := held(); got != 1 {
		t.Errorf("config.sync_held once the grace is over = %d, want 1: a primary that stays unread must gate out loud", got)
	}
	if f.replica.didRealSync() {
		t.Error("the replica was pushed to while the primary's build was unknown")
	}
}

// TestAutoSync_ReadablePrimaryBuildResetsTheGrace: the grace counts consecutive
// unread passes, so a build that reads once in between starts it over; two
// separate blips never add up to a loud pass.
func TestAutoSync_ReadablePrimaryBuildResetsTheGrace(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) { r.dryDiff = driftDiff })
	blank := func(n int) {
		setMemberVersion(f.srv, f.primaryM.ID, "")
		for i := 0; i < n; i++ {
			f.tick(t)
		}
	}
	blank(unknownPrimaryGracePasses - 1)
	setMemberVersion(f.srv, f.primaryM.ID, "dev")
	f.tick(t)
	blank(unknownPrimaryGracePasses - 1)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_held"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 0 {
		t.Fatalf("config.sync_held after two short blips with a readable pass between = %d, want 0", len(evs))
	}
}

// TestAutoSyncHoldsCommitSkewOnDevFleet: the skew the app version cannot see. A
// self-built fleet reports the "dev" placeholder on every member (the
// Dockerfile's ARG VERSION default), so version equality vouches for nothing;
// mid-rolling-rebuild, the halves run different code while reading identical.
// The commit is what separates them, and a member whose commit differs must be
// held exactly like a version-skewed one, then sync itself once it is rebuilt.
func TestAutoSyncHoldsCommitSkewOnDevFleet(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // this member needs the new config

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	// Identical versions, different commits: the fleet the old gate called aligned.
	setMemberBuild(srv, pm.ID, "dev", "d18a96d1f84d")
	setMemberBuild(srv, rm.ID, "dev", "321f9c86aa10")

	srv.forceAutoSyncNow(t.Context())

	if replica.didBackup() || replica.didRealSync() {
		t.Fatal("member running a different commit was pushed to; want held")
	}
	if memberVerified(srv, rm.ID) {
		t.Error("a commit-skewed member was recorded verified in sync; it was never measured")
	}
	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: "config.sync_held"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_held events = %d, want 1", len(evs))
	}
	// The operator reads this event on a fleet where both versions say "dev":
	// without the commits in the metadata, it names no difference at all.
	if got := evs[0].Metadata["member_commit"]; got != "321f9c86aa10" {
		t.Errorf("member_commit metadata = %v, want 321f9c86aa10", got)
	}
	if got := evs[0].Metadata["primary_commit"]; got != "d18a96d1f84d" {
		t.Errorf("primary_commit metadata = %v, want d18a96d1f84d", got)
	}

	// The member is rebuilt onto the primary's commit: the hold clears itself.
	setMemberBuild(srv, rm.ID, "dev", "d18a96d1f84d")
	srv.forceAutoSyncNow(t.Context())
	if !replica.didRealSync() {
		t.Error("member was not synced once its commit matched the primary's")
	}
}

// TestAutoSyncSyncsWhenCommitUnreadable: a member too old to report app_commit,
// or built without the ldflag, answers "" or "unknown". Holding sync forever on
// a member that cannot answer would be a worse gate than the version-only one,
// so an unanswerable commit falls back to the version verdict and the sync runs.
func TestAutoSyncSyncsWhenCommitUnreadable(t *testing.T) {
	for _, commit := range []string{"", unstampedCommit} {
		t.Run("commit="+commit, func(t *testing.T) {
			srv, store := newTestServer(t)
			primary := newStubAutoMember(t, "ptoken")
			primary.versionHash = "hash-B"
			replica := newStubAutoMember(t, "rtoken")
			replica.dryDiff = driftDiff

			pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
			rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
			enableAutoSync(t, store, pm.ID)
			setMemberBuild(srv, pm.ID, "dev", "d18a96d1f84d")
			setMemberBuild(srv, rm.ID, "dev", commit)

			srv.forceAutoSyncNow(t.Context())

			if !replica.didRealSync() {
				t.Error("member with an unreadable commit was held; want synced on the version verdict")
			}
		})
	}
}
