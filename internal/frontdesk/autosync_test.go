package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/auth"
)

// TestAutoSyncCoalescesThenApplies: a drifted primary is not synced on the first
// observation (the config might still be mid-edit); only once the hash repeats on
// the next tick does Front Desk propagate it, stamping each changed member's
// last-sync marker. The pass after that verifies the member now serves the
// primary's hash and records the fleet as converged. No member is asked to
// snapshot itself along the way.
func TestAutoSyncCoalescesThenApplies(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff    // this member needs the new config
	replica.appliedHash = "hash-B" // and holds it once the import lands

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	// First tick: observe the change, do not act yet (coalescing window).
	prev := srv.autoSyncOnce(t.Context(), "")
	if prev != "hash-B" {
		t.Fatalf("first tick prev = %q, want hash-B", prev)
	}
	if replica.didRealSync() || replica.didBackup() {
		t.Fatal("replica synced on the first observation; should wait for the hash to settle")
	}

	// Second tick: the hash settled, so propagate.
	srv.autoSyncOnce(t.Context(), prev)
	if replica.didBackup() {
		t.Error("Front Desk asked the replica to snapshot itself; members back themselves up on their own schedule")
	}
	if !replica.didRealSync() {
		t.Error("replica did not receive the config")
	}
	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt == nil {
		t.Error("replica last-sync timestamp not stamped")
	}
	if got.LastConfigSyncReason != autoSyncReason {
		t.Errorf("last-sync reason = %q, want %q", got.LastConfigSyncReason, autoSyncReason)
	}

	// Third tick: the member now serves the primary's hash, so it is measured as
	// converged. Verification costs this one extra tick.
	srv.autoSyncOnce(t.Context(), prev)
	if !memberVerified(srv, rm.ID) {
		t.Error("member serves the primary's hash but was not verified in sync")
	}
	if memberDiverged(srv, rm.ID) {
		t.Error("member serves the primary's hash but is still flagged as diverged")
	}
}

// TestForceAutoSyncNowConvergesImmediately: the enable-time kick pushes to a
// drifted fleet in a single pass, with no coalescing wait, and stamps the
// member's last-sync marker with the "auto-sync was enabled" reason.
func TestForceAutoSyncNowConvergesImmediately(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff    // this member needs the new config
	replica.appliedHash = "hash-B" // and holds it once the import lands

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	// Single call, no prior tick: the kick must act at once.
	srv.forceAutoSyncNow(t.Context())

	if replica.didBackup() {
		t.Error("the kick asked the replica to snapshot itself; members back themselves up on their own schedule")
	}
	if !replica.didRealSync() {
		t.Error("replica did not receive the config on the kick")
	}
	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncReason != autoSyncKickReason {
		t.Errorf("last-sync reason = %q, want %q", got.LastConfigSyncReason, autoSyncKickReason)
	}

	// The following pass measures the member against the primary and finds it
	// converged.
	srv.autoSyncOnce(t.Context(), "hash-B")
	if !memberVerified(srv, rm.ID) {
		t.Error("member serves the primary's hash but was not verified in sync")
	}
}

// TestForceAutoSyncNowDisabledIsNoop: the kick does nothing when auto-sync is off
// (e.g. the operator toggled it back off before the goroutine ran).
func TestForceAutoSyncNowDisabledIsNoop(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	if err := store.SetAutoSync(t.Context(), false, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}

	srv.forceAutoSyncNow(t.Context())

	if replica.didRealSync() || replica.didBackup() {
		t.Error("kick synced a member while auto-sync was disabled")
	}
}

// TestConvergeFleetAbortsAfterRearm: a convergence pass that captured an older
// rearm generation (because a member add, token update, or repoint landed while
// it was applying) must abort rather than push a config built for the fleet as
// it was. The member is left untouched and unmeasured for the rearm's own pass,
// which converges it against the current primary and member list.
func TestConvergeFleetAbortsAfterRearm(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.appliedHash = "hash-B"

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	// The generation the pass captured before it read the member list.
	cfg, _ := store.GetAutoSync(t.Context())
	staleGen := cfg.Gen
	// A rearm lands mid-pass: clears the marker and bumps the generation.
	if err := store.RearmAutoSync(t.Context()); err != nil {
		t.Fatalf("RearmAutoSync: %v", err)
	}

	// The older pass runs at the stale generation. It must not mutate members: no
	// stale primary config is pushed, and the member is left for the rearm's own
	// pass rather than being measured or written by this one.
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", nil, autoSyncReason, staleGen)

	if replica.didBackup() || replica.didRealSync() {
		t.Error("stale pass pushed config to a member after the rearm; want aborted before mutating")
	}
	got, err := store.GetAutoSync(t.Context())
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}

	// A pass at the current generation pushes, and the one after it verifies the
	// member normally.
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", nil, autoSyncReason, got.Gen)
	if !replica.didRealSync() {
		t.Error("a pass at the current generation did not push to the member")
	}
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", nil, autoSyncReason, got.Gen)
	if !memberVerified(srv, rm.ID) {
		t.Error("member was not verified in sync by the pass after the push")
	}
}

// TestConvergeFleetAbortsImportWhenRearmLandsAfterDryRun: the tightest race the
// pre-import gates can still close. A rearm/repoint lands during a member's (slow)
// dry-run diff, after the loop's top-of-iteration staleness check. The final gate
// must catch it: the member is NOT overwritten with the now-stale export, and the
// hash is not recorded, so the rearm's own pass converges it with the fresh primary.
func TestConvergeFleetAbortsImportWhenRearmLandsAfterDryRun(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // needs the new config, so it reaches the import path

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	cfg, _ := store.GetAutoSync(t.Context())
	staleGen := cfg.Gen
	// The rearm fires inside the member's dry-run, opening the post-dry-run /
	// pre-import window the final gate exists to close.
	replica.onDryRun = func() {
		if err := store.RearmAutoSync(t.Context()); err != nil {
			t.Errorf("RearmAutoSync: %v", err)
		}
	}

	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", nil, autoSyncReason, staleGen)

	if replica.dryRunCount() == 0 {
		t.Fatal("test setup: the dry-run never ran, so the post-dry-run window was not exercised")
	}
	if replica.didRealSync() {
		t.Error("imported stale export after a rearm landed post-backup; want aborted before mutating")
	}
}

