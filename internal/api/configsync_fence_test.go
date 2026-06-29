package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

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

	// No header (a pre-fence Front Desk) onto a member a fenced generation has
	// already converged: the un-versioned write must be refused, not allowed to
	// clobber the newer config and leave the marker (still 6) lying.
	if resp, rec := doImportGen(t, r, withExtra, nil); rec.Code != http.StatusOK || resp.Applied || !resp.Stale {
		t.Fatalf("header-less import after a fenced gen: code=%d applied=%v stale=%v, want 200 not-applied stale", rec.Code, resp.Applied, resp.Stale)
	}
	if providerNames(t)["extra"] {
		t.Error("a header-less import must not overwrite versioned config (extra provider leaked)")
	}
	if got := storedSourceGen(t); got != "6" {
		t.Errorf("marker after refused header-less import = %q, want unchanged 6", got)
	}
}

// TestConfigSync_CommitFenceHeaderlessAppliesUnfenced: on a member no fenced push
// has touched (marker unset), a header-less import from a pre-fence Front Desk
// still applies unconditionally, preserving the pre-fence behaviour during a
// rolling upgrade.
func TestConfigSync_CommitFenceHeaderlessAppliesUnfenced(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	base := doExport(t, r)

	if resp, rec := doImportGen(t, r, withExtraProvider(base, "extra"), nil); rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("header-less import on an unfenced member: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if !providerNames(t)["extra"] {
		t.Error("a header-less import on an unfenced member should apply")
	}
	if got := storedSourceGen(t); got != "" {
		t.Errorf("marker after header-less import = %q, want still unset", got)
	}
}

// setStoredSourceGen writes the commit-fence marker directly, for modelling a
// member that already carries one (or a corrupt one) before an import arrives.
func setStoredSourceGen(t *testing.T, raw string) {
	t.Helper()
	_, err := apiTestDB.Pool().Exec(context.Background(),
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		keyFleetLastSourceGen, raw)
	if err != nil {
		t.Fatalf("seed source-gen marker: %v", err)
	}
}

// TestConfigSync_CommitFenceGenZeroIsFenced: a fenced import carrying generation 0
// (the wizard can sync at auto_sync_gen 0) still records a marker, so a later
// header-less import is refused rather than being allowed to overwrite it. Zero is
// a real applied generation, not "never fenced".
func TestConfigSync_CommitFenceGenZeroIsFenced(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	base := doExport(t, r)

	// Fenced import at generation 0 applies and records the marker as "0".
	if resp, rec := doImportGen(t, r, base, gptr(0)); rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("gen=0 import: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if got := storedSourceGen(t); got != "0" {
		t.Fatalf("marker after gen=0 = %q, want 0", got)
	}

	// A header-less import must now be refused: the member has been fenced (marker
	// present), even though its value is 0.
	if resp, rec := doImportGen(t, r, withExtraProvider(base, "extra"), nil); rec.Code != http.StatusOK || resp.Applied || !resp.Stale {
		t.Fatalf("header-less import after gen=0: code=%d applied=%v stale=%v, want 200 not-applied stale", rec.Code, resp.Applied, resp.Stale)
	}
	if providerNames(t)["extra"] {
		t.Error("a header-less import must not overwrite a gen-0 fenced config")
	}
}

// TestConfigSync_CommitFenceUnparseableHeaderIsUnfenced: a non-numeric header is
// ignored (treated as no header), so on an unfenced member the import still
// applies and records no marker.
func TestConfigSync_CommitFenceUnparseableHeaderIsUnfenced(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	base := doExport(t, r)

	body, _ := json.Marshal(withExtraProvider(base, "extra"))
	req := httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader(body))
	req.Header.Set(fleetSourceGenHeader, "not-a-number")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	var resp importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (body %s)", err, rec.Body.String())
	}
	if rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("unparseable-header import: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if !providerNames(t)["extra"] {
		t.Error("an unparseable header should fall back to an unfenced apply")
	}
	if got := storedSourceGen(t); got != "" {
		t.Errorf("marker after unparseable-header import = %q, want still unset", got)
	}
}

// TestConfigSync_CommitFenceCorruptMarkerStillFences: a corrupt (non-numeric)
// stored marker still counts as fenced, so a header-less import is refused; a
// fresh fenced import then rewrites a clean value.
func TestConfigSync_CommitFenceCorruptMarkerStillFences(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	base := doExport(t, r)
	setStoredSourceGen(t, "garbage")

	// Header-less import is refused: the marker is present (even if unparseable).
	if resp, rec := doImportGen(t, r, withExtraProvider(base, "extra"), nil); rec.Code != http.StatusOK || resp.Applied || !resp.Stale {
		t.Fatalf("header-less import with corrupt marker: code=%d applied=%v stale=%v, want 200 not-applied stale", rec.Code, resp.Applied, resp.Stale)
	}
	if providerNames(t)["extra"] {
		t.Error("a header-less import must not overwrite config behind a corrupt marker")
	}

	// A fenced import applies (the corrupt marker floors to 0, so any generation is
	// accepted) and rewrites a clean value.
	if resp, rec := doImportGen(t, r, base, gptr(3)); rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("fenced import over corrupt marker: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}
	if got := storedSourceGen(t); got != "3" {
		t.Errorf("marker after fenced rewrite = %q, want 3", got)
	}
}

// TestConfigSync_CommitFenceLockContentionFailsClosed: the fence serializes on a
// Postgres advisory lock, so while another connection holds it an import blocks;
// when the request's context deadline elapses the import fails closed with 500
// rather than applying without the fence check. This also proves the lock is real
// (a second import genuinely waits on the first).
func TestConfigSync_CommitFenceLockContentionFailsClosed(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	base := doExport(t, r)

	// Hold the fence advisory lock on a dedicated connection so the import's
	// transaction-scoped lock cannot be granted.
	holder, err := apiTestDB.Pool().Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire holder conn: %v", err)
	}
	defer holder.Release()
	if _, err := holder.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, fleetSourceGenLock); err != nil {
		t.Fatalf("hold advisory lock: %v", err)
	}
	defer func() { _, _ = holder.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, fleetSourceGenLock) }()

	// The import clears the no-DB checks and computeDiff, opens its transaction,
	// then blocks acquiring the lock until this deadline elapses.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	body, _ := json.Marshal(withExtraProvider(base, "extra"))
	req := httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set(fleetSourceGenHeader, "1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("import under lock contention = %d, want 500 (failed closed)", rec.Code)
	}
	if providerNames(t)["extra"] {
		t.Error("a fence-blocked import must not apply its config")
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
