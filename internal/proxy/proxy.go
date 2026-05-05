package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// providerUnsupportedParams lists OpenAI Chat Completions parameters that are
// universally unsupported (cause 400 errors) per provider type. These are
// preemptively stripped from requests to avoid a wasted round-trip.
// Sources: official provider docs + empirical testing.
var providerUnsupportedParams = map[string][]string{
	"anthropic": {
		"top_p", // deprecated on all current Anthropic models
	},
	"google": {
		"frequency_penalty", // not supported on Gemini OpenAI-compat endpoint
		"presence_penalty",  // not supported on Gemini OpenAI-compat endpoint
		"logprobs",          // not supported
		"top_logprobs",      // not supported
	},
	"cohere": {
		"logprobs",     // not supported
		"top_logprobs", // not supported
	},
}

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, modelLookupMs, providerLookupMs, keyDecryptMs, ttft float64, vkHash string, attempt int) {
	defer func() { _ = resp.Body.Close() }()
	debuglog.Debug("proxy: handleStreamingResponse entered", "model", logData.modelID, "provider", logData.providerID, "upstream_status", resp.StatusCode, "attempt", attempt, "ttft_ms", ttft)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	debuglog.Debug("proxy: streaming headers sent", "model", logData.modelID, "provider", logData.providerID)

	logData.statusCode = resp.StatusCode
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.modelLookupMs = modelLookupMs
	logData.providerLookupMs = providerLookupMs
	logData.keyDecryptMs = keyDecryptMs
	logData.ttftMs = ttft
	logData.failoverAttempt = attempt
	logData.state = "streaming"
	h.updateRequestLog(r.Context(), logData)

	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024) // 4MB per line
	debuglog.Debug("proxy: streaming scanner created", "model", logData.modelID, "provider", logData.providerID)
	var promptTokens, completionTokens int
	var promptCacheHitTokens, promptCacheMissTokens int
	var lastErrMsg string
	clientDisconnected := false
	sawDone := false
	chunkCount := 0
	errorChunkCount := 0
	var bytesWritten int64

	for scanner.Scan() {
		line := scanner.Bytes()
		chunkCount++

		select {
		case <-r.Context().Done():
			clientDisconnected = true
			goto logUpdate
		default:
		}

		if n, err := w.Write(line); err != nil {
			clientDisconnected = true
			debuglog.Warn("proxy: client write failed during stream",
				"error", err, "model", logData.modelID, "provider", logData.providerID,
				"chunks", chunkCount, "bytes_written", bytesWritten)
			goto logUpdate
		} else {
			bytesWritten += int64(n)
		}
		if n, err := w.Write([]byte("\n")); err != nil {
			clientDisconnected = true
			debuglog.Warn("proxy: client write failed during stream (newline)",
				"error", err, "model", logData.modelID, "provider", logData.providerID,
				"chunks", chunkCount, "bytes_written", bytesWritten)
			goto logUpdate
		} else {
			bytesWritten += int64(n)
		}
		if canFlush {
			flusher.Flush()
		}

		if strings.HasPrefix(string(line), "data: ") {
			payload := strings.TrimPrefix(string(line), "data: ")
			if payload == "[DONE]" {
				sawDone = true
				debuglog.Debug("proxy: received [DONE] sentinel", "model", logData.modelID, "provider", logData.providerID, "chunks", chunkCount)
				break
			}
			var chunk struct {
				Usage *Usage                    `json:"usage"`
				Error *struct{ Message string } `json:"error"`
			}
			if json.Unmarshal([]byte(payload), &chunk) == nil {
				if chunk.Usage != nil {
					promptTokens = chunk.Usage.PromptTokens
					completionTokens = chunk.Usage.CompletionTokens
					if chunk.Usage.PromptCacheHitTokens > 0 {
						promptCacheHitTokens = chunk.Usage.PromptCacheHitTokens
						promptCacheMissTokens = chunk.Usage.PromptTokens - chunk.Usage.PromptCacheHitTokens
					}
				}
				if chunk.Error != nil {
					lastErrMsg = chunk.Error.Message
					errorChunkCount++
					debuglog.Warn("proxy: SSE error chunk", "model", logData.modelID, "provider", logData.providerID, "error_message", chunk.Error.Message, "chunk_number", chunkCount)
				}
			}
		}
	}

