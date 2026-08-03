package proxy

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// providerAllowFunc turns a caller's effective provider allow-list into a
// membership test over provider ids. A nil list is the unrestricted case and
// admits every provider; a non-nil list admits exactly its members, so an empty
// one admits none. The test is the list's PRESENCE, never its length.
func providerAllowFunc(allowed *[]string) func(uuid.UUID) bool {
	if allowed == nil {
		return func(uuid.UUID) bool { return true }
	}
	set := make(map[string]struct{}, len(*allowed))
	for _, id := range *allowed {
		set[id] = struct{}{}
	}
	return func(id uuid.UUID) bool {
		_, ok := set[id.String()]
		return ok
	}
}

// ListModels returns the models this virtual key can actually call, in
// OpenAI-compatible format.
//
// The catalogue is scoped by the same pair a chat request is filtered by: the
// key's own allowed_providers intersected with its owner account's cap
// (effectiveAllowedProviders, then the candidate filter in resolveCandidates).
// Both context values are populated by ProxyKeyMiddleware, which runs on every
// /v1 route including this one. An unrestricted caller produces a nil effective
// list and still sees the whole catalogue, so this narrows the response only
// for a key an operator has deliberately restricted, and what it hides is
// exactly what would have come back as a 403.
func (h *Handler) ListModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.modelRepo.ListEnabled(r.Context())
	if err != nil {
		debuglog.Error("proxy: failed to list models", "error", err)
		writeOpenAIError(w, "failed to list models", http.StatusInternalServerError)
		return
	}

	keyAllowed, _ := r.Context().Value(ctxkeys.VirtualKeyAllowedProvidersKey).(*[]string)
	ownerAllowed, _ := r.Context().Value(ctxkeys.UserAllowedProvidersKey).(*[]string)
	providerAllowed := providerAllowFunc(effectiveAllowedProviders(keyAllowed, ownerAllowed))

	openAIModels := make([]map[string]any, 0, len(models))
	for _, m := range models {
		if !providerAllowed(m.ProviderID) {
			continue
		}
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
				if !providerAllowed(m.ProviderID) {
					// Keep walking the priority order rather than dropping the
					// group here. A request naming the group is filtered
					// candidate by candidate, so the group stays reachable while
					// ANY entry sits on a provider this caller may use, and the
					// first such entry is the one that would serve it. Only a
					// group with no reachable entry at all falls out.
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
