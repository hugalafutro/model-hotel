package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// stubAutoMember plays a member for the auto-sync loop: it answers the config
// version GET (the drift signal), the export GET, the dry-run and real config
// imports, and the pre-sync backup POST. Each is independently configurable so a
// single stub can be a primary or a replica in any disposition.
type stubAutoMember struct {
	mu          sync.Mutex
	srv         *httptest.Server
	token       string
	versionHash string
	exportBody  string
	dryDiff     string // diff object returned on a dry-run import
	importCode  int    // status for the dry-run import (default 200)
	importBody  string // full dry-run import body; overrides dryDiff when set
	backupCode  int
	gotBackup   bool
	gotRealSync bool
}

func newStubAutoMember(t *testing.T, token string) *stubAutoMember {
	t.Helper()
	sm := &stubAutoMember{
		token:       token,
		versionHash: "hash-A",
		exportBody:  fleetExportWithKey,
		dryDiff:     `{"providers":{},"virtual_keys":{},"settings":{}}`, // converged
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
			_ = json.NewEncoder(w).Encode(map[string]string{"version": sm.versionHash})
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/export":
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
			sm.gotRealSync = true
			_, _ = w.Write([]byte(`{"schema_version_ok":true,"master_key_ok":true,"applied":true,"diff":` + sm.dryDiff + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/backups":
			sm.gotBackup = true
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

const driftDiff = `{"providers":{"added":["anthropic"]},"virtual_keys":{},"settings":{}}`

// enableAutoSync points auto-sync at primaryID with a stale last-applied hash, so
// the loop sees the primary as changed.
func enableAutoSync(t *testing.T, store *Store, primaryID, lastHash string) {
	t.Helper()
	if err := store.SetAutoSync(t.Context(), true, primaryID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	if err := store.SetAutoSyncLastHash(t.Context(), lastHash); err != nil {
		t.Fatalf("SetAutoSyncLastHash: %v", err)
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