logUpdate:
	totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
	var tps float64
	if completionTokens > 0 && totalDuration > 0 {
		tps = float64(completionTokens) / float64(totalDuration) * 1000
	}

	errMsg := lastErrMsg
	if errMsg == "" && scanner.Err() != nil {
		errMsg = scanner.Err().Error()
	}
	if clientDisconnected {
		errMsg = "client disconnected"
		debuglog.Warn("proxy: client disconnected during streaming", "model", logData.modelID)
	}
	if errMsg == "" && !sawDone {
		errMsg = "stream truncated: upstream closed connection without [DONE] sentinel"
		debuglog.Warn("proxy: stream ended without [DONE] sentinel", "model", logData.modelID, "provider", logData.providerID, "chunks", chunkCount)
	}

	logData.statusCode = resp.StatusCode
	logData.durationMs = totalDuration
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.modelLookupMs = modelLookupMs
	logData.providerLookupMs = providerLookupMs
	logData.keyDecryptMs = keyDecryptMs
	logData.ttftMs = ttft
	logData.tokensPerSecond = tps
	logData.tokensPrompt = promptTokens
	logData.tokensCompletion = completionTokens
	logData.tokensPromptCacheHit = promptCacheHitTokens
	logData.tokensPromptCacheMiss = promptCacheMissTokens
	logData.errorMessage = errMsg
	logData.failoverAttempt = attempt
	if errMsg != "" {
		logData.state = "failed"
	} else {
		logData.state = "completed"
	}
	h.updateRequestLog(r.Context(), logData)

	debuglog.Info("proxy: streaming finished", "model", logData.modelID, "provider", logData.providerID, "attempt", attempt, "ttft_ms", ttft, "duration_ms", totalDuration, "chunks", chunkCount, "bytes_written", bytesWritten, "prompt_tokens", promptTokens, "completion_tokens", completionTokens, "error_chunks", errorChunkCount, "has_error", errMsg != "")
	if errMsg != "" {
		debuglog.Warn("proxy: streaming error", "model", logData.modelID, "provider", logData.providerID, "error", errMsg, "upstream_status", resp.StatusCode, "attempt", attempt, "duration_ms", totalDuration)
	} else {
		debuglog.Debug("proxy: streaming completed successfully", "model", logData.modelID, "provider", logData.providerID, "attempt", attempt, "ttft_ms", ttft, "duration_ms", totalDuration)
	}

	if vkHash != "" && !clientDisconnected {
		totalTokens := promptTokens + completionTokens
		if err := h.virtualKeyRepo.AddTokens(r.Context(), vkHash, totalTokens); err != nil {
			keyLabel := vkHash
			if logData.virtualKeyName != "" {
				keyLabel = logData.virtualKeyName
			}
			events.Publish(events.Event{
				Type:     "tokens.error",
				Severity: "error",
				Message:  fmt.Sprintf("Token counting failed for key %q", keyLabel),
				Metadata: map[string]interface{}{"error": err.Error(), "key": keyLabel},
			})
		}
	}
}

