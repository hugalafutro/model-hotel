package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// ModelResponse is the JSON response format for model API endpoints.
type ModelResponse struct {
	ID                           string   `json:"id"`
	ModelID                      string   `json:"model_id"`
	Name                         string   `json:"name"`
	Description                  string   `json:"description"`
	DisplayName                  string   `json:"display_name"`
	ProviderID                   string   `json:"provider_id"`
	ProviderName                 string   `json:"provider_name"`
	ProviderEnabled              bool     `json:"provider_enabled"`
	Capabilities                 string   `json:"capabilities"`
	Params                       string   `json:"params"`
	Modality                     string   `json:"modality"`
	InputModalities              string   `json:"input_modalities"`
	OutputModalities             string   `json:"output_modalities"`
	ContextLength                *int     `json:"context_length"`
	MaxOutputTokens              *int     `json:"max_output_tokens"`
	InputPricePerMillion         *float64 `json:"input_price_per_million"`
	InputPricePerMillionCacheHit *float64 `json:"input_price_per_million_cache_hit"`
	OutputPricePerMillion        *float64 `json:"output_price_per_million"`
	OwnedBy                      string   `json:"owned_by"`
	Enabled                      bool     `json:"enabled"`
	DisabledManually             bool     `json:"disabled_manually"`
	PriceCustomized              bool     `json:"price_customized"`
	CreatedAt                    string   `json:"created_at"`
	LastSeenAt                   string   `json:"last_seen_at"`
}