// TestConvergeFleetCancelsImportInFlightOnRearm: the irreducible window the pre-
// import gates cannot cover. A rearm/repoint lands while the member import HTTP
// call is already in flight. watchRearm must cancel the request context so the
// import aborts before committing rather than writing the now-stale export, and
// no hash is recorded so the rearm's own pass reconverges the member.
func TestConvergeFleetCancelsImportInFlightOnRearm(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // needs the new config, so it reaches the real import

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	cfg, _ := store.GetAutoSync(t.Context())
	staleGen := cfg.Gen
	// The repoint lands the instant the import arrives, then the member handler stalls
	// well past the watcher's poll interval to model a slow import. If watchRearm does
	// its job it cancels the client request out from under applyMemberConfig, so the
	// pass returns far sooner than this ceiling; if it does not, the client blocks the
	// full stall and the pass runs long. Elapsed time is therefore the cancellation
	// signal. onImport never reports a commit, so didRealSync is a clean secondary
	// check. The handler unblocking later is irrelevant: convergeFleet does not wait
	// on it once the client call is cancelled.
	const stall = 2 * time.Second
	replica.onImport = func(reqCtx context.Context) bool {
		srv.rearmAutoSync(t.Context()) // bumps the generation and broadcasts the cancel
		select {
		case <-reqCtx.Done():
		case <-time.After(stall):
		}
		return false
	}

	start := time.Now()
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", nil, autoSyncReason, staleGen)
	if elapsed := time.Since(start); elapsed > stall-time.Second {
		t.Errorf("convergeFleet ran %v; watchRearm did not cancel the in-flight import", elapsed)
	}
	if replica.didRealSync() {
		t.Error("in-flight import committed after a rearm; want the request cancelled before commit")
	}
}

// TestAutoSyncSkipsConvergedMember: a member already serving the primary's hash
// is left untouched (no import) and measured as verified in sync. A tokenless
// member alongside it is skipped rather than pushed to.
func TestAutoSyncSkipsConvergedMember(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.versionHash = "hash-B" // already holds the primary's config

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	replicaMember, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	// A tokenless member is present too: it cannot be authenticated to, so it is
	// skipped rather than pushed to or measured.
	tokenless, err := store.CreateMember(t.Context(), "tokenless", "http://127.0.0.1:9", "")
	if err != nil {
		t.Fatalf("create tokenless member: %v", err)
	}
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B") // already settled: act this tick

	if replica.didBackup() || replica.didRealSync() {
		t.Error("a converged member must not be snapshotted or re-imported")
	}
	if !memberVerified(srv, replicaMember.ID) {
		t.Error("a member already serving the primary's hash was not verified in sync")
	}
	if memberVerified(srv, tokenless.ID) {
		t.Error("a tokenless member was verified in sync; it cannot be measured at all")
	}
	// Nothing was written to the converged member, so its persisted
	// last_config_sync_at must NOT move: that column means a real config write.
	rm, err := store.GetMember(t.Context(), replicaMember.ID)
	if err != nil {
		t.Fatalf("get replica: %v", err)
	}
	if rm.LastConfigSyncAt != nil {
		t.Error("converged member LastConfigSyncAt was stamped; want untouched (no write happened)")
	}
	// Instead, the live "verified in sync" heartbeat advances so the Members table
	// shows auto-sync confirmed the member against the primary.
	snap := srv.poller.Snapshot()
	if snap[replicaMember.ID].AutoSyncVerifiedAt == nil {
		t.Error("converged member AutoSyncVerifiedAt = nil, want the verify heartbeat stamped")
	}
	// Only a measured member may be stamped. The tokenless one cannot be asked what
	// it holds, and the primary is the source rather than something in sync with
	// itself, so neither carries a heartbeat.
	if snap[tokenless.ID].AutoSyncVerifiedAt != nil {
		t.Error("tokenless member was stamped verified; it can never be measured")
	}
	if snap[pm.ID].AutoSyncVerifiedAt != nil {
		t.Error("primary was stamped verified; the primary is the source, not a synced member")
	}
}

// TestAutoSync_MemberWithADifferentConfigIsSynced: the skip is narrow. A member
// whose own hash differs from the primary's is pushed to as before.
func TestAutoSync_MemberWithADifferentConfigIsSynced(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.versionHash = "hash-drifted"
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if got := replica.realSyncCount(); got != 1 {
		t.Errorf("real imports = %d, want 1: a member holding a different config is synced", got)
	}
}

// TestAutoSyncTakesNoPreSyncBackup: Front Desk overwrites a drifted member without
// asking it to snapshot itself first. Members back themselves up on their own
// schedule, so a member's backup endpoint is never on the sync path and the fleet
// converges without one being reachable at all.
func TestAutoSyncTakesNoPreSyncBackup(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.appliedHash = "hash-B"

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B") // pushes
	srv.autoSyncOnce(t.Context(), "hash-B") // verifies

	if !replica.didRealSync() {
		t.Error("the drifted member was not overwritten")
	}
	if got := replica.backupCount(); got != 0 {
		t.Errorf("member backups requested = %d, want 0: Front Desk takes no pre-sync snapshot", got)
	}
	if got := primary.backupCount(); got != 0 {
		t.Errorf("primary backups requested = %d, want 0", got)
	}
	if !memberVerified(srv, rm.ID) {
		t.Error("the member was not verified in sync after converging without a snapshot")
	}
}

// TestAutoSyncUnreachableMemberIsNotVerified: a member whose import probe fails
// (its server is down) is left untouched and never counts as verified in sync,
// so the next tick measures it again rather than taking silence for agreement.
func TestAutoSyncUnreachableMemberIsNotVerified(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	// A dead URL: the dry-run import is a transport failure, not an HTTP answer.
	down, _ := store.CreateMember(t.Context(), "down", "http://127.0.0.1:9", "dtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if memberVerified(srv, down.ID) {
		t.Error("an unreachable member was recorded verified in sync; nothing about it was measured")
	}
}

// TestAutoSyncSchemaBlockedMemberSkipped: a member that reports a schema or
// MASTER_KEY mismatch is held off (not overwritten) and never counts as verified.
func TestAutoSyncSchemaBlockedMemberSkipped(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	blocked := newStubAutoMember(t, "btoken")
	blocked.dryDiff = driftDiff
	blocked.importCode = http.StatusUnprocessableEntity // 422: schema mismatch
	blocked.importBody = `{"schema_version_ok":false,"master_key_ok":false}`

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	bm, _ := store.CreateMember(t.Context(), "blocked", blocked.srv.URL, "btoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if blocked.didBackup() || blocked.didRealSync() {
		t.Error("a schema-blocked member must not be snapshotted or overwritten")
	}
	if memberVerified(srv, bm.ID) {
		t.Error("a member that cannot take the config was recorded verified in sync")
	}
}

