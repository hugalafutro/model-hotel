package model

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Model struct {
	ID                    uuid.UUID `json:"id"`
	ProviderID            uuid.UUID `json:"provider_id"`
	ModelID               string    `json:"model_id"`
	Name                  string    `json:"name"`
	Description           string    `json:"description"`
	DisplayName           string    `json:"display_name"`
	Capabilities          string    `json:"capabilities"`
	Params                string    `json:"params"`
	Modality              string    `json:"modality"`
	InputModalities       string    `json:"input_modalities"`
	OutputModalities      string    `json:"output_modalities"`
	ContextLength         *int      `json:"context_length"`
	MaxOutputTokens       *int      `json:"max_output_tokens"`
	InputPricePerMillion  *float64  `json:"input_price_per_million"`
	OutputPricePerMillion *float64  `json:"output_price_per_million"`
	OwnedBy               string    `json:"owned_by"`
	Enabled               bool      `json:"enabled"`
	CreatedAt             time.Time `json:"created_at"`
	LastSeenAt            time.Time `json:"last_seen_at"`
	ProviderName          string    `json:"provider_name"`
	ProviderEnabled       bool      `json:"provider_enabled"`
}

type Capability struct {
	Streaming         bool `json:"streaming"`
	Vision            bool `json:"vision"`
	VideoInput        bool `json:"video_input"`
	AudioInput        bool `json:"audio_input"`
	Reasoning         bool `json:"reasoning"`
	ToolCalling       bool `json:"tool_calling"`
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	StructuredOutput  bool `json:"structured_output"`
	PDFUpload         bool `json:"pdf_upload"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const modelColumns = `m.id, m.provider_id, m.model_id, COALESCE(m.name, ''), COALESCE(m.description, ''), COALESCE(m.display_name, ''), COALESCE(m.capabilities, '{}'), COALESCE(m.params, '{}'), COALESCE(m.modality, ''), COALESCE(m.input_modalities, '[]'), COALESCE(m.output_modalities, '[]'), m.context_length, m.max_output_tokens, m.input_price_per_million, m.output_price_per_million, COALESCE(m.owned_by, ''), m.enabled, m.created_at, COALESCE(m.last_seen_at, m.created_at), p.name, p.enabled`

const upsertColumns = `id, provider_id, model_id, COALESCE(name, ''), COALESCE(description, ''), COALESCE(display_name, ''), COALESCE(capabilities, '{}'), COALESCE(params, '{}'), COALESCE(modality, ''), COALESCE(input_modalities, '[]'), COALESCE(output_modalities, '[]'), context_length, max_output_tokens, input_price_per_million, output_price_per_million, COALESCE(owned_by, ''), enabled, created_at, COALESCE(last_seen_at, created_at)`

func (r *Repository) Upsert(ctx context.Context, m *Model) error {
	query := `
		INSERT INTO models (id, provider_id, model_id, name, description, display_name, capabilities, params, modality, input_modalities, output_modalities, context_length, max_output_tokens, input_price_per_million, output_price_per_million, owned_by, enabled, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, now())
		ON CONFLICT (provider_id, model_id)
		DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			display_name = EXCLUDED.display_name,
			capabilities = EXCLUDED.capabilities,
			params = EXCLUDED.params,
			modality = EXCLUDED.modality,
			input_modalities = EXCLUDED.input_modalities,
			output_modalities = EXCLUDED.output_modalities,
			context_length = EXCLUDED.context_length,
			max_output_tokens = EXCLUDED.max_output_tokens,
			input_price_per_million = EXCLUDED.input_price_per_million,
			output_price_per_million = EXCLUDED.output_price_per_million,
			owned_by = EXCLUDED.owned_by,
			enabled = EXCLUDED.enabled,
			last_seen_at = now()
		RETURNING ` + upsertColumns

	err := r.pool.QueryRow(ctx, query,
		m.ID, m.ProviderID, m.ModelID, m.Name, m.Description, m.DisplayName, m.Capabilities, m.Params,
		m.Modality, m.InputModalities, m.OutputModalities,
		m.ContextLength, m.MaxOutputTokens, m.InputPricePerMillion, m.OutputPricePerMillion, m.OwnedBy, m.Enabled,
	).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.CreatedAt, &m.LastSeenAt,
	)

	return err
}

func scanModels(rows pgx.Rows) ([]*Model, error) {
	var models []*Model
	for rows.Next() {
		var m Model
		if err := rows.Scan(
			&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
			&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
			&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.OutputPricePerMillion,
			&m.OwnedBy, &m.Enabled, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
		); err != nil {
			return nil, err
		}
		models = append(models, &m)
	}
	return models, nil
}

func (r *Repository) List(ctx context.Context, providerID *uuid.UUID) ([]*Model, error) {
	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id`

	if providerID != nil {
		query += " WHERE m.provider_id = $1"
	}

	query += " ORDER BY m.model_id ASC"

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

	return scanModels(rows)
}

func (r *Repository) ListEnabled(ctx context.Context) ([]*Model, error) {
	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.enabled = true AND p.enabled = true ORDER BY m.model_id ASC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanModels(rows)
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Model, error) {
	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.id = $1`

	var m Model
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
	)

	if err != nil {
		return nil, err
	}

	return &m, nil
}

func (r *Repository) GetByModelID(ctx context.Context, modelID string) ([]*Model, error) {
	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.model_id = $1 AND m.enabled = true AND p.enabled = true ORDER BY p.created_at ASC`

	rows, err := r.pool.Query(ctx, query, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanModels(rows)
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

func (r *Repository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*Model, error) {
	query := `UPDATE models SET enabled = $1 WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, enabled, id)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}