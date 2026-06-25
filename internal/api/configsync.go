package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
)

// This file implements the HA "Phase 5" fleet config-sync member endpoints:
// GET /api/config/export and POST /api/config/import. Front Desk pulls the
// export from a chosen primary and pushes the import to each replica so the
// fleet converges to one configuration. Only config is moved: never request
// logs, metering, events, backups, or per-instance auth.
//
// The transport is config-only JSON, NOT the pg_dump backup (which is the whole
// database and is destructive to restore). Provider keys travel as their stored
// AES-GCM ciphertext: every member shares MASTER_KEY by HA design, so a replica
// decrypts them with its own copy and no key is ever re-entered. Cross-instance
// references are carried as stable NAMES, never instance-local UUIDs (a virtual
// key's allowed_providers is translated provider-UUID -> name on export and back
// on import).
//
// v1 syncs providers, virtual keys, and the syncable settings subset. Models and
// failover groups auto-regenerate on each member from the synced providers
// (discovery + automatic group formation), so they are intentionally not copied;
// their manual overrides (disable / rename / custom priority) are a documented
// follow-up because they require model-ID translation.

const (
	// configSchemaVersion is the envelope version a member understands. An import
	// carrying a different version is refused rather than half-applied.
	configSchemaVersion = 1

	// maxConfigImportBody bounds an import payload. Fleet config is small (a
	// handful of providers + keys); 8 MiB is generous and caps a hostile body.
	maxConfigImportBody = 8 << 20
)

// ConfigSyncHandler serves the member-side config export/import endpoints. It is
// mounted inside the admin-authenticated /api group, so every call already
// requires the admin token (or a session when TOTP is on): a caller able to
// import config controls the data plane, so no weaker gate is acceptable.
type ConfigSyncHandler struct {
	db         *db.DB
	settings   SettingsStore
	masterKey  string
	appVersion string
}

// NewConfigSyncHandler builds the handler. masterKey is needed only to verify
// (on import) that this member can decrypt the incoming provider keys; the
// plaintext is never produced here.
func NewConfigSyncHandler(database *db.DB, settingsRepo SettingsStore, masterKey, appVersion string) *ConfigSyncHandler {
	return &ConfigSyncHandler{db: database, settings: settingsRepo, masterKey: masterKey, appVersion: appVersion}
}

// Register mounts GET/POST /config/{export,import}. The parent router must apply
// admin auth (see type doc).
func (h *ConfigSyncHandler) Register(r chi.Router) {
	r.Get("/config/export", h.Export)
	r.Post("/config/import", h.Import)
}

// ---------------------------------------------------------------------------
// Envelope types
// ---------------------------------------------------------------------------

// ConfigEnvelope is the JSON exchanged between members. []byte fields marshal as
// base64, so the provider key ciphertext rides safely inside JSON.
type ConfigEnvelope struct {
	SchemaVersion int           `json:"schema_version"`
	AppVersion    string        `json:"app_version"`
	ExportedAt    time.Time     `json:"exported_at"`
	Config        ConfigPayload `json:"config"`
}

// ConfigPayload is the config-only body of the envelope.
type ConfigPayload struct {
	Providers   []ExportProvider  `json:"providers"`
	VirtualKeys []ExportVK        `json:"virtual_keys"`
	Settings    map[string]string `json:"settings"`
}

// ExportProvider is a provider with its encrypted key material verbatim.
type ExportProvider struct {
	Name                 string  `json:"name"`
	BaseURL              string  `json:"base_url"`
	Enabled              bool    `json:"enabled"`
	AutodiscoveryEnabled bool    `json:"autodiscovery_enabled"`
	EncryptedKey         []byte  `json:"encrypted_key,omitempty"`
	KeyNonce             []byte  `json:"key_nonce,omitempty"`
	KeySalt              []byte  `json:"key_salt,omitempty"`
	MaskedKey            *string `json:"masked_key,omitempty"`
}

