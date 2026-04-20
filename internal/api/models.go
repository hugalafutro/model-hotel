package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/model"
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