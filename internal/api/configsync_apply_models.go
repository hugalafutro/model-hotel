package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/model"
)

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
// Raw SQL, so the settings repository's cache is not evicted for the key; it is
// read raw as well, and a repository read would be stale (value or absence) for
// up to the cache TTL.
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