// ExportVK is a virtual key carried by its hash (the plaintext never existed
// server-side). allowed_providers is carried as provider NAMES, resolved back to
// this member's provider UUIDs on import.
type ExportVK struct {
	Name                 string   `json:"name"`
	KeyHash              string   `json:"key_hash"`
	KeyPreview           string   `json:"key_preview"`
	RateLimitRPS         *float64 `json:"rate_limit_rps,omitempty"`
	RateLimitBurst       *int     `json:"rate_limit_burst,omitempty"`
	RateLimitTPM         *int     `json:"rate_limit_tpm,omitempty"`
	AllowedProviderNames []string `json:"allowed_provider_names,omitempty"`
	StripReasoning       bool     `json:"strip_reasoning"`
}

// entityDiff lists the names changed for one entity kind in a sync.
type entityDiff struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
}

// configDiff is the per-kind summary returned by a (dry-run or applied) import.
type configDiff struct {
	Providers   entityDiff `json:"providers"`
	VirtualKeys entityDiff `json:"virtual_keys"`
	Settings    entityDiff `json:"settings"`
}

// importResponse is the body of POST /config/import.
type importResponse struct {
	SchemaVersionOK bool       `json:"schema_version_ok"`
	MasterKeyOK     bool       `json:"master_key_ok"`
	Applied         bool       `json:"applied"`
	Diff            configDiff `json:"diff"`
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

// Export returns this member's full config envelope so Front Desk can replicate
// it onto the fleet.
func (h *ConfigSyncHandler) Export(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pool := h.db.Pool()

	idToName, err := h.providerIDToName(ctx, pool)
	if err != nil {
		debuglog.Error("configsync: load providers for export", "error", err)
		http.Error(w, "could not read providers", http.StatusInternalServerError)
		return
	}

	providers, err := exportProviders(ctx, pool)
	if err != nil {
		debuglog.Error("configsync: export providers", "error", err)
		http.Error(w, "could not export providers", http.StatusInternalServerError)
		return
	}
	vks, err := exportVirtualKeys(ctx, pool, idToName)
	if err != nil {
		debuglog.Error("configsync: export virtual keys", "error", err)
		http.Error(w, "could not export virtual keys", http.StatusInternalServerError)
		return
	}
	set, err := exportSettings(ctx, pool)
	if err != nil {
		debuglog.Error("configsync: export settings", "error", err)
		http.Error(w, "could not export settings", http.StatusInternalServerError)
		return
	}

	writeJSON(w, ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		AppVersion:    h.appVersion,
		ExportedAt:    time.Now().UTC(),
		Config:        ConfigPayload{Providers: providers, VirtualKeys: vks, Settings: set},
	})
}