// TestAutoSync_GatewayErroredPushStampsOnVerify: a push answered 5xx by a proxy
// mid-import is not stamped (the write was never confirmed), but the import
// completes member-side; the next pass measures the member holding the primary's
// hash, which proves the push landed, and stamps last_config_sync_at then. A
// converged pass after that must not stamp again: the marker still means a real
// write, not a heartbeat.
func TestAutoSync_GatewayErroredPushStampsOnVerify(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.appliedHash = "hash-B"                     // the import applies...
	replica.realImportCode = http.StatusGatewayTimeout // ...but the answer is a proxy's 504

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if got := replica.realSyncCount(); got != 1 {
		t.Fatalf("real imports = %d, want 1", got)
	}
	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt != nil {
		t.Fatal("a push answered 504 was stamped as a sync; the write was never confirmed")
	}

	// The next pass measures the member serving hash-B: the lost push is proven
	// landed, so the marker it missed is stamped now.
	srv.autoSyncOnce(t.Context(), "hash-B")

	got, err = store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt == nil {
		t.Fatal("a verified member with an unconfirmed push behind it was not stamped")
	}
	if got.LastConfigSyncReason != unconfirmedSyncReason {
		t.Errorf("reason = %q, want %q", got.LastConfigSyncReason, unconfirmedSyncReason)
	}
	if !memberVerified(srv, rm.ID) {
		t.Error("the converged member was not verified in sync")
	}

	// The stamp is once per lost push, not per converged tick: overwrite the
	// marker with a sentinel and prove a further converged pass leaves it alone.
	if err := store.SetMemberLastSync(t.Context(), rm.ID, time.Now().UTC(), "sentinel"); err != nil {
		t.Fatalf("SetMemberLastSync: %v", err)
	}
	srv.autoSyncOnce(t.Context(), "hash-B")
	got, err = store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncReason != "sentinel" {
		t.Errorf("reason = %q after a quiet converged pass, want the sentinel untouched: converged ticks must not stamp", got.LastConfigSyncReason)
	}
}

// TestAutoSync_TimedOutPushStampsOnVerify: the same proof for the other lost
// answer, Front Desk's own relay deadline expiring while the member is still
// importing. The push is not stamped; the pass that measures the member holding
// the primary's hash stamps it.
func TestAutoSync_TimedOutPushStampsOnVerify(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.appliedHash = "hash-B"
	replica.onImport = func(context.Context) bool {
		time.Sleep(time.Second) // outlives the relay deadline below, then commits
		return true
	}
	// Comfortably above the dry-run round-trip even on a loaded machine, and a
	// quarter of the import sleep above, so the real import always times out.
	srv.syncClient = newProbeClient(250 * time.Millisecond)

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	// realSyncCount waits on the stub's mutex, so this both proves the import
	// committed after the client hung up and fences the next pass behind it.
	if got := replica.realSyncCount(); got != 1 {
		t.Fatalf("real imports = %d, want 1: the import commits after the deadline", got)
	}
	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt != nil {
		t.Fatal("a timed-out push was stamped as a sync; the write was never confirmed")
	}

	srv.autoSyncOnce(t.Context(), "hash-B")

	got, err = store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt == nil {
		t.Fatal("a verified member with a timed-out push behind it was not stamped")
	}
	if got.LastConfigSyncReason != unconfirmedSyncReason {
		t.Errorf("reason = %q, want %q", got.LastConfigSyncReason, unconfirmedSyncReason)
	}
}

// TestAutoSync_RefusedPushDoesNotStampOnVerify: a 4xx is a definite refusal, so
// no unconfirmed push is remembered. A member that later converges anyway (the
// primary edited back to what it already held) is verified but NOT stamped:
// Front Desk never wrote to it.
func TestAutoSync_RefusedPushDoesNotStampOnVerify(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.realImportCode = http.StatusUnauthorized // definite refusal, nothing applied

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	// The member ends up holding the primary's config through no write of Front
	// Desk's (a revert on the primary looks like this).
	replica.setVersionHash("hash-B")
	srv.autoSyncOnce(t.Context(), "hash-B")

	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt != nil {
		t.Error("a refused push was stamped on convergence; Front Desk never wrote to this member")
	}
	if !memberVerified(srv, rm.ID) {
		t.Error("the converged member was not verified in sync")
	}
}

// TestAutoSync_ConfirmedStampClearsUnconfirmedPush: a lost push followed by a
// confirmed one must not leave the flag behind, or the converged pass after the
// confirmed sync would overwrite its honest stamp (timestamp AND reason) with the
// unconfirmed-push wording.
func TestAutoSync_ConfirmedStampClearsUnconfirmedPush(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.realImportCode = http.StatusGatewayTimeout // lost answer, nothing adopted

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B") // lost push: flag set, no stamp

	// The lost push is rate-limited like a timeout; drop the timer so the next
	// pass may push again, as a primary edit would.
	srv.resetIncompleteRetries()
	replica.mu.Lock()
	replica.realImportCode = 0
	replica.mu.Unlock()
	replica.setAppliedHash("hash-B")

	srv.autoSyncOnce(t.Context(), "hash-B") // confirmed push: honest stamp, flag cleared

	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncReason != autoSyncReason {
		t.Fatalf("reason = %q, want %q from the confirmed push", got.LastConfigSyncReason, autoSyncReason)
	}

	// The converged pass must leave the confirmed stamp alone: prove it with a
	// sentinel it would overwrite if the flag had survived.
	if err := store.SetMemberLastSync(t.Context(), rm.ID, time.Now().UTC(), "sentinel"); err != nil {
		t.Fatalf("SetMemberLastSync: %v", err)
	}
	srv.autoSyncOnce(t.Context(), "hash-B")
	got, err = store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncReason != "sentinel" {
		t.Errorf("reason = %q, want the sentinel: a confirmed stamp must clear the unconfirmed-push flag", got.LastConfigSyncReason)
	}
}

// TestAutoSync_InstantGatewayErrorRetriesNextTick: an instant 5xx is not a lost
// answer. A proxy answers 502 immediately when its upstream is down (a member
// that crashed mid-import looks like this), so nothing is importing behind such
// an answer: the re-push must come on the next tick rather than sitting out
// incompleteRetryInterval, and no unconfirmed push is remembered, so a member
// that later converges through no write of Front Desk's is verified but never
// stamped.
func TestAutoSync_InstantGatewayErrorRetriesNextTick(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.realImportCode = http.StatusBadGateway // instant: the proxy's upstream is down

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")
	if got := replica.realSyncCount(); got != 1 {
		t.Fatalf("real imports = %d, want 1", got)
	}

	// The push failed outright, so it must not be rate-limited: the next pass
	// pushes again instead of waiting out incompleteRetryInterval.
	srv.autoSyncOnce(t.Context(), "hash-B")
	if got := replica.realSyncCount(); got != 2 {
		t.Fatalf("real imports after the next tick = %d, want 2: an instant 5xx is a plain failure, not a maybe-landed import", got)
	}

	// The member comes to hold the primary's config through no write of Front
	// Desk's (an operator restore, or the primary edited back): verified, but not
	// stamped, because no unconfirmed push was remembered for it.
	replica.setVersionHash("hash-B")
	srv.autoSyncOnce(t.Context(), "hash-B")
	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt != nil {
		t.Error("an instant-5xx push was stamped on convergence; the write was never confirmed and nothing was importing behind the answer")
	}
	if !memberVerified(srv, rm.ID) {
		t.Error("the converged member was not verified in sync")
	}
}

