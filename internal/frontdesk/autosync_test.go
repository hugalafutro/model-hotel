package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/events"
)

// stubAutoMember plays a member for the auto-sync loop: it answers the config
// version GET (the drift signal), the export GET, the dry-run and real config
// imports, and the pre-sync backup POST. Each is independently configurable so a
// single stub can be a primary or a replica in any disposition.
type stubAutoMember struct {
	mu           sync.Mutex
	srv          *httptest.Server
	token        string
	versionHash  string
	versionCode  int    // status for the version GET (default 200)
	versionRaw   string // raw version body; overrides the {"version":...} JSON when set
	exportBody   string
	exportCode   int    // status for the export GET (default 200)
	dryDiff      string // diff object returned on a dry-run import
	importCode   int    // status for the dry-run import (default 200)
	importBody   string // full dry-run import body; overrides dryDiff when set
	backupCode   int
	gotBackup    bool
	gotRealSync  bool
	gotSourceGen string // X-Fleet-Source-Gen seen on the last real (non-dry-run) import
	staleImport  bool   // when true, the real import answers with the commit-fence "stale" response
	onBackup     func() // fired inside the backup handler, to simulate a rearm landing mid-pass
	// onImport fires inside the real (non-dry-run) import handler. It receives the
	// request context and returns whether the import should be recorded as applied;
	// returning false models the import being cancelled in flight before it commits.
	onImport func(reqCtx context.Context) (commit bool)
}