func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, modelLookupMs, providerLookupMs, keyDecryptMs, ttft float64, vkHash string, attempt int) {
	defer func() { _ = resp.Body.Close() }()
	debuglog.Debug("proxy: handleNonStreamingResponse entered", "model", logData.modelID, "provider", logData.providerID, "upstream_status", resp.StatusCode, "attempt", attempt, "ttft_ms", ttft)

	w.Header().Set("Content-Type", "application/json")
	var chatResp ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err == nil {
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		var tps float64
		if chatResp.Usage.CompletionTokens > 0 && totalDuration > 0 {
			tps = float64(chatResp.Usage.CompletionTokens) / float64(totalDuration) * 1000
		}

		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		logData.providerLookupMs = providerLookupMs
		logData.keyDecryptMs = keyDecryptMs
		logData.ttftMs = ttft
		logData.tokensPerSecond = tps
		logData.tokensPrompt = chatResp.Usage.PromptTokens
		logData.tokensCompletion = chatResp.Usage.CompletionTokens
		if chatResp.Usage.PromptCacheHitTokens > 0 {
			logData.tokensPromptCacheHit = chatResp.Usage.PromptCacheHitTokens
			logData.tokensPromptCacheMiss = chatResp.Usage.PromptTokens - chatResp.Usage.PromptCacheHitTokens
		}
		logData.failoverAttempt = attempt
		logData.state = "completed"
		h.updateRequestLog(r.Context(), logData)

		if vkHash != "" {
			totalTokens := chatResp.Usage.PromptTokens + chatResp.Usage.CompletionTokens
			if err := h.virtualKeyRepo.AddTokens(r.Context(), vkHash, totalTokens); err != nil {
				keyLabel := vkHash
				if logData.virtualKeyName != "" {
					keyLabel = logData.virtualKeyName
				}
				events.Publish(events.Event{
					Type:     "tokens.error",
					Severity: "error",
					Message:  fmt.Sprintf("Token counting failed for key %q", keyLabel),
					Metadata: map[string]interface{}{"error": err.Error(), "key": keyLabel},
				})
			}
		}

		if err := json.NewEncoder(w).Encode(chatResp); err != nil {
			debuglog.Error("proxy: failed to encode response", "error", err)
		}
		debuglog.Info("proxy: non-streaming completed", "model", logData.modelID, "provider", logData.providerID, "attempt", attempt, "status", resp.StatusCode, "duration_ms", totalDuration, "prompt_tokens", chatResp.Usage.PromptTokens, "completion_tokens", chatResp.Usage.CompletionTokens)
	} else {
		body, _ := io.ReadAll(resp.Body)
		errMsg := util.SanitizeLogBody(string(body), 500)
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		logData.providerLookupMs = providerLookupMs
		logData.keyDecryptMs = keyDecryptMs
		logData.ttftMs = ttft
		logData.errorMessage = fmt.Sprintf("response decode error: %s", errMsg)
		logData.failoverAttempt = attempt
		logData.state = "failed"
		h.updateRequestLog(r.Context(), logData)
		debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerID)
		debuglog.Debug("proxy: non-streaming error details", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerID, "error", errMsg, "duration_ms", totalDuration)
		writeOpenAIError(w, fmt.Sprintf("upstream provider returned HTTP %d", resp.StatusCode), resp.StatusCode)
	}
}

