package api

import (
	"context"
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

// failoverApplyTimeout bounds the custom failover group build. The build is
// local database work only, so this deadline means the database is unavailable,
// not that the job is large.
const failoverApplyTimeout = 30 * time.Second

// applyOutcome carries what the post-commit steps of an import could not do.
// The core config is already durable when these run, so none of them fail the
// import; they travel to Front Desk instead, which retries until they succeed.
type applyOutcome struct {
	// SkippedGroups names custom failover groups this member could not build.
	SkippedGroups []string
	// PartialGroups names custom failover groups this member built with fewer
	// entries than the primary sent, because it holds fewer of the models they
	// reference. Reported so the operator alert can name them; see incomplete.
	PartialGroups []string
	// GroupApplyErr is set when the whole group build failed, in which case no
	// group was evaluated.
	GroupApplyErr error
	// DiscoveryErr is set when post-import discovery failed. Recorded for operators
	// but does not on its own mark the import incomplete: a provider outage is
	// routine, and a discovery failure that matters shows up as skipped groups.
	DiscoveryErr error
	// UnappliedModels names the per-model intent this member could not apply because
	// it holds no such model: the primary's disables and its manual-enable pins
	// alike. Like PartialGroups it is reported, not counted as a failure to apply:
	// the member did everything the envelope asked and simply has fewer models. It
	// routes to none of them either, so nothing is mis-served; what it explains is
	// the config hash difference that keeps the member flagged.
	UnappliedModels []string
	// ModelStateErr is set when a per-model reconcile (disables or pins) failed
	// outright. Both are joined into it, because either one leaves the member
	// diverging from the primary and the operator needs to see whichever failed.
	ModelStateErr error
}

// incomplete reports whether the member failed to materialise part of the
// config: a failover group it could not build, or a per-model reconcile that
// failed outright, both of which leave it routing differently from the
// primary. A discovery error alone is not one: a provider outage is routine, and
// one that matters surfaces as skipped groups.
//
// PartialGroups and UnappliedModels are deliberately NOT part of this. Both mean
// the member did everything the envelope asked and simply holds fewer models: it
// built the group with what it has, and it cannot disable a model it does not
// have. It is still configured differently from the primary, but that divergence
// is established by the config hash. These two only let the operator alert say
// which group is short and which models are missing.
func (o applyOutcome) incomplete() bool {
	return o.GroupApplyErr != nil || len(o.SkippedGroups) > 0 || o.ModelStateErr != nil
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
func (h *ConfigSyncHandler) apply(ctx context.Context, env ConfigEnvelope, sourceGen *int64) (applyOutcome, error) {
	tx, err := h.db.Begin(ctx)
	if err != nil {
		return applyOutcome{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := enforceSourceGenFence(ctx, tx, sourceGen); err != nil {
		return applyOutcome{}, err
	}
	if err := guardAgainstProviderWipe(ctx, tx, env.Config.Providers); err != nil {
		return applyOutcome{}, err
	}

	if err := upsertProviders(ctx, tx, env.Config.Providers, h.validateProviderURL); err != nil {
		return applyOutcome{}, err
	}
	// Declarative replace: drop providers absent from the primary. This cascades
	// to their discovered models (FK ON DELETE CASCADE) but request_logs are
	// preserved: their provider_id FK is ON DELETE SET NULL (migration 010), so
	// history stays and only the provider link is nulled.
	//
	// RETURNING the deleted ids so their references can be pruned out of the two
	// allow-list columns, which no foreign key covers. Already inside the import
	// transaction, so the delete and the prune commit together. Ordering matters
	// as well: this runs BEFORE upsertVirtualKeys and applyUsers, so a row the
	// envelope also rewrites ends up with the envelope's value rather than a
	// pruned one, and a row the envelope skips still gets cleaned.
	providerNames := names(env.Config.Providers, func(p ExportProvider) string { return p.Name })
	deletedRows, err := tx.Query(ctx, `DELETE FROM providers WHERE name <> ALL($1) RETURNING id::text`, providerNames)
	if err != nil {
		return applyOutcome{}, err
	}
	deletedProviderIDs, err := pgx.CollectRows(deletedRows, pgx.RowTo[string])
	if err != nil {
		return applyOutcome{}, err
	}
	if err := provider.PruneAllowLists(ctx, tx, deletedProviderIDs); err != nil {
		return applyOutcome{}, err
	}

	// Provider names resolve to THIS member's UUIDs only after the upsert above.
	nameToID, err := providerNameToID(ctx, tx)
	if err != nil {
		return applyOutcome{}, err
	}
	// Users converge before virtual keys so key ownership (carried by
	// username) resolves against the freshly synced roster.
	if err := applyUsers(ctx, tx, env.Config.Users, nameToID); err != nil {
		return applyOutcome{}, err
	}
	userNameToID, err := usernameToID(ctx, tx)
	if err != nil {
		return applyOutcome{}, err
	}
	if err := upsertVirtualKeys(ctx, tx, env.Config.VirtualKeys, nameToID, userNameToID); err != nil {
		return applyOutcome{}, err
	}
	vkHashes := names(env.Config.VirtualKeys, func(v ExportVK) string { return v.KeyHash })
	if _, err := tx.Exec(ctx, `DELETE FROM virtual_keys WHERE key_hash <> ALL($1)`, vkHashes); err != nil {
		return applyOutcome{}, err
	}

	removedSettings, err := h.applySettingsTx(ctx, tx, env.Config.Settings)
	if err != nil {
		return applyOutcome{}, err
	}

	if sourceGen != nil {
		// Advance the fence marker in the same transaction as the config write, so
		// the commit that applies this generation's config and the record that it
		// was applied are atomic. A raw upsert (not settings.SetTx) because the
		// _fleet_* keys are deliberately outside the SetTx allowlist; the value is
		// monotonic because an older generation was already rejected above.
		if err := writeAppliedSourceGen(ctx, tx, *sourceGen); err != nil {
			return applyOutcome{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return applyOutcome{}, err
	}

	out := h.postImportRefresh(ctx, env, removedSettings)
	return out, nil
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
// CWE-918). Standing guard with no live path today: every current url-typed
// setting (apprise + SSO) is instance-local and skipped before validation, so
// these branches fire only if a future url-typed setting joins the syncable
// set.
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
// core config is already durable, so nothing here can fail the sync. The
// returned outcome records what these steps could not do.
func (h *ConfigSyncHandler) postImportRefresh(ctx context.Context, env ConfigEnvelope, removedSettings []string) applyOutcome {
	var out applyOutcome
	// The core config is committed, so the remaining work is not bound to the
	// caller's request. Front Desk's import client gives up after 240s
	// (frontdesk.memberSyncTimeout) while discovery on a fresh member routinely
	// runs longer, and inheriting that deadline starves the group build, which
	// depends on discovery's output.
	//
	// Detached rather than given an aggregate deadline. A ceiling here would not
	// bound discovery, which detaches each provider under its own 180s timeout and
	// never consults this context (discovery.go), so the only thing it could expire
	// is the group build below: exactly the step that must run. The real bound is
	// per provider, times a finite provider list, and the group build carries its
	// own budget.
	ctx = context.WithoutCancel(ctx)

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
	// Settings too, and here rather than after the discovery pass below: that
	// pass can run for minutes, and the settings cache holds absences as well as
	// values, so a key the primary set for the first time would otherwise stay
	// "unset" on this member until the pass ended or the cache TTL ran out,
	// whichever came first. A removed key gets NotifyDeleted alone: it evicts
	// and notifies subscribers with the empty value, where InvalidateCache would
	// first re-read a row that no longer exists.
	for k := range env.Config.Settings {
		if isSyncableSetting(k) {
			h.settings.InvalidateCache(k)
		}
	}
	for _, k := range removedSettings {
		h.settings.NotifyDeleted(k)
	}

	// Populate this member's models so custom failover groups can resolve. The
	// "discover on provider creation" default is a dashboard action this raw
	// import bypasses, and scheduled discovery may be off, so without this a
	// freshly-synced member would have providers but no models, and hotel/<group>
	// would route to nothing until a restart or a manual discover. Best-effort:
	// the core config already committed, and groups reconcile on the next sync.
	if h.discoverAll != nil {
		if err := h.discoverAll(ctx); err != nil {
			debuglog.Warn("configsync: post-import discovery failed; custom failover groups may not resolve until models exist", "error", err)
			out.DiscoveryErr = err
		}
	}

	// Per-model disables, after discovery for the same reason the group build is:
	// the rows these refs resolve against are the ones discovery just created. It
	// runs before the group build only so the model state is settled by the time
	// anything downstream reads it; upsertFailoverGroups resolves entries by model
	// presence alone and is indifferent to the order.
	unapplied, err := h.applyDisabledModels(ctx, env.Config.DisabledModels)
	out.UnappliedModels = unapplied
	if err != nil {
		debuglog.Warn("configsync: failed to apply per-model disables", "error", err)
		out.ModelStateErr = err
	}

	// Manual-enable pins, immediately after the disables so a member ends up in the
	// same state whichever order a malformed envelope names a model in. Joined
	// rather than assigned, so a pin failure cannot erase a disable failure the
	// operator alert still has to report.
	pinned, err := h.applyEnabledModels(ctx, env.Config.EnabledModels)
	out.UnappliedModels = append(out.UnappliedModels, pinned...)
	if err != nil {
		debuglog.Warn("configsync: failed to apply per-model manual-enable pins", "error", err)
		out.ModelStateErr = errors.Join(out.ModelStateErr, err)
	}

	// Custom failover groups, in their own transaction now that discovery has had
	// a chance to create the models their entries reference. Best-effort for the
	// same reason: a group that cannot resolve yet reconciles on the next sync.
	groupCtx, groupCancel := context.WithTimeout(ctx, failoverApplyTimeout)
	groupRes, err := h.applyFailoverGroups(groupCtx, env.Config.FailoverGroups)
	groupCancel()
	out.SkippedGroups = groupRes.Skipped
	out.PartialGroups = groupRes.Partial
	if err != nil {
		debuglog.Warn("configsync: failed to apply custom failover groups", "error", err)
		out.GroupApplyErr = err
	}

	failover.InvalidateFailoverCache()
	return out
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
// atomically with the config it certifies. Because it bypasses the settings
// repository, nothing evicts the repository's cache for this key: that is fine
// only while the key is read the way readAppliedSourceGen reads it, with raw SQL
// inside the transaction. A repository read of it would serve a cached value,
// or a cached absence, for up to the cache TTL against this write.
func writeAppliedSourceGen(ctx context.Context, tx pgx.Tx, gen int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		keyFleetLastSourceGen, strconv.FormatInt(gen, 10))
	return err
}
