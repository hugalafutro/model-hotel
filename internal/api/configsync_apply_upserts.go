package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// applyFailoverGroups upserts the custom failover groups and declaratively
// removes custom groups absent from the envelope, in a dedicated transaction.
// It runs after the core-config commit and after discovery, so the models the
// entries reference exist. Auto-created groups are never touched. The declarative
// delete keeps a group still named in the envelope even if it was just skipped
// for too few resolvable entries, so a transient model gap does not delete the
// operator's group. It returns what the build could not fully do.
func (h *ConfigSyncHandler) applyFailoverGroups(ctx context.Context, groups []ExportFailoverGroup) (groupApplyResult, error) {
	// Distinguish "field absent" from "explicitly empty". A nil slice means the
	// envelope carried no failover_groups key at all (a pre-PR primary), so leave
	// the member's own custom groups untouched rather than wiping them on the first
	// sync of a rolling upgrade. A non-nil empty slice means a current primary that
	// genuinely has zero custom groups, which must reconcile: the declarative delete
	// below then removes every stale custom group the member still has.
	if groups == nil {
		return groupApplyResult{}, nil
	}
	tx, err := h.db.Pool().Begin(ctx)
	if err != nil {
		return groupApplyResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	res, err := upsertFailoverGroups(ctx, tx, groups)
	if err != nil {
		return groupApplyResult{}, err
	}
	groupNames := names(groups, func(g ExportFailoverGroup) string { return g.DisplayModel })
	if _, err := tx.Exec(ctx,
		`DELETE FROM model_failover_groups WHERE auto_created = false AND display_model <> ALL($1)`,
		groupNames); err != nil {
		return groupApplyResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return groupApplyResult{}, err
	}
	return res, nil
}

// providerTypeForImport keeps an imported provider's type non-empty. A payload
// from a member that predates the stored type carries none, and a row without a
// type would fall back to the URL rules on every read; deriving it once here
// pins the same answer to the row instead.
func providerTypeForImport(p ExportProvider) string {
	// An unknown type would be stored verbatim and then fall through to the
	// generic path on every read, so a payload from a newer build (or a
	// tampered one) is derived rather than trusted.
	if provider.IsKnownType(p.ProviderType) {
		return p.ProviderType
	}
	return provider.LegacyTypeFromURL(p.BaseURL)
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
			INSERT INTO providers (name, base_url, provider_type, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled, scheduled_disable_on, max_in_flight, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11, now())
			ON CONFLICT (name) DO UPDATE SET
				base_url = EXCLUDED.base_url,
				provider_type = EXCLUDED.provider_type,
				encrypted_key = EXCLUDED.encrypted_key,
				key_nonce = EXCLUDED.key_nonce,
				key_salt = EXCLUDED.key_salt,
				masked_key = EXCLUDED.masked_key,
				enabled = EXCLUDED.enabled,
				autodiscovery_enabled = EXCLUDED.autodiscovery_enabled,
				scheduled_disable_on = EXCLUDED.scheduled_disable_on,
				max_in_flight = EXCLUDED.max_in_flight,
				updated_at = now()`,
			p.Name, p.BaseURL, providerTypeForImport(p), p.EncryptedKey, p.KeyNonce, p.KeySalt, p.MaskedKey, p.Enabled, p.AutodiscoveryEnabled, p.ScheduledDisableOn, p.MaxInFlight)
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
		// The interactive API only ever stores hashes it computed itself; this
		// path takes them off the wire, so it checks the encoding here. Login
		// already fails closed on a malformed hash, so this is not an
		// authentication fix: it stops an unusable hash from being written at
		// all, where it would otherwise surface much later as an account that
		// silently cannot log in.
		if err := user.ValidateHashFormat(u.PasswordHash); err != nil {
			return fmt.Errorf("%w: user %s", errInvalidSyncedPasswordHash, strconv.Quote(u.Username))
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
		// now intends. Stale-but-bounded still beats writing NULL.
		//
		// The presence test is the POINTER, not the length. A key whose providers
		// were all deleted upstream exports a present-but-empty list, and reading
		// that as "unrestricted" is exactly the escalation this guards.
		//
		// Which is also how this branch is reached in practice. Deleting a provider
		// on the primary runs provider.PruneAllowLists, so any key scoped solely to
		// it is left with `{}` and exports an empty list: an ordinary admin action
		// trips this skip, with its warn, on every sync until the key is repaired or
		// removed. The other way in, a NON-empty list none of whose names resolve,
		// stays the rare one: providers are upserted in the same transaction before
		// this runs, so every name a legitimate primary exported does resolve.
		//
		// Note the deliberate difference from applyUsers, which writes an empty wire
		// cap through as `{}` instead of skipping. A user cannot be skipped: the
		// declarative replace would delete the row. A key can, and skipping keeps the
		// member's own row rather than converging it, which is the accepted gap
		// recorded in the design doc.
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

// groupApplyResult reports what a custom failover group import could not fully
// do, in export order. Skipped groups are absent from this member; partial
// groups exist here with fewer entries than the primary sent, so this member
// fails over across fewer providers for those models. The two are mutually
// exclusive per group: a group below the two-entry floor is skipped, never
// partial. Both are nil when every group was written in full.
type groupApplyResult struct {
	Skipped []string
	Partial []string
}

// upsertFailoverGroups re-creates each custom failover group on this member by
// resolving its (provider, model_id) entry refs back to local model UUIDs. An
// entry whose model is not present here is dropped; a group left with fewer than
// two routable entries is skipped (a one-member failover group is meaningless,
// matching pruneStaleEntries), and one left with two or more but fewer than the
// primary sent is written short and reported as partial. Always writes
// auto_created = false.
func upsertFailoverGroups(ctx context.Context, tx pgx.Tx, groups []ExportFailoverGroup) (groupApplyResult, error) {
	var res groupApplyResult
	if len(groups) == 0 {
		return res, nil
	}
	// (provider, model_id) -> local model UUID. Built inside the transaction so
	// it reflects the just-synced provider set (deleted providers cascade-removed
	// their models). Models themselves come from each member's discovery.
	localUUID := map[string]string{}
	rows, err := tx.Query(ctx,
		`SELECT p.name, m.model_id, m.id FROM models m JOIN providers p ON m.provider_id = p.id`)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var provider, modelID, id string
		if err := rows.Scan(&provider, &modelID, &id); err != nil {
			rows.Close()
			return res, err
		}
		localUUID[provider+"\x00"+modelID] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
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
			res.Skipped = append(res.Skipped, g.DisplayModel)
			continue
		}
		if len(priority) < len(g.Entries) {
			// Routable, but across fewer providers than the primary routes it: this
			// member holds fewer of the group's models. The group is written with what
			// resolved, and named so the operator alert can say which one is short.
			debuglog.Warn("configsync: building custom failover group with fewer entries than the primary sent",
				"group", g.DisplayModel, "resolved", len(priority), "wanted", len(g.Entries))
			res.Partial = append(res.Partial, g.DisplayModel)
		}
		priorityJSON, err := json.Marshal(priority)
		if err != nil {
			return res, err
		}
		entryEnabledJSON, err := json.Marshal(entryEnabled)
		if err != nil {
			return res, err
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
			return res, err
		}
	}
	return res, nil
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
