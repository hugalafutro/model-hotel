package api

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

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
		CreatedAt:                    m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		LastSeenAt:                   m.LastSeenAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func (h *Handler) RegisterModels(r chi.Router) {
	r.Route("/models", func(r chi.Router) {
		r.Get("/", h.ListModels)
		r.Patch("/{id}", h.UpdateModel)
		r.Delete("/{id}", h.DeleteModel)
		r.Post("/{id}/test", h.TestModel)
	})
}

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
	if err := validateStringPtrLength("display_name", req.DisplayName, 1, 128); err != nil {
		respondBadRequest(w, "invalid display name", err)
		return
	}

	if err := validateIntPtrRange("context_length", req.ContextLength, 256, 2000000); err != nil {
		respondBadRequest(w, "invalid context length", err)
		return
	}

	if err := validateIntPtrRange("max_output_tokens", req.MaxOutputTokens, 1, 128000); err != nil {
		respondBadRequest(w, "invalid max output tokens", err)
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
		respondError(w, "failed to update model", err, http.StatusInternalServerError)
		return
	}

	resp := modelToResponse(*m)
	writeJSON(w, resp)
}

func (h *Handler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	id, ok := parseUUIDParam(w, r, "id", "model ID")
	if !ok {
		return
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())
	if err := modelRepo.DeleteByID(r.Context(), id); err != nil {
		respondError(w, "failed to delete model", err, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type TestModelResponse struct {
	Success    bool   `json:"success"`
	TTFTMs     *int64 `json:"ttft_ms,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Response   string `json:"response"`
	Error      string `json:"error,omitempty"`
}

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

	targetURL := util.SanitizeBaseURL(prov.BaseURL) + "/chat/completions"
	proxyReq, _ := http.NewRequestWithContext(r.Context(), "POST", targetURL, bytes.NewReader(bodyBytes))
	proxyReq.Header.Set("Authorization", "Bearer "+apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")

	reqHashBytes := make([]byte, 8)
	rand.Read(reqHashBytes)
	reqHash := hex.EncodeToString(reqHashBytes)

	startRequest := time.Now()
	testClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := testClient.Do(proxyReq)
	if err != nil {
		logQuery := `
			INSERT INTO request_logs (
				provider_id, model_id, request_id, request_hash, status_code,
				latency_ms, duration_ms, ttft_ms,
				proxy_overhead_ms, parse_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms,
				error_message, streaming, virtual_key_name, virtual_key_id, failover_attempt, state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		`
		durationMs := float64(time.Since(start).Milliseconds())
		_, logErr := h.dbPool.Pool().Exec(r.Context(), logQuery,
			m.ProviderID, m.ModelID, reqHash, reqHash, 502,
			durationMs, durationMs, 0,
			proxyOverheadMs, 0, 0, 0, keyDecryptMs,
			err.Error(), false, "internal", nil, 0, "failed",
		)
		if logErr != nil {
			log.Printf("[admin] error: TestModel log insert failed: %v", logErr)
		}

		writeJSON(w, TestModelResponse{Error: err.Error()})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	duration := time.Since(startRequest).Milliseconds()

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}

		logQuery := `
			INSERT INTO request_logs (
				provider_id, model_id, request_id, request_hash, status_code,
				latency_ms, duration_ms, ttft_ms,
				proxy_overhead_ms, parse_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms,
				error_message, tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, virtual_key_id, failover_attempt, state
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
		`
		durationMs := float64(duration)
		_, logErr := h.dbPool.Pool().Exec(r.Context(), logQuery,
			m.ProviderID, m.ModelID, reqHash, reqHash, resp.StatusCode,
			durationMs, durationMs, 0,
			proxyOverheadMs, 0, 0, 0, keyDecryptMs,
			errMsg, 0, 0, 0, false, "internal", nil, 0, "failed",
		)
		if logErr != nil {
			log.Printf("[admin] error: TestModel log insert failed: %v", logErr)
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
	_ = json.Unmarshal(respBody, &chatResp)

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
			provider_id, model_id, request_id, request_hash, status_code,
			latency_ms, duration_ms, ttft_ms,
			proxy_overhead_ms, parse_ms, model_lookup_ms, provider_lookup_ms, key_decrypt_ms,
			tokens_per_second, tokens_prompt, tokens_completion, streaming, virtual_key_name, virtual_key_id, failover_attempt, state
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
	`
	durationMs := float64(duration)
	// For a non-streaming test request, TTFT = total duration because there is
	// no separate streaming phase. Mark ttft_ms = 0 in the log to indicate
	// this was not a streaming request and TTFT is not meaningful.
	_, logErr := h.dbPool.Pool().Exec(r.Context(), logQuery,
		m.ProviderID, m.ModelID, reqHash, reqHash, resp.StatusCode,
		durationMs, durationMs, 0,
		proxyOverheadMs, 0, 0, 0, keyDecryptMs,
		tps, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, false, "internal", nil, 0, "completed",
	)
	if logErr != nil {
		log.Printf("[admin] error: TestModel log insert failed: %v", logErr)
	}

	writeJSON(w, TestModelResponse{
		Success:    true,
		DurationMs: duration,
		Response:   content,
	})
}