// TestClearUnconfirmedPushKeepsNewerHash: clearing is compare-and-delete. The
// stamp paths check the flag, write the stamp, then clear; a concurrent push
// (the wizard runs on its own goroutine) can lose ITS answer to newer config in
// that window and re-mark the member. The stale clear must not drop the newer
// entry, or that push would never be stamped on verification.
func TestClearUnconfirmedPushKeepsNewerHash(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.markUnconfirmedPush("m1", "hash-C")

	srv.clearUnconfirmedPush("m1", "hash-B") // a stamp for an older push landing late
	if !srv.hasUnconfirmedPush("m1", "hash-C") {
		t.Fatal("a stale clear dropped a newer unconfirmed push; its verify stamp is lost")
	}

	srv.clearUnconfirmedPush("m1", "hash-C")
	if srv.hasUnconfirmedPush("m1", "hash-C") {
		t.Error("clearing the matching hash must drop the entry")
	}
}

// TestPutAutoSyncValidation: enabling needs a primary, and the primary must be a
// known member with a stored admin token (the loop authenticates with it).
func TestPutAutoSyncValidation(t *testing.T) {
	srv, store := newTestServer(t)
	withTok, _ := store.CreateMember(t.Context(), "with-token", "http://127.0.0.1:9", "tok")
	noTok, _ := store.CreateMember(t.Context(), "no-token", "http://127.0.0.1:10", "")

	// Enable without a primary: rejected.
	if rec := do(t, srv, http.MethodPut, "/api/fleet/autosync", `{"enabled":true,"primary_id":""}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("enable without primary = %d, want 400", rec.Code)
	}
	// Primary with no stored token: rejected.
	if rec := do(t, srv, http.MethodPut, "/api/fleet/autosync", `{"enabled":true,"primary_id":"`+noTok.ID+`"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("tokenless primary = %d, want 400", rec.Code)
	}
	// Unknown primary: rejected.
	if rec := do(t, srv, http.MethodPut, "/api/fleet/autosync", `{"enabled":true,"primary_id":"00000000-0000-0000-0000-000000000000"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown primary = %d, want 400", rec.Code)
	}
	// Valid: a tokened primary.
	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync", `{"enabled":true,"primary_id":"`+withTok.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid enable = %d (%s)", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if !cfg.Enabled || cfg.PrimaryID != withTok.ID {
		t.Errorf("auto-sync = %+v, want enabled at %s", cfg, withTok.ID)
	}
}

// TestDeleteMemberClearsPrimary: removing the designated primary clears the
// pointer so the auto-sync loop stops treating a gone member as the source.
func TestDeleteMemberClearsPrimary(t *testing.T) {
	_, store := newTestServer(t)
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "tok")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	if err := store.DeleteMember(t.Context(), pm.ID); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.PrimaryID != "" {
		t.Errorf("primary_id = %q after deleting the primary, want cleared", cfg.PrimaryID)
	}
}

// TestDeleteMemberForgetsItsInMemoryState: the three per-member flags Front Desk
// keeps outside the store (skew hold, config divergence, stale backups) are
// dropped when the member is removed, so a member re-added later starts clean and
// the maps do not accumulate an entry per member ever removed.
func TestDeleteMemberForgetsItsInMemoryState(t *testing.T) {
	srv, store := newTestServer(t)
	// A second member so removing the first is not blocked by the last-active guard.
	if _, err := store.CreateMember(t.Context(), "keep", "http://127.0.0.1:9", "tok"); err != nil {
		t.Fatalf("create keep: %v", err)
	}
	gone, err := store.CreateMember(t.Context(), "gone", "http://127.0.0.1:10", "tok")
	if err != nil {
		t.Fatalf("create gone: %v", err)
	}

	srv.syncHeld[gone.ID] = memberBuild{Version: "dev", Commit: "old"}.key()
	srv.syncIncomplete[gone.ID] = incompleteState{diverged: true, lastAttempt: time.Now()}
	srv.backupStale[gone.ID] = true

	rec := do(t, srv, http.MethodDelete, "/api/members/"+gone.ID, "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete member = %d (%s)", rec.Code, rec.Body.String())
	}

	if _, stillHeld := srv.syncHeld[gone.ID]; stillHeld {
		t.Error("skew hold survived the member's removal")
	}
	if _, ok := srv.syncIncomplete[gone.ID]; ok {
		t.Error("divergence state survived the member's removal")
	}
	if srv.backupStale[gone.ID] {
		t.Error("backup staleness flag survived the member's removal")
	}
}

// TestAutoSyncDisabledIsNoop: with auto-sync off, the loop touches nothing.
func TestAutoSyncDisabledIsNoop(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	// Designate a primary but leave auto-sync disabled.
	if err := store.SetAutoSync(t.Context(), false, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}

	if got := srv.autoSyncOnce(t.Context(), "hash-B"); got != "" {
		t.Errorf("disabled autoSyncOnce returned %q, want empty", got)
	}
	if replica.didBackup() || replica.didRealSync() {
		t.Error("disabled auto-sync touched a member")
	}
}

// TestAutoSyncNoChangeWhenHashUnchanged: a primary that has not moved still hands
// its hash to the next tick's coalescing window, and the member already holding
// that config is measured rather than written to, however much its presence-based
// dry-run diff would claim otherwise.
func TestAutoSyncNoChangeWhenHashUnchanged(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-A"
	replica := newStubAutoMember(t, "rtoken")
	replica.versionHash = "hash-A" // already holds the primary's config
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID)                                       // last applied == current
	alignFleetVersions(t, srv, store, "dev")

	if got := srv.autoSyncOnce(t.Context(), "hash-A"); got != "hash-A" {
		t.Errorf("autoSyncOnce = %q, want hash-A carried forward", got)
	}
	if replica.didBackup() || replica.didRealSync() {
		t.Error("an unchanged primary triggered a sync")
	}
}

// TestAutoSyncPrimaryTokenlessIsNoop: a designated primary with no stored token
// can't be read, so the loop does nothing rather than erroring.
func TestAutoSyncPrimaryTokenlessIsNoop(t *testing.T) {
	srv, store := newTestServer(t)
	// Point auto-sync at a tokenless member directly (the handler would reject this,
	// but the loop must still be defensive if the token is later cleared).
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	if got := srv.autoSyncOnce(t.Context(), ""); got != "" {
		t.Errorf("tokenless primary returned %q, want empty", got)
	}
}

// TestFetchMemberConfigVersionRejectsBadResponses: the drift probe rejects a
// non-200, malformed JSON, and an empty version string.
func TestFetchMemberConfigVersionRejectsBadResponses(t *testing.T) {
	srv, store := newTestServer(t)
	stub := newStubAutoMember(t, "tok")
	created, _ := store.CreateMember(t.Context(), "m", stub.srv.URL, "tok")
	m, err := store.GetMember(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}

	for name, mutate := range map[string]func(){
		"non-200":    func() { stub.versionCode = http.StatusInternalServerError },
		"bad json":   func() { stub.versionCode = http.StatusOK; stub.versionRaw = "not json" },
		"empty hash": func() { stub.versionCode = http.StatusOK; stub.versionRaw = `{"version":""}` },
	} {
		t.Run(name, func(t *testing.T) {
			stub.mu.Lock()
			stub.versionCode = http.StatusOK
			stub.versionRaw = ""
			stub.mu.Unlock()
			mutate()
			if _, _, err := srv.fetchMemberConfigVersion(t.Context(), m, "tok"); err == nil {
				t.Errorf("%s: expected an error, got nil", name)
			}
		})
	}
}

