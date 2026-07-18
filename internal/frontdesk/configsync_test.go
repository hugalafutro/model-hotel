package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	alignFleetVersions(t, srv, store, "dev")

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

// TestConfigSyncAttributesInitiator proves a manual sync stamps who ran it on
// both the member's sync-reason marker (surfaced in the Members table / member
// detail) and the audit event's metadata, so the log can tell an admin-driven
// run from a phone-driven one. An admin bearer carries no paired device, so it
// is attributed to the dashboard.
func TestConfigSyncAttributesInitiator(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	alignFleetVersions(t, srv, store, "dev")

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d (%s)", rec.Code, rec.Body.String())
	}

	want := manualSyncReason("the dashboard")

	got, err := store.GetMember(t.Context(), rm.ID)
	if err != nil {
		t.Fatalf("get member: %v", err)
	}
	if got.LastConfigSyncReason != want {
		t.Errorf("member sync reason = %q, want %q", got.LastConfigSyncReason, want)
	}

	evs, _, err := store.ListEvents(t.Context(), EventFilter{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var eventReason any
	for _, e := range evs {
		if e.Type == "config.synced" && e.MemberID == rm.ID {
			eventReason = e.Metadata["reason"]
		}
	}
	if eventReason != want {
		t.Errorf("config.synced reason metadata = %v, want %q", eventReason, want)
	}
}

// TestActorFromContext covers the three attribution branches: no device (admin
// or dashboard session), a labelled device, and the defensive blank-label path.
func TestActorFromContext(t *testing.T) {
	if got := actorFromContext(t.Context()); got != "the dashboard" {
		t.Errorf("no-device actor = %q, want %q", got, "the dashboard")
	}

	withDevice := context.WithValue(t.Context(), deviceCtxKey{}, &PairedDevice{Label: "Pixel", Role: RoleOperator})
	if got := actorFromContext(withDevice); got != "Pixel (operator)" {
		t.Errorf("device actor = %q, want %q", got, "Pixel (operator)")
	}

	blankLabel := context.WithValue(t.Context(), deviceCtxKey{}, &PairedDevice{Role: RoleMonitor})
	if got := actorFromContext(blankLabel); got != "a paired device (monitor)" {
		t.Errorf("blank-label actor = %q, want %q", got, "a paired device (monitor)")
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
	alignFleetVersions(t, srv, store, "dev")

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
	alignFleetVersions(t, srv, store, "dev")

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
	alignFleetVersions(t, srv, store, "dev")

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
	var failReason any
	for _, e := range evs {
		if e.Type == "config.synced" && e.MemberID == rm.ID {
			t.Error("a member left unchanged must not emit config.synced")
		}
		if e.Type == "config.sync_failed" && e.MemberID == rm.ID {
			failReason = e.Metadata["reason"]
		}
	}
	// The backup-failure skip is attributed like every other sync outcome, so the
	// log distinguishes who triggered the run that could not back up.
	if want := manualSyncReason("the dashboard"); failReason != want {
		t.Errorf("config.sync_failed reason metadata = %v, want %q", failReason, want)
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
	cm, _ := store.CreateMember(t.Context(), "converged", converged.srv.URL, "ctoken")
	alignFleetVersions(t, srv, store, "dev")

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
	// A converged member must not get a real import either: re-importing reopens
	// the overwrite-without-backup window. Its only import call is the gating
	// dry-run, so gotDryRun stays true.
	if !converged.gotDryRun {
		t.Error("a converged member must not be imported into; only the dry-run should run")
	}
	// Nothing was written, so the persisted last_config_sync_at must stay unset
	// (that column means a real config write). The wizard instead advances the
	// live "verified in sync" heartbeat so the Members table shows it confirmed
	// this member against the primary.
	m, err := store.GetMember(t.Context(), cm.ID)
	if err != nil {
		t.Fatalf("get converged member: %v", err)
	}
	if m.LastConfigSyncAt != nil {
		t.Error("converged member LastConfigSyncAt was stamped; want untouched (no write happened)")
	}
	if snap := srv.poller.Snapshot(); snap[cm.ID].AutoSyncVerifiedAt == nil {
		t.Error("converged member AutoSyncVerifiedAt = nil, want the verify heartbeat stamped")
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
	alignFleetVersions(t, srv, store, "dev")

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
	// applied=false despite a 200 -> "did not apply". A non-empty diff so the
	// pre-sync gate sees a changing member and proceeds to the real import (an
	// empty diff would be short-circuited as already-converged).
	notApplied := newStubConfigMember(t, "ntoken")
	notApplied.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":false,"diff":{"providers":{"added":["p"]}}}`
	// 422 schema mismatch -> "version mismatch", NOT a MASTER_KEY message.
	badSchema := newStubConfigMember(t, "stoken")
	badSchema.importCode = http.StatusUnprocessableEntity
	badSchema.importBody = `{"schema_version_ok":false,"master_key_ok":false}`
	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	nm, _ := store.CreateMember(t.Context(), "not-applied", notApplied.srv.URL, "ntoken")
	bm, _ := store.CreateMember(t.Context(), "bad-schema", badSchema.srv.URL, "stoken")
	um, _ := store.CreateMember(t.Context(), "unreachable", "http://127.0.0.1:1", "utoken")
	alignFleetVersions(t, srv, store, "dev")

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
	// An unreachable member fails the pre-sync dry-run, so the backup is never
	// attempted: its error must report the unreachability, not be mislabeled a
	// "backup failed" skip.
	if got := byID[um.ID].Error; !strings.Contains(got, "reach") || strings.Contains(got, "backup") {
		t.Errorf("unreachable error = %q, want a reach failure and not a backup failure", got)
	}
}

// configSyncCount scrapes /metrics and returns the current value of the
// frontdesk_config_sync_total counter for the given result label (0 if the
// series has not been emitted yet).
func configSyncCount(t *testing.T, srv *Server, result string) float64 {
	t.Helper()
	prefix := `frontdesk_config_sync_total{result="` + result + `"} `
	body := scrape(t, srv, testFrontdeskToken).Body.String()
	for line := range strings.SplitSeq(body, "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			if err != nil {
				t.Fatalf("parse %q: %v", line, err)
			}
			return v
		}
	}
	return 0
}

// TestConfigSyncStampFailureFailsResult: when a member applies the config but the
// durable last-sync stamp write fails, the whole result must fail, not just the
// metric label. A premature success would let the wizard report the member synced
// and auto-sync mark it converged while the store/UI still show it unsynced, so it
// would never be retried. The result flips to not-OK with an error, the counter
// records "err" (never "ok"), and a config.sync_failed event is emitted.
func TestConfigSyncStampFailureFailsResult(t *testing.T) {
	srv, store := newTestServer(t)
	replica := newStubConfigMember(t, "rtoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")

	// Delete the row so the in-memory member still resolves (URL, ID) for the
	// pure-HTTP apply, but SetMemberLastSync affects zero rows and errors. The DB
	// stays open so /metrics can be scraped for the counter values.
	if err := store.DeleteMember(t.Context(), rm.ID); err != nil {
		t.Fatalf("delete member: %v", err)
	}

	okBefore := configSyncCount(t, srv, "ok")
	errBefore := configSyncCount(t, srv, "err")

	res := srv.applyMemberConfig(t.Context(), rm, "rtoken", []byte(fleetExportWithKey), "test", true, 1)
	if res.OK || res.Error == "" {
		t.Fatalf("stamp failure must fail the result, got OK=%v err=%q", res.OK, res.Error)
	}

	if moved := configSyncCount(t, srv, "ok") - okBefore; moved != 0 {
		t.Errorf("ok counter moved by %v on a failed stamp, want 0", moved)
	}
	if moved := configSyncCount(t, srv, "err") - errBefore; moved != 1 {
		t.Errorf("err counter moved by %v, want 1", moved)
	}

	evs, _, err := store.ListEvents(t.Context(), EventFilter{})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	var sawFail, sawSynced bool
	for _, e := range evs {
		switch e.Type {
		case "config.sync_failed":
			sawFail = true
		case "config.synced":
			sawSynced = true
		}
	}
	if !sawFail {
		t.Error("expected a config.sync_failed event on stamp failure")
	}
	if sawSynced {
		t.Error("a config.synced event must not fire when the stamp failed")
	}
}

// A client that hangs up mid-run (Bellhop's HTTP timeout, an impatient reverse
// proxy) must not abort a sync in flight: the run is detached from the request
// context, so the import still applies, the config.synced event is recorded and
// the member's last-sync stamp moves. Without the detach, the cancel aborted
// all three, leaving no trace of the run at all. The primary's export stub
// cancels the request context to simulate the disconnect at the earliest
// mid-run moment, so everything after it runs under a cancelled parent.
func TestConfigSyncSurvivesClientDisconnect(t *testing.T) {
	srv, store := newTestServer(t)
	replica := newStubConfigMember(t, "rtoken")

	ctx, cancel := context.WithCancel(t.Context())
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ptoken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet && r.URL.Path == "/api/config/export" {
			cancel() // the requester is gone; the sync must keep going
			_, _ = w.Write([]byte(`{"schema_version":1,"app_version":"v-test","config":{"providers":[{"name":"openai","base_url":"https://o"}]}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(primary.Close)

	pm, _ := store.CreateMember(t.Context(), "primary", primary.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	alignFleetVersions(t, srv, store, "dev")

	req := httptest.NewRequest(http.MethodPost, "/api/config/sync", strings.NewReader(`{"primary_id":"`+pm.ID+`"}`)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d (%s), want 200 despite the disconnect", rec.Code, rec.Body.String())
	}
	if !replica.gotImport {
		t.Fatal("the replica import must run to completion after the client disconnected")
	}
	members, err := store.ListMembers(t.Context())
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	for _, m := range members {
		if m.ID == rm.ID && m.LastConfigSyncAt == nil {
			t.Error("the replica's last-sync stamp must move even though the client hung up")
		}
	}
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
		t.Error("expected a config.synced event despite the disconnect")
	}
}

// The autosync status carries last_sync_at: when a sync (manual or automatic)
// last actually wrote config to any member — the max of the per-member stamps,
// not when a sync was last attempted. Empty until a sync really changes a
// member; Bellhop renders it under its sync action.
func TestAutoSyncStatusReportsLastSyncAt(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	_, _ = store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	alignFleetVersions(t, srv, store, "dev")

	read := func() autoSyncStatus {
		t.Helper()
		rec := do(t, srv, http.MethodGet, "/api/fleet/autosync", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("autosync status = %d (%s)", rec.Code, rec.Body.String())
		}
		var got autoSyncStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		return got
	}

	if got := read(); got.LastSyncAt != "" {
		t.Fatalf("last_sync_at before any sync = %q, want empty", got.LastSyncAt)
	}

	if rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("sync = %d (%s)", rec.Code, rec.Body.String())
	}

	got := read()
	if got.LastSyncAt == "" {
		t.Fatal("last_sync_at must be set after a sync wrote config")
	}
	at, err := time.Parse(time.RFC3339Nano, got.LastSyncAt)
	if err != nil {
		t.Fatalf("last_sync_at %q is not RFC3339: %v", got.LastSyncAt, err)
	}
	if time.Since(at) > time.Minute {
		t.Errorf("last_sync_at %v is stale, want ~now", at)
	}
}

// TestConfigSyncHoldsVersionSkew: the sync handler refuses a version-skewed
// member unconditionally, even though the wizard gates first, so a bypassed UI
// cannot force a mismatched push.
func TestConfigSyncHoldsVersionSkew(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	replica := newStubConfigMember(t, "rtoken")

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
	setMemberVersion(srv, pm.ID, "v1.0.0")
	setMemberVersion(srv, rm.ID, "v0.9.0")

	rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("sync = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Results []syncResultItem `json:"results"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Results) != 1 || resp.Results[0].MemberID != rm.ID {
		t.Fatalf("results = %+v", resp.Results)
	}
	item := resp.Results[0]
	if item.OK {
		t.Error("version-skewed member reported ok; want refused")
	}
	if !strings.Contains(item.Error, "version") {
		t.Errorf("error = %q, want it to name the version skew", item.Error)
	}
	if replica.gotImport || replica.gotBackup {
		t.Error("version-skewed member was contacted for import/backup; want held")
	}
}