func modelToResponse(m model.Model) ModelResponse {
	return ModelResponse{
		ID:                           m.ID.String(),
		ModelID:                      m.ModelID,
		Name:                         m.Name,
		Description:                  m.Description,
		DisplayName:                  m.DisplayName,
		ProviderID:                   m.ProviderID.String(),
		ProviderName:                 m.ProviderName,
		ProviderEnabled:              m.ProviderEnabled,
		Capabilities:                 m.Capabilities,
		Params:                       m.Params,
		Modality:                     m.Modality,
		InputModalities:              m.InputModalities,
		OutputModalities:             m.OutputModalities,
		ContextLength:                m.ContextLength,
		MaxOutputTokens:              m.MaxOutputTokens,
		InputPricePerMillion:         m.InputPricePerMillion,
		InputPricePerMillionCacheHit: m.InputPricePerMillionCacheHit,
		OutputPricePerMillion:        m.OutputPricePerMillion,
		OwnedBy:                      m.OwnedBy,
		Enabled:                      m.Enabled,
		DisabledManually:             m.DisabledManually,
		PriceCustomized:              m.PriceCustomized,
		CreatedAt:                    m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		LastSeenAt:                   m.LastSeenAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

// modelCursor is the keyset cursor for cursor-based model pagination.
// The cursor fields depend on the sort_by parameter:
//   - name: uses Name + ModelID for keyset
//   - discovered: uses LastSeenAt for keyset
//   - context: uses ContextLength for keyset
//   - output: uses MaxOutputTokens for keyset
//   - provider: uses ProviderName for keyset
//   - status: uses StatusSort (0=active, 1=manually disabled, 2=disabled) for keyset
//
// All sorts include ID as a tiebreaker.
type modelCursor struct {
	SortBy        string    `json:"sort_by"`
	Name          string    `json:"name,omitempty"`
	ModelID       string    `json:"model_id,omitempty"`
	LastSeenAt    time.Time `json:"last_seen_at"`
	ContextLength *int      `json:"context_length,omitempty"`
	MaxOutput     *int      `json:"max_output_tokens,omitempty"`
	ProviderName  string    `json:"provider_name,omitempty"`
	StatusSort    *int      `json:"status_sort,omitempty"`
	ID            string    `json:"id"`
}

func (c *modelCursor) encode() string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

func (c *modelCursor) decode(s string) error {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("invalid base64: %w", err)
	}
	return json.Unmarshal(b, c)
}

// ModelsCursorResponse is the cursor-based paginated response for models.
type ModelsCursorResponse struct {
	Entries []ModelResponse `json:"entries"`
	// Total counts every row matching the filters. EnabledTotal counts the
	// subset the proxy can serve (model enabled AND provider enabled), the same
	// rule /v1/models applies, so the page title can report usable models even
	// though only one page of rows is loaded.
	Total        int `json:"total"`
	EnabledTotal int `json:"enabled_total"`
	// ParkedTotal counts rows whose provider is disabled: listed, kept, but not
	// served until the provider is enabled again.
	ParkedTotal int `json:"parked_total"`
	// DisabledTotal counts rows whose own enabled flag is off, parked or not:
	// exactly the rows the Models page's "delete disabled" removes for these
	// filters, so the button and the delete agree without loading every row.
	DisabledTotal int  `json:"disabled_total"`
	HasBefore     bool `json:"has_before"`
	HasAfter      bool `json:"has_after"`
}

// RegisterModels mounts model management routes.
func (h *Handler) RegisterModels(r chi.Router) {
	r.Route("/models", func(r chi.Router) {
		// Reads serve the Models page (models grant), the Chat UI's model
		// picker (chat grant), and the Dashboard's model count and drill-down
		// (usage grant; stats payloads already expose the model names).
		r.Group(func(r chi.Router) {
			r.Use(requireGrant(user.GrantModels, user.GrantChat, user.GrantUsage))
			r.Get("/", h.ListModels)
			r.Get("/cursor", h.ListModelsCursor)
		})
		r.Group(func(r chi.Router) {
			r.Use(requireAdmin)
			r.Post("/bulk-delete", h.BulkDeleteModels)
			r.Patch("/{id}", h.UpdateModel)
			r.Delete("/{id}", h.DeleteModel)
			r.Post("/{id}/test", h.TestModel)
		})
	})
}

// parseProviderEnabledParam reads the optional provider_enabled query value:
// "" means no filter, "true"/"false" filter on the owning provider's enabled
// flag (NULL counts as false, matching the proxy), anything else is a 400. Shared by the list and cursor endpoints so both
// views of the Models page agree on what "available on the proxy" means.
func parseProviderEnabledParam(w http.ResponseWriter, raw string) (*bool, bool) {
	return parseBoolFilterParam(w, "provider_enabled", raw)
}

// parseBoolFilterParam reads an optional tri-state boolean query value: ""
// means no filter, "true"/"false" filter, anything else is a 400 naming the
// parameter.
func parseBoolFilterParam(w http.ResponseWriter, name, raw string) (*bool, bool) {
	switch raw {
	case "":
		return nil, true
	case "true", "false":
		v := raw == "true"
		return &v, true
	default:
		http.Error(w, "invalid "+name, http.StatusBadRequest)
		return nil, false
	}
}

// ListModels returns all models with optional provider filtering.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	modelRepo := model.NewRepository(h.dbPool.Pool())

	providerIDParam := r.URL.Query().Get("provider_id")
	var providerID *uuid.UUID

	if providerIDParam != "" {
		parsedID, err := uuid.Parse(providerIDParam)
		if err != nil {
			http.Error(w, "invalid provider_id", http.StatusBadRequest)
			return
		}
		providerID = &parsedID
	}

	providerEnabled, ok := parseProviderEnabledParam(w, r.URL.Query().Get("provider_enabled"))
	if !ok {
		return
	}

	models, err := modelRepo.ListFiltered(r.Context(), providerID, providerEnabled)
	if err != nil {
		respondError(w, "failed to list models", err, http.StatusInternalServerError)
		return
	}

	responses := make([]ModelResponse, len(models))
	for i, m := range models {
		responses[i] = modelToResponse(*m)
	}

	writeJSON(w, responses)
}

