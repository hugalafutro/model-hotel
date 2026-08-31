package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// maxModelNameRunes bounds the client-supplied `model` routing field.
//
// The field is routing metadata, and the pipeline persists it before it
// validates it: the pending request-log row carries it, the request.started
// event carries it, the "request start" app-log line carries it, and the
// resolve failures quote it back in the error response. None of those sinks
// had a bound of its own, so a multi-megabyte model inside the (legal, capped)
// request body was a multi-megabyte write to every one of them, from one
// virtual key. Real routing targets are "provider/model" or "hotel/group";
// the admin write paths cap display models at 128 characters, so 512 is far
// above anything that can resolve.
const maxModelNameRunes = 512

// modelTooLongMessage is the constant refusal for an oversized model. A test
// pins the number it spells to maxModelNameRunes so the two cannot drift.
const modelTooLongMessage = "model exceeds maximum length of 512 characters"

// modelExcerptRunes is how much of an oversized model the request-log row
// keeps. The row exists so the refusal stays attributed to its virtual key;
// a prefix is enough to recognise the request in the log and small enough that
// the attacker's string cannot use the row as its sink.
const modelExcerptRunes = 64

// modelTooLong reports whether the model field is over the bound. An empty
// model is not too long; "model is required" is a separate guard downstream.
func modelTooLong(model string) bool {
	return utf8.RuneCountInString(model) > maxModelNameRunes
}

// modelExcerpt returns the bounded form of an oversized model for the
// request-log row: the first modelExcerptRunes runes and an ellipsis, cut on a
// rune boundary so the stored text stays valid UTF-8.
func modelExcerpt(model string) string {
	if utf8.RuneCountInString(model) <= modelExcerptRunes {
		return model
	}
	return string([]rune(model)[:modelExcerptRunes]) + "…"
}

