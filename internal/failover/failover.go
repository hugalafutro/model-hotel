package failover

import (
	"context"
	"encoding/json"
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

type RoutingEntry struct {
	ModelID      string      `json:"model_id"`
	Providers    []RoutingProvider `json:"providers"`
}

type RoutingProvider struct {
	ModelUUID    uuid.UUID `json:"model_uuid"`
	ProviderID   uuid.UUID `json:"provider_id"`
	ProviderName string    `json:"provider_name"`
	ModelID      string    `json:"model_id"`
	Priority     int       `json:"priority"`
	Enabled      bool      `json:"enabled"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) GetByModel(ctx context.Context, modelID string) (*FailoverGroup, error) {
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

	var fg FailoverGroup
	var rawPriority, rawEntryEnabled []byte

	query := `
		INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, display_name, description, auto_created)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (display_model)
		DO UPDATE SET priority_order = $2, entry_enabled = $3, group_enabled = $4, display_name = $5, description = $6, updated_at = now()
		RETURNING id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order, 
		          COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		          created_at, COALESCE(updated_at, created_at)`

	var groupEnabledVal bool = true
	if groupEnabled != nil {
		groupEnabledVal = *groupEnabled
	}

	var autoCreatedVal bool = false
	if autoCreated != nil {
		autoCreatedVal = *autoCreated
	}

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

	return &fg, nil
}

func (r *Repository) Delete(ctx context.Context, displayModel string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE display_model = $1`, displayModel)
	return err
}

func (r *Repository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE id = $1`, id)
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

	var fg FailoverGroup
	var rawPriority, rawEntryEnabled []byte

	query := `
		UPDATE model_failover_groups 
		SET priority_order = $2, entry_enabled = $3, group_enabled = $4, display_name = $5, description = $6, updated_at = now()
		WHERE id = $1
		RETURNING id, display_model, COALESCE(display_name, ''), COALESCE(description, ''), priority_order, 
		          COALESCE(entry_enabled, '{}'), COALESCE(group_enabled, true), COALESCE(auto_created, false),
		          created_at, COALESCE(updated_at, created_at)`

	var groupEnabledVal bool = true
	if groupEnabled != nil {
		groupEnabledVal = *groupEnabled
	}

	err = r.pool.QueryRow(ctx, query, id, priorityJSON, entryEnabledJSON, groupEnabledVal, displayName, description).
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
			continue
		}
		if err := json.Unmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
			continue
		}
		if err := json.Unmarshal(entryEnabledJSON, &fg.EntryEnabled); err != nil {
			continue
		}
		groups = append(groups, &fg)
	}
	return groups, nil
}

func (r *Repository) SyncAllModels(ctx context.Context) error {
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT m.model_id
		FROM models m
		JOIN providers p ON m.provider_id = p.id
		WHERE m.enabled = true AND p.enabled = true
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var modelIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		modelIDs = append(modelIDs, id)
	}

	for _, modelID := range modelIDs {
		if err := r.SyncForModel(ctx, modelID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) SyncForModel(ctx context.Context, modelID string) error {
	rows, err := r.pool.Query(ctx, `
		SELECT m.id, m.provider_id
		FROM models m
		JOIN providers p ON m.provider_id = p.id
		WHERE m.model_id = $1 AND m.enabled = true AND p.enabled = true
		ORDER BY p.created_at ASC
	`, modelID)
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
		r.Delete(ctx, modelID)
		return nil
	}

	// Build entry_enabled map - all entries enabled by default
	entryEnabled := make(map[string]bool)
	for _, uuid := range modelUUIDs {
		entryEnabled[uuid.String()] = true
	}

	autoCreated := true
	_, err = r.UpsertWithConfig(ctx, modelID, modelUUIDs, entryEnabled, nil, nil, nil, &autoCreated)
	return err
}