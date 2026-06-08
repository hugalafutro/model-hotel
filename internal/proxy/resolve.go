package proxy

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

type resolveTimings struct {
	failoverLookupMs float64
	modelLookupMs    float64
	providerLookupMs float64
	keyDecryptMs     float64
	dialMs           float64
	settingsReadMs   float64
}

// resolveCacheHits tracks whether each overhead component hit a prewarmed cache.
// true = cache hit (fast, prewarmed); false = cache miss (had to compute/DB read).
// Absent fields (parse, dial) are not applicable — they have no cache.
// The canonical definition lives in internal/util.CacheHits so both proxy and
// api packages can reference a single type.
type resolveCacheHits = util.CacheHits

// proxyOverheadMs returns the total proxy overhead from accumulated timings.
// dialMs may be 0 (before the failover loop) or populated after each dial.
func (t resolveTimings) proxyOverheadMs(parseMs float64) float64 {
	return parseMs + t.failoverLookupMs + t.modelLookupMs + t.providerLookupMs + t.keyDecryptMs + t.dialMs + t.settingsReadMs
}

func (h *Handler) resolveHotelModel(ctx context.Context, displayModel string) ([]modelCandidate, resolveTimings, resolveCacheHits, error) {
	debuglog.Debug("resolve: resolving hotel model", "model", displayModel)
	var t resolveTimings
	var ch resolveCacheHits

	// A — failover-group lookup + validation. Stamp ch.Failover / failoverLookupMs
	// only after success so the early-error ledger leaves them zero.
	failoverLookupStart := time.Now()
	fg, failoverHit, err := h.lookupFailoverGroup(ctx, displayModel)
	if err != nil {
		return nil, t, ch, err
	}
	ch.Failover = &failoverHit
	t.failoverLookupMs = float64(time.Since(failoverLookupStart).Microseconds()) / 1000.0
	debuglog.Debug("resolve: failover group found", "model", displayModel, "entries", len(fg.PriorityOrder), "enabled", fg.GroupEnabled)

	// B — enabled-model collection + batch model lookup.
	modelLookupStart := time.Now()
	enabledModelIDs := enabledEntryIDs(fg)
	ch.Model = batchCacheHit(enabledModelIDs, model.IsCachedByUUID)
	models, err := h.modelRepo.GetByIDs(ctx, enabledModelIDs)
	if err != nil {
		return nil, t, ch, err
	}
	t.modelLookupMs = float64(time.Since(modelLookupStart).Microseconds()) / 1000.0

	// C — provider collection + batch provider lookup. providerLookupStart opens
	// the window that physically contains the settings read (D) and every key
	// decrypt (E); providerLookupMs is derived by subtracting those below.
	providerLookupStart := time.Now()
	var settingsReadInWindow float64

	providerIDs := providerIDsFor(enabledModelIDs, models)
	ch.Provider = batchCacheHit(providerIDs, provider.IsCachedByID)
	providers, err := h.providerRepo.GetByIDs(ctx, providerIDs)
	if err != nil {
		return nil, t, ch, err
	}

	// D — read circuit_breaker_enabled once before the loop to avoid
	// per-candidate settings reads. The single elapsed sample is accounted for
	// in both settingsReadMs (via the context accumulator) and
	// settingsReadInWindow (subtracted from providerLookupMs below) — keep both
	// adds here, off one pre-computed cbElapsed, so the dual accounting stays
	// visible in one place. Only track settings actually read during resolve;
	// failover_on_rate_limit is read later in shouldFailover (outside the resolve
	// phase) so checking it here would show a false amber for requests that
	// never trigger 429.
	cbEnabled, settingsHit, cbElapsed := h.readCircuitBreakerFlag(ctx)
	settingsReadInWindow += cbElapsed
	if v := ctx.Value(ctxkeys.SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			*p += cbElapsed
		}
	}
	ch.Settings = &settingsHit

	// E — candidate-build loop (no window math inside; it only returns the
	// running key-decrypt total the caller subtracts).
	debuglog.Debug("resolve: building candidates from failover group", "model", displayModel, "priority_order_count", len(fg.PriorityOrder))
	candidates, keyDecryptTotal, decryptFailures, keyHit := h.buildFailoverCandidates(fg, models, providers, cbEnabled)

	// F — finalize timings + terminal error.
	// Only record key cache hit if there were keys to decrypt.
	if keyDecryptTotal > 0 {
		ch.Key = &keyHit
	}
	t.providerLookupMs = max(0, float64(time.Since(providerLookupStart).Microseconds())/1000.0-keyDecryptTotal-settingsReadInWindow)
	t.keyDecryptMs = keyDecryptTotal
	if len(candidates) == 0 && decryptFailures > 0 {
		return nil, t, ch, fmt.Errorf("all %d candidate(s) failed key decryption (wrong master key?)", decryptFailures)
	}
	debuglog.Debug("resolve: hotel model resolved", "model", displayModel, "candidates", len(candidates), "decrypt_failures", decryptFailures)
	return candidates, t, ch, nil
}

