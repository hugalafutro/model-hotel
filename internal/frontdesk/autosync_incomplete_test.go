package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"
)

// Auto-sync tests for a member that reports an INCOMPLETE apply: the retry
// rate limit, the edge-triggered event, and what an incomplete member does to
// fleet convergence.

// TestAutoSync_IncompleteStampClearsUnconfirmedPush: the incomplete arm stamps
// too (the member committed the config), so it must clear the flag for the same
// reason as the OK arm.
func TestAutoSync_IncompleteStampClearsUnconfirmedPush(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	// A 504 is a lost answer on its own; an instant 502 would be a plain failure
	// now (see lostAnswer5xx).
	replica.realImportCode = http.StatusGatewayTimeout // lost answer, nothing adopted

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B") // lost push: flag set

	srv.resetIncompleteRetries()
	replica.mu.Lock()
	replica.realImportCode = 0
	replica.incompleteImport = true // commits, but cannot build a group
	replica.mu.Unlock()

	srv.autoSyncOnce(t.Context(), "hash-B") // incomplete apply: stamps, must clear the flag

	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt == nil {
		t.Fatal("an incomplete apply did not stamp; the member committed the config")
	}

	// The member later comes to hold the primary's config. With the flag cleared
	// the converged pass must not restamp; overwrite with a sentinel to see.
	if err := store.SetMemberLastSync(t.Context(), rm.ID, time.Now().UTC(), "sentinel"); err != nil {
		t.Fatalf("SetMemberLastSync: %v", err)
	}
	replica.setVersionHash("hash-B")
	srv.autoSyncOnce(t.Context(), "hash-B")
	got, err = store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncReason != "sentinel" {
		t.Errorf("reason = %q, want the sentinel: an incomplete apply's stamp must clear the unconfirmed-push flag", got.LastConfigSyncReason)
	}
}

// TestAutoSync_IncompleteMemberIsNotConverged: a member that commits the config
// but cannot build every custom failover group reports incomplete=true. Front
// Desk must not treat that as converged: OK stays false (feeding the existing
// allConverged=false path so the loop retries this member) and the last-sync
// stamp is left untouched.
func TestAutoSync_IncompleteMemberIsNotConverged(t *testing.T) {
	srv, store := newTestServer(t)
	replica := newStubConfigMember(t, "rtoken")
	replica.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,` +
		`"incomplete":true,"unapplied":["ds4flash","glm52"],"diff":{}}`
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	res := srv.applyMemberConfig(t.Context(), rm, "rtoken", []byte(fleetExportWithKey), "test", false, 0, "")

	if res.OK {
		t.Fatal("OK = true, want false: the member did not fully apply the config")
	}
	if res.Error == "" {
		t.Fatal("Error is empty, want a description of what was not built")
	}
	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	// The member did commit this config, so the marker advances even though the
	// result is not OK. Freezing it would leave a permanently diverged member
	// looking un-synced as well, and trip the staleness watchdog a day later on
	// top of the divergence alert that already names the real problem.
	if got.LastConfigSyncAt == nil {
		t.Error("last-sync marker not stamped on an incomplete apply; the member did commit the config")
	}
}

// TestAutoSync_ResponseWithoutIncompleteFieldReadsAsApplied: an older member that
// never sends the incomplete field decodes it to false, so the push itself
// reports as applied. That is a statement about the push, not about the member:
// whether it ended up holding the config is settled by the hash comparison in the
// loop (see TestAutoSync_OlderMemberOmittingIncompleteIsStillFlagged, where the
// same response is caught).
func TestAutoSync_ResponseWithoutIncompleteFieldReadsAsApplied(t *testing.T) {
	srv, store := newTestServer(t)
	replica := newStubConfigMember(t, "rtoken")
	replica.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,"diff":{}}`
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	res := srv.applyMemberConfig(t.Context(), rm, "rtoken", []byte(fleetExportWithKey), "test", false, 0, "")

	if !res.OK {
		t.Fatalf("OK = false (%s), want true: an older member omits the field", res.Error)
	}
}