// TestAutoSyncRearmsOnTokenAdd is the Greptile fix: a tokenless member is skipped
// while the fleet is recorded converged, but the moment it gains an admin token the
// applied hash is cleared so the next tick brings it in line, without waiting for
// the primary's config to change again.
func TestAutoSyncRearmsOnTokenAdd(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	// Replica is added without a token, so it is not yet syncable.
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	before, _ := store.GetAutoSync(t.Context())

	// Give the replica an admin token via the API. This must re-arm auto-sync.
	rec := do(t, srv, http.MethodPatch, "/api/members/"+rm.ID, `{"token":"rtoken"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch token = %d (%s)", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.Gen == before.Gen {
		t.Fatalf("rearm generation = %d, want it bumped so a pass in flight for the old fleet aborts", cfg.Gen)
	}

	// The next settled pass now converges the freshly-tokened replica.
	alignFleetVersions(t, srv, store, "dev")
	prev := srv.autoSyncOnce(t.Context(), "")
	srv.autoSyncOnce(t.Context(), prev)
	if !replica.didRealSync() {
		t.Error("newly-tokened replica was not synced after re-arm")
	}
}

// TestAutoSyncRearmsOnMemberAdd: adding a new member with a token re-arms the loop
// so the newcomer is converged without waiting for the primary to change.
func TestAutoSyncRearmsOnMemberAdd(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	newcomer := newStubAutoMember(t, "ntoken")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	before, _ := store.GetAutoSync(t.Context())

	body := `{"name":"newcomer","url":"` + newcomer.srv.URL + `","token":"ntoken"}`
	rec := do(t, srv, http.MethodPost, "/api/members", body, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create member = %d (%s)", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.Gen == before.Gen {
		t.Errorf("rearm generation = %d, want it bumped after adding a tokened member", cfg.Gen)
	}
}

// TestRunAutoSyncStopsOnContextCancel: the loop returns promptly when its context
// is cancelled.
func TestRunAutoSyncStopsOnContextCancel(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	done := make(chan struct{})
	go func() { srv.RunAutoSync(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("RunAutoSync did not return after context cancel")
	}
}

// TestGetAutoSyncHandler: the GET endpoint returns the current setup.
func TestGetAutoSyncHandler(t *testing.T) {
	srv, store := newTestServer(t)
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "tok")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/fleet/autosync", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("get autosync = %d", rec.Code)
	}
	var got AutoSyncConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled || got.PrimaryID != pm.ID {
		t.Errorf("autosync = %+v, want enabled at %s", got, pm.ID)
	}
}

// TestAutoSyncTokenLoadFailureIsNotVerified (Greptile P1): a member whose stored
// token ciphertext can't be decrypted (e.g. a MASTER_KEY mismatch) has HasToken
// true but fails MemberToken. Nothing about it can be measured, so it must never
// read as verified in sync; the next pass tries again.
func TestAutoSyncTokenLoadFailureIsNotVerified(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", "http://127.0.0.1:9", "rtoken")
	// Replace the replica's token with ciphertext encrypted under a DIFFERENT master
	// key: the fields are correctly sized (so decrypt fails authentication rather than
	// panicking) and HasToken stays true, reproducing a MASTER_KEY-mismatch token that
	// MemberToken cannot decrypt.
	kp, err := auth.Encrypt("rtoken", testMasterKey+"-mismatch")
	if err != nil {
		t.Fatalf("encrypt under mismatched key: %v", err)
	}
	if _, err := store.db.ExecContext(t.Context(),
		`UPDATE members SET token_cipher = ?, token_nonce = ?, token_salt = ? WHERE id = ?`,
		kp.Ciphertext, kp.Nonce, kp.Salt, rm.ID,
	); err != nil {
		t.Fatalf("write mismatched token: %v", err)
	}
	enableAutoSync(t, store, pm.ID)

	srv.autoSyncOnce(t.Context(), "hash-B") // settled: reach the apply stage

	if memberVerified(srv, rm.ID) {
		t.Error("a member whose token could not be decrypted was recorded verified in sync")
	}
}

// TestSetAutoSyncRearms: changing the auto-sync setup bumps the rearm
// generation, so a pass in flight for the previous setup aborts instead of
// finishing a write the operator has just invalidated.
func TestSetAutoSyncRearms(t *testing.T) {
	_, store := newTestServer(t)
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "tok")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	before, _ := store.GetAutoSync(t.Context())
	// Re-applying the setup (re-enable, or any primary change) must rearm.
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.Gen == before.Gen {
		t.Errorf("rearm generation = %d after re-applying setup, want it bumped", cfg.Gen)
	}
}

// TestSetAutoSyncGuarded covers the atomic repoint guard: an unauthorized write
// may set the first primary or leave it unchanged, but may not repoint a
// configured one; an authorized (valid-token) write may repoint freely.
func TestSetAutoSyncGuarded(t *testing.T) {
	_, store := newTestServer(t)
	a, _ := store.CreateMember(t.Context(), "a", "http://127.0.0.1:9", "tok")
	b, _ := store.CreateMember(t.Context(), "b", "http://127.0.0.1:8", "tok")

	// First set from the empty state needs no token.
	applied, err := store.SetAutoSyncGuarded(t.Context(), true, a.ID, false)
	if err != nil {
		t.Fatalf("guarded first: %v", err)
	}
	if !applied {
		t.Fatal("first set from empty primary should apply without a token")
	}

	// Toggling enabled while leaving the primary unchanged needs no token and is
	// honored (this is the enable/disable control).
	applied, err = store.SetAutoSyncGuarded(t.Context(), true, a.ID, false)
	if err != nil {
		t.Fatalf("guarded unchanged: %v", err)
	}
	if !applied {
		t.Fatal("unchanged-primary write should apply without a token")
	}
	if cfg, _ := store.GetAutoSync(t.Context()); !cfg.Enabled {
		t.Fatal("unchanged-primary toggle should have enabled auto-sync")
	}

	// Repointing a configured primary without a valid token must not apply and
	// must leave the stored primary untouched.
	applied, err = store.SetAutoSyncGuarded(t.Context(), true, b.ID, false)
	if err != nil {
		t.Fatalf("guarded unauthorized repoint: %v", err)
	}
	if applied {
		t.Fatal("repoint without a token must not apply")
	}
	if cfg, _ := store.GetAutoSync(t.Context()); cfg.PrimaryID != a.ID {
		t.Fatalf("primary = %q after refused repoint, want %q", cfg.PrimaryID, a.ID)
	}

	// The same repoint with a valid token applies, and must preserve the stored
	// enabled flag: a confirmed primary change carries enabled=false here (a stale
	// snapshot), but auto-sync is on, so it must stay on.
	applied, err = store.SetAutoSyncGuarded(t.Context(), false, b.ID, true)
	if err != nil {
		t.Fatalf("guarded authorized repoint: %v", err)
	}
	if !applied {
		t.Fatal("repoint with a valid token should apply")
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.PrimaryID != b.ID {
		t.Fatalf("primary = %q after authorized repoint, want %q", cfg.PrimaryID, b.ID)
	}
	if !cfg.Enabled {
		t.Fatal("repoint must preserve the stored enabled flag, not the request's stale value")
	}

	// Clearing the primary forces auto-sync off regardless of the request's flag:
	// it cannot run without a primary, so even a stale enabled=true must not stick.
	applied, err = store.SetAutoSyncGuarded(t.Context(), true, "", true)
	if err != nil {
		t.Fatalf("guarded clear: %v", err)
	}
	if !applied {
		t.Fatal("clear with a valid token should apply")
	}
	cfg, _ = store.GetAutoSync(t.Context())
	if cfg.PrimaryID != "" {
		t.Fatalf("primary = %q after clear, want empty", cfg.PrimaryID)
	}
	if cfg.Enabled {
		t.Fatal("clearing the primary must force auto-sync off")
	}
}

// TestStoreAutoSyncDBErrors: the auto-sync store methods surface DB failures
// rather than swallowing them. Closing the handle forces every query to fail.
func TestStoreAutoSyncDBErrors(t *testing.T) {
	_, store := newTestServer(t)
	if err := store.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	ctx := context.Background()
	if _, err := store.GetAutoSync(ctx); err == nil {
		t.Error("GetAutoSync: want error on a closed DB")
	}
	if err := store.SetAutoSync(ctx, true, "x"); err == nil {
		t.Error("SetAutoSync: want error on a closed DB")
	}
	if err := store.RearmAutoSync(ctx); err == nil {
		t.Error("RearmAutoSync: want error on a closed DB")
	}
	if err := store.SetMemberLastSync(ctx, "id", time.Now(), "r"); err == nil {
		t.Error("SetMemberLastSync: want error on a closed DB")
	}
}

// TestGetAutoSyncHandlerDBError: a store failure surfaces as a 500, not a silent
// empty body. The admin token authenticates without a DB read, so the error comes
// from the handler's own GetAutoSync call.
func TestGetAutoSyncHandlerDBError(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/fleet/autosync", "", true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("get autosync on closed DB = %d, want 500", rec.Code)
	}
}

// TestPutAutoSyncDBError: a store failure surfaces as a 500. Clearing the primary
// skips member validation, so on a closed DB the guarded write is what fails.
func TestPutAutoSyncDBError(t *testing.T) {
	srv, store := newTestServer(t)
	if err := store.db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync", `{"enabled":false,"primary_id":""}`, true)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("put autosync on closed DB = %d, want 500", rec.Code)
	}
}

// TestPutAutoSyncDisable: turning auto-sync off is accepted and persisted.
func TestPutAutoSyncDisable(t *testing.T) {
	srv, store := newTestServer(t)
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "tok")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	// Clearing the configured primary is gated on a fresh admin-token confirmation
	// (TestServerAutoSyncPrimaryGate covers the refusal path), so pass the token.
	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync",
		`{"enabled":false,"primary_id":"","confirm_token":"`+testFrontdeskToken+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("disable = %d (%s)", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.Enabled {
		t.Error("auto-sync still enabled after disable")
	}
}

// TestAutoSyncSendsSourceGenHeader: a real auto-sync import carries the current
// rearm generation in X-Fleet-Source-Gen, the token the member's commit fence
// uses to refuse a stale, out-of-order push.
func TestAutoSyncSendsSourceGenHeader(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // this member needs the new config

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	_, _ = store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.forceAutoSyncNow(t.Context())

	if !replica.didRealSync() {
		t.Fatal("replica did not receive the config")
	}
	cfg, _ := store.GetAutoSync(t.Context())
	want := strconv.FormatInt(cfg.Gen, 10)
	if got := replica.sourceGen(); got != want {
		t.Errorf("real import X-Fleet-Source-Gen = %q, want %q (current rearm generation)", got, want)
	}
}

// TestAutoSync_HashMatchAfterPushConvergesTheFleet: the pass that pushes only
// pushes. Verification is the next pass's hash query, and only once the member
// serves the primary's hash is the fleet recorded as converged. That costs one
// extra tick and is what makes convergence a measurement rather than a claim.
func TestAutoSync_HashMatchAfterPushConvergesTheFleet(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-stale"
		r.appliedHash = "hash-B" // it genuinely holds the primary's config afterwards
		r.dryDiff = driftDiff
	})

	f.tick(t)
	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("first pass: real imports = %d, want 1", got)
	}
	if f.verified() {
		t.Error("member recorded verified on the pushing pass; verification is the next pass's hash query")
	}

	f.tick(t)
	if got := f.replica.realSyncCount(); got != 1 {
		t.Errorf("real imports = %d after the verifying pass, want 1: a matching member is left alone", got)
	}
	if !f.verified() {
		t.Error("member not recorded verified once its own hash matched the primary's")
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 0 {
		t.Errorf("config.sync_incomplete events = %d for a member that converged, want 0", n)
	}
	if len(f.srv.incompleteSnapshot()) != 0 {
		t.Errorf("incompleteSnapshot = %v, want empty", f.srv.incompleteSnapshot())
	}
	if snap := f.srv.poller.Snapshot(); snap[f.replicaM.ID].AutoSyncVerifiedAt == nil {
		t.Error("verified member AutoSyncVerifiedAt = nil, want the heartbeat stamped")
	}
}

