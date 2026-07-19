package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
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

// errWouldWipeProviders is returned by apply when the envelope carries zero
// providers but this member currently has providers: applying the declarative
// replace would delete every provider (and, via the users replace, is the
// reported backdoor-wipe vector). Import maps it to a 400 refusal.
var errWouldWipeProviders = errors.New("configsync: refusing to wipe every provider off a populated member")

// errInvalidSyncedURL is returned by apply when a syncable url-typed setting in
// the envelope fails the same netguard validation the interactive PUT
// /api/settings handler enforces. Without it a compromised primary could push an
// oidc_issuer_url the interactive endpoint rejects, which the gateway later
// fetches during OIDC discovery / token exchange (reported SSRF bypass,
// CWE-918). Import maps it to a 400 refusal.
var errInvalidSyncedURL = errors.New("configsync: refusing to apply a setting with an invalid URL")

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
	// OwnerUsername carries key ownership by username (user ids are
	// instance-local; usernames are the users sync key). Nil = unowned. An
	// owner that does not resolve on the member imports as unowned rather
	// than failing the sync.
	OwnerUsername *string `json:"owner_username,omitempty"`
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
	// Aggregate per-user proxy limits (phase 2 of multi-user).
	RateLimitRPS   *float64 `json:"rate_limit_rps,omitempty"`
	RateLimitBurst *int     `json:"rate_limit_burst,omitempty"`
	RateLimitTPM   *int     `json:"rate_limit_tpm,omitempty"`
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
