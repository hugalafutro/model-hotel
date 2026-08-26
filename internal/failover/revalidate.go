package failover

import (
	"context"

	"github.com/google/uuid"

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
	existingIDs := make(map[uuid.UUID]struct{})
	ids := make([]uuid.UUID, 0, len(allUUIDs))
	for id := range allUUIDs {
		ids = append(ids, id)
	}

	rows, err := r.pool.Query(ctx, `SELECT id FROM models WHERE id = ANY($1)`, ids)
	if err != nil {
		debuglog.Error("failover: failed to query existing models for prune", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			continue
		}
		existingIDs[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		debuglog.Error("failover: error iterating model rows during prune", "error", err)
		return
	}

	// Now prune each group.
	for _, g := range groups {
		var validPriority []uuid.UUID
		var prunedIDs []string

		for _, id := range g.PriorityOrder {
			if _, exists := existingIDs[id]; exists {
				validPriority = append(validPriority, id)
			} else {
				prunedIDs = append(prunedIDs, id.String())
			}
		}

		if len(prunedIDs) == 0 {
			continue // Nothing to prune in this group.
		}

		if len(validPriority) <= 1 {
			// Group has 0 or 1 valid entries left — delete it.
			if err := r.DeleteByID(ctx, g.ID); err != nil {
				debuglog.Error("failover: failed to delete pruned group", "display_model", g.DisplayModel, "error", err)
				continue
			}
			// Record purged entries and deleted group only after successful DB operations.
			result.PurgedEntries = append(result.PurgedEntries, PrunedEntryInfo{
				GroupDisplayModel: g.DisplayModel,
				PrunedModelIDs:    prunedIDs,
			})
			reason := "no valid providers after prune"
			if len(validPriority) == 1 {
				reason = "only 1 valid provider after prune (need 2+ for failover)"
			}
			result.DeletedGroups = append(result.DeletedGroups, DeletedGroupInfo{
				DisplayModel:  g.DisplayModel,
				ProviderCount: len(validPriority),
				Reason:        reason,
				ProviderNames: []string{},
			})
			debuglog.Info("failover: deleted group after pruning stale entries",
				"display_model", g.DisplayModel,
				"pruned", len(prunedIDs),
				"remaining", len(validPriority))
		} else {
			// Group still viable — rewrite its membership only. pruneMembership
			// never writes group_enabled, so the group's enabled state (and the
			// discovery stamp that goes with it) is preserved structurally
			// rather than by round-tripping the current value.
			validEntryEnabled := make(map[string]bool)
			for _, id := range validPriority {
				if enabled, ok := g.EntryEnabled[id.String()]; ok {
					validEntryEnabled[id.String()] = enabled
				} else {
					validEntryEnabled[id.String()] = true
				}
			}
			err := r.pruneMembership(ctx, g.ID, g.DisplayModel, validPriority, validEntryEnabled)
			if err != nil {
				debuglog.Error("failover: failed to update group after pruning", "display_model", g.DisplayModel, "error", err)
			} else {
				// Record purged entries only after successful DB update.
				result.PurgedEntries = append(result.PurgedEntries, PrunedEntryInfo{
					GroupDisplayModel: g.DisplayModel,
					PrunedModelIDs:    prunedIDs,
				})
				debuglog.Info("failover: pruned stale entries from group",
					"display_model", g.DisplayModel,
					"pruned", len(prunedIDs),
					"remaining", len(validPriority))
			}
		}
	}
}

// pruneMembership rewrites one group's membership after stale entries were
// removed. It is deliberately narrower than Update: it writes priority_order and
// entry_enabled only, never group_enabled — and therefore never
// auto_disabled_at.
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
func (r *Repository) pruneMembership(ctx context.Context, id uuid.UUID, displayModel string, priorityOrder []uuid.UUID, entryEnabled map[string]bool) error {
	priorityJSON, err := jsonMarshal(priorityOrder)
	if err != nil {
		return err
	}
	entryEnabledJSON, err := jsonMarshal(entryEnabled)
	if err != nil {
		return err
	}
	if _, err := r.pool.Exec(ctx,
		`UPDATE model_failover_groups
		    SET priority_order = $2, entry_enabled = $3, updated_at = now()
		  WHERE id = $1`, id, priorityJSON, entryEnabledJSON); err != nil {
		return err
	}
	// Drop the cached copy instead of re-reading and re-caching it: the next
	// lookup refetches, and a surviving cache entry would still list the member
	// that was just pruned. Same idiom as revalidateCustomGroups.
	InvalidateFailoverCacheKey(displayModel)
	return nil
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

	memberIDs := make([]uuid.UUID, 0, len(memberSet))
	for id := range memberSet {
		memberIDs = append(memberIDs, id)
	}
	routable, err := r.routableMemberIDs(ctx, memberIDs)
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