// UpdateModel updates model configuration (enabled status, pricing overrides).
func (h *Handler) UpdateModel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "model ID")
	if !ok {
		return
	}

	var req model.UpdateModelRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())

	hasChanges := req.DisplayName != nil || req.ContextLength != nil || req.MaxOutputTokens != nil || req.InputPricePerMillion != nil || req.InputPricePerMillionCacheHit != nil || req.OutputPricePerMillion != nil || req.PriceCustomized != nil || req.Enabled != nil
	if !hasChanges {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	// Validate field bounds
	dn, dnErr := validateClearableNamePtr("display_name", req.DisplayName, 128)
	if dnErr != nil {
		respondBadRequest(w, "invalid display name", dnErr)
		return
	}
	req.DisplayName = dn

	if err := validateIntPtrRange("context_length", req.ContextLength, 256, 2000000); err != nil {
		respondBadRequest(w, "invalid context length", err)
		return
	}

	if err := validateIntPtrRange("max_output_tokens", req.MaxOutputTokens, 1, 128000); err != nil {
		respondBadRequest(w, "invalid max output_tokens", err)
		return
	}

	if err := validateFloatPtrRange("input_price_per_million", req.InputPricePerMillion, 0, 1000); err != nil {
		respondBadRequest(w, "invalid input price", err)
		return
	}

	if err := validateFloatPtrRange("input_price_per_million_cache_hit", req.InputPricePerMillionCacheHit, 0, 1000); err != nil {
		respondBadRequest(w, "invalid cached input price", err)
		return
	}

	if err := validateFloatPtrRange("output_price_per_million", req.OutputPricePerMillion, 0, 1000); err != nil {
		respondBadRequest(w, "invalid output price", err)
		return
	}

	m, err := modelRepo.Update(r.Context(), id, req)
	if err != nil {
		respondError(w, fmt.Sprintf("failed to update model %s", id), err, http.StatusInternalServerError)
		return
	}

	resp := modelToResponse(*m)
	writeJSON(w, resp)
}

// DeleteModel removes a model from the database.
func (h *Handler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "model ID")
	if !ok {
		return
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())

	// Fetch the model before deletion so we can sync failover groups.
	var modelID string
	err := h.dbPool.Pool().QueryRow(r.Context(),
		"SELECT model_id FROM models WHERE id = $1", id,
	).Scan(&modelID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Model doesn't exist — idempotent delete, just return 204.
			// No failover sync needed since there's nothing to clean up.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		respondError(w, fmt.Sprintf("failed to lookup model %s", id), err, http.StatusInternalServerError)
		return
	}

	if err := modelRepo.DeleteByID(r.Context(), id); err != nil {
		respondError(w, fmt.Sprintf("failed to delete model %s", id), err, http.StatusInternalServerError)
		return
	}

	// Sync failover groups since the deleted model may leave a group
	// with too few candidates. SyncForModel handles the auto-group for
	// this model's base name; PruneModelUUID cleans up any custom groups
	// that reference the deleted model UUID.
	failoverRepo := failover.NewRepository(h.dbPool.Pool())
	bgCtx := context.WithoutCancel(r.Context())
	if _, err := failoverRepo.SyncForModel(bgCtx, modelID); err != nil {
		debuglog.Info("admin: failed to sync failover groups after model delete", "error", err)
	}
	if err := failoverRepo.PruneModelUUID(bgCtx, id); err != nil {
		debuglog.Info("admin: failed to prune stale failover entries after model delete", "error", err)
	}

	w.WriteHeader(http.StatusNoContent)
}

// maxBulkDeleteIDs caps the number of models a single bulk-delete request may
// remove, bounding the DELETE ... = ANY($1) parameter and the follow-up
// failover resync work. Far above any realistic manual selection.
const maxBulkDeleteIDs = 10000

// BulkDeleteRequest is the JSON body for POST /api/models/bulk-delete.
type BulkDeleteRequest struct {
	IDs []string `json:"ids"`
}

// BulkDeleteResponse reports the outcome of a bulk delete. Deleted may be less
// than Requested when some IDs no longer exist (idempotent, not an error).
type BulkDeleteResponse struct {
	Requested int64 `json:"requested"`
	Deleted   int64 `json:"deleted"`
}

// BulkDeleteModels removes many models in a single request and resyncs the
// affected failover groups once at the end. The Models page uses it to clear a
// large selection without firing one HTTP DELETE per model — a concurrent burst
// that trips the admin IP rate limiter and surfaces spurious "N failed" toasts.
func (h *Handler) BulkDeleteModels(w http.ResponseWriter, r *http.Request) {
	var req BulkDeleteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 {
		respondBadRequest(w, "ids must not be empty", nil)
		return
	}
	if len(req.IDs) > maxBulkDeleteIDs {
		respondBadRequest(w, fmt.Sprintf("too many ids (max %d)", maxBulkDeleteIDs), nil)
		return
	}

	ids := make([]uuid.UUID, 0, len(req.IDs))
	for _, s := range req.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			respondBadRequest(w, "invalid model ID: "+s, err)
			return
		}
		ids = append(ids, id)
	}

	// Capture the affected provider model_ids before deletion so we can resync
	// the right failover auto-groups afterwards.
	modelIDs, err := h.collectDistinctModelIDs(r.Context(), ids)
	if err != nil {
		respondError(w, "failed to look up models for bulk delete", err, http.StatusInternalServerError)
		return
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())
	deleted, err := modelRepo.DeleteByIDs(r.Context(), ids)
	if err != nil {
		respondError(w, "failed to bulk delete models", err, http.StatusInternalServerError)
		return
	}

	// Resync failover groups since the deleted models may leave groups with too
	// few candidates. SyncForModel handles the auto-group for each affected base
	// name (deduped, one resync per base instead of one per model); PruneModelUUID
	// cleans up any custom groups that referenced the deleted UUIDs. Best-effort
	// like single-model DeleteModel: log but don't fail the delete. WithoutCancel
	// so it survives the request completing.
	h.resyncFailoverAfterModelDelete(context.WithoutCancel(r.Context()), modelIDs, ids)

	writeJSON(w, BulkDeleteResponse{Requested: int64(len(req.IDs)), Deleted: deleted})
}

