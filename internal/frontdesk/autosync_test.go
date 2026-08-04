package frontdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// stubAutoMember plays a member for the auto-sync loop: it answers the config
// version GET (the drift signal), the export GET, and the dry-run and real config
// imports. Each is independently configurable so a single stub can be a primary or
// a replica in any disposition.
//
// It also answers POST /api/backups and counts the calls. No sync path may call
// that endpoint: members back themselves up on their own schedule. Serving it (and
// asserting the count is zero) catches a reintroduced pre-sync backup as a counted
// call rather than as a 404 that some other assertion happens to notice.
type stubAutoMember struct {
	mu          sync.Mutex
	srv         *httptest.Server
	token       string
	instanceID  string
	versionHash string
	// appliedHash, when set, is the config-version hash this member starts
	// reporting once it accepts a real import: a member that applied the primary's
	// config hashes identically to it, which is what lets the next pass skip it. An
	// incomplete import never adopts it, since the member did not build everything.
	appliedHash string
	versionCode int    // status for the version GET (default 200)
	versionRaw  string // raw version body; overrides the {"version":...} JSON when set
	// versionDelay, when set, holds the version GET response for that long before
	// answering. It stands in for the envelope-build-and-hash cost the real
	// endpoint pays, to prove which client (probe vs. read) the caller used.
	versionDelay time.Duration
	exportBody   string
	exportCode   int    // status for the export GET (default 200)
	dryDiff      string // diff object returned on a dry-run import
	importCode   int    // status for the dry-run import (default 200)
	importBody   string // full dry-run import body; overrides dryDiff when set
	// realImportBody is the full body the real (non-dry-run) import answers with,
	// overriding the default success response. It is how a test pins exactly what
	// the member claims about its own apply: an explicit "incomplete":false, or an
	// older member's response that omits the field entirely.
	realImportBody string
	gotBackup      bool
	backups        int // how many backups this member was asked to take; must stay 0
	dryRuns        int // how many dry-run imports this member was asked for
	gotRealSync    bool
	realSyncs      int    // how many real (non-dry-run) imports this member accepted
	gotSourceGen   string // X-Fleet-Source-Gen seen on the last real (non-dry-run) import
	staleImport    bool   // when true, the real import answers with the commit-fence "stale" response
	// incompleteImport makes the real import answer applied-but-incomplete: the
	// core config committed, one custom failover group could not be built.
	incompleteImport bool
	// onDryRun fires inside the dry-run import handler, to simulate a rearm landing
	// in the window between the (slow) dry-run and the final staleness gate.
	onDryRun func()
	// onImport fires inside the real (non-dry-run) import handler. It receives the
	// request context and returns whether the import should be recorded as applied;
	// returning false models the import being cancelled in flight before it commits.
	onImport func(reqCtx context.Context) (commit bool)
}

func newStubAutoMember(t *testing.T, token string) *stubAutoMember {
	t.Helper()
	sm := &stubAutoMember{
		token:       token,
		instanceID:  fmt.Sprintf("iid-auto-%d", memberServerSeq.Add(1)),
		versionHash: "hash-A",
		exportBody:  fleetExportWithKey,
		dryDiff:     `{"providers":{},"virtual_keys":{},"settings":{}}`, // converged
		versionCode: http.StatusOK,
		exportCode:  http.StatusOK,
		importCode:  http.StatusOK,
	}
	sm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+sm.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sm.mu.Lock()
		defer sm.mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/version":
			if sm.versionDelay > 0 {
				time.Sleep(sm.versionDelay)
			}
			w.WriteHeader(sm.versionCode)
			if sm.versionRaw != "" {
				_, _ = w.Write([]byte(sm.versionRaw))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"version": sm.versionHash})
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/export":
			w.WriteHeader(sm.exportCode)
			_, _ = w.Write([]byte(sm.exportBody))
		case r.Method == http.MethodPost && r.URL.Path == "/api/config/import":
			if r.URL.Query().Get("dryRun") != "" {
				sm.dryRuns++
				if sm.onDryRun != nil {
					sm.onDryRun() // simulate a rearm/repoint landing after the dry-run, before the import
				}
				w.WriteHeader(sm.importCode)
				if sm.importBody != "" {
					_, _ = w.Write([]byte(sm.importBody))
					return
				}
				_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":false,"diff":` + sm.dryDiff + `}`))
				return
			}
			sm.gotSourceGen = r.Header.Get(fleetSourceGenHeader)
			if sm.staleImport {
				// Simulate the member's commit fence refusing a stale, out-of-order push.
				_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":false,"stale":true,"diff":` + sm.dryDiff + `}`))
				return
			}
			if sm.onImport != nil && !sm.onImport(r.Context()) {
				return // import cancelled in flight before commit: record nothing
			}
			sm.gotRealSync = true
			sm.realSyncs++
			if sm.incompleteImport {
				_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":true,` +
					`"incomplete":true,"unapplied":["ds4flash"],"diff":` + sm.dryDiff + `}`))
				return
			}
			if sm.appliedHash != "" {
				sm.versionHash = sm.appliedHash // the member now holds the primary's config
			}
			if sm.realImportBody != "" {
				_, _ = w.Write([]byte(sm.realImportBody))
				return
			}
			_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":true,"diff":` + sm.dryDiff + `}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/settings":
			// The token probe (createMember/patchMember) hits this; 200 = accepted.
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/system"):
			// The fleet-identity self-report the add path reads: a faithful member
			// stub answers a non-primary box with a unique instance_id.
			_, _ = w.Write([]byte(`{"fleet":{"is_primary":false},"instance_id":"` + sm.instanceID + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/backups":
			// No sync path may reach here; the counters exist to prove it.
			sm.gotBackup = true
			sm.backups++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"filename":"backup_x_frontdesk.dump"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

func (sm *stubAutoMember) didBackup() bool { sm.mu.Lock(); defer sm.mu.Unlock(); return sm.gotBackup }
func (sm *stubAutoMember) didRealSync() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.gotRealSync
}
func (sm *stubAutoMember) backupCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.backups
}

