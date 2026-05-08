package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Model struct {
	ID                           uuid.UUID `json:"id"`
	ProviderID                   uuid.UUID `json:"provider_id"`
	ModelID                      string    `json:"model_id"`
	Name                         string    `json:"name"`
	Description                  string    `json:"description"`
	DisplayName                  string    `json:"display_name"`
	Capabilities                 string    `json:"capabilities"`
	Params                       string    `json:"params"`
	Modality                     string    `json:"modality"`
	InputModalities              string    `json:"input_modalities"`
	OutputModalities             string    `json:"output_modalities"`
	ContextLength                *int      `json:"context_length"`
	MaxOutputTokens              *int      `json:"max_output_tokens"`
	InputPricePerMillion         *float64  `json:"input_price_per_million"`
	InputPricePerMillionCacheHit *float64  `json:"input_price_per_million_cache_hit"`
	OutputPricePerMillion        *float64  `json:"output_price_per_million"`
	OwnedBy                      string    `json:"owned_by"`
	Enabled                      bool      `json:"enabled"`
	DisabledManually             bool      `json:"disabled_manually"`
	CreatedAt                    time.Time `json:"created_at"`
	LastSeenAt                   time.Time `json:"last_seen_at"`
	ProviderName                 string    `json:"provider_name"`
	ProviderEnabled              bool      `json:"provider_enabled"`
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

const modelColumns = `m.id, m.provider_id, m.model_id, COALESCE(m.name, ''), COALESCE(m.description, ''), COALESCE(m.display_name, ''), COALESCE(m.capabilities, '{}'), COALESCE(m.params, '{}'), COALESCE(m.modality, ''), COALESCE(m.input_modalities, '[]'), COALESCE(m.output_modalities, '[]'), m.context_length, m.max_output_tokens, m.input_price_per_million, m.input_price_per_million_cache_hit, m.output_price_per_million, COALESCE(m.owned_by, ''), m.enabled, m.disabled_manually, m.created_at, COALESCE(m.last_seen_at, m.created_at), p.name, p.enabled`

const upsertColumns = `id, provider_id, model_id, COALESCE(name, ''), COALESCE(description, ''), COALESCE(display_name, ''), COALESCE(capabilities, '{}'), COALESCE(params, '{}'), COALESCE(modality, ''), COALESCE(input_modalities, '[]'), COALESCE(output_modalities, '[]'), context_length, max_output_tokens, input_price_per_million, input_price_per_million_cache_hit, output_price_per_million, COALESCE(owned_by, ''), enabled, disabled_manually, created_at, COALESCE(last_seen_at, created_at)`

func (r *Repository) Upsert(ctx context.Context, m *Model) error {
	query := `
		INSERT INTO models (id, provider_id, model_id, name, description, display_name, capabilities, params, modality, input_modalities, output_modalities, context_length, max_output_tokens, input_price_per_million, input_price_per_million_cache_hit, output_price_per_million, owned_by, enabled, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, now())
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
			input_price_per_million_cache_hit = EXCLUDED.input_price_per_million_cache_hit,
			output_price_per_million = EXCLUDED.output_price_per_million,
			owned_by = EXCLUDED.owned_by,
			enabled = CASE WHEN models.disabled_manually = false THEN true ELSE models.enabled END,
			last_seen_at = now()
		RETURNING ` + upsertColumns

	err := r.pool.QueryRow(ctx, query,
		m.ID, m.ProviderID, m.ModelID, m.Name, m.Description, m.DisplayName, m.Capabilities, m.Params,
		m.Modality, m.InputModalities, m.OutputModalities,
		m.ContextLength, m.MaxOutputTokens, m.InputPricePerMillion, m.InputPricePerMillionCacheHit, m.OutputPricePerMillion, m.OwnedBy, m.Enabled,
	).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.CreatedAt, &m.LastSeenAt,
	)

	InvalidateModelCache()
	return err
}

