package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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
// v1 syncs providers, virtual keys, the syncable settings subset, and CUSTOM
// (user-created) failover groups. Models and AUTO-CREATED failover groups
// regenerate on each member from the synced providers (discovery + automatic
// group formation), so they are intentionally not copied. A custom group's
// priority_order / entry_enabled reference instance-local model UUIDs, so it is
// carried as stable (provider name, model_id) entry refs and resolved back to
// this member's model UUIDs on import (an entry whose model is absent here is
// dropped; a group left with fewer than two routable entries is skipped). The
// remaining manual override (per-model disable) is still a documented follow-up.

const (
	// configSchemaVersion is the envelope version a member understands. An import
	// carrying a different version is refused rather than half-applied.
	configSchemaVersion = 1

	// maxConfigImportBody bounds an import payload. Fleet config is small (a
	// handful of providers + keys); 8 MiB is generous and caps a hostile body.
	maxConfigImportBody = 8 << 20

	// fleetSourceGenHeader carries Front Desk's monotonic source generation
	// (its auto_sync_gen) on a real import. It is the member-side commit fence:
	// an import whose generation is older than the highest this member has
	// applied is refused, so a stale push that was already in flight when the
	// primary was repointed cannot land after the fresh one. The header is
	// optional: an older Front Desk omits it and the import applies unfenced
	// (the pre-fence behaviour), and an older member ignores it, so the fence
	// engages only when both ends understand it. Never set on a dry run.
	fleetSourceGenHeader = "X-Fleet-Source-Gen"

	// fleetSourceGenLock is the Postgres advisory-lock key that serializes
	// fenced imports on this member, so the read-current-generation / reject-or-
	// advance step is atomic against a concurrent import (two pushes whose bytes
	// both arrived before either committed). It is transaction-scoped, released
	// when the import's transaction ends. The value is an arbitrary fixed
	// constant; it only has to be unique within this app's advisory-lock use,
	// and config sync is the only advisory lock taken.
	fleetSourceGenLock int64 = 0x4D48_5F46_454E_4331 // "MH_FENC1"
)

// errStaleSourceGen is returned by apply when the incoming source generation is
// older than the one this member last applied, so Import answers with a benign
// "superseded" response instead of a 500.
var errStaleSourceGen = errors.New("configsync: import source generation is older than last applied")

// ConfigSyncHandler serves the member-side config export/import endpoints. It is
// mounted inside the admin-authenticated /api group, so every call already
// requires the admin token (or a session when TOTP is on): a caller able to
// import config controls the data plane, so no weaker gate is acceptable.
type ConfigSyncHandler struct {
	db         *db.DB
	settings   SettingsStore
	masterKey  string
	appVersion string
	// discoverAll runs model discovery on this member after an import commits its
	// providers, so custom failover groups can resolve. Nil disables it (tests
	// that seed models directly pass nil).
	discoverAll func(context.Context) error
}

// NewConfigSyncHandler builds the handler. masterKey is needed only to verify
// (on import) that this member can decrypt the incoming provider keys; the
// plaintext is never produced here.
func NewConfigSyncHandler(database *db.DB, settingsRepo SettingsStore, masterKey, appVersion string,
	discoverAll func(context.Context) error) *ConfigSyncHandler {
	return &ConfigSyncHandler{
		db: database, settings: settingsRepo, masterKey: masterKey, appVersion: appVersion, discoverAll: discoverAll,
	}
}