// collectDistinctModelIDs returns the distinct provider model_id strings for the
// given model UUIDs, used to decide which failover auto-groups to resync.
func (h *Handler) collectDistinctModelIDs(ctx context.Context, ids []uuid.UUID) ([]string, error) {
	rows, err := h.dbPool.Pool().Query(ctx, `SELECT DISTINCT model_id FROM models WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modelIDs []string
	for rows.Next() {
		var mid string
		if err := rows.Scan(&mid); err != nil {
			return nil, err
		}
		modelIDs = append(modelIDs, mid)
	}
	return modelIDs, rows.Err()
}

// resyncFailoverAfterModelDelete resyncs the auto-group for each affected base
// model once and prunes any custom groups that referenced the deleted UUIDs.
// This mirrors the per-model cleanup in DeleteModel, batched for a bulk delete.
func (h *Handler) resyncFailoverAfterModelDelete(ctx context.Context, modelIDs []string, deletedIDs []uuid.UUID) {
	ResyncFailoverAfterModelDelete(ctx, failover.NewRepository(h.dbPool.Pool()), modelIDs, deletedIDs)
}

// ResyncFailoverAfterModelDelete rebuilds the auto-groups of every affected
// base model name and drops the deleted UUIDs from custom groups. Best-effort:
// each failure is logged and the rest continues, because the rows are already
// gone and a half-synced group is better than an aborted cleanup. Shared by
// the dashboard bulk delete and the discovery-pass prune.
func ResyncFailoverAfterModelDelete(ctx context.Context, repo *failover.Repository, modelIDs []string, deletedIDs []uuid.UUID) {
	for _, mid := range modelIDs {
		if _, err := repo.SyncForModel(ctx, mid); err != nil {
			debuglog.Info("failover: sync after model delete failed", "model_id", mid, "error", err)
		}
	}
	for _, id := range deletedIDs {
		if err := repo.PruneModelUUID(ctx, id); err != nil {
			debuglog.Info("failover: prune stale entries after model delete failed", "id", id, "error", err)
		}
	}
}

// TestModelResponse is the JSON response for model test requests.
type TestModelResponse struct {
	Success          bool   `json:"success"`
	Streaming        bool   `json:"streaming"`
	TTFTMs           *int64 `json:"ttft_ms,omitempty"`
	ResponseHeaderMs *int64 `json:"response_header_ms,omitempty"`
	DurationMs       int64  `json:"duration_ms"`
	Response         string `json:"response"`
	Error            string `json:"error,omitempty"`
}

// TestModel tests a model by making a test request and returning latency metrics.
func (h *Handler) TestModel(w http.ResponseWriter, r *http.Request) {
	m, prov, ok := h.resolveTestModelTarget(w, r)
	if !ok {
		return
	}

	start := time.Now()
	keyDecryptStart := time.Now()
	apiKey, ok := h.decryptTestModelKey(w, prov)
	if !ok {
		return
	}
	keyDecryptMs := float64(time.Since(keyDecryptStart).Microseconds()) / 1000.0
	proxyOverheadMs := float64(time.Since(start).Microseconds()) / 1000.0

	baseBody, providerType, targetURL, reqHash := buildTestModelRequest(m, prov)

	startRequest := time.Now()
	resp, err := h.doTestModelRequest(r.Context(), providerType, targetURL, m.ModelID, apiKey, baseBody)
	if err != nil {
		durationMs := float64(time.Since(start).Milliseconds())
		h.logTestModelRequestError(r.Context(), m, reqHash, durationMs, proxyOverheadMs, keyDecryptMs, err.Error(), clientip.From(r))
		writeJSON(w, TestModelResponse{Error: err.Error()})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	duration := time.Since(startRequest).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		// The same two-layer scrub the proxy runs over the bodies it logs and
		// forwards. An auth failure is exactly where an upstream quotes the
		// operator's key back ("Incorrect API key provided: sk-..."), and this
		// string is returned to the dashboard AND persisted to
		// request_logs.error_message. The key was decrypted for the probe just
		// above, so the exact match covers shapes the regex cannot.
		errMsg := util.MaskCredential(apiKey, util.SanitizeLogBody(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)), 10000))
		h.logTestModelHTTPError(r.Context(), m, reqHash, resp.StatusCode, float64(duration), proxyOverheadMs, keyDecryptMs, errMsg, clientip.From(r))
		writeJSON(w, TestModelResponse{DurationMs: duration, Error: errMsg})
		return
	}

	content, tps, promptTokens, completionTokens := parseTestModelResponse(respBody, duration)
	h.logTestModelCompleted(r.Context(), m, reqHash, resp.StatusCode, float64(duration), proxyOverheadMs, keyDecryptMs, tps, promptTokens, completionTokens, clientip.From(r))
	writeJSON(w, TestModelResponse{
		Success:          true,
		Streaming:        false,
		ResponseHeaderMs: &duration,
		DurationMs:       duration,
		Response:         content,
	})
}

// resolveTestModelTarget loads and validates the model + provider for a test
// request: parse the id param, fetch the (enabled) model, fetch its provider. It
// writes the appropriate HTTP error and returns ok=false on any failure.
func (h *Handler) resolveTestModelTarget(w http.ResponseWriter, r *http.Request) (m *model.Model, prov *provider.Provider, ok bool) {
	id, ok := parseUUIDParam(w, r, "id", "model ID")
	if !ok {
		return nil, nil, false
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())
	m, err := modelRepo.Get(r.Context(), id)
	if err != nil {
		respondLookupError(w, err, pgx.ErrNoRows, "model not found", "failed to load model")
		return nil, nil, false
	}

	// A disabled model can still be probed when the caller opts in via
	// ?allow_disabled=true. The failover "Retry N/A" action uses this to
	// re-check members that went N/A (disabled) and re-enable the ones that
	// answer. The Models page test button never sets it, so its contract
	// (only enabled models are testable) is unchanged.
	if !m.Enabled && r.URL.Query().Get("allow_disabled") != "true" {
		http.Error(w, "model is disabled", http.StatusBadRequest)
		return nil, nil, false
	}

	prov, err = h.providerRepo.Get(r.Context(), m.ProviderID)
	if err != nil {
		respondError(w, "provider not found", nil, http.StatusInternalServerError)
		return nil, nil, false
	}
	return m, prov, true
}

// decryptTestModelKey decrypts the provider API key for a test request. Keyless
// providers (nil encrypted bytes) yield an empty key. It writes an HTTP error
// and returns ok=false if decryption fails.
func (h *Handler) decryptTestModelKey(w http.ResponseWriter, prov *provider.Provider) (apiKey string, ok bool) {
	// Keyless providers store nil encrypted key bytes — skip decryption.
	if len(prov.EncryptedKey) == 0 {
		return "", true
	}
	apiKey, err := auth.Decrypt(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
	if err != nil {
		respondError(w, "failed to decrypt API key", nil, http.StatusInternalServerError)
		return "", false
	}
	return apiKey, true
}

// buildTestModelRequest constructs the OpenAI-shaped chat-completions probe
// body (a short "Respond only with `Hi`" prompt) and resolves the provider type
// and target URL, returning them alongside a fresh random request hash for
// logging. The body is left un-rewritten here: doTestModelRequest sends it
// through paramrewrite.BuildUpstreamBody so the probe applies the exact same
// provider rewrites (param injection/stripping, learned renames) as live proxy
// traffic instead of maintaining a second, drift-prone body.
//
// max_tokens is kept small: the test only confirms the model responds. A
// reasoning model may spend the whole budget on reasoning and return empty
// content — that still counts as success, and the UI omits the response text
// when it's empty rather than paying for a full reasoning generation. For
// OpenAI gpt-5/o-series models that reject max_tokens, the shared self-heal
// renames it to max_completion_tokens and retries, so the probe succeeds.
func buildTestModelRequest(m *model.Model, prov *provider.Provider) (baseBody []byte, providerType, targetURL, reqHash string) {
	body := map[string]any{
		"model": m.ModelID,
		"messages": []map[string]string{
			{"role": "user", "content": "Respond only with `Hi`"},
		},
		"max_tokens": 10,
	}
	baseBody, _ = json.Marshal(body)

	providerType = provider.TypeOf(prov)
	targetURL = util.BuildProviderTargetURL(prov.BaseURL, providerType, "/chat/completions")
	switch providerType {
	case "vertex-express":
		// Vertex express serves chat only on the native generateContent route;
		// doTestModelRequest translates the probe body to match.
		targetURL = util.BuildProviderTargetURL(prov.BaseURL, providerType,
			"/publishers/google/models/"+url.PathEscape(m.ModelID)+":generateContent")
	case "anthropic-messages":
		// The Messages API is the only chat route this type serves.
		targetURL = util.BuildProviderTargetURL(prov.BaseURL, providerType, "/messages")
	}

	reqHashBytes := make([]byte, 8)
	rand.Read(reqHashBytes)
	reqHash = hex.EncodeToString(reqHashBytes)

	return baseBody, providerType, targetURL, reqHash
}

// doTestModelRequest sends the probe through the shared self-heal executor with
// a 30s-timeout client, honoring the test-only transport/redirect hooks when
// set. Routing through paramrewrite.SelfHealChatCompletion means the probe uses
// the same body-rewrite and 400 param-retry (e.g. max_tokens ->
// max_completion_tokens) that the proxy failover loop uses for live traffic.
func (h *Handler) doTestModelRequest(ctx context.Context, providerType, targetURL, modelID, apiKey string, baseBody []byte) (*http.Response, error) {
	testClient := &http.Client{Timeout: 30 * time.Second}
	if h.testModelTransport != nil {
		testClient.Transport = h.testModelTransport
	}
	if h.testModelCheckRedirect != nil {
		testClient.CheckRedirect = h.testModelCheckRedirect
	}
	switch providerType {
	case "vertex-express":
		return h.doTestModelEgressRequest(ctx, testClient, targetURL, providerType, modelID, apiKey, baseBody,
			gemini.TranslateRequest, gemini.BuildChatCompletion)
	case "anthropic-messages":
		return h.doTestModelEgressRequest(ctx, testClient, targetURL, providerType, modelID, apiKey, baseBody,
			anthropicegress.TranslateRequest, anthropicegress.BuildChatCompletion)
	}
	return paramrewrite.SelfHealChatCompletion(ctx, testClient, targetURL, providerType, modelID, baseBody, func(req *http.Request) {
		util.SetProviderAuthHeaders(req, providerType, apiKey)
		req.Header.Set("Content-Type", "application/json")
	})
}

// doTestModelEgressRequest sends the probe through an egress adapter's
// translation instead of the chat-completions self-heal (whose 400 param retry
// rewrites chat bodies, meaningless against a route that never sees one). A 200
// body is swapped for its chat.completion translation so parseTestModelResponse
// reads it unchanged.
//
// It serves every provider type whose chat traffic the proxy translates rather
// than forwards, and must keep serving all of them: a type routed through an
// adapter in the pipeline but through the self-heal here would have its Test
// button report a 404 for a model that answers live traffic perfectly well.
func (h *Handler) doTestModelEgressRequest(ctx context.Context, client *http.Client, targetURL, providerType, modelID, apiKey string, baseBody []byte,
	translateRequest func([]byte) ([]byte, string, bool, error),
	buildChatCompletion func([]byte, string, string, int64) ([]byte, error),
) (*http.Response, error) {
	body, _, _, err := translateRequest(baseBody)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // provider URL is admin-configured, not arbitrary user input
	req, err := http.NewRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	util.SetProviderAuthHeaders(req, providerType, apiKey)
	req.Header.Set("Content-Type", "application/json")
	//nolint:gosec // provider URL is admin-configured, not arbitrary user input
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	upstream, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return resp, err
	}
	translated, err := buildChatCompletion(upstream, "chatcmpl-test-"+modelID, modelID, time.Now().Unix())
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return resp, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(translated))
	return resp, nil
}
