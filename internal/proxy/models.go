package proxy

import (
	"encoding/json"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ListModels returns all available models in OpenAI-compatible format.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.modelRepo.ListEnabled(r.Context())
	if err != nil {
		debuglog.Error("proxy: failed to list models", "error", err)
		writeOpenAIError(w, "failed to list models", http.StatusInternalServerError)
		return
	}

	openAIModels := make([]map[string]any, 0, len(models))
	for _, m := range models {
		modelID := provider.NormalizeName(m.ProviderName) + "/" + m.ModelID
		openAIModels = append(openAIModels, modelToOpenAIItem(m, modelID, m.ProviderName))
	}

	groups, err := h.failoverRepo.GetEnabled(r.Context())
	if err != nil {
		debuglog.Warn("proxy: failed to list failover groups", "error", err)
	} else {
		for _, g := range groups {
			for _, modelUUID := range g.PriorityOrder {
				entryEnabled := true
				if val, ok := g.EntryEnabled[modelUUID.String()]; ok {
					entryEnabled = val
				}
				if !entryEnabled {
					continue
				}

				m, err := h.modelRepo.Get(r.Context(), modelUUID)
				if err != nil || !m.Enabled || !m.ProviderEnabled {
					continue
				}

				openAIModels = append(openAIModels, modelToOpenAIItem(m, "hotel/"+g.DisplayModel, "hotel"))
				break
			}
		}
	}

	response := map[string]any{
		"object": "list",
		"data":   openAIModels,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		debuglog.Error("proxy: failed to encode models response", "error", err)
	}
}

// modelToOpenAIItem builds an OpenAI-compatible model object from a model entity.
func modelToOpenAIItem(m *model.Model, id, providerName string) map[string]any {
	ownedBy := m.OwnedBy
	if ownedBy == "" {
		ownedBy = m.ProviderName
	}

	item := map[string]any{
		"id":       id,
		"object":   "model",
		"created":  m.CreatedAt.Unix(),
		"owned_by": ownedBy,
		"provider": providerName,
	}

	if m.ContextLength != nil {
		item["context_length"] = *m.ContextLength
		item["max_context_length"] = *m.ContextLength
	}
	if m.MaxOutputTokens != nil {
		item["max_output_tokens"] = *m.MaxOutputTokens
	}
	if m.DisplayName != "" {
		item["name"] = m.DisplayName
	} else if m.Name != "" {
		item["name"] = m.Name
	}
	if m.Description != "" {
		item["description"] = m.Description
	}
	if m.Modality != "" {
		item["modality"] = m.Modality
	}
	if m.Capabilities != "" && m.Capabilities != "{}" {
		var caps map[string]any
		if err := json.Unmarshal([]byte(m.Capabilities), &caps); err == nil {
			item["capabilities"] = caps
		} else {
			debuglog.Warn("proxy: invalid capabilities JSON in model", "model", m.ModelID, "error", err)
		}
	}
	if m.InputModalities != "" && m.InputModalities != "[]" {
		var modalities []string
		if err := json.Unmarshal([]byte(m.InputModalities), &modalities); err == nil {
			item["input_modalities"] = modalities
		} else {
			debuglog.Warn("proxy: invalid input_modalities JSON in model", "model", m.ModelID, "error", err)
		}
	}
	if m.OutputModalities != "" && m.OutputModalities != "[]" {
		var modalities []string
		if err := json.Unmarshal([]byte(m.OutputModalities), &modalities); err == nil {
			item["output_modalities"] = modalities
		} else {
			debuglog.Warn("proxy: invalid output_modalities JSON in model", "model", m.ModelID, "error", err)
		}
	}
	if m.InputPricePerMillion != nil {
		item["input_price_per_million"] = *m.InputPricePerMillion
	}
	if m.OutputPricePerMillion != nil {
		item["output_price_per_million"] = *m.OutputPricePerMillion
	}

	return item
}
