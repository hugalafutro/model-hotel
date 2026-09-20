package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Model represents a discovered or configured LLM model.
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
	// SearchPricePerThousand is what a thousand search units cost, the unit a
	// rerank model bills in (one query against up to 100 documents); nil for
	// every token-billed model.
	SearchPricePerThousand *float64 `json:"search_price_per_thousand"`
	OwnedBy                string   `json:"owned_by"`
	Enabled                bool     `json:"enabled"`
	DisabledManually       bool     `json:"disabled_manually"`
	DisplayNameCustomized  bool     `json:"display_name_customized"`
	PriceCustomized        bool     `json:"price_customized"`
	// LimitsCustomized pins context_length and max_output_tokens against the
	// scan the way PriceCustomized pins the prices: set by any edit of either,
	// cleared by an explicit limits_customized=false (migration 092).
	LimitsCustomized bool `json:"limits_customized"`
	// PriceSources records where each stored price came from; see
	// PriceSources for the vocabulary.
	PriceSources    PriceSources `json:"price_sources"`
	CreatedAt       time.Time    `json:"created_at"`
	LastSeenAt      time.Time    `json:"last_seen_at"`
	ProviderName    string       `json:"provider_name"`
	ProviderEnabled bool         `json:"provider_enabled"`

	// LiveMeta marks which context-limit fields on THIS in-memory model were
	// populated directly from the provider's live API during the current scan
	// (as opposed to the hardcoded catalog or models.dev enrichment). It is
	// transient: never read from or written to the database, and excluded from
	// JSON (clients never see it). Upsert uses it to merge per field — live
	// fields overwrite the stored value, so a genuine provider context change
	// propagates, while a non-live value is fill-only and stays stable so a
	// flaky probe or a models.dev re-fetch can't flip a stored value. The zero
	// value (all false) is the safe default for stub-, catalog- and
	// models.dev-sourced models. Price columns don't ride here: they follow the
	// incoming value unless the operator pinned them (price_customized), judged
	// inside the upsert query.
	LiveMeta LiveMetaFields `json:"-"`

	// PreserveCapabilities marks an in-memory model whose capabilities are a
	// placeholder rather than a reading (a discoverer that lists the model but
	// could not fetch its details this scan). Upsert then keeps the stored
	// capabilities instead of overwriting them with the placeholder. Transient
	// like LiveMeta: never stored, never serialized.
	PreserveCapabilities bool `json:"-"`
}

// LiveMetaFields records, per context-limit field, whether the value came
// from the provider's own live API this scan. See Model.LiveMeta.
type LiveMetaFields struct {
	ContextLength   bool
	MaxOutputTokens bool
}

// MarkLiveMetaFromCurrent flags every context-limit field that is currently
// set (non-nil) as live-sourced. Discoverers call this on a model right after
// populating it from the provider's live payload and before any catalog or
// models.dev fill runs, so only provider-reported fields are flagged.
func (m *Model) MarkLiveMetaFromCurrent() {
	m.LiveMeta.ContextLength = m.ContextLength != nil
	m.LiveMeta.MaxOutputTokens = m.MaxOutputTokens != nil
}

// Capability represents the feature capabilities of a model.
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

// Repository provides database operations for models.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new model repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Where a model's price came from. Every price a row holds carries one, set
// by whatever wrote the price: discovery for a provider's own listing, the
// embedded catalog converters, models.dev enrichment, or an operator edit.
const (
	PriceSourceProvider  = "provider"
	PriceSourceCatalog   = "catalog"
	PriceSourceModelsDev = "modelsdev"
	PriceSourceManual    = "manual"
)

// PriceSources names the source of each price field, keyed the way the
// price_sources JSONB column stores them. A field is empty when that price is
// unset, or when the row was last written before sources were recorded.
type PriceSources struct {
	Input    string `json:"input,omitempty"`
	CacheHit string `json:"cache_hit,omitempty"`
	Output   string `json:"output,omitempty"`
	Search   string `json:"search,omitempty"`
}

