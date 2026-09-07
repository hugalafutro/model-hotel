package proxy

import (
	"cmp"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ListModels returns the models this virtual key can actually call, in
// OpenAI-compatible format.
//
// The catalogue is scoped by the same pair a chat request is filtered by: the
// key's own allowed_providers intersected with its owner account's cap
// (effectiveAllowedProviders, then the candidate filter in resolveCandidates).
// ProxyKeyMiddleware runs on every /v1 route including this one and always
// publishes the key's own list, but it publishes the OWNER cap only inside its
// `if vk.Owner != nil` block, so that value is simply absent for an unowned
// key. The comma-ok reads below turn that absence into a nil cap, which
// effectiveAllowedProviders already reads as "this side restricts nothing" —
// the same shape /api/chat/* relies on from the other direction, where the key
// side is the missing one. An unrestricted caller produces a nil effective
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
	// The enabled catalogue, keyed for the failover-group walk below: ListEnabled
	// already returned exactly the rows a group entry may serve
	// (model.enabled AND provider.enabled), so an entry not in here is skipped
	// without a per-entry round trip.
	byID := make(map[uuid.UUID]*model.Model, len(models))
	for _, m := range models {
		byID[m.ID] = m
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
				if !g.IsEntryEnabled(modelUUID) {
					continue
				}
				m, ok := byID[modelUUID]
				if !ok {
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
	item := map[string]any{
		"id":       id,
		"object":   "model",
		"created":  m.CreatedAt.Unix(),
		"owned_by": cmp.Or(m.OwnedBy, m.ProviderName),
		"provider": providerName,
	}

	if m.ContextLength != nil {
		item["context_length"] = *m.ContextLength
		item["max_context_length"] = *m.ContextLength
	}
	if m.MaxOutputTokens != nil {
		item["max_output_tokens"] = *m.MaxOutputTokens
	}
	if name := cmp.Or(m.DisplayName, m.Name); name != "" {
		item["name"] = name
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
	// Same rule declaredModalities encodes, kept inline here because an
	// unreadable column is worth a log line on the catalog surface.
	for _, col := range []struct{ key, raw string }{
		{"input_modalities", m.InputModalities},
		{"output_modalities", m.OutputModalities},
	} {
		if col.raw == "" || col.raw == "[]" {
			continue
		}
		var modalities []string
		if err := json.Unmarshal([]byte(col.raw), &modalities); err == nil {
			item[col.key] = modalities
		} else {
			debuglog.Warn("proxy: invalid modalities JSON in model", "column", col.key, "model", m.ModelID, "error", err)
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
