package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, modelLookupMs, providerLookupMs, keyDecryptMs, ttft float64, vkHash string, attempt int) {
	defer func() { _ = resp.Body.Close() }()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

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
	var promptTokens, completionTokens int
	var promptCacheHitTokens, promptCacheMissTokens int
	var lastErrMsg string
	clientDisconnected := false

	for scanner.Scan() {
		line := scanner.Bytes()

		select {
		case <-r.Context().Done():
			clientDisconnected = true
			goto logUpdate
		default:
		}

		if canFlush {
			flusher.Flush()
		}
		_, _ = w.Write(line)
		_, _ = w.Write([]byte("\n"))
		if canFlush {
			flusher.Flush()
		}

		if strings.HasPrefix(string(line), "data: ") {
			payload := strings.TrimPrefix(string(line), "data: ")
			if payload == "[DONE]" {
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
		log.Printf("[proxy] warning: client disconnected during streaming, model=%s", logData.modelID)
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

	if errMsg == "" {
		log.Printf("[proxy] streaming completed, model=%s provider=%s attempt=%d ttft=%.1fms duration=%.1fms", logData.modelID, logData.providerID, attempt, ttft, totalDuration)
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
			log.Printf("[proxy] error: failed to encode response: %v", err)
		}
	} else {
		body, _ := io.ReadAll(resp.Body)
		errMsg := string(body)
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
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
		log.Printf("[proxy] warning: upstream non-200 status=%d model=%s provider=%s", resp.StatusCode, logData.modelID, logData.providerID)
		http.Error(w, fmt.Sprintf("upstream provider returned HTTP %d", resp.StatusCode), resp.StatusCode)
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
			log.Printf("[proxy] warning: failed to read request body: %v", err)
			http.Error(w, "failed to read request body", http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
	}

	var req ChatCompletionRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		log.Printf("[proxy] warning: failed to parse request body: %v", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	parseMs := float64(time.Since(parseStart).Microseconds()) / 1000.0

	if req.Model == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
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

	log.Printf("[proxy] request start model=%s stream=%v key=%q", req.Model, req.Stream, vkName)

	logData := &requestLogData{
		modelID:         req.Model,
		streaming:       req.Stream,
		virtualKeyName:  vkName,
		virtualKeyID:    vkID,
		failoverAttempt: 0,
		state:           "pending",
	}
	if err := h.insertRequestLog(r.Context(), logData); err != nil {
		log.Printf("[proxy] error: failed to insert initial request log: %v", err)
	}

	var candidates []modelCandidate
	var timings resolveTimings
	var err error

	if strings.HasPrefix(req.Model, "hotel/") {
		displayModel := strings.TrimPrefix(req.Model, "hotel/")
		candidates, timings, err = h.resolveHotelModel(r.Context(), displayModel)
		if err != nil {
			logData.statusCode = 404
			logData.errorMessage = err.Error()
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.parseMs = parseMs
			logData.state = "failed"
			h.updateRequestLog(r.Context(), logData)
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		if len(candidates) == 0 {
			logData.statusCode = 502
			logData.errorMessage = "no available provider for hotel/" + displayModel
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.parseMs = parseMs
			logData.state = "failed"
			h.updateRequestLog(r.Context(), logData)
			http.Error(w, "no available provider for hotel/"+displayModel, http.StatusBadGateway)
			return
		}
	} else if strings.Contains(req.Model, "/") && !strings.HasPrefix(req.Model, "hotel/") {
		parts := strings.SplitN(req.Model, "/", 2)
		if len(parts) != 2 {
			logData.statusCode = 400
			logData.errorMessage = "invalid model format"
			logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
			logData.parseMs = parseMs
			logData.state = "failed"
			h.updateRequestLog(r.Context(), logData)
			http.Error(w, "invalid model format, expected provider/model", http.StatusBadRequest)
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
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	} else {
		logData.statusCode = 400
		logData.errorMessage = "invalid model format: " + req.Model
		logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.parseMs = parseMs
		logData.state = "failed"
		h.updateRequestLog(r.Context(), logData)
		http.Error(w, "invalid model format, expected provider/model or hotel/model", http.StatusBadRequest)
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
		http.Error(w, "model not found or disabled", http.StatusNotFound)
		return
	}

	proxyOverhead := parseMs + timings.modelLookupMs + timings.providerLookupMs + timings.keyDecryptMs

	var proxyReqBody []byte
	if req.Stream {
		var raw map[string]interface{}
		if json.Unmarshal(bodyBytes, &raw) == nil {
			raw["stream_options"] = map[string]interface{}{
				"include_usage": true,
			}
			if b, err := json.Marshal(raw); err == nil {
				proxyReqBody = b
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
			log.Printf("[proxy] failover backoff: waiting %v before attempt %d", backoff, attempt+1)
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				log.Printf("[proxy] client disconnected during failover backoff")
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
				http.Error(w, "client disconnected", http.StatusRequestTimeout)
				return
			}
		}

		logData.providerID = candidate.provider.ID
		log.Printf("[proxy] failover attempt=%d provider=%s model=%s", attempt+1, candidate.provider.ID, candidate.model.ModelID)
		go func(pid uuid.UUID) {
			tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer tcancel()
			_ = h.providerRepo.TouchLastUsed(tctx, pid)
		}(candidate.provider.ID)
		targetURL := util.SanitizeBaseURL(candidate.provider.BaseURL) + "/chat/completions"

		upstreamBody := proxyReqBody
		if req.Model != candidate.model.ModelID {
			var raw map[string]interface{}
			if json.Unmarshal(proxyReqBody, &raw) == nil {
				raw["model"] = candidate.model.ModelID
				if b, err := json.Marshal(raw); err == nil {
					upstreamBody = b
				}
			}
		}

		failoverCtx := r.Context()
		proxyReq, err := http.NewRequestWithContext(failoverCtx, "POST", targetURL, bytes.NewReader(upstreamBody))
		if err != nil {
			lastErr = fmt.Sprintf("attempt %d: failed to create request: %v", attempt, err)
			continue
		}
		if candidate.apiKey != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+candidate.apiKey)
		}
		proxyReq.Header.Set("Content-Type", "application/json")

		// Reuse the shared upstream Transport instead of creating a new one
		// per request. A fresh Transport spawns persistent readLoop/writeLoop
		// goroutines per connection that only die after IdleConnTimeout, so
		// creating one per request causes unbounded goroutine growth.
		upstreamClient := &http.Client{
			Transport: h.upstreamTransport,
		}
		resp, err := upstreamClient.Do(proxyReq)
		if err != nil {
			lastErr = fmt.Sprintf("attempt %d: provider error: %v", attempt, err)
			if h.settingsRepo.GetBool(r.Context(), "circuit_breaker_enabled", true) {
				h.circuitBreaker.RecordFailure(candidate.provider.ID)
			}
			continue
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

		if shouldFailoverNow {
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Sprintf("attempt %d: HTTP %d", attempt, resp.StatusCode)
			log.Printf("[proxy] failover triggered: attempt=%d provider=%s status=%d", attempt+1, candidate.provider.ID, resp.StatusCode)
			logData.failoverAttempt = attempt
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			errMsg := util.SanitizeLogBody(string(body), 500)
			log.Printf("[proxy] warning: upstream non-200 status=%d model=%s provider=%s", resp.StatusCode, req.Model, candidate.provider.ID)
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
			http.Error(w, fmt.Sprintf("upstream provider returned HTTP %d", resp.StatusCode), resp.StatusCode)
			return
		}

		if req.Stream {
			h.handleStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, timings.modelLookupMs, timings.providerLookupMs, timings.keyDecryptMs, ttft, vkHash, attempt)
			return
		}

		h.handleNonStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, timings.modelLookupMs, timings.providerLookupMs, timings.keyDecryptMs, ttft, vkHash, attempt)
		return
	}

	log.Printf("[proxy] error: all providers exhausted for model=%s: %s", req.Model, lastErr)
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
	http.Error(w, fmt.Sprintf("all providers failed for model %s", req.Model), http.StatusBadGateway)
}
