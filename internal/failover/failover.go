package failover

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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

		// Record what was pruned.
		result.PurgedEntries = append(result.PurgedEntries, PrunedEntryInfo{
			GroupDisplayModel: g.DisplayModel,
			PrunedModelIDs:    prunedIDs,
		})

		if len(validPriority) <= 1 {
			// Group has 0 or 1 valid entries left — delete it.
			if err := r.DeleteByID(ctx, g.ID); err != nil {
				debuglog.Error("failover: failed to delete pruned group", "display_model", g.DisplayModel, "error", err)
				continue
			}
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
			// Group still viable — update with pruned entries.
			validEntryEnabled := make(map[string]bool)
			for _, id := range validPriority {
				if enabled, ok := g.EntryEnabled[id.String()]; ok {
					validEntryEnabled[id.String()] = enabled
				} else {
					validEntryEnabled[id.String()] = true
				}
			}
			_, err := r.Update(ctx, g.ID, validPriority, validEntryEnabled, nil, nil, nil)
			if err != nil {
				debuglog.Error("failover: failed to update group after pruning", "display_model", g.DisplayModel, "error", err)
			} else {
				debuglog.Info("failover: pruned stale entries from group",
					"display_model", g.DisplayModel,
					"pruned", len(prunedIDs),
					"remaining", len(validPriority))
			}
		}
	}
}

var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = json.Unmarshal
)

// FailoverGroup represents a configured failover group for a model.
//
//nolint:revive // stutter and exported are acceptable: FailoverGroup is a domain concept
type FailoverGroup struct {
	ID            uuid.UUID       `json:"id"`
	DisplayModel  string          `json:"display_model"`
	DisplayName   *string         `json:"display_name"`
	Description   string          `json:"description"`
	PriorityOrder []uuid.UUID     `json:"priority_order"`
	EntryEnabled  map[string]bool `json:"entry_enabled"`
	GroupEnabled  bool            `json:"group_enabled"`
	AutoCreated   bool            `json:"auto_created"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// Repository provides persistence for failover groups.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new failover group repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// GetByModel retrieves a failover group by its display model name.
func (r *Repository) GetByModel(ctx context.Context, modelID string) (*FailoverGroup, error) {
	if fg, ok := GetCachedFailoverByModel(modelID); ok {
		return fg, nil
	}

	var fg FailoverGroup
	var priorityJSON []byte
	var entryEnabledJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		WHERE display_model = $1
	`, modelID).Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &priorityJSON,
		&entryEnabledJSON, &fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

// Upsert creates or updates a failover group with the given priority order.
func (r *Repository) Upsert(ctx context.Context, displayModel string, priorityOrder []uuid.UUID) (*FailoverGroup, error) {
	return r.UpsertWithConfig(ctx, displayModel, priorityOrder, nil, nil, nil, nil, nil)
}

