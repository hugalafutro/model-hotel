package api

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
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
	LastSeenAt    time.Time `json:"last_seen_at,omitempty"`
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
	Entries   []ModelResponse `json:"entries"`
	Total     int             `json:"total"`
	HasBefore bool            `json:"has_before"`
	HasAfter  bool            `json:"has_after"`
}

// RegisterModels mounts model management routes.
func (h *Handler) RegisterModels(r chi.Router) {
	r.Route("/models", func(r chi.Router) {
		r.Get("/", h.ListModels)
		r.Get("/cursor", h.ListModelsCursor)
		r.Patch("/{id}", h.UpdateModel)
		r.Delete("/{id}", h.DeleteModel)
		r.Post("/{id}/test", h.TestModel)
	})
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

	models, err := modelRepo.List(r.Context(), providerID)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "invalid request body", err)
		return
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())

	hasChanges := req.DisplayName != nil || req.ContextLength != nil || req.MaxOutputTokens != nil || req.InputPricePerMillion != nil || req.OutputPricePerMillion != nil || req.Enabled != nil
	if !hasChanges {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}

	// Validate field bounds
	dn, dnErr := validateNamePtr("display_name", req.DisplayName, 1, 128)
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
	if err := modelRepo.DeleteByID(r.Context(), id); err != nil {
		respondError(w, fmt.Sprintf("failed to delete model %s", id), err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// TestModelResponse is the JSON response for model test requests.
type TestModelResponse struct {
	Success    bool   `json:"success"`
	Streaming  bool   `json:"streaming"`
	TTFTMs     *int64 `json:"ttft_ms,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Response   string `json:"response"`
	Error      string `json:"error,omitempty"`
}

// TestModel tests a model by making a test request and returning latency metrics.
func (h *Handler) TestModel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "model ID")
	if !ok {
		return
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())
	m, err := modelRepo.Get(r.Context(), id)
	if err != nil {
		http.Error(w, "model not found", http.StatusNotFound)
		return
	}

	if !m.Enabled {
		http.Error(w, "model is disabled", http.StatusBadRequest)
		return
	}

	prov, err := h.providerRepo.Get(r.Context(), m.ProviderID)
	if err != nil {
		respondError(w, "provider not found", nil, http.StatusInternalServerError)
		return
	}

	start := time.Now()
	keyDecryptStart := time.Now()

	// Keyless providers store nil encrypted key bytes — skip decryption.
	var apiKey string
	if len(prov.EncryptedKey) == 0 {
		apiKey = ""
	} else {
		var err error
		apiKey, err = auth.Decrypt(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
		if err != nil {
			respondError(w, "failed to decrypt API key", nil, http.StatusInternalServerError)
			return
		}
	}
	keyDecryptMs := float64(time.Since(keyDecryptStart).Microseconds()) / 1000.0
	proxyOverheadMs := float64(time.Since(start).Microseconds()) / 1000.0

	body := map[string]interface{}{
		"model": m.ModelID,
		"messages": []map[string]string{
			{"role": "user", "content": "Respond only with `Hi`"},
		},
		"max_tokens": 10,
	}
	bodyBytes, _ := json.Marshal(body)

	providerType := provider.DetectProviderType(prov.BaseURL)
	targetURL := util.BuildProviderTargetURL(prov.BaseURL, providerType)
	//nolint:gosec // provider URL is admin-configured, not arbitrary user input
	proxyReq, _ := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(bodyBytes))
	util.SetProviderAuthHeaders(proxyReq, providerType, apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")

	reqHashBytes := make([]byte, 8)
	rand.Read(reqHashBytes)
	reqHash := hex.EncodeToString(reqHashBytes)

	startRequest := time.Now()
	testClient := &http.Client{Timeout: 30 * time.Second}
	//nolint:gosec // provider URL is admin-configured, not arbitrary user input
	resp, err := testClient.Do(proxyReq)
	if err != nil {
		logQuery := `
			INSERT INTO request_logs (
				provider_id, model_id, request_hash, status_code,
				latency_ms, duration_ms, ttft_ms,
				proxy_overhead_ms, parse_ms, failover_lookup_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, dial_ms, settings_read_ms,
				error_message, streaming, virtual_key_name, virtual_key_id, failover_attempt, state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		`
		durationMs := float64(time.Since(start).Milliseconds())
		_, logErr := h.dbPool.Pool().Exec(r.Context(), logQuery,
			m.ProviderID, m.ModelID, reqHash, 502,
			durationMs, durationMs, 0,
			proxyOverheadMs, 0, 0, 0, 0, keyDecryptMs, 0, 0,
			err.Error(), false, "internal", nil, 0, "failed",
		)
		if logErr != nil {
			debuglog.Error("admin: TestModel log insert failed", "error", logErr)
		}

		writeJSON(w, TestModelResponse{Error: err.Error()})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	duration := time.Since(startRequest).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		errMsg := util.SanitizeLogBody(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody)), 2000)

		logQuery := `
			INSERT INTO request_logs (
				provider_id, model_id, request_hash, status_code,
				latency_ms, duration_ms, ttft_ms,
				proxy_overhead_ms, parse_ms, failover_lookup_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, dial_ms, settings_read_ms,
				error_message, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, virtual_key_id, failover_attempt, state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24)
		`
		durationMs := float64(duration)
		_, logErr := h.dbPool.Pool().Exec(r.Context(), logQuery,
			m.ProviderID, m.ModelID, reqHash, resp.StatusCode,
			durationMs, durationMs, 0,
			proxyOverheadMs, 0, 0, 0, 0, keyDecryptMs, 0, 0,
			errMsg, 0, 0, 0, false, "internal", nil, 0, "failed",
		)
		if logErr != nil {
			debuglog.Error("admin: TestModel log insert failed", "error", logErr)
		}

		writeJSON(w, TestModelResponse{DurationMs: duration, Error: errMsg})
		return
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		debuglog.Debug("admin: failed to parse test model chat response", "error", err)
	}

	content := ""
	if len(chatResp.Choices) > 0 {
		content = chatResp.Choices[0].Message.Content
	}

	var tps float64
	if chatResp.Usage.CompletionTokens > 0 && duration > 0 {
		tps = float64(chatResp.Usage.CompletionTokens) / float64(duration) * 1000
	}

	logQuery := `
		INSERT INTO request_logs (
			provider_id, model_id, request_hash, status_code,
			latency_ms, duration_ms, ttft_ms,
			proxy_overhead_ms, parse_ms, failover_lookup_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms, dial_ms, settings_read_ms,
			tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, virtual_key_id, failover_attempt, state
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)
	`
	durationMs := float64(duration)
	// For a non-streaming test request, TTFT equals total duration
	// (no separate streaming phase). The log stores ttft_ms = 0 to
	// indicate non-streaming, but the API response reports duration as TTFT.
	_, logErr := h.dbPool.Pool().Exec(r.Context(), logQuery,
		m.ProviderID, m.ModelID, reqHash, resp.StatusCode,
		durationMs, durationMs, 0,
		proxyOverheadMs, 0, 0, 0, 0, keyDecryptMs, 0, 0,
		tps, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, false, "internal", nil, 0, "completed",
	)
	if logErr != nil {
		debuglog.Error("admin: TestModel log insert failed", "error", logErr)
	}

	writeJSON(w, TestModelResponse{
		Success:    true,
		Streaming:  false,
		TTFTMs:     &duration,
		DurationMs: duration,
		Response:   content,
	})
}

// See util.BuildProviderTargetURL for URL construction and util.SetProviderAuthHeaders for auth.
