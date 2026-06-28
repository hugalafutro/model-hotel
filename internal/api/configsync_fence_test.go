package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
)

// doImportGen posts an import carrying an optional commit-fence source generation
// (the X-Fleet-Source-Gen header). A nil gen models a pre-fence Front Desk that
// sends no header. It returns the decoded response and the HTTP recorder.
func doImportGen(t *testing.T, r chi.Router, env ConfigEnvelope, gen *int64) (importResponse, *httptest.ResponseRecorder) {
	t.Helper()
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader(body))
	if gen != nil {
		req.Header.Set(fleetSourceGenHeader, strconv.FormatInt(*gen, 10))
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var resp importResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode import response: %v (body %s)", err, rec.Body.String())
		}
	}
	return resp, rec
}

// providerNames returns the set of provider names currently on the member.
func providerNames(t *testing.T) map[string]bool {
	t.Helper()
	rows, err := apiTestDB.Pool().Query(context.Background(), `SELECT name FROM providers`)
	if err != nil {
		t.Fatalf("query providers: %v", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan provider: %v", err)
		}
		out[n] = true
	}
	return out
}

// storedSourceGen returns the persisted commit-fence marker, or "" when absent.
func storedSourceGen(t *testing.T) string {
	t.Helper()
	var v string
	err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT value FROM settings WHERE key = $1`, keyFleetLastSourceGen).Scan(&v)
	if err != nil {
		return "" // pgx.ErrNoRows or anything else: treat as unset for the assertion
	}
	return v
}

// withExtraProvider returns a copy of env with one extra keyless provider, so a
// successful import is observable (the provider appears) and a refused one is not.
func withExtraProvider(env ConfigEnvelope, name string) ConfigEnvelope {
	out := env
	out.Config.Providers = append(append([]ExportProvider(nil), env.Config.Providers...),
		ExportProvider{Name: name, BaseURL: "https://" + name + ".example", Enabled: true, AutodiscoveryEnabled: true})
	return out
}

func gptr(v int64) *int64 { return &v }

// TestConfigSync_CommitFenceRejectsStaleSourceGen exercises the member-side
// commit fence end to end: an import older than the last applied generation is
// refused (config untouched), an equal or newer one applies, the marker advances
// monotonically, and a header-less (pre-fence) import still applies unconditionally.
func TestConfigSync_CommitFenceRejectsStaleSourceGen(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	base := doExport(t, r) // envelope with just openai (decryptable key)
	withExtra := withExtraProvider(base, "extra")

	// gen=5: first fenced import applies and records the marker.
	if resp, rec := doImportGen(t, r, base, gptr(5)); rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("gen=5 import: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if got := storedSourceGen(t); got != "5" {
		t.Fatalf("marker after gen=5 = %q, want 5", got)
	}

	// gen=4: older than the applied generation, so the fence refuses it. The
	// response is a benign "stale", not an error, and the carried change (the extra
	// provider) must NOT land.
	resp, rec := doImportGen(t, r, withExtra, gptr(4))
	if rec.Code != http.StatusOK || resp.Applied || !resp.Stale || !resp.SchemaVersionOK || !resp.MasterKeyOK {
		t.Fatalf("gen=4 import: code=%d applied=%v stale=%v schemaOK=%v keyOK=%v, want 200 not-applied stale schemaOK keyOK",
			rec.Code, resp.Applied, resp.Stale, resp.SchemaVersionOK, resp.MasterKeyOK)
	}
	if providerNames(t)["extra"] {
		t.Error("a stale-refused import must not apply its config (extra provider leaked)")
	}
	if got := storedSourceGen(t); got != "5" {
		t.Errorf("marker after refused gen=4 = %q, want unchanged 5", got)
	}

	// gen=5 again: an equal generation is allowed (a legitimate same-generation
	// config change), so the extra provider now lands.
	if resp, rec := doImportGen(t, r, withExtra, gptr(5)); rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("gen=5 (equal) import: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if !providerNames(t)["extra"] {
		t.Error("an equal-generation import should apply (extra provider missing)")
	}

	// gen=6: newer generation applies and declaratively removes the extra provider
	// again, advancing the marker.
	if resp, rec := doImportGen(t, r, base, gptr(6)); rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("gen=6 import: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if providerNames(t)["extra"] {
		t.Error("gen=6 import should have declaratively removed the extra provider")
	}
	if got := storedSourceGen(t); got != "6" {
		t.Errorf("marker after gen=6 = %q, want 6", got)
	}

	// No header (a pre-fence Front Desk): the import is unfenced and applies
	// unconditionally, and it leaves the marker untouched.
	if resp, rec := doImportGen(t, r, withExtra, nil); rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("header-less import: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if !providerNames(t)["extra"] {
		t.Error("a header-less (legacy) import should apply unconditionally")
	}
	if got := storedSourceGen(t); got != "6" {
		t.Errorf("marker after header-less import = %q, want unchanged 6", got)
	}
}

// TestConfigSync_CommitFenceDryRunNeverFenced confirms a dry-run is never refused
// by the fence even when it carries an older generation: it is read-only, so Front
// Desk can always preview a diff.
func TestConfigSync_CommitFenceDryRunNeverFenced(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	base := doExport(t, r)

	// Apply at gen=10 so the marker is well above the dry-run's generation.
	if _, rec := doImportGen(t, r, base, gptr(10)); rec.Code != http.StatusOK {
		t.Fatalf("seed apply gen=10: code=%d", rec.Code)
	}

	// A dry-run carrying an older generation still returns the diff (not stale).
	body, _ := json.Marshal(withExtraProvider(base, "extra"))
	req := httptest.NewRequest(http.MethodPost, "/config/import?dryRun=1", bytes.NewReader(body))
	req.Header.Set(fleetSourceGenHeader, "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	var resp importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode dry-run: %v (body %s)", err, rec.Body.String())
	}
	if rec.Code != http.StatusOK || resp.Applied || resp.Stale {
		t.Fatalf("dry-run with old gen: code=%d applied=%v stale=%v, want 200 not-applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if providerNames(t)["extra"] {
		t.Error("a dry-run must not write (extra provider leaked)")
	}
}