// StampPriceSources records source for every price the model holds that has
// no source yet. Callers that set prices call it right after, so a price
// filled later by another source keeps that source's name.
func (m *Model) StampPriceSources(source string) {
	if m.InputPricePerMillion != nil && m.PriceSources.Input == "" {
		m.PriceSources.Input = source
	}
	if m.InputPricePerMillionCacheHit != nil && m.PriceSources.CacheHit == "" {
		m.PriceSources.CacheHit = source
	}
	if m.OutputPricePerMillion != nil && m.PriceSources.Output == "" {
		m.PriceSources.Output = source
	}
	if m.SearchPricePerThousand != nil && m.PriceSources.Search == "" {
		m.PriceSources.Search = source
	}
}

const modelColumns = `m.id, m.provider_id, m.model_id, COALESCE(m.name, ''), COALESCE(m.description, ''), COALESCE(m.display_name, ''), COALESCE(m.capabilities, '{}'), COALESCE(m.params, '{}'), COALESCE(m.modality, ''), COALESCE(m.input_modalities, '[]'), COALESCE(m.output_modalities, '[]'), m.context_length, m.max_output_tokens, m.input_price_per_million, m.input_price_per_million_cache_hit, m.output_price_per_million, m.search_price_per_thousand, COALESCE(m.owned_by, ''), m.enabled, m.disabled_manually, m.display_name_customized, m.price_customized, m.limits_customized, COALESCE(m.price_sources, '{}'::jsonb), m.created_at, COALESCE(m.last_seen_at, m.created_at), p.name, COALESCE(p.enabled, false)`

const upsertColumns = `id, provider_id, model_id, COALESCE(name, ''), COALESCE(description, ''), COALESCE(display_name, ''), COALESCE(capabilities, '{}'), COALESCE(params, '{}'), COALESCE(modality, ''), COALESCE(input_modalities, '[]'), COALESCE(output_modalities, '[]'), context_length, max_output_tokens, input_price_per_million, input_price_per_million_cache_hit, output_price_per_million, search_price_per_thousand, COALESCE(owned_by, ''), enabled, disabled_manually, display_name_customized, price_customized, limits_customized, COALESCE(price_sources, '{}'::jsonb), created_at, COALESCE(last_seen_at, created_at)`

