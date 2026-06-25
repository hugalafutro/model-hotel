package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/settings"
)

// configSyncMasterKey is a fixed key so encrypt-on-seed and decrypt-on-import
// share the same secret, modelling a fleet where MASTER_KEY matches.
const configSyncMasterKey = "test-master-key-0123456789abcdef"

// newConfigSyncRouter builds a router with the member-side config-sync endpoints
// mounted (no auth: the parent group's AuthMiddleware is out of scope here).
func newConfigSyncRouter(t *testing.T, masterKey string) chi.Router {
	t.Helper()
	h := NewConfigSyncHandler(apiTestDB, settings.NewRepository(apiTestDB.Pool()), masterKey, "v-test")
	r := chi.NewRouter()
	h.Register(r)
	return r
}

// cleanConfigTables empties the tables config-sync touches so each test starts
// from a known state (the api suite shares one database).
func cleanConfigTables(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := apiTestDB.Pool().Exec(ctx,
		`TRUNCATE request_logs, models, virtual_keys, providers, settings CASCADE`)
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

// seedProvider inserts a provider with an encrypted key and returns its UUID.
func seedProvider(t *testing.T, name, plaintextKey, masterKey string) string {
	t.Helper()
	kp, err := auth.Encrypt(plaintextKey, masterKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	var id string
	err = apiTestDB.Pool().QueryRow(context.Background(), `
		INSERT INTO providers (name, base_url, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled)
		VALUES ($1, $2, $3, $4, $5, $6, true, true) RETURNING id`,
		name, "https://"+name+".example", kp.Ciphertext, kp.Nonce, kp.Salt, "sk-***").Scan(&id)
	if err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	return id
}

func doExport(t *testing.T, r chi.Router) ConfigEnvelope {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/export", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, body %s", rec.Code, rec.Body.String())
	}
	var env ConfigEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	return env
}

func doImport(t *testing.T, r chi.Router, env ConfigEnvelope, query string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/config/import"+query, bytes.NewReader(body)))
	return rec
}

func TestConfigSync_ExportImportRoundTrip(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	r := newConfigSyncRouter(t, configSyncMasterKey)

	provID := seedProvider(t, "openai", "sk-secret-value", configSyncMasterKey)
	_, err := apiTestDB.Pool().Exec(ctx, `
		INSERT INTO virtual_keys (name, key_hash, key_preview, allowed_providers, strip_reasoning)
		VALUES ('vk1', 'hash1', 'mh-***', $1, true)`, []string{provID})
	if err != nil {
		t.Fatalf("seed vk: %v", err)
	}
	settingsRepo := settings.NewRepository(apiTestDB.Pool())
	if err := settingsRepo.Set(ctx, "hedging_enabled", "true"); err != nil {
		t.Fatalf("seed setting: %v", err)
	}

	env := doExport(t, r)
	if env.SchemaVersion != configSchemaVersion {
		t.Fatalf("schema_version = %d", env.SchemaVersion)
	}
	if len(env.Config.Providers) != 1 || env.Config.Providers[0].Name != "openai" {
		t.Fatalf("providers = %+v", env.Config.Providers)
	}
	if len(env.Config.Providers[0].EncryptedKey) == 0 {
		t.Fatal("provider encrypted key not exported")
	}
	if len(env.Config.VirtualKeys) != 1 ||
		len(env.Config.VirtualKeys[0].AllowedProviderNames) != 1 ||
		env.Config.VirtualKeys[0].AllowedProviderNames[0] != "openai" {
		t.Fatalf("vk allowed names not translated to provider name: %+v", env.Config.VirtualKeys)
	}
	if env.Config.Settings["hedging_enabled"] != "true" {
		t.Fatalf("settings = %+v", env.Config.Settings)
	}

	// Fresh replica: wipe and import. Provider UUIDs will be new, so the VK's
	// allowed_providers must be re-resolved by name to the new provider's UUID.
	cleanConfigTables(t)
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode import: %v", err)
	}
	if !resp.Applied || !resp.MasterKeyOK {
		t.Fatalf("import response = %+v", resp)
	}
	if !contains(resp.Diff.Providers.Added, "openai") || !contains(resp.Diff.VirtualKeys.Added, "vk1") {
		t.Fatalf("diff = %+v", resp.Diff)
	}

	// Provider key decrypts on the replica.
	var ek, nonce, salt []byte
	var newProvID string
	if err := apiTestDB.Pool().QueryRow(ctx,
		`SELECT id, encrypted_key, key_nonce, key_salt FROM providers WHERE name = 'openai'`).
		Scan(&newProvID, &ek, &nonce, &salt); err != nil {
		t.Fatalf("read imported provider: %v", err)
	}
	plain, err := auth.Decrypt(ek, nonce, salt, configSyncMasterKey)
	if err != nil || plain != "sk-secret-value" {
		t.Fatalf("decrypt imported key = %q, err %v", plain, err)
	}

	// VK allowed_providers re-points at the replica's own provider UUID.
	var allowed []string
	if err := apiTestDB.Pool().QueryRow(ctx,
		`SELECT allowed_providers FROM virtual_keys WHERE key_hash = 'hash1'`).Scan(&allowed); err != nil {
		t.Fatalf("read imported vk: %v", err)
	}
	if len(allowed) != 1 || allowed[0] != newProvID {
		t.Fatalf("allowed_providers = %v, want [%s]", allowed, newProvID)
	}

	if got := settings.NewRepository(apiTestDB.Pool()).GetWithDefault(ctx, "hedging_enabled", "false"); got != "true" {
		t.Fatalf("imported setting = %q", got)
	}
}

