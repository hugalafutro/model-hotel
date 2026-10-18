package failover

import (
	"context"
	"maps"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// pruneStaleEntries checks all groups for entries referencing models that no
// longer exist in the database. Stale UUIDs are removed from priority_order
// and entry_enabled. Groups left with ≤1 valid entry are deleted entirely
// (both auto-created and custom), since a failover group with 0 or 1 models
// serves no purpose.
func (r *Repository) pruneStaleEntries(ctx context.Context, groups []*FailoverGroup, result *SyncResult) {
	// Collect all UUIDs referenced across groups and batch-check existence.
	allUUIDs := make(map[uuid.UUID]struct{})
	for _, g := range groups {
		for _, id := range g.PriorityOrder {
			allUUIDs[id] = struct{}{}
		}
	}

	if len(allUUIDs) == 0 {
		return
	}

	// Query which UUIDs still exist in the models table.
	ids := slices.Collect(maps.Keys(allUUIDs))

	rows, err := r.pool.Query(ctx, `SELECT id FROM models WHERE id = ANY($1)`, ids)
	if err != nil {
		debuglog.Error("failover: failed to query existing models for prune", "error", err)
		return
	}
	// A read error aborts the prune: a dropped row would make a live model look
	// deleted, and the prune would strip it from every group it belongs to.
	live, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		debuglog.Error("failover: error reading model rows during prune", "error", err)
		return
	}
	existingIDs := make(map[uuid.UUID]struct{}, len(live))
	for _, id := range live {
		existingIDs[id] = struct{}{}
	}

	// Now prune each group. The stale ids come from the snapshot the caller
	// read; the write and the "is the group still viable" decision both run
	// against the row as it is NOW, so a member added since the read is kept
	// and counted, and a concurrent add cannot be erased by a delete decided on
	// the snapshot.
	for _, g := range groups {
		var prunedIDs []string
		for _, id := range g.PriorityOrder {
			if _, exists := existingIDs[id]; !exists {
				prunedIDs = append(prunedIDs, id.String())
			}
		}
		if len(prunedIDs) == 0 {
			continue // Nothing to prune in this group.
		}

		remaining, err := r.pruneMembership(ctx, g.ID, g.DisplayModel, prunedIDs)
		if err != nil {
			debuglog.Error("failover: failed to update group after pruning", "display_model", g.DisplayModel, "error", err)
			continue
		}
		// Record purged entries only after the successful DB update.
		result.PurgedEntries = append(result.PurgedEntries, PrunedEntryInfo{
			GroupDisplayModel: g.DisplayModel,
			PrunedModelIDs:    prunedIDs,
		})
		if remaining > 1 {
			// Group still viable. pruneMembership never writes group_enabled, so
			// the group's enabled state (and the discovery stamp that goes with
			// it) is preserved structurally rather than by round-tripping the
			// current value.
			debuglog.Info("failover: pruned stale entries from group",
				"display_model", g.DisplayModel,
				"pruned", len(prunedIDs),
				"remaining", remaining)
			continue
		}
		// 0 or 1 valid entries left — delete the group, unless a member landed
		// between the count and this delete (the delete re-checks the row).
		deleted, err := r.deleteIfUnviable(ctx, g.ID)
		if err != nil {
			debuglog.Error("failover: failed to delete pruned group", "display_model", g.DisplayModel, "error", err)
			continue
		}
		if !deleted {
			debuglog.Info("failover: pruned group gained a member before its delete, kept", "display_model", g.DisplayModel)
			continue
		}
		reason := "no valid providers after prune"
		if remaining == 1 {
			reason = "only 1 valid provider after prune (need 2+ for failover)"
		}
		result.DeletedGroups = append(result.DeletedGroups, DeletedGroupInfo{
			DisplayModel:  g.DisplayModel,
			ProviderCount: remaining,
			Reason:        reason,
			ProviderNames: []string{},
		})
		debuglog.Info("failover: deleted group after pruning stale entries",
			"display_model", g.DisplayModel,
			"pruned", len(prunedIDs),
			"remaining", remaining)
	}
}