// lookupFailoverGroup performs phase A: the failover cache probe, the repo
// lookup, and the group-enabled / non-empty validations. It returns the group
// and the cache-hit bool but does NOT stamp ch.Failover / failoverLookupMs —
// the caller does that only on success, preserving the "zero on early error"
// contract.
func (h *Handler) lookupFailoverGroup(ctx context.Context, displayModel string) (*failover.FailoverGroup, bool, error) {
	// Check failover cache before lookup (lookup populates cache on miss).
	failoverHit := failover.IsCachedByModel(displayModel)

	fg, err := h.failoverRepo.GetByModel(ctx, displayModel)
	if err != nil {
		return nil, false, err
	}

	if !fg.GroupEnabled {
		debuglog.Warn("resolve: failover group disabled", "model", displayModel)
		return nil, false, fmt.Errorf("failover group disabled")
	}

	if len(fg.PriorityOrder) == 0 {
		debuglog.Warn("resolve: empty failover group", "model", displayModel)
		return nil, false, fmt.Errorf("no entries in failover group")
	}

	return fg, failoverHit, nil
}

// enabledEntryIDs collects the model UUIDs whose failover entry is enabled,
// preserving PriorityOrder. Pure selector (phase B-collect).
func enabledEntryIDs(fg *failover.FailoverGroup) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(fg.PriorityOrder))
	for _, modelUUID := range fg.PriorityOrder {
		entryEnabled := true
		if val, ok := fg.EntryEnabled[modelUUID.String()]; ok {
			entryEnabled = val
		}
		if entryEnabled {
			ids = append(ids, modelUUID)
		}
	}
	return ids
}

// providerIDsFor collects the unique provider IDs of the enabled+provider-enabled
// models among ids. Pure selector (phase C-collect).
func providerIDsFor(ids []uuid.UUID, models map[uuid.UUID]*model.Model) []uuid.UUID {
	providerIDSet := make(map[uuid.UUID]struct{})
	for _, modelUUID := range ids {
		if m, ok := models[modelUUID]; ok && m.Enabled && m.ProviderEnabled {
			providerIDSet[m.ProviderID] = struct{}{}
		}
	}
	providerIDs := make([]uuid.UUID, 0, len(providerIDSet))
	for pid := range providerIDSet {
		providerIDs = append(providerIDs, pid)
	}
	return providerIDs
}

// batchCacheHit reports whether every id is cached (all-must-hit). It returns
// nil for an empty batch so the caller skips the cache-hit field — an empty
// all-must-hit set would otherwise falsely read "hit".
func batchCacheHit[T comparable](ids []T, cached func(T) bool) *bool {
	if len(ids) == 0 {
		return nil
	}
	hit := true
	for _, id := range ids {
		if !cached(id) {
			hit = false
			break
		}
	}
	return &hit
}

// readCircuitBreakerFlag performs phase D: the single circuit_breaker_enabled
// settings read. It returns the flag, the cache-hit, and the elapsed sample so
// the caller can do the dual accounting (settingsReadInWindow + the context
// accumulator) off one pre-computed value.
func (h *Handler) readCircuitBreakerFlag(ctx context.Context) (enabled, hit bool, elapsedMs float64) {
	hit = h.settingsRepo.IsCached("circuit_breaker_enabled")
	cbStart := time.Now()
	enabled = h.settingsRepo.GetBool(ctx, "circuit_breaker_enabled", true)
	elapsedMs = float64(time.Since(cbStart).Microseconds()) / 1000.0
	return enabled, hit, elapsedMs
}

