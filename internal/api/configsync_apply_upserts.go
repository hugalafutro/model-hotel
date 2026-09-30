package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/budget"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// applyFailoverGroups upserts the envelope's failover groups, custom and auto,
// and declaratively removes custom groups the envelope does not name as custom,
// in a dedicated transaction. It runs after the core-config commit and after
// discovery, so the models the entries reference exist. Auto-created groups are
// upserted but never deleted: their existence is this member's discovery's call.
// The declarative delete keeps a custom group still named in the envelope even if
// it was just skipped for too few resolvable entries, so a transient model gap
// does not delete the operator's group. The auto groups as sent are stored in the
// same transaction (keyFleetAutoFailoverGroups) so the rows and the echo that
// certifies them can never come from different imports. It returns what the build
// could not fully do.
//
// storeEcho is false when this import's discovery failed: the auto groups it
// could not resolve were skipped without a report, and an echo would certify
// that gap as converged. Deleting the echo instead leaves the member's own rows
// in its hash, so Front Desk keeps it amber and its re-push reruns discovery.
func (h *ConfigSyncHandler) applyFailoverGroups(ctx context.Context, groups []ExportFailoverGroup, storeEcho bool) (groupApplyResult, error) {
	// Distinguish "field absent" from "explicitly empty". A nil slice means the
	// envelope carried no failover_groups key, so leave the member's own custom
	// groups untouched rather than wiping them on the first sync of a rolling
	// upgrade. A non-nil empty slice means a primary with zero custom groups, which
	// must reconcile: the declarative delete below removes every stale custom group
	// the member still has.
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
	// Only the names the primary holds as CUSTOM keep a custom row here. A custom
	// row that shares its name with a primary auto group is not kept by that name:
	// if the auto group resolved, the upsert above already turned the row into it;
	// if it was skipped, keeping the custom row would export the name beside the
	// echoed auto group and the hashes could never meet.
	var custom, sent []ExportFailoverGroup
	for _, g := range groups {
		if g.AutoCreated {
			sent = append(sent, g)
		} else {
			custom = append(custom, g)
		}
	}
	customNames := names(custom, func(g ExportFailoverGroup) string { return g.DisplayModel })
	if _, err := tx.Exec(ctx,
		`DELETE FROM model_failover_groups WHERE auto_created = false AND display_model <> ALL($1)`,
		customNames); err != nil {
		return groupApplyResult{}, err
	}
	// The echo (keyFleetAutoFailoverGroups) rides the same transaction as the rows
	// it certifies. Raw SQL, not the settings store: the store's transactional
	// write enforces the syncable allowlist, which _fleet_* keys are outside of.
	if sent == nil {
		sent = []ExportFailoverGroup{}
	}
	raw, err := json.Marshal(sent)
	if err != nil {
		return groupApplyResult{}, err
	}
	echoSQL, echoArgs := `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		[]any{keyFleetAutoFailoverGroups, string(raw)}
	if !storeEcho {
		echoSQL, echoArgs = `DELETE FROM settings WHERE key = $1`, []any{keyFleetAutoFailoverGroups}
	}
	if _, err := tx.Exec(ctx, echoSQL, echoArgs...); err != nil {
		return groupApplyResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return groupApplyResult{}, err
	}
	// Drop the process cache now, not at the end of the import: a scan reading a
	// cached pre-import row would write that order back over the one just
	// committed and see no change to clear the echo for.
	failover.InvalidateFailoverCache()
	failover.MarkFleetAutoEchoWritten()
	return res, nil
}

// providerTypeForImport keeps an imported provider's type non-empty. A payload
// that carries none would leave a row falling back to the URL rules on every
// read; deriving the type once here pins the answer to the row instead.
func providerTypeForImport(p ExportProvider) string {
	// An unknown type would be stored verbatim and fall through to the generic
	// path on every read, so it is derived rather than trusted.
	if provider.IsKnownType(p.ProviderType) {
		return p.ProviderType
	}
	return provider.LegacyTypeFromURL(p.BaseURL)
}

// validateSyncedProvider applies to an imported provider the load-bearing
// checks the interactive admin API applies on create and update, so a
// compromised primary cannot write through this path a value that does harm on
// a member. The URL's shape is checked separately (validateURL); this covers
// the in-flight ceiling (a value below one is read as no ceiling at all), the
// name's length and printability, and the disable date's format.
//
// The admin API's routability rule (no "/", not "hotel") is deliberately not
// applied. The primary's own admin API already refuses such names as new
// names, so a primary holds one only from before the rule existed, and it must
// keep syncing rather than stall the fleet over a name its dashboard kept. A
// member cannot apply the rule to "new" providers only: to a member joining
// the fleet every provider is new. The worst such a name does is leave one
// provider unreachable by name, which is no harm to guard against.
//
// A disable date in the past is accepted: the member's own sweep fires it
// immediately, which is what the operator asked for. The URL's length is not
// bounded here, because a long URL is harmless where an unprintable or
// ten-thousand-character name reaches logs, the dashboard and hotel/ model
// strings. The name is validated as sent and
// stored as sent (the admin API stores it trimmed): storing a trimmed copy would
// diverge from the primary's hash and re-sync forever.
func validateSyncedProvider(p ExportProvider) error {
	if err := provider.ValidateMaxInFlight(p.MaxInFlight); err != nil {
		return fmt.Errorf("%w: provider %q: %w", errInvalidSyncedProvider, p.Name, err)
	}
	if _, err := validateNameString("name", p.Name, 1, 100); err != nil {
		return fmt.Errorf("%w: provider %q: %w", errInvalidSyncedProvider, p.Name, err)
	}
	if p.ScheduledDisableOn != nil {
		if _, err := time.Parse("2006-01-02", *p.ScheduledDisableOn); err != nil {
			return fmt.Errorf("%w: provider %q: scheduled_disable_on must be a YYYY-MM-DD date", errInvalidSyncedProvider, p.Name)
		}
	}
	if err := provider.ValidateQuotaReservePercent(p.QuotaReservePercent); err != nil {
		return fmt.Errorf("%w: provider %q: %w", errInvalidSyncedProvider, p.Name, err)
	}
	return nil
}

// validateSyncedProviderNames refuses an envelope carrying two providers whose
// names are the same one once spaces become hyphens. That is the form routing
// uses (provider.NormalizeName), so such a pair fights over one routing id and
// one provider cache slot, and the unique index migration 082 installs would
// refuse the second row anyway. This is the envelope-wide half of the provider
// validation, so it runs over the whole list once, ahead of the writes, rather
// than per row inside them.
func validateSyncedProviderNames(providers []ExportProvider) error {
	seen := make(map[string]string, len(providers))
	for _, p := range providers {
		normalized := provider.NormalizeName(p.Name)
		if first, ok := seen[normalized]; ok {
			return fmt.Errorf("%w: providers %q and %q are one name once spaces become hyphens, which is the form routing uses",
				errInvalidSyncedProvider, first, p.Name)
		}
		seen[normalized] = p.Name
	}
	return nil
}

func upsertProviders(ctx context.Context, tx pgx.Tx, providers []ExportProvider, validateURL func(string) error) error {
	for _, p := range providers {
		if err := validateSyncedProvider(p); err != nil {
			return err
		}
		// Defense in depth on the import path: a compromised primary must not write
		// a provider base_url the interactive admin API would reject. validateURL is
		// the same guard CreateProvider/UpdateProvider use
		// (config.ValidateProviderURL): it resolves DNS and blocks loopback, RFC
		// 1918/ULA, link-local, CGNAT and cloud-metadata addresses (hosts in
		// ALLOWED_PROVIDER_HOSTS are exempted). The runtime proxy SafeDialer blocks
		// these at dial time too, but rejecting here keeps the poisoned value out of
		// the database. Nil validateURL disables the check.
		if validateURL != nil {
			if err := validateURL(p.BaseURL); err != nil {
				// Same refusal class as the bounds above: the envelope carries what
				// the admin API would reject, so a 400, not a 500.
				return fmt.Errorf("%w: provider %q has an invalid base_url: %w", errInvalidSyncedProvider, p.Name, err)
			}
		}
		// A primary that renames "a b" to "a-b" sends a name this member does not
		// have, keyed on a raw name that does not match its old row, and the
		// normalized index (migration 082) refuses the insert before the
		// declarative delete could remove the old row. Nothing about that
		// resolves itself, so every later push is refused too. Rename the local
		// twin first, in the same transaction: the index guarantees at most one
		// row normalizes to this name, and the envelope-wide check above
		// guarantees no two exported names do, so this moves exactly the row the
		// upsert below is about to collide with, keeping its id and the models
		// hanging off it. Every other case matches no row and the raw-name
		// ON CONFLICT below is unchanged.
		//
		// The envelope carries no rename intent, so a primary that deleted "a b"
		// and created an unrelated "a-b" reuses the row rather than the id it
		// would have got from a fresh insert. The provider set and every synced
		// field still end up exactly as the envelope describes them, and the
		// post-import discovery run reconciles the models under the reused row.
		// The same statement also fires for a local row the envelope never
		// named, when the primary added a provider whose normalized name matches
		// it: that row keeps its id and models and takes the envelope's fields,
		// where the declarative delete used to remove it. Logged, because the
		// member keeps a row it would otherwise have lost.
		renamed, err := tx.Exec(ctx,
			`UPDATE providers SET name = $1, updated_at = now()
			 WHERE REPLACE(name, ' ', '-') = REPLACE($1, ' ', '-') AND name <> $1`, p.Name)
		if err != nil {
			return err
		}
		if renamed.RowsAffected() > 0 {
			debuglog.Info("configsync: renamed a local provider onto the envelope's spelling of the same name", "name", p.Name)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO providers (name, base_url, provider_type, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled, scheduled_disable_on, max_in_flight, quota_reserve_percent, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11, COALESCE($12::smallint, 0), now())
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
				quota_reserve_percent = COALESCE($12::smallint, providers.quota_reserve_percent),
				updated_at = now()`,
			p.Name, p.BaseURL, providerTypeForImport(p), p.EncryptedKey, p.KeyNonce, p.KeySalt, p.MaskedKey, p.Enabled, p.AutodiscoveryEnabled, p.ScheduledDisableOn, p.MaxInFlight, p.QuotaReservePercent)
		if err != nil {
			// No normalized-name refusal to translate here: the rename above has
			// already moved this member's twin, if it had one, onto the name being
			// inserted, and the envelope-wide check ahead of the loop rules out two
			// exported names colliding with each other. What reaches this line is a
			// database failure, not a name the member could not take.
			return err
		}
	}
	return nil
}

