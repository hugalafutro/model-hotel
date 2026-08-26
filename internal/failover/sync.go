package failover

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// DeletedGroupInfo describes a failover group that was deleted during sync.
type DeletedGroupInfo struct {
	DisplayModel  string   `json:"display_model"`
	Reason        string   `json:"reason"`
	ProviderCount int      `json:"provider_count"`
	ProviderNames []string `json:"provider_names"`
}

// PrunedEntryInfo describes entries removed from a group during sync
// because they reference models that no longer exist in the database.
type PrunedEntryInfo struct {
	GroupDisplayModel string   `json:"group_display_model"`
	PrunedModelIDs    []string `json:"pruned_model_ids"`
}

// UpdatedGroupInfo describes membership changes applied to a group during sync.
type UpdatedGroupInfo struct {
	DisplayModel    string   `json:"display_model"`
	RemovedModelIDs []string `json:"removed_model_ids,omitempty"` // model UUIDs dropped
	AddedModelIDs   []string `json:"added_model_ids,omitempty"`   // model UUIDs added
}

// DisabledGroupInfo describes a custom failover group that sync auto-disabled
// because it no longer has the two routable members a failover group needs (a
// member's model or provider was disabled, e.g. by discovery dropping a model
// the provider stopped listing). The group's membership is kept intact so the
// user can re-enable it once a member returns.
type DisabledGroupInfo struct {
	DisplayModel   string `json:"display_model"`
	EffectiveCount int    `json:"effective_count"`
	Reason         string `json:"reason"`
}

// SyncResult describes the outcome of a failover group sync operation.
type SyncResult struct {
	DeletedGroups  []DeletedGroupInfo  `json:"deleted_groups"`
	UpdatedGroups  []UpdatedGroupInfo  `json:"updated_groups,omitempty"`
	PurgedEntries  []PrunedEntryInfo   `json:"purged_entries,omitempty"`
	DisabledGroups []DisabledGroupInfo `json:"disabled_groups,omitempty"`
	SyncErrors     []string            `json:"sync_errors,omitempty"`
}

// mergePriorityOrder preserves the user's existing priority order while
// incorporating new models and dropping removed ones.
// Entries already in existingOrder (and still present in currentIDs) keep
// their relative position. New entries not in existingOrder are appended at
// the end in the order they appear in currentIDs.
func mergePriorityOrder(existingOrder, currentIDs []uuid.UUID) []uuid.UUID {
	currentSet := make(map[uuid.UUID]struct{}, len(currentIDs))
	for _, id := range currentIDs {
		currentSet[id] = struct{}{}
	}

	seen := make(map[uuid.UUID]struct{})
	merged := make([]uuid.UUID, 0, len(currentIDs))

	// First: keep existing entries that are still present (preserves user order).
	// Guard against duplicate UUIDs in existingOrder.
	for _, id := range existingOrder {
		if _, ok := currentSet[id]; ok {
			if _, already := seen[id]; !already {
				merged = append(merged, id)
				seen[id] = struct{}{}
			}
		}
	}

	// Then: append new entries not seen before
	for _, id := range currentIDs {
		if _, ok := seen[id]; !ok {
			merged = append(merged, id)
		}
	}

	return merged
}

// normalizeBaseModel returns the canonical base model name used for failover
// grouping. It takes the segment after the last "/" (the actual model name)
// and lowercases it, so that "GLM-5.1", "glm-5.1", "zai-org/glm-5.1",
// "zai-org/anthracite-org/magnum-v4-72b", and "anthracite-org/magnum-v4-72b"
// all normalize to their leaf model name for grouping.
func normalizeBaseModel(modelID string) string {
	if idx := strings.LastIndex(modelID, "/"); idx >= 0 {
		return strings.ToLower(modelID[idx+1:])
	}
	return strings.ToLower(modelID)
}

// deleteUndersizedAutoGroup deletes the auto-created group for base when fewer
// than two enabled providers remain, recording the deletion in result.
// providerCount is the number of enabled providers found (0 or 1);
// providerNames may be empty when the caller only resolved UUIDs.
// No-op when no auto group exists for base.
func (r *Repository) deleteUndersizedAutoGroup(ctx context.Context, base string, providerCount int, providerNames []string, result *SyncResult) {
	if !r.deleteAutoGroup(ctx, base) {
		return
	}
	reason := "no enabled providers found"
	if providerCount == 1 {
		reason = "only 1 enabled provider (need 2+ for failover)"
	}
	result.DeletedGroups = append(result.DeletedGroups, DeletedGroupInfo{
		DisplayModel:  base,
		ProviderCount: providerCount,
		Reason:        reason,
		ProviderNames: providerNames,
	})
}

