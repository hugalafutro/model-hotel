package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

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

	// Extract virtual key info early from context (available before body parsing).
	vkName := ""
	var vkID string
	var vkHash string
	if v := r.Context().Value(virtualKeyNameKey); v != nil {
		vkName, _ = v.(string)
	}
	if v := r.Context().Value(virtualKeyIDKey); v != nil {
		vkID, _ = v.(string)
	}
	if v := r.Context().Value(VirtualKeyHashKey); v != nil {
		vkHash, _ = v.(string)
	}

	// Create the log entry early so early-return paths can record failures.
	// modelID may be empty here; it gets updated after body parsing.
	logData := &requestLogData{
		modelID:         reqModel,
		streaming:       isStreaming,
		virtualKeyName:  vkName,
		virtualKeyID:    vkID,
		failoverAttempt: 0,
		state:           "pending",
		endpointType:    endpointType,
	}
	h.insertRequestLogAsync(logData)

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
				h.failRequest(logData, 400, "failed to read request body", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
				writeOpenAIError(w, "failed to read request body", http.StatusBadRequest)
				return nil, false
			}
			_ = r.Body.Close()
		}

		var req ChatCompletionRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			debuglog.Warn("proxy: failed to parse request body", "error", err)
			publishRequestStartedEvent(logData)
			h.failRequest(logData, 400, "invalid request body", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
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

	// Update log entry with model resolved from body parsing (if not set by middleware).
	logData.modelID = reqModel
	logData.streaming = isStreaming

	// Publish the SSE "request.started" event after modelID is resolved
	// so subscribers always see the correct model (not an empty string).
	publishRequestStartedEvent(logData)

	if reqModel == "" {
		h.failRequest(logData, 400, "model is required", 0, startTime, parseMs, resolveTimings{}, resolveCacheHits{}, 0)
		writeOpenAIError(w, "model is required", http.StatusBadRequest)
		return nil, false
	}

	debuglog.Info("proxy: request start", "model", reqModel, "stream", isStreaming, "key", vkName, "client_ip", r.RemoteAddr)
	debuglog.Debug("proxy: request details", "model", reqModel, "stream", isStreaming, "key", vkName, "vk_id", vkID, "has_hash", vkHash != "", "body_length", len(bodyBytes))

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
			h.failRequest(st.logData, 404, err.Error(), 0, st.startTime, st.parseMs, timings, cacheHits, 0)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return nil, false
		}
		if len(candidates) == 0 {
			h.failRequest(st.logData, 502, "no available provider for hotel/"+displayModel, 0, st.startTime, st.parseMs, timings, cacheHits, 0)
			writeOpenAIError(w, "no available provider for hotel/"+displayModel, http.StatusBadGateway)
			return nil, false
		}
	case strings.Contains(st.reqModel, "/") && !strings.HasPrefix(st.reqModel, "hotel/"):
		debuglog.Debug("proxy: model resolution path", "type", "specific_provider", "model", st.reqModel)
		parts := strings.SplitN(st.reqModel, "/", 2)
		providerName, modelID := parts[0], parts[1]
		candidates, timings, cacheHits, err = h.resolveSpecificProvider(r.Context(), providerName, modelID)
		if err != nil {
			h.failRequest(st.logData, 404, err.Error(), 0, st.startTime, st.parseMs, timings, cacheHits, 0)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return nil, false
		}
	default:
		h.failRequest(st.logData, 400, "invalid model format: "+st.reqModel, 0, st.startTime, st.parseMs, timings, resolveCacheHits{}, 0)
		writeOpenAIError(w, "invalid model format, expected provider/model or hotel/model", http.StatusBadRequest)
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

	// Filter candidates by virtual key's allowed_providers.
	// If the key has a non-nil allowed list, remove candidates whose
	// provider ID is not in the list. nil = all providers allowed.
	if v := r.Context().Value(ctxkeys.VirtualKeyAllowedProvidersKey); v != nil {
		if allowed, ok := v.(*[]string); ok && allowed != nil && len(*allowed) > 0 {
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
				h.failRequest(st.logData, 403, "virtual key does not have access to any provider for this model", 0, st.startTime, st.parseMs, timings, cacheHits, 0)
				writeOpenAIError(w, "virtual key does not have access to any provider for this model", http.StatusForbidden)
				return nil, false
			}
			debuglog.Info("proxy: filtered candidates by allowed_providers", "before", len(candidates), "after", len(filtered), "key", st.logData.virtualKeyName)
			candidates = filtered
		}
	}

	st.timings = timings
	st.cacheHits = cacheHits
	st.isFailover = isFailover
	return candidates, true
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
	// Read once before the loop so all attempts within a single request use
	// the same timeout, avoiding inconsistency if the setting changes mid-request.
	rtStart := time.Now()
	baseTimeout := h.settingsRepo.GetDuration(r.Context(), "request_timeout", time.Minute)
	ctxkeys.AddSettingsReadMs(r.Context(), rtStart)
	st.failoverTimeout = baseTimeout
	if st.isStreaming {
		st.failoverTimeout = baseTimeout * 10
	}

	// Read circuit_breaker_enabled once before the loop to avoid repeated settings reads.
	cbStart2 := time.Now()
	st.circuitBreakerEnabled = h.settingsRepo.GetBool(r.Context(), "circuit_breaker_enabled", true)
	ctxkeys.AddSettingsReadMs(r.Context(), cbStart2)

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
