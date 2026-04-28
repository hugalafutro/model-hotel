package proxy

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
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
		log.Printf("[resolve] warning: failover group disabled for model=%s", displayModel)
		return nil, t, fmt.Errorf("failover group disabled")
	}

	if len(fg.PriorityOrder) == 0 {
		log.Printf("[resolve] warning: empty failover group for model=%s", displayModel)
		return nil, t, fmt.Errorf("no entries in failover group")
	}

	t.modelLookupMs = float64(time.Since(modelLookupStart).Microseconds()) / 1000.0

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
			log.Printf("[resolve] skipping candidate: model not found, id=%s", modelUUID)
			continue
		}
		if !m.Enabled {
			log.Printf("[resolve] skipping candidate: model disabled, model=%s", m.ModelID)
			continue
		}
		if !m.ProviderEnabled {
			log.Printf("[resolve] skipping candidate: provider disabled, model=%s", m.ModelID)
			continue
		}
		prov, ok := providers[m.ProviderID]
		if !ok {
			log.Printf("[resolve] skipping candidate: provider not found, provider_id=%s model=%s", m.ProviderID, m.ModelID)
			continue
		}
		if !prov.Enabled {
			log.Printf("[resolve] skipping candidate: provider disabled, provider=%s model=%s", prov.Name, m.ModelID)
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
			keyDecryptTotal += float64(time.Since(kdStart).Microseconds()) / 1000.0
			if err != nil {
				log.Printf("[resolve] error: key decryption failed for provider=%s: %v", prov.Name, err)
				continue
			}
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

	if !m.Enabled {
		log.Printf("[resolve] model disabled: model=%s provider=%s", modelID, providerName)
	}
	if !prov.Enabled {
		log.Printf("[resolve] provider disabled: provider=%s model=%s", providerName, modelID)
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
		if err != nil {
			log.Printf("[resolve] error: key decryption failed for provider=%s: %v", prov.Name, err)
			return nil, t, err
		}
	}

	return []modelCandidate{{model: m, provider: prov, apiKey: apiKey}}, t, nil
}

func (h *Handler) shouldFailover(ctx context.Context, statusCode int) bool {
	if statusCode >= 500 {
		log.Printf("[resolve] failover decision: status=%d (5xx → failover)", statusCode)
		return true
	}
	if statusCode == 429 {
		enabled := h.settingsRepo.GetBool(ctx, "failover_on_rate_limit", true)
		log.Printf("[resolve] failover decision: status=429 rate_limit_failover=%v", enabled)
		return enabled
	}
	if statusCode == 401 || statusCode == 403 {
		log.Printf("[resolve] failover decision: status=%d (auth error → failover)", statusCode)
		return true
	}
	return false
}
