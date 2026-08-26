package model

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// DisabledModelRef identifies a model that was newly disabled by discovery.
type DisabledModelRef struct {
	ID      uuid.UUID
	ModelID string
}

// MissingScanThreshold is how many consecutive confirmed-missing discovery
// scans it takes before a model is disabled. Each scan-level miss is already
// triple-checked by in-scan confirmation probes (api.ConfirmMissingModels), so
// two independent scans missing the same model is strong evidence it is gone,
// while a single flaky scan (DNS flap, partial upstream listing) never
// disables anything.
const MissingScanThreshold = 2

// RecordMissingModels applies one scan's membership verdict: enabled models
// absent from presentModelIDs get their consecutive-miss streak incremented,
// and only those whose streak reaches MissingScanThreshold are disabled (the
// streak resets so a later reappearance starts clean). Present models have any
// streak cleared. Returns the newly disabled models and the still-enabled
// pending ones (streak below threshold). An empty presentModelIDs list is a
// no-op guard: an empty listing is far more likely a broken scan than a
// provider that removed every model.
//
// A model the operator pinned by enabling it manually (manually_enabled_at, see
// migration 070) is exempt: its streak keeps growing, but it is never disabled
// and appears in NEITHER return slice. The operator tested that model against
// the provider after the listing stopped naming it, so their evidence is newer
// and more direct than the listing's silence — and returning it as pending
// would raise a claim asking them to decide something they already decided.
// The exemption only covers this listing-based path; a refusal on real traffic
// still retires the model (AutoRetireIfConfirmed), and the next sighting hands
// it back to automatic management by clearing the pin.
func (r *Repository) RecordMissingModels(ctx context.Context, providerID uuid.UUID, providerName string, presentModelIDs []string) (disabled, pending []DisabledModelRef, err error) {
	if len(presentModelIDs) == 0 {
		return nil, nil, nil
	}

	// One atomic statement: the CTE clears the streak of every present model
	// (Upsert also does this, but "present" here includes reappeared models a
	// confirmation probe found that the caller did not re-upsert), while the
	// main UPDATE records one confirmed miss for every enabled model the scan
	// did not list. Rows that reach the threshold are disabled with their
	// streak reset (a later reappearance must not sit one flaky scan away
	// from another disable); the rest keep counting into the next scan. A pinned
	// row takes neither branch: its streak accrues untouched, so the count is
	// there to read the moment a sighting clears the pin.
	rows, err := r.pool.Query(ctx, `
		WITH reset AS (
			UPDATE models SET missing_scans = 0
			WHERE provider_id = $1 AND model_id = ANY($2) AND missing_scans > 0
		)
		UPDATE models
		SET missing_scans = CASE WHEN missing_scans + 1 >= $3 AND manually_enabled_at IS NULL THEN 0 ELSE missing_scans + 1 END,
		    enabled = CASE WHEN missing_scans + 1 >= $3 AND manually_enabled_at IS NULL THEN false ELSE enabled END
		WHERE provider_id = $1 AND model_id != ALL($2) AND enabled = true
		RETURNING id, model_id, NOT enabled, manually_enabled_at IS NOT NULL
	`, providerID, presentModelIDs, MissingScanThreshold)
	if err != nil {
		debuglog.Error("model: record missing failed", "provider", providerName, "provider_id", providerID, "error", err)
		return nil, nil, err
	}
	defer rows.Close()
	// Pinned rows are collected separately so they leave both return slices
	// empty-handed: neither a disable to announce nor a claim to raise.
	var pinnedRefs []DisabledModelRef
	for rows.Next() {
		var ref DisabledModelRef
		var wasDisabled, pinned bool
		if err := rows.Scan(&ref.ID, &ref.ModelID, &wasDisabled, &pinned); err != nil {
			debuglog.Error("model: record missing scan failed", "provider", providerName, "provider_id", providerID, "error", err)
			return nil, nil, err
		}
		switch {
		case pinned:
			pinnedRefs = append(pinnedRefs, ref)
		case wasDisabled:
			disabled = append(disabled, ref)
		default:
			pending = append(pending, ref)
		}
	}
	if err := rows.Err(); err != nil {
		debuglog.Error("model: record missing failed", "provider", providerName, "provider_id", providerID, "error", err)
		return nil, nil, err
	}

	if len(pinnedRefs) > 0 {
		debuglog.Info("model: pinned models still missing from listing", "provider", providerName, "count", len(pinnedRefs))
	}
	if len(disabled) > 0 || len(pending) > 0 {
		debuglog.Info("model: recorded missing models",
			"provider", providerName, "provider_id", providerID,
			"disabled", len(disabled), "pending", len(pending), "threshold", MissingScanThreshold)
	}
	InvalidateModelCache()
	return disabled, pending, nil
}