// TestAutoSync_LargeExportIsStillRead: a config envelope bigger than the shared
// member-response cap must still be read. The export is the one member response
// whose size grows with the fleet's own config, and it is load-bearing for every
// member: a refused read aborts the whole pass, so nothing converges and the
// failure repeats every tick. Its limit therefore matches what the member-side
// import will accept, not the cap sized for fixed-shape documents.
func TestAutoSync_LargeExportIsStillRead(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.dryDiff = driftDiff
		r.appliedHash = "hash-B"
	})
	// Comfortably past maxMemberRespBody (1 MiB) and comfortably under the export's
	// own limit, padded inside the envelope so it stays valid JSON.
	pad := strings.Repeat("p", 2<<20)
	f.primary.mu.Lock()
	f.primary.exportBody = `{"schema_version":2,"pad":"` + pad + `","config":{}}`
	f.primary.mu.Unlock()

	f.tick(t)

	if got := f.primary.exportCount(); got != 1 {
		t.Fatalf("primary exports = %d, want 1", got)
	}
	if !f.replica.didRealSync() {
		t.Error("a >1 MiB export was refused, so the member never received the config")
	}
}

// TestAutoSync_ConvergedFleetKeepsMeasuringItsMembers: convergence is a
// measurement with a shelf life. Once the fleet holds the primary's config the
// pass keeps running on every tick, so each member's own hash is read again and
// again. The cost of that steady state is exactly those reads: a member that
// matches is never dry-run, never imported into, and never asked to snapshot
// itself, the primary is never asked to build its config export, and the tick
// emits nothing.
func TestAutoSync_ConvergedFleetKeepsMeasuringItsMembers(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-B" // already holds the primary's config
		r.dryDiff = driftDiff    // a presence-based diff would claim otherwise
	})

	f.tick(t)
	if !f.verified() {
		t.Fatal("member not verified after the first pass; it already matches the primary")
	}
	base := f.replica.versionReadCount()
	if base == 0 {
		t.Fatalf("member hash reads = 0 on the converging pass, want at least 1")
	}

	const ticks = 3
	for range ticks {
		f.tick(t)
	}

	if got, want := f.replica.versionReadCount(), base+ticks; got != want {
		t.Errorf("member hash reads = %d after %d further ticks, want %d: a settled fleet is re-measured every tick",
			got, ticks, want)
	}
	if got := f.replica.dryRunCount(); got != 0 {
		t.Errorf("dry-runs = %d, want 0: a matching member is skipped before the diff", got)
	}
	if got := f.replica.realSyncCount(); got != 0 {
		t.Errorf("real imports = %d, want 0: a matching member is never written to", got)
	}
	if got := f.replica.backupCount(); got != 0 {
		t.Errorf("member backups requested = %d, want 0", got)
	}
	// The primary's export is the expensive half of a pass: it builds and ships the
	// whole config envelope. A settled fleet has no member to give it to, so it is
	// never asked for one, however many ticks run.
	if got := f.primary.exportCount(); got != 0 {
		t.Errorf("primary config exports = %d, want 0: a settled fleet never needs the envelope", got)
	}
	for _, typ := range []string{"config.auto_synced", "config.sync_incomplete", "config.sync_recovered", "config.sync_held"} {
		if n := countEventsOfType(t, f.store, typ); n != 0 {
			t.Errorf("%s events = %d on a settled fleet, want 0", typ, n)
		}
	}
	// Nothing was written to the member, so its persisted stamp stays put while the
	// live verify heartbeat keeps advancing.
	rm, err := f.store.GetMember(t.Context(), f.replicaM.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if rm.LastConfigSyncAt != nil {
		t.Error("a re-measured member had last_config_sync_at stamped; want it left for real writes only")
	}
	if snap := f.srv.poller.Snapshot(); snap[f.replicaM.ID].AutoSyncVerifiedAt == nil {
		t.Error("verified member AutoSyncVerifiedAt = nil, want the heartbeat stamped every pass")
	}
}

