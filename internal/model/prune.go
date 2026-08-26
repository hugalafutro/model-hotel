package model

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// PrunedModel is what PruneRetired reports for each deleted row: enough for
// the log line and the failover resync, nothing else.
type PrunedModel struct {
	ID           uuid.UUID
	ProviderID   uuid.UUID
	ProviderName string
	ModelID      string
}

// PruneRetired deletes rows that discovery retired before horizon and that
// nothing else has a claim on, oldest first, at most limit of them. Only rows
// of the given providers are considered: the caller passes the providers whose
// scan just succeeded, so a provider that could not be reached this pass keeps
// every row. A model the change journal saw come back (added or re-enabled)
// since flapSince is excluded, inside the same statements that select and
// delete, so a return that lands while the prune runs still keeps its row;
// those rows are flapping, not retired. The row's own retirement is a
// "disabled" journal entry and does not count: it is the retirement, not a
// flap, which is what lets a horizon shorter than the claim window prune.
//
// Kept, always: rows the operator switched off (disabled_manually), pinned on
// (manually_enabled_at), rows the proxy retired from traffic (auto_retired_at,
// the provider still lists those), and rows of a disabled provider (parked
// with their pins, prices and failover memberships for a re-enable). A
// dismissed claim is not a reason to keep a row: dismissal acknowledges the
// retirement, it does not undo it.
//
// The model cache is invalidated only when something was deleted.
func (r *Repository) PruneRetired(ctx context.Context, horizon, flapSince time.Time, providerIDs []uuid.UUID, limit int) ([]PrunedModel, error) {
	candidates, err := r.selectPruneCandidates(ctx, horizon, flapSince, providerIDs, limit)
	if err != nil || len(candidates) == 0 {
		return nil, err
	}
	return r.deletePruneCandidates(ctx, candidates, flapSince)
}

// notFlappedSince is the SQL predicate shared by the prune's SELECT and
// DELETE: the row's (provider, model) pair has not come back (no "added" or
// "reenabled" entry) in the discovery change journal since the bound
// parameter. The "disabled" bucket api.flapCounts also reads is deliberately
// left out: every retired row has exactly one of those (its retirement), so
// counting it would keep every row for the whole claim window and make any
// horizon shorter than the window inert. Applied in the DELETE as well as the
// SELECT so eligibility is decided by the statement that deletes, not by an
// earlier snapshot.
const notFlappedSince = `NOT EXISTS (
		SELECT 1
		  FROM discovery_changes dc
		  CROSS JOIN LATERAL jsonb_array_elements(
		           COALESCE(dc.diff->'added',     '[]'::jsonb) ||
		           COALESCE(dc.diff->'reenabled', '[]'::jsonb)
		       ) AS e
		 WHERE dc.provider_id = m.provider_id
		   AND dc.detected_at >= %s
		   AND e->>'model_id' = m.model_id)`

// selectPruneCandidates finds rows that discovery retired before horizon and
// that nothing else has a claim on, oldest first, at most limit of them. Only
// rows of the given providers are considered: the caller passes the providers
// whose scan just succeeded, so a provider that could not be reached this
// pass keeps every row. notFlappedSince excludes models the change journal
// saw come back within the claims window; those are flapping, not retired.
//
// Kept, always: rows the operator switched off (disabled_manually), pinned on
// (manually_enabled_at), rows the proxy retired from traffic (auto_retired_at,
// the provider still lists those), and rows of a disabled provider (parked
// with their pins, prices and failover memberships for a re-enable). A
// dismissed claim is not a reason to keep a row: dismissal acknowledges the
// retirement, it does not undo it.
func (r *Repository) selectPruneCandidates(ctx context.Context, horizon, flapSince time.Time, providerIDs []uuid.UUID, limit int) ([]PrunedModel, error) {
	if len(providerIDs) == 0 || limit <= 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.provider_id, p.name, m.model_id
		  FROM models m
		  JOIN providers p ON p.id = m.provider_id
		 WHERE m.provider_id = ANY($1)
		   AND COALESCE(p.enabled, false) = true
		   AND m.enabled = false
		   AND m.disabled_manually = false
		   AND m.manually_enabled_at IS NULL
		   AND m.auto_retired_at IS NULL
		   AND COALESCE(m.last_seen_at, m.created_at) < $2
		   AND `+fmt.Sprintf(notFlappedSince, "$3")+`
		 ORDER BY COALESCE(m.last_seen_at, m.created_at) ASC, m.id ASC
		 LIMIT $4`,
		providerIDs, horizon, flapSince, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[PrunedModel])
}

// deletePruneCandidates deletes exactly the candidates that are still
// eligible for pruning at delete time and reports only those. It re-checks
// both the model's own state and its provider's enabled flag inside the
// DELETE: a scan or an operator action landing between the SELECT that built
// candidates and this call can re-enable a row or its provider, and that row
// must stay. The model cache is invalidated only when something was deleted.
func (r *Repository) deletePruneCandidates(ctx context.Context, candidates []PrunedModel, flapSince time.Time) ([]PrunedModel, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(candidates))
	for i, c := range candidates {
		ids[i] = c.ID
	}
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM models m
		 USING providers p
		 WHERE p.id = m.provider_id
		   AND m.id = ANY($1)
		   AND m.enabled = false
		   AND m.disabled_manually = false
		   AND m.manually_enabled_at IS NULL
		   AND m.auto_retired_at IS NULL
		   AND COALESCE(p.enabled, false) = true
		   AND `+fmt.Sprintf(notFlappedSince, "$2"), ids, flapSince)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, nil
	}
	InvalidateModelCache()
	if int(tag.RowsAffected()) == len(candidates) {
		return candidates, nil
	}
	// Something was revived mid-flight: report only what is actually gone.
	alive, err := r.aliveModelIDs(ctx, ids)
	if err != nil {
		// The DELETE already committed, so the rows are gone whatever this
		// query says. Report every candidate: the caller still has to resync
		// the failover groups they belonged to, and a revived row costs one
		// redundant resync where a dropped row would leave a stale group.
		debuglog.Debug("prune: reconciliation query failed, reporting all candidates", "error", err)
		return candidates, nil
	}
	deleted := candidates[:0]
	for _, c := range candidates {
		if !alive[c.ID] {
			deleted = append(deleted, c)
		}
	}
	return deleted, nil
}

// aliveModelIDs reports which of ids still exist in the models table. It
// queries the table directly instead of going through the model cache: a
// cache fill racing the DELETE can still hold a deleted row and would report
// it as alive.
func (r *Repository) aliveModelIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM models WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	found, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	alive := make(map[uuid.UUID]bool, len(found))
	for _, id := range found {
		alive[id] = true
	}
	return alive, nil
}