// buildFailoverCandidates performs phase E: walk PriorityOrder, skip
// disabled/missing/circuit-broken entries, decrypt keys (keyless → ""), and
// build the candidate list. It owns only the per-key decrypt timing, returning
// the running total so the caller subtracts it from providerLookupMs; it does
// no window math itself.
func (h *Handler) buildFailoverCandidates(fg *failover.FailoverGroup, models map[uuid.UUID]*model.Model,
	providers map[uuid.UUID]*provider.Provider, cbEnabled bool,
) (candidates []modelCandidate, keyDecryptTotal float64, decryptFailures int, keyHit bool) {
	candidates = make([]modelCandidate, 0, len(fg.PriorityOrder))
	keyHit = true
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
		if cbEnabled && h.circuitBreaker.IsOpen(prov.ID, prov.Name) {
			debuglog.Info("resolve: skipping candidate: circuit breaker open", "provider", prov.Name, "model", m.ModelID)
			continue
		}
		// Keyless providers store nil encrypted key bytes — skip decryption.
		var apiKey string
		if len(prov.EncryptedKey) == 0 {
			apiKey = ""
		} else {
			// Check key cache before the actual decryption call.
			if !auth.IsKeyCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt) {
				keyHit = false
			}
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
	return candidates, keyDecryptTotal, decryptFailures, keyHit
}

func (h *Handler) resolveSpecificProvider(ctx context.Context, providerName, modelID string) ([]modelCandidate, resolveTimings, resolveCacheHits, error) {
	debuglog.Debug("resolve: resolving specific provider", "provider", providerName, "model", modelID)
	var t resolveTimings
	var ch resolveCacheHits
	providerLookupStart := time.Now()

	// Check provider cache before lookup.
	provHit := provider.IsCachedByName(providerName)

	prov, err := h.providerRepo.GetByName(ctx, providerName)
	if err != nil {
		debuglog.Warn("resolve: provider not found", "provider", providerName, "error", err)
		return nil, t, ch, fmt.Errorf("provider not found: %s", providerName)
	}
	debuglog.Debug("resolve: provider found", "provider", prov.Name, "provider_id", prov.ID, "enabled", prov.Enabled)

	ch.Provider = &provHit
	t.providerLookupMs = float64(time.Since(providerLookupStart).Microseconds()) / 1000.0

	modelLookupStart := time.Now()

	// Check model cache before lookup.
	modelHit := model.IsCachedByCompositeKey(prov.ID, modelID)

	m, err := h.modelRepo.GetByProviderAndModelID(ctx, prov.ID, modelID)
	if err != nil {
		debuglog.Warn("resolve: model not found", "model", modelID, "provider", providerName, "error", err)
		return nil, t, ch, fmt.Errorf("model not found: %s on provider %s", modelID, providerName)
	}
	debuglog.Debug("resolve: model found", "model", m.ModelID, "provider", prov.Name, "enabled", m.Enabled, "provider_enabled", m.ProviderEnabled)
	ch.Model = &modelHit
	t.modelLookupMs = float64(time.Since(modelLookupStart).Microseconds()) / 1000.0

	if !m.Enabled {
		debuglog.Info("resolve: model disabled", "model", modelID, "provider", providerName)
	}
	if !prov.Enabled {
		debuglog.Info("resolve: provider disabled", "provider", providerName, "model", modelID)
	}
	if !m.Enabled || !prov.Enabled {
		return nil, t, ch, fmt.Errorf("model or provider disabled")
	}

	// Keyless providers (e.g. OpenCode Zen free models) store nil encrypted
	// key bytes. When the key is empty, skip decryption and use empty string.
	var apiKey string
	if len(prov.EncryptedKey) == 0 {
		apiKey = ""
	} else {
		// Check key cache before the actual decryption call.
		keyHit := auth.IsKeyCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt)
		ch.Key = &keyHit
		var err error
		kdStart := time.Now()
		apiKey, err = auth.DecryptCached(prov.EncryptedKey, prov.KeyNonce, prov.KeySalt, h.cfg.MasterKey)
		t.keyDecryptMs = float64(time.Since(kdStart).Microseconds()) / 1000.0
		debuglog.Debug("resolve: key decrypted", "provider", prov.Name, "model", modelID, "decrypt_ms", t.keyDecryptMs)
		if err != nil {
			debuglog.Error("resolve: key decryption failed", "provider", prov.Name, "model", modelID, "error", err)
			return nil, t, ch, err
		}
	}

	debuglog.Debug("resolve: specific provider resolved", "provider", prov.Name, "model", m.ModelID, "id", m.ID, "has_api_key", apiKey != "")
	return []modelCandidate{{model: m, provider: prov, apiKey: apiKey}}, t, ch, nil
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
	// 499 Client Closed Request: the upstream provider reported that the
	// client disconnected mid-stream. Try the next candidate.
	if statusCode == 499 {
		return true
	}
	return false
}