// applyUsers converges the users table to the envelope, keyed by username. A nil
// slice means the envelope omits the field: leave this member's users alone
// (same contract as failover groups). Sequence matters: delete absent users
// first, then blank all remaining emails, then upsert. The blanking step lets an
// email move between two surviving accounts without tripping the unique index
// mid-upsert (row-by-row upserts would otherwise 23505 on a swap). Sessions of
// removed or disabled users die at the auth middleware, which re-checks the
// users row on every request. nameToID resolves each account's provider cap back
// to this member's provider UUIDs; it is built after the provider upsert so
// every name a legitimate primary exported resolves.
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
		if err := budget.Validate(u.BudgetUSD, u.BudgetPeriod); err != nil {
			return fmt.Errorf("%w: user %s: %w", errInvalidSyncedBudget, strconv.Quote(u.Username), err)
		}
		// The interactive API only stores hashes it computed itself; this path takes
		// them off the wire, so it checks the encoding. Login already fails closed
		// on a malformed hash, so this is not an authentication fix: it stops an
		// unusable hash from being written at all, where it would surface later as
		// an account that silently cannot log in.
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
		// escalation this guards.
		var allowedProviders *[]string
		if u.AllowedProviderNames != nil {
			resolved := translateIDs(*u.AllowedProviderNames, nameToID)
			// Two ways a cap resolves to nothing here, and they are NOT the same.
			//
			// The wire list is EMPTY: the primary itself resolved nothing, i.e. every
			// provider in this account's cap has been deleted there. Fall through and
			// write the empty array. proxy.effectiveAllowedProviders treats a non-nil
			// cap as "exactly these providers" INCLUDING when empty, so `{}`
			// reproduces the primary's own deny-everything behaviour rather than
			// NULL's "every provider". Refusing would wedge fleet sync on an ordinary
			// provider deletion and freeze the member on its previous cap, which may
			// be WIDER than what the primary now enforces.
			//
			// The wire list is NON-EMPTY but none of it resolves: anomalous. The
			// declarative provider replace runs earlier in this transaction and
			// nameToID is built from its result, so every name a legitimate primary
			// exported resolves here. Refuse the envelope; the rollback undoes the
			// users delete with it.
			if len(resolved) == 0 && len(*u.AllowedProviderNames) > 0 {
				return fmt.Errorf("%w: user %s", errUnresolvableUserProviders, strconv.Quote(u.Username))
			}
			// A partially resolving list is as anomalous as a fully unresolvable one
			// and narrows the account silently, so it is logged. Username only: no
			// request content is ever logged.
			if len(resolved) < len(*u.AllowedProviderNames) {
				debuglog.Warn("configsync: some of a user's allowed_providers do not resolve on this member; importing the subset",
					"user", u.Username, "wanted", len(*u.AllowedProviderNames), "resolved", len(resolved))
			}
			allowedProviders = &resolved
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO users (username, display_name, email, password_hash, role, grants, enabled,
			                   rate_limit_rps, rate_limit_burst, rate_limit_tpm, allowed_providers, budget_usd, budget_period)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
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
				budget_usd = EXCLUDED.budget_usd,
				budget_period = EXCLUDED.budget_period,
				updated_at = now()`,
			u.Username, u.DisplayName, u.Email, u.PasswordHash, u.Role, grants, u.Enabled,
			u.RateLimitRPS, u.RateLimitBurst, u.RateLimitTPM, allowedProviders, u.BudgetUSD, u.BudgetPeriod); err != nil {
			return err
		}
	}
	return nil
}

// usernameToID maps this member's usernames to their instance-local user
// ids, for resolving synced key ownership.
func usernameToID(ctx context.Context, tx pgx.Tx) (map[string]string, error) {
	return stringMap(ctx, tx, `SELECT username, id::text FROM users`)
}

func upsertVirtualKeys(ctx context.Context, tx pgx.Tx, vks []ExportVK, nameToID, userNameToID map[string]string) error {
	for _, v := range vks {
		if err := validateSyncedRateLimits("virtual key "+strconv.Quote(v.Name), v.RateLimitRPS, v.RateLimitBurst, v.RateLimitTPM); err != nil {
			return err
		}
		if err := budget.Validate(v.BudgetUSD, v.BudgetPeriod); err != nil {
			return fmt.Errorf("%w: virtual key %s: %w", errInvalidSyncedBudget, strconv.Quote(v.Name), err)
		}
		var allowed []string // target provider UUIDs; nil => all allowed
		if v.AllowedProviderNames != nil {
			allowed = translateIDs(*v.AllowedProviderNames, nameToID)
		}
		// Privilege-safety: a key restricted to providers none of which resolve on
		// this member is NOT imported. A nil allowed_providers means "all providers
		// allowed" (pgx writes the nil slice as NULL, and the proxy treats only NULL
		// as unrestricted), so writing it would turn a restricted key into an
		// unrestricted one. Skipping is not a clean no-op: a key this member does
		// not have stays absent, but one it already has keeps its existing row,
		// whose allowed_providers may be broader than the primary intends.
		//
		// The presence test is the POINTER, not the length. A key whose providers
		// were all deleted upstream exports a present-but-empty list, and reading
		// that as "unrestricted" is the escalation this guards.
		//
		// That is also how the branch is reached in practice: deleting a provider on
		// the primary runs provider.PruneAllowLists, so a key scoped solely to it is
		// left with `{}` and exports an empty list, tripping this skip on every sync
		// until the key is repaired or removed. A NON-empty list none of whose names
		// resolve is rare: providers are upserted in the same transaction first.
		//
		// applyUsers instead writes an empty wire cap through as `{}`: a user cannot
		// be skipped, because the declarative replace would delete the row. A key
		// can, and skipping keeps the member's own row rather than converging it.
		if v.AllowedProviderNames != nil && len(allowed) == 0 {
			debuglog.Warn("configsync: skipping virtual key whose allowed_providers do not resolve on this member", "key", v.Name)
			continue
		}
		// Owner rides by username; an owner that does not resolve here (users are
		// applied first in the same transaction) imports as unowned rather than
		// failing the sync.
		var ownerID *string
		if v.OwnerUsername != nil {
			if id, ok := userNameToID[*v.OwnerUsername]; ok {
				ownerID = &id
			} else {
				debuglog.Warn("configsync: virtual key owner does not resolve on this member, importing unowned", "key", v.Name, "owner", *v.OwnerUsername)
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_rps, rate_limit_burst, rate_limit_tpm, allowed_providers, strip_reasoning, owner_user_id, budget_usd, budget_period)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (key_hash) DO UPDATE SET
				name = EXCLUDED.name,
				key_preview = EXCLUDED.key_preview,
				rate_limit_rps = EXCLUDED.rate_limit_rps,
				rate_limit_burst = EXCLUDED.rate_limit_burst,
				rate_limit_tpm = EXCLUDED.rate_limit_tpm,
				allowed_providers = EXCLUDED.allowed_providers,
				strip_reasoning = EXCLUDED.strip_reasoning,
				owner_user_id = EXCLUDED.owner_user_id,
				budget_usd = EXCLUDED.budget_usd,
				budget_period = EXCLUDED.budget_period`,
			v.Name, v.KeyHash, v.KeyPreview, v.RateLimitRPS, v.RateLimitBurst, v.RateLimitTPM, allowed, v.StripReasoning, ownerID, v.BudgetUSD, v.BudgetPeriod)
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

