package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

type resolveTimings struct {
	failoverLookupMs float64
	modelLookupMs    float64
	providerLookupMs float64
	keyDecryptMs     float64
	dialMs           float64
	settingsReadMs   float64
}

// proxyOverheadMs returns the total proxy overhead from accumulated timings.
// dialMs may be 0 (before the failover loop) or populated after each dial.
func (t resolveTimings) proxyOverheadMs(parseMs float64) float64 {
	return parseMs + t.failoverLookupMs + t.modelLookupMs + t.providerLookupMs + t.keyDecryptMs + t.dialMs + t.settingsReadMs
}

func (h *Handler) resolveHotelModel(ctx context.Context, displayModel string) ([]modelCandidate, resolveTimings, error) {
	debuglog.Debug("resolve: resolving hotel model", "model", displayModel)
	var t resolveTimings
	modelLookupStart := time.Now()

	fg, err := h.failoverRepo.GetByModel(ctx, displayModel)
	if err != nil {
		return nil, t, err
	}

	if !fg.GroupEnabled {
		debuglog.Warn("resolve: failover group disabled", "model", displayModel)
		return nil, t, fmt.Errorf("failover group disabled")
	}

	if len(fg.PriorityOrder) == 0 {
		debuglog.Warn("resolve: empty failover group", "model", displayModel)
		return nil, t, fmt.Errorf("no entries in failover group")
	}

	t.failoverLookupMs = float64(time.Since(modelLookupStart).Microseconds()) / 1000.0
	debuglog.Debug("resolve: failover group found", "model", displayModel, "entries", len(fg.PriorityOrder), "enabled", fg.GroupEnabled)

	providerLookupStart := time.Now()
	var keyDecryptTotal float64

	// Collect enabled model UUIDs for batch lookup
	enabledModelIDs := make([]uuid.UUID, 0, len(fg.PriorityOrder))
	for _, modelUUID := range fg.PriorityOrder {
		entryEnabled := true
		if val, ok := fg.EntryEnabled[modelUUID.String()]; ok {
			entryEnabled = val
		}
		if entryEnabled {
			enabledModelIDs = append(enabledModelIDs, modelUUID)
		}
	}

	models, err := h.modelRepo.GetByIDs(ctx, enabledModelIDs)
	if err != nil {
		return nil, t, err
	}

	// Collect unique provider IDs for batch lookup
	providerIDSet := make(map[uuid.UUID]struct{})
	for _, modelUUID := range enabledModelIDs {
		if m, ok := models[modelUUID]; ok && m.Enabled && m.ProviderEnabled {
			providerIDSet[m.ProviderID] = struct{}{}
		}
	}
	providerIDs := make([]uuid.UUID, 0, len(providerIDSet))
	for pid := range providerIDSet {
		providerIDs = append(providerIDs, pid)
	}

	providers, err := h.providerRepo.GetByIDs(ctx, providerIDs)
	if err != nil {
		return nil, t, err
	}

	candidates := make([]modelCandidate, 0, len(fg.PriorityOrder))
	var decryptFailures int
	debuglog.Debug("resolve: building candidates from failover group", "model", displayModel, "priority_order_count", len(fg.PriorityOrder))
	for _, modelUUID := range fg.PriorityOrder {
		entryEnabled := true
		if val, ok := fg.EntryEnabled[modelUUID.String()]; ok {
			entryEnabled = val
		}
		if !entryEnabled {
			continue
		}

		m, ok := models[modelUUID]
		if !ok {
			debuglog.Info("resolve: skipping candidate: model not found", "id", modelUUID)
			continue
		}
		if !m.Enabled {
			debuglog.Info("resolve: skipping candidate: model disabled", "model", m.ModelID)
			continue
		}
		if !m.ProviderEnabled {
			debuglog.Info("resolve: skipping candidate: provider disabled", "model", m.ModelID)
			continue
		}
		prov, ok := providers[m.ProviderID]
		if !ok {
			debuglog.Info("resolve: skipping candidate: provider not found", "provider_id", m.ProviderID, "model", m.ModelID)
			continue
		}
		if !prov.Enabled {
			debuglog.Info("resolve: skipping candidate: provider disabled", "provider", prov.Name, "model", m.ModelID)
			continue
		}

		// Circuit breaker: skip providers that are in the open state.
		cbStart := time.Now()
		cbEnabled := h.settingsRepo.GetBool(ctx, "circuit_breaker_enabled", true)
		ctxkeys.AddSettingsReadMs(ctx, cbStart)
		if cbEnabled && h.circuitBreaker.IsOpen(prov.ID) {
			debuglog.Info("resolve: skipping candidate: circuit breaker open", "provider", prov.Name, "model", m.ModelID)
			continue
		}
		// Keyless providers store nil encrypted key bytes — skip decryption.
		var apiKey string
		if len(prov.EncryptedKey) == 0 {
			apiKey = ""
		} else {
			var err error
			kdStart := time.Now()
			apiKey, err = auth.DecryptCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
			kdMs := float64(time.Since(kdStart).Microseconds()) / 1000.0
			keyDecryptTotal += kdMs
			debuglog.Debug("resolve: key decrypted", "provider", prov.Name, "model", m.ModelID, "decrypt_ms", kdMs)
			if err != nil {
				debuglog.Error("resolve: key decryption failed", "provider", prov.Name, "model", m.ModelID, "entry", modelUUID, "error", err)
				decryptFailures++
				continue
			}
		}
		candidates = append(candidates, modelCandidate{model: m, provider: prov, apiKey: apiKey})
	}

	t.providerLookupMs = max(0, float64(time.Since(providerLookupStart).Microseconds())/1000.0-keyDecryptTotal)
	t.keyDecryptMs = keyDecryptTotal
	if len(candidates) == 0 && decryptFailures > 0 {
		return nil, t, fmt.Errorf("all %d candidate(s) failed key decryption (wrong master key?)", decryptFailures)
	}
	debuglog.Debug("resolve: hotel model resolved", "model", displayModel, "candidates", len(candidates), "decrypt_failures", decryptFailures)
	return candidates, t, nil
}