func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	startTime := time.Now()

	parseStart := time.Now()
	var bodyBytes []byte
	if cached, ok := r.Context().Value(ctxkeys.RequestBodyKey).([]byte); ok {
		bodyBytes = cached
	} else {
		var err error
		bodyBytes, err = io.ReadAll(r.Body)
		if err != nil {
			debuglog.Warn("proxy: failed to read request body", "error", err)
			writeOpenAIError(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
	}

	var req ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		debuglog.Warn("proxy: failed to parse request body", "error", err)
		writeOpenAIError(w, "invalid request body", http.StatusBadRequest)
		return
	}
	parseMs := float64(time.Since(parseStart).Microseconds()) / 1000.0

	if req.Model == "" {
		writeOpenAIError(w, "model is required", http.StatusBadRequest)
		return
	}

	vkName := ""
	var vkID string
	var vkHash string
	if v := r.Context().Value(virtualKeyNameKey); v != nil {
		vkName = v.(string)
	}
	if v := r.Context().Value(virtualKeyIDKey); v != nil {
		vkID = v.(string)
	}
	if v := r.Context().Value(VirtualKeyHashKey); v != nil {
		vkHash = v.(string)
	}

	debuglog.Info("proxy: request start", "model", req.Model, "stream", req.Stream, "key", vkName)
	debuglog.Debug("proxy: request details", "model", req.Model, "stream", req.Stream, "key", vkName, "vk_id", vkID, "has_hash", vkHash != "", "body_length", len(bodyBytes))

	logData := &requestLogData{
		modelID:         req.Model,
		streaming:       req.Stream,
		virtualKeyName:  vkName,
		virtualKeyID:    vkID,
		failoverAttempt: 0,
		state:           "pending",
	}
	if err := h.insertRequestLog(r.Context(), logData); err != nil {
		debuglog.Error("proxy: failed to insert initial request log", "error", err)
	}

	var candidates []modelCandidate
	var timings resolveTimings
	var err error

	if strings.HasPrefix(req.Model, "hotel/") {
		debuglog.Debug("proxy: model resolution path", "type", "hotel", "model", req.Model)
		displayModel := strings.TrimPrefix(req.Model, "hotel/")
		candidates, timings, err = h.resolveHotelModel(r.Context(), displayModel)
		if err != nil {
			logData.statusCode = 404
			logData.errorMessage = err.Error()
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.parseMs = parseMs
			logData.state = "failed"
			h.updateRequestLog(r.Context(), logData)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return
		}
		if len(candidates) == 0 {
			logData.statusCode = 502
			logData.errorMessage = "no available provider for hotel/" + displayModel
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.parseMs = parseMs
			logData.state = "failed"
			h.updateRequestLog(r.Context(), logData)
			writeOpenAIError(w, "no available provider for hotel/"+displayModel, http.StatusBadGateway)
			return
		}
	} else if strings.Contains(req.Model, "/") && !strings.HasPrefix(req.Model, "hotel/") {
		debuglog.Debug("proxy: model resolution path", "type", "specific_provider", "model", req.Model)
		parts := strings.SplitN(req.Model, "/", 2)
		if len(parts) != 2 {
			logData.statusCode = 400
			logData.errorMessage = "invalid model format"
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.parseMs = parseMs
			logData.state = "failed"
			h.updateRequestLog(r.Context(), logData)
			writeOpenAIError(w, "invalid model format, expected provider/model", http.StatusBadRequest)
			return
		}
		providerName, modelID := parts[0], parts[1]
		candidates, timings, err = h.resolveSpecificProvider(r.Context(), providerName, modelID)
		if err != nil {
			logData.statusCode = 404
			logData.errorMessage = err.Error()
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.parseMs = parseMs
			logData.state = "failed"
			h.updateRequestLog(r.Context(), logData)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return
		}
	} else {
		logData.statusCode = 400
		logData.errorMessage = "invalid model format: " + req.Model
		logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.parseMs = parseMs
		logData.state = "failed"
		h.updateRequestLog(r.Context(), logData)
		writeOpenAIError(w, "invalid model format, expected provider/model or hotel/model", http.StatusBadRequest)
		return
	}

	if len(candidates) == 0 {
		logData.statusCode = 404
		logData.errorMessage = "model not found or disabled"
		logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.parseMs = parseMs
		logData.modelLookupMs = timings.modelLookupMs
		logData.state = "failed"
		h.updateRequestLog(r.Context(), logData)
		writeOpenAIError(w, "model not found or disabled", http.StatusNotFound)
		return
	}

	proxyOverhead := parseMs + timings.modelLookupMs + timings.providerLookupMs + timings.keyDecryptMs
	debuglog.Debug("proxy: model resolved", "model", req.Model, "candidates", len(candidates), "overhead_ms", proxyOverhead)

	var proxyReqBody []byte
	if req.Stream {
		var raw map[string]interface{}
		if json.Unmarshal(bodyBytes, &raw) == nil {
			raw["stream_options"] = map[string]interface{}{
				"include_usage": true,
			}
			if b, err := json.Marshal(raw); err == nil {
				proxyReqBody = b
				debuglog.Debug("proxy: injected stream_options into request", "model", req.Model)
			}
		}
	}
	if proxyReqBody == nil {
		proxyReqBody = bodyBytes
	}

	var lastErr string
	for attempt, candidate := range candidates {
		// Exponential backoff between failover attempts: 0ms, 100ms, 200ms, 400ms...
		// Capped at 2s. First attempt (attempt=0) has no delay.
		if attempt > 0 {
			backoff := time.Duration(math.Min(float64(100*time.Millisecond)*math.Pow(2, float64(attempt-1)), float64(2*time.Second)))
			debuglog.Info("proxy: failover backoff", "backoff", backoff, "attempt", attempt+1)
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				debuglog.Info("proxy: client disconnected during failover backoff")
				logData.statusCode = 499
				logData.errorMessage = "client disconnected during failover"
				logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
				logData.proxyOverheadMs = proxyOverhead
				logData.parseMs = parseMs
				logData.modelLookupMs = timings.modelLookupMs
				logData.providerLookupMs = timings.providerLookupMs
				logData.keyDecryptMs = timings.keyDecryptMs
				logData.failoverAttempt = attempt - 1
				logData.state = "failed"
				h.updateRequestLog(r.Context(), logData)
				writeOpenAIError(w, "client disconnected", http.StatusRequestTimeout)
				return
			}
		}

		logData.providerID = candidate.provider.ID
		if attempt == 0 {
			debuglog.Info("proxy: routing to provider", "provider", candidate.provider.ID, "model", candidate.model.ModelID, "total_candidates", len(candidates))
		} else {
			debuglog.Info("proxy: failover attempt", "attempt", attempt+1, "provider", candidate.provider.ID, "model", candidate.model.ModelID)
		}
		debuglog.Debug("proxy: candidate details", "provider_id", candidate.provider.ID, "provider_name", candidate.provider.Name, "model_id", candidate.model.ModelID, "provider_type", provider.DetectProviderType(candidate.provider.BaseURL), "attempt", attempt+1, "total_candidates", len(candidates))
		go func(pid uuid.UUID) {
			defer func() {
				if r := recover(); r != nil {
					debuglog.Error("proxy: panic in TouchLastUsed (provider)", "error", r)
				}
			}()
			tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer tcancel()
			if err := h.providerRepo.TouchLastUsed(tctx, pid); err != nil {
				debuglog.Debug("proxy: failed to touch provider last-used", "error", err)
			}
		}(candidate.provider.ID)
		providerType := provider.DetectProviderType(candidate.provider.BaseURL)
		debuglog.Debug("proxy: detected provider type", "provider_type", providerType, "base_url", util.SanitizeBaseURL(candidate.provider.BaseURL))
		targetURL := buildProviderTargetURL(candidate.provider.BaseURL, providerType)
		debuglog.Debug("proxy: built target URL", "target_url", targetURL)

		upstreamBody := proxyReqBody
		needsRewrite := req.Model != candidate.model.ModelID || providerType == "anthropic"
		debuglog.Debug("proxy: request rewrite check", "needs_rewrite", needsRewrite, "request_model", req.Model, "resolved_model", candidate.model.ModelID, "provider_type", providerType)
		if needsRewrite {
			var raw map[string]interface{}
			if json.Unmarshal(proxyReqBody, &raw) == nil {
				if req.Model != candidate.model.ModelID {
					raw["model"] = candidate.model.ModelID
				}
				// Preemptively strip params known to be universally rejected per provider.
				// These are always unsupported and cause 400 errors if sent.
				// Learned rejections (from 400 auto-retry) are cached per provider+model below.
				if params, ok := providerUnsupportedParams[providerType]; ok {
					for _, p := range params {
						delete(raw, p)
					}
				}
				cacheKey := fmt.Sprintf("%s:%s", providerType, candidate.model.ModelID)
				if cached := getCachedRejectedParams(&h.deprecationCache, cacheKey); cached != nil {
					for param := range cached {
						delete(raw, param)
					}
				}
				if b, err := json.Marshal(raw); err == nil {
					upstreamBody = b
				}
			}
		}

		failoverCtx, failoverCancel := context.WithTimeout(r.Context(), 30*time.Second)
		proxyReq, err := http.NewRequestWithContext(failoverCtx, "POST", targetURL, bytes.NewReader(upstreamBody))
		if err != nil {
			failoverCancel()
			lastErr = fmt.Sprintf("attempt %d: failed to create request: %v", attempt, err)
			continue
		}

		setProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
		proxyReq.Header.Set("Content-Type", "application/json")
		debuglog.Debug("proxy: sending upstream request", "method", proxyReq.Method, "url", targetURL, "content_length", len(upstreamBody), "has_api_key", candidate.apiKey != "")

		// Reuse the shared upstream Transport instead of creating a new one
		// per request. A fresh Transport spawns persistent readLoop/writeLoop
		// goroutines per connection that only die after IdleConnTimeout, so
		// creating one per request causes unbounded goroutine growth.
		upstreamClient := &http.Client{
			Transport: h.upstreamTransport,
		}
		resp, err := upstreamClient.Do(proxyReq)
		failoverCancel() // context no longer needed after Do completes
		if err != nil {
			debuglog.Warn("proxy: upstream request failed", "attempt", attempt+1, "provider", candidate.provider.ID, "error", err)
			lastErr = fmt.Sprintf("attempt %d: provider error: %v", attempt, err)
			// Client-initiated cancellations and deadline exceeded are not
			// provider failures. If the caller disconnected (Canceled) or
			// the request timed out (DeadlineExceeded), we must not penalize
			// the circuit breaker for that.
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				if h.settingsRepo.GetBool(r.Context(), "circuit_breaker_enabled", true) {
					h.circuitBreaker.RecordFailure(candidate.provider.ID)
				}
			} else {
				debuglog.Info("proxy: client disconnected during request to provider", "provider", candidate.provider.ID, "model", req.Model)
			}
			continue
		}

		// Auto-retry param-rejection 400s: parse the error, learn which params
		// are rejected for this model, strip them, and retry once.
		// Works universally — any LLM API mentioning "temperature" or "top_p"
		// in a 400 error can only mean the sampling parameter.
		if resp.StatusCode == 400 {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			debuglog.Debug("proxy: received 400 from upstream, checking for param rejection", "provider", candidate.provider.ID, "model", candidate.model.ModelID, "body_length", len(body))
			// Restore the body so downstream error handling (line ~605) can read it
			// if we don't successfully retry. Must be set before any fallthrough.
			resp.Body = io.NopCloser(bytes.NewReader(body))
			if readErr == nil {
				if rejected := parseProviderParamError(body); rejected != nil {
					// Cache the learned rejections for future preemptive stripping.
					// Merge with any existing entries using CompareAndSwap to avoid
					// data races from concurrent goroutines mutating the same map.
					cacheKey := fmt.Sprintf("%s:%s", providerType, candidate.model.ModelID)
					for {
						existing, loaded := h.deprecationCache.LoadOrStore(cacheKey, rejected)
						if !loaded {
							// First entry for this key — we just stored 'rejected'.
							break
						}
						// Merge with existing, creating a new map to avoid data races.
						merged := make(map[string]bool)
						for k := range existing.(map[string]bool) {
							merged[k] = true
						}
						for k := range rejected {
							merged[k] = true
						}
						if h.deprecationCache.CompareAndSwap(cacheKey, existing, merged) {
							break
						}
						// CompareAndSwap failed — another goroutine updated it, retry.
					}
					// Rebuild the request body without rejected params
					var raw map[string]interface{}
					if json.Unmarshal(proxyReqBody, &raw) == nil {
						raw["model"] = candidate.model.ModelID
						for param := range rejected {
							delete(raw, param)
						}
						// Also strip provider-universally-rejected params on retry
						if params, ok := providerUnsupportedParams[providerType]; ok {
							for _, p := range params {
								delete(raw, p)
							}
						}
						if rebuilt, err := json.Marshal(raw); err == nil {
							retryCtx, retryCancel := context.WithTimeout(r.Context(), 30*time.Second)
							retryReq, retryErr := http.NewRequestWithContext(retryCtx, "POST", targetURL, bytes.NewReader(rebuilt))
							if retryErr != nil {
								retryCancel()
								lastErr = fmt.Sprintf("attempt %d: failed to create retry request: %v", attempt, retryErr)
								continue
							}
							setProviderAuthHeaders(retryReq, providerType, candidate.apiKey)
							retryReq.Header.Set("Content-Type", "application/json")
							retryClient := &http.Client{Transport: h.upstreamTransport}
							resp, retryErr = retryClient.Do(retryReq)
							retryCancel()
							if retryErr != nil {
								debuglog.Warn("proxy: auto-retry request failed", "attempt", attempt+1, "provider", candidate.provider.ID, "error", retryErr)
								lastErr = fmt.Sprintf("attempt %d: retry error: %v", attempt, retryErr)
								continue
							}
							// Successfully retried — fall through to normal response handling
							debuglog.Info("proxy: auto-retry succeeded", "model", candidate.model.ModelID, "rejected_params", mapKeys(rejected))
						}
					}
				}
			}
		}

		ttft := float64(time.Since(startTime).Microseconds()) / 1000.0

		hasMoreCandidates := attempt < len(candidates)-1
		isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

		if isFailoverEligible {
			// Upstream is unhealthy — record failure for circuit breaker.
			if h.settingsRepo.GetBool(r.Context(), "circuit_breaker_enabled", true) {
				h.circuitBreaker.RecordFailure(candidate.provider.ID)
			}
		} else {
			// Provider responded (even with a non-failover error like 400) —
			// it's alive from a health perspective.
			if h.settingsRepo.GetBool(r.Context(), "circuit_breaker_enabled", true) {
				h.circuitBreaker.RecordSuccess(candidate.provider.ID)
			}
		}

		shouldFailoverNow := isFailoverEligible && hasMoreCandidates
		debuglog.Debug("proxy: failover decision", "status", resp.StatusCode, "is_failover_eligible", isFailoverEligible, "has_more_candidates", hasMoreCandidates, "should_failover_now", shouldFailoverNow, "attempt", attempt+1)

		if shouldFailoverNow {
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Sprintf("attempt %d: HTTP %d", attempt, resp.StatusCode)
			debuglog.Info("proxy: failover triggered", "attempt", attempt+1, "provider", candidate.provider.ID, "status", resp.StatusCode)
			logData.failoverAttempt = attempt
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			errMsg := util.SanitizeLogBody(string(body), 2000)
			debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode, "model", req.Model, "provider", candidate.provider.ID, "body", errMsg)
			debuglog.Debug("proxy: upstream error response", "status", resp.StatusCode, "model", req.Model, "provider", candidate.provider.ID, "body_length", len(body), "attempt", attempt+1)
			logData.statusCode = resp.StatusCode
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.proxyOverheadMs = proxyOverhead
			logData.parseMs = parseMs
			logData.modelLookupMs = timings.modelLookupMs
			logData.providerLookupMs = timings.providerLookupMs
			logData.keyDecryptMs = timings.keyDecryptMs
			logData.ttftMs = ttft
			logData.errorMessage = errMsg
			logData.failoverAttempt = attempt
			logData.state = "failed"
			h.updateRequestLog(r.Context(), logData)
			// Forward the upstream error to the client. If the upstream returned
			// valid JSON (most OpenAI-compatible providers do), pass it through
			// as-is. If it's not JSON (e.g. plain text, HTML error page), wrap it
			// in an OpenAI-compatible error envelope so clients like SillyTavern
			// don't crash on JSON.parse.
			if json.Valid(body) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				_, _ = w.Write(body)
			} else {
				writeOpenAIError(w, errMsg, resp.StatusCode)
			}
			return
		}

		debuglog.Debug("proxy: upstream responded OK, dispatching to handler", "stream", req.Stream, "model", req.Model, "provider", candidate.provider.ID, "status", resp.StatusCode)
		if req.Stream {
			h.handleStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, timings.modelLookupMs, timings.providerLookupMs, timings.keyDecryptMs, ttft, vkHash, attempt)
			return
		}

		h.handleNonStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, timings.modelLookupMs, timings.providerLookupMs, timings.keyDecryptMs, ttft, vkHash, attempt)
		return
	}

	debuglog.Error("proxy: all providers exhausted", "model", req.Model, "error", lastErr)
	logData.providerID = uuid.Nil
	logData.statusCode = 502
	logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.modelLookupMs = timings.modelLookupMs
	logData.providerLookupMs = timings.providerLookupMs
	logData.keyDecryptMs = timings.keyDecryptMs
	logData.errorMessage = fmt.Sprintf("all providers failed: %s", lastErr)
	logData.failoverAttempt = len(candidates) - 1
	logData.state = "failed"
	h.updateRequestLog(r.Context(), logData)
	writeOpenAIError(w, fmt.Sprintf("all providers failed for model %s", req.Model), http.StatusBadGateway)
}