func newStubAutoMember(t *testing.T, token string) *stubAutoMember {
	t.Helper()
	sm := &stubAutoMember{
		token:       token,
		versionHash: "hash-A",
		exportBody:  fleetExportWithKey,
		dryDiff:     `{"providers":{},"virtual_keys":{},"settings":{}}`, // converged
		versionCode: http.StatusOK,
		exportCode:  http.StatusOK,
		importCode:  http.StatusOK,
		backupCode:  http.StatusCreated,
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
			_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":true,"diff":` + sm.dryDiff + `}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/settings":
			// The token probe (createMember/patchMember) hits this; 200 = accepted.
			_, _ = w.Write([]byte(`{}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/backups":
			sm.gotBackup = true
			if sm.onBackup != nil {
				sm.onBackup() // simulate a rearm/repoint landing after the backup, before the import
			}
			w.WriteHeader(sm.backupCode)
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
func (sm *stubAutoMember) sourceGen() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.gotSourceGen
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
// the next tick does Front Desk propagate it, backing each changed member up
// first and stamping its last-sync marker.
func TestAutoSyncCoalescesThenApplies(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // this member needs the new config

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID, "hash-A")

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
	if !replica.didBackup() {
		t.Error("replica was not backed up before the auto-sync")
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
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-B" {
		t.Errorf("applied hash = %q, want hash-B recorded after convergence", cfg.LastHash)
	}
}

// TestForceAutoSyncNowConvergesImmediately: the enable-time kick converges a
// drifted fleet in a single pass, with no coalescing wait, and stamps the
// member's last-sync marker with the "auto-sync was enabled" reason.
func TestForceAutoSyncNowConvergesImmediately(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B" // changed vs the recorded last hash
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // this member needs the new config

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID, "hash-A")

	// Single call, no prior tick: the kick must act at once.
	srv.forceAutoSyncNow(t.Context())

	if !replica.didBackup() {
		t.Error("replica was not backed up before the kick sync")
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
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-B" {
		t.Errorf("applied hash = %q, want hash-B recorded after convergence", cfg.LastHash)
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

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID, "hash-A")

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

	// A pass at the current generation records normally.
	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", autoSyncReason, got.Gen)
	got, _ = store.GetAutoSync(t.Context())
	if got.LastHash != "hash-B" {
		t.Errorf("current-gen record = %q, want hash-B", got.LastHash)
	}
}

// TestConvergeFleetAbortsImportWhenRearmLandsAfterBackup: the tightest race. A
// rearm/repoint lands after a member's pre-sync backup is taken but before its
// import runs. The final staleness gate must catch it: the member is snapshotted
// (harmless) but NOT overwritten with the now-stale export, and the hash is not
// recorded, so the rearm's own pass converges it with the fresh primary.
func TestConvergeFleetAbortsImportWhenRearmLandsAfterBackup(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff // needs the new config, so it reaches the backup+import path

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	enableAutoSync(t, store, pm.ID, "hash-A")

	cfg, _ := store.GetAutoSync(t.Context())
	staleGen := cfg.Gen
	// The rearm fires the instant the member's pre-sync backup is taken, opening the
	// post-backup/pre-import window the final gate exists to close.
	replica.onBackup = func() {
		if err := store.RearmAutoSync(t.Context()); err != nil {
			t.Errorf("RearmAutoSync: %v", err)
		}
	}

	srv.convergeFleet(t.Context(), pm, "ptoken", "hash-B", autoSyncReason, staleGen)

	if !replica.didBackup() {
		t.Fatal("test setup: backup never ran, so the post-backup window was not exercised")
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

// TestAutoSyncSkipsConvergedMember: a member whose dry-run diff is empty is left
// untouched (no backup, no import), but the fleet still counts as converged so the
// new hash is recorded and the loop quiesces.
func TestAutoSyncSkipsConvergedMember(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken") // default dryDiff is empty (converged)

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	// A tokenless member is present too: it must be skipped without blocking the
	// fleet from being recorded as converged.
	store.CreateMember(t.Context(), "tokenless", "http://127.0.0.1:9", "") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")

	srv.autoSyncOnce(t.Context(), "hash-B") // already settled: act this tick

	if replica.didBackup() || replica.didRealSync() {
		t.Error("a converged member must not be backed up or re-imported")
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-B" {
		t.Errorf("applied hash = %q, want hash-B (fleet converged)", cfg.LastHash)
	}
}

// TestAutoSyncBackupFailureSkipsMember: if a member's pre-sync backup fails, its
// config is NOT overwritten, the fleet is not marked converged, and the applied
// hash is left stale so the next tick retries.
func TestAutoSyncBackupFailureSkipsMember(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubAutoMember(t, "ptoken")
	primary.versionHash = "hash-B"
	replica := newStubAutoMember(t, "rtoken")
	replica.dryDiff = driftDiff
	replica.backupCode = http.StatusInternalServerError // backup fails

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken") //nolint:errcheck // presence is the point
	enableAutoSync(t, store, pm.ID, "hash-A")

	srv.autoSyncOnce(t.Context(), "hash-B")

	if replica.didRealSync() {
		t.Error("member was overwritten despite a failed pre-sync backup")
	}
	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-A" {
		t.Errorf("applied hash = %q, want it left at hash-A so the next tick retries", cfg.LastHash)
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

	srv.autoSyncOnce(t.Context(), "hash-B")

	cfg, _ := store.GetAutoSync(t.Context())
	if cfg.LastHash != "hash-A" {
		t.Errorf("applied hash = %q, want it held at hash-A so the next tick retries", cfg.LastHash)
	}
}

// TestAutoSyncSchemaBlockedMemberSkipped: a member that reports a schema or
// MASTER_KEY mismatch is held off (not backed up, not overwritten) and the fleet
// is not marked converged.
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

	srv.autoSyncOnce(t.Context(), "hash-B")

	if blocked.didBackup() || blocked.didRealSync() {
		t.Error("a schema-blocked member must not be backed up or overwritten")
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

	ch := srv.bus.Subscribe()
	defer srv.bus.Unsubscribe(ch)

	srv.forceAutoSyncNow(t.Context())

	// The dry-run said it needed the config, so it is snapshotted before the (then
	// refused) import: the backup is a harmless recoverable snapshot.
	if !replica.didBackup() {
		t.Error("replica should still be snapshotted before the refused import")
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