// upsertAutoGroup creates or updates the auto failover group for base from the
// current enabled member model UUIDs, preserving the existing group's entry
// toggles (for members still present), user priority order, display name, and
// description. It returns the pre-upsert group snapshot (nil when the group is
// new) and the merged priority order that was written.
func (r *Repository) upsertAutoGroup(ctx context.Context, base string, currentIDs []uuid.UUID) (existing *FailoverGroup, priorityOrder []uuid.UUID, err error) {
	entryEnabled := make(map[string]bool, len(currentIDs))
	for _, id := range currentIDs {
		entryEnabled[id.String()] = true
	}

	existing, _ = r.GetByModel(ctx, base)
	if existing != nil {
		for uuidStr, enabled := range existing.EntryEnabled {
			if _, stillPresent := entryEnabled[uuidStr]; stillPresent {
				entryEnabled[uuidStr] = enabled
			}
		}
	}

	priorityOrder = currentIDs
	if existing != nil {
		priorityOrder = mergePriorityOrder(existing.PriorityOrder, currentIDs)
	}

	groupEnabled := true
	autoCreated := true
	var syncDisplayName, syncDescription *string
	if existing != nil {
		syncDisplayName = existing.DisplayName
		if existing.Description != "" {
			syncDescription = &existing.Description
		}
	}
	_, err = r.UpsertWithConfig(ctx, base, priorityOrder, entryEnabled, &groupEnabled, syncDisplayName, syncDescription, &autoCreated)
	return existing, priorityOrder, err
}

// diffGroupMembership reports which model UUIDs the sync removed from and added
// to a group, comparing the pre-upsert snapshot against the current members.
// A nil existing (brand-new group) reports every member as added.
func diffGroupMembership(existing *FailoverGroup, currentIDs []uuid.UUID) (removed, added []string) {
	if existing == nil {
		added = make([]string, 0, len(currentIDs))
		for _, id := range currentIDs {
			added = append(added, id.String())
		}
		return nil, added
	}

	currentSet := make(map[uuid.UUID]struct{}, len(currentIDs))
	for _, id := range currentIDs {
		currentSet[id] = struct{}{}
	}
	existingSet := make(map[uuid.UUID]struct{}, len(existing.PriorityOrder))
	for _, id := range existing.PriorityOrder {
		existingSet[id] = struct{}{}
	}
	for _, id := range existing.PriorityOrder {
		if _, ok := currentSet[id]; !ok {
			removed = append(removed, id.String())
		}
	}
	for _, id := range currentIDs {
		if _, ok := existingSet[id]; !ok {
			added = append(added, id.String())
		}
	}
	return removed, added
}

// SyncAllModels synchronizes all enabled models with providers and updates failover groups.
func (r *Repository) SyncAllModels(ctx context.Context) (*SyncResult, error) {
	result := &SyncResult{}

	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.model_id, m.provider_id, p.name
		FROM models m
		JOIN providers p ON m.provider_id = p.id
		WHERE m.enabled = true AND p.enabled = true
		ORDER BY m.model_id, p.created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type modelInfo struct {
		uuid         uuid.UUID
		modelID      string
		providerID   uuid.UUID
		providerName string
	}

	baseToModels := make(map[string][]modelInfo)
	for rows.Next() {
		var id, providerID uuid.UUID
		var modelID, providerName string
		if err := rows.Scan(&id, &modelID, &providerID, &providerName); err != nil {
			debuglog.Warn("failover: skipping unscannable model row during sync", "error", err)
			continue
		}
		base := normalizeBaseModel(modelID)
		baseToModels[base] = append(baseToModels[base], modelInfo{
			uuid:         id,
			modelID:      modelID,
			providerID:   providerID,
			providerName: providerName,
		})
	}
	if err := rows.Err(); err != nil {
		debuglog.Error("failover: error iterating model rows during SyncAllModels", "error", err)
		return nil, err
	}

	syncedBases := make(map[string]bool)
	for base, models := range baseToModels {
		if len(models) <= 1 {
			providerNames := make([]string, 0, len(models))
			for _, m := range models {
				providerNames = append(providerNames, m.providerName)
			}
			r.deleteUndersizedAutoGroup(ctx, base, len(models), providerNames, result)
			continue
		}

		currentIDs := make([]uuid.UUID, len(models))
		for i, m := range models {
			currentIDs[i] = m.uuid
		}

		syncedBases[base] = true
		if _, _, err := r.upsertAutoGroup(ctx, base, currentIDs); err != nil {
			result.SyncErrors = append(result.SyncErrors, fmt.Sprintf("%s: %v", base, err))
			continue
		}
	}

	allGroups, _ := r.List(ctx)
	for _, g := range allGroups {
		if g.AutoCreated {
			if _, ok := syncedBases[g.DisplayModel]; !ok {
				if r.deleteAutoGroup(ctx, g.DisplayModel) {
					result.DeletedGroups = append(result.DeletedGroups, DeletedGroupInfo{
						DisplayModel:  g.DisplayModel,
						ProviderCount: 0,
						Reason:        "no enabled providers found",
						ProviderNames: []string{},
					})
				}
			}
		}
	}

	// Prune stale entries from all groups (auto and custom).
	// Models may have been deleted (e.g. provider cascade) leaving
	// UUIDs in priority_order/entry_enabled that reference non-existent rows.
	// Filter out groups already deleted in the loop above to avoid duplicate
	// DeletedGroups entries.
	var groupsForPrune []*FailoverGroup
	for _, g := range allGroups {
		alreadyDeleted := false
		for _, dg := range result.DeletedGroups {
			if dg.DisplayModel == g.DisplayModel {
				alreadyDeleted = true
				break
			}
		}
		if !alreadyDeleted {
			groupsForPrune = append(groupsForPrune, g)
		}
	}
	r.pruneStaleEntries(ctx, groupsForPrune, result)

	// Auto-disable custom groups that dropped below two routable members (a
	// member's model or provider was disabled, not deleted, so prune left it in
	// place). Re-list so the revalidation sees the post-prune state.
	if afterPrune, err := r.List(ctx); err == nil {
		r.revalidateCustomGroups(ctx, afterPrune, result)
	} else {
		debuglog.Error("failover: failed to re-list groups for custom-group revalidation", "error", err)
	}

	debuglog.Info("failover: synced groups", "synced", len(syncedBases), "deleted", len(result.DeletedGroups))

	return result, nil
}