// TestAutoSync_IncompleteWithEmptyUnappliedHasSensibleMessage: a member can
// report incomplete with nothing named, either because its whole group-build
// transaction failed before any group was evaluated, or because it runs a build
// whose fault this one has no field for. The message must not degrade into
// "0 failover group(s)... could not be built here: " nonsense, and must not name
// failover groups on a report that never mentioned them.
func TestAutoSync_IncompleteWithEmptyUnappliedHasSensibleMessage(t *testing.T) {
	srv, store := newTestServer(t)
	replica := newStubConfigMember(t, "rtoken")
	replica.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,"incomplete":true,"diff":{}}`
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	res := srv.applyMemberConfig(t.Context(), rm, "rtoken", []byte(fleetExportWithKey), "test", false, 0, "")

	if res.OK {
		t.Fatal("OK = true, want false: the member did not fully apply the config")
	}
	if res.Error == "" {
		t.Fatal("Error is empty, want a description of what was not built")
	}
	const want = "applied, but this member could not materialise all of it"
	if res.Error != want {
		t.Errorf("Error = %q, want %q", res.Error, want)
	}
}

// TestAutoSync_IncompleteEventIsEdgeTriggered: a diverged member is re-checked on
// every pass, so config.sync_incomplete must fire once on the transition in rather
// than once per pass. The matching config.sync_recovered fires once when the
// member finally serves the primary's hash. Driven through the loop, since the
// hash comparison there is what arms and disarms the edge.
func TestAutoSync_IncompleteEventIsEdgeTriggered(t *testing.T) {
	f := newIncompleteFleet(t)

	for range 4 {
		f.settledTick(t)
	}

	evs, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_incomplete events = %d, want exactly 1 across repeated diverged passes", len(evs))
	}
	if evs[0].MemberID != f.replicaM.ID {
		t.Errorf("incomplete event member = %q, want %q", evs[0].MemberID, f.replicaM.ID)
	}
	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Error("incompleteSnapshot does not hold the diverged member")
	}
	if n := countEventsOfType(t, f.store, "config.sync_recovered"); n != 0 {
		t.Errorf("config.sync_recovered events = %d while the member is still diverged, want 0", n)
	}

	// The member's own discovery catches up, so it now serves the primary's hash:
	// recovery fires once, and the passes after it stay quiet.
	f.replica.setVersionHash("hash-B")
	for range 2 {
		f.settledTick(t)
	}

	rec, _, err := f.store.ListEvents(t.Context(), EventFilter{Type: "config.sync_recovered"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(rec) != 1 {
		t.Fatalf("config.sync_recovered events = %d, want exactly 1 across repeated healthy passes", len(rec))
	}
	if rec[0].MemberID != f.replicaM.ID {
		t.Errorf("recovered event member = %q, want %q", rec[0].MemberID, f.replicaM.ID)
	}
	if len(f.srv.incompleteSnapshot()) != 0 {
		t.Errorf("incompleteSnapshot = %v after recovery, want empty", f.srv.incompleteSnapshot())
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d after recovery, want the original 1", n)
	}
}

// TestAutoSync_IncompleteRetryIsRateLimited: an incomplete member is reachable,
// and its dry-run diff is never zero (diffKeyed is presence-based), so every tick
// would otherwise re-import it, and rerun its model discovery, forever, on a member
// that cannot converge. The retry is bounded, and bounding it must not look like
// convergence: the fleet hash stays unrecorded.
func TestAutoSync_IncompleteRetryIsRateLimited(t *testing.T) {
	f := newIncompleteFleet(t)

	f.settledTick(t)
	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("first pass: real imports = %d, want 1", got)
	}

	for range 3 {
		f.settledTick(t)
	}
	if got := f.replica.realSyncCount(); got != 1 {
		t.Errorf("real imports = %d after three further ticks, want 1: the retry is rate-limited", got)
	}
	if got := f.replica.backupCount(); got != 0 {
		t.Errorf("member backups requested = %d, want 0: Front Desk takes no pre-sync snapshot", got)
	}

	if f.verified() {
		t.Error("a member skipped by the retry rate limit was recorded verified; a skipped push proves nothing")
	}
}

// TestAutoSync_IncompleteRetriesAfterTheInterval: the bound delays the retry, it
// does not abandon it. Once the interval has passed the member is pushed again,
// so a member whose discovery catches up converges within one interval.
func TestAutoSync_IncompleteRetriesAfterTheInterval(t *testing.T) {
	f := newIncompleteFleet(t)

	f.settledTick(t)
	f.settledTick(t) // still inside the window
	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("real imports = %d inside the retry window, want 1", got)
	}

	backdateIncompleteRetry(t, f.srv, f.replicaM.ID, incompleteRetryInterval+time.Minute)
	f.settledTick(t)

	if got := f.replica.realSyncCount(); got != 2 {
		t.Errorf("past the interval: real imports = %d, want 2", got)
	}
}

// TestAutoSync_PrimaryEditClearsIncompleteRetryTimers: an operator edit must
// propagate at once, not after the retry bound. The tick that observes the
// primary's hash move clears every timer, so the following (settled) tick
// re-pushes the incomplete member immediately.
func TestAutoSync_PrimaryEditClearsIncompleteRetryTimers(t *testing.T) {
	f := newIncompleteFleet(t)

	f.settledTick(t)
	f.settledTick(t) // rate-limited
	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("real imports = %d inside the retry window, want 1", got)
	}

	// The operator edits the primary, so its config hash moves.
	f.primary.versionHash = "hash-C"

	prev := f.srv.autoSyncOnce(t.Context(), "hash-B")
	if prev != "hash-C" {
		t.Fatalf("coalescing tick returned %q, want hash-C", prev)
	}
	if got := f.replica.realSyncCount(); got != 1 {
		t.Fatalf("real imports = %d on the coalescing tick, want 1: it observes, it does not push", got)
	}

	f.srv.autoSyncOnce(t.Context(), prev)
	if got := f.replica.realSyncCount(); got != 2 {
		t.Errorf("after the primary moved: real imports = %d, want 2", got)
	}
}

// TestAutoSync_IncompleteEventDoesNotRearmOnSkippedTicks: the config.sync_incomplete
// alert is edge-triggered on the transition into incomplete. A rate-limited tick
// leaves the member's incomplete entry in place, so it must not re-arm the edge and
// alert again, either while skipping or on the retry that follows the interval.
func TestAutoSync_IncompleteEventDoesNotRearmOnSkippedTicks(t *testing.T) {
	f := newIncompleteFleet(t)

	f.settledTick(t)
	for range 3 {
		f.settledTick(t)
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Fatalf("config.sync_incomplete events = %d across skipped ticks, want exactly 1", n)
	}

	backdateIncompleteRetry(t, f.srv, f.replicaM.ID, incompleteRetryInterval+time.Minute)
	f.settledTick(t)
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d after the bounded retry, want the original 1", n)
	}
}

// TestAutoSync_IncompleteSnapshotSurvivesRateLimiting: the fleet badge reads the
// incomplete set, so a member whose retry is bounded must still be reported. If
// the bound dropped it the fleet would go green while it serves 404s.
func TestAutoSync_IncompleteSnapshotSurvivesRateLimiting(t *testing.T) {
	f := newIncompleteFleet(t)

	f.settledTick(t)
	for range 3 {
		f.settledTick(t)
	}

	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Error("incompleteSnapshot dropped the member while its retry was rate-limited; the fleet badge would clear")
	}
}

// TestAutoSync_IncompleteMemberLeavesFleetUnconverged drives the loop's own entry
// points rather than applyMemberConfig directly: a member that commits the config
// but cannot build its failover groups leaves the fleet hash unrecorded and its
// last-sync stamp untouched, and the operator's enable-time kick re-pushes it
// rather than waiting out the retry bound.
//
// A kick clears the retry timers, which is also the "this member has had its
// chance" signal, so neither kick flags anything: the deliberate action gets a
// clean run. The tick that follows measures the member against the primary and
// raises the alert.
func TestAutoSync_IncompleteMemberLeavesFleetUnconverged(t *testing.T) {
	f := newIncompleteFleet(t)

	f.srv.forceAutoSyncNow(t.Context())
	f.srv.forceAutoSyncNow(t.Context())

	if got := f.replica.realSyncCount(); got != 2 {
		t.Errorf("real imports = %d across two operator kicks, want 2: a deliberate kick retries now", got)
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 0 {
		t.Errorf("config.sync_incomplete events = %d on the kicks themselves, want 0: a kick re-pushes before it judges", n)
	}

	f.settledTick(t)
	if f.verified() {
		t.Error("a member that could not build every group was recorded verified in sync")
	}
	got, err := f.store.GetMember(t.Context(), f.replicaM.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt == nil {
		t.Error("last-sync marker not stamped on an incomplete apply; the member did commit the config")
	}
	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Error("incompleteSnapshot does not hold the incomplete member")
	}
	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d, want exactly 1 across repeated incomplete passes", n)
	}
}

// TestAutoSync_HealthyMemberIsSyncedOnceWhileAnotherIsIncomplete is the headline
// regression, mirroring the defect measured on a three-member fleet: one member
// that can never converge held allConverged false, so convergeFleet ran every
// tick, and every OTHER healthy member was re-imported (and re-ran its model
// discovery) on each of them. Bounding the incomplete member's retry protects only
// that member.
//
// Across several passes the healthy member must take exactly one import: the one
// that gives it the config. From then on its own config hash equals the primary's
// and it is skipped. The incomplete member stays bounded, the fleet hash stays
// unrecorded (it genuinely has not converged), and no member is asked to snapshot
// itself.
func TestAutoSync_HealthyMemberIsSyncedOnceWhileAnotherIsIncomplete(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"

	incomplete := newStubAutoMember(t, "itoken")
	incomplete.versionHash = "hash-incomplete" // it never builds the custom groups, so it never matches
	incomplete.dryDiff = driftDiff
	incomplete.incompleteImport = true

	healthy := newStubAutoMember(t, "htoken")
	healthy.versionHash = "hash-stale" // needs the config once
	healthy.appliedHash = "hash-B"     // and holds it afterwards
	healthy.dryDiff = driftDiff        // a presence-based diff never reads as converged

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	im, _ := store.CreateMember(t.Context(), "incomplete", incomplete.srv.URL, "itoken")
	store.CreateMember(t.Context(), "healthy", healthy.srv.URL, "htoken")
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")

	for range 5 {
		srv.autoSyncOnce(t.Context(), "hash-B") // settled: every tick runs a convergence pass
	}

	if got := healthy.realSyncCount(); got != 1 {
		t.Errorf("healthy member real imports = %d across 5 passes, want 1: an unconvergeable neighbour must not re-import it every tick", got)
	}
	if got := healthy.backupCount(); got != 0 {
		t.Errorf("healthy member backups = %d, want 0: Front Desk takes no pre-sync snapshot", got)
	}
	if got := incomplete.realSyncCount(); got != 1 {
		t.Errorf("incomplete member real imports = %d across 5 passes, want 1: its retry is bounded", got)
	}
	if memberVerified(srv, im.ID) {
		t.Error("a member that could not build every group was recorded verified in sync")
	}
	if !srv.incompleteSnapshot()[im.ID] {
		t.Error("incompleteSnapshot dropped the incomplete member; the fleet badge would clear")
	}
}

// TestAutoSync_OlderMemberOmittingIncompleteIsStillFlagged closes the deliberate
// fail-open of the self-report: a member running older code answers without the
// incomplete field at all, which decodes to false and reads as a clean apply. Its
// hash still differs, so it is caught anyway.
func TestAutoSync_OlderMemberOmittingIncompleteIsStillFlagged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.dryDiff = driftDiff
		// The default success body carries no incomplete field, exactly as an older
		// member answers.
	})

	f.tick(t)
	f.tick(t)

	if n := countEventsOfType(t, f.store, "config.sync_incomplete"); n != 1 {
		t.Errorf("config.sync_incomplete events = %d, want 1: a response without the field is not evidence of success", n)
	}
	if f.verified() {
		t.Error("a member that never reported its apply was recorded verified")
	}
}

// TestAutoSync_FlagNamesPartiallyBuiltGroups: a member whose model inventory is
// smaller than the primary's builds a custom failover group with fewer entries.
// It skipped nothing, so it has no unapplied groups to report, and the generic
// "does not match" alert would name nothing. The alert must say which group is
// short.
func TestAutoSync_FlagNamesPartiallyBuiltGroups(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted" // a short group hashes differently, forever
		r.dryDiff = driftDiff
		r.realImportBody = partialImportBody
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
	const wantMsg = "replica applied the config but built testgroup with fewer entries than the primary has"
	if evs[0].Message != wantMsg {
		t.Errorf("event message = %q, want %q", evs[0].Message, wantMsg)
	}
	meta, err := json.Marshal(evs[0].Metadata)
	if err != nil {
		t.Fatalf("marshal event metadata: %v", err)
	}
	if got, want := string(meta), `{"partial":["testgroup"],"unapplied":[],"unapplied_models":[],"unreadable":false}`; got != want {
		t.Errorf("event metadata = %s, want %s", got, want)
	}
}

// TestAutoSync_PartialBuildStaysUnconverged: a member that built every group it
// was sent, one of them short, applied the config and says so. It is still
// configured differently from the primary and fails over across fewer providers,
// so its hash differs and it must stay unconverged: the fleet hash is withheld
// and the member is flagged. Reporting a partial build must not become a way to
// pass as converged.
func TestAutoSync_PartialBuildStaysUnconverged(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		r.dryDiff = driftDiff
		r.realImportBody = partialImportBody
	})

	f.tick(t)
	f.tick(t)

	if f.verified() {
		t.Error("a member holding a short failover group was recorded verified")
	}
	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Error("incompleteSnapshot does not hold the partially built member; the fleet badge would stay green")
	}
}

// TestMarkMemberIncompleteMetadataIsNeverCapped: divergenceMessage's per-clause
// name cap only shapes the human-readable Message. A member that named more
// groups than the cap still reports every one of them in the event's Metadata,
// which is where a consumer reads the complete set.
func TestMarkMemberIncompleteMetadataIsNeverCapped(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m, err := store.CreateMember(ctx, "replica", "https://replica.example", "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	unapplied := divergenceCapNames("u", divergenceMessageMaxNames+1)
	partial := divergenceCapNames("p", divergenceMessageMaxNames+1)
	srv.recordSyncAttempt(m.ID, unapplied, partial, nil)
	srv.markMemberIncomplete(ctx, m)

	evs, _, err := store.ListEvents(ctx, EventFilter{Type: "config.sync_incomplete"})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("config.sync_incomplete events = %d, want 1", len(evs))
	}

	const wantMsg = "replica applied the config but could not build 6 failover group(s): " +
		"u1, u2, u3, u4, u5, and 1 more, and built p1, p2, p3, p4, p5, and 1 more " +
		"with fewer entries than the primary has"
	if evs[0].Message != wantMsg {
		t.Fatalf("event message = %q, want %q: the message must stay capped even though metadata will not", evs[0].Message, wantMsg)
	}

	raw, err := json.Marshal(evs[0].Metadata)
	if err != nil {
		t.Fatalf("marshal event metadata: %v", err)
	}
	var meta struct {
		Unapplied []string `json:"unapplied"`
		Partial   []string `json:"partial"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("unmarshal event metadata: %v", err)
	}
	if !slices.Equal(meta.Unapplied, unapplied) {
		t.Errorf("metadata unapplied = %v, want %v: the message cap must not reach Metadata", meta.Unapplied, unapplied)
	}
	if !slices.Equal(meta.Partial, partial) {
		t.Errorf("metadata partial = %v, want %v: the message cap must not reach Metadata", meta.Partial, partial)
	}
}
