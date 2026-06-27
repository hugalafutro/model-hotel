package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubConfigMember is a fake Model Hotel member exposing /api/config/export and
// /api/config/import with the Bearer-auth contract the real endpoints have. The
// import response is configurable so a test can model a normal replica, a
// MASTER_KEY mismatch (409), or an already-converged member (empty diff).
type stubConfigMember struct {
	token       string
	exportBody  string
	importCode  int
	importBody  string
	importDelay time.Duration // models a slow import (member-side discovery)
	backupCode  int           // status returned by POST /api/backups (0 -> 200)
	gotImport   bool
	gotDryRun   bool
	gotBackup   bool
	srv         *httptest.Server
}

func newStubConfigMember(t *testing.T, token string) *stubConfigMember {
	t.Helper()
	sm := &stubConfigMember{
		token:      token,
		exportBody: `{"schema_version":1,"app_version":"v-test","config":{"providers":[{"name":"openai","base_url":"https://o"}]}}`,
		importCode: http.StatusOK,
		importBody: `{"schema_version_ok":true,"master_key_ok":true,"applied":true,"diff":{"providers":{"added":["openai"]},"virtual_keys":{},"settings":{}}}`,
	}
	sm.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+sm.token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/config/export":
			_, _ = w.Write([]byte(sm.exportBody))
		case r.Method == http.MethodPost && r.URL.Path == "/api/config/import":
			sm.gotImport = true
			sm.gotDryRun = r.URL.Query().Get("dryRun") != ""
			if sm.importDelay > 0 {
				time.Sleep(sm.importDelay)
			}
			w.WriteHeader(sm.importCode)
			_, _ = w.Write([]byte(sm.importBody))
		case r.Method == http.MethodPost && r.URL.Path == "/api/backups":
			sm.gotBackup = true
			code := sm.backupCode
			if code == 0 {
				code = http.StatusOK
			}
			w.WriteHeader(code)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

func TestConfigSyncApplies(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	body := `{"primary_id":"` + pm.ID + `"}`
	rec := do(t, srv, http.MethodPost, "/api/config/sync", body, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].MemberID != rm.ID || !resp.Results[0].OK {
		t.Fatalf("results = %+v", resp.Results)
	}
	if !replica.gotImport || replica.gotDryRun {
		t.Errorf("replica import: got=%v dryRun=%v (want applied, not dry run)", replica.gotImport, replica.gotDryRun)
	}
	if !replica.gotBackup {
		t.Error("a changing replica must be snapshotted before the destructive import")
	}
	if primary.gotImport || primary.gotBackup {
		t.Error("primary must not be imported into or backed up (it is the source)")
	}

	// A config.synced event was recorded.
	evs, _, err := store.ListEvents(t.Context(), EventFilter{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var sawSynced bool
	for _, e := range evs {
		if e.Type == "config.synced" && e.MemberID == rm.ID {
			sawSynced = true
		}
	}
	if !sawSynced {
		t.Error("expected a config.synced event for the replica")
	}
}

// A slow import (the member runs model discovery on apply, which routinely
// exceeds the fast health-probe timeout) must still be reported as applied: the
// import relay uses a separate client with a far longer deadline. Here the probe
// client is given a deadline shorter than the replica's import delay; a
// successful result proves the relay did NOT route the import through the probe
// client (export is instant, so the short probe is fine for it).
func TestConfigSyncImportUsesLongerDeadlineThanProbe(t *testing.T) {
	srv, store := newTestServer(t)
	srv.probe = newProbeClient(50 * time.Millisecond)
	srv.syncClient = newProbeClient(3 * time.Second)

	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")
	replica.importDelay = 200 * time.Millisecond

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || !resp.Results[0].OK {
		t.Fatalf("slow import must be reported applied (relay must use the long-deadline client), got %+v", resp.Results)
	}
}

func TestConfigSyncReportsFailure(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")
	replica.importCode = http.StatusConflict
	replica.importBody = `{"schema_version_ok":true,"master_key_ok":false}`

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d", rec.Code)
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].OK || resp.Results[0].Error == "" {
		t.Fatalf("expected a failure result, got %+v", resp.Results)
	}
	evs, _, _ := store.ListEvents(t.Context(), EventFilter{})
	var sawFail bool
	for _, e := range evs {
		if e.Type == "config.sync_failed" && e.MemberID == rm.ID {
			sawFail = true
		}
	}
	if !sawFail {
		t.Error("expected a config.sync_failed event")
	}
}

// A member whose pre-sync backup fails must be left untouched and reported, never
// overwritten: the wizard now gives the same recoverability guarantee as the
// auto-syncer.
func TestConfigSyncBackupFailureSkipsMember(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")
	replica.backupCode = http.StatusInternalServerError // the snapshot fails

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d", rec.Code)
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].OK || !strings.Contains(resp.Results[0].Error, "backup") {
		t.Fatalf("want a backup-failure result, got %+v", resp.Results)
	}
	if !replica.gotBackup {
		t.Error("a backup should have been attempted before the overwrite")
	}
	// The destructive (non-dry-run) import must never run: the last import call was
	// the gating dry-run, so gotDryRun stays true.
	if !replica.gotDryRun {
		t.Error("the destructive import must be skipped when the backup fails")
	}
	evs, _, _ := store.ListEvents(t.Context(), EventFilter{})
	for _, e := range evs {
		if e.Type == "config.synced" && e.MemberID == rm.ID {
			t.Error("a member left unchanged must not emit config.synced")
		}
	}
}

// An already-converged member is not snapshotted: there is nothing to overwrite, so
// the wizard skips the backup just as the auto-syncer does, avoiding backup spam.
func TestConfigSyncConvergedMemberNotBackedUp(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	converged := newStubConfigMember(t, "ctoken")
	converged.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":true,"diff":{}}`

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "converged", converged.srv.URL, "ctoken")

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d", rec.Code)
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || !resp.Results[0].OK {
		t.Fatalf("converged member should report OK, got %+v", resp.Results)
	}
	if converged.gotBackup {
		t.Error("an already-converged member must not be snapshotted")
	}
}

