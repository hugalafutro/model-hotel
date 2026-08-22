package model

import (
	"context"
	"errors"
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
	OwnedBy                      string    `json:"owned_by"`
	Enabled                      bool      `json:"enabled"`
	DisabledManually             bool      `json:"disabled_manually"`
	DisplayNameCustomized        bool      `json:"display_name_customized"`
	PriceCustomized              bool      `json:"price_customized"`
	CreatedAt                    time.Time `json:"created_at"`
	LastSeenAt                   time.Time `json:"last_seen_at"`
	ProviderName                 string    `json:"provider_name"`
	ProviderEnabled              bool      `json:"provider_enabled"`

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

const modelColumns = `m.id, m.provider_id, m.model_id, COALESCE(m.name, ''), COALESCE(m.description, ''), COALESCE(m.display_name, ''), COALESCE(m.capabilities, '{}'), COALESCE(m.params, '{}'), COALESCE(m.modality, ''), COALESCE(m.input_modalities, '[]'), COALESCE(m.output_modalities, '[]'), m.context_length, m.max_output_tokens, m.input_price_per_million, m.input_price_per_million_cache_hit, m.output_price_per_million, COALESCE(m.owned_by, ''), m.enabled, m.disabled_manually, m.display_name_customized, m.price_customized, m.created_at, COALESCE(m.last_seen_at, m.created_at), p.name, COALESCE(p.enabled, false)`

const upsertColumns = `id, provider_id, model_id, COALESCE(name, ''), COALESCE(description, ''), COALESCE(display_name, ''), COALESCE(capabilities, '{}'), COALESCE(params, '{}'), COALESCE(modality, ''), COALESCE(input_modalities, '[]'), COALESCE(output_modalities, '[]'), context_length, max_output_tokens, input_price_per_million, input_price_per_million_cache_hit, output_price_per_million, COALESCE(owned_by, ''), enabled, disabled_manually, display_name_customized, price_customized, created_at, COALESCE(last_seen_at, created_at)`

// Upsert inserts or updates a model based on provider_id and model_id.
func (r *Repository) Upsert(ctx context.Context, m *Model) error {
	query := `
		INSERT INTO models (id, provider_id, model_id, name, description, display_name, capabilities, params, modality, input_modalities, output_modalities, context_length, max_output_tokens, input_price_per_million, input_price_per_million_cache_hit, output_price_per_million, owned_by, enabled, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, now())
		ON CONFLICT (provider_id, model_id)
		DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			display_name = CASE WHEN models.display_name_customized THEN models.display_name ELSE EXCLUDED.display_name END,
			capabilities = EXCLUDED.capabilities,
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
			context_length = CASE WHEN $19 THEN COALESCE(EXCLUDED.context_length, models.context_length) ELSE COALESCE(models.context_length, EXCLUDED.context_length) END,
			max_output_tokens = CASE WHEN $20 THEN COALESCE(EXCLUDED.max_output_tokens, models.max_output_tokens) ELSE COALESCE(models.max_output_tokens, EXCLUDED.max_output_tokens) END,
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
	).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.DisplayNameCustomized, &m.PriceCustomized, &m.CreatedAt, &m.LastSeenAt,
	)

	if err != nil {
		debuglog.Error("model: upsert failed", "model_id", m.ModelID, "provider", m.ProviderName, "provider_id", m.ProviderID, "error", err)
	}
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
			&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.DisplayNameCustomized, &m.PriceCustomized, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
		); err != nil {
			return nil, err
		}
		models = append(models, &m)
	}
	return models, nil
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

	var m Model
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.DisplayNameCustomized, &m.PriceCustomized, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
	)

	if err != nil {
		return nil, err
	}

	cacheModelByUUID(&m)
	return &m, nil
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

	var m Model
	err := r.pool.QueryRow(ctx, query, providerID, modelID).Scan(
		&m.ID, &m.ProviderID, &m.ModelID, &m.Name, &m.Description, &m.DisplayName, &m.Capabilities,
		&m.Params, &m.Modality, &m.InputModalities, &m.OutputModalities,
		&m.ContextLength, &m.MaxOutputTokens, &m.InputPricePerMillion, &m.InputPricePerMillionCacheHit, &m.OutputPricePerMillion,
		&m.OwnedBy, &m.Enabled, &m.DisabledManually, &m.DisplayNameCustomized, &m.PriceCustomized, &m.CreatedAt, &m.LastSeenAt, &m.ProviderName, &m.ProviderEnabled,
	)

	if err != nil {
		return nil, err
	}

	cacheModelByCompositeKey(providerID, modelID, &m)
	cacheModelByUUID(&m)
	return &m, nil
}

// DisabledModelRef identifies a model that was newly disabled by discovery.
type DisabledModelRef struct {
	ID      uuid.UUID
	ModelID string
}

// MissingScanThreshold is how many consecutive confirmed-missing discovery
// scans it takes before a model is disabled. Each scan-level miss is already
// triple-checked by in-scan confirmation probes (api.ConfirmMissingModels), so
// two independent scans missing the same model is strong evidence it is gone,
// while a single flaky scan (DNS flap, partial upstream listing) never
// disables anything.
const MissingScanThreshold = 2

// RecordMissingModels applies one scan's membership verdict: enabled models
// absent from presentModelIDs get their consecutive-miss streak incremented,
// and only those whose streak reaches MissingScanThreshold are disabled (the
// streak resets so a later reappearance starts clean). Present models have any
// streak cleared. Returns the newly disabled models and the still-enabled
// pending ones (streak below threshold). An empty presentModelIDs list is a
// no-op guard: an empty listing is far more likely a broken scan than a
// provider that removed every model.
//
// A model the operator pinned by enabling it manually (manually_enabled_at, see
// migration 070) is exempt: its streak keeps growing, but it is never disabled
// and appears in NEITHER return slice. The operator tested that model against
// the provider after the listing stopped naming it, so their evidence is newer
// and more direct than the listing's silence — and returning it as pending
// would raise a claim asking them to decide something they already decided.
// The exemption only covers this listing-based path; a refusal on real traffic
// still retires the model (AutoRetireIfConfirmed), and the next sighting hands
// it back to automatic management by clearing the pin.
func (r *Repository) RecordMissingModels(ctx context.Context, providerID uuid.UUID, providerName string, presentModelIDs []string) (disabled, pending []DisabledModelRef, err error) {
	if len(presentModelIDs) == 0 {
		return nil, nil, nil
	}

	// One atomic statement: the CTE clears the streak of every present model
	// (Upsert also does this, but "present" here includes reappeared models a
	// confirmation probe found that the caller did not re-upsert), while the
	// main UPDATE records one confirmed miss for every enabled model the scan
	// did not list. Rows that reach the threshold are disabled with their
	// streak reset (a later reappearance must not sit one flaky scan away
	// from another disable); the rest keep counting into the next scan. A pinned
	// row takes neither branch: its streak accrues untouched, so the count is
	// there to read the moment a sighting clears the pin.
	rows, err := r.pool.Query(ctx, `
		WITH reset AS (
			UPDATE models SET missing_scans = 0
			WHERE provider_id = $1 AND model_id = ANY($2) AND missing_scans > 0
		)
		UPDATE models
		SET missing_scans = CASE WHEN missing_scans + 1 >= $3 AND manually_enabled_at IS NULL THEN 0 ELSE missing_scans + 1 END,
		    enabled = CASE WHEN missing_scans + 1 >= $3 AND manually_enabled_at IS NULL THEN false ELSE enabled END
		WHERE provider_id = $1 AND model_id != ALL($2) AND enabled = true
		RETURNING id, model_id, NOT enabled, manually_enabled_at IS NOT NULL
	`, providerID, presentModelIDs, MissingScanThreshold)
	if err != nil {
		debuglog.Error("model: record missing failed", "provider", providerName, "provider_id", providerID, "error", err)
		return nil, nil, err
	}
	defer rows.Close()
	// Pinned rows are collected separately so they leave both return slices
	// empty-handed: neither a disable to announce nor a claim to raise.
	var pinnedRefs []DisabledModelRef
	for rows.Next() {
		var ref DisabledModelRef
		var wasDisabled, pinned bool
		if err := rows.Scan(&ref.ID, &ref.ModelID, &wasDisabled, &pinned); err != nil {
			debuglog.Error("model: record missing scan failed", "provider", providerName, "provider_id", providerID, "error", err)
			return nil, nil, err
		}
		switch {
		case pinned:
			pinnedRefs = append(pinnedRefs, ref)
		case wasDisabled:
			disabled = append(disabled, ref)
		default:
			pending = append(pending, ref)
		}
	}
	if err := rows.Err(); err != nil {
		debuglog.Error("model: record missing failed", "provider", providerName, "provider_id", providerID, "error", err)
		return nil, nil, err
	}

	if len(pinnedRefs) > 0 {
		debuglog.Info("model: pinned models still missing from listing", "provider", providerName, "count", len(pinnedRefs))
	}
	if len(disabled) > 0 || len(pending) > 0 {
		debuglog.Info("model: recorded missing models",
			"provider", providerName, "provider_id", providerID,
			"disabled", len(disabled), "pending", len(pending), "threshold", MissingScanThreshold)
	}
	InvalidateModelCache()
	return disabled, pending, nil
}

// SetEnabled enables or disables a model by its UUID. This is the OPERATOR
// path: it records the choice in disabled_manually and clears any traffic
// retirement, since a hand-written enabled flag supersedes what the gateway
// concluded on its own (migration 063).
//
// It clears the operator's own dismissal for the same reason, and clearing it
// HERE rather than leaving it to the next sighting is what keeps a claim
// recoverable. Upsert only clears the stamp while auto_retired_at is NULL, so a
// model that is dismissed, enabled by hand, and then retired again by traffic
// before the next discovery scan — seconds versus about an hour, so the likely
// order, not the unlikely one — would carry a dismissal that nothing could ever
// clear again. It would sit disabled and absent from the claim list for good.
// Doing it in the same statement as the enable makes the recovery atomic instead
// of dependent on scan timing.
//
// An enable also arms manually_enabled_at, the pin that keeps discovery from
// disabling the model again for being absent from the provider's listing
// (migration 070). A disable withdraws it: the operator is no longer vouching
// for a model they just switched off.
func (r *Repository) SetEnabled(ctx context.Context, id uuid.UUID, enabled bool) (*Model, error) {
	query := `UPDATE models SET enabled = $1, disabled_manually = NOT $1,
	                            auto_retired_at = NULL, discovery_dismissed_at = NULL,
	                            manually_enabled_at = CASE WHEN $1 THEN now() ELSE NULL END
	           WHERE id = $2`
	_, err := r.pool.Exec(ctx, query, enabled, id)
	if err != nil {
		debuglog.Error("model: set enabled failed", "id", id, "enabled", enabled, "error", err)
		return nil, err
	}
	InvalidateModelCache()
	return r.Get(ctx, id)
}

// AutoRetireIfConfirmed disables a model the proxy has concluded the provider no
// longer serves, staging the write inside a transaction and committing it only
// if confirm still holds once the row is written. It reports whether the change
// was committed.
//
// Staging exists because the justification can expire while the write is being
// made: the model can answer a request — proving the decision wrong — mid-write.
// Deciding, writing, then undoing would work on the model row alone, but the
// undo is not enough, because the disabled state is VISIBLE to other sessions in
// between. A concurrent custom-group revalidation that samples it will
// auto-disable the group for having too few routable members, and nothing
// re-enables that group when the model comes back. Staging removes the
// intermediate state rather than correcting it: an abandoned write is never
// committed, so nothing can derive state from it.
//
// It stamps auto_retired_at instead of disabled_manually, which keeps this
// distinct from both an operator's disable and discovery's. See migration 063
// for why all three have to be told apart; the short version is that a
// re-sighting must not revive this model, because the provider was refusing it
// while still listing it.
//
// The write is conditional on the row still being a routable, untouched model.
// What it cannot see is evidence that predates an operator's action: strikes
// gathered before they enabled the model still read as a routable model here.
// That resolves itself rather than needing to be detected — if the model really
// is gone it refuses three more requests and is retired again, with a fresh
// alert, which is the correct answer to an operator enabling a dead model.
//
// confirm runs with the row already written and locked, so keep it to an
// in-memory check — anything slow holds a row lock for its duration.
func (r *Repository) AutoRetireIfConfirmed(ctx context.Context, id uuid.UUID, confirm func() bool) (bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		debuglog.Error("model: auto-retire begin failed", "id", id, "error", err)
		return false, err
	}
	// Safe on both paths: Rollback after a successful Commit is a no-op.
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock the row and check it still looks like the model the evidence was
	// gathered against, before writing anything.
	//
	// The decision is made on the request path and executed here, so the row can
	// have moved on in between — an operator disabling it by hand, an operator
	// re-enabling it after an earlier retirement, another member of the fleet
	// retiring it first. Writing by id alone would overwrite whatever they did
	// with a conclusion drawn from traffic that predates it.
	//
	// FOR UPDATE is what makes the check worth making: it holds the row from
	// here until the transaction ends, so no operator write can slip between the
	// check and the commit. Combined with the staging below, the entire decision
	// is atomic with respect to everything else touching this model.
	var enabled, manual bool
	var retiredAt *time.Time
	switch err := tx.QueryRow(ctx,
		`SELECT enabled, disabled_manually, auto_retired_at FROM models WHERE id = $1 FOR UPDATE`,
		id).Scan(&enabled, &manual, &retiredAt); {
	case errors.Is(err, pgx.ErrNoRows):
		// Deleted since the decision. Nothing to retire, and not an error.
		return false, nil
	case err != nil:
		debuglog.Error("model: auto-retire state read failed", "id", id, "error", err)
		return false, err
	}
	if !enabled || manual || retiredAt != nil {
		debuglog.Info("model: skipping auto-retire, the model's state changed since the decision",
			"id", id, "enabled", enabled, "disabled_manually", manual, "already_retired", retiredAt != nil)
		return false, nil
	}

	query := `UPDATE models SET enabled = false, auto_retired_at = now() WHERE id = $1`
	if _, err := tx.Exec(ctx, query, id); err != nil {
		debuglog.Error("model: auto-retire failed", "id", id, "error", err)
		return false, err
	}

	if !confirm() {
		// The deferred rollback discards the write. Nothing else ever saw it,
		// so there is no cache to invalidate and nothing to undo.
		return false, nil
	}

	if err := tx.Commit(ctx); err != nil {
		debuglog.Error("model: auto-retire commit failed", "id", id, "error", err)
		return false, err
	}
	InvalidateModelCache()
	return true, nil
}

// RevertAutoRetire undoes a traffic retirement this gateway wrote, and reports
// whether it actually undid one.
//
// Conditional on the row still being exactly as the retirement left it. The undo
// runs after the disable has committed, so anything can have happened in
// between — and the case that matters is an operator disabling the model by hand
// in that window. An unconditional re-enable would silently return their
// disabled model to routing, overwriting a deliberate decision with a stale
// one. The predicate also covers the model having been re-enabled already, and
// the retirement having been cleared by an operator, both of which mean there is
// nothing here to revert.
func (r *Repository) RevertAutoRetire(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE models
		   SET enabled = true, auto_retired_at = NULL
		 WHERE id = $1
		   AND enabled = false
		   AND disabled_manually = false
		   AND auto_retired_at IS NOT NULL`, id)
	if err != nil {
		debuglog.Error("model: revert auto-retire failed", "id", id, "error", err)
		return false, err
	}
	if tag.RowsAffected() == 0 {
		return false, nil
	}
	InvalidateModelCache()
	return true, nil
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
// PriceCustomized set to false explicitly clears the pin AND nulls all three
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
	PriceCustomized              *bool    `json:"price_customized"`
	Enabled                      *bool    `json:"enabled"`
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
	// The pin follows the operator's action: editing a price pins it (their
	// number must survive the next scan), an explicit PriceCustomized overrides
	// that, and unpinning also nulls the price columns so the next scan
	// re-derives them from source instead of keeping the operator's leftovers.
	// An unpin therefore suppresses price edits in the same request — the two
	// are contradictory, and the explicit unpin wins (also avoids assigning the
	// same column twice in one UPDATE).
	unpin := req.PriceCustomized != nil && !*req.PriceCustomized
	priceEdited := false
	if !unpin {
		if req.InputPricePerMillion != nil {
			setClauses = append(setClauses, fmt.Sprintf("input_price_per_million = $%d", argIdx))
			args = append(args, *req.InputPricePerMillion)
			argIdx++
			priceEdited = true
		}
		if req.InputPricePerMillionCacheHit != nil {
			setClauses = append(setClauses, fmt.Sprintf("input_price_per_million_cache_hit = $%d", argIdx))
			args = append(args, *req.InputPricePerMillionCacheHit)
			argIdx++
			priceEdited = true
		}
		if req.OutputPricePerMillion != nil {
			setClauses = append(setClauses, fmt.Sprintf("output_price_per_million = $%d", argIdx))
			args = append(args, *req.OutputPricePerMillion)
			argIdx++
			priceEdited = true
		}
	}
	if unpin {
		setClauses = append(setClauses, "price_customized = false",
			"input_price_per_million = NULL", "input_price_per_million_cache_hit = NULL", "output_price_per_million = NULL")
	} else if req.PriceCustomized != nil || priceEdited {
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