func TestConfigSync_ImportMasterKeyMismatch(t *testing.T) {
	cleanConfigTables(t)
	// Export with one key, import with a handler holding a DIFFERENT master key.
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	env := doExport(t, exportRouter)

	cleanConfigTables(t)
	importRouter := newConfigSyncRouter(t, "a-totally-different-master-key!!")
	rec := doImport(t, importRouter, env, "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("mismatch status = %d, want 409", rec.Code)
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.MasterKeyOK || resp.Applied {
		t.Fatalf("mismatch response = %+v", resp)
	}
	// Nothing written.
	var n int
	_ = apiTestDB.Pool().QueryRow(context.Background(), `SELECT count(*) FROM providers`).Scan(&n)
	if n != 0 {
		t.Fatalf("providers written on mismatch: %d", n)
	}
}

func TestConfigSync_ImportDryRunWritesNothing(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	env := doExport(t, exportRouter)

	cleanConfigTables(t)
	rec := doImport(t, exportRouter, env, "?dryRun=1")
	if rec.Code != http.StatusOK {
		t.Fatalf("dryRun status = %d", rec.Code)
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Applied {
		t.Fatal("dryRun must not apply")
	}
	if !contains(resp.Diff.Providers.Added, "openai") {
		t.Fatalf("dryRun diff = %+v", resp.Diff)
	}
	var n int
	_ = apiTestDB.Pool().QueryRow(context.Background(), `SELECT count(*) FROM providers`).Scan(&n)
	if n != 0 {
		t.Fatalf("dryRun wrote %d providers", n)
	}
}

func TestConfigSync_DeclarativeReplacePreservesLogsAndModels(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	r := newConfigSyncRouter(t, configSyncMasterKey)

	oldID := seedProvider(t, "old", "sk-old", configSyncMasterKey)
	keepID := seedProvider(t, "keep", "sk-keep", configSyncMasterKey)
	// A request log under the provider that will be removed: it must survive with
	// provider_id nulled, not be deleted.
	if _, err := apiTestDB.Pool().Exec(ctx,
		`INSERT INTO request_logs (provider_id) VALUES ($1)`, oldID); err != nil {
		t.Fatalf("seed log: %v", err)
	}
	// A model under the provider that will be kept (updated): it must survive the
	// in-place upsert (we must not delete-and-recreate the provider).
	if _, err := apiTestDB.Pool().Exec(ctx,
		`INSERT INTO models (provider_id, model_id, enabled) VALUES ($1, 'gpt-x', true)`, keepID); err != nil {
		t.Fatalf("seed model: %v", err)
	}

	// Export, then import only "keep" so "old" is declaratively removed.
	env := doExport(t, r)
	filtered := env
	filtered.Config.Providers = nil
	for _, p := range env.Config.Providers {
		if p.Name == "keep" {
			filtered.Config.Providers = append(filtered.Config.Providers, p)
		}
	}
	rec := doImport(t, r, filtered, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	var provCount int
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM providers`).Scan(&provCount)
	if provCount != 1 {
		t.Fatalf("providers after replace = %d, want 1", provCount)
	}
	// The removed provider's log survived, provider_id nulled.
	var logCount, nullLinks int
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM request_logs`).Scan(&logCount)
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM request_logs WHERE provider_id IS NULL`).Scan(&nullLinks)
	if logCount != 1 || nullLinks != 1 {
		t.Fatalf("logs=%d nullLinks=%d, want 1/1 (history preserved, link nulled)", logCount, nullLinks)
	}
	// The kept provider's model survived the in-place update.
	var modelCount int
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM models`).Scan(&modelCount)
	if modelCount != 1 {
		t.Fatalf("models after in-place provider update = %d, want 1", modelCount)
	}
}

func TestConfigSync_DiffAddedUpdatedRemoved(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	r := newConfigSyncRouter(t, configSyncMasterKey)

	// Target starts with "keep" + "extra" providers and two VKs and a setting.
	seedProvider(t, "keep", "sk-keep", configSyncMasterKey)
	seedProvider(t, "extra", "sk-extra", configSyncMasterKey)
	for _, h := range []struct{ name, hash string }{{"vk-keep", "hk"}, {"vk-extra", "he"}} {
		if _, err := apiTestDB.Pool().Exec(ctx,
			`INSERT INTO virtual_keys (name, key_hash, key_preview) VALUES ($1, $2, 'p')`,
			h.name, h.hash); err != nil {
			t.Fatalf("seed vk: %v", err)
		}
	}
	sr := settings.NewRepository(apiTestDB.Pool())
	if err := sr.Set(ctx, "hedging_enabled", "true"); err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	// A syncable setting the envelope omits -> must be removed (declarative).
	if err := sr.Set(ctx, "ttft_timeout", "5s"); err != nil {
		t.Fatalf("seed setting: %v", err)
	}
	// A non-syncable setting -> must be preserved (never touched by sync).
	if err := apiTestDB.Pool().QueryRow(ctx,
		`INSERT INTO settings (key, value) VALUES ('log_export_json', 'true') RETURNING key`).
		Scan(new(string)); err != nil {
		t.Fatalf("seed non-syncable setting: %v", err)
	}

	// Import keeps "keep" (updated) + adds "new", drops "extra"; same for VKs;
	// updates hedging_enabled and adds request_timeout.
	kp1, _ := auth.Encrypt("sk-keep2", configSyncMasterKey)
	kp2, _ := auth.Encrypt("sk-new", configSyncMasterKey)
	env := ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config: ConfigPayload{
			Providers: []ExportProvider{
				{Name: "keep", BaseURL: "https://keep", Enabled: true, AutodiscoveryEnabled: true, EncryptedKey: kp1.Ciphertext, KeyNonce: kp1.Nonce, KeySalt: kp1.Salt},
				{Name: "new", BaseURL: "https://new", Enabled: true, AutodiscoveryEnabled: true, EncryptedKey: kp2.Ciphertext, KeyNonce: kp2.Nonce, KeySalt: kp2.Salt},
			},
			VirtualKeys: []ExportVK{
				{Name: "vk-keep", KeyHash: "hk", KeyPreview: "p"},
				{Name: "vk-new", KeyHash: "hnew", KeyPreview: "p"},
			},
			Settings: map[string]string{"hedging_enabled": "false", "request_timeout": "30s"},
		},
	}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import = %d (%s)", rec.Code, rec.Body.String())
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	d := resp.Diff
	if !contains(d.Providers.Added, "new") || !contains(d.Providers.Updated, "keep") || !contains(d.Providers.Removed, "extra") {
		t.Errorf("provider diff = %+v", d.Providers)
	}
	if !contains(d.VirtualKeys.Added, "vk-new") || !contains(d.VirtualKeys.Updated, "vk-keep") || !contains(d.VirtualKeys.Removed, "vk-extra") {
		t.Errorf("vk diff = %+v", d.VirtualKeys)
	}
	if !contains(d.Settings.Added, "request_timeout") || !contains(d.Settings.Updated, "hedging_enabled") ||
		!contains(d.Settings.Removed, "ttft_timeout") {
		t.Errorf("settings diff = %+v", d.Settings)
	}
	// The omitted syncable setting is gone; the non-syncable one survives.
	var ttft, logExport int
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM settings WHERE key = 'ttft_timeout'`).Scan(&ttft)
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM settings WHERE key = 'log_export_json'`).Scan(&logExport)
	if ttft != 0 {
		t.Errorf("omitted syncable setting ttft_timeout should be deleted")
	}
	if logExport != 1 {
		t.Errorf("non-syncable setting log_export_json must be preserved")
	}

	// End state converged: only the envelope's providers/VKs remain.
	var provNames, vkHashes []string
	rows, _ := apiTestDB.Pool().Query(ctx, `SELECT name FROM providers ORDER BY name`)
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		provNames = append(provNames, n)
	}
	rows.Close()
	rows, _ = apiTestDB.Pool().Query(ctx, `SELECT key_hash FROM virtual_keys ORDER BY key_hash`)
	for rows.Next() {
		var h string
		_ = rows.Scan(&h)
		vkHashes = append(vkHashes, h)
	}
	rows.Close()
	if len(provNames) != 2 || provNames[0] != "keep" || provNames[1] != "new" {
		t.Errorf("providers after import = %v", provNames)
	}
	if len(vkHashes) != 2 || !contains(vkHashes, "hk") || !contains(vkHashes, "hnew") {
		t.Errorf("vk hashes after import = %v", vkHashes)
	}
}