// upsertFailoverGroups re-creates each failover group on this member by
// resolving its (provider, model_id) entry refs back to local model UUIDs. An
// entry whose model is not present here is dropped; a group left with fewer than
// two routable entries is skipped (a one-member failover group is meaningless,
// matching pruneStaleEntries), and a custom one left with two or more but fewer
// than the primary sent is written short and reported as partial. auto_created is
// written as the envelope says.
//
// An auto group follows this member's discovery rules on top of the primary's
// intent: only models that are enabled here count (discovery forms auto groups
// from enabled models alone, so a disabled one would be pruned on the next scan,
// and that prune would drop the echo and re-import the group, forever), and any
// entry this member's own auto row already holds that the primary did not send
// (a model only this member's provider lists) stays, after the primary's, with
// its toggle. That is the order discovery would produce itself, so a scan after
// the import changes nothing.
func upsertFailoverGroups(ctx context.Context, tx pgx.Tx, groups []ExportFailoverGroup) (groupApplyResult, error) {
	var res groupApplyResult
	if len(groups) == 0 {
		return res, nil
	}
	// (provider, model_id) -> local model UUID, with the model's effective enabled
	// state. Built inside the transaction so it reflects the just-synced provider
	// set (deleted providers cascade-removed their models). Models themselves come
	// from each member's discovery.
	type localModel struct {
		id      string
		enabled bool
	}
	localUUID := map[string]localModel{}
	enabledUUID := map[string]bool{}
	rows, err := tx.Query(ctx,
		`SELECT p.name, m.model_id, m.id, (m.enabled AND p.enabled) FROM models m JOIN providers p ON m.provider_id = p.id`)
	if err != nil {
		return res, err
	}
	for rows.Next() {
		var provider, modelID, id string
		var enabled bool
		if err := rows.Scan(&provider, &modelID, &id, &enabled); err != nil {
			rows.Close()
			return res, err
		}
		localUUID[provider+"\x00"+modelID] = localModel{id: id, enabled: enabled}
		enabledUUID[id] = enabled
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	ownAuto, err := readOwnAutoGroups(ctx, tx)
	if err != nil {
		return res, err
	}

	for _, g := range groups {
		priority := make([]string, 0, len(g.Entries))
		entryEnabled := map[string]bool{}
		for _, e := range g.Entries {
			m, ok := localUUID[e.ProviderName+"\x00"+e.ModelID]
			if !ok || (g.AutoCreated && !m.enabled) {
				continue // model absent on this member (not discovered yet, or removed)
			}
			priority = append(priority, m.id)
			entryEnabled[m.id] = e.Enabled
		}
		if g.AutoCreated {
			for _, id := range ownAuto[g.DisplayModel].priority {
				if _, sent := entryEnabled[id]; sent || !enabledUUID[id] {
					continue
				}
				priority = append(priority, id)
				entryEnabled[id] = ownAuto[g.DisplayModel].enabled[id]
			}
		}
		// An auto group this member cannot fill, or can only fill in part, is its
		// own discovery's to form or drop: neither is an operator error to alert on,
		// so only custom groups are reported.
		if len(priority) < 2 {
			if !g.AutoCreated {
				debuglog.Warn("configsync: skipping custom failover group with too few resolvable entries",
					"group", g.DisplayModel, "resolved", len(priority), "wanted", len(g.Entries))
				res.Skipped = append(res.Skipped, g.DisplayModel)
			}
			continue
		}
		if !g.AutoCreated && len(priority) < len(g.Entries) {
			// Routable, but across fewer providers than the primary routes it: this
			// member holds fewer of the group's models. The group is written with what
			// resolved, and named so the operator alert can say which is short.
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
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (display_model) DO UPDATE SET
				priority_order = EXCLUDED.priority_order,
				entry_enabled  = EXCLUDED.entry_enabled,
				group_enabled  = EXCLUDED.group_enabled,
				display_name   = EXCLUDED.display_name,
				description    = EXCLUDED.description,
				auto_created   = EXCLUDED.auto_created,
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
				--
				-- An auto row never carries the stamp (discovery deletes an undersized
				-- auto group rather than disabling it), so an imported auto group
				-- landing on a custom row that had one clears it outright.
				auto_disabled_at = CASE WHEN EXCLUDED.group_enabled OR EXCLUDED.auto_created THEN NULL ELSE model_failover_groups.auto_disabled_at END,
				updated_at     = now()`,
			g.DisplayModel, priorityJSON, entryEnabledJSON, g.GroupEnabled, g.DisplayName, g.Description, g.AutoCreated); err != nil {
			return res, err
		}
	}
	return res, nil
}

// ownAutoGroup is what this member's discovery already holds for one auto group:
// its entry order and toggles, keyed by local model UUID.
type ownAutoGroup struct {
	priority []string
	enabled  map[string]bool
}

// readOwnAutoGroups reads every auto group this member holds, so an import can
// keep the entries the primary did not send (see upsertFailoverGroups).
func readOwnAutoGroups(ctx context.Context, q querier) (map[string]ownAutoGroup, error) {
	rows, err := q.Query(ctx,
		`SELECT display_model, priority_order, COALESCE(entry_enabled, '{}') FROM model_failover_groups WHERE auto_created = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ownAutoGroup{}
	for rows.Next() {
		var name string
		var priorityJSON, enabledJSON []byte
		if err := rows.Scan(&name, &priorityJSON, &enabledJSON); err != nil {
			return nil, err
		}
		// A row this member cannot parse carries no extras worth keeping: the
		// upsert below overwrites it with the primary's entries, which is the only
		// repair a corrupt row gets (discovery's own read of it fails too).
		var g ownAutoGroup
		if err := json.Unmarshal(priorityJSON, &g.priority); err != nil {
			debuglog.Warn("configsync: unparseable priority_order on an auto group; overwriting it", "group", name, "error", err)
			continue
		}
		g.enabled = map[string]bool{}
		if err := json.Unmarshal(enabledJSON, &g.enabled); err != nil {
			debuglog.Warn("configsync: unparseable entry_enabled on an auto group; overwriting it", "group", name, "error", err)
			continue
		}
		// entry_enabled absence means enabled (matches proxy/enabledEntryIDs).
		for _, id := range g.priority {
			if _, ok := g.enabled[id]; !ok {
				g.enabled[id] = true
			}
		}
		out[name] = g
	}
	return out, rows.Err()
}

func providerNameToID(ctx context.Context, q querier) (map[string]string, error) {
	return stringMap(ctx, q, `SELECT name, id FROM providers`)
}
