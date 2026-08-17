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
	"github.com/hugalafutro/model-hotel/internal/user"
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
	for k := range env.Config.Settings {
		if isSyncableSetting(k) {
			h.settings.InvalidateCache(k)
		}
	}
	for _, k := range removedSettings {
		h.settings.InvalidateCache(k)
		h.settings.NotifyDeleted(k)
	}
	return out
}

// modelStateApplyTimeout bounds a per-model reconcile. Local database work only,
// so this deadline means the database is unavailable, not that the fleet has many
// models.
const modelStateApplyTimeout = 30 * time.Second

// modelIntentWriter performs one section's own reconcile statements inside the
// shared transaction applyModelIntent opens. wanted is the sub-select pairing the
// primary's refs back into (provider_name, model_id) rows, and providers/modelIDs
// are the two bind arrays every statement using it must pass, in that order.
type modelIntentWriter func(ctx context.Context, tx pgx.Tx, wanted string, providers, modelIDs []string) error

// applyModelIntent is the shared machinery behind the two per-model reconciles,
// the operator's disables and the operator's manual-enable pins. Both carry the
// same kind of payload, a list of stable refs naming operator intent, so both need
// the same handling around their own writes, and the handling is what the fleet's
// convergence rests on.
//
// A nil slice is an envelope from a primary that predates the section, so this
// member's state is left alone; distinguishing that from an explicitly empty list
// is what stops the first sync of a rolling upgrade from wiping the intent the
// operator recorded. A non-nil empty slice is a current primary with none, which
// must reconcile.
//
// The writes and the acknowledgement commit together: a member that recorded the
// acknowledgement without applying what it could, or the reverse, would export a
// list describing neither state. Afterwards the model cache is dropped, because
// both sections move models.enabled and the proxy reads routability from it.
func (h *ConfigSyncHandler) applyModelIntent(ctx context.Context, refs []ExportModelRef,
	ackKey string, write modelIntentWriter) ([]string, error) {
	if refs == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, modelStateApplyTimeout)
	defer cancel()

	providers := make([]string, len(refs))
	modelIDs := make([]string, len(refs))
	for i, ref := range refs {
		providers[i] = ref.ProviderName
		modelIDs[i] = ref.ModelID
	}

	tx, err := h.db.Pool().Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// unnest pairs the two arrays back into the (provider name, model_id) rows the
	// refs came from, so the match is on the whole pair rather than on either half.
	const wanted = `SELECT * FROM unnest($1::text[], $2::text[]) AS w(provider_name, model_id)`
	if err := write(ctx, tx, wanted, providers, modelIDs); err != nil {
		return nil, err
	}

	// Which of the primary's refs this member actually holds. Read inside the same
	// transaction as the writes, so the report describes the state that committed.
	rows, err := tx.Query(ctx, `
		SELECT p.name, m.model_id
		  FROM models m JOIN providers p ON m.provider_id = p.id
		 WHERE EXISTS (`+wanted+` WHERE w.provider_name = p.name AND w.model_id = m.model_id)`,
		providers, modelIDs)
	if err != nil {
		return nil, err
	}
	present := map[ExportModelRef]bool{}
	for rows.Next() {
		var ref ExportModelRef
		if err := rows.Scan(&ref.ProviderName, &ref.ModelID); err != nil {
			rows.Close()
			return nil, err
		}
		present[ref] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Acknowledge the refs there is no model here to apply, so this member's own
	// export carries the primary's full intent and the two hash alike.
	var unappliedRefs []ExportModelRef
	for _, ref := range refs {
		if !present[ref] {
			unappliedRefs = append(unappliedRefs, ref)
		}
	}
	if err := writeUnappliedModelRefs(ctx, tx, ackKey, unappliedRefs); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	// The writes bypassed the model cache, and the proxy reads routability from it.
	model.InvalidateModelCache()

	unapplied := make([]string, 0, len(unappliedRefs))
	for _, ref := range unappliedRefs {
		unapplied = append(unapplied, ref.String())
	}
	return unapplied, nil
}