// providerIDToName maps provider UUID (text) -> name for translating a virtual
// key's allowed_providers list out of instance-local IDs.
func (h *ConfigSyncHandler) providerIDToName(ctx context.Context, q querier) (map[string]string, error) {
	rows, err := q.Query(ctx, `SELECT id, name FROM providers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func exportProviders(ctx context.Context, q querier) ([]ExportProvider, error) {
	rows, err := q.Query(ctx, `
		SELECT name, base_url, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled
		FROM providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportProvider{}
	for rows.Next() {
		var p ExportProvider
		if err := rows.Scan(&p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt,
			&p.MaskedKey, &p.Enabled, &p.AutodiscoveryEnabled); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func exportVirtualKeys(ctx context.Context, q querier, idToName map[string]string) ([]ExportVK, error) {
	rows, err := q.Query(ctx, `
		SELECT name, key_hash, key_preview, rate_limit_rps, rate_limit_burst, rate_limit_tpm,
		       allowed_providers, strip_reasoning
		FROM virtual_keys ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportVK{}
	for rows.Next() {
		var v ExportVK
		var allowedIDs []string
		if err := rows.Scan(&v.Name, &v.KeyHash, &v.KeyPreview, &v.RateLimitRPS, &v.RateLimitBurst,
			&v.RateLimitTPM, &allowedIDs, &v.StripReasoning); err != nil {
			return nil, err
		}
		// Translate instance-local provider UUIDs to names; drop any that no
		// longer resolve. A key whose entire allow-list is stale exports an empty
		// AllowedProviderNames; import refuses to widen it to all-allowed and
		// skips it instead (see upsertVirtualKeys).
		for _, id := range allowedIDs {
			if name, ok := idToName[id]; ok {
				v.AllowedProviderNames = append(v.AllowedProviderNames, name)
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func exportSettings(ctx context.Context, q querier) (map[string]string, error) {
	keys := syncableSettingKeys()
	rows, err := q.Query(ctx, `SELECT key, value FROM settings WHERE key = ANY($1)`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, val string
		if err := rows.Scan(&k, &val); err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// Import applies an envelope onto this member. With ?dryRun=1 it returns the diff
// without writing. Otherwise it converges this member to the envelope inside a
// single transaction: all-or-nothing.
func (h *ConfigSyncHandler) Import(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigImportBody)

	var env ConfigEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if env.SchemaVersion != configSchemaVersion {
		writeJSONStatus(w, http.StatusUnprocessableEntity, importResponse{SchemaVersionOK: false})
		return
	}
	if len(env.Config.Providers) == 0 && len(env.Config.VirtualKeys) == 0 && len(env.Config.Settings) == 0 {
		// A structurally empty envelope is almost always a mistake, and applying
		// it would delete everything on the target. Refuse rather than wipe.
		http.Error(w, "refusing to import an empty config", http.StatusBadRequest)
		return
	}

	// MASTER_KEY guard: prove this member can decrypt an incoming provider key
	// before writing anything. A mismatch means the fleet's keys differ; storing
	// undecryptable ciphertext would silently break the data plane.
	if !h.canDecryptSample(env.Config.Providers) {
		writeJSONStatus(w, http.StatusConflict, importResponse{SchemaVersionOK: true, MasterKeyOK: false})
		return
	}

	diff, err := h.computeDiff(ctx, env)
	if err != nil {
		debuglog.Error("configsync: compute diff", "error", err)
		http.Error(w, "could not read current config", http.StatusInternalServerError)
		return
	}

	if r.URL.Query().Get("dryRun") != "" {
		writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: false, Diff: diff})
		return
	}

	if err := h.apply(ctx, env); err != nil {
		debuglog.Error("configsync: apply import", "error", err)
		http.Error(w, "could not apply config", http.StatusInternalServerError)
		return
	}

	writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: true, Diff: diff})
}

// canDecryptSample returns true when there is no encrypted key to check, or the
// first one decrypts under this member's MASTER_KEY.
func (h *ConfigSyncHandler) canDecryptSample(providers []ExportProvider) bool {
	for _, p := range providers {
		if len(p.EncryptedKey) == 0 {
			continue
		}
		if _, err := auth.Decrypt(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.masterKey); err != nil {
			return false
		}
		return true // one good decrypt proves the shared key
	}
	return true // keyless fleet: nothing to verify
}

// computeDiff classifies each entity as added (new to this member), updated
// (present on both), or removed (here but not in the envelope).
func (h *ConfigSyncHandler) computeDiff(ctx context.Context, env ConfigEnvelope) (configDiff, error) {
	pool := h.db.Pool()
	var d configDiff

	curProviders, err := nameSet(ctx, pool, `SELECT name FROM providers`)
	if err != nil {
		return d, err
	}
	wantProviders := map[string]struct{}{}
	for _, p := range env.Config.Providers {
		wantProviders[p.Name] = struct{}{}
		if _, ok := curProviders[p.Name]; ok {
			d.Providers.Updated = append(d.Providers.Updated, p.Name)
		} else {
			d.Providers.Added = append(d.Providers.Added, p.Name)
		}
	}
	for name := range curProviders {
		if _, ok := wantProviders[name]; !ok {
			d.Providers.Removed = append(d.Providers.Removed, name)
		}
	}

	curVKs, err := hashToName(ctx, pool, `SELECT key_hash, name FROM virtual_keys`)
	if err != nil {
		return d, err
	}
	wantVKs := map[string]struct{}{}
	for _, v := range env.Config.VirtualKeys {
		wantVKs[v.KeyHash] = struct{}{}
		if _, ok := curVKs[v.KeyHash]; ok {
			d.VirtualKeys.Updated = append(d.VirtualKeys.Updated, v.Name)
		} else {
			d.VirtualKeys.Added = append(d.VirtualKeys.Added, v.Name)
		}
	}
	for hash, name := range curVKs {
		if _, ok := wantVKs[hash]; !ok {
			d.VirtualKeys.Removed = append(d.VirtualKeys.Removed, name)
		}
	}

	curSettings, err := nameSet(ctx, pool, `SELECT key FROM settings`)
	if err != nil {
		return d, err
	}
	for k := range env.Config.Settings {
		if !isSyncableSetting(k) {
			continue
		}
		if _, ok := curSettings[k]; ok {
			d.Settings.Updated = append(d.Settings.Updated, k)
		} else {
			d.Settings.Added = append(d.Settings.Added, k)
		}
	}
	// A syncable setting present here but not on the primary is removed (the
	// replica falls back to the built-in default), mirroring providers/VKs.
	for k := range curSettings {
		if !isSyncableSetting(k) {
			continue
		}
		if _, ok := env.Config.Settings[k]; !ok {
			d.Settings.Removed = append(d.Settings.Removed, k)
		}
	}
	return d, nil
}

// apply converges this member to the envelope in one transaction.
func (h *ConfigSyncHandler) apply(ctx context.Context, env ConfigEnvelope) error {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := upsertProviders(ctx, tx, env.Config.Providers); err != nil {
		return err
	}
	// Declarative replace: drop providers absent from the primary. This cascades
	// to their discovered models (FK ON DELETE CASCADE) but request_logs are
	// preserved: their provider_id FK is ON DELETE SET NULL (migration 010), so
	// history stays and only the provider link is nulled.
	providerNames := names(env.Config.Providers, func(p ExportProvider) string { return p.Name })
	if _, err := tx.Exec(ctx, `DELETE FROM providers WHERE name <> ALL($1)`, providerNames); err != nil {
		return err
	}

	// Provider names resolve to THIS member's UUIDs only after the upsert above.
	nameToID, err := providerNameToID(ctx, tx)
	if err != nil {
		return err
	}
	if err := upsertVirtualKeys(ctx, tx, env.Config.VirtualKeys, nameToID); err != nil {
		return err
	}
	vkHashes := names(env.Config.VirtualKeys, func(v ExportVK) string { return v.KeyHash })
	if _, err := tx.Exec(ctx, `DELETE FROM virtual_keys WHERE key_hash <> ALL($1)`, vkHashes); err != nil {
		return err
	}

	for k, v := range env.Config.Settings {
		if !isSyncableSetting(k) {
			continue // skip non-syncable / unknown keys silently
		}
		if err := h.settings.SetTx(ctx, tx, k, v); err != nil {
			return err
		}
	}
	// Declarative replace for settings too: delete any syncable key this member
	// has that the primary does not, so the replica falls back to the same
	// built-in default the primary is using. Non-syncable keys (apprise,
	// observability, instance-local) are never touched.
	removedSettings, err := h.syncableSettingsToDelete(ctx, tx, env.Config.Settings)
	if err != nil {
		return err
	}
	if err := h.settings.DeleteKeysTx(ctx, tx, removedSettings); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// The writes bypassed the in-memory caches, so drop them: the proxy must see
	// the new providers/keys and discovery must re-read providers.
	provider.InvalidateProviderCache()
	model.InvalidateModelCache()
	failover.InvalidateFailoverCache()
	for k := range env.Config.Settings {
		if isSyncableSetting(k) {
			h.settings.InvalidateCache(k)
		}
	}
	for _, k := range removedSettings {
		h.settings.InvalidateCache(k)
		h.settings.NotifyDeleted(k)
	}
	return nil
}

// syncableSettingsToDelete returns the syncable settings keys present on this
// member but absent from the envelope (the primary is on the built-in default).
func (h *ConfigSyncHandler) syncableSettingsToDelete(ctx context.Context, q querier, want map[string]string) ([]string, error) {
	cur, err := nameSet(ctx, q, `SELECT key FROM settings`)
	if err != nil {
		return nil, err
	}
	var toDelete []string
	for k := range cur {
		if isSyncableSetting(k) {
			if _, ok := want[k]; !ok {
				toDelete = append(toDelete, k)
			}
		}
	}
	return toDelete, nil
}

func upsertProviders(ctx context.Context, tx pgx.Tx, providers []ExportProvider) error {
	for _, p := range providers {
		_, err := tx.Exec(ctx, `
			INSERT INTO providers (name, base_url, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, now())
			ON CONFLICT (name) DO UPDATE SET
				base_url = EXCLUDED.base_url,
				encrypted_key = EXCLUDED.encrypted_key,
				key_nonce = EXCLUDED.key_nonce,
				key_salt = EXCLUDED.key_salt,
				masked_key = EXCLUDED.masked_key,
				enabled = EXCLUDED.enabled,
				autodiscovery_enabled = EXCLUDED.autodiscovery_enabled,
				updated_at = now()`,
			p.Name, p.BaseURL, p.EncryptedKey, p.KeyNonce, p.KeySalt, p.MaskedKey, p.Enabled, p.AutodiscoveryEnabled)
		if err != nil {
			return err
		}
	}
	return nil
}

func upsertVirtualKeys(ctx context.Context, tx pgx.Tx, vks []ExportVK, nameToID map[string]string) error {
	for _, v := range vks {
		var allowed []string // target provider UUIDs; nil => all allowed
		for _, name := range v.AllowedProviderNames {
			if id, ok := nameToID[name]; ok {
				allowed = append(allowed, id)
			}
		}
		// Privilege-safety: if this key was restricted to providers but none of
		// them resolve on this member, do NOT import it. An empty/nil
		// allowed_providers means "all providers allowed" (the proxy only filters
		// on a non-empty list), so writing it would silently turn a restricted key
		// into an unrestricted one. Skipping leaves the restricted key absent
		// rather than over-privileged. In the normal flow this never triggers:
		// providers are upserted in the same transaction before this runs, so
		// every name resolves.
		if len(v.AllowedProviderNames) > 0 && len(allowed) == 0 {
			debuglog.Warn("configsync: skipping virtual key whose allowed_providers do not resolve on this member", "key", v.Name)
			continue
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_rps, rate_limit_burst, rate_limit_tpm, allowed_providers, strip_reasoning)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (key_hash) DO UPDATE SET
				name = EXCLUDED.name,
				key_preview = EXCLUDED.key_preview,
				rate_limit_rps = EXCLUDED.rate_limit_rps,
				rate_limit_burst = EXCLUDED.rate_limit_burst,
				rate_limit_tpm = EXCLUDED.rate_limit_tpm,
				allowed_providers = EXCLUDED.allowed_providers,
				strip_reasoning = EXCLUDED.strip_reasoning`,
			v.Name, v.KeyHash, v.KeyPreview, v.RateLimitRPS, v.RateLimitBurst, v.RateLimitTPM, allowed, v.StripReasoning)
		if err != nil {
			return err
		}
	}
	return nil
}

func providerNameToID(ctx context.Context, q querier) (map[string]string, error) {
	rows, err := q.Query(ctx, `SELECT name, id FROM providers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var name, id string
		if err := rows.Scan(&name, &id); err != nil {
			return nil, err
		}
		out[name] = id
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// querier is the read surface shared by *pgxpool.Pool and pgx.Tx.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func nameSet(ctx context.Context, q querier, sql string) (map[string]struct{}, error) {
	rows, err := q.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out[s] = struct{}{}
	}
	return out, rows.Err()
}

func hashToName(ctx context.Context, q querier, sql string) (map[string]string, error) {
	rows, err := q.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var hash, name string
		if err := rows.Scan(&hash, &name); err != nil {
			return nil, err
		}
		out[hash] = name
	}
	return out, rows.Err()
}

func names[T any](items []T, key func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, key(it))
	}
	return out
}

// appriseSettingKeys are the alerting destination settings v1 leaves
// instance-local (the apprise endpoint + encrypted targets), so a member keeps
// its own alert routing even after a config sync.
var appriseSettingKeys = map[string]bool{
	"alert_apprise_api_url": true,
	"alert_apprise_targets": true,
}

// isSyncableSetting reports whether a settings key is replicated by config sync:
// it must be in the shared settings allowlist and not an instance-local apprise
// secret. Used on both ends (export, diff, apply) so a hand-crafted envelope
// cannot push a key this member would not itself export.
func isSyncableSetting(key string) bool {
	return settings.AllowedSettings[key] && !appriseSettingKeys[key]
}

// syncableSettingKeys returns the settings keys this member exports.
func syncableSettingKeys() []string {
	out := make([]string, 0, len(settings.AllowedSettings))
	for k := range settings.AllowedSettings {
		if isSyncableSetting(k) {
			out = append(out, k)
		}
	}
	return out
}

// writeJSONStatus writes v as JSON with an explicit status code.
func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		debuglog.Error("configsync: encode response", "error", err)
	}
}
