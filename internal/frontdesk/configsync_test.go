package frontdesk

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// stubConfigMember is a fake Model Hotel member exposing /api/config/export and
// /api/config/import with the Bearer-auth contract the real endpoints have. The
// import response is configurable so a test can model a normal replica, a
// MASTER_KEY mismatch (409), or an already-converged member (empty diff).
type stubConfigMember struct {
	token      string
	exportBody string
	importCode int
	importBody string
	gotImport  bool
	gotDryRun  bool
	srv        *httptest.Server
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
			w.WriteHeader(sm.importCode)
			_, _ = w.Write([]byte(sm.importBody))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(sm.srv.Close)
	return sm
}

func TestConfigSyncPreviewClassifies(t *testing.T) {
	srv, store := newTestServer(t)
	primary := newStubConfigMember(t, "ptoken")
	overwrite := newStubConfigMember(t, "otoken") // default: non-empty diff
	matches := newStubConfigMember(t, "mtoken")
	matches.importBody = `{"schema_version_ok":true,"master_key_ok":true,"applied":false,"diff":{"providers":{},"virtual_keys":{},"settings":{}}}`
	mismatch := newStubConfigMember(t, "xtoken")
	mismatch.importCode = http.StatusConflict
	mismatch.importBody = `{"schema_version_ok":true,"master_key_ok":false}`

	pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
	om, _ := store.CreateMember(t.Context(), "overwrite", overwrite.srv.URL, "otoken")
	mm, _ := store.CreateMember(t.Context(), "matches", matches.srv.URL, "mtoken")
	xm, _ := store.CreateMember(t.Context(), "mismatch", mismatch.srv.URL, "xtoken")
	nm, _ := store.CreateMember(t.Context(), "no-token", "http://127.0.0.1:1", "")

	rec := do(t, srv, http.MethodGet, "/api/config/preview?primary="+pm.ID, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		PrimaryID string              `json:"primary_id"`
		Items     []configPreviewItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string]configPreviewItem{}
	for _, it := range resp.Items {
		byID[it.MemberID] = it
	}
	if byID[pm.ID].Disposition != dispMatches {
		t.Errorf("primary disposition = %q", byID[pm.ID].Disposition)
	}
	if byID[om.ID].Disposition != dispOverwrite || byID[om.ID].Added != 1 {
		t.Errorf("overwrite item = %+v", byID[om.ID])
	}
	if byID[mm.ID].Disposition != dispMatches {
		t.Errorf("matches disposition = %q", byID[mm.ID].Disposition)
	}
	if byID[xm.ID].Disposition != dispBlocked || byID[xm.ID].Note == "" {
		t.Errorf("mismatch item = %+v (want blocked + note)", byID[xm.ID])
	}
	if byID[nm.ID].Disposition != dispBlocked {
		t.Errorf("no-token disposition = %q", byID[nm.ID].Disposition)
	}
	// Preview must be a dry run on the replicas.
	if !overwrite.gotDryRun {
		t.Error("preview should call import with dryRun")
	}
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
	if primary.gotImport {
		t.Error("primary must not be imported into (it is the source)")
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