func TestConfigSync_KeylessProviderImports(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	r := newConfigSyncRouter(t, configSyncMasterKey)

	// A keyless provider (e.g. a local Ollama) carries no encrypted key; the
	// MASTER_KEY canary has nothing to verify and the import still applies.
	env := ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config: ConfigPayload{
			Providers: []ExportProvider{
				{Name: "ollama", BaseURL: "http://ollama:11434", Enabled: true, AutodiscoveryEnabled: true},
			},
		},
	}
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("keyless import = %d (%s)", rec.Code, rec.Body.String())
	}
	var n int
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM providers WHERE name = 'ollama'`).Scan(&n)
	if n != 1 {
		t.Fatalf("keyless provider not imported: count %d", n)
	}
}

func TestConfigSync_SkipsVirtualKeyWithUnresolvableRestriction(t *testing.T) {
	cleanConfigTables(t)
	ctx := context.Background()
	r := newConfigSyncRouter(t, configSyncMasterKey)

	// A VK restricted to a provider the envelope does not include would, if
	// imported, become unrestricted (empty allow-list = all allowed). It must be
	// skipped instead so a restricted key never silently becomes a master key.
	kp, _ := auth.Encrypt("sk", configSyncMasterKey)
	env := ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config: ConfigPayload{
			Providers: []ExportProvider{
				{Name: "present", BaseURL: "https://p", Enabled: true, AutodiscoveryEnabled: true, EncryptedKey: kp.Ciphertext, KeyNonce: kp.Nonce, KeySalt: kp.Salt},
			},
			VirtualKeys: []ExportVK{
				{Name: "vk-open", KeyHash: "hopen", KeyPreview: "p"}, // unrestricted -> imported
				{Name: "vk-restricted", KeyHash: "hres", KeyPreview: "p", AllowedProviderNames: []string{"absent-provider"}},
			},
		},
	}
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import = %d (%s)", rec.Code, rec.Body.String())
	}
	var open, restricted int
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM virtual_keys WHERE key_hash = 'hopen'`).Scan(&open)
	_ = apiTestDB.Pool().QueryRow(ctx, `SELECT count(*) FROM virtual_keys WHERE key_hash = 'hres'`).Scan(&restricted)
	if open != 1 {
		t.Errorf("unrestricted key should import: count %d", open)
	}
	if restricted != 0 {
		t.Errorf("key with unresolvable restriction must be skipped, not imported unrestricted: count %d", restricted)
	}
}

