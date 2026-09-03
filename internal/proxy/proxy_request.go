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
// validates it: the pending request-log row, the request.started event, the
// "request start" app-log line and the resolve failures all carry it. None of
// those sinks has a bound of its own, so an unbounded model inside a legal,
// capped request body would be a multi-megabyte write to each of them from one
// virtual key. Real routing targets are "provider/model" or "hotel/group", and
// the admin write paths cap display models at 128 characters, so 512 is far
// above anything that can resolve.
const maxModelNameRunes = 512

// modelTooLongMessage is the constant refusal for an oversized model. The
// number it spells must match maxModelNameRunes.
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

// rejectOversizedModel is the one outcome every ingest path has for a model past
// maxModelNameRunes: the pending row the caller already inserted is closed as a
// validation failure carrying the excerpt rather than the field, subscribers see
// the started/completed pair every other early guard emits, and the caller gets
// the constant message back. The response never quotes the field.
//
// It takes the raw model and derives the excerpt itself, so no ingest path can
// put the field on the row by forgetting to. The middleware-preparsed path must
// still hand the excerpt to its own pending INSERT, which runs before this can.
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
// (nil, false), on which the caller simply returns.
func (h *Handler) ingestRequest(w http.ResponseWriter, r *http.Request, endpointType string) (*requestState, bool) {
	startTime := time.Now()

	var parseMs float64
	var reqModel string
	var isStreaming bool

	// Read pre-parsed values from the middleware context when available:
	// streamingAwareTimeout has already read the body and extracted model and
	// stream, so the json.Unmarshal below is skipped.
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

	// Fallback for a route streamingAwareTimeout does not cover, where the
	// middleware provided no pre-parsed values: parse the body directly.
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
		// The middleware provided model and stream; the body bytes are still
		// needed for stream_options injection and upstream forwarding.
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

	// Update the log entry with the model body parsing resolved, when the
	// middleware did not set one.
	logData.modelID = reqModel
	logData.streaming = isStreaming

	// The SSE "request.started" event goes out after modelID is resolved, so
	// subscribers always see the real model rather than an empty string.
	publishRequestStartedEvent(logData)

	if reqModel == "" {
		h.failRequest(logData, 400, KindValidation, "model is required", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "model is required", http.StatusBadRequest)
		return nil, false
	}

	debuglog.Info("proxy: request start", "client_ip", clientip.From(r), "model", reqModel, "stream", isStreaming, "key", logData.virtualKeyName)
	debuglog.Debug("proxy: request details", "model", reqModel, "stream", isStreaming, "key", logData.virtualKeyName, "vk_id", logData.virtualKeyID, "has_hash", vkHash != "", "body_length", len(bodyBytes))

	logData.promptTextBytes = promptTextBytes(bodyBytes)
	logData.content = newContentFence(bodyBytes)

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
	// The owning user's UUID, empty for unowned keys, scopes the SSE request
	// events to that user. It is persisted on the log row itself when there is
	// no virtual key to resolve an owner through (dashboard chat/arena), which
	// is what lets the owner-scoped logs REST API see those rows.
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
		var skips breakerSkipSummary
		candidates, timings, cacheHits, skips, err = h.resolveHotelModel(r.Context(), displayModel)
		if err != nil {
			h.failRequest(st.logData, 404, KindValidation, err.Error(), 0, st.startTime, st.parseMs, timings, cacheHits, 0)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return nil, false
		}
		// The candidates the breaker refused lead the attempt trail, so an
		// operator reading it also sees which providers were never asked, and
		// why.
		for _, s := range skips.skipped {
			st.logData.appendBreakerSkip(s.providerID, s.providerName, s.model)
		}
		if len(candidates) == 0 {
			h.failNoAvailableProvider(w, r, st, displayModel, timings, cacheHits, skips)
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

	st.logData.cacheHits = cacheHits

	// Normalize logData after resolution: split the raw request model (say
	// "NanoGPT/deepseek-ai/DeepSeek-R1-0528") into provider name and model so
	// log lines are readable.
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
		// result no longer says where each member came from. It is the only
		// signal that an account cap is in play, which matters because a key
		// that has always worked can start refusing purely because its owner's
		// cap moved. The response body says none of it: a proxy client learns it
		// lacks access, not whose rule denied it.
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
// This is the enforcement point for the per-user provider cap, and the only one
// that runs on every request. The write-time check in
// internal/api/virtualkeys.go only produces a friendly refusal on the two
// dashboard write paths, so a stored list wider than its owner's cap is a normal
// state rather than a corruption: upsertVirtualKeys in
// internal/api/configsync_apply.go writes virtual_keys rows straight from a
// fleet import without consulting the cap, and narrowing
// users.allowed_providers updates only the users row.
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
// and read the per-request failover knobs (the request timeout, 10x for
// streaming, circuit-breaker enablement, and the overall request deadline). The
// results are stored on st for the failover loop, which recomputes
// proxyOverhead after each dial, so the value set here is a pre-loop estimate.
func (h *Handler) loadFailoverConfig(r *http.Request, st *requestState) {
	// Re-read the accumulated settings-read time from the context pointer, which
	// now holds the rate limiter's contribution plus the circuit-breaker and
	// failover reads the resolve handlers added.
	if v := r.Context().Value(ctxkeys.SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			st.timings.settingsReadMs = *p
		}
	}

	// Initial overhead estimate, with dialMs still 0. The failover loop
	// recomputes proxyOverhead after each dial so every exit path (backoff
	// disconnect, error, failRequest) uses the current accumulated total.
	st.proxyOverhead = st.timings.proxyOverheadMs(st.parseMs)

	// The non-streaming timeout comes from the request_timeout setting, default
	// 1m. Streaming requests get 10x that, to accommodate reasoning models that
	// can take minutes before the first token, and so do the long-running
	// multimodal endpoints (image generation, audio), whose legitimate latencies
	// run just as long without carrying a chat-style stream flag.
	//
	// Read once before the loop so every attempt in a request uses the same
	// timeout even if the setting changes mid-request.
	rtStart := time.Now()
	baseTimeout := h.settingsRepo.GetDuration(r.Context(), "request_timeout", time.Minute)
	ctxkeys.AddSettingsReadMs(r.Context(), rtStart)
	st.failoverTimeout = baseTimeout
	if st.isStreaming || st.longRunning {
		st.failoverTimeout = baseTimeout * 10
	}

	// Read circuit_breaker_enabled once before the loop, to avoid repeated
	// settings reads.
	cbStart2 := time.Now()
	st.circuitBreakerEnabled = h.settingsRepo.GetBool(r.Context(), "circuit_breaker_enabled", true)
	// Same once-per-request read for the adaptive in-flight limiter; a nil
	// limiter (handler-literal tests) admits everything whatever this says.
	st.inflightEnabled = h.inflight != nil && h.settingsRepo.GetBool(r.Context(), "inflight_limiter_enabled", true)
	// And for the last candidate's one retry of a transient 5xx.
	st.serverErrorRetryEnabled = h.settingsRepo.GetBool(r.Context(), "server_error_retry_enabled", true)
	ctxkeys.AddSettingsReadMs(r.Context(), cbStart2)

	// Request hedging config (streaming only; applied at the failover gate).
	// Read once here so every attempt in the request sees a consistent value.
	hedgeStart := time.Now()
	st.hedgingEnabled = h.settingsRepo.GetBool(r.Context(), "hedging_enabled", false)
	st.hedgeDelay = max(h.settingsRepo.GetDuration(r.Context(), "hedge_delay", 4*time.Second), minHedgeDelay)
	ctxkeys.AddSettingsReadMs(r.Context(), hedgeStart)

	// The overall request deadline caps total time across all failover
	// candidates, so a silent client cannot pin a goroutine for N candidates x
	// failoverTimeout. The ceiling is 2x the per-candidate timeout, giving a
	// second attempt its full time while capping any number of subsequent
	// candidates to the remaining budget.
	st.overallDeadline = st.startTime.Add(st.failoverTimeout * 2)

	// Final re-read of the accumulated settings-read time, which now also holds
	// this function's request_timeout and circuit_breaker_enabled reads.
	if v := r.Context().Value(ctxkeys.SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			st.timings.settingsReadMs = *p
		}
	}
}
