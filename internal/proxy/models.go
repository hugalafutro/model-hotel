package proxy

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.modelRepo.ListEnabled(r.Context())
	if err != nil {
		log.Printf("[proxy] error: failed to list models: %v", err)
		http.Error(w, "failed to list models", http.StatusInternalServerError)
		return
	}

	openAIModels := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		ownedBy := m.OwnedBy
		if ownedBy == "" {
			ownedBy = m.ProviderName
		}

		modelID := provider.NormalizeName(m.ProviderName) + "/" + m.ModelID

		item := map[string]interface{}{
			"id":       modelID,
			"object":   "model",
			"created":  m.CreatedAt.Unix(),
			"owned_by": ownedBy,
			"provider": m.ProviderName,
		}

		if m.ContextLength != nil {
			item["context_length"] = *m.ContextLength
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
		if m.InputPricePerMillion != nil {
			item["input_price_per_million"] = *m.InputPricePerMillion
		}
		if m.OutputPricePerMillion != nil {
			item["output_price_per_million"] = *m.OutputPricePerMillion
		}

		openAIModels = append(openAIModels, item)
	}

	groups, err := h.failoverRepo.GetEnabled(r.Context())
	if err != nil {
		log.Printf("[proxy] warning: failed to list failover groups: %v", err)
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

				ownedBy := m.OwnedBy
				if ownedBy == "" {
					ownedBy = m.ProviderName
				}

				item := map[string]interface{}{
					"id":       "hotel/" + g.DisplayModel,
					"object":   "model",
					"created":  m.CreatedAt.Unix(),
					"owned_by": ownedBy,
					"provider": "hotel",
				}

				if m.ContextLength != nil {
					item["context_length"] = *m.ContextLength
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
				if m.InputPricePerMillion != nil {
					item["input_price_per_million"] = *m.InputPricePerMillion
				}
				if m.OutputPricePerMillion != nil {
					item["output_price_per_million"] = *m.OutputPricePerMillion
				}

				openAIModels = append(openAIModels, item)
				break
			}
		}
	}

	response := map[string]interface{}{
		"object": "list",
		"data":   openAIModels,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("[proxy] error: failed to encode models response: %v", err)
	}
}