// Upsert inserts or updates a model based on provider_id and model_id.
func (r *Repository) Upsert(ctx context.Context, m *Model) error {
	query := `
		INSERT INTO models (id, provider_id, model_id, name, description, display_name, capabilities, params, modality, input_modalities, output_modalities, context_length, max_output_tokens, input_price_per_million, input_price_per_million_cache_hit, output_price_per_million, search_price_per_thousand, owned_by, enabled, last_seen_at, price_sources)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $22, $17, $18, now(), $21)
		ON CONFLICT (provider_id, model_id)
		DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			display_name = CASE WHEN models.display_name_customized THEN models.display_name ELSE EXCLUDED.display_name END,
			capabilities = CASE WHEN $23 THEN COALESCE(models.capabilities, EXCLUDED.capabilities) ELSE EXCLUDED.capabilities END,
			params = EXCLUDED.params,
			modality = EXCLUDED.modality,
			input_modalities = EXCLUDED.input_modalities,
			output_modalities = EXCLUDED.output_modalities,
			-- Context/output-limit fields come from variable sources (the
			-- provider's live API, our hardcoded catalog, models.dev) that disagree
			-- and can flip across restarts. To keep stored metadata stable AND
			-- still honour a genuine provider change, each is merged by source:
			-- when the value came from the provider's live API this scan (the
			-- $live_* flag, derived from m.LiveMeta) it WINS and overwrites;
			-- otherwise it is fill-only — the stored value is kept and the
			-- incoming value only fills a gap.
			-- An operator's edit (limits_customized) outranks both: it survives
			-- the scan until an explicit unpin, which nulls the columns so the
			-- next scan refills them from source.
			context_length = CASE WHEN models.limits_customized THEN COALESCE(models.context_length, EXCLUDED.context_length) WHEN $19 THEN COALESCE(EXCLUDED.context_length, models.context_length) ELSE COALESCE(models.context_length, EXCLUDED.context_length) END,
			max_output_tokens = CASE WHEN models.limits_customized THEN COALESCE(models.max_output_tokens, EXCLUDED.max_output_tokens) WHEN $20 THEN COALESCE(EXCLUDED.max_output_tokens, models.max_output_tokens) ELSE COALESCE(models.max_output_tokens, EXCLUDED.max_output_tokens) END,
			-- Prices FOLLOW their source instead: unless the operator pinned them
			-- (price_customized, set by any price edit), the scan's value — live
			-- API, embedded catalog, or models.dev enrichment, already merged in
			-- that precedence before this upsert — overwrites the stored one, and
			-- only a scan with no price at all keeps the stored value. This is
			-- what lets a vendor price change (or corrected enrichment data, e.g.
			-- the canonical-provider fix for the random-reseller-price bug)
			-- propagate to existing rows; the old fill-only behavior froze
			-- whatever value landed first, forever. A pinned row's STORED values
			-- are the operator's — no source, live included, replaces them until
			-- they unpin — but a NULL price on a pinned row still fills from the
			-- scan (the pin protects values, it does not veto gap-fill).
			input_price_per_million = CASE WHEN models.price_customized THEN COALESCE(models.input_price_per_million, EXCLUDED.input_price_per_million) ELSE COALESCE(EXCLUDED.input_price_per_million, models.input_price_per_million) END,
			input_price_per_million_cache_hit = CASE WHEN models.price_customized THEN COALESCE(models.input_price_per_million_cache_hit, EXCLUDED.input_price_per_million_cache_hit) ELSE COALESCE(EXCLUDED.input_price_per_million_cache_hit, models.input_price_per_million_cache_hit) END,
			output_price_per_million = CASE WHEN models.price_customized THEN COALESCE(models.output_price_per_million, EXCLUDED.output_price_per_million) ELSE COALESCE(EXCLUDED.output_price_per_million, models.output_price_per_million) END,
			search_price_per_thousand = CASE WHEN models.price_customized THEN COALESCE(models.search_price_per_thousand, EXCLUDED.search_price_per_thousand) ELSE COALESCE(EXCLUDED.search_price_per_thousand, models.search_price_per_thousand) END,
			-- The sources merge key by key in the same direction as the prices:
			-- a key is present exactly when its price is, so whichever side's
			-- price wins above, its source wins here.
			price_sources = CASE WHEN models.price_customized THEN EXCLUDED.price_sources || models.price_sources ELSE models.price_sources || EXCLUDED.price_sources END,
			owned_by = EXCLUDED.owned_by,
			-- A sighting re-enables a model that discovery disabled for going
			-- missing, because reappearing in the listing is genuine new
			-- evidence there. It must NOT re-enable one the proxy retired from
			-- traffic: that model never left the listing, the provider was
			-- refusing it while still advertising it, so a sighting says
			-- nothing new. Reviving it would put it back into routing to fail,
			-- re-alert and churn failover groups on every scan. Only an
			-- operator clears auto_retired_at (migration 063).
			enabled = CASE
				WHEN models.disabled_manually = false AND models.auto_retired_at IS NULL THEN true
				ELSE models.enabled
			END,
			-- Any sighting resets the consecutive-miss streak used by
			-- RecordMissingModels, so a model that reappears (even via a manual
			-- re-test between scheduled scans) starts over from zero misses.
			missing_scans = 0,
			-- A sighting also disarms the operator's manual-enable pin: the pin
			-- exists only to overrule a listing that omits a model the operator
			-- verified working, so the listing naming it again ends the disagreement
			-- and hands the model back to automatic management.
			manually_enabled_at = NULL,
			-- A sighting also retires any operator dismissal, so a model that is
			-- dismissed, comes back, and vanishes again counts as a new claim
			-- instead of staying suppressed by a stale stamp.
			--
			-- Except for a model the proxy retired from traffic. That one never
			-- left the listing, so it is sighted on every single scan, and
			-- clearing the stamp here would make dismissing it impossible: the
			-- operator would silence the claim and the next scan would bring it
			-- straight back, with no way to stop it. Once an operator enables the
			-- model the retirement stamp goes (SetEnabled/Update null it), and
			-- the very next sighting takes this branch again and clears the
			-- dismissal, so nothing stays suppressed by a stale stamp.
			discovery_dismissed_at = CASE
				WHEN models.auto_retired_at IS NULL THEN NULL
				ELSE models.discovery_dismissed_at
			END,
			last_seen_at = now()
		RETURNING ` + upsertColumns

	err := r.pool.QueryRow(ctx, query,
		m.ID, m.ProviderID, m.ModelID, m.Name, m.Description, m.DisplayName, m.Capabilities, m.Params,
		m.Modality, m.InputModalities, m.OutputModalities,
		m.ContextLength, m.MaxOutputTokens, m.InputPricePerMillion, m.InputPricePerMillionCacheHit, m.OutputPricePerMillion, m.OwnedBy, m.Enabled,
		// $19/$20: "this value came from the provider's live API" flags for the
		// context/output-limit CASE clauses above (overwrite vs fill-only). Zero
		// value (false) => fill-only, the safe default. The price columns don't
		// take flags: they follow the incoming value unless price_customized,
		// judged entirely inside the query.
		m.LiveMeta.ContextLength, m.LiveMeta.MaxOutputTokens,
		// $21: the sources the scan stamped on its prices, merged in the
		// price_sources clause and written whole on insert.
		m.PriceSources,
		// $22: the per-search price, merged like the per-million ones.
		m.SearchPricePerThousand,
		// $23: the incoming capabilities are a placeholder; keep the stored ones.
		m.PreserveCapabilities,
	).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion, &m.SearchPricePerThousand,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.DisplayNameCustomized, &m.PriceCustomized, &m.LimitsCustomized, &m.PriceSources, &m.CreatedAt, &m.LastSeenAt,
	)

	if err != nil {
		debuglog.Error("model: upsert failed", "model_id", m.ModelID, "provider", m.ProviderName, "provider_id", m.ProviderID, "error", err)
	}
	InvalidateModelCache()
	return err
}