// Register mounts GET/POST /config/{export,import} and GET /config/version. The
// parent router must apply admin auth (see type doc).
func (h *ConfigSyncHandler) Register(r chi.Router) {
	r.Get("/config/export", h.Export)
	r.Get("/config/version", h.Version)
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
	// Not omitempty: a member running this code always emits the key, as [] when it
	// has no custom groups. That lets import tell "primary genuinely has zero custom
	// groups" (present empty array, reconcile to zero) apart from "envelope predates
	// this field" (key absent, decodes to nil, leave the member's groups alone).
	FailoverGroups []ExportFailoverGroup `json:"failover_groups"`
	// Same nil-vs-empty contract as FailoverGroups: always emitted by a member
	// running this code ([] when there are no accounts), absent in an envelope
	// from an older primary (decodes to nil, import leaves users alone).
	Users []ExportUser `json:"users"`
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

// ExportFailoverGroup is a CUSTOM (non-auto-created) failover group. Its
// priority_order / entry_enabled reference instance-local model UUIDs, so it is
// carried as ordered (provider name, model_id) entry refs, resolved back to this
// member's model UUIDs on import. Auto-created groups are excluded: they
// regenerate identically on every member from the synced providers.
type ExportFailoverGroup struct {
	DisplayModel string                `json:"display_model"`
	DisplayName  *string               `json:"display_name,omitempty"`
	Description  string                `json:"description,omitempty"`
	GroupEnabled bool                  `json:"group_enabled"`
	Entries      []ExportFailoverEntry `json:"entries"`
}

// ExportFailoverEntry is one member of a failover group, identified by the stable
// (provider name, model_id) pair rather than the instance-local model UUID.
type ExportFailoverEntry struct {
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
	Enabled      bool   `json:"enabled"`
}

// ExportUser is a dashboard user account, keyed by username. The password
// hash travels verbatim: it is argon2id-encoded (never plaintext) and the
// whole envelope only moves between admin-authenticated fleet members.
// Deliberately NOT wrapped in MASTER_KEY encryption for transit: the envelope
// uniformly carries what the DB stores (provider keys travel as ciphertext
// only because they are encrypted at rest), the identical bytes ride the
// pg_dump backup at the same trust boundary, and argon2id is the one field
// here actually designed to survive exfiltration (VK sha256 hashes are the
// weaker neighbours).
type ExportUser struct {
	Username     string   `json:"username"`
	DisplayName  string   `json:"display_name,omitempty"`
	Email        *string  `json:"email,omitempty"`
	PasswordHash string   `json:"password_hash"`
	Role         string   `json:"role"`
	Grants       []string `json:"grants"`
	Enabled      bool     `json:"enabled"`
}

// entityDiff lists the names changed for one entity kind in a sync.
type entityDiff struct {
	Added   []string `json:"added"`
	Updated []string `json:"updated"`
	Removed []string `json:"removed"`
}

// configDiff is the per-kind summary returned by a (dry-run or applied) import.
type configDiff struct {
	Providers      entityDiff `json:"providers"`
	VirtualKeys    entityDiff `json:"virtual_keys"`
	Settings       entityDiff `json:"settings"`
	FailoverGroups entityDiff `json:"failover_groups"`
	Users          entityDiff `json:"users"`
}

// importResponse is the body of POST /config/import.
type importResponse struct {
	SchemaVersionOK bool `json:"schema_version_ok"`
	MasterKeyOK     bool `json:"master_key_ok"`
	Applied         bool `json:"applied"`
	// Stale is true when the import was refused by the commit fence because its
	// source generation was older than the one already applied. It is a benign,
	// expected outcome (a newer config won), not a failure: SchemaVersionOK and
	// MasterKeyOK are still true, Applied is false, and nothing was written.
	Stale bool       `json:"stale,omitempty"`
	Diff  configDiff `json:"diff"`
}

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

// Export returns this member's full config envelope so Front Desk can replicate
// it onto the fleet.
func (h *ConfigSyncHandler) Export(w http.ResponseWriter, r *http.Request) {
	env, err := h.buildEnvelope(r.Context())
	if err != nil {
		debuglog.Error("configsync: build export envelope", "error", err)
		http.Error(w, "could not export config", http.StatusInternalServerError)
		return
	}
	writeJSON(w, env)
}

// Version returns a stable content hash of this member's syncable config, so
// Front Desk's auto-sync poller can cheaply detect that the primary's config
// changed without pulling and diffing the full export every tick. The hash
// covers only the Config payload (providers, virtual keys, syncable settings,
// custom failover groups, users), never the volatile envelope fields
// (exported_at), so
// it changes if and only if a synced entity changed. Same auth as Export.
func (h *ConfigSyncHandler) Version(w http.ResponseWriter, r *http.Request) {
	env, err := h.buildEnvelope(r.Context())
	if err != nil {
		debuglog.Error("configsync: build version envelope", "error", err)
		http.Error(w, "could not read config", http.StatusInternalServerError)
		return
	}
	// Marshal only the Config payload: providers come out ORDER BY name, virtual
	// keys ORDER BY created_at, failover groups ORDER BY display_model, and the
	// settings map is key-sorted by encoding/json, so the bytes are deterministic
	// for an unchanged config and the hash is stable across calls.
	payload, err := json.Marshal(env.Config)
	if err != nil {
		debuglog.Error("configsync: marshal config for version", "error", err)
		http.Error(w, "could not read config", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(payload)
	writeJSON(w, map[string]string{"version": hex.EncodeToString(sum[:])})
}

// buildEnvelope reads this member's full config (providers with key ciphertext,
// virtual keys with provider-name-translated allow-lists, syncable settings) into
// an envelope. Any read failure aborts with the underlying error.
func (h *ConfigSyncHandler) buildEnvelope(ctx context.Context) (ConfigEnvelope, error) {
	pool := h.db.Pool()
	idToName, err := h.providerIDToName(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	providers, err := exportProviders(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	vks, err := exportVirtualKeys(ctx, pool, idToName)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	set, err := exportSettings(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	refByUUID, err := modelRefByUUID(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	groups, err := exportFailoverGroups(ctx, pool, refByUUID)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	users, err := exportUsers(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	return ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		AppVersion:    h.appVersion,
		ExportedAt:    time.Now().UTC(),
		Config: ConfigPayload{
			Providers: providers, VirtualKeys: vks, Settings: set, FailoverGroups: groups,
			Users: users,
		},
	}, nil
}

// modelRef is the stable cross-member identity of a model: the provider's name
// plus the provider-scoped model_id. (provider_id, model_id) is unique per
// member, but the UUIDs differ, so failover entries travel by this pair.
type modelRef struct {
	provider string
	modelID  string
}

// modelRefByUUID maps each local model UUID to its stable (provider, model_id)
// ref, for translating a failover group's UUID entries out on export.
func modelRefByUUID(ctx context.Context, q querier) (map[string]modelRef, error) {
	rows, err := q.Query(ctx,
		`SELECT m.id, p.name, m.model_id FROM models m JOIN providers p ON m.provider_id = p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]modelRef{}
	for rows.Next() {
		var id, provider, modelID string
		if err := rows.Scan(&id, &provider, &modelID); err != nil {
			return nil, err
		}
		out[id] = modelRef{provider: provider, modelID: modelID}
	}
	return out, rows.Err()
}

// exportFailoverGroups reads every CUSTOM (auto_created = false) failover group
// and carries each as ordered (provider, model_id) entry refs. Auto-created
// groups are skipped: they regenerate identically on each member. An entry whose
// model UUID no longer resolves (model deleted) is dropped; the group is still
// exported so the importer can decide whether enough entries survive.
func exportFailoverGroups(ctx context.Context, q querier, refByUUID map[string]modelRef) ([]ExportFailoverGroup, error) {
	// description is COALESCEd because the main app's failover Upsert lists the
	// column with a *string value, so a nil description writes a SQL NULL (the
	// column DEFAULT '' only applies when the column is omitted). Every other read
	// in the failover package COALESCEs it; without this, a custom group with a
	// NULL description fails the Scan into g.Description and kills the whole export.
	rows, err := q.Query(ctx, `
		SELECT display_model, display_name, COALESCE(description, ''), COALESCE(group_enabled, true),
		       priority_order, COALESCE(entry_enabled, '{}')
		FROM model_failover_groups WHERE auto_created = false ORDER BY display_model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportFailoverGroup{}
	for rows.Next() {
		var g ExportFailoverGroup
		// priority_order / entry_enabled are JSONB stored as marshaled JSON, so
		// scan into bytes and unmarshal (matching failover.GetByModel).
		var priorityJSON, entryEnabledJSON []byte
		if err := rows.Scan(&g.DisplayModel, &g.DisplayName, &g.Description, &g.GroupEnabled,
			&priorityJSON, &entryEnabledJSON); err != nil {
			return nil, err
		}
		var priority []string
		if err := json.Unmarshal(priorityJSON, &priority); err != nil {
			return nil, err
		}
		entryEnabled := map[string]bool{}
		if len(entryEnabledJSON) > 0 {
			if err := json.Unmarshal(entryEnabledJSON, &entryEnabled); err != nil {
				return nil, err
			}
		}
		for _, uuidStr := range priority {
			ref, ok := refByUUID[uuidStr]
			if !ok {
				continue // model deleted since the group referenced it
			}
			// entry_enabled absence means enabled (matches proxy/enabledEntryIDs).
			enabled := true
			if v, ok := entryEnabled[uuidStr]; ok {
				enabled = v
			}
			g.Entries = append(g.Entries, ExportFailoverEntry{
				ProviderName: ref.provider, ModelID: ref.modelID, Enabled: enabled,
			})
		}
		out = append(out, g)
	}
	return out, rows.Err()
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

// exportUsers carries every dashboard user account, keyed by username. The
// argon2id password hash rides verbatim so a member can authenticate the same
// credentials; grants and role port as-is (no instance-local IDs involved).
func exportUsers(ctx context.Context, q querier) ([]ExportUser, error) {
	rows, err := q.Query(ctx, `
		SELECT username, display_name, email, password_hash, role, grants, enabled
		FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportUser{}
	for rows.Next() {
		var u ExportUser
		if err := rows.Scan(&u.Username, &u.DisplayName, &u.Email, &u.PasswordHash,
			&u.Role, &u.Grants, &u.Enabled); err != nil {
			return nil, err
		}
		out = append(out, u)
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
	if len(env.Config.Providers) == 0 && len(env.Config.VirtualKeys) == 0 &&
		len(env.Config.Settings) == 0 {
		// A config with no providers, virtual keys, or settings is almost always a
		// mistake, and applying it would delete everything on the target. Refuse
		// rather than wipe. Note we deliberately do NOT let a non-empty
		// failover_groups rescue such an envelope from this guard: groups reference
		// models which reference providers, so zero providers means the groups are
		// unresolvable, yet apply would still run the declarative provider/VK
		// deletes against empty lists and wipe the member clean.
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
		// A dry run is read-only and is never fenced: Front Desk relies on it to
		// preview the diff before deciding to snapshot and import.
		writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: false, Diff: diff})
		return
	}

	// Commit fence: a real import may carry Front Desk's monotonic source
	// generation. apply rejects one older than this member last applied, and
	// advances the marker atomically with the write, so an out-of-order push
	// cannot clobber a newer config. The header is absent for an older Front
	// Desk, in which case sourceGen is nil and the import applies unfenced.
	sourceGen := parseSourceGen(r.Header.Get(fleetSourceGenHeader))
	switch err := h.apply(ctx, env, sourceGen); {
	case errors.Is(err, errStaleSourceGen):
		// Benign: a newer generation already won on this member (or an un-versioned
		// push arrived after one had). Report it as a non-applied, non-error outcome
		// so Front Desk does not surface a failure.
		debuglog.Debug("configsync: refused stale import", "source_gen", sourceGenLabel(sourceGen))
		writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: false, Stale: true, Diff: diff})
		return
	case err != nil:
		debuglog.Error("configsync: apply import", "error", err)
		http.Error(w, "could not apply config", http.StatusInternalServerError)
		return
	}

	writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: true, Diff: diff})
}

// parseSourceGen reads the optional fleet source-generation header. It returns
// nil when the header is absent or unparseable, so a malformed or missing value
// degrades to an unfenced import rather than rejecting a legitimate push.
func parseSourceGen(raw string) *int64 {
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		debuglog.Warn("configsync: ignoring unparseable source-generation header", "value", raw)
		return nil
	}
	return &n
}

// sourceGenLabel renders an optional source generation for logs without
// dereferencing a nil (a headerless import has none).
func sourceGenLabel(gen *int64) string {
	if gen == nil {
		return "none"
	}
	return strconv.FormatInt(*gen, 10)
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

	// Custom failover groups, scoped to auto_created = false to match the apply
	// side (auto groups regenerate per member and are never synced). The counts
	// reflect intent: a group the importer later skips for too few resolvable
	// entries on this member still shows as added/updated here.
	curGroups, err := nameSet(ctx, pool, `SELECT display_model FROM model_failover_groups WHERE auto_created = false`)
	if err != nil {
		return d, err
	}
	wantGroups := map[string]struct{}{}
	for _, g := range env.Config.FailoverGroups {
		wantGroups[g.DisplayModel] = struct{}{}
		if _, ok := curGroups[g.DisplayModel]; ok {
			d.FailoverGroups.Updated = append(d.FailoverGroups.Updated, g.DisplayModel)
		} else {
			d.FailoverGroups.Added = append(d.FailoverGroups.Added, g.DisplayModel)
		}
	}
	// Mirror applyFailoverGroups exactly: a nil slice means the field was absent (a
	// pre-PR primary), which apply leaves untouched, so report no removals here. An
	// explicit empty array reconciles to zero, so its removals are real. Reporting
	// removals for a nil slice would scare an operator mid-rolling-upgrade with
	// deletions the apply never performs.
	if env.Config.FailoverGroups != nil {
		for name := range curGroups {
			if _, ok := wantGroups[name]; !ok {
				d.FailoverGroups.Removed = append(d.FailoverGroups.Removed, name)
			}
		}
	}

	// Users, keyed by username, with the same nil-guard as failover groups: a
	// nil slice means the envelope predates the field and apply leaves users
	// alone, so report no removals either.
	curUsers, err := nameSet(ctx, pool, `SELECT username FROM users`)
	if err != nil {
		return d, err
	}
	wantUsers := map[string]struct{}{}
	for _, u := range env.Config.Users {
		wantUsers[u.Username] = struct{}{}
		if _, ok := curUsers[u.Username]; ok {
			d.Users.Updated = append(d.Users.Updated, u.Username)
		} else {
			d.Users.Added = append(d.Users.Added, u.Username)
		}
	}
	if env.Config.Users != nil {
		for name := range curUsers {
			if _, ok := wantUsers[name]; !ok {
				d.Users.Removed = append(d.Users.Removed, name)
			}
		}
	}
	return d, nil
}

// apply converges this member to the envelope in one transaction, enforcing the
// commit fence. Under a transaction-scoped advisory lock (so concurrent imports
// cannot interleave their read-and-decide), it reads the highest source
// generation this member has applied and:
//
//   - sourceGen present (a fenced push): refuses with errStaleSourceGen if it is
//     older than the marker, otherwise applies and advances the marker in the same
//     transaction as the config write;
//   - sourceGen absent (a pre-fence Front Desk, which sends no header): applies
//     only while the marker is unset (a member no fenced push has touched), and is
//     refused once any fenced generation has been recorded. An un-versioned write
//     must never overwrite versioned config, or it could leave the member on old
//     config while the marker claims a newer generation already applied.
//
// The lock is taken for every import, headed or not, so a headerless push cannot
// slip past a generation that already committed. That, plus the same-transaction
// advance, is what makes a newer config win regardless of the order pushes arrive.
func (h *ConfigSyncHandler) apply(ctx context.Context, env ConfigEnvelope, sourceGen *int64) error {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// pg_advisory_xact_lock blocks a concurrent import's fence step until this
	// transaction ends, so the read-current / reject-or-advance below is atomic
	// even when two pushes' bytes both arrived before either committed. Released
	// automatically on commit or rollback.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, fleetSourceGenLock); err != nil {
		return err
	}
	last, fenced, err := readAppliedSourceGen(ctx, tx)
	if err != nil {
		return err
	}
	switch {
	case sourceGen != nil:
		if fenced && *sourceGen < last {
			return errStaleSourceGen // a newer generation already applied; refuse
		}
	case fenced:
		// Headerless (pre-fence) import onto a member any fenced generation already
		// converged (including generation 0): applying an un-versioned write now
		// could clobber that config and leave the marker lying. Refuse; the fenced
		// source reconverges.
		return errStaleSourceGen
	}

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
	if err := applyUsers(ctx, tx, env.Config.Users); err != nil {
		return err
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

	if sourceGen != nil {
		// Advance the fence marker in the same transaction as the config write, so
		// the commit that applies this generation's config and the record that it
		// was applied are atomic. A raw upsert (not settings.SetTx) because the
		// _fleet_* keys are deliberately outside the SetTx allowlist; the value is
		// monotonic because an older generation was already rejected above.
		if err := writeAppliedSourceGen(ctx, tx, *sourceGen); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Stamp the HA synced marker AFTER the commit, via Set (not SetTx): this
	// instance-local, non-syncable key drives the member dashboard's "synced
	// from primary" readout. It must be written post-commit and through Set
	// because SetTx enforces the settings allowlist, which _fleet_* keys are
	// deliberately absent from (so the declarative replace above never touches
	// them). A failure here is non-fatal: the config is already durable.
	if err := h.settings.Set(ctx, keyFleetConfigSyncedAt, time.Now().UTC().Format(time.RFC3339)); err != nil {
		debuglog.Warn("configsync: failed to stamp fleet synced marker", "error", err)
	}

	// Core config (providers, virtual keys, settings) is now durable. The writes
	// bypassed the in-memory caches, so drop them: the proxy must see the new
	// providers/keys and discovery must re-read providers.
	provider.InvalidateProviderCache()
	model.InvalidateModelCache()

	// Populate this member's models so custom failover groups can resolve. The
	// "discover on provider creation" default is a dashboard action this raw
	// import bypasses, and scheduled discovery may be off, so without this a
	// freshly-synced member would have providers but no models, and hotel/<group>
	// would route to nothing until a restart or a manual discover. Best-effort:
	// the core config already committed, and groups reconcile on the next sync.
	if h.discoverAll != nil {
		if err := h.discoverAll(ctx); err != nil {
			debuglog.Warn("configsync: post-import discovery failed; custom failover groups may not resolve until models exist", "error", err)
		}
	}

	// Custom failover groups, in their own transaction now that discovery has had
	// a chance to create the models their entries reference. Best-effort for the
	// same reason: a group that cannot resolve yet reconciles on the next sync.
	if err := h.applyFailoverGroups(ctx, env.Config.FailoverGroups); err != nil {
		debuglog.Warn("configsync: failed to apply custom failover groups", "error", err)
	}

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

// applyFailoverGroups upserts the custom failover groups and declaratively
// removes custom groups absent from the envelope, in a dedicated transaction.
// It runs after the core-config commit and after discovery, so the models the
// entries reference exist. Auto-created groups are never touched. The declarative
// delete keeps a group still named in the envelope even if it was just skipped
// for too few resolvable entries, so a transient model gap does not delete the
// operator's group.
func (h *ConfigSyncHandler) applyFailoverGroups(ctx context.Context, groups []ExportFailoverGroup) error {
	// Distinguish "field absent" from "explicitly empty". A nil slice means the
	// envelope carried no failover_groups key at all (a pre-PR primary), so leave
	// the member's own custom groups untouched rather than wiping them on the first
	// sync of a rolling upgrade. A non-nil empty slice means a current primary that
	// genuinely has zero custom groups, which must reconcile: the declarative delete
	// below then removes every stale custom group the member still has.
	if groups == nil {
		return nil
	}
	tx, err := h.db.Pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := upsertFailoverGroups(ctx, tx, groups); err != nil {
		return err
	}
	groupNames := names(groups, func(g ExportFailoverGroup) string { return g.DisplayModel })
	if _, err := tx.Exec(ctx,
		`DELETE FROM model_failover_groups WHERE auto_created = false AND display_model <> ALL($1)`,
		groupNames); err != nil {
		return err
	}
	return tx.Commit(ctx)
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

// applyUsers converges the users table to the envelope, keyed by username. A
// nil slice means the envelope predates the field: leave this member's users
// alone (same contract as failover groups). Sequence matters: delete absent
// users first, then blank all remaining emails, then upsert. The blanking step
// lets an email move between two surviving accounts without tripping the
// unique index mid-upsert (row-by-row upserts would otherwise 23505 on a
// swap). Sessions of removed or disabled users die at the auth middleware,
// which re-checks the users row on every request.
func applyUsers(ctx context.Context, tx pgx.Tx, users []ExportUser) error {
	if users == nil {
		return nil
	}
	usernames := names(users, func(u ExportUser) string { return u.Username })
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE username <> ALL($1)`, usernames); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET email = NULL`); err != nil {
		return err
	}
	for _, u := range users {
		grants := u.Grants
		if grants == nil {
			grants = []string{}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (username, display_name, email, password_hash, role, grants, enabled)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (username) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				email = EXCLUDED.email,
				password_hash = EXCLUDED.password_hash,
				role = EXCLUDED.role,
				grants = EXCLUDED.grants,
				enabled = EXCLUDED.enabled,
				updated_at = now()`,
			u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, grants, u.Enabled); err != nil {
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

// upsertFailoverGroups re-creates each custom failover group on this member by
// resolving its (provider, model_id) entry refs back to local model UUIDs. An
// entry whose model is not present here is dropped; a group left with fewer than
// two routable entries is skipped (a one-member failover group is meaningless,
// matching pruneStaleEntries). Always writes auto_created = false.
func upsertFailoverGroups(ctx context.Context, tx pgx.Tx, groups []ExportFailoverGroup) error {
	if len(groups) == 0 {
		return nil
	}
	// (provider, model_id) -> local model UUID. Built inside the transaction so
	// it reflects the just-synced provider set (deleted providers cascade-removed
	// their models). Models themselves come from each member's discovery.
	localUUID := map[string]string{}
	rows, err := tx.Query(ctx,
		`SELECT p.name, m.model_id, m.id FROM models m JOIN providers p ON m.provider_id = p.id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var provider, modelID, id string
		if err := rows.Scan(&provider, &modelID, &id); err != nil {
			rows.Close()
			return err
		}
		localUUID[provider+"\x00"+modelID] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, g := range groups {
		priority := make([]string, 0, len(g.Entries))
		entryEnabled := map[string]bool{}
		for _, e := range g.Entries {
			id, ok := localUUID[e.ProviderName+"\x00"+e.ModelID]
			if !ok {
				continue // model absent on this member (not discovered yet, or removed)
			}
			priority = append(priority, id)
			entryEnabled[id] = e.Enabled
		}
		if len(priority) < 2 {
			debuglog.Warn("configsync: skipping custom failover group with too few resolvable entries",
				"group", g.DisplayModel, "resolved", len(priority), "wanted", len(g.Entries))
			continue
		}
		priorityJSON, err := json.Marshal(priority)
		if err != nil {
			return err
		}
		entryEnabledJSON, err := json.Marshal(entryEnabled)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO model_failover_groups
				(display_model, priority_order, entry_enabled, group_enabled, display_name, description, auto_created)
			VALUES ($1, $2, $3, $4, $5, $6, false)
			ON CONFLICT (display_model) DO UPDATE SET
				priority_order = EXCLUDED.priority_order,
				entry_enabled  = EXCLUDED.entry_enabled,
				group_enabled  = EXCLUDED.group_enabled,
				display_name   = EXCLUDED.display_name,
				description    = EXCLUDED.description,
				auto_created   = false,
				updated_at     = now()`,
			g.DisplayModel, priorityJSON, entryEnabledJSON, g.GroupEnabled, g.DisplayName, g.Description); err != nil {
			return err
		}
	}
	return nil
}

// readAppliedSourceGen returns the highest Front Desk source generation this
// member has applied and whether a marker row exists at all, read inside the
// import transaction. present is the signal the fence keys on: a generation of 0
// is a real applied generation (the wizard can sync at auto_sync_gen 0), so it
// must be distinguished from "never fenced" rather than collapsed to the same
// zero. A missing row reports present=false; an unparseable value reports
// present=true at a floor of 0, so the corrupt marker still fences out a
// header-less write yet a fresh fenced import can rewrite a clean value.
func readAppliedSourceGen(ctx context.Context, tx pgx.Tx) (gen int64, present bool, err error) {
	var raw string
	switch scanErr := tx.QueryRow(ctx, `SELECT value FROM settings WHERE key = $1`, keyFleetLastSourceGen).Scan(&raw); {
	case errors.Is(scanErr, pgx.ErrNoRows):
		return 0, false, nil
	case scanErr != nil:
		return 0, false, scanErr
	}
	n, parseErr := strconv.ParseInt(raw, 10, 64)
	if parseErr != nil {
		// Deliberate: a corrupt marker floors to 0 but stays present, so a header-
		// less write is still refused and a fenced import rewrites a clean value,
		// rather than wedging the fence on a 500 forever.
		debuglog.Warn("configsync: unparseable stored source generation, flooring to 0", "value", raw)
		return 0, true, nil //nolint:nilerr // intentional: corrupt marker floors but stays present
	}
	return n, true, nil
}

// writeAppliedSourceGen records gen as the highest applied source generation,
// upserting the _fleet_last_source_gen row directly (the key is outside the
// SetTx allowlist). Called inside the import transaction so the marker advances
// atomically with the config it certifies.
func writeAppliedSourceGen(ctx context.Context, tx pgx.Tx, gen int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		keyFleetLastSourceGen, strconv.FormatInt(gen, 10))
	return err
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

// sessionIdleTimeoutKey is the dashboard auto-logout window. It is a per-instance
// admin-session preference (each deployment's operators choose their own idle
// timeout), so config sync leaves it instance-local like the apprise routing
// secrets above: a managed member keeps and can edit its own value.
const sessionIdleTimeoutKey = "session_idle_timeout_minutes"

// isSyncableSetting reports whether a settings key is replicated by config sync:
// it must be in the shared settings allowlist and not an instance-local apprise
// secret or session preference. Used on both ends (export, diff, apply) so a
// hand-crafted envelope cannot push a key this member would not itself export.
func isSyncableSetting(key string) bool {
	return settings.AllowedSettings[key] &&
		!appriseSettingKeys[key] &&
		key != sessionIdleTimeoutKey
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