// TestAutoSync_ConvergedFleetDoesNotFlap: running a pass on every tick must not
// make a settled fleet twitch. Across several passes no member is ever flagged,
// no event fires, and the fleet state stays ok.
func TestAutoSync_ConvergedFleetDoesNotFlap(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-B"
		r.dryDiff = driftDiff
	})

	for i := range 5 {
		f.tick(t)
		if len(f.srv.incompleteSnapshot()) != 0 {
			t.Fatalf("tick %d: incompleteSnapshot = %v, want empty", i, f.srv.incompleteSnapshot())
		}
		if len(f.srv.heldSnapshot()) != 0 {
			t.Fatalf("tick %d: heldSnapshot = %v, want empty", i, f.srv.heldSnapshot())
		}
		state, reasons, _, err := f.srv.fleetStateNow(t.Context())
		if err != nil {
			t.Fatalf("tick %d: fleetStateNow: %v", i, err)
		}
		if state != FleetOK {
			t.Fatalf("tick %d: fleet state = %q %v, want ok", i, state, reasons)
		}
	}
	if got := f.replica.realSyncCount(); got != 0 {
		t.Errorf("real imports = %d across five settled ticks, want 0", got)
	}
	if !f.verified() {
		t.Error("member lost its verified state across repeated passes")
	}
}

// TestAutoSync_MidEditPrimaryRunsNoPass: the coalescing gate outlives the change.
// A primary whose hash differs from the previous tick's is mid-edit, so the tick
// observes and returns: no member is measured and none is written to, however
// settled the fleet was before.
func TestAutoSync_MidEditPrimaryRunsNoPass(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-B"
		r.dryDiff = driftDiff
	})

	f.tick(t) // converged
	reads := f.replica.versionReadCount()

	f.primary.setVersionHash("hash-C") // the operator is part-way through an edit

	if got := f.srv.autoSyncOnce(t.Context(), "hash-B"); got != "hash-C" {
		t.Fatalf("autoSyncOnce = %q, want hash-C carried into the next tick", got)
	}
	if got := f.replica.versionReadCount(); got != reads {
		t.Errorf("member hash reads = %d on a mid-edit tick, want them held at %d", got, reads)
	}
	if got := f.replica.realSyncCount(); got != 0 {
		t.Errorf("real imports = %d on a mid-edit tick, want 0", got)
	}
}

// TestAutoSync_ConfigVersionReadUsesReadClientNotProbe proves the config-version
// read is bound by memberReadTimeout, not the 4s health-probe timeout: the
// endpoint builds and hashes the whole config envelope, so it is not cheap once
// every member's hash is read on every tick. The probe is deliberately set
// shorter than the member's response delay; a converged result (the fleet hash
// recorded on the first pass, since the replica already matches) proves the read
// did not route through it.
func TestAutoSync_ConfigVersionReadUsesReadClientNotProbe(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-B" // already matches the primary: no push needed
		r.versionDelay = 200 * time.Millisecond
	})
	f.srv.probe = newProbeClient(50 * time.Millisecond)
	f.srv.readClient = newProbeClient(3 * time.Second)

	f.tick(t)

	if got := f.replica.realSyncCount(); got != 0 {
		t.Errorf("real imports = %d, want 0: an already-matching member is never pushed to", got)
	}
	if !f.verified() {
		t.Error("member not verified on the first pass; a probe-timeout read would have left it unmeasured")
	}
}

// TestAutoSync_PushedMemberIsNotStampedVerifiedUntilItMatches: a completed write
// is not a verification. A member that commits every import and never ends up
// holding the config is re-pushed once per incompleteRetryInterval forever, so a
// heartbeat taken from the write would keep reading "verified in sync" beside the
// amber badge that says the member does not match. That pairing is what kept the
// original divergence invisible. Only the hash comparison may stamp it.
func TestAutoSync_PushedMemberIsNotStampedVerifiedUntilItMatches(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted" // and it never adopts the primary's
		r.dryDiff = driftDiff
	})

	f.tick(t) // pushes, and the member commits it
	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("first pass: real imports = %d, want 1", got)
	}
	if snap := f.srv.poller.Snapshot(); snap[f.replicaM.ID].AutoSyncVerifiedAt != nil {
		t.Error("member stamped verified in sync by the write alone; only a matching hash may do that")
	}

	f.tick(t) // measures the divergence and flags it
	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Fatal("test setup: the member was not flagged, so the pairing under test never arose")
	}

	// The bounded retry comes round: the member commits another import and still
	// does not match.
	backdateIncompleteRetry(t, f.srv, f.replicaM.ID, incompleteRetryInterval+time.Minute)
	f.tick(t)
	if got := f.replica.realSyncCount(); got != 2 {
		t.Fatalf("past the interval: real imports = %d, want 2", got)
	}
	if snap := f.srv.poller.Snapshot(); snap[f.replicaM.ID].AutoSyncVerifiedAt != nil {
		t.Error("re-push refreshed the verified heartbeat of a member measured as diverged")
	}
}

