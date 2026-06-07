package proxy

import (
	"encoding/json"
	"io"
	"net/http"
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