func TestConfigSyncUnknownPrimary(t *testing.T) {
	srv, _ := newTestServer(t)
	const missing = "00000000-0000-0000-0000-000000000000"
	if rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+missing+`"}`, true); rec.Code < 400 {
		t.Fatalf("sync unknown primary should error, got %d", rec.Code)
	}
}

func TestConfigSyncPrimaryExportNon200(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	// Reachable, but the export endpoint errors (e.g. 500): distinct from the
	// transport failure in TestConfigSyncPrimaryExportFails.
	primary.exportBody = ""
	primary.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ptoken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	if rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true); rec.Code != http.StatusBadGateway {
		t.Fatalf("sync non-200 export = %d, want 502", rec.Code)
	}
}

func TestConfigSyncReplicaBadJSON(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	// Replica returns 200 with a non-JSON body: pushMemberImport's parse error
	// is treated as "could not reach this member".
	bad := newStubConfigMember(t, "btoken")
	bad.importBody = "not json"
	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	store.CreateMember(t.Context(), "bad-json", bad.srv.URL, "btoken")

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d", rec.Code)
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].OK || resp.Results[0].Error == "" {
		t.Fatalf("bad-json replica should be a reported failure: %+v", resp.Results)
	}
}

func TestConfigSyncPrimaryExportFails(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	// The primary cannot serve its export.
	primary.srv.Close()
	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")

	// Sync surfaces a bad-gateway when the primary export fails.
	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("sync export-fail = %d, want 502", rec.Code)
	}
}

func TestConfigSyncApplyVariants(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	// applied=false despite a 200 -> "did not apply".
	notApplied := newStubConfigMember(t, "ntoken")
	notApplied.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":false,"diff":{}}`
	// 422 schema mismatch -> "version mismatch", NOT a MASTER_KEY message.
	badSchema := newStubConfigMember(t, "stoken")
	badSchema.importCode = http.StatusUnprocessableEntity
	badSchema.importBody = `{"schema_version_ok":false,"master_key_ok":false}`
	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	nm, _ := store.CreateMember(t.Context(), "not-applied", notApplied.srv.URL, "ntoken")
	bm, _ := store.CreateMember(t.Context(), "bad-schema", badSchema.srv.URL, "stoken")
	store.CreateMember(t.Context(), "unreachable", "http://127.0.0.1:1", "utoken")

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d", rec.Code)
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 3 {
		t.Fatalf("want 3 results, got %+v", resp.Results)
	}
	byID := map[string]syncResultItem{}
	for _, r := range resp.Results {
		if r.OK || r.Error == "" {
			t.Errorf("result should be a reported failure: %+v", r)
		}
		byID[r.MemberID] = r
	}
	if !strings.Contains(byID[bm.ID].Error, "version") {
		t.Errorf("bad-schema error = %q, want version mismatch", byID[bm.ID].Error)
	}
	if !strings.Contains(byID[nm.ID].Error, "did not apply") {
		t.Errorf("not-applied error = %q", byID[nm.ID].Error)
	}
}