// buildProviderTargetURL constructs the full upstream URL for a given provider.
// Most providers use base + "/chat/completions" but Anthropic needs "/v1/chat/completions"
// because its base URL (https://api.anthropic.com) lacks the /v1 prefix.
// Defensive: if the base URL already ends with /v1, don't double-append it.
func buildProviderTargetURL(baseURL, providerType string) string {
	sanitized := util.SanitizeBaseURL(baseURL)
	switch providerType {
	case "anthropic":
		// Avoid double /v1 if the user configured https://api.anthropic.com/v1
		if strings.HasSuffix(sanitized, "/v1") {
			return sanitized + "/chat/completions"
		}
		return sanitized + "/v1/chat/completions"
	default:
		return sanitized + "/chat/completions"
	}
}

// setProviderAuthHeaders sets the correct authentication headers for each provider type.
// - Anthropic: x-api-key + anthropic-version (no Bearer auth)
// - All others: standard Authorization: Bearer header
func setProviderAuthHeaders(req *http.Request, providerType, apiKey string) {
	if apiKey == "" {
		return
	}
	switch providerType {
	case "anthropic":
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	default:
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
}

// getCachedRejectedParams returns params known to be rejected for a provider+model,
// learned from previous 400 responses.
func getCachedRejectedParams(cache *sync.Map, cacheKey string) map[string]bool {
	if v, ok := cache.Load(cacheKey); ok {
		if m, ok := v.(map[string]bool); ok {
			return m
		}
	}
	return nil
}

// parseProviderParamError parses 400 error bodies for rejected sampling/param names.
// Any LLM API mentioning these param names in a 400 error can only be referring
// to the request parameter — there is no other meaning in this context.
// This works universally across all providers, not just Anthropic.
func parseProviderParamError(body []byte) map[string]bool {
	var errResp struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &errResp) != nil {
		return nil
	}
	msg := errResp.Error.Message
	rejected := make(map[string]bool)

	// "cannot both be specified" — strip top_p, keep temperature
	if strings.Contains(msg, "cannot both be specified") {
		rejected["top_p"] = true
	}
	// Known sampling/optional params that providers commonly reject.
	// We match against backtick-wrapped names (e.g. `top_p`) and quote-wrapped
	// names (e.g. "top_p") to avoid false positives from substring matching.
	// Short/common words like "n", "stop", "seed" are NOT matched loosely
	// because they appear in many unrelated error messages.
	matchParams := []string{
		"temperature", "top_p", "top_k", "top_a",
		"frequency_penalty", "presence_penalty",
		"logprobs", "top_logprobs",
		"max_tokens", "stream_options", "reasoning_effort",
	}
	for _, p := range matchParams {
		// Match backtick-wrapped: `param` or quote-wrapped: "param"
		if strings.Contains(msg, "`"+p+"`") || strings.Contains(msg, "\""+p+"\"") {
			rejected[p] = true
		}
	}
	// "stop", "n", "seed" are too common as substrings — only match when
	// explicitly quoted or backticked in the error message.
	for _, p := range []string{"stop", "n", "seed"} {
		if strings.Contains(msg, "`"+p+"`") || strings.Contains(msg, "\""+p+"\"") {
			rejected[p] = true
		}
	}
	// Also catch any top_{single_letter} variant when backtick/quote-wrapped
	if idx := strings.Index(msg, "`top_"); idx >= 0 && idx+7 <= len(msg) {
		c := msg[idx+5]
		if c >= 'a' && c <= 'z' && msg[idx+6] == '`' {
			rejected[msg[idx+1:idx+6]] = true
		}
	}
	if idx := strings.Index(msg, "\"top_"); idx >= 0 && idx+7 <= len(msg) {
		c := msg[idx+5]
		if c >= 'a' && c <= 'z' && msg[idx+6] == '"' {
			rejected[msg[idx+1:idx+6]] = true
		}
	}
	if len(rejected) == 0 {
		return nil
	}
	return rejected
}

// mapKeys returns the keys of a map[string]bool for logging.
func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// writeOpenAIError writes an OpenAI-compatible JSON error response.
// All proxy error responses must be JSON, not plain text, because clients like
// SillyTavern parse responses as JSON and crash on plain text error messages.
func writeOpenAIError(w http.ResponseWriter, message string, statusCode int) {
	util.WriteOpenAIError(w, message, statusCode)
}