// deleteIfUnviable deletes a group only while its row still holds 0 or 1
// members, and reports whether it did. The condition runs inside the DELETE,
// so a member added after the caller's count keeps the group.
func (r *Repository) deleteIfUnviable(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM model_failover_groups
		  WHERE id = $1
		    AND (jsonb_typeof(priority_order) <> 'array' OR jsonb_array_length(priority_order) <= 1)`, id)
	InvalidateFailoverCache()
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// pruneMembership removes the stale members from one group's membership and
// reports how many members the row holds afterwards. It is deliberately
// narrower than Update: it writes priority_order and entry_enabled only, never
// group_enabled — and therefore never auto_disabled_at.
//
// The removal happens in SQL, by value, against the row as it is NOW rather
// than as it was read: an operator PUT or a discovery sync that added a member
// between the read and this write keeps that member. Writing the filtered copy
// back would have dropped it, and for a custom group nothing rebuilds the list.
//
// That narrowness is the whole reason it exists instead of reusing Update.
// Update is the OPERATOR's path and clears the discovery stamp on every call,
// which is right there and destructive here: pruning is triggered by a member's
// model row being DELETED (provider removal, bulk delete, rename), which is
// nobody's opinion about the group. Clearing the stamp from here would erase a
// live claim for a group that is still disabled and still unroutable, and
// nothing would ever put it back, because revalidateCustomGroups skips groups
// that are already disabled. The group would stay dead, `hotel/<model>` with it,
// and the badge would be silent about it forever.
//
// Not writing group_enabled at all also expresses "don't silently re-enable a
// manually-disabled group" structurally, instead of reading the current value
// and writing it back through a call that has side effects on other columns.
func (r *Repository) pruneMembership(ctx context.Context, id uuid.UUID, displayModel string, prunedIDs []string) (remaining int, err error) {
	err = r.pool.QueryRow(ctx,
		`UPDATE model_failover_groups
		    SET priority_order = CASE WHEN jsonb_typeof(priority_order) = 'array'
		                             THEN COALESCE((SELECT jsonb_agg(e ORDER BY ord)
		                                              FROM jsonb_array_elements(priority_order) WITH ORDINALITY AS t(e, ord)
		                                             WHERE NOT ((e #>> '{}') = ANY($2::text[]))), '[]'::jsonb)
		                             ELSE '[]'::jsonb END,
		        entry_enabled  = CASE WHEN jsonb_typeof(entry_enabled) = 'object'
		                             THEN entry_enabled - $2::text[]
		                             ELSE '{}'::jsonb END,
		        updated_at     = now()
		  WHERE id = $1
		  RETURNING jsonb_array_length(priority_order)`, id, prunedIDs).Scan(&remaining)
	if err != nil {
		return 0, err
	}
	// Drop the cached copy instead of re-reading and re-caching it: the next
	// lookup refetches, and a surviving cache entry would still list the member
	// that was just pruned. Same idiom as revalidateCustomGroups.
	InvalidateFailoverCacheKey(displayModel)
	return remaining, nil
}

// routableMemberIDs returns the subset of ids whose model is enabled and whose
// provider is enabled — i.e. the members the proxy would actually route to. A
// disabled-but-still-present model is excluded here even though it survives the
// existence check in pruneStaleEntries.
func (r *Repository) routableMemberIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]struct{}, error) {
	routable := make(map[uuid.UUID]struct{})
	if len(ids) == 0 {
		return routable, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT m.id
		FROM models m
		JOIN providers p ON m.provider_id = p.id
		WHERE m.id = ANY($1) AND m.enabled = true AND p.enabled = true
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			// Don't swallow a scan error: a dropped row would make a live
			// member look unroutable and could auto-disable a healthy group.
			return nil, err
		}
		routable[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return routable, nil
}

// revalidateCustomGroups auto-disables every enabled custom failover group that
// no longer has at least two routable members. A member counts as routable when
// its model and its provider are both enabled; the per-entry toggle is
// deliberately ignored, because that is a reversible user choice the router
// already honors, whereas this guard targets the structural case where there are
// simply too few live members to fail over. This closes the gap that lets
// discovery (which disables, never deletes, a vanished model) silently leave a
// custom group with one live member: pruneStaleEntries only removes members
// whose model row is gone, so a disabled-but-present member keeps the group at
// its old size.
//
// Auto-created groups are intentionally skipped: SyncAllModels/SyncForModel
// rebuild or delete those from enabled membership on every sync. Disabling
// (rather than deleting) preserves the user's hand-built membership so the group
// can be re-enabled once a member returns.
func (r *Repository) revalidateCustomGroups(ctx context.Context, groups []*FailoverGroup, result *SyncResult) {
	memberSet := make(map[uuid.UUID]struct{})
	var candidates []*FailoverGroup
	for _, g := range groups {
		if g.AutoCreated || !g.GroupEnabled {
			continue
		}
		candidates = append(candidates, g)
		for _, id := range g.PriorityOrder {
			memberSet[id] = struct{}{}
		}
	}
	if len(candidates) == 0 {
		return
	}

	routable, err := r.routableMemberIDs(ctx, slices.Collect(maps.Keys(memberSet)))
	if err != nil {
		debuglog.Error("failover: failed to query routable members for revalidation", "error", err)
		return
	}

	for _, g := range candidates {
		count := 0
		for _, id := range g.PriorityOrder {
			if _, ok := routable[id]; ok {
				count++
			}
		}
		if count >= 2 {
			continue
		}
		// auto_disabled_at is what makes this distinguishable from the operator
		// switching the same group off by hand: both write group_enabled = false
		// and bump updated_at, and nothing else in the row tells them apart. The
		// discovery-claim badge counts only groups carrying this stamp, so a
		// deliberate operator disable never nags them (migration 062). Every
		// operator-driven write of group_enabled clears it back to NULL.
		if _, err := r.pool.Exec(ctx,
			`UPDATE model_failover_groups
			    SET group_enabled = false, auto_disabled_at = now(), updated_at = now()
			  WHERE id = $1`,
			g.ID); err != nil {
			debuglog.Error("failover: failed to auto-disable undersized custom group", "display_model", g.DisplayModel, "error", err)
			continue
		}
		// Reflect the disable on the in-memory struct so callers that already hold
		// this slice (e.g. the list handler) don't need to re-query.
		g.GroupEnabled = false
		// Invalidate this group's cache key precisely rather than flushing the
		// whole failover cache for every disabled group.
		InvalidateFailoverCacheKey(g.DisplayModel)
		result.DisabledGroups = append(result.DisabledGroups, DisabledGroupInfo{
			DisplayModel:   g.DisplayModel,
			EffectiveCount: count,
			Reason:         "fewer than 2 routable members (need 2+ for failover)",
		})
		debuglog.Info("failover: auto-disabled custom group with too few routable members",
			"display_model", g.DisplayModel, "routable", count)
	}
}

// RevalidateCustomGroups auto-disables enabled custom failover groups that have
// dropped below two routable members. It lists the current groups and applies
// revalidateCustomGroups, returning the resulting DisabledGroups so callers (the
// discovery scan) can fold them into their change report.
func (r *Repository) RevalidateCustomGroups(ctx context.Context) (*SyncResult, error) {
	groups, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	result := &SyncResult{}
	r.revalidateCustomGroups(ctx, groups, result)
	return result, nil
}

// RevalidateCustomGroupsIn revalidates a caller-supplied groups slice instead of
// querying for it, auto-disabling undersized custom groups and flipping their
// GroupEnabled flag in place. The list handler uses this to revalidate the same
// slice it already fetched, avoiding a second List round-trip per request.
func (r *Repository) RevalidateCustomGroupsIn(ctx context.Context, groups []*FailoverGroup) *SyncResult {
	result := &SyncResult{}
	r.revalidateCustomGroups(ctx, groups, result)
	return result
}