// applyDisabledModels reconciles this member's operator-disabled models to the
// primary's list, and returns the refs it could not apply because no such model
// exists here.
//
// Both directions replay the operator's own action: a ref present here that was
// not disabled is switched off, and a model disabled here but absent from the list
// is switched back on exactly as Repository.SetEnabled(true) would, clearing
// auto_retired_at and discovery_dismissed_at alongside, because a hand-written
// enabled flag supersedes what discovery or the proxy concluded (migration 063).
// The disable direction leaves those two stamps in place; see below for why.
//
// Only disabled_manually rows are touched in the enable direction. A model this
// member's discovery disabled, or the proxy retired from traffic, is evidence
// about what this member's provider served it, and the primary's list says
// nothing about that; re-enabling those would revive models the provider is
// refusing here and churn the failover groups built on them every pass.
func (h *ConfigSyncHandler) applyDisabledModels(ctx context.Context, refs []ExportModelRef) ([]string, error) {
	return h.applyModelIntent(ctx, refs, keyFleetUnappliedModelDisables,
		func(ctx context.Context, tx pgx.Tx, wanted string, providers, modelIDs []string) error {
			// The disable direction deliberately leaves auto_retired_at and
			// discovery_dismissed_at alone, where Repository.SetEnabled(false) clears both.
			// The model ends up switched off either way, so neither stamp has anything to
			// contradict, and they are this member's own evidence about what its provider
			// served it: clearing them would convert a local traffic retirement into an
			// operator disable, and a later re-enable on the primary would then put a model
			// the provider is refusing here back into routing until three more failures
			// re-retired it. The enable direction below does clear them, because there the
			// operator is saying to trust the provider's listing again.
			// Unnarrowed on purpose. Skipping rows already flagged disabled_manually would
			// be the obvious optimisation, but nothing constrains that flag against enabled,
			// so a row carrying both would be passed over here and still counted present,
			// leaving it routing and reported as applied. The write is idempotent, so
			// covering every matched row costs nothing and repairs such a row instead.
			if _, err := tx.Exec(ctx, `
				UPDATE models m
				   SET enabled = false, disabled_manually = true
				  FROM providers p
				 WHERE m.provider_id = p.id
				   AND EXISTS (`+wanted+` WHERE w.provider_name = p.name AND w.model_id = m.model_id)`,
				providers, modelIDs); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				UPDATE models m
				   SET enabled = true, disabled_manually = false,
				       auto_retired_at = NULL, discovery_dismissed_at = NULL
				  FROM providers p
				 WHERE m.provider_id = p.id
				   AND m.disabled_manually = true
				   AND NOT EXISTS (`+wanted+` WHERE w.provider_name = p.name AND w.model_id = m.model_id)`,
				providers, modelIDs)
			return err
		})
}

// applyEnabledModels reconciles this member's manual-enable pins to the primary's
// list, and returns the refs it could not apply because no such model exists here.
// It runs immediately after applyDisabledModels: the two lists are disjoint in
// practice, because every write that sets disabled_manually clears the pin, but
// ordering them makes even a malformed envelope land the same way on every member.
//
// The two directions are deliberately asymmetric, unlike the disables'.
//
// The pin direction force-enables: the operator verified this model serves, so
// their word outranks both this member's listing evidence (discovery's disable and
// its dismissed claim) and its traffic retirement, and all of it is cleared,
// missing_scans included. Clearing the retirement is the one place a pin overrules
// the proxy, and it is safe because the retirement machinery re-arms on the very
// next refusal by name; leaving the stamp would instead have the model both pinned
// and refused, with nothing to resolve it.
//
// The unpin direction only clears the pin. A ref gone from the primary's list means
// the operator dropped the pin, not that the model must go: it stays enabled and
// this member's own listing-based machinery takes it from here. missing_scans is
// reset with the pin because a pin held past the disable threshold leaves a mature
// streak behind, and clearing the stamp alone would disable the model on its very
// next scan instead of giving it the same grace an unpinned model gets (the same
// rule POST /discovery/unpin follows).
//
// Both directions re-zero missing_scans on every import, which is why a member's
// own discrepancy modal rarely lists its pinned rows: listClaimRows reports a pin
// only once its miss streak is above zero, and each sync pass wipes the streak the
// member has accumulated since the last one. Pin visibility is a primary-side
// surface by design; a member shows the pin only if it misses a scan between two
// syncs.
func (h *ConfigSyncHandler) applyEnabledModels(ctx context.Context, refs []ExportModelRef) ([]string, error) {
	return h.applyModelIntent(ctx, refs, keyFleetUnappliedModelEnables,
		func(ctx context.Context, tx pgx.Tx, wanted string, providers, modelIDs []string) error {
			// COALESCE keeps an existing pin's own timestamp: the stamp is when THIS
			// member first honoured the pin, and re-stamping it on every sync would
			// rewrite the row on every pass for no change in meaning.
			if _, err := tx.Exec(ctx, `
				UPDATE models m
				   SET enabled = true, disabled_manually = false,
				       auto_retired_at = NULL, discovery_dismissed_at = NULL,
				       missing_scans = 0,
				       manually_enabled_at = COALESCE(m.manually_enabled_at, now())
				  FROM providers p
				 WHERE m.provider_id = p.id
				   AND EXISTS (`+wanted+` WHERE w.provider_name = p.name AND w.model_id = m.model_id)`,
				providers, modelIDs); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				UPDATE models m
				   SET manually_enabled_at = NULL, missing_scans = 0
				  FROM providers p
				 WHERE m.provider_id = p.id
				   AND m.manually_enabled_at IS NOT NULL
				   AND NOT EXISTS (`+wanted+` WHERE w.provider_name = p.name AND w.model_id = m.model_id)`,
				providers, modelIDs)
			return err
		})
}

// writeUnappliedModelRefs records the refs this member could not apply for one
// section, replacing whatever that marker held before. Always written, including
// as an empty list: a member that has just discovered the missing models must stop
// claiming them, or it would keep exporting intent it now genuinely applies.
func writeUnappliedModelRefs(ctx context.Context, tx pgx.Tx, key string, refs []ExportModelRef) error {
	if refs == nil {
		refs = []ExportModelRef{}
	}
	encoded, err := json.Marshal(refs)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		key, string(encoded))
	return err
}

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
			INSERT INTO providers (name, base_url, provider_type, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled, scheduled_disable_on, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, now())
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
				updated_at = now()`,
			p.Name, p.BaseURL, providerTypeForImport(p), p.EncryptedKey, p.KeyNonce, p.KeySalt, p.MaskedKey, p.Enabled, p.AutodiscoveryEnabled, p.ScheduledDisableOn)
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