// UpsertWithConfig creates or updates a failover group with full configuration options.
func (r *Repository) UpsertWithConfig(ctx context.Context, displayModel string, priorityOrder []uuid.UUID,
	entryEnabled map[string]bool, groupEnabled *bool, displayName, description *string, autoCreated *bool) (*FailoverGroup, error) {
	priorityJSON, err := jsonMarshal(priorityOrder)
	if err != nil {
		return nil, err
	}

	entryEnabledJSON, err := jsonMarshal(entryEnabled)
	if err != nil {
		return nil, err
	}

	groupEnabledVal := true
	if groupEnabled != nil {
		groupEnabledVal = *groupEnabled
	}

	autoCreatedVal := false
	if autoCreated != nil {
		autoCreatedVal = *autoCreated
	}

	// Build ON CONFLICT DO UPDATE SET clause dynamically
	// so that nil display_name/description means "don't touch",
	// not "overwrite with NULL".
	// The INSERT VALUES positions are fixed ($1-$7), so the DO UPDATE SET
	// clause can reference them directly — we just conditionally include
	// display_name and description columns.
	doSetClauses := []string{
		"priority_order = $2",
		"entry_enabled = $3",
		"group_enabled = $4",
	}
	if displayName != nil {
		doSetClauses = append(doSetClauses, "display_name = $5")
	}
	if description != nil {
		doSetClauses = append(doSetClauses, "description = $6")
	}
	doSetClauses = append(doSetClauses, "auto_created = $7", "updated_at = now()")

	query := fmt.Sprintf(`INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, display_name, description, auto_created)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (display_model)
		DO UPDATE SET %s
		RETURNING id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		          COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		          created_at, COALESCE(updated_at, created_at)`, strings.Join(doSetClauses, ", "))

	var fg FailoverGroup
	var rawPriority, rawEntryEnabled []byte

	err = r.pool.QueryRow(ctx, query, displayModel, priorityJSON, entryEnabledJSON, groupEnabledVal, displayName, description, autoCreatedVal).
		Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &rawPriority, &rawEntryEnabled,
			&fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(rawPriority, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(rawEntryEnabled, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

// Delete removes a failover group by its display model name.
func (r *Repository) Delete(ctx context.Context, displayModel string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE display_model = $1`, displayModel)
	InvalidateFailoverCache()
	return err
}

// DeleteByID removes a failover group by its ID.
func (r *Repository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE id = $1`, id)
	InvalidateFailoverCache()
	return err
}

// GetByID retrieves a failover group by its ID.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*FailoverGroup, error) {
	var fg FailoverGroup
	var priorityJSON []byte
	var entryEnabledJSON []byte

	err := r.pool.QueryRow(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		WHERE id = $1
	`, id).Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &priorityJSON,
		&entryEnabledJSON, &fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

// GetEnabled returns all enabled failover groups.
func (r *Repository) GetEnabled(ctx context.Context) ([]*FailoverGroup, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), group_enabled, COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		WHERE group_enabled = true
		ORDER BY display_model
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanFailoverGroups(rows)
}

// Update modifies an existing failover group by ID.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, priorityOrder []uuid.UUID,
	entryEnabled map[string]bool, groupEnabled *bool, displayName, description *string) (*FailoverGroup, error) {
	priorityJSON, err := jsonMarshal(priorityOrder)
	if err != nil {
		return nil, err
	}

	entryEnabledJSON, err := jsonMarshal(entryEnabled)
	if err != nil {
		return nil, err
	}

	groupEnabledVal := true
	if groupEnabled != nil {
		groupEnabledVal = *groupEnabled
	}

	var setClauses []string
	var args []interface{}
	argIdx := 2 // $1 is reserved for id

	setClauses = append(setClauses, fmt.Sprintf("priority_order = $%d", argIdx))
	args = append(args, priorityJSON)
	argIdx++

	setClauses = append(setClauses, fmt.Sprintf("entry_enabled = $%d", argIdx))
	args = append(args, entryEnabledJSON)
	argIdx++

	setClauses = append(setClauses, fmt.Sprintf("group_enabled = $%d", argIdx))
	args = append(args, groupEnabledVal)
	argIdx++

	if displayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
		args = append(args, *displayName)
		argIdx++
	}

	if description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *description)
	}

	setClauses = append(setClauses, "updated_at = now()")

	args = append([]interface{}{id}, args...)

	query := fmt.Sprintf(`UPDATE model_failover_groups SET %s WHERE id = $1
		RETURNING id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		          COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		          created_at, COALESCE(updated_at, created_at)`, strings.Join(setClauses, ", "))

	var fg FailoverGroup
	var rawPriority, rawEntryEnabled []byte

	err = r.pool.QueryRow(ctx, query, args...).
		Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &rawPriority, &rawEntryEnabled,
			&fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(rawPriority, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := jsonUnmarshal(rawEntryEnabled, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

// List returns all failover groups.
func (r *Repository) List(ctx context.Context) ([]*FailoverGroup, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order,
		       COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		       created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		ORDER BY display_model
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanFailoverGroups(rows)
}

func scanFailoverGroups(rows pgx.Rows) ([]*FailoverGroup, error) {
	var groups []*FailoverGroup
	for rows.Next() {
		var fg FailoverGroup
		var priorityJSON []byte
		var entryEnabledJSON []byte
		if err := rows.Scan(&fg.ID, &fg.DisplayModel, &fg.DisplayName, &fg.Description, &priorityJSON,
			&entryEnabledJSON, &fg.GroupEnabled, &fg.AutoCreated, &fg.CreatedAt, &fg.UpdatedAt); err != nil {
			debuglog.Warn("failover: row scan failed", "error", err)
			return nil, fmt.Errorf("scanFailoverGroups: row scan failed: %w", err)
		}
		if err := jsonUnmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
			return nil, fmt.Errorf("scanFailoverGroups: unmarshal priority for %s: %w", fg.DisplayModel, err)
		}
		if err := jsonUnmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
			return nil, fmt.Errorf("scanFailoverGroups: unmarshal entry_enabled for %s: %w", fg.DisplayModel, err)
		}
		groups = append(groups, &fg)
	}
	return groups, nil
}

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

// SyncResult describes the outcome of a failover group sync operation.
type SyncResult struct {
	DeletedGroups []DeletedGroupInfo `json:"deleted_groups"`
	PurgedEntries []PrunedEntryInfo  `json:"purged_entries,omitempty"`
	SyncErrors    []string           `json:"sync_errors,omitempty"`
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

	syncedBases := make(map[string]bool)
	for base, models := range baseToModels {
		if len(models) <= 1 {
			if r.deleteAutoGroup(ctx, base) {
				providerNames := make([]string, 0, len(models))
				for _, m := range models {
					providerNames = append(providerNames, m.providerName)
				}
				reason := "no enabled providers found"
				if len(models) == 1 {
					reason = "only 1 enabled provider (need 2+ for failover)"
				}
				result.DeletedGroups = append(result.DeletedGroups, DeletedGroupInfo{
					DisplayModel:  base,
					ProviderCount: len(models),
					Reason:        reason,
					ProviderNames: providerNames,
				})
			}
			continue
		}

		currentIDs := make([]uuid.UUID, len(models))
		entryEnabled := make(map[string]bool)
		for i, m := range models {
			currentIDs[i] = m.uuid
			entryEnabled[m.uuid.String()] = true
		}

		existing, _ := r.GetByModel(ctx, base)
		if existing != nil {
			for uuidStr, enabled := range existing.EntryEnabled {
				if _, stillPresent := entryEnabled[uuidStr]; stillPresent {
					entryEnabled[uuidStr] = enabled
				}
			}
		}

		var priorityOrder []uuid.UUID
		if existing != nil {
			priorityOrder = mergePriorityOrder(existing.PriorityOrder, currentIDs)
		} else {
			priorityOrder = currentIDs
		}

		syncedBases[base] = true
		groupEnabled := true
		autoCreated := true
		var syncDisplayName, syncDescription *string
		if existing != nil {
			syncDisplayName = existing.DisplayName
			if existing.Description != "" {
				syncDescription = &existing.Description
			}
		}
		_, err := r.UpsertWithConfig(ctx, base, priorityOrder, entryEnabled, &groupEnabled, syncDisplayName, syncDescription, &autoCreated)
		if err != nil {
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
	r.pruneStaleEntries(ctx, allGroups, result)

	debuglog.Info("failover: synced groups", "synced", len(syncedBases), "deleted", len(result.DeletedGroups))

	return result, nil
}

// SyncForModel syncs the failover group for a specific model.
func (r *Repository) SyncForModel(ctx context.Context, modelID string) error {
	base := normalizeBaseModel(modelID)

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
		return err
	}
	defer rows.Close()

	var currentIDs []uuid.UUID
	for rows.Next() {
		var id, providerID uuid.UUID
		if err := rows.Scan(&id, &providerID); err != nil {
			continue
		}
		currentIDs = append(currentIDs, id)
	}

	if len(currentIDs) <= 1 {
		r.deleteAutoGroup(ctx, base)
		return nil
	}

	entryEnabled := make(map[string]bool)
	for _, id := range currentIDs {
		entryEnabled[id.String()] = true
	}

	existing, _ := r.GetByModel(ctx, base)
	if existing != nil {
		for uuidStr, enabled := range existing.EntryEnabled {
			if _, stillPresent := entryEnabled[uuidStr]; stillPresent {
				entryEnabled[uuidStr] = enabled
			}
		}
	}

	var priorityOrder []uuid.UUID
	if existing != nil {
		priorityOrder = mergePriorityOrder(existing.PriorityOrder, currentIDs)
	} else {
		priorityOrder = currentIDs
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
	if err != nil {
		debuglog.Error("failover: failed to sync group", "display_model", base, "error", err)
		return err
	}
	debuglog.Info("failover: synced group", "display_model", base, "providers", len(priorityOrder))
	return err
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
