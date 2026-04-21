package failover

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type FailoverGroup struct {
	ID            uuid.UUID `json:"id"`
	DisplayModel  string    `json:"display_model"`
	PriorityOrder []uuid.UUID `json:"priority_order"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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

	err := r.pool.QueryRow(ctx, `
		SELECT id, display_model, priority_order, created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		WHERE display_model = $1
	`, modelID).Scan(&fg.ID, &fg.DisplayModel, &priorityJSON, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	return &fg, nil
}

func (r *Repository) Upsert(ctx context.Context, displayModel string, priorityOrder []uuid.UUID) (*FailoverGroup, error) {
	priorityJSON, err := json.Marshal(priorityOrder)
	if err != nil {
		return nil, err
	}

	var fg FailoverGroup
	var rawPriority []byte

	err = r.pool.QueryRow(ctx, `
		INSERT INTO model_failover_groups (display_model, priority_order)
		VALUES ($1, $2)
		ON CONFLICT (display_model)
		DO UPDATE SET priority_order = $2, updated_at = now()
		RETURNING id, display_model, priority_order, created_at, COALESCE(updated_at, created_at)
	`, displayModel, priorityJSON).Scan(&fg.ID, &fg.DisplayModel, &rawPriority, &fg.CreatedAt, &fg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(rawPriority, &fg.PriorityOrder); err != nil {
		return nil, err
	}

	return &fg, nil
}

func (r *Repository) Delete(ctx context.Context, displayModel string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE display_model = $1`, displayModel)
	return err
}

func (r *Repository) List(ctx context.Context) ([]*FailoverGroup, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, display_model, priority_order, created_at, COALESCE(updated_at, created_at)
		FROM model_failover_groups
		ORDER BY display_model
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var groups []*FailoverGroup
	for rows.Next() {
		var fg FailoverGroup
		var priorityJSON []byte
		if err := rows.Scan(&fg.ID, &fg.DisplayModel, &priorityJSON, &fg.CreatedAt, &fg.UpdatedAt); err != nil {
			continue
		}
		if err := json.Unmarshal(priorityJSON, &fg.PriorityOrder); err != nil {
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

	_, err = r.Upsert(ctx, modelID, modelUUIDs)
	return err
}