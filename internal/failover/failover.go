package failover

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

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

	if err := json.Unmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

func (r *Repository) Upsert(ctx context.Context, displayModel string, priorityOrder []uuid.UUID) (*FailoverGroup, error) {
	return r.UpsertWithConfig(ctx, displayModel, priorityOrder, nil, nil, nil, nil, nil)
}

func (r *Repository) UpsertWithConfig(ctx context.Context, displayModel string, priorityOrder []uuid.UUID,
	entryEnabled map[string]bool, groupEnabled *bool, displayName, description *string, autoCreated *bool) (*FailoverGroup, error) {
	priorityJSON, err := json.Marshal(priorityOrder)
	if err != nil {
		return nil, err
	}

	entryEnabledJSON, err := json.Marshal(entryEnabled)
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
	doSetClauses = append(doSetClauses, "auto_created = $7")
	doSetClauses = append(doSetClauses, "updated_at = now()")

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

	if err := json.Unmarshal(rawPriority, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(rawEntryEnabled, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

func (r *Repository) Delete(ctx context.Context, displayModel string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE display_model = $1`, displayModel)
	InvalidateFailoverCache()
	return err
}

func (r *Repository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE id = $1`, id)
	InvalidateFailoverCache()
	return err
}

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

	if err := json.Unmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

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

func (r *Repository) Update(ctx context.Context, id uuid.UUID, priorityOrder []uuid.UUID,
	entryEnabled map[string]bool, groupEnabled *bool, displayName, description *string) (*FailoverGroup, error) {
	priorityJSON, err := json.Marshal(priorityOrder)
	if err != nil {
		return nil, err
	}

	entryEnabledJSON, err := json.Marshal(entryEnabled)
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

	if err := json.Unmarshal(rawPriority, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(rawEntryEnabled, &fg.EntryEnabled); err != nil {
		return nil, err
	}

	cacheFailoverGroup(&fg)
	return &fg, nil
}

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
			log.Printf("[failover] warning: row scan failed: %v", err)
			return nil, fmt.Errorf("scanFailoverGroups: row scan failed: %w", err)
		}
		if err := json.Unmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
			return nil, fmt.Errorf("scanFailoverGroups: unmarshal priority for %s: %w", fg.DisplayModel, err)
		}
		if err := json.Unmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
			return nil, fmt.Errorf("scanFailoverGroups: unmarshal entry_enabled for %s: %w", fg.DisplayModel, err)
		}
		groups = append(groups, &fg)
	}
	return groups, nil
}

type DisabledGroupInfo struct {
	DisplayModel  string   `json:"display_model"`
	Reason        string   `json:"reason"`
	ProviderCount int      `json:"provider_count"`
	ProviderNames []string `json:"provider_names"`
}

type SyncResult struct {
	DisabledGroups []DisabledGroupInfo `json:"disabled_groups"`
	SyncErrors     []string            `json:"sync_errors,omitempty"`
}


var commonPrefixes = []string{
	"zai-org/",
	"deepseek/",
	"meta-llama/",
	"mistralai/",
	"openai/",
	"anthropic/",
	"google/",
	"allenai/",
	"bigscience/",
	"facebook/",
	"microsoft/",
	"nvidia/",
	"stabilityai/",
	"tiiuae/",
	"databricks/",
	"EleutherAI/",
	"mosaicml/",
	"togethercomputer/",
}

func stripPrefix(modelID string) string {
	for _, prefix := range commonPrefixes {
		if strings.HasPrefix(modelID, prefix) {
			return strings.TrimPrefix(modelID, prefix)
		}
	}
	return modelID
}

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
		base := stripPrefix(modelID)
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
			if r.disableAutoGroup(ctx, base) {
				providerNames := make([]string, 0, len(models))
				for _, m := range models {
					providerNames = append(providerNames, m.providerName)
				}
				reason := "no enabled providers found"
				if len(models) == 1 {
					reason = "only 1 enabled provider (need 2+ for failover)"
				}
				result.DisabledGroups = append(result.DisabledGroups, DisabledGroupInfo{
					DisplayModel:  base,
					ProviderCount: len(models),
					Reason:        reason,
					ProviderNames: providerNames,
				})
			}
			continue
		}

		priorityOrder := make([]uuid.UUID, len(models))
		entryEnabled := make(map[string]bool)
		for i, m := range models {
			priorityOrder[i] = m.uuid
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
		if g.AutoCreated && g.GroupEnabled {
			if _, ok := syncedBases[g.DisplayModel]; !ok {
				if r.disableAutoGroup(ctx, g.DisplayModel) {
					result.DisabledGroups = append(result.DisabledGroups, DisabledGroupInfo{
						DisplayModel:  g.DisplayModel,
						ProviderCount: 0,
						Reason:        "no enabled providers found",
						ProviderNames: []string{},
					})
				}
			}
		}
	}

	log.Printf("[failover] synced %d groups, disabled %d groups", len(syncedBases), len(result.DisabledGroups))

	return result, nil
}

func (r *Repository) SyncForModel(ctx context.Context, modelID string) error {
	base := stripPrefix(modelID)

	args := []interface{}{base}
	conditions := []string{"m.model_id = $1"}
	for i, prefix := range commonPrefixes {
		args = append(args, prefix+base)
		conditions = append(conditions, fmt.Sprintf("m.model_id = $%d", i+2))
	}

	query := fmt.Sprintf(`
		SELECT m.id, m.provider_id
		FROM models m
		JOIN providers p ON m.provider_id = p.id
		WHERE m.enabled = true AND p.enabled = true AND (%s)
		ORDER BY p.created_at ASC
	`, strings.Join(conditions, " OR "))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	var modelUUIDs []uuid.UUID
	for rows.Next() {
		var id, providerID uuid.UUID
		if err := rows.Scan(&id, &providerID); err != nil {
			continue
		}
		modelUUIDs = append(modelUUIDs, id)
	}

	if len(modelUUIDs) <= 1 {
		r.disableAutoGroup(ctx, base)
		return nil
	}

	entryEnabled := make(map[string]bool)
	for _, id := range modelUUIDs {
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

	groupEnabled := true
	autoCreated := true
	var syncDisplayName, syncDescription *string
	if existing != nil {
		syncDisplayName = existing.DisplayName
		if existing.Description != "" {
			syncDescription = &existing.Description
		}
	}
	_, err = r.UpsertWithConfig(ctx, base, modelUUIDs, entryEnabled, &groupEnabled, syncDisplayName, syncDescription, &autoCreated)
	if err != nil {
		log.Printf("[failover] error: failed to sync group for %q: %v", base, err)
		return err
	}
	log.Printf("[failover] synced group for %q with %d providers", base, len(modelUUIDs))
	return err
}

func (r *Repository) disableAutoGroup(ctx context.Context, displayModel string) bool {
	tag, err := r.pool.Exec(ctx, `
		UPDATE model_failover_groups
		SET group_enabled = false, updated_at = now()
		WHERE display_model = $1 AND auto_created = true AND group_enabled = true
	`, displayModel)
	if err == nil && tag.RowsAffected() > 0 {
		InvalidateFailoverCache()
		log.Printf("[failover] disabled auto-group for %q", displayModel)
		return true
	}
	return false
}
