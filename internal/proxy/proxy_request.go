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

// ingestRequest performs phase A of ChatCompletions: read the pre-parsed
// model/stream/parse-time and virtual-key identity from the middleware context,
// create the early "pending" request-log entry, fall back to parsing the body
// when middleware did not pre-parse, publish the request.started event, and run
// the three early-failure guards (body read, body parse, empty model).
//
// On success it returns a populated *requestState and true. On any guard
// failure it records the failure, writes the OpenAI error response, and returns
// (nil, false) — the caller must simply return.
func (h *Handler) ingestRequest(w http.ResponseWriter, r *http.Request) (*requestState, bool) {
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