// SyncForModel syncs the failover group for a specific model. The returned
// SyncResult describes the group changes applied (never nil on success).
func (r *Repository) SyncForModel(ctx context.Context, modelID string) (*SyncResult, error) {
	base := normalizeBaseModel(modelID)
	result := &SyncResult{}

	// Match all enabled models whose leaf name (after last "/", lowercased) equals base.
	// SUBSTRING(... FROM '[^/]+$') extracts the segment after the last "/".
	// This handles "glm-5.1", "GLM-5.1", "zai-org/glm-5.1",
	// "zai-org/anthracite-org/magnum-v4-72b", etc.
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.provider_id
		FROM models m
		JOIN providers p ON m.provider_id = p.id
		WHERE m.enabled = true AND p.enabled = true
		  AND LOWER(SUBSTRING(m.model_id FROM '[^/]+$')) = $1
		ORDER BY p.created_at ASC
	`, base)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var currentIDs []uuid.UUID
	for rows.Next() {
		var id, providerID uuid.UUID
		if err := rows.Scan(&id, &providerID); err != nil {
			debuglog.Warn("failover: skipping unscannable group-member row", "error", err)
			continue
		}
		currentIDs = append(currentIDs, id)
	}
	if err := rows.Err(); err != nil {
		debuglog.Error("failover: error iterating model rows during SyncForModel", "error", err)
		return nil, err
	}

	if len(currentIDs) <= 1 {
		r.deleteUndersizedAutoGroup(ctx, base, len(currentIDs), []string{}, result)
		return result, nil
	}

	existing, priorityOrder, err := r.upsertAutoGroup(ctx, base, currentIDs)
	if err != nil {
		debuglog.Error("failover: failed to sync group", "display_model", base, "error", err)
		return nil, err
	}

	// Report membership changes so discovery summaries show what the sync did;
	// a brand-new auto-group reports every member as added instead of being silent.
	if removed, added := diffGroupMembership(existing, currentIDs); len(removed) > 0 || len(added) > 0 {
		result.UpdatedGroups = append(result.UpdatedGroups, UpdatedGroupInfo{
			DisplayModel:    base,
			RemovedModelIDs: removed,
			AddedModelIDs:   added,
		})
	}

	debuglog.Info("failover: synced group", "display_model", base, "providers", len(priorityOrder))
	return result, nil
}

// PruneModelUUID finds failover groups containing the given model UUID in their
// priority_order and prunes stale entries from them. This is called after a
// model is deleted to clean up custom groups that may reference it, which
// SyncForModel alone does not handle (it only manages the auto-group for the
// deleted model's base name).
func (r *Repository) PruneModelUUID(ctx context.Context, modelUUID uuid.UUID) error {
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		WHERE priority_order::jsonb @> to_jsonb(ARRAY[$1]::uuid[])
	`, modelUUID)
	if err != nil {
		return fmt.Errorf("PruneModelUUID: query groups containing %s: %w", modelUUID, err)
	}
	defer rows.Close()

	groups, err := scanFailoverGroups(rows)
	if err != nil {
		return fmt.Errorf("PruneModelUUID: scan groups: %w", err)
	}

	if len(groups) == 0 {
		return nil
	}

	result := &SyncResult{}
	r.pruneStaleEntries(ctx, groups, result)

	for _, d := range result.DeletedGroups {
		debuglog.Info("failover: pruned group after model deletion",
			"display_model", d.DisplayModel, "reason", d.Reason)
	}
	for _, p := range result.PurgedEntries {
		debuglog.Info("failover: pruned stale entries after model deletion",
			"display_model", p.GroupDisplayModel, "pruned", len(p.PrunedModelIDs))
	}
	return nil
}

func (r *Repository) deleteAutoGroup(ctx context.Context, displayModel string) bool {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM model_failover_groups
		WHERE display_model = $1 AND auto_created = true
	`, displayModel)
	if err == nil && tag.RowsAffected() > 0 {
		InvalidateFailoverCache()
		debuglog.Info("failover: deleted auto-group", "display_model", displayModel)
		return true
	}
	return false
}