// rejectOversizedModel is the one outcome every ingest path has for a model
// past maxModelNameRunes: the pending row (already inserted by the caller) is
// closed as a validation failure carrying the excerpt rather than the field,
// subscribers see the started/completed pair every other early guard emits,
// and the caller gets the constant message back. The response never quotes
// the field. It takes the raw model and derives the excerpt itself so no
// ingest path can put the field on the row at the refusal by forgetting to;
// the middleware-preparsed path must still hand the excerpt to its own
// pending INSERT, which runs before this can.
func (h *Handler) rejectOversizedModel(w http.ResponseWriter, logData *requestLogData, model string, startTime time.Time, parseMs float64) {
	logData.modelID = modelExcerpt(model)
	publishRequestStartedEvent(logData)
	h.failRequest(logData, http.StatusBadRequest, KindValidation, modelTooLongMessage, 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
	writeOpenAIError(w, modelTooLongMessage, http.StatusBadRequest)
}

// ingestRequest performs phase A of ChatCompletions and the JSON multimodal
// endpoints: read the pre-parsed model/stream/parse-time and virtual-key
// identity from the middleware context, create the early "pending" request-log
// entry (tagged with endpointType), fall back to parsing the body when
// middleware did not pre-parse, publish the request.started event, and run
// the three early-failure guards (body read, body parse, empty model).
//
// On success it returns a populated *requestState and true. On any guard
// failure it records the failure, writes the OpenAI error response, and returns
// (nil, false) — the caller must simply return.
func (h *Handler) ingestRequest(w http.ResponseWriter, r *http.Request, endpointType string) (*requestState, bool) {
	startTime := time.Now()

	var parseMs float64
	var reqModel string
	var isStreaming bool

	// Read pre-parsed values from middleware context when available.
	// streamingAwareTimeout already read the body and extracted model+stream,
	// so we skip the redundant json.Unmarshal that previously measured as parseMs.
	if v := r.Context().Value(ctxkeys.RequestBodyParseMsKey); v != nil {
		if ms, ok := v.(float64); ok {
			parseMs = ms
		}
	}
	if v := r.Context().Value(ctxkeys.RequestModelKey); v != nil {
		if m, ok := v.(string); ok {
			reqModel = m
		}
	}
	if v := r.Context().Value(ctxkeys.IsStreamingKey); v != nil {
		if s, ok := v.(bool); ok {
			isStreaming = s
		}
	}

	// Fallback: if middleware did not provide pre-parsed values (e.g. route
	// not covered by streamingAwareTimeout), parse from body directly.
	var bodyBytes []byte

	// The middleware-provided model is what the pending INSERT below would
	// carry, so the bound is checked before the row exists: the row is
	// inserted with the excerpt and closed as the refusal. Every later sink
	// (the event, the app-log lines, the response) is behind the return.
	if modelTooLong(reqModel) {
		logData, _ := h.newPendingRequestLog(r, endpointType, modelExcerpt(reqModel), isStreaming)
		h.rejectOversizedModel(w, logData, reqModel, startTime, parseMs)
		return nil, false
	}

	logData, vkHash := h.newPendingRequestLog(r, endpointType, reqModel, isStreaming)

	if reqModel == "" {
		parseStart := time.Now()
		if cached, ok := r.Context().Value(ctxkeys.RequestBodyKey).([]byte); ok {
			bodyBytes = cached
		} else {
			var err error
			bodyBytes, err = io.ReadAll(r.Body)
			if err != nil {
				debuglog.Warn("proxy: failed to read request body", "error", err)
				publishRequestStartedEvent(logData)
				h.failRequest(logData, 400, KindValidation, "failed to read request body", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
				writeOpenAIError(w, "failed to read request body", http.StatusBadRequest)
				return nil, false
			}
			_ = r.Body.Close()
		}

		var req ChatCompletionRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			debuglog.Warn("proxy: failed to parse request body", "error", err)
			publishRequestStartedEvent(logData)
			h.failRequest(logData, 400, KindValidation, "invalid request body", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
			writeOpenAIError(w, "invalid request body", http.StatusBadRequest)
			return nil, false
		}
		parseMs = float64(time.Since(parseStart).Microseconds()) / 1000.0
		reqModel = req.Model
		isStreaming = req.Stream
	} else {
		// Middleware provided model+stream; still need body bytes for
		// stream_options injection and upstream forwarding.
		if cached, ok := r.Context().Value(ctxkeys.RequestBodyKey).([]byte); ok {
			bodyBytes = cached
		}
	}

	// The body-parsed model has not touched any sink yet: the pending row was
	// inserted with an empty model, and modelID, the event and the app-log
	// lines all come after this point. Same refusal, same excerpt on the row.
	if modelTooLong(reqModel) {
		logData.streaming = isStreaming
		h.rejectOversizedModel(w, logData, reqModel, startTime, parseMs)
		return nil, false
	}

	// Update log entry with model resolved from body parsing (if not set by middleware).
	logData.modelID = reqModel
	logData.streaming = isStreaming

	// Publish the SSE "request.started" event after modelID is resolved
	// so subscribers always see the correct model (not an empty string).
	publishRequestStartedEvent(logData)

	if reqModel == "" {
		h.failRequest(logData, 400, KindValidation, "model is required", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "model is required", http.StatusBadRequest)
		return nil, false
	}

	debuglog.Info("proxy: request start", "client_ip", clientip.From(r), "model", reqModel, "stream", isStreaming, "key", logData.virtualKeyName)
	debuglog.Debug("proxy: request details", "model", reqModel, "stream", isStreaming, "key", logData.virtualKeyName, "vk_id", logData.virtualKeyID, "has_hash", vkHash != "", "body_length", len(bodyBytes))

	logData.promptTextBytes = promptTextBytes(bodyBytes)

	return &requestState{
		startTime:   startTime,
		reqModel:    reqModel,
		isStreaming: isStreaming,
		vkHash:      vkHash,
		bodyBytes:   bodyBytes,
		parseMs:     parseMs,
		logData:     logData,
	}, true
}

// newPendingRequestLog extracts the virtual-key identity from the request
// context and creates + async-inserts the early "pending" request-log entry,
// the phase-A preamble shared by the chat and multipart ingest paths.
// modelID/streaming may be zero when the body has not been parsed yet; the
// caller updates logData once they are known. The virtual-key hash is
// returned separately because requestState carries it outside the log entry.
func (h *Handler) newPendingRequestLog(r *http.Request, endpointType, modelID string, isStreaming bool) (logData *requestLogData, vkHash string) {
	vkName := ""
	var vkID string
	if v := r.Context().Value(virtualKeyNameKey); v != nil {
		vkName, _ = v.(string)
	}
	if v := r.Context().Value(virtualKeyIDKey); v != nil {
		vkID, _ = v.(string)
	}
	if v := r.Context().Value(VirtualKeyHashKey); v != nil {
		vkHash, _ = v.(string)
	}
	// The owning user's UUID (empty for unowned keys) scopes the SSE request
	// events to that user, and is persisted on the log row itself when there is
	// no virtual key to resolve an owner through (dashboard chat/arena), which is
	// what lets the owner-scoped logs REST API see those rows at all.
	var ownerUserID string
	if v := r.Context().Value(ctxkeys.VirtualKeyOwnerIDKey); v != nil {
		ownerUserID, _ = v.(string)
	}

	logData = &requestLogData{
		modelID:         modelID,
		streaming:       isStreaming,
		virtualKeyName:  vkName,
		virtualKeyID:    vkID,
		clientIP:        clientip.From(r),
		ownerUserID:     ownerUserID,
		failoverAttempt: 0,
		state:           "pending",
		endpointType:    endpointType,
	}
	h.insertRequestLogAsync(logData)
	return logData, vkHash
}

// resolveCandidates performs phase B of ChatCompletions: resolve the request
// model into an ordered candidate list (hotel failover group, specific
// provider/model, or invalid-format), normalize the log entry's provider/model
// fields, and apply the virtual key's allowed_providers access filter.
//
// On success it stores the resolve timings, cache hits, and failover flag into
// st and returns (candidates, true). On any failure it records the failure,
// writes the OpenAI error response, and returns (nil, false).
func (h *Handler) resolveCandidates(w http.ResponseWriter, r *http.Request, st *requestState) ([]modelCandidate, bool) {
	var candidates []modelCandidate
	var timings resolveTimings
	var cacheHits resolveCacheHits
	var err error

	// Capture accumulated settings read time (pointer in context, set by
	// rate limiter middleware and added to by resolve/proxy handlers).
	if v := r.Context().Value(ctxkeys.SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			timings.settingsReadMs = *p
		}
	}

	isFailover := false

	switch {
	case strings.HasPrefix(st.reqModel, "hotel/"):
		isFailover = true
		debuglog.Debug("proxy: model resolution path", "type", "hotel", "model", st.reqModel)
		displayModel := strings.ToLower(strings.TrimPrefix(st.reqModel, "hotel/"))
		candidates, timings, cacheHits, err = h.resolveHotelModel(r.Context(), displayModel)
		if err != nil {
			h.failRequest(st.logData, 404, KindValidation, err.Error(), 0, st.startTime, st.parseMs, timings, cacheHits, 0)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return nil, false
		}
		if len(candidates) == 0 {
			h.failRequest(st.logData, 502, KindProviderError, "no available provider for hotel/"+displayModel, 0, st.startTime, st.parseMs, timings, cacheHits, 0)
			writeOpenAIError(w, "no available provider for hotel/"+displayModel, http.StatusBadGateway)
			return nil, false
		}
	case strings.Contains(st.reqModel, "/") && !strings.HasPrefix(st.reqModel, "hotel/"):
		debuglog.Debug("proxy: model resolution path", "type", "specific_provider", "model", st.reqModel)
		parts := strings.SplitN(st.reqModel, "/", 2)
		providerName, modelID := parts[0], parts[1]
		candidates, timings, cacheHits, err = h.resolveSpecificProvider(r.Context(), providerName, modelID)
		if err != nil {
			h.failRequest(st.logData, 404, KindValidation, err.Error(), 0, st.startTime, st.parseMs, timings, cacheHits, 0)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return nil, false
		}
	default:
		h.failRequest(st.logData, 400, KindValidation, "invalid model format: "+st.reqModel, 0, st.startTime, st.parseMs, timings, resolveCacheHits{}, 0)
		writeOpenAIError(w, `invalid model format: expected "provider/model" or "hotel/group"`, http.StatusBadRequest)
		return nil, false
	}

	// Store cache hit data from resolve phase into the log entry.
	st.logData.cacheHits = cacheHits

	// Normalize logData fields after resolution: split the raw request model
	// (e.g. "NanoGPT/deepseek-ai/DeepSeek-R1-0528") into provider name and
	// model-only components so log lines are human-readable.
	if parts := strings.SplitN(st.reqModel, "/", 2); len(parts) == 2 && !strings.HasPrefix(st.reqModel, "hotel/") {
		st.logData.providerName = parts[0]
		st.logData.modelID = parts[1]
	} else {
		st.logData.providerName = "hotel"
	}

	// Filter candidates by the effective provider allow-list: the virtual key's
	// own list intersected with its owner account's cap. nil means no
	// restriction; a non-nil list restricts to exactly its members, so an empty
	// one denies everything rather than allowing everything.
	keyAllowed, _ := r.Context().Value(ctxkeys.VirtualKeyAllowedProvidersKey).(*[]string)
	ownerAllowed, _ := r.Context().Value(ctxkeys.UserAllowedProvidersKey).(*[]string)
	if allowed := effectiveAllowedProviders(keyAllowed, ownerAllowed); allowed != nil {
		allowedSet := make(map[string]struct{}, len(*allowed))
		for _, id := range *allowed {
			allowedSet[id] = struct{}{}
		}
		filtered := candidates[:0]
		for _, c := range candidates {
			if _, ok := allowedSet[c.provider.ID.String()]; ok {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			h.failRequest(st.logData, 403, KindAuth, "virtual key does not have access to any provider for this model", 0, st.startTime, st.parseMs, timings, cacheHits, 0)
			writeOpenAIError(w, "virtual key does not have access to any provider for this model", http.StatusForbidden)
			return nil, false
		}
		// owner_capped records whether the owner HAS a cap, not which side did
		// the narrowing: the two lists are intersected before this runs and the
		// result no longer says where each member came from. It is still the
		// field worth having, because a key that has always worked can start
		// refusing purely because its owner's cap moved, and this is the only
		// signal that an account cap is in play at all. The response body
		// deliberately says none of it: a proxy client learns it lacks access,
		// not whose rule denied it.
		debuglog.Info("proxy: filtered candidates by allowed_providers",
			"before", len(candidates), "after", len(filtered),
			"owner_capped", ownerAllowed != nil, "key", st.logData.virtualKeyName)
		candidates = filtered
	}

	st.timings = timings
	st.cacheHits = cacheHits
	st.isFailover = isFailover
	return candidates, true
}

// effectiveAllowedProviders intersects a virtual key's provider allow-list with
// its owner's account cap. Either side being nil means that side imposes no
// restriction; a non-nil list restricts to exactly its members INCLUDING when
// empty, which is what makes the pair fail-closed.
//
// This is the enforcement point for the per-user provider cap, and it is the
// only one that runs on every request. The write-time check in
// internal/api/virtualkeys.go merely produces a friendly refusal on the two
// dashboard write paths, so a stored list wider than its owner's cap is a
// normal state, not a corruption: upsertVirtualKeys in
// internal/api/configsync_apply.go writes virtual_keys rows straight from a
// fleet import without consulting the cap, and narrowing users.allowed_providers
// updates only the users row, leaving that owner's existing keys untouched.
func effectiveAllowedProviders(key, owner *[]string) *[]string {
	switch {
	case key == nil && owner == nil:
		return nil
	case owner == nil:
		return key
	case key == nil:
		return owner
	}
	ownerSet := make(map[string]struct{}, len(*owner))
	for _, id := range *owner {
		ownerSet[id] = struct{}{}
	}
	out := []string{}
	for _, id := range *key {
		if _, ok := ownerSet[id]; ok {
			out = append(out, id)
		}
	}
	return &out
}

// loadFailoverConfig performs phase C of ChatCompletions: finalize the
// accumulated settings-read time, compute the initial proxy-overhead estimate,
// and read the per-request failover knobs (request timeout — 10× for streaming,
// circuit-breaker enablement, and the overall request deadline). The results
// are stored on st for the failover loop. The loop recomputes proxyOverhead
// after each dial, so the value set here is only the pre-loop estimate.
func (h *Handler) loadFailoverConfig(r *http.Request, st *requestState) {
	// Re-read accumulated settings read time from context pointer.
	// The initial read captured the rate limiter's contribution,
	// but resolve handlers called AddSettingsReadMs for circuit breaker and
	// failover settings. The pointer now holds the total.
	if v := r.Context().Value(ctxkeys.SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			st.timings.settingsReadMs = *p
		}
	}

	// Initial overhead estimate (dialMs=0 — not yet populated).
	// proxyOverhead is recomputed after each dial inside the failover loop
	// so that all exit paths (backoff disconnect, error, failRequest) use
	// the current accumulated total.
	st.proxyOverhead = st.timings.proxyOverheadMs(st.parseMs)

	// Non-streaming timeout is configurable via request_timeout setting (default 1m).
	// Streaming requests get 10× the non-streaming timeout to accommodate
	// thinking/reasoning models that can take several minutes before first token.
	// Long-running multimodal endpoints (image generation, audio) get the same
	// extended budget: their legitimate latencies and response transfers also
	// run for minutes without carrying a chat-style stream flag.
	// Read once before the loop so all attempts within a single request use
	// the same timeout, avoiding inconsistency if the setting changes mid-request.
	rtStart := time.Now()
	baseTimeout := h.settingsRepo.GetDuration(r.Context(), "request_timeout", time.Minute)
	ctxkeys.AddSettingsReadMs(r.Context(), rtStart)
	st.failoverTimeout = baseTimeout
	if st.isStreaming || st.longRunning {
		st.failoverTimeout = baseTimeout * 10
	}

	// Read circuit_breaker_enabled once before the loop to avoid repeated settings reads.
	cbStart2 := time.Now()
	st.circuitBreakerEnabled = h.settingsRepo.GetBool(r.Context(), "circuit_breaker_enabled", true)
	ctxkeys.AddSettingsReadMs(r.Context(), cbStart2)

	// Request hedging config (streaming only; applied at the failover gate).
	// Read once here so every attempt in the request sees a consistent value.
	hedgeStart := time.Now()
	st.hedgingEnabled = h.settingsRepo.GetBool(r.Context(), "hedging_enabled", false)
	st.hedgeDelay = max(h.settingsRepo.GetDuration(r.Context(), "hedge_delay", 4*time.Second), minHedgeDelay)
	ctxkeys.AddSettingsReadMs(r.Context(), hedgeStart)

	// Overall request deadline: caps total time across all failover candidates
	// to prevent resource pinning from silent clients. Without this, N candidates
	// with per-candidate failoverTimeout could hold a goroutine for N×failoverTimeout.
	// The ceiling is 2× the per-candidate timeout, giving a second attempt full time
	// while capping any number of subsequent candidates to the remaining budget.
	st.overallDeadline = st.startTime.Add(st.failoverTimeout * 2)

	// Final re-read of accumulated settings read time. The initial read
	// captured the rate limiter's contribution, resolve handlers added
	// circuit breaker/failover settings, and the proxy loop added
	// request_timeout and circuit_breaker_enabled reads. Recompute
	// proxyOverhead with the complete total.
	if v := r.Context().Value(ctxkeys.SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			st.timings.settingsReadMs = *p
		}
	}
}