// SetEnabled enables or disables a model by its UUID. This is the OPERATOR
// path: it records the choice in disabled_manually and clears any traffic
// retirement, since a hand-written enabled flag supersedes what the gateway
// concluded on its own (migration 063).
//
// It clears the operator's own dismissal for the same reason, and clearing it
// HERE rather than leaving it to the next sighting is what keeps a claim
// recoverable. Upsert only clears the stamp while auto_retired_at is NULL, so a
// model that is dismissed, enabled by hand, and then retired again by traffic
// before the next discovery scan — seconds versus about an hour, so the likely
// order, not the unlikely one — would carry a dismissal that nothing could ever
// clear again. It would sit disabled and absent from the claim list for good.
// Doing it in the same statement as the enable makes the recovery atomic instead
// of dependent on scan timing.
//
// An enable also arms manually_enabled_at, the pin that keeps discovery from
// disabling the model again for being absent from the provider's listing
// (migration 070). A disable withdraws it: the operator is no longer vouching
// for a model they just switched off.
func (r *Repository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*Model, error) {
	query := `UPDATE models SET enabled = $1, disabled_manually = NOT $1,
	                            auto_retired_at = NULL, discovery_dismissed_at = NULL,
	                            manually_enabled_at = CASE WHEN $1 THEN now() ELSE NULL END
	           WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, enabled, id)
	if err != nil {
		debuglog.Error("model: set enabled failed", "id", id, "enabled", enabled, "error", err)
		return nil, err
	}
	InvalidateModelCache()
	return r.Get(ctx, id)
}

// AutoRetireIfConfirmed disables a model the proxy has concluded the provider no
// longer serves, staging the write inside a transaction and committing it only
// if confirm still holds once the row is written. It reports whether the change
// was committed.
//
// Staging exists because the justification can expire while the write is being
// made: the model can answer a request — proving the decision wrong — mid-write.
// Deciding, writing, then undoing would work on the model row alone, but the
// undo is not enough, because the disabled state is VISIBLE to other sessions in
// between. A concurrent custom-group revalidation that samples it will
// auto-disable the group for having too few routable members, and nothing
// re-enables that group when the model comes back. Staging removes the
// intermediate state rather than correcting it: an abandoned write is never
// committed, so nothing can derive state from it.
//
// It stamps auto_retired_at instead of disabled_manually, which keeps this
// distinct from both an operator's disable and discovery's. See migration 063
// for why all three have to be told apart; the short version is that a
// re-sighting must not revive this model, because the provider was refusing it
// while still listing it.
//
// The write is conditional on the row still being a routable, untouched model.
// What it cannot see is evidence that predates an operator's action: strikes
// gathered before they enabled the model still read as a routable model here.
// That resolves itself rather than needing to be detected — if the model really
// is gone it refuses three more requests and is retired again, with a fresh
// alert, which is the correct answer to an operator enabling a dead model.
//
// confirm runs with the row already written and locked, so keep it to an
// in-memory check — anything slow holds a row lock for its duration.
func (r *Repository) AutoRetireIfConfirmed(ctx context.Context, id uuid.UUID, confirm func() bool) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		debuglog.Error("model: auto-retire begin failed", "id", id, "error", err)
		return false, err
	}
	// Safe on both paths: Rollback after a successful Commit is a no-op.
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row and check it still looks like the model the evidence was
	// gathered against, before writing anything.
	//
	// The decision is made on the request path and executed here, so the row can
	// have moved on in between — an operator disabling it by hand, an operator
	// re-enabling it after an earlier retirement, another member of the fleet
	// retiring it first. Writing by id alone would overwrite whatever they did
	// with a conclusion drawn from traffic that predates it.
	//
	// FOR UPDATE is what makes the check worth making: it holds the row from
	// here until the transaction ends, so no operator write can slip between the
	// check and the commit. Combined with the staging below, the entire decision
	// is atomic with respect to everything else touching this model.
	var enabled, manual bool
	var retiredAt *time.Time
	switch err := tx.QueryRow(ctx,
		`SELECT enabled, disabled_manually, auto_retired_at FROM models WHERE id = $1 FOR UPDATE`,
		id).Scan(&enabled, &manual, &retiredAt); {
	case errors.Is(err, pgx.ErrNoRows):
		// Deleted since the decision. Nothing to retire, and not an error.
		return false, nil
	case err != nil:
		debuglog.Error("model: auto-retire state read failed", "id", id, "error", err)
		return false, err
	}
	if !enabled || manual || retiredAt != nil {
		debuglog.Info("model: skipping auto-retire, the model's state changed since the decision",
			"id", id, "enabled", enabled, "disabled_manually", manual, "already_retired", retiredAt != nil)
		return false, nil
	}

	query := `UPDATE models SET enabled = false, auto_retired_at = now() WHERE id = $1`
	if _, err := tx.Exec(ctx, query, id); err != nil {
		debuglog.Error("model: auto-retire failed", "id", id, "error", err)
		return false, err
	}

	if !confirm() {
		// The deferred rollback discards the write. Nothing else ever saw it,
		// so there is no cache to invalidate and nothing to undo.
		return false, nil
	}

	if err := tx.Commit(ctx); err != nil {
		debuglog.Error("model: auto-retire commit failed", "id", id, "error", err)
		return false, err
	}
	InvalidateModelCache()
	return true, nil
}

// RevertAutoRetire undoes a traffic retirement this gateway wrote, and reports
// whether it actually undid one.
//
// Conditional on the row still being exactly as the retirement left it. The undo
// runs after the disable has committed, so anything can have happened in
// between — and the case that matters is an operator disabling the model by hand
// in that window. An unconditional re-enable would silently return their
// disabled model to routing, overwriting a deliberate decision with a stale
// one. The predicate also covers the model having been re-enabled already, and
// the retirement having been cleared by an operator, both of which mean there is
// nothing here to revert.
func (r *Repository) RevertAutoRetire(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE models
		   SET enabled = true, auto_retired_at = NULL
		 WHERE id = $1
		   AND enabled = false
		   AND disabled_manually = false
		   AND auto_retired_at IS NOT NULL`, id)
	if err != nil {
		debuglog.Error("model: revert auto-retire failed", "id", id, "error", err)
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	InvalidateModelCache()
	return true, nil
}