// TestConfigSync_WizardStampsVerifiedOnItsOwnWrite: the operator-driven path keeps
// the old behaviour. The wizard is a deliberate action whose result is the write
// itself, so it advances the heartbeat; the auto-sync loop's call to the same
// function does not, because it takes that reading from its own hash comparison.
func TestConfigSync_WizardStampsVerifiedOnItsOwnWrite(t *testing.T) {
	srv, store := newTestServer(t)
	wizard := newStubConfigMember(t, "wtoken")
	auto := newStubConfigMember(t, "atoken")
	wm, _ := store.CreateMember(t.Context(), "wizard", wizard.srv.URL, "wtoken")
	am, _ := store.CreateMember(t.Context(), "auto", auto.srv.URL, "atoken")

	// emitSuccessEvent true is the wizard's call; false is the auto-syncer's.
	if res := srv.applyMemberConfig(t.Context(), wm, "wtoken", []byte(fleetExportWithKey),
		manualSyncReason("the dashboard"), true, 0, ""); !res.OK {
		t.Fatalf("wizard sync OK = false (%s), want true", res.Error)
	}
	if res := srv.applyMemberConfig(t.Context(), am, "atoken", []byte(fleetExportWithKey),
		autoSyncReason, false, 0, ""); !res.OK {
		t.Fatalf("auto sync OK = false (%s), want true", res.Error)
	}

	snap := srv.poller.Snapshot()
	if snap[wm.ID].AutoSyncVerifiedAt == nil {
		t.Error("wizard member AutoSyncVerifiedAt = nil, want the heartbeat stamped on a completed write")
	}
	if snap[am.ID].AutoSyncVerifiedAt != nil {
		t.Error("auto-sync member AutoSyncVerifiedAt was stamped by the write; the loop's hash check owns that")
	}
}

// TestAutoSync_EmptyDiffDoesNotOverrideTheHash: the dry-run diff cannot promote a
// member to converged, and it cannot veto an import either. The diff covers
// providers, virtual keys and settings only, so a member differing in a custom
// failover group or a per-model disable reports nothing to write while its hash
// correctly says it is out of sync. The hash decides both ways: the member is
// imported into, and it still does not get the "verified in sync" heartbeat, which
// means the hash matched and nothing else.
func TestAutoSync_EmptyDiffDoesNotOverrideTheHash(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		// The default dryDiff is empty: the presence-based diff sees nothing to write.
	})

	f.tick(t)

	if got := f.replica.dryRunCount(); got == 0 {
		t.Fatal("dry-runs = 0: a member whose hash differs must still be evaluated")
	}
	if got := f.replica.realSyncCount(); got != 1 {
		t.Errorf("real imports = %d, want 1: an empty diff cannot outvote a hash that differs", got)
	}
	if f.verified() {
		t.Error("a member that does not serve the primary's hash was recorded verified; the diff must not outvote the hash")
	}
	if snap := f.srv.poller.Snapshot(); snap[f.replicaM.ID].AutoSyncVerifiedAt != nil {
		t.Error("member stamped verified in sync while its hash differs")
	}
}

// TestSyncFailureMessage: the event line names the cause. A timed-out push is not
// a refusal, and reads as one unless it is worded apart: the member took the
// request and is very likely still importing, which is exactly why the caller
// stamps it as received and rate-limits the re-push.
func TestSyncFailureMessage(t *testing.T) {
	tests := []struct {
		name     string
		cause    string
		timedOut bool
		want     string
	}{
		{
			name:  "names the cause",
			cause: "MASTER_KEY does not match the primary",
			want:  "Failed to sync config to beta: MASTER_KEY does not match the primary",
		},
		{
			name: "no cause to name",
			want: "Failed to sync config to beta",
		},
		{
			name:     "a timeout is not a failure to apply",
			cause:    "this member did not answer in time",
			timedOut: true,
			want:     "beta did not answer the config push in time; it may still be applying",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := syncFailureMessage("beta", tc.cause, tc.timedOut); got != tc.want {
				t.Errorf("syncFailureMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestAutoSync_TimedOutPushEventSaysItMayStillBeApplying: the end-to-end shape of
// the above. A member still importing when the relay gives up must not be reported
// as having failed, and the specific cause has to reach the event rather than only
// the log.
func TestAutoSync_TimedOutPushEventSaysItMayStillBeApplying(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.dryDiff = driftDiff
		r.onImport = func(reqCtx context.Context) bool {
			select {
			case <-reqCtx.Done():
			case <-time.After(5 * time.Second):
			}
			return false
		}
	})
	f.srv.syncClient = newProbeClient(150 * time.Millisecond)

	f.tick(t)

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_failed"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_failed events = %d, want 1", len(evs))
	}
	if strings.Contains(evs[0].Message, "Failed to sync") {
		t.Errorf("message = %q; a member still importing must not be reported as a failed sync", evs[0].Message)
	}
	if !strings.Contains(evs[0].Message, "may still be applying") {
		t.Errorf("message = %q, want it to say the member may still be applying", evs[0].Message)
	}
	if got := evs[0].Metadata["timed_out"]; got != true {
		t.Errorf("metadata timed_out = %v, want true", got)
	}
	if got, _ := evs[0].Metadata["error"].(string); got == "" {
		t.Error("metadata carries no cause, so the precise reason still reaches only the log")
	}
}

// TestAutoSync_ModelOnlyChangeConverges: the end-to-end shape of the empty-diff
// rule, and the reason it had to change. When the only thing the primary altered
// is a per-model disable, the member's dry-run diff is empty (the diff covers
// providers, virtual keys and settings) while its hash differs. Before this the
// pass skipped it on the empty diff, so the member sat amber forever and the
// operator's disable never reached it.
func TestAutoSync_ModelOnlyChangeConverges(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-stale"
		r.appliedHash = "hash-B" // applying the envelope does converge it
		// dryDiff stays the default empty: nothing the diff can see has changed.
	})

	f.tick(t) // pushes on the hash alone
	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("real imports = %d, want 1: the member had to be written to", got)
	}

	f.tick(t) // measures
	if !f.verified() {
		t.Error("a member whose only difference was per-model state never converged")
	}
	if memberDiverged(f.srv, f.replicaM.ID) {
		t.Error("the member stayed flagged after converging")
	}
}
