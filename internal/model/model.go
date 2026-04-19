package model

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/google/uuid"
)

type Model struct {
	ID           uuid.UUID  `json:"id"`
	ProviderID   uuid.UUID  `json:"provider_id"`
	ModelID      string     `json:"model_id"`
	DisplayName string     `json:"display_name"`
	Capabilities string     `json:"capabilities"`
	Params       string     `json:"params"`
	Enabled      bool       `json:"enabled"`
	CreatedAt    time.Time  `json:"created_at"`
}

type Capability struct {
	Vision   bool `json:"vision"`
	Streaming bool `json:"streaming"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Upsert(ctx context.Context, m *Model) error {
	query := `
		INSERT INTO models (id, provider_id, model_id, display_name, capabilities, params, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (provider_id, model_id)
		DO UPDATE SET
			display_name = EXCLUDED.display_name,
			capabilities = EXCLUDED.capabilities,
			params = EXCLUDED.params,
			enabled = true
		RETURNING id, provider_id, model_id, display_name, capabilities, params, enabled, created_at
	`

	err := r.pool.QueryRow(ctx, query,
		m.ID, m.ProviderID, m.ModelID, m.DisplayName, m.Capabilities, m.Params, m.Enabled,
	).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Enabled, &m.CreatedAt,
	)

	return err
}

func (r *Repository) List(ctx context.Context, providerID *uuid.UUID) ([]*Model, error) {
	query := `
		SELECT id, provider_id, model_id, display_name, capabilities, params, enabled, created_at
		FROM models
	`

	if providerID != nil {
		query += " WHERE provider_id = $1"
	}

	query += " ORDER BY created_at DESC"

	var rows pgx.Rows
	var err error

	if providerID != nil {
		rows, err = r.pool.Query(ctx, query, providerID)
	} else {
		rows, err = r.pool.Query(ctx, query)
	}

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var models []*Model
	for rows.Next() {
		var m Model
		err := rows.Scan(
			&m.ID, &m.ProviderID, &m.ModelID, &m.DisplayName, &m.Capabilities,
			&m.Params, &m.Enabled, &m.CreatedAt,
		)
		if err != nil {
			return nil, err
		}
		models = append(models, &m)
	}

	return models, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Model, error) {
	query := `
		SELECT id, provider_id, model_id, display_name, capabilities, params, enabled, created_at
		FROM models
		WHERE id = $1
	`

	var m Model
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Enabled, &m.CreatedAt,
	)

	if err != nil {
		return nil, err
	}

	return &m, nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM models WHERE id = $1`
	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	return nil
}

func (r *Repository) DisableMissingModels(ctx context.Context, providerID uuid.UUID, existingModelIDs []string) error {
	query := `
		UPDATE models
		SET enabled = false
		WHERE provider_id = $1 AND model_id != ALL($2)
	`

	_, err := r.pool.Exec(ctx, query, providerID, existingModelIDs)
	return err
}