func (sm *stubAutoMember) dryRunCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.dryRuns
}

func (sm *stubAutoMember) realSyncCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.realSyncs
}

func (sm *stubAutoMember) sourceGen() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.gotSourceGen
}

// setVersionHash changes the hash this member serves, standing in for the member
// finally holding the primary's config (or drifting away from it) between passes.
func (sm *stubAutoMember) setVersionHash(h string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.versionHash = h
}

const driftDiff = `{"providers":{"added":["anthropic"]},"virtual_keys":{},"settings":{}}`

// enableAutoSync points auto-sync at primaryID with a stale last-applied hash, so
// the loop sees the primary as changed.
func enableAutoSync(t *testing.T, store *Store, primaryID, lastHash string) {
	t.Helper()
	if err := store.SetAutoSync(t.Context(), true, primaryID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	seedAutoSyncHash(t, store, lastHash)
}

// seedAutoSyncHash stamps an "already applied" hash at the current rearm
// generation, so a following convergence pass (which captures that same
// generation) records onto it rather than no-opping.
func seedAutoSyncHash(t *testing.T, store *Store, hash string) {
	t.Helper()
	cfg, err := store.GetAutoSync(t.Context())
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}
	if _, err := store.RecordAutoSyncHash(t.Context(), hash, cfg.Gen); err != nil {
		t.Fatalf("RecordAutoSyncHash: %v", err)
	}
}

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
	enableAutoSync(t, store, pm.ID, "hash-A")
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

	// Third tick: the member now serves the primary's hash, so the fleet is recorded
	// as converged. Verification costs this one extra tick.
	srv.autoSyncOnce(t.Context(), prev)
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-B" {
		t.Errorf("applied hash = %q, want hash-B recorded once the member was verified", cfg.LastHash)
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
	enableAutoSync(t, store, pm.ID, "hash-A")
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

	// The following pass verifies the member against the primary and records the
	// fleet as converged.
	srv.autoSyncOnce(t.Context(), "hash-B")
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-B" {
		t.Errorf("applied hash = %q, want hash-B recorded once the member was verified", cfg.LastHash)
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

