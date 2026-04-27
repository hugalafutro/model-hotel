package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/user/llm-proxy/internal/auth"
)

type resolveTimings struct {
	modelLookupMs    float64
	providerLookupMs float64
	keyDecryptMs     float64
}

func (h *Handler) resolveHotelModel(ctx context.Context, displayModel string) ([]modelCandidate, resolveTimings, error) {
	var t resolveTimings
	modelLookupStart := time.Now()

	fg, err := h.failoverRepo.GetByModel(ctx, displayModel)
	if err != nil {
		return nil, t, err
	}

	if !fg.GroupEnabled {
		return nil, t, fmt.Errorf("failover group disabled")
	}

	if len(fg.PriorityOrder) == 0 {
		return nil, t, fmt.Errorf("no entries in failover group")
	}

	t.modelLookupMs = float64(time.Since(modelLookupStart).Microseconds()) / 1000.0

	providerLookupStart := time.Now()
	var keyDecryptTotal float64
	candidates := make([]modelCandidate, 0, len(fg.PriorityOrder))
	for _, modelUUID := range fg.PriorityOrder {
		entryEnabled := true
		if val, ok := fg.EntryEnabled[modelUUID.String()]; ok {
			entryEnabled = val
		}
		if !entryEnabled {
			continue
		}

		m, err := h.modelRepo.Get(ctx, modelUUID)
		if err != nil || !m.Enabled || !m.ProviderEnabled {
			continue
		}
		prov, err := h.providerRepo.Get(ctx, m.ProviderID)
		if err != nil || !prov.Enabled {
			continue
		}
		kdStart := time.Now()
		apiKey, err := auth.DecryptCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
		keyDecryptTotal += float64(time.Since(kdStart).Microseconds()) / 1000.0
		if err != nil {
			continue
		}
		candidates = append(candidates, modelCandidate{model: m, provider: prov, apiKey: apiKey})
	}

	t.providerLookupMs = float64(time.Since(providerLookupStart).Microseconds())/1000.0 - keyDecryptTotal
	t.keyDecryptMs = keyDecryptTotal
	return candidates, t, nil
}

func (h *Handler) resolveSpecificProvider(ctx context.Context, providerName, modelID string) ([]modelCandidate, resolveTimings, error) {
	var t resolveTimings
	providerLookupStart := time.Now()

	prov, err := h.providerRepo.GetByName(ctx, providerName)
	if err != nil {
		return nil, t, fmt.Errorf("provider not found: %s", providerName)
	}

	t.providerLookupMs = float64(time.Since(providerLookupStart).Microseconds()) / 1000.0

	modelLookupStart := time.Now()
	m, err := h.modelRepo.GetByProviderAndModelID(ctx, prov.ID, modelID)
	if err != nil {
		return nil, t, fmt.Errorf("model not found: %s on provider %s", modelID, providerName)
	}
	t.modelLookupMs = float64(time.Since(modelLookupStart).Microseconds()) / 1000.0

	if !m.Enabled || !prov.Enabled {
		return nil, t, fmt.Errorf("model or provider disabled")
	}

	kdStart := time.Now()
	apiKey, err := auth.DecryptCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
	t.keyDecryptMs = float64(time.Since(kdStart).Microseconds()) / 1000.0
	if err != nil {
		return nil, t, err
	}

	return []modelCandidate{{model: m, provider: prov, apiKey: apiKey}}, t, nil
}

func (h *Handler) shouldFailover(statusCode int) bool {
	if statusCode >= 500 {
		return true
	}
	if statusCode == 429 {
		return h.settingsRepo.GetBool(context.Background(), "failover_on_rate_limit", true)
	}
	if statusCode == 401 || statusCode == 403 {
		return true
	}
	return false
}