// scanModel reads one modelColumns row. pgx.Rows satisfies pgx.Row, so the
// multi-row loop scans through here too.
func scanModel(row pgx.Row) (*Model, error) {
	var m Model
	if err := row.Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion, &m.SearchPricePerThousand,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.DisplayNameCustomized, &m.PriceCustomized, &m.LimitsCustomized, &m.PriceSources, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
	); err != nil {
		return nil, err
	}
	return &m, nil
}

func scanModels(rows pgx.Rows) ([]*Model, error) {
	var models []*Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

// List returns all models, optionally filtered by provider ID.
func (r *Repository) List(ctx context.Context, providerID *uuid.UUID) ([]*Model, error) {
	return r.ListFiltered(ctx, providerID, nil)
}

// ListFiltered returns models filtered by provider ID and/or the owning
// provider's enabled flag. A nil filter means "any". The provider flag is what
// separates rows the proxy can serve (see ListEnabled) from rows that merely
// exist: a disabled provider keeps its models, pins, prices and failover
// memberships so re-enabling it is instant, but the dashboard must not count
// those rows as available.
func (r *Repository) ListFiltered(ctx context.Context, providerID *uuid.UUID, providerEnabled *bool) ([]*Model, error) {
	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id`

	var conditions []string
	var args []any
	if providerID != nil {
		args = append(args, *providerID)
		conditions = append(conditions, fmt.Sprintf("m.provider_id = $%d", len(args)))
	}
	if providerEnabled != nil {
		args = append(args, *providerEnabled)
		conditions = append(conditions, fmt.Sprintf("COALESCE(p.enabled, false) = $%d", len(args)))
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}

	query += " ORDER BY m.model_id ASC"

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanModels(rows)
}

// PinnedModelIDs returns the model_ids of one provider's manual-enable pins
// (manually_enabled_at, migration 070) as a set. The pin has no field on Model:
// it governs what discovery may do to a row, not what the row is, so it is read
// where that decision is made rather than carried through every model scan.
func (r *Repository) PinnedModelIDs(ctx context.Context, providerID uuid.UUID) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT model_id FROM models WHERE provider_id = $1 AND manually_enabled_at IS NOT NULL`, providerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pinned := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		pinned[id] = true
	}
	return pinned, rows.Err()
}

// ListEnabled returns all enabled models from enabled providers.
func (r *Repository) ListEnabled(ctx context.Context) ([]*Model, error) {
	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.enabled = true AND p.enabled = true ORDER BY m.model_id ASC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanModels(rows)
}