func (h *Handler) resolveSpecificProvider(ctx context.Context, providerName, modelID string) ([]modelCandidate, resolveTimings, error) {
	debuglog.Debug("resolve: resolving specific provider", "provider", providerName, "model", modelID)
	var t resolveTimings
	providerLookupStart := time.Now()

	prov, err := h.providerRepo.GetByName(ctx, providerName)
	if err != nil {
		debuglog.Warn("resolve: provider not found", "provider", providerName, "error", err)
		return nil, t, fmt.Errorf("provider not found: %s", providerName)
	}
	debuglog.Debug("resolve: provider found", "provider", prov.Name, "provider_id", prov.ID, "enabled", prov.Enabled)

	t.providerLookupMs = float64(time.Since(providerLookupStart).Microseconds()) / 1000.0

	modelLookupStart := time.Now()
	m, err := h.modelRepo.GetByProviderAndModelID(ctx, prov.ID, modelID)
	if err != nil {
		debuglog.Warn("resolve: model not found", "model", modelID, "provider", providerName, "error", err)
		return nil, t, fmt.Errorf("model not found: %s on provider %s", modelID, providerName)
	}
	debuglog.Debug("resolve: model found", "model", m.ModelID, "provider", prov.Name, "enabled", m.Enabled, "provider_enabled", m.ProviderEnabled)
	t.modelLookupMs = float64(time.Since(modelLookupStart).Microseconds()) / 1000.0

	if !m.Enabled {
		debuglog.Info("resolve: model disabled", "model", modelID, "provider", providerName)
	}
	if !prov.Enabled {
		debuglog.Info("resolve: provider disabled", "provider", providerName, "model", modelID)
	}
	if !m.Enabled || !prov.Enabled {
		return nil, t, fmt.Errorf("model or provider disabled")
	}

	// Keyless providers (e.g. OpenCode Zen free models) store nil encrypted
	// key bytes. When the key is empty, skip decryption and use empty string.
	var apiKey string
	if len(prov.EncryptedKey) == 0 {
		apiKey = ""
	} else {
		var err error
		kdStart := time.Now()
		apiKey, err = auth.DecryptCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
		t.keyDecryptMs = float64(time.Since(kdStart).Microseconds()) / 1000.0
		debuglog.Debug("resolve: key decrypted", "provider", prov.Name, "model", modelID, "decrypt_ms", t.keyDecryptMs)
		if err != nil {
			debuglog.Error("resolve: key decryption failed", "provider", prov.Name, "model", modelID, "error", err)
			return nil, t, err
		}
	}

	debuglog.Debug("resolve: specific provider resolved", "provider", prov.Name, "model", m.ModelID, "id", m.ID, "has_api_key", apiKey != "")
	return []modelCandidate{{model: m, provider: prov, apiKey: apiKey}}, t, nil
}

func (h *Handler) shouldFailover(ctx context.Context, statusCode int) bool {
	if statusCode >= 500 {
		return true
	}
	if statusCode == 429 {
		sStart := time.Now()
		enabled := h.settingsRepo.GetBool(ctx, "failover_on_rate_limit", true)
		ctxkeys.AddSettingsReadMs(ctx, sStart)
		return enabled
	}
	if statusCode == 401 || statusCode == 403 {
		return true
	}
	// 404 from a provider means the model doesn't exist there (stale DB entry,
	// overloaded provider returning not_found, etc.) — try the next candidate.
	if statusCode == 404 {
		return true
	}
	return false
}
