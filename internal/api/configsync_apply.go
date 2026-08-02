package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/netguard"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

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

	if err := enforceSourceGenFence(ctx, tx, sourceGen); err != nil {
		return err
	}
	if err := guardAgainstProviderWipe(ctx, tx, env.Config.Providers); err != nil {
		return err
	}

	if err := upsertProviders(ctx, tx, env.Config.Providers, h.validateProviderURL); err != nil {
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
	// Users converge before virtual keys so key ownership (carried by
	// username) resolves against the freshly synced roster.
	if err := applyUsers(ctx, tx, env.Config.Users, nameToID); err != nil {
		return err
	}
	userNameToID, err := usernameToID(ctx, tx)
	if err != nil {
		return err
	}
	if err := upsertVirtualKeys(ctx, tx, env.Config.VirtualKeys, nameToID, userNameToID); err != nil {
		return err
	}
	vkHashes := names(env.Config.VirtualKeys, func(v ExportVK) string { return v.KeyHash })
	if _, err := tx.Exec(ctx, `DELETE FROM virtual_keys WHERE key_hash <> ALL($1)`, vkHashes); err != nil {
		return err
	}

	removedSettings, err := h.applySettingsTx(ctx, tx, env.Config.Settings)
	if err != nil {
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

	h.postImportRefresh(ctx, env, removedSettings)
	return nil
}

// enforceSourceGenFence takes the fleet source-generation advisory lock and
// applies the commit fence for this import. pg_advisory_xact_lock blocks a
// concurrent import's fence step until the surrounding transaction ends, so
// the read-current / reject-or-advance is atomic even when two pushes' bytes
// both arrived before either committed. Released automatically on commit or
// rollback.
func enforceSourceGenFence(ctx context.Context, tx pgx.Tx, sourceGen *int64) error {
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
	return nil
}

// guardAgainstProviderWipe is the destructive-wipe rail. The declarative
// delete in apply removes every provider absent from the envelope, so an
// envelope with zero providers would delete the member's entire provider set
// (cascading to discovered models) and, paired with the users replace, is the
// reported backdoor-wipe vector. buildEnvelope always ships the full config,
// so a functioning primary never legitimately pushes zero providers onto a
// member that has some. Refuse here, inside the transaction and before any
// delete, so the check and the delete it guards are atomic and no throwaway
// setting or virtual key can dress the envelope past it. An empty-provider
// envelope onto a member that also has no providers is a harmless no-op and is
// allowed (fleet bootstrap / keys-only sync onto an empty member).
func guardAgainstProviderWipe(ctx context.Context, tx pgx.Tx, providers []ExportProvider) error {
	if len(providers) == 0 {
		var existing int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM providers`).Scan(&existing); err != nil {
			return err
		}
		if existing > 0 {
			return errWouldWipeProviders
		}
	}
	return nil
}

// applySettingsTx converges syncable settings to the envelope inside the
// import transaction and returns the keys it deleted. Declarative replace:
// any syncable key this member has that the primary does not is deleted, so
// the replica falls back to the same built-in default the primary is using.
// Non-syncable keys (apprise, observability, instance-local) are never
// touched, and unknown keys are skipped silently.
func (h *ConfigSyncHandler) applySettingsTx(ctx context.Context, tx pgx.Tx, want map[string]string) ([]string, error) {
	for k, v := range want {
		if !isSyncableSetting(k) {
			continue // skip non-syncable / unknown keys silently
		}
		// Mirror the interactive PUT /api/settings validation: the config-sync path
		// writes the same url-typed settings the server later fetches (CWE-918) and
		// the same numeric limiter settings the data plane enforces on. A legitimate
		// primary already validated both on the way in.
		if err := validateSyncedSetting(k, v); err != nil {
			return nil, err
		}
		if err := h.settings.SetTx(ctx, tx, k, v); err != nil {
			return nil, err
		}
	}
	removedSettings, err := h.syncableSettingsToDelete(ctx, tx, want)
	if err != nil {
		return nil, err
	}
	if err := h.settings.DeleteKeysTx(ctx, tx, removedSettings); err != nil {
		return nil, err
	}
	return removedSettings, nil
}

// validateSyncedSetting applies the interactive UpdateSettings checks
// (settings.go) to a setting arriving by config sync, so a compromised primary
// cannot write through this path what the interactive endpoint would reject.
// Unknown keys pass through untouched; the caller has already gated syncability.
//
// url / url_public get the full netguard treatment: these are values the server
// itself later fetches or reflects into a redirect URI (reported SSRF bypass,
// CWE-918).
//
// int / float get their MINIMUM enforced, and deliberately not their maximum or
// their parseability. The floors are the ones that change runtime enforcement:
// rate_limit_ip_burst is min 1 because IPLimiter.getLimiter passes it to
// rate.NewLimiter unclamped, so a negative one denies every request from every
// IP, and rate_limit_burst is the same bug for every virtual key without a
// per-key override. A value above the ceiling is a capacity/sanity bound that
// relaxes no enforcement, and an unparseable one is inert because GetInt/GetFloat
// fall back to the built-in default. Skipping both keeps rolling upgrades
// working: a newer primary that raises a ceiling (or sends a value shape an older
// member cannot parse) must not make that member reject the ENTIRE envelope, the
// same trap validateSyncedRateLimits documents for grants. Floors are the
// structural end of the range and are not widened downward in practice.
func validateSyncedSetting(key, value string) error {
	rule, ok := allowedSettings[key]
	if !ok {
		return nil
	}
	switch rule.typeName {
	case "url":
		if err := netguard.ValidateURL(value); err != nil {
			return fmt.Errorf("%w %q: %w", errInvalidSyncedURL, key, err)
		}
	case "url_public":
		if err := netguard.ValidatePublicURL(value); err != nil {
			return fmt.Errorf("%w %q: %w", errInvalidSyncedURL, key, err)
		}
	// An unparseable value is left alone rather than rejected: GetInt/GetFloat
	// answer with the built-in default, so it relaxes nothing.
	case "int":
		if v, err := strconv.Atoi(value); err == nil && float64(v) < rule.min {
			return fmt.Errorf("%w: %s must be >= %d, got %d", errInvalidSyncedSettingBound, key, int(rule.min), v)
		}
	case "float":
		if v, err := strconv.ParseFloat(value, 64); err == nil && v < rule.min {
			return fmt.Errorf("%w: %s must be >= %g, got %g", errInvalidSyncedSettingBound, key, rule.min, v)
		}
	}
	return nil
}

// validateSyncedRateLimits applies the same bounds to an imported rate limit
// that the interactive virtual-key and user endpoints enforce via
// validateRateLimits (virtualkeys.go), so a config-sync import cannot write a
// per-key or per-user limit the interactive endpoint would reject. subject
// names the row for the error message. Nil values mean "fall back to the global
// setting" and are always fine.
//
// This is the same defense-in-depth shape as validateSyncedSetting: a
// legitimate primary already validated these on the way in, so anything out of
// bounds here means a compromised or corrupt envelope, and the limits it would
// relax are the ones metering the data plane.
//
// Deliberately NOT mirrored on the import path: the interactive API's username
// length/whitespace rules, display-name length, role allowlist, virtual-key
// reserved names, and user.ValidateGrants. Those are cosmetic or structural
// rather than security bounds (a compromised primary that could exploit them can
// already push an admin user outright), and porting the allowlists specifically
// would break rolling upgrades: a newer primary pushing a grant or role an older
// member does not know yet would fail the ENTIRE import rather than degrade one
// field. Only bounds that change runtime enforcement belong here.
func validateSyncedRateLimits(subject string, rps *float64, burst, tpm *int) error {
	if rps != nil && *rps < 0 {
		return fmt.Errorf("%w: %s rate_limit_rps must be >= 0, got %f", errInvalidSyncedRateLimit, subject, *rps)
	}
	if burst != nil && *burst < 1 {
		return fmt.Errorf("%w: %s rate_limit_burst must be >= 1, got %d", errInvalidSyncedRateLimit, subject, *burst)
	}
	if tpm != nil && *tpm < 1 {
		return fmt.Errorf("%w: %s rate_limit_tpm must be >= 1, got %d", errInvalidSyncedRateLimit, subject, *tpm)
	}
	return nil
}

// postImportRefresh runs the best-effort post-commit steps of an import: the
// core config is already durable, so nothing here can fail the sync.
func (h *ConfigSyncHandler) postImportRefresh(ctx context.Context, env ConfigEnvelope, removedSettings []string) {
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

func upsertProviders(ctx context.Context, tx pgx.Tx, providers []ExportProvider, validateURL func(string) error) error {
	for _, p := range providers {
		// Defense in depth on the import path: a compromised primary must not be
		// able to write a provider base_url that the interactive admin API would
		// reject. validateURL is the same guard CreateProvider/UpdateProvider use
		// (config.ValidateProviderURL): it resolves DNS and blocks loopback, RFC
		// 1918/ULA, link-local, CGNAT and cloud-metadata addresses (hosts in
		// ALLOWED_PROVIDER_HOSTS are exempted). The runtime proxy SafeDialer also
		// blocks these at dial time, but rejecting here keeps the poisoned value
		// out of the database entirely. Nil validateURL disables the check (tests).
		if validateURL != nil {
			if err := validateURL(p.BaseURL); err != nil {
				return fmt.Errorf("provider %q has an invalid base_url: %w", p.Name, err)
			}
		}
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
// which re-checks the users row on every request. nameToID resolves each
// account's provider cap back to this member's provider UUIDs; it is built
// after the provider upsert so every name a legitimate primary exported
// resolves.
func applyUsers(ctx context.Context, tx pgx.Tx, users []ExportUser, nameToID map[string]string) error {
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
		if err := validateSyncedRateLimits("user "+strconv.Quote(u.Username), u.RateLimitRPS, u.RateLimitBurst, u.RateLimitTPM); err != nil {
			return err
		}
		grants := u.Grants
		if grants == nil {
			grants = []string{}
		}
		// Resolve the account provider cap into this member's UUIDs. The presence
		// test is the POINTER, not the length, for the same reason as
		// upsertVirtualKeys: an account whose capped providers were all deleted
		// exports a present-but-empty list, and reading that as "uncapped" is the
		// escalation being guarded.
		var allowedProviders *[]string
		if u.AllowedProviderNames != nil {
			resolved := []string{}
			for _, name := range *u.AllowedProviderNames {
				if id, ok := nameToID[name]; ok {
					resolved = append(resolved, id)
				}
			}
			// Two ways a cap can resolve to nothing here, and they are NOT the
			// same thing.
			//
			// The wire list is EMPTY: the primary itself resolved nothing, i.e.
			// every provider in this account's cap has been deleted there. Fall
			// through and write the empty array. proxy.effectiveAllowedProviders
			// treats a non-nil cap as "exactly these providers" INCLUDING when
			// empty, so `{}` reproduces the primary's own effective behaviour
			// (deny everything) rather than NULL's "every provider". Refusing
			// instead would wedge fleet sync on an ordinary provider deletion,
			// and because a refusal fails the ENTIRE import the member would stay
			// frozen on its previous cap for this account, which may be WIDER
			// than what the primary now effectively enforces.
			//
			// The wire list is NON-EMPTY but none of it resolves: anomalous. The
			// declarative provider replace runs earlier in this same transaction
			// and nameToID is built from its result, so every name a legitimate
			// primary exported resolves here. Refuse the envelope; the rollback
			// undoes the users delete above with it.
			if len(resolved) == 0 && len(*u.AllowedProviderNames) > 0 {
				return fmt.Errorf("%w: user %s", errUnresolvableUserProviders, strconv.Quote(u.Username))
			}
			// A partially resolving list is just as anomalous as a fully
			// unresolvable one for the same reason, and it narrows the account
			// silently, so say so. Matches the warn upsertVirtualKeys emits on its
			// analogous branch. Username only: no request content is ever logged.
			if len(resolved) < len(*u.AllowedProviderNames) {
				debuglog.Warn("configsync: some of a user's allowed_providers do not resolve on this member; importing the subset",
					"user", u.Username, "wanted", len(*u.AllowedProviderNames), "resolved", len(resolved))
			}
			allowedProviders = &resolved
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (username, display_name, email, password_hash, role, grants, enabled,
			                   rate_limit_rps, rate_limit_burst, rate_limit_tpm, allowed_providers)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (username) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				email = EXCLUDED.email,
				password_hash = EXCLUDED.password_hash,
				role = EXCLUDED.role,
				grants = EXCLUDED.grants,
				enabled = EXCLUDED.enabled,
				rate_limit_rps = EXCLUDED.rate_limit_rps,
				rate_limit_burst = EXCLUDED.rate_limit_burst,
				rate_limit_tpm = EXCLUDED.rate_limit_tpm,
				allowed_providers = EXCLUDED.allowed_providers,
				updated_at = now()`,
			u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, grants, u.Enabled,
			u.RateLimitRPS, u.RateLimitBurst, u.RateLimitTPM, allowedProviders); err != nil {
			return err
		}
	}
	return nil
}