// Get retrieves a model by its UUID.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Model, error) {
	if m, ok := GetCachedByUUID(id); ok {
		return m, nil
	}

	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.id = $1`

	m, err := scanModel(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		return nil, err
	}

	cacheModelByUUID(m)
	return m, nil
}

// GetByIDs retrieves multiple models by their UUIDs.
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

// GetByModelID returns all enabled models matching the given model ID string.
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

// GetByProviderAndModelID retrieves a model by provider ID and model ID.
func (r *Repository) GetByProviderAndModelID(ctx context.Context, providerID uuid.UUID, modelID string) (*Model, error) {
	if m, ok := GetCachedByCompositeKey(providerID, modelID); ok {
		return m, nil
	}

	query := `SELECT ` + modelColumns + ` FROM models m JOIN providers p ON m.provider_id = p.id WHERE m.provider_id = $1 AND m.model_id = $2`

	m, err := scanModel(r.pool.QueryRow(ctx, query, providerID, modelID))
	if err != nil {
		return nil, err
	}

	cacheModelByCompositeKey(providerID, modelID, m)
	cacheModelByUUID(m)
	return m, nil
}

// DeleteByID removes a model by its UUID.
func (r *Repository) DeleteByID(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM models WHERE id = $1`, id)
	if err != nil {
		debuglog.Error("model: delete failed", "id", id, "error", err)
		return err
	}
	InvalidateModelCache()
	return nil
}