// TestConvergeFleetSkipsRecordAfterRearm: a convergence pass that captured an
// older rearm generation (because a member add, token update, or repoint landed
// while it was applying) must not write its now-stale hash over the cleared
// marker. The marker stays empty so the next tick re-converges with the fresh
// fleet, rather than skipping it as already-applied.
func TestConvergeFleetSkipsRecordAfterRearm(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.appliedHash = "hash-B"

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")

	// The generation the pass captured before it read the member list.
	cfg, _ := store.GetAutoSync(t.Context())
	staleGen := cfg.Gen
	// A rearm lands mid-pass: clears the marker and bumps the generation.
	if err := store.RearmAutoSync(t.Context()); err != nil {
		t.Fatalf("RearmAutoSync: %v", err)
	}

	// The older pass runs at the stale generation. It must not mutate members
	// (no stale primary config pushed) and must not record its hash.
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", autoSyncReason, staleGen)

	if replica.didBackup() || replica.didRealSync() {
		t.Error("stale pass pushed config to a member after the rearm; want aborted before mutating")
	}
	got, err := store.GetAutoSync(t.Context())
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}
	if got.LastHash != "" {
		t.Errorf("stale pass overwrote the rearm-cleared marker: %q, want empty", got.LastHash)
	}

	// A pass at the current generation pushes, and the one after it verifies the
	// member and records normally.
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", autoSyncReason, got.Gen)
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", autoSyncReason, got.Gen)
	got, _ = store.GetAutoSync(t.Context())
	if got.LastHash != "hash-B" {
		t.Errorf("current-gen record = %q, want hash-B", got.LastHash)
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
	enableAutoSync(t, store, pm.ID, "hash-A")
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

	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", autoSyncReason, staleGen)

	if replica.dryRunCount() == 0 {
		t.Fatal("test setup: the dry-run never ran, so the post-dry-run window was not exercised")
	}
	if replica.didRealSync() {
		t.Error("imported stale export after a rearm landed post-backup; want aborted before mutating")
	}
	got, _ := store.GetAutoSync(t.Context())
	if got.LastHash != "" {
		t.Errorf("stale pass recorded a hash after the rearm: %q, want empty", got.LastHash)
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
	enableAutoSync(t, store, pm.ID, "hash-A")
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
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", autoSyncReason, staleGen)
	if elapsed := time.Since(start); elapsed > stall-time.Second {
		t.Errorf("convergeFleet ran %v; watchRearm did not cancel the in-flight import", elapsed)
	}
	if replica.didRealSync() {
		t.Error("in-flight import committed after a rearm; want the request cancelled before commit")
	}
	got, _ := store.GetAutoSync(t.Context())
	if got.LastHash != "" {
		t.Errorf("stale pass recorded a hash after the rearm: %q, want empty", got.LastHash)
	}
}

// TestAutoSyncSkipsConvergedMember: a member already serving the primary's hash is
// left untouched (no import), and the fleet counts as converged so the new hash is
// recorded and the loop quiesces. A tokenless member alongside it must not hold
// that back.
func TestAutoSyncSkipsConvergedMember(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.versionHash = "hash-B" // already holds the primary's config

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	replicaMember, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	// A tokenless member is present too: it must be skipped without blocking the
	// fleet from being recorded as converged.
	store.CreateMember(t.Context(), "tokenless", "http://127.0.0.1:9", "") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B") // already settled: act this tick

	if replica.didBackup() || replica.didRealSync() {
		t.Error("a converged member must not be snapshotted or re-imported")
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-B" {
		t.Errorf("applied hash = %q, want hash-B (fleet converged)", cfg.LastHash)
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
	if snap := srv.poller.Snapshot(); snap[replicaMember.ID].AutoSyncVerifiedAt == nil {
		t.Error("converged member AutoSyncVerifiedAt = nil, want the verify heartbeat stamped")
	}
}

// TestAutoSync_MemberHoldingThisConfigIsSkipped: every member serves the same
// content hash of its syncable config, so a member reporting the primary's hash
// already holds exactly this config and is skipped outright, without even the
// dry-run. The dry-run cannot establish that (it keys on presence, so a matching
// member still reports every shared entity as updated), which is why the member's
// own hash is read first. The skip must not hold the fleet hash back: a member that
// matches is genuinely converged.
func TestAutoSync_MemberHoldingThisConfigIsSkipped(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.versionHash = "hash-B" // this member already holds the primary's config
	replica.dryDiff = driftDiff    // a presence-based diff would claim it needs syncing

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if got := replica.dryRunCount(); got != 0 {
		t.Errorf("dry-runs = %d, want 0: a member holding this config is skipped before the diff", got)
	}
	if got := replica.realSyncCount(); got != 0 {
		t.Errorf("real imports = %d, want 0: the member already holds this config", got)
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-B" {
		t.Errorf("applied hash = %q, want hash-B: skipping a matching member must not hold the fleet back", cfg.LastHash)
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
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if got := replica.realSyncCount(); got != 1 {
		t.Errorf("real imports = %d, want 1: a member holding a different config is synced", got)
	}
}

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
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if got := replica.dryRunCount(); got == 0 {
		t.Error("dry-runs = 0: a member whose hash could not be read must still be evaluated")
	}
	if got := replica.realSyncCount(); got != 1 {
		t.Errorf("real imports = %d, want 1: an unreadable hash is not a converged member", got)
	}
}

// setMemberHealth seeds the poller's in-memory health for a member so tests that
// gate on reachability (the quiet verify tick) can drive the up, down, and
// never-probed paths without a live /health probe.
func setMemberHealth(srv *Server, memberID string, known, healthy bool) {
	srv.poller.mu.Lock()
	st := srv.poller.statuses[memberID]
	st.Health = HealthStatus{Known: known, Healthy: healthy}
	srv.poller.statuses[memberID] = st
	srv.poller.mu.Unlock()
}

// setMemberHealthFailures seeds the poller's consecutive-failure count, so a test
// can model a member inside the fail-threshold grace window: its badge still
// reads healthy (last known good) while its latest probe is actually failing.
func setMemberHealthFailures(srv *Server, memberID string, fails int) {
	srv.poller.mu.Lock()
	srv.poller.healthFailures[memberID] = fails
	srv.poller.mu.Unlock()
}

// setMemberVersion seeds the poller's last-polled app version for a member, so
// tests can align (or skew) the fleet against the auto-sync version gate
// without a live settings probe.
func setMemberVersion(srv *Server, memberID, version string) {
	srv.poller.mu.Lock()
	st := srv.poller.statuses[memberID]
	st.Version = version
	srv.poller.statuses[memberID] = st
	srv.poller.mu.Unlock()
}

// alignFleetVersions stamps every current member's polled app version to ver,
// so the auto-sync version gate sees an aligned fleet and the push paths under
// test are actually reached (the gate fails closed on unknown versions).
func alignFleetVersions(t *testing.T, srv *Server, store *Store, ver string) {
	t.Helper()
	members, err := store.ListMembers(t.Context())
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	for _, m := range members {
		setMemberVersion(srv, m.ID, ver)
	}
}

// TestAutoSyncQuietTickPingsHealthyMembers: on a converged fleet (primary hash
// unchanged) the loop writes nothing, but it must advance the "verified in sync"
// heartbeat for each reachable member so the Members table shows it is running.
// A member Front Desk cannot reach is left frozen, and last_config_sync_at never
// moves on a quiet tick.
func TestAutoSyncQuietTickPingsHealthyMembers(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-A" // matches LastHash below: nothing to propagate

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	up, _ := store.CreateMember(t.Context(), "up", "http://127.0.0.1:9", "utok")
	down, _ := store.CreateMember(t.Context(), "down", "http://127.0.0.1:10", "dtok")
	unknown, _ := store.CreateMember(t.Context(), "unknown", "http://127.0.0.1:11", "ktok")
	// "neverProbed" gets no poller entry at all, exercising the snapshot-miss path.
	neverProbed, _ := store.CreateMember(t.Context(), "never", "http://127.0.0.1:12", "ntok")
	// "grace" is inside the fail-threshold window: badge still healthy, but its
	// latest probe failed, so it must not be stamped as verified.
	grace, _ := store.CreateMember(t.Context(), "grace", "http://127.0.0.1:13", "gtok")
	enableAutoSync(t, store, pm.ID, "hash-A")
	setMemberHealth(srv, up.ID, true, true)
	setMemberHealth(srv, down.ID, true, false)
	setMemberHealth(srv, unknown.ID, false, false) // reachable status not yet known
	setMemberHealth(srv, grace.ID, true, true)
	setMemberHealthFailures(srv, grace.ID, 1) // one missed probe, still in grace window

	srv.autoSyncOnce(t.Context(), "hash-A") // hash == LastHash: quiet verify tick

	snap := srv.poller.Snapshot()
	if snap[up.ID].AutoSyncVerifiedAt == nil {
		t.Error("healthy member AutoSyncVerifiedAt = nil, want the quiet tick to ping it")
	}
	if snap[down.ID].AutoSyncVerifiedAt != nil {
		t.Error("unreachable member AutoSyncVerifiedAt was stamped; want it frozen")
	}
	if snap[unknown.ID].AutoSyncVerifiedAt != nil {
		t.Error("unknown-health member AutoSyncVerifiedAt was stamped; want it frozen until a health probe confirms it")
	}
	if snap[neverProbed.ID].AutoSyncVerifiedAt != nil {
		t.Error("never-probed member AutoSyncVerifiedAt was stamped; want it frozen with no health entry")
	}
	if snap[grace.ID].AutoSyncVerifiedAt != nil {
		t.Error("grace-window member AutoSyncVerifiedAt was stamped; want it frozen while a probe is failing")
	}
	if snap[pm.ID].AutoSyncVerifiedAt != nil {
		t.Error("primary AutoSyncVerifiedAt was stamped; the primary is the source, not a synced member")
	}
	// A quiet tick writes nothing to the DB.
	if rm, _ := store.GetMember(t.Context(), up.ID); rm.LastConfigSyncAt != nil {
		t.Error("quiet tick stamped last_config_sync_at; want it left for real writes only")
	}
}

// TestMarkFleetVerifiedListErrorIsSafe: if the member list cannot be read, the
// verify heartbeat pass logs and returns without panicking or stamping anything.
func TestMarkFleetVerifiedListErrorIsSafe(t *testing.T) {
	srv, store := newTestServer(t)
	m, _ := store.CreateMember(t.Context(), "m", "http://127.0.0.1:9", "tok")
	setMemberHealth(srv, m.ID, true, true)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // a cancelled context makes ListMembers error before returning rows

	srv.markFleetVerified(ctx, "")

	if snap := srv.poller.Snapshot(); snap[m.ID].AutoSyncVerifiedAt != nil {
		t.Error("heartbeat was stamped despite the member list read failing")
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
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")
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
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-B" {
		t.Errorf("applied hash = %q, want hash-B recorded after convergence", cfg.LastHash)
	}
}

// TestAutoSyncUnreachableMemberHoldsHash: a member whose import probe fails (its
// server is down) is left untouched and the applied hash is not recorded, so the
// next tick retries rather than declaring the fleet converged.
func TestAutoSyncUnreachableMemberHoldsHash(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	// A dead URL: the dry-run import is a transport failure, not an HTTP answer.
	store.CreateMember(t.Context(), "down", "http://127.0.0.1:9", "dtoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-A" {
		t.Errorf("applied hash = %q, want it held at hash-A so the next tick retries", cfg.LastHash)
	}
}

// TestAutoSyncSchemaBlockedMemberSkipped: a member that reports a schema or
// MASTER_KEY mismatch is held off (not overwritten) and the fleet is not marked
// converged.
func TestAutoSyncSchemaBlockedMemberSkipped(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	blocked := newStubAutoMember(t, "btoken")
	blocked.dryDiff = driftDiff
	blocked.importCode = http.StatusUnprocessableEntity // 422: schema mismatch
	blocked.importBody = `{"schema_version_ok":false,"master_key_ok":false}`

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "blocked", blocked.srv.URL, "btoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if blocked.didBackup() || blocked.didRealSync() {
		t.Error("a schema-blocked member must not be snapshotted or overwritten")
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-A" {
		t.Errorf("applied hash = %q, want it held at hash-A (member not syncable)", cfg.LastHash)
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

// TestAutoSyncNoChangeWhenHashUnchanged: when the primary's hash already equals
// the last applied hash, the loop short-circuits without touching any member.
func TestAutoSyncNoChangeWhenHashUnchanged(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-A"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")                             // last applied == current

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

// TestAutoSyncPrimaryVersionUnreadable: if the primary's version endpoint errors,
// the loop holds the applied hash and propagates nothing.
func TestAutoSyncPrimaryVersionUnreadable(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionCode = http.StatusInternalServerError

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	enableAutoSync(t, store, pm.ID, "hash-A")

	if got := srv.autoSyncOnce(t.Context(), ""); got != "" {
		t.Errorf("unreadable version returned %q, want empty", got)
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-A" {
		t.Errorf("applied hash = %q, want it held at hash-A", cfg.LastHash)
	}
}

// TestAutoSyncPrimaryExportUnreadable: a primary whose export fails at the apply
// stage leaves the fleet untouched and the hash unrecorded.
func TestAutoSyncPrimaryExportUnreadable(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	primary.exportCode = http.StatusInternalServerError
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")

	srv.autoSyncOnce(t.Context(), "hash-B") // settled: reach the apply stage

	if replica.didBackup() || replica.didRealSync() {
		t.Error("a member was touched despite the primary export failing")
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-A" {
		t.Errorf("applied hash = %q, want it held at hash-A", cfg.LastHash)
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
			if _, err := srv.fetchMemberConfigVersion(t.Context(), m, "tok"); err == nil {
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
	// Simulate the fleet having converged at hash-B while the replica was tokenless.
	seedAutoSyncHash(t, store, "hash-B")

	// Give the replica an admin token via the API. This must re-arm auto-sync.
	rec := do(t, srv, http.MethodPatch, "/api/members/"+rm.ID, `{"token":"rtoken"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch token = %d (%s)", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "" {
		t.Fatalf("applied hash = %q, want cleared so the new token triggers a sync", cfg.LastHash)
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
	seedAutoSyncHash(t, store, "hash-A")

	body := `{"name":"newcomer","url":"` + newcomer.srv.URL + `","token":"ntoken"}`
	rec := do(t, srv, http.MethodPost, "/api/members", body, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create member = %d (%s)", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "" {
		t.Errorf("applied hash = %q, want cleared after adding a tokened member", cfg.LastHash)
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

// TestAutoSyncTokenLoadFailureHoldsHash (Greptile P1): a member whose stored token
// ciphertext can't be decrypted (e.g. a MASTER_KEY mismatch) has HasToken true but
// fails MemberToken. It must not be recorded as converged, since nothing re-arms it
// later; the applied hash is held so the loop keeps retrying.
func TestAutoSyncTokenLoadFailureHoldsHash(t *testing.T) {
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
	enableAutoSync(t, store, pm.ID, "hash-A")

	srv.autoSyncOnce(t.Context(), "hash-B") // settled: reach the apply stage

	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-A" {
		t.Errorf("applied hash = %q, want held at hash-A (replica token could not be loaded)", cfg.LastHash)
	}
}

// TestSetAutoSyncClearsAppliedHash: changing the auto-sync setup resets the
// last-applied hash, so the next poll always runs a convergence pass.
func TestSetAutoSyncClearsAppliedHash(t *testing.T) {
	_, store := newTestServer(t)
	pm, _ := store.CreateMember(t.Context(), "primary", "http://127.0.0.1:9", "tok")
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	seedAutoSyncHash(t, store, "hash-X")
	// Re-applying the setup (re-enable, or any primary change) must clear the hash.
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "" {
		t.Errorf("LastHash = %q after re-applying setup, want cleared", cfg.LastHash)
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
	// Simulate a prior convergence at hash-B that is now stale (replica drifted).
	if err := store.SetAutoSync(t.Context(), true, pm.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	seedAutoSyncHash(t, store, "hash-B")

	// Operator re-applies the setup through the API; this must re-arm the loop.
	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync", `{"enabled":true,"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("put autosync = %d (%s)", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "" {
		t.Fatalf("LastHash = %q after re-enable, want cleared", cfg.LastHash)
	}

	// A settled pass now converges the drifted replica without the primary changing.
	alignFleetVersions(t, srv, store, "dev")
	prev := srv.autoSyncOnce(t.Context(), "")
	srv.autoSyncOnce(t.Context(), prev)
	if !replica.didRealSync() {
		t.Error("re-enabling auto-sync did not converge a replica that drifted while off")
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
	if _, err := store.RecordAutoSyncHash(ctx, "h", 0); err == nil {
		t.Error("RecordAutoSyncHash: want error on a closed DB")
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
	enableAutoSync(t, store, pm.ID, "hash-A")
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
	enableAutoSync(t, store, pm.ID, "hash-A")
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
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash == "hash-B" {
		t.Error("the applied hash must not be recorded when a reachable member refused as stale")
	}
	if sawSyncFailed(ch) {
		t.Error("a benign stale fence refusal must not emit a config.sync_failed event")
	}
}

// sawSyncFailed drains the bus channel and reports whether a config.sync_failed
// event was published.
func sawSyncFailed(ch chan events.Event) bool {
	for {
		select {
		case ev := <-ch:
			if ev.Type == "config.sync_failed" {
				return true
			}
		default:
			return false
		}
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
	enableAutoSync(t, store, pm.ID, "hash-A")
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")

	// Two held passes: the member is never pushed to, and the second pass must
	// not re-alert (edge-triggered hold).
	srv.forceAutoSyncNow(t.Context())
	srv.forceAutoSyncNow(t.Context())

	if replica.didBackup() || replica.didRealSync() {
		t.Fatal("version-skewed member was pushed to; want held")
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash == "hash-B" {
		t.Error("applied hash recorded despite a held member; want fleet not converged")
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
	enableAutoSync(t, store, pm.ID, "hash-A")
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
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash == "hash-B" {
		t.Error("applied hash recorded while a member is held for skew; want the fleet left unconverged")
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

	res := srv.applyMemberConfig(t.Context(), rm, "rtoken", []byte(fleetExportWithKey), "test", false, 0)

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
	if got.LastConfigSyncAt != nil {
		t.Error("last-sync marker was stamped on an incomplete apply; want left untouched")
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

	res := srv.applyMemberConfig(t.Context(), rm, "rtoken", []byte(fleetExportWithKey), "test", false, 0)

	if !res.OK {
		t.Fatalf("OK = false (%s), want true: an older member omits the field", res.Error)
	}
}

// TestAutoSync_IncompleteWithEmptyUnappliedHasSensibleMessage: when the member's
// whole group-build transaction fails (rather than skipping individual groups)
// it sends incomplete=true with unapplied absent. The error message must not
// degrade into "0 failover group(s)... could not be built here: " nonsense.
func TestAutoSync_IncompleteWithEmptyUnappliedHasSensibleMessage(t *testing.T) {
	srv, store := newTestServer(t)
	replica := newStubConfigMember(t, "rtoken")
	replica.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,"incomplete":true,"diff":{}}`
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	res := srv.applyMemberConfig(t.Context(), rm, "rtoken", []byte(fleetExportWithKey), "test", false, 0)

	if res.OK {
		t.Fatal("OK = true, want false: the member did not fully apply the config")
	}
	if res.Error == "" {
		t.Fatal("Error is empty, want a description of what was not built")
	}
	const want = "applied, but this member could not build its failover groups"
	if res.Error != want {
		t.Errorf("Error = %q, want %q", res.Error, want)
	}
}

// countEventsOfType returns how many stored events of the given type the fleet
// has, so an edge-triggered emission can be asserted across repeated passes.
func countEventsOfType(t *testing.T, store *Store, typ string) int {
	t.Helper()
	evs, _, err := store.ListEvents(t.Context(), EventFilter{Type: typ})
	if err != nil {
		t.Fatalf("ListEvents(%s): %v", typ, err)
	}
	return len(evs)
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

// incompleteFleet is a two-member fleet whose replica commits every import but
// never builds its custom failover group, so the fleet can never converge. The
// stubs count the real imports each pass costs.
type incompleteFleet struct {
	srv      *Server
	store    *Store
	primary  *stubAutoMember
	replica  *stubAutoMember
	replicaM *Member
}

func newIncompleteFleet(t *testing.T) *incompleteFleet {
	t.Helper()
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // presence-based diffs are never zero for a real member
	replica.incompleteImport = true

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")
	return &incompleteFleet{srv: srv, store: store, primary: primary, replica: replica, replicaM: rm}
}

// settledTick runs one convergence pass the way the loop does once the primary's
// hash has settled (prev equals the hash the primary reports), so the pass takes
// the converge path rather than the coalescing branch that clears retry timers.
func (f *incompleteFleet) settledTick(t *testing.T) {
	t.Helper()
	f.srv.autoSyncOnce(t.Context(), f.primary.versionHash)
}

// backdateIncompleteRetry moves a member's last incomplete retry attempt d into
// the past, so the next pass sees the rate-limit window as expired.
func backdateIncompleteRetry(t *testing.T, s *Server, memberID string, d time.Duration) {
	t.Helper()
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	st, ok := s.syncIncomplete[memberID]
	if !ok {
		t.Fatalf("member %s is not marked incomplete", memberID)
	}
	st.lastAttempt = st.lastAttempt.Add(-d)
	s.syncIncomplete[memberID] = st
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

	cfg, err := f.store.GetAutoSync(t.Context())
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}
	if cfg.LastHash == "hash-B" {
		t.Error("applied hash recorded on a rate-limited tick; a skipped retry must never count as converged")
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
	cfg, err := f.store.GetAutoSync(t.Context())
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}
	if cfg.LastHash == "hash-B" {
		t.Error("applied hash recorded while a member is incomplete; want the fleet left unconverged")
	}
	got, err := f.store.GetMember(t.Context(), f.replicaM.ID)
	if err != nil {
		t.Fatalf("GetMember: %v", err)
	}
	if got.LastConfigSyncAt != nil {
		t.Error("last-sync marker stamped on an incomplete apply; want left untouched")
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
	store.CreateMember(t.Context(), "healthy", healthy.srv.URL, "htoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")
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
	cfg, err := store.GetAutoSync(t.Context())
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}
	if cfg.LastHash == "hash-B" {
		t.Error("applied hash recorded while a member is incomplete; want the fleet left unconverged")
	}
	if !srv.incompleteSnapshot()[im.ID] {
		t.Error("incompleteSnapshot dropped the incomplete member; the fleet badge would clear")
	}
}

// hashFleet is a two-member fleet for the convergence-by-hash tests: a primary
// reporting hash-B, one replica the caller configures, and auto-sync enabled at a
// stale hash-A so every settled tick runs a convergence pass.
type hashFleet struct {
	srv      *Server
	store    *Store
	primary  *stubAutoMember
	replica  *stubAutoMember
	replicaM *Member
}

// newHashFleet builds that fleet, running setup on the replica stub before it is
// registered so the member is in its intended disposition from the first request.
func newHashFleet(t *testing.T, setup func(replica *stubAutoMember)) *hashFleet {
	t.Helper()
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	setup(replica)

	pm, err := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	rm, err := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	if err != nil {
		t.Fatalf("create replica: %v", err)
	}
	enableAutoSync(t, store, pm.ID, "hash-A")
	alignFleetVersions(t, srv, store, "dev")
	return &hashFleet{srv: srv, store: store, primary: primary, replica: replica, replicaM: rm}
}

// tick runs one settled convergence pass, the way the loop does once the primary's
// hash has stopped moving.
func (f *hashFleet) tick(t *testing.T) {
	t.Helper()
	f.srv.autoSyncOnce(t.Context(), "hash-B")
}

// lastHash reads the fleet's recorded applied hash, the marker that is written
// only once every reachable member has been verified against the primary.
func (f *hashFleet) lastHash(t *testing.T) string {
	t.Helper()
	cfg, err := f.store.GetAutoSync(t.Context())
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}
	return cfg.LastHash
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
	if got := f.lastHash(t); got == "hash-B" {
		t.Error("fleet hash recorded on the pushing pass; want it withheld until the member is verified")
	}

	f.tick(t)
	if got := f.replica.realSyncCount(); got != 1 {
		t.Errorf("real imports = %d after the verifying pass, want 1: a matching member is left alone", got)
	}
	if got := f.lastHash(t); got != "hash-B" {
		t.Errorf("fleet hash = %q, want hash-B once the member's own hash matched", got)
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
	if got := f.lastHash(t); got != "hash-B" {
		t.Errorf("fleet hash = %q, want hash-B recorded on the first pass; a probe-timeout read would have "+
			"left it unmeasured and unrecorded", got)
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
	if got := f.lastHash(t); got == "hash-B" {
		t.Error("fleet hash recorded while a member is diverged; want it withheld so the loop retries")
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
	if got := f.lastHash(t); got == "hash-B" {
		t.Error("fleet hash recorded for a member that never reported its apply; want it withheld")
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
	if got := f.lastHash(t); got == "hash-B" {
		t.Error("fleet hash recorded for a member that could not be verified; want it withheld")
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
		manualSyncReason("the dashboard"), true, 0); !res.OK {
		t.Fatalf("wizard sync OK = false (%s), want true", res.Error)
	}
	if res := srv.applyMemberConfig(t.Context(), am, "atoken", []byte(fleetExportWithKey),
		autoSyncReason, false, 0); !res.OK {
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
// member to converged. One that does not serve the primary's hash yet has nothing
// to write is left alone, holds the fleet unconverged, and does not get the
// "verified in sync" heartbeat, which now means the hash matched and nothing else.
func TestAutoSync_EmptyDiffDoesNotOverrideTheHash(t *testing.T) {
	f := newHashFleet(t, func(r *stubAutoMember) {
		r.versionHash = "hash-drifted"
		// The default dryDiff is empty: the presence-based diff sees nothing to write.
	})

	f.tick(t)

	if got := f.replica.dryRunCount(); got == 0 {
		t.Fatal("dry-runs = 0: a member whose hash differs must still be evaluated")
	}
	if got := f.replica.realSyncCount(); got != 0 {
		t.Errorf("real imports = %d, want 0: there is nothing to write", got)
	}
	if got := f.lastHash(t); got == "hash-B" {
		t.Error("fleet hash recorded for a member that does not serve it; the diff must not outvote the hash")
	}
	if snap := f.srv.poller.Snapshot(); snap[f.replicaM.ID].AutoSyncVerifiedAt != nil {
		t.Error("member stamped verified in sync while its hash differs")
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
	if got := f.lastHash(t); got != "hash-B" {
		t.Errorf("fleet hash = %q, want hash-B once the member matched", got)
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
	if got, want := string(meta), `{"partial":[],"unapplied":["ds4flash"]}`; got != want {
		t.Errorf("event metadata = %s, want %s", got, want)
	}
}

// partialImportBody is a member's answer to a real import that built every group
// it was sent but built one of them short: it resolved fewer entries for
// testgroup than the primary has. incomplete stays absent because nothing failed
// here; the member simply holds fewer models.
const partialImportBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,` +
	`"partial":["testgroup"],"diff":{}}`

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
	if got, want := string(meta), `{"partial":["testgroup"],"unapplied":[]}`; got != want {
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
	if got, want := string(meta), `{"partial":["testgroup"],"unapplied":["ds4flash"]}`; got != want {
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

	if got := f.lastHash(t); got == "hash-B" {
		t.Error("fleet hash recorded while a member holds a short failover group; want it withheld")
	}
	if !f.srv.incompleteSnapshot()[f.replicaM.ID] {
		t.Error("incompleteSnapshot does not hold the partially built member; the fleet badge would stay green")
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
	if got, want := string(meta), `{"partial":[],"unapplied":[]}`; got != want {
		t.Errorf("event metadata = %s, want %s", got, want)
	}
}

// divergenceCapNames returns n distinct group names built from prefix, numbered
// from 1, for exercising divergenceMessage's per-clause name cap. The numbering is
// deterministic so the expected message strings in the cap tests can be written as
// literals.
func divergenceCapNames(prefix string, n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("%s%d", prefix, i+1)
	}
	return names
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
		name               string
		unapplied, partial []string
		want               string
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
			name:      "both",
			unapplied: []string{"ds4flash"},
			partial:   []string{"testgroup"},
			want: "replica applied the config but could not build 1 failover group(s): ds4flash, " +
				"and built testgroup with fewer entries than the primary has",
		},
		{
			name: "neither",
			want: "replica applied the config but does not match the primary's config",
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
			if got := divergenceMessage("replica", tc.unapplied, tc.partial); got != tc.want {
				t.Errorf("divergenceMessage() = %q, want %q", got, tc.want)
			}
		})
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
	srv.recordSyncAttempt(m.ID, unapplied, partial)
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := autoSyncStaleTier(tc.cfg, tc.lastSync, tc.haveSync, now); got != tc.want {
				t.Errorf("tier = %d, want %d", got, tc.want)
			}
		})
	}
}