func scanModels(rows pgx.Rows) ([]*Model, error) {
	var models []*Model
	for rows.Next() {
		var m Model
		if err := rows.Scan(
			&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
			&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
			&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
			&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
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
	if m, ok := GetCachedByUUID(id); ok {
		return m, nil
	}

	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.id = $1`

	var m Model
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
	)

	if err != nil {
		return nil, err
	}

	cacheModelByUUID(&m)
	return &m, nil
}

func (r *Repository) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*Model, error) {
	if len(ids) == 0 {
		return make(map[uuid.UUID]*Model), nil
	}

	// Collect IDs that need to be fetched from DB (not in cache)
	var uncachedIDs []uuid.UUID
	result := make(map[uuid.UUID]*Model, len(ids))
	for _, id := range ids {
		if m, ok := GetCachedByUUID(id); ok {
			result[id] = m
		} else {
			uncachedIDs = append(uncachedIDs, id)
		}
	}

	if len(uncachedIDs) == 0 {
		return result, nil
	}

	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.id = ANY($1)`

	rows, err := r.pool.Query(ctx, query, uncachedIDs)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	models, err := scanModels(rows)
	if err != nil {
		return result, err
	}

	WarmModelCache(models)

	for _, m := range models {
		result[m.ID] = m
	}

	return result, nil
}

func (r *Repository) GetByModelID(ctx context.Context, modelID string) ([]*Model, error) {
	if models, ok := GetCachedByModelID(modelID); ok {
		return models, nil
	}

	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.model_id = $1 AND m.enabled = true AND p.enabled = true ORDER BY p.created_at ASC`

	rows, err := r.pool.Query(ctx, query, modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	models, err := scanModels(rows)
	if err != nil {
		return nil, err
	}

	cacheModelsByModelID(modelID, models)
	return models, nil
}

func (r *Repository) GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*Model, error) {
	if m, ok := GetCachedByCompositeKey(providerID, modelID); ok {
		return m, nil
	}

	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.provider_id = $1 AND m.model_id = $2`

	var m Model
	err := r.pool.QueryRow(ctx, query, providerID, modelID).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
	)

	if err != nil {
		return nil, err
	}

	cacheModelByCompositeKey(providerID, modelID, &m)
	cacheModelByUUID(&m)
	return &m, nil
}

func (r *Repository) DisableMissingModels(ctx context.Context, providerID uuid.UUID, existingModelIDs []string) (int64, error) {
	if len(existingModelIDs) == 0 {
		return 0, nil
	}
	query := `
		UPDATE models
		SET enabled = false
		WHERE provider_id = $1 AND model_id != ALL($2)
	`

	tag, err := r.pool.Exec(ctx, query, providerID, existingModelIDs)
	if err != nil {
		return 0, err
	}
	InvalidateModelCache()
	return tag.RowsAffected(), nil
}

func (r *Repository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*Model, error) {
	query := `UPDATE models SET enabled = $1, disabled_manually = NOT $1 WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, enabled, id)
	if err != nil {
		return nil, err
	}
	InvalidateModelCache()
	return r.Get(ctx, id)
}

func (r *Repository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM models WHERE id = $1`, id)
	if err != nil {
		return err
	}
	InvalidateModelCache()
	return nil
}

type UpdateModelRequest struct {
	DisplayName           *string  `json:"display_name"`
	ContextLength         *int     `json:"context_length"`
	MaxOutputTokens       *int     `json:"max_output_tokens"`
	InputPricePerMillion  *float64 `json:"input_price_per_million"`
	OutputPricePerMillion *float64 `json:"output_price_per_million"`
	Enabled               *bool    `json:"enabled"`
}

func (r *Repository) Update(ctx context.Context, id uuid.UUID, req UpdateModelRequest) (*Model, error) {
	var setClauses []string
	var args []interface{}
	argIdx := 2 // $1 is reserved for id

	if req.DisplayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
		args = append(args, *req.DisplayName)
		argIdx++
	}
	if req.ContextLength != nil {
		setClauses = append(setClauses, fmt.Sprintf("context_length = $%d", argIdx))
		args = append(args, *req.ContextLength)
		argIdx++
	}
	if req.MaxOutputTokens != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_output_tokens = $%d", argIdx))
		args = append(args, *req.MaxOutputTokens)
		argIdx++
	}
	if req.InputPricePerMillion != nil {
		setClauses = append(setClauses, fmt.Sprintf("input_price_per_million = $%d", argIdx))
		args = append(args, *req.InputPricePerMillion)
		argIdx++
	}
	if req.OutputPricePerMillion != nil {
		setClauses = append(setClauses, fmt.Sprintf("output_price_per_million = $%d", argIdx))
		args = append(args, *req.OutputPricePerMillion)
		argIdx++
	}
	if req.Enabled != nil {
		setClauses = append(setClauses, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *req.Enabled)
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("disabled_manually = $%d", argIdx))
		args = append(args, !*req.Enabled)
	}

	if len(setClauses) == 0 {
		return r.Get(ctx, id)
	}

	args = append([]interface{}{id}, args...)

	query := fmt.Sprintf("UPDATE models SET %s WHERE id = $1", strings.Join(setClauses, ", "))

	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	InvalidateModelCache()
	return r.Get(ctx, id)
}