// DeleteByIDs removes multiple models in a single statement and returns the
// number of rows actually deleted (IDs that no longer exist are silently
// skipped, matching DeleteByID's idempotent semantics). It exists so the Models
// page can clear a large selection in one request instead of firing one HTTP
// DELETE per model — a burst that trips the admin IP rate limiter.
func (r *Repository) DeleteByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM models WHERE id = ANY($1)`, ids)
	if err != nil {
		debuglog.Error("model: bulk delete failed", "count", len(ids), "error", err)
		return 0, err
	}
	InvalidateModelCache()
	return tag.RowsAffected(), nil
}

// UpdateModelRequest contains optional fields for updating a model.
//
// Editing any price implicitly sets the price_customized pin, so discovery
// stops refreshing that model's prices from live/catalog/models.dev.
// PriceCustomized set to false explicitly clears the pin AND nulls every
// price columns, so the next scan re-derives them from source; set to true it
// pins the currently stored prices without editing them. An explicit
// PriceCustomized wins over the implicit pin when both appear in one request.
type UpdateModelRequest struct {
	DisplayName                  *string  `json:"display_name"`
	ContextLength                *int     `json:"context_length"`
	MaxOutputTokens              *int     `json:"max_output_tokens"`
	InputPricePerMillion         *float64 `json:"input_price_per_million"`
	InputPricePerMillionCacheHit *float64 `json:"input_price_per_million_cache_hit"`
	OutputPricePerMillion        *float64 `json:"output_price_per_million"`
	SearchPricePerThousand       *float64 `json:"search_price_per_thousand"`
	PriceCustomized              *bool    `json:"price_customized"`
	// LimitsCustomized false clears the limits pin and nulls both limits so
	// the next scan refills them; true (or any limit edit) pins them.
	LimitsCustomized *bool `json:"limits_customized"`
	Enabled          *bool `json:"enabled"`
}

// Update applies partial updates to a model.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, req UpdateModelRequest) (*Model, error) {
	var setClauses []string
	var args []any
	argIdx := 2 // $1 is reserved for id

	if req.DisplayName != nil {
		if *req.DisplayName == "" {
			// Empty string = clear to NULL, reset customization flag
			setClauses = append(setClauses, "display_name = NULL", "display_name_customized = false")
		} else {
			setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argIdx))
			args = append(args, *req.DisplayName)
			argIdx++
			setClauses = append(setClauses, fmt.Sprintf("display_name_customized = $%d", argIdx))
			args = append(args, true)
			argIdx++
		}
	}
	// The limits pin follows the operator's action the way the price pin does
	// below: an edit pins, an explicit unpin nulls the columns (and suppresses
	// edits in the same request, the two being contradictory) so the next
	// scan refills them from source.
	unpinLimits := req.LimitsCustomized != nil && !*req.LimitsCustomized
	limitsEdited := false
	if !unpinLimits {
		if req.ContextLength != nil {
			setClauses = append(setClauses, fmt.Sprintf("context_length = $%d", argIdx))
			args = append(args, *req.ContextLength)
			argIdx++
			limitsEdited = true
		}
		if req.MaxOutputTokens != nil {
			setClauses = append(setClauses, fmt.Sprintf("max_output_tokens = $%d", argIdx))
			args = append(args, *req.MaxOutputTokens)
			argIdx++
			limitsEdited = true
		}
	}
	switch {
	case unpinLimits:
		setClauses = append(setClauses, "limits_customized = false", "context_length = NULL", "max_output_tokens = NULL")
	case limitsEdited || (req.LimitsCustomized != nil && *req.LimitsCustomized):
		setClauses = append(setClauses, "limits_customized = true")
	}
	// The pin follows the operator's action: editing a price pins it (their
	// number must survive the next scan), an explicit PriceCustomized overrides
	// that, and unpinning also nulls the price columns so the next scan
	// re-derives them from source instead of keeping the operator's leftovers.
	// An unpin therefore suppresses price edits in the same request — the two
	// are contradictory, and the explicit unpin wins (also avoids assigning the
	// same column twice in one UPDATE).
	unpin := req.PriceCustomized != nil && !*req.PriceCustomized
	priceEdited := false
	edited := PriceSources{}
	if !unpin {
		if req.InputPricePerMillion != nil {
			setClauses = append(setClauses, fmt.Sprintf("input_price_per_million = $%d", argIdx))
			args = append(args, *req.InputPricePerMillion)
			argIdx++
			priceEdited = true
			edited.Input = PriceSourceManual
		}
		if req.InputPricePerMillionCacheHit != nil {
			setClauses = append(setClauses, fmt.Sprintf("input_price_per_million_cache_hit = $%d", argIdx))
			args = append(args, *req.InputPricePerMillionCacheHit)
			argIdx++
			priceEdited = true
			edited.CacheHit = PriceSourceManual
		}
		if req.OutputPricePerMillion != nil {
			setClauses = append(setClauses, fmt.Sprintf("output_price_per_million = $%d", argIdx))
			args = append(args, *req.OutputPricePerMillion)
			argIdx++
			priceEdited = true
			edited.Output = PriceSourceManual
		}
		if req.SearchPricePerThousand != nil {
			setClauses = append(setClauses, fmt.Sprintf("search_price_per_thousand = $%d", argIdx))
			args = append(args, *req.SearchPricePerThousand)
			argIdx++
			priceEdited = true
			edited.Search = PriceSourceManual
		}
	}
	if unpin {
		// The prices go with the pin, and so do their sources: the next scan
		// writes both afresh.
		setClauses = append(setClauses, "price_customized = false",
			"input_price_per_million = NULL", "input_price_per_million_cache_hit = NULL", "output_price_per_million = NULL", "search_price_per_thousand = NULL",
			"price_sources = '{}'::jsonb")
	} else if priceEdited {
		// Only the edited prices become the operator's; the others keep the
		// source that wrote them.
		setClauses = append(setClauses, fmt.Sprintf("price_sources = price_sources || $%d", argIdx))
		args = append(args, edited)
		argIdx++
	}
	if req.PriceCustomized != nil && !unpin || priceEdited {
		setClauses = append(setClauses, "price_customized = true")
	}
	if req.Enabled != nil {
		setClauses = append(setClauses, fmt.Sprintf("enabled = $%d", argIdx))
		args = append(args, *req.Enabled)
		argIdx++
		setClauses = append(setClauses, fmt.Sprintf("disabled_manually = $%d", argIdx))
		args = append(args, !*req.Enabled)
		// Operator intent supersedes a traffic retirement AND their own earlier
		// dismissal, same as SetEnabled — and for the same reason it has to happen
		// in this statement rather than on the next sighting: a model retired
		// again before that scan would keep a dismissal nothing could clear.
		setClauses = append(setClauses, "auto_retired_at = NULL", "discovery_dismissed_at = NULL")
		// And the manual-enable pin follows the operator's verdict, same as in
		// SetEnabled: an enable arms it, a disable withdraws it. Only a write
		// that touches enabled says anything about the pin, so editing a display
		// name or a price leaves it exactly where it was.
		if *req.Enabled {
			setClauses = append(setClauses, "manually_enabled_at = now()")
		} else {
			setClauses = append(setClauses, "manually_enabled_at = NULL")
		}
	}

	if len(setClauses) == 0 {
		return r.Get(ctx, id)
	}

	args = append([]any{id}, args...)

	query := fmt.Sprintf("UPDATE models SET %s WHERE id = $1", strings.Join(setClauses, ", "))

	_, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		debuglog.Error("model: update failed", "id", id, "error", err)
		return nil, err
	}
	InvalidateModelCache()
	return r.Get(ctx, id)
}