func TestConfigSync_RefusesEmptyEnvelope(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	rec := doImport(t, r, ConfigEnvelope{SchemaVersion: configSchemaVersion}, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty envelope status = %d, want 400", rec.Code)
	}
}

func TestConfigSync_RejectsWrongSchemaVersion(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	env := ConfigEnvelope{SchemaVersion: 999}
	env.Config.Providers = []ExportProvider{{Name: "x", BaseURL: "https://x"}}
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("bad schema status = %d, want 422", rec.Code)
	}
}

func TestConfigSync_ImportRejectsInvalidJSON(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader([]byte("not json"))))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON = %d, want 400", rec.Code)
	}
}

// cancelledCtx returns an already-cancelled context so DB queries fail fast,
// exercising the handlers' read-error branches without a broken database.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestConfigSync_ExportDBError(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/config/export", http.NoBody).WithContext(cancelledCtx())
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("export with DB error = %d, want 500", rec.Code)
	}
}

func TestConfigSync_ImportDBError(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	// A valid, keyless envelope: it clears decode, schema, empty, and MASTER_KEY
	// checks (no DB), so the failure surfaces in computeDiff's read.
	env := ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config:        ConfigPayload{Providers: []ExportProvider{{Name: "ollama", BaseURL: "http://o"}}},
	}
	body, _ := json.Marshal(env)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/config/import", bytes.NewReader(body)).WithContext(cancelledCtx())
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("import with DB error = %d, want 500", rec.Code)
	}
}

// contains reports whether s is in xs.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
