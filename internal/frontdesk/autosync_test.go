package frontdesk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	// versionSections, when set, rides in the version response as the per-section
	// hash map a current member serves; nil models an older member whose response
	// carries only the overall hash.
	versionSections map[string]string
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
	// realImportCode, when set, is the status the real import answers with even
	// though the member applies the config anyway: what a reverse proxy answering
	// 502/504 mid-import looks like from Front Desk, with the member finishing
	// the apply behind it.
	realImportCode int
	gotBackup      bool
	backups        int // how many backups this member was asked to take; must stay 0
	dryRuns        int // how many dry-run imports this member was asked for
	// versionReads counts the config-version GETs this member answered. It is the
	// direct evidence of whether a pass measured this member at all, which is what
	// separates a convergence pass from a tick that skipped the fleet entirely.
	versionReads int
	// exports counts the config-export GETs this member answered. On the primary
	// it is the cost a settled fleet does not pay: the export is read only once a
	// member is found that needs it.
	exports      int
	gotRealSync  bool
	realSyncs    int    // how many real (non-dry-run) imports this member accepted
	gotSourceGen string // X-Fleet-Source-Gen seen on the last real (non-dry-run) import
	staleImport  bool   // when true, the real import answers with the commit-fence "stale" response
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
			sm.versionReads++
			if sm.versionDelay > 0 {
				time.Sleep(sm.versionDelay)
			}
			w.WriteHeader(sm.versionCode)
			if sm.versionRaw != "" {
				_, _ = w.Write([]byte(sm.versionRaw))
				return
			}
			if sm.versionSections != nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"version": sm.versionHash, "sections": sm.versionSections})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"version": sm.versionHash})
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/export":
			sm.exports++
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
			if sm.realImportCode != 0 {
				// The apply above still happened; only the answer is lost.
				w.WriteHeader(sm.realImportCode)
				return
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

func (sm *stubAutoMember) exportCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.exports
}

func (sm *stubAutoMember) versionReadCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.versionReads
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

// setAppliedHash changes what the member ends up holding after an import. Clearing
// it between passes turns a member that adopted the primary's config into one that
// commits every import and never matches.
func (sm *stubAutoMember) setAppliedHash(h string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.appliedHash = h
}

const driftDiff = `{"providers":{"added":["anthropic"]},"virtual_keys":{},"settings":{}}`

// enableAutoSync turns auto-sync on and points it at primaryID.
func enableAutoSync(t *testing.T, store *Store, primaryID string) {
	t.Helper()
	if err := store.SetAutoSync(t.Context(), true, primaryID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
}

// memberVerified reports whether a pass has measured this member holding the
// primary's config. It reads the verified-in-sync heartbeat, which only a hash
// match moves, so it is the observable that says "this member was measured as
// converged" rather than "this member was written to".
func memberVerified(s *Server, memberID string) bool {
	return s.poller.Snapshot()[memberID].AutoSyncVerifiedAt != nil
}

// memberDiverged reports whether the member is currently flagged as not holding
// the primary's config: the state the amber fleet badge and the
// config.sync_incomplete alert read.
func memberDiverged(s *Server, memberID string) bool {
	s.syncIncompleteMu.Lock()
	defer s.syncIncompleteMu.Unlock()
	return s.syncIncomplete[memberID].diverged
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

// setMemberBuild seeds both halves of a member's polled build, for the fleet the
// version alone cannot describe: every self-built image reports "dev", so the
// commit is the only field that distinguishes one build from another.
func setMemberBuild(srv *Server, memberID, version, commit string) {
	srv.poller.mu.Lock()
	st := srv.poller.statuses[memberID]
	st.Version = version
	st.Commit = commit
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

	srv.syncHeld[gone.ID] = true
	srv.syncIncomplete[gone.ID] = incompleteState{diverged: true, lastAttempt: time.Now()}
	srv.backupStale[gone.ID] = true

	rec := do(t, srv, http.MethodDelete, "/api/members/"+gone.ID, "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete member = %d (%s)", rec.Code, rec.Body.String())
	}

	if srv.syncHeld[gone.ID] {
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
	srv.syncHeld = make(map[string]bool)
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
	srv.syncHeld = make(map[string]bool)
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
	srv.syncHeld[m.ID] = true
	srv.holdLogChecked[m.ID] = true
	srv.syncHeldMu.Unlock()

	if err := store.db.Close(); err != nil {
		t.Fatalf("close store db: %v", err)
	}
	srv.closeSyncHold(t.Context(), m, "m1 is no longer held for sync: its app version matches the primary's again")

	srv.syncHeldMu.Lock()
	checked := srv.holdLogChecked[m.ID]
	held := srv.syncHeld[m.ID]
	srv.syncHeldMu.Unlock()
	if checked {
		t.Error("a failed config.sync_recovered persist stayed memoised as closed; the next pass must re-read the log and retry")
	}
	if held {
		t.Error("the in-memory hold must stay cleared even when the closing event fails to persist")
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

// verified reports whether a pass has measured the replica holding the primary's
// config, the per-member record of convergence.
func (f *incompleteFleet) verified() bool {
	return memberVerified(f.srv, f.replicaM.ID)
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
	enableAutoSync(t, store, pm.ID)
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
	store.CreateMember(t.Context(), "healthy", healthy.srv.URL, "htoken") //nolint:errcheck // presence is the point
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
	enableAutoSync(t, store, pm.ID)
	alignFleetVersions(t, srv, store, "dev")
	return &hashFleet{srv: srv, store: store, primary: primary, replica: replica, replicaM: rm}
}

// tick runs one settled convergence pass, the way the loop does once the primary's
// hash has stopped moving.
func (f *hashFleet) tick(t *testing.T) {
	t.Helper()
	f.srv.autoSyncOnce(t.Context(), "hash-B")
}

// verified reports whether a pass has measured the replica holding the primary's
// config. It reads the verified-in-sync heartbeat, which only a hash match moves,
// and is the per-member record that replaced the fleet-wide applied-hash marker.
func (f *hashFleet) verified() bool {
	return memberVerified(f.srv, f.replicaM.ID)
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
	if got, want := string(meta), `{"partial":["testgroup"],"unapplied":[],"unapplied_models":[],"unreadable":false}`; got != want {
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

// seedUnreadableSince backdates a member's unreadable-hash clock, so a test can
// reach the far side of unreadableHashThreshold without waiting it out. It seeds
// the same field the production path sets on its first failed read.
func seedUnreadableSince(srv *Server, memberID string, since time.Time) {
	srv.syncIncompleteMu.Lock()
	defer srv.syncIncompleteMu.Unlock()
	st := srv.syncIncomplete[memberID]
	st.unreadableSince = since
	srv.syncIncomplete[memberID] = st
}

// unreadableSince reads a member's unreadable-hash clock.
func unreadableSince(srv *Server, memberID string) time.Time {
	srv.syncIncompleteMu.Lock()
	defer srv.syncIncompleteMu.Unlock()
	return srv.syncIncomplete[memberID].unreadableSince
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
