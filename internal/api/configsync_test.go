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

// contains reports whether s is in xs.
func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
