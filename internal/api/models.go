package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/util"
)

type ModelResponse struct {
	ID                    string  `json:"id"`
	ModelID               string  `json:"model_id"`
	Name                  string  `json:"name"`
	Description           string  `json:"description"`
	DisplayName           string  `json:"display_name"`
	ProviderID           string  `json:"provider_id"`
	ProviderName          string  `json:"provider_name"`
	Capabilities          string  `json:"capabilities"`
	Params                string  `json:"params"`
	Modality              string  `json:"modality"`
	InputModalities       string  `json:"input_modalities"`
	OutputModalities      string  `json:"output_modalities"`
	ContextLength         *int    `json:"context_length"`
	MaxOutputTokens       *int    `json:"max_output_tokens"`
	InputPricePerMillion  *float64 `json:"input_price_per_million"`
	OutputPricePerMillion *float64 `json:"output_price_per_million"`
	OwnedBy               string  `json:"owned_by"`
	Enabled               bool    `json:"enabled"`
	CreatedAt             string  `json:"created_at"`
	LastSeenAt            string  `json:"last_seen_at"`
}

func (h *Handler) RegisterModels(r chi.Router) {
	r.Route("/models", func(r chi.Router) {
		r.Get("/", h.ListModels)
		r.Patch("/{id}", h.UpdateModel)
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	responses := make([]ModelResponse, len(models))
	for i, m := range models {
		responses[i] = ModelResponse{
			ID:                    m.ID.String(),
			ModelID:               m.ModelID,
			Name:                  m.Name,
			Description:           m.Description,
			DisplayName:           m.DisplayName,
			ProviderID:            m.ProviderID.String(),
			ProviderName:          m.ProviderName,
			Capabilities:          m.Capabilities,
			Params:                m.Params,
			Modality:              m.Modality,
			InputModalities:       m.InputModalities,
			OutputModalities:      m.OutputModalities,
			ContextLength:         m.ContextLength,
			MaxOutputTokens:       m.MaxOutputTokens,
			InputPricePerMillion:  m.InputPricePerMillion,
			OutputPricePerMillion: m.OutputPricePerMillion,
			OwnedBy:               m.OwnedBy,
			Enabled:               m.Enabled,
			CreatedAt:             m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			LastSeenAt:            m.LastSeenAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responses)
}

func (h *Handler) UpdateModel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid model ID", http.StatusBadRequest)
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	modelRepo := model.NewRepository(h.dbPool.Pool())

	if req.Enabled != nil {
		m, err := modelRepo.SetEnabled(r.Context(), id, *req.Enabled)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		resp := ModelResponse{
			ID:                    m.ID.String(),
			ModelID:               m.ModelID,
			Name:                  m.Name,
			Description:           m.Description,
			DisplayName:           m.DisplayName,
			ProviderID:            m.ProviderID.String(),
			ProviderName:          m.ProviderName,
			Capabilities:          m.Capabilities,
			Params:                m.Params,
			Modality:              m.Modality,
			InputModalities:       m.InputModalities,
			OutputModalities:      m.OutputModalities,
			ContextLength:         m.ContextLength,
			MaxOutputTokens:       m.MaxOutputTokens,
			InputPricePerMillion:  m.InputPricePerMillion,
			OutputPricePerMillion: m.OutputPricePerMillion,
			OwnedBy:               m.OwnedBy,
			Enabled:               m.Enabled,
			CreatedAt:             m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			LastSeenAt:            m.LastSeenAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	http.Error(w, "no fields to update", http.StatusBadRequest)
}

type TestModelResponse struct {
	Success    bool    `json:"success"`
	TTFTMs     int64   `json:"ttft_ms"`
	DurationMs int64   `json:"duration_ms"`
	Response   string  `json:"response"`
	Error      string  `json:"error,omitempty"`
}

func (h *Handler) TestModel(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid model ID", http.StatusBadRequest)
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

	prov, err := h.db.Get(r.Context(), m.ProviderID)
	if err != nil {
		http.Error(w, "provider not found", http.StatusInternalServerError)
		return
	}

	apiKey, err := auth.Decrypt(prov.EncryptedKey, prov.KeyNonce, h.cfg.MasterKey)
	if err != nil {
		http.Error(w, "failed to decrypt API key", http.StatusInternalServerError)
		return
	}

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

	start := time.Now()
	resp, err := http.DefaultClient.Do(proxyReq)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TestModelResponse{Error: err.Error()})
		return
	}
	defer resp.Body.Close()
	duration := time.Since(start).Milliseconds()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(respBody))
		if len(errMsg) > 300 {
			errMsg = errMsg[:300]
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(TestModelResponse{DurationMs: duration, Error: errMsg})
		return
	}

	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	json.Unmarshal(respBody, &chatResp)

	content := ""
	if len(chatResp.Choices) > 0 {
		content = chatResp.Choices[0].Message.Content
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(TestModelResponse{
		Success:    true,
		TTFTMs:     duration,
		DurationMs: duration,
		Response:   content,
	})
}