// usernameToID maps this member's usernames to their instance-local user
// ids, for resolving synced key ownership.
func usernameToID(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT username, id::text FROM users`)
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

func upsertVirtualKeys(ctx context.Context, tx pgx.Tx, vks []ExportVK, nameToID, userNameToID map[string]string) error {
	for _, v := range vks {
		if err := validateSyncedRateLimits("virtual key "+strconv.Quote(v.Name), v.RateLimitRPS, v.RateLimitBurst, v.RateLimitTPM); err != nil {
			return err
		}
		var allowed []string // target provider UUIDs; nil => all allowed
		if v.AllowedProviderNames != nil {
			for _, name := range *v.AllowedProviderNames {
				if id, ok := nameToID[name]; ok {
					allowed = append(allowed, id)
				}
			}
		}
		// Privilege-safety: if this key was restricted to providers but none of
		// them resolve on this member, do NOT import it. A nil allowed_providers
		// means "all providers allowed" (pgx writes the nil slice as NULL, and
		// the proxy treats only NULL as unrestricted), so writing it would
		// silently turn a restricted key into an unrestricted one. Skipping is
		// the lesser evil rather than a clean no-op: a key this member does not
		// have yet simply stays absent, but a key it already has keeps its
		// existing row, whose allowed_providers may be broader than the primary
		// now intends. Stale-but-bounded still beats writing NULL. In the normal
		// flow this never triggers: providers are upserted in the same
		// transaction before this runs, so every name resolves.
		//
		// The presence test is the POINTER, not the length. A key whose providers
		// were all deleted upstream exports a present-but-empty list, and reading
		// that as "unrestricted" is exactly the escalation this guards.
		if v.AllowedProviderNames != nil && len(allowed) == 0 {
			debuglog.Warn("configsync: skipping virtual key whose allowed_providers do not resolve on this member", "key", v.Name)
			continue
		}
		// Owner rides by username; an owner that does not resolve here (should
		// not happen: users are applied first in the same transaction) imports
		// as unowned rather than failing the sync.
		var ownerID *string
		if v.OwnerUsername != nil {
			if id, ok := userNameToID[*v.OwnerUsername]; ok {
				ownerID = &id
			} else {
				debuglog.Warn("configsync: virtual key owner does not resolve on this member, importing unowned", "key", v.Name, "owner", *v.OwnerUsername)
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_rps, rate_limit_burst, rate_limit_tpm, allowed_providers, strip_reasoning, owner_user_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (key_hash) DO UPDATE SET
				name = EXCLUDED.name,
				key_preview = EXCLUDED.key_preview,
				rate_limit_rps = EXCLUDED.rate_limit_rps,
				rate_limit_burst = EXCLUDED.rate_limit_burst,
				rate_limit_tpm = EXCLUDED.rate_limit_tpm,
				allowed_providers = EXCLUDED.allowed_providers,
				strip_reasoning = EXCLUDED.strip_reasoning,
				owner_user_id = EXCLUDED.owner_user_id`,
			v.Name, v.KeyHash, v.KeyPreview, v.RateLimitRPS, v.RateLimitBurst, v.RateLimitTPM, allowed, v.StripReasoning, ownerID)
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
				-- Migration 062: auto_disabled_at is what separates "discovery
				-- disabled this group" from "an operator switched it off", and the
				-- clear is ONE-WAY. revalidateCustomGroups skips groups that are
				-- already disabled, so nothing re-stamps a disabled group; clearing
				-- the stamp there is permanent, and a member whose hotel/<model>
				-- routing is dead would go silent about it forever.
				--
				-- So only the enable direction clears. An imported
				-- group_enabled = true is fleet-level operator intent to have this
				-- group ON, which does contradict a local auto-disable, and it is
				-- genuinely self-healing: with group_enabled = true this member's
				-- next revalidation no longer skips the group and re-disables and
				-- re-stamps it if it really is short of routable members here.
				--
				-- An imported group_enabled = false says nothing about THIS
				-- member's routable membership (it is the primary's own group
				-- state), so it must not erase a stamp this member's discovery
				-- earned. A row inserted rather than updated starts at NULL, so a
				-- claim is never invented either.
				auto_disabled_at = CASE WHEN EXCLUDED.group_enabled THEN NULL ELSE model_failover_groups.auto_disabled_at END,
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
