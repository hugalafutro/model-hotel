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
	if err := applyUsers(ctx, tx, env.Config.Users); err != nil {
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
		// Mirror the interactive PUT /api/settings URL validation: the config-sync
		// path also writes url-typed settings (oidc_issuer_url, ...) that the server
		// later fetches, so a compromised primary must not bypass netguard here
		// (CWE-918). A legitimate primary already validated these on the way in.
		if err := validateSyncedSettingURL(k, v); err != nil {
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

// validateSyncedSettingURL applies the same netguard checks to a url-typed
// setting that the interactive UpdateSettings handler enforces (settings.go),
// so a config-sync import cannot write an oidc_issuer_url / apprise base URL the
// interactive endpoint would reject (reported SSRF bypass, CWE-918). Non-URL and
// unknown keys pass through untouched; the caller has already gated syncability.
func validateSyncedSettingURL(key, value string) error {
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
			INSERT INTO users (username, display_name, email, password_hash, role, grants, enabled,
			                   rate_limit_rps, rate_limit_burst, rate_limit_tpm)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
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
				updated_at = now()`,
			u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, grants, u.Enabled,
			u.RateLimitRPS, u.RateLimitBurst, u.RateLimitTPM); err != nil {
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
