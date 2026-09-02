package api

import (
	"context"
	"errors"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/db"
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
// Synced: providers, virtual keys, the syncable settings subset, users, CUSTOM
// (user-created) failover groups, and the operator's per-model disables. Model
// rows themselves and AUTO-CREATED failover groups regenerate on each member from
// the synced providers (discovery + automatic group formation), so they are
// intentionally not copied. A custom group's priority_order / entry_enabled
// reference instance-local model UUIDs, so it is carried as stable (provider name,
// model_id) entry refs and resolved back to this member's model UUIDs on import
// (an entry whose model is absent here is dropped; a group left with fewer than
// two routable entries is skipped).
//
// Per-model disables travel by the same stable ref. Only the OPERATOR's disable
// (models.disabled_manually) is carried: discovery's disable and the proxy's
// traffic retirement are per-member evidence about what a provider served that
// member, so replicating them would turn one member's provider trouble into a
// fleet-wide outage. Migration 063 is what keeps the three kinds apart.
//
// The operator's manual-enable PINS (models.manually_enabled_at, migration 070)
// travel the same way and for the same reason: a pin says the operator tested a
// model the provider stopped listing and it serves, which is a statement about
// the provider, not about one member, so every member should honour it instead
// of disabling the model two scans after its own listing drops it.

const (
	// configSchemaVersion is the envelope version a member understands. An import
	// carrying a different version is refused rather than half-applied.
	//
	// v2 changed what an EXISTING field MEANS, which is why it moved even though
	// no field was added or removed. In v1 a virtual key's allowed_provider_names
	// was a plain list and an empty one was indistinguishable from an absent one;
	// in v2 it is a pointer, and present-but-empty means "restricted, but none of
	// its providers resolve on this member".
	//
	// The bump exists to protect the IMPORTING side, which this repo's fix could
	// not reach. A v1 member decodes the v2 [] into a zero-length slice and
	// applies its own guard, `len(v.AllowedProviderNames) > 0 && len(allowed) == 0`
	// (upsertVirtualKeys before this change): the first conjunct is false, so it
	// skips nothing and writes a nil allowed_providers, which is SQL NULL, which
	// the proxy reads as every provider. A stale-only restricted key would land on
	// that member wide open. Refusing the envelope outright is the only defence,
	// and configsync_import.go already does it with 422 + SchemaVersionOK: false.
	configSchemaVersion = 2

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
// /api/settings handler enforces (the reported CWE-918 SSRF bypass). Import
// maps it to a 400 refusal. Currently a standing guard with no live path: every
// url-typed setting (apprise + SSO) is instance-local and skipped before
// validation, so this fires only if a future url-typed setting joins the
// syncable set.
var errInvalidSyncedURL = errors.New("configsync: refusing to apply a setting with an invalid URL")

// errInvalidSyncedSettingBound is returned by apply when a syncable numeric
// setting in the envelope falls below the minimum the interactive PUT
// /api/settings handler enforces. Same class of hole as the per-key limits
// below, one level up and with a wider blast radius: rate_limit_ip_burst is
// min 1 interactively, and IPLimiter.getLimiter hands a negative one straight to
// rate.NewLimiter with no clamp, so every client IP is denied. Import maps it to
// a 400 refusal.
var errInvalidSyncedSettingBound = errors.New("configsync: refusing to apply a setting below its minimum")

// errInvalidSyncedRateLimit is returned by apply when a virtual key or user in
// the envelope carries a rate limit the interactive API would reject. The values
// are not merely cosmetic: rate_limit_tpm <= 0 is the TPMLimiter's "no cap"
// sentinel, so a negative one bought this member unmetered token spend past the
// global default, and a negative rate_limit_burst alongside a positive RPS makes
// rate.NewLimiter refuse every request (a per-key denial of service). Import
// maps it to a 400 refusal.
var errInvalidSyncedRateLimit = errors.New("configsync: refusing to apply an invalid rate limit")

// errInvalidSyncedProvider is returned by apply when a provider in the envelope
// carries a field the interactive API would reject: a max_in_flight outside
// 1..10000, which the runtime would read as "no ceiling" (zero or less) rather
// than reject, silently deleting the operator's cap and then exporting the
// value to every member on the next sync; a base_url its SSRF check refuses;
// an unprintable or over-long name; or a disable date that is not a date.
// Import maps it to a 400 refusal.
var errInvalidSyncedProvider = errors.New("configsync: refusing to apply an invalid provider")

// errInvalidSyncedPasswordHash is returned by apply when a user in the envelope
// carries a password_hash that is not a well-formed argon2id hash. A password
// hash is the one credential field this member does not compute itself, and
// login already fails closed on a malformed one, so this is not an
// authentication fix: it keeps an unusable hash out of the database instead of
// letting it surface later as an account that silently cannot log in. Import
// maps it to a 400 refusal.
//
// This refusal is deliberately harsher than the harm it prevents, and that is
// worth being explicit about. The neighbouring sentinels refuse envelopes that
// would be actively damaging to apply (an SSRF-capable URL, a rate limit that
// denies every request, a cap that silently widens). A malformed hash is inert
// by comparison, yet refusing the envelope stops providers, keys, settings and
// failover groups from converging too, fleet-wide, for as long as it persists.
// It is accepted because a legitimate primary only ever exports hashes it
// computed itself, so one that fails to parse means a corrupt or tampered
// envelope, and applying credentials from an envelope that has demonstrably
// been altered is the worse trade. Be clear about which half carries the
// argument: for the TAMPERED reading refusing is the point, but for plain
// CORRUPTION (a direct database write, a bad hash imported before this check
// existed on a member later promoted to primary) the refusal is pure cost with
// no security benefit. That case is why exportUsers checks the same encoding
// and raises configsync.malformed_password_hash at the source, so the fleet
// freeze is at least explained and fixable rather than silent.
var errInvalidSyncedPasswordHash = errors.New("configsync: refusing to apply a malformed password hash")

// errUnresolvableUserProviders is returned by apply when a user in the envelope
// carries a NON-EMPTY provider cap none of whose names resolve on this member.
// That is anomalous rather than merely inconvenient: providers are replaced
// declaratively earlier in the same transaction, so every name a legitimate
// primary exported resolves. Writing the user with a NULL cap would silently
// promote them from restricted to unrestricted, so the whole import is refused.
// Import maps it to a 400.
//
// Skipping the row is not the escape it looks like. For a user who does not yet
// exist on this member, skipping means usernameToID cannot find her afterwards,
// so upsertVirtualKeys imports every key she owns with owner_user_id NULL. The
// proxy then loads no Owner at all, never populates UserAllowedProvidersKey, and
// effectiveAllowedProviders takes its owner == nil arm, which drops the owner
// side of the intersection entirely: a key with no cap of its own ends up
// completely unrestricted. That is a wider escalation than the one being
// guarded. For a user who does exist, skipping is merely stale, leaving a
// pre-existing cap that may be broader than the primary now intends.
//
// A present-but-EMPTY cap is deliberately NOT this error: there the primary
// itself resolves nothing, and applyUsers writes the empty array through so the
// member reproduces the primary's deny-everything behaviour instead of wedging
// fleet sync on an ordinary provider deletion.
var errUnresolvableUserProviders = errors.New("configsync: refusing to apply a user whose provider cap does not resolve")

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
	// validateProviderURL guards imported provider base_urls with the same SSRF
	// check the interactive admin API applies on CreateProvider/UpdateProvider
	// (config.ValidateProviderURL): resolve DNS and reject loopback, RFC
	// 1918/ULA, link-local, CGNAT and cloud-metadata addresses (hosts in
	// ALLOWED_PROVIDER_HOSTS are exempted). Keeps a compromised primary from
	// persisting a base_url the admin API would refuse. Nil disables the check
	// (tests that do not exercise it pass nil).
	validateProviderURL func(string) error
}

// NewConfigSyncHandler builds the handler. masterKey is needed only to verify
// (on import) that this member can decrypt the incoming provider keys; the
// plaintext is never produced here. validateProviderURL applies the admin API's
// SSRF check to imported provider base_urls (see the field doc); production
// passes config.ValidateProviderURL, tests may pass nil to skip it.
func NewConfigSyncHandler(database *db.DB, settingsRepo SettingsStore, masterKey, appVersion string,
	discoverAll func(context.Context) error, validateProviderURL func(string) error) *ConfigSyncHandler {
	return &ConfigSyncHandler{
		db: database, settings: settingsRepo, masterKey: masterKey, appVersion: appVersion,
		discoverAll: discoverAll, validateProviderURL: validateProviderURL,
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
	// DisabledModels are the models the operator switched off by hand, by stable
	// ref. Same nil-vs-empty contract again: [] means "the primary has none, clear
	// yours", absent means an older primary whose per-model state must be left
	// alone.
	DisabledModels []ExportModelRef `json:"disabled_models"`
	// EnabledModels are the models the operator pinned enabled by hand while the
	// provider's listing omits them (models.manually_enabled_at), by stable ref.
	// Same nil-vs-empty contract as DisabledModels: [] means "the primary has no
	// pins, clear yours", absent means an older primary whose pins must be left
	// alone. Import force-enables and pins; reconcile only clears pins, never
	// disables, so a member's own listing-based disable machinery resumes.
	EnabledModels []ExportModelRef `json:"enabled_models"`
}

// ExportModelRef is a model's stable cross-member identity: the provider's name
// plus the provider-scoped model_id. The same pair a failover group's entries
// travel by, for the same reason (model UUIDs are instance-local).
type ExportModelRef struct {
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
}

// String renders a ref the way the gateway names that model everywhere else: the
// provider, a slash, then the provider-scoped id. Model ids routinely contain
// slashes themselves (meta-llama/Llama-3-70b), which makes this look ambiguous and
// is not: the proxy resolves an incoming name with SplitN(name, "/", 2)
// (proxy_request.go), so the first slash separates and the rest is the id. An
// operator reading openai/meta-llama/Llama-3-70b in an alert sees exactly the
// string they would send to /v1/chat/completions.
func (r ExportModelRef) String() string { return r.ProviderName + "/" + r.ModelID }

// ExportProvider is a provider with its encrypted key material verbatim.
type ExportProvider struct {
	Name                 string  `json:"name"`
	BaseURL              string  `json:"base_url"`
	ProviderType         string  `json:"provider_type"`
	Enabled              bool    `json:"enabled"`
	AutodiscoveryEnabled bool    `json:"autodiscovery_enabled"`
	EncryptedKey         []byte  `json:"encrypted_key,omitempty"`
	KeyNonce             []byte  `json:"key_nonce,omitempty"`
	KeySalt              []byte  `json:"key_salt,omitempty"`
	MaskedKey            *string `json:"masked_key,omitempty"`
	ScheduledDisableOn   *string `json:"scheduled_disable_on,omitempty"`
	MaxInFlight          *int    `json:"max_in_flight,omitempty"`
}

// ExportVK is a virtual key carried by its hash (the plaintext never existed
// server-side). allowed_providers is carried as provider NAMES, resolved back to
// this member's provider UUIDs on import.
type ExportVK struct {
	Name           string   `json:"name"`
	KeyHash        string   `json:"key_hash"`
	KeyPreview     string   `json:"key_preview"`
	RateLimitRPS   *float64 `json:"rate_limit_rps,omitempty"`
	RateLimitBurst *int     `json:"rate_limit_burst,omitempty"`
	RateLimitTPM   *int     `json:"rate_limit_tpm,omitempty"`
	// AllowedProviderNames carries the key's provider restriction by NAME
	// (UUIDs are instance-local). Three distinct states, and the distinction is
	// load-bearing:
	//   nil            - no restriction, every provider
	//   ["openai"]     - restricted, and the names resolve here
	//   [] (non-nil)   - restricted, but NOTHING resolves on this member
	// Collapsing the last two is a privilege escalation: a key whose providers
	// were all deleted would import as unrestricted.
	//
	// Two separate JSON mechanisms keep those states distinct on the wire.
	// Marshalling: omitempty tests the POINTER, not the slice length, so a
	// present-but-empty restriction is emitted as [] rather than dropped.
	// Unmarshalling: an absent field leaves the zero value untouched, so an
	// older primary that never emits the field yields nil and reads as
	// unrestricted, exactly as it did before this became a pointer.
	AllowedProviderNames *[]string `json:"allowed_provider_names,omitempty"`
	StripReasoning       bool      `json:"strip_reasoning"`
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
	// AllowedProviderNames carries the account provider cap by NAME, with the
	// same three-state contract as ExportVK.AllowedProviderNames (nil = no cap,
	// non-empty = capped and resolves, present-but-empty = capped with nothing
	// resolving on the exporting member). Where a key with an unresolvable cap
	// is skipped, a user cannot be: skipping a user this member does not have
	// yet makes her keys import unowned, which removes the owner side of the
	// proxy's cap intersection outright (see errUnresolvableUserProviders). So
	// applyUsers splits the third state by which side failed to resolve - a
	// present-but-empty list is written through as an empty array (the primary
	// itself resolves nothing, and an empty cap denies everything), while names
	// that arrive non-empty and resolve to nothing are anomalous and refuse the
	// import. Writing NULL is never an option: it would promote a capped account
	// to unrestricted.
	AllowedProviderNames *[]string `json:"allowed_provider_names,omitempty"`
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
	// Incomplete is true when the import committed but this member could not
	// materialise all of it. Named for the failure so an older member that omits
	// the field decodes to false and reads as complete.
	Incomplete bool `json:"incomplete,omitempty"`
	// Unapplied names the custom failover groups this member did not build.
	Unapplied []string `json:"unapplied,omitempty"`
	// Partial names custom failover groups this member built with fewer entries
	// than the primary sent, so it fails over across fewer providers for those
	// models. Reported alongside Incomplete, never as part of it: the member
	// applied everything it was asked to.
	Partial []string `json:"partial,omitempty"`
	// UnappliedModels names the per-model intent this member could not apply, as
	// provider/model_id, because it holds no such model: the primary's disables and
	// its manual-enable pins alike. Reported alongside Incomplete for the same
	// reason as Partial, and it is what explains a config hash that will keep
	// differing until this member discovers those models.
	UnappliedModels []string `json:"unapplied_models,omitempty"`
	// ModelStateFailed is true when a per-model reconcile (disables or pins) failed
	// outright, so this member is still routing to models the primary switched off,
	// or still auto-disabling ones it pinned. It is one of the two things Incomplete
	// can mean, and without it the reader cannot tell which: an unbuilt failover
	// group names itself in Unapplied, but a failed reconcile has no names to give,
	// and was reported as a group failure.
	ModelStateFailed bool `json:"model_state_failed,omitempty"`
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

// ssoInstanceLocalKeys are the SSO provider settings each member keeps to
// itself: which IdPs this member offers and how it reaches them. Per-member so
// a fleet can enable an IdP on some members and not others, and because the
// public base URL (the IdP callback) is inherently this member's own address.
// The email allowlists are deliberately NOT here: who may log in is an ACL,
// and ACL drift across members is how a revoked account keeps access - the
// allowlists stay fleet-synced alongside the user accounts they bind to.
var ssoInstanceLocalKeys = map[string]bool{
	"oidc_enabled":           true,
	"oidc_issuer_url":        true,
	"oidc_client_id":         true,
	"oidc_client_secret":     true,
	"oidc_public_base_url":   true,
	"github_sso_enabled":     true,
	"github_client_id":       true,
	"github_client_secret":   true,
	"github_public_base_url": true,
}

// isSyncableSetting reports whether a settings key is replicated by config sync:
// it must be in the shared settings allowlist and not an instance-local apprise
// secret, session preference, or per-member SSO provider setting. Used on both
// ends (export, diff, apply) so a hand-crafted envelope cannot push a key this
// member would not itself export.
func isSyncableSetting(key string) bool {
	return settings.AllowedSettings[key] &&
		!appriseSettingKeys[key] &&
		!ssoInstanceLocalKeys[key] &&
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
