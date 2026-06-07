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
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// newRequestWithContext is injectable for testing request creation errors.
var newRequestWithContext = http.NewRequestWithContext

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, opts streamOptions) {

	// Progressive stall timeout (progressiveChunkThreshold /
	// progressiveStallMultiplier, package consts): after this many chunks the
	// stream is clearly alive — extend the watchdog timeout to tolerate
	// tool-call pauses and long reasoning chains.

	defer func() {
		// Drain remaining bytes so the Transport reuses the connection.
		// Skip drain if the client already disconnected: the upstream body
		// could be large and we'd block the goroutine for no benefit.
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	debuglog.Debug("proxy: handleStreamingResponse entered", "model", logData.modelID, "provider", logData.providerName, "upstream_status", resp.StatusCode, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "true_ttft_ms", opts.trueTtftMs, "has_probe_buf", opts.preReadBuf != nil)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	debuglog.Debug("proxy: streaming headers sent", "model", logData.modelID, "provider", logData.providerName)

	logData.statusCode = resp.StatusCode
	logData.proxyOverheadMs = opts.proxyOverheadMs
	logData.parseMs = opts.parseMs
	logData.failoverLookupMs = opts.failoverLookupMs
	logData.modelLookupMs = opts.modelLookupMs
	logData.providerLookupMs = opts.providerLookupMs
	logData.keyDecryptMs = opts.keyDecryptMs
	logData.dialMs = opts.dialMs
	logData.settingsReadMs = opts.settingsReadMs
	logData.responseHeaderMs = opts.responseHeaderMs
	logData.ttftMs = opts.trueTtftMs
	logData.failoverAttempt = opts.attempt
	logData.state = "streaming"
	// Fire-and-forget: the interim "streaming" state update runs before
	// the first streamed byte. Blocking on WaitForInsert (up to 5s) would
	// delay the client. The final update (completed/failed) waits properly.
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

	// streamSink owns w/flusher and the running bytesWritten total (Phase 1
	// of the streaming-pipeline refactor). All emit paths go through it.
	sink := newStreamSink(w)

	// streamReader owns the scanner (replaying the TTFT probe buffer), the
	// stall watchdog, chunk counting, the empty-line limit, client-disconnect
	// detection, BOM/CR cleanup, and SSE classification (Phase 3). It yields
	// classified sseEvents; this orchestrator owns emits and transforms.
	reader := newStreamReader(r.Context(), resp.Body, opts, logData)

	// st accumulates the per-stream metrics, carry flags, and observer state
	// (Phase 4 §6 migration). Created before the loop and mutated in place so
	// the transforms/observers and the finalizer share one named contract
	// instead of a fistful of loop-locals. The stall flag and final chunkCount
	// are filled from the reader at logUpdate.
	st := &streamState{}
	// Periodic streaming progress logging (every 50 chunks) to give
	// visibility into stream health without flooding logs.
	const chunkLogInterval = 50
	// Read strip_reasoning flag from context once before the scanner loop.
	// The value is set by ProxyKeyMiddleware and never changes mid-stream.
	stripReasoning := false
	if v := r.Context().Value(ctxkeys.VirtualKeyStripReasoningKey); v != nil {
		if sr, ok := v.(bool); ok {
			stripReasoning = sr
		}
	}
	debuglog.Debug("proxy: strip_reasoning flag", "enabled", stripReasoning, "model", logData.modelID, "provider", logData.providerName)

	for {
		ev, ok := reader.Next()
		if !ok {
			// Reader stopped: disconnect (skip the stream-end error flush,
			// matching the prior goto), empty-line abort, or normal EOF.
			if reader.disconnected {
				st.clientDisconnected = true
				goto logUpdate
			}
			if reader.abortErrMsg != "" {
				st.lastErrMsg = reader.abortErrMsg
			}
			break
		}
		chunkCount := reader.chunkCount
		line := ev.raw

		// Periodic streaming progress log for observability.
		if chunkCount%chunkLogInterval == 0 {
			debuglog.Debug("proxy: streaming progress", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten, "prompt_tokens", st.promptTokens, "completion_tokens", st.completionTokens, "thinking", st.sawThinking)
		}

		if ev.kind == sseBlank {
			// When strip_reasoning skips a reasoning chunk, the SSE
			// separator (empty line) that followed it must also be
			// suppressed. Bare \n events break parsers like openai-go's
			// ssestream (Warp's backend). Only forward the separator
			// when the preceding data line was actually forwarded.
			if sink.swallowBlank {
				sink.swallowBlank = false
				continue
			}
			// Forward empty lines — they are SSE event separators required by
			// the spec. Clients like eventsource-parser dispatch events on
			// blank lines; omitting them causes all data lines to be
			// concatenated into one invalid event.
			if err := sink.write([]byte("\n")); err != nil {
				st.clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream (blank line)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
				goto logUpdate
			}
			sink.flush()
			continue
		}

		if ev.kind == sseComment {
			// Not a data line — an SSE comment (": ..."), an event/id/retry
			// directive, etc. Pass through without parsing.
			lineStr := ev.clean
			// P1-C: Detect Anthropic-style "event: error" lines for logging.
			// Anthropic streams use typed events like:
			//   event: error
			//   data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}
			// We track "event: error" so the next data line is known to be an
			// error payload, allowing us to extract the message for logging.
			if strings.HasPrefix(lineStr, "event:") {
				evt := strings.TrimSpace(lineStr[6:])
				if evt == "error" {
					st.lastAnthropicEvent = "error"
				} else {
					st.lastAnthropicEvent = ""
				}
			}
			// Flush any accumulated error when a non-data line arrives
			// (the error payload has already been captured in the data line).
			st.flushAccumulatedError(chunkCount, logData)
			if err := sink.write(line); err != nil {
				st.clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
				goto logUpdate
			}
			if err := sink.write([]byte("\n")); err != nil {
				st.clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
				goto logUpdate
			}
			sink.flush()
			continue
		}

		if ev.kind == sseDone {
			st.sawDone = true
			// Write [DONE] sentinel to the downstream client.
			if err := sink.write(line); err != nil {
				st.clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
				goto logUpdate
			}
			if err := sink.write([]byte("\n\n")); err != nil {
				st.clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
				goto logUpdate
			}
			sink.flush()
			debuglog.Debug("proxy: received [DONE] sentinel", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
			break
		}

		// ev.kind == sseData — parse, transform, observe, and forward the chunk.
		if h.handleDataChunk(sink, st, ev, stripReasoning, chunkCount, logData) {
			goto logUpdate
		}
	}

	// Flush any remaining accumulated error bytes at stream end.
	if len(st.errAccum) > 0 {
		if accumulatedMsg := parseAccumulatedError(st.errAccum); accumulatedMsg != "" {
			st.lastErrMsg = accumulatedMsg
			st.errorChunkCount++
			debuglog.Warn("proxy: accumulated SSE error (stream end)", "error_message", accumulatedMsg, "model", logData.modelID, "provider", logData.providerName, "chunks", reader.chunkCount)
		}
	}

logUpdate:
	// Stop the watchdog before reading its stall flag, matching the prior
	// inline ordering (close, then read the atomic).
	reader.Close()
	// st was accumulated in place by the loop; fill the reader-owned fields the
	// loop couldn't (final chunk count and the stall flag, read after watchdog
	// teardown), then finalize.
	st.chunkCount = reader.chunkCount
	st.stalled = reader.stalled()
	h.finalizeStream(st, sink, reader.err(), logData, opts, resp.StatusCode, startTime)
}

// handleDataChunk processes one sseData event end-to-end: capture split/Anthropic
// SSE errors (P1-B/P1-C), parse the chunk, run the transforms (strip_reasoning,
// reasoning-normalize, empty-content-strip, finish_reason) and the side-channel
// observers, then forward. It emits at most once; the `written` flag and the
// whole transform dispatch are encapsulated here. Returns stop=true when a client
// write failed (the caller jumps to finalize), false otherwise (advance to the
// next event).
func (h *Handler) handleDataChunk(sink *streamSink, st *streamState, ev sseEvent, stripReasoning bool, chunkCount int, logData *requestLogData) (stop bool) {
	payload := ev.payload
	line := ev.raw

	// Capture split (P1-B) and Anthropic typed (P1-C) SSE errors into streamState.
	// anthropicErrorCounted prevents the chunk.Error observer from double-counting.
	anthropicErrorCounted := st.captureSSEError(payload, &st.lastAnthropicEvent, chunkCount, logData)

	var written bool
	var chunk streamChunk
	jsonValid := json.Unmarshal([]byte(payload), &chunk) == nil
	if jsonValid {
		// Side-channel observers (usage, native_finish_reason, repeated content,
		// chunk.Error) run for EVERY valid chunk — BEFORE the transforms, which may
		// emit-and-return early (strip_reasoning keep-alive/forward). Running them
		// here is what keeps usage/token metering from being silently dropped when a
		// provider rides `usage` on the same chunk as a reasoning delta. They read
		// the immutable typed chunk and never emit, so position doesn't affect output.
		st.observeDataChunk(chunk, anthropicErrorCounted, chunkCount, logData)

		// strip_reasoning: drop reasoning-only deltas (keep-alive) or forward the
		// stripped chunk. See computeStripReasoning.
		if stripReasoning && len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			switch decision, newPayload := computeStripReasoning(payload, &st.lastFinishReason, logData); decision {
			case stripPassthrough:
				// Payload didn't parse — leave it for the later blocks.
				goto stripReasoningDone
			case stripDrop:
				// Keep-alive marshal failed (practically unreachable) — drop.
				return false
			case stripKeepalive:
				if err := sink.writeData(newPayload); err != nil {
					st.clientDisconnected = true
					debuglog.Warn("proxy: client write failed during reasoning keep-alive", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
					return true
				}
				sink.flush()
				return false
			case stripForward:
				if err := sink.writeData(newPayload); err != nil {
					st.clientDisconnected = true
					debuglog.Warn("proxy: client write failed during reasoning strip", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
					return true
				}
				sink.flush()
				return false
			}
		}
	stripReasoningDone:

		// Reasoning field normalization: ensure reasoning_content is populated
		// regardless of upstream format (Ollama reasoning, OpenRouter/MiniMax
		// reasoning_details, <thinking> tags in content).
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			delta := chunk.Choices[0].Delta
			if newPayload, ok := normalizeReasoningChunk(delta.Content, delta.ReasoningContent, payload, &st.lastFinishReason, logData); ok {
				if err := sink.writeData(newPayload); err != nil {
					st.clientDisconnected = true
					debuglog.Warn("proxy: client write failed during reasoning normalization", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
					return true
				}
				sink.flush()
				written = true
				debuglog.Debug("proxy: normalized reasoning fields", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
			}
		}

		// Strip the noise content:"" that accompanies reasoning-only deltas.
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			delta := chunk.Choices[0].Delta
			hasReasoning := delta.ReasoningContent != nil && *delta.ReasoningContent != ""
			hasEmptyContent := delta.Content != nil && *delta.Content == ""
			if hasReasoning && hasEmptyContent {
				if newPayload, ok := stripEmptyReasoningContent(payload, &st.lastFinishReason, logData); ok {
					if err := sink.writeData(newPayload); err != nil {
						st.clientDisconnected = true
						debuglog.Warn("proxy: client write failed during empty content strip", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
						return true
					}
					sink.flush()
					written = true
					debuglog.Debug("proxy: stripped empty content from reasoning chunk", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
				}
			}
		}

		// Normalize provider finish_reason and suppress P2-2 bare duplicates.
		switch decision, newPayload := computeFinishReason(chunk, payload, &st.lastFinishReason); decision {
		case finishSuppress:
			debuglog.Debug("proxy: suppressing duplicate finish_reason chunk", "finish_reason", normalizeFinishReason(*chunk.Choices[0].FinishReason), "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
			sink.swallowBlank = true
			return false
		case finishRewrite:
			// Only emit if an earlier transform hasn't already written.
			if !written {
				if err := sink.writeData(newPayload); err != nil {
					st.clientDisconnected = true
					debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
					return true
				}
				sink.flush()
				written = true
				debuglog.Debug("proxy: normalized finish_reason", "original", *chunk.Choices[0].FinishReason, "normalized", normalizeFinishReason(*chunk.Choices[0].FinishReason), "model", logData.modelID, "provider", logData.providerName)
			}
		case finishNone:
		}
	}
	if !written && !jsonValid {
		// Drop invalid/truncated JSON instead of forwarding broken bytes.
		preview := payload
		if len(preview) > 80 {
			runes := []rune(preview)
			if len(runes) > 80 {
				preview = string(runes[:80]) + "..."
			}
		}
		debuglog.Warn("proxy: skipping invalid JSON chunk from upstream",
			"model", logData.modelID, "provider", logData.providerName,
			"chunk_number", chunkCount, "payload_preview", preview)
		sink.swallowBlank = true
		return false
	}
	if !written {
		// No transform applied — forward the original line verbatim (preserves
		// upstream framing like LM Studio's no-space "data:").
		if err := sink.write(line); err != nil {
			st.clientDisconnected = true
			debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
			return true
		}
		if err := sink.write([]byte("\n\n")); err != nil {
			st.clientDisconnected = true
			debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
			return true
		}
		sink.flush()
		sink.swallowBlank = true
	}
	return false
}

func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, failoverLookupMs, modelLookupMs, providerLookupMs, keyDecryptMs, dialMs, settingsReadMs, responseHeaderMs float64, vkHash string, attempt int) {
	defer func() {
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	debuglog.Debug("proxy: handleNonStreamingResponse entered", "model", logData.modelID, "provider", logData.providerName, "upstream_status", resp.StatusCode, "attempt", attempt, "response_header_ms", responseHeaderMs)

	w.Header().Set("Content-Type", "application/json")
	var chatResp ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err == nil {
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		var tps float64
		var reasoningTokens int
		if chatResp.Usage.CompletionTokensDetails != nil && chatResp.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			reasoningTokens = chatResp.Usage.CompletionTokensDetails.ReasoningTokens
		}
		totalOutputTokens := chatResp.Usage.CompletionTokens + reasoningTokens
		generationDuration := totalDuration - responseHeaderMs
		// Avoid absurd TPS when generation time is negligible
		// (e.g. non-streaming where response_header_ms ≈ duration_ms).
		minGeneration := max(1.0, totalDuration*0.05)
		if totalOutputTokens > 0 && generationDuration >= minGeneration {
			tps = float64(totalOutputTokens) / float64(generationDuration) * 1000
		} else if totalOutputTokens > 0 && totalDuration > 0 {
			tps = float64(totalOutputTokens) / float64(totalDuration) * 1000
		}

		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		logData.providerLookupMs = providerLookupMs
		logData.keyDecryptMs = keyDecryptMs
		logData.failoverLookupMs = failoverLookupMs
		logData.dialMs = dialMs
		logData.settingsReadMs = settingsReadMs
		logData.responseHeaderMs = responseHeaderMs
		logData.tokensPerSecond = tps
		logData.tokensPrompt = chatResp.Usage.PromptTokens
		logData.tokensCompletion = chatResp.Usage.CompletionTokens
		logData.tokensCompletionReasoning = reasoningTokens
		logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss = extractCacheTokens(chatResp.Usage)
		logData.failoverAttempt = attempt
		logData.state = "completed"
		// Fire-and-forget: skip WaitForInsert to avoid blocking TTFB.
		// The async INSERT is very likely complete by now; if not, the
		// UPDATE simply affects 0 rows (harmless, logged as warning).
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})

		if vkHash != "" {
			h.recordTokenUsage(vkHash, chatResp.Usage.PromptTokens, chatResp.Usage.CompletionTokens, reasoningTokens, logData.virtualKeyName)
		}

		// Normalize reasoning fields in the response message so that
		// reasoning_content is always populated regardless of upstream
		// provider format (Ollama's reasoning, OpenRouter's reasoning_details,
		// MiniMax's <thinking> tags in content).
		for i := range chatResp.Choices {
			msg := &chatResp.Choices[i].Message
			// Rule 1: reasoning → reasoning_content
			if msg.Reasoning != "" && msg.ReasoningContent == "" {
				msg.ReasoningContent = msg.Reasoning
			}
			// Rule 2: reasoning_details text → reasoning_content
			if msg.ReasoningContent == "" && len(msg.ReasoningDetails) > 0 {
				var texts []string
				for _, rd := range msg.ReasoningDetails {
					if rd.Type == "reasoning.text" && rd.Text != "" {
						texts = append(texts, rd.Text)
					}
				}
				if len(texts) > 0 {
					msg.ReasoningContent = strings.Join(texts, "")
				}
			}
			// Rule 3: <thinking> tags in content → reasoning_content
			if c, ok := msg.Content.(string); ok && c != "" {
				if thinking, remaining, found := extractThinkingFromContent(c); found {
					if msg.ReasoningContent == "" {
						msg.ReasoningContent = thinking
					} else {
						msg.ReasoningContent += thinking
					}
					msg.Content = remaining
				}
			}
		}

		if err := json.NewEncoder(w).Encode(chatResp); err != nil {
			debuglog.Error("proxy: failed to encode response", "model", logData.modelID, "provider", logData.providerName, "error", err)
		}
		debuglog.Info("proxy: non-streaming completed", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "status", resp.StatusCode, "duration_ms", totalDuration, "prompt_tokens", chatResp.Usage.PromptTokens, "completion_tokens", chatResp.Usage.CompletionTokens)
	} else {
		body, _ := io.ReadAll(resp.Body)
		errMsg := util.SanitizeLogBody(string(body), 10000)
		totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
		logData.statusCode = resp.StatusCode
		logData.durationMs = totalDuration
		logData.proxyOverheadMs = proxyOverhead
		logData.parseMs = parseMs
		logData.modelLookupMs = modelLookupMs
		logData.providerLookupMs = providerLookupMs
		logData.keyDecryptMs = keyDecryptMs
		logData.failoverLookupMs = failoverLookupMs
		logData.dialMs = dialMs
		logData.settingsReadMs = settingsReadMs
		logData.responseHeaderMs = responseHeaderMs
		logData.errorMessage = fmt.Sprintf("response decode error: %s", errMsg)
		logData.failoverAttempt = attempt
		logData.state = "failed"
		// Fire-and-forget: skip WaitForInsert to avoid blocking before error response.
		h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
		debuglog.Debug("proxy: non-streaming error details", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "error", errMsg, "duration_ms", totalDuration)
		writeOpenAIError(w, fmt.Sprintf("upstream provider returned HTTP %d", resp.StatusCode), resp.StatusCode)
	}
}

// failRequest populates logData with failure details and updates the request log.
// Always populates all timing fields from timings - if zero-valued, they record as 0ms.
func (h *Handler) failRequest(logData *requestLogData, statusCode int, errMsg string, attempt int, startTime time.Time, parseMs float64, timings resolveTimings, cacheHits resolveCacheHits, proxyOverhead float64) {
	logData.statusCode = statusCode
	logData.errorMessage = errMsg
	logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.modelLookupMs = timings.modelLookupMs
	logData.providerLookupMs = timings.providerLookupMs
	logData.keyDecryptMs = timings.keyDecryptMs
	logData.dialMs = timings.dialMs
	logData.failoverLookupMs = timings.failoverLookupMs
	logData.settingsReadMs = timings.settingsReadMs
	logData.cacheHits = cacheHits
	logData.failoverAttempt = attempt
	logData.state = "failed"
	// Fire-and-forget: skip WaitForInsert to avoid blocking before error response.
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
}

// ChatCompletions handles OpenAI-compatible chat completion requests with failover support.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	st, ok := h.ingestRequest(w, r)
	if !ok {
		return
	}
	// Re-bind the ingested state to locals so the resolve/config/failover
	// phases below read unchanged. Subsequent refactor phases progressively
	// move these into requestState methods.
	startTime := st.startTime
	parseMs := st.parseMs
	reqModel := st.reqModel
	isStreaming := st.isStreaming
	vkHash := st.vkHash
	bodyBytes := st.bodyBytes
	logData := st.logData

	candidates, ok := h.resolveCandidates(w, r, st)
	if !ok {
		return
	}
	timings := st.timings
	cacheHits := st.cacheHits
	isFailover := st.isFailover

	// Re-read accumulated settings read time from context pointer.
	// The initial read captured the rate limiter's contribution,
	// but resolve handlers called AddSettingsReadMs for circuit breaker and
	// failover settings. The pointer now holds the total.
	if v := r.Context().Value(ctxkeys.SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			timings.settingsReadMs = *p
		}
	}

	// Initial overhead estimate (dialMs=0 — not yet populated).
	// proxyOverhead is recomputed after each dial inside the failover loop
	// so that all exit paths (backoff disconnect, error, failRequest) use
	// the current accumulated total.
	proxyOverhead := timings.proxyOverheadMs(parseMs)
	debuglog.Debug("proxy: model resolved (pre-loop)", "model", logData.modelID, "provider", logData.providerName, "candidates", len(candidates), "overhead_ms", proxyOverhead)

	// Use the original request body as the base for per-candidate rewrites.
	// stream_options injection is deferred to the per-candidate rewrite block
	// so it can be conditioned on provider type (avoided for providers that
	// strict-validate unknown fields like Anthropic, Google, Cohere).
	proxyReqBody := bodyBytes

	// Per-request DNS resolution timing. SafeDialer's DialContext writes
	// into this pointer via context, avoiding cross-request races on a
	// shared atomic field.
	var dialMs float64

	// Non-streaming timeout is configurable via request_timeout setting (default 1m).
	// Streaming requests get 10× the non-streaming timeout to accommodate
	// thinking/reasoning models that can take several minutes before first token.
	// Read once before the loop so all attempts within a single request use
	// the same timeout, avoiding inconsistency if the setting changes mid-request.
	rtStart := time.Now()
	baseTimeout := h.settingsRepo.GetDuration(r.Context(), "request_timeout", time.Minute)
	ctxkeys.AddSettingsReadMs(r.Context(), rtStart)
	failoverTimeout := baseTimeout
	if isStreaming {
		failoverTimeout = baseTimeout * 10
	}

	var lastErr string
	// Read circuit_breaker_enabled once before the loop to avoid repeated settings reads.
	cbStart2 := time.Now()
	circuitBreakerEnabled := h.settingsRepo.GetBool(r.Context(), "circuit_breaker_enabled", true)
	ctxkeys.AddSettingsReadMs(r.Context(), cbStart2)

	// Overall request deadline: caps total time across all failover candidates
	// to prevent resource pinning from silent clients. Without this, N candidates
	// with per-candidate failoverTimeout could hold a goroutine for N×failoverTimeout.
	// The ceiling is 2× the per-candidate timeout, giving a second attempt full time
	// while capping any number of subsequent candidates to the remaining budget.
	overallDeadline := startTime.Add(failoverTimeout * 2)

	// Final re-read of accumulated settings read time. The initial read
	// captured the rate limiter's contribution, resolve handlers added
	// circuit breaker/failover settings, and the proxy loop added
	// request_timeout and circuit_breaker_enabled reads. Recompute
	// proxyOverhead with the complete total.
	if v := r.Context().Value(ctxkeys.SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			timings.settingsReadMs = *p
		}
	}

	for attempt, candidate := range candidates {
		// Overall deadline check: stop failover if the total time budget
		// across all candidates has been exceeded. This prevents N candidates
		// from holding a goroutine for N×failoverTimeout when the client
		// is silent but connected (no TCP reset).
		if time.Now().After(overallDeadline) && attempt > 0 {
			debuglog.Warn("proxy: overall request deadline exceeded, stopping failover", "model", logData.modelID, "attempt", attempt+1, "total_candidates", len(candidates), "deadline", overallDeadline)
			lastErr = fmt.Sprintf("attempt %d: overall request deadline exceeded", attempt)
			break
		}

		// Exponential backoff between failover attempts: 0ms, ~100ms, ~200ms, ~400ms...
		// Capped at 2s, with ±50ms jitter to avoid thundering herd.
		// First attempt (attempt=0) has no delay.
		if attempt > 0 {
			backoff := failoverBackoff(100*time.Millisecond, 2*time.Second, attempt)
			debuglog.Info("proxy: failover backoff", "backoff", backoff, "attempt", attempt+1)
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				debuglog.Info("proxy: client disconnected during failover backoff", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt+1)
				h.failRequest(logData, 499, "client disconnected during failover", attempt-1, startTime, parseMs, timings, cacheHits, proxyOverhead)
				writeOpenAIError(w, "client disconnected", http.StatusRequestTimeout)
				return
			}
		}

		logData.providerID = candidate.provider.ID
		logData.providerName = candidate.provider.Name
		if isFailover {
			logData.resolvedModelID = candidate.model.ModelID
		}
		if attempt == 0 {
			debuglog.Info("proxy: routing to provider", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "total_candidates", len(candidates))
		} else {
			debuglog.Info("proxy: failover attempt", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID)
		}
		debuglog.Debug("proxy: candidate details", "provider_id", candidate.provider.ID, "provider_name", candidate.provider.Name, "model_id", candidate.model.ModelID, "provider_type", provider.DetectProviderType(candidate.provider.BaseURL), "attempt", attempt+1, "total_candidates", len(candidates))
		//nolint:gosec // intentional: failover goroutine needs independent lifecycle
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
		targetURL := util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType)
		debuglog.Debug("proxy: built target URL", "target_url", targetURL)

		upstreamBody := proxyReqBody
		needsRewrite := reqModel != candidate.model.ModelID || providerType == "anthropic" || NeedsProviderInjection(providerType) || isStreaming
		debuglog.Debug("proxy: request rewrite check", "needs_rewrite", needsRewrite, "request_model", logData.modelID, "provider", logData.providerName, "resolved_model", candidate.model.ModelID, "provider_type", providerType)
		if needsRewrite {
			upstreamBody = buildUpstreamBody(proxyReqBody, providerType, candidate.model.ModelID, reqModel, isStreaming, &h.deprecationCache, nil)
		}
		// Log the actual model name in the upstream body for debugging rewrite issues.
		if upstreamModel, _, _ := strings.Cut(string(upstreamBody), ","); strings.Contains(upstreamModel, `"model"`) {
			debuglog.Debug("proxy: upstream body model", "upstream_model_snippet", upstreamModel)
		}

		var retryCancel context.CancelFunc
		streamCancelOrigin := "failover_timeout"
		failoverCtx, failoverCancel := context.WithTimeout(r.Context(), failoverTimeout)
		failoverCtx = context.WithValue(failoverCtx, ctxkeys.CancelOriginKey, "failover_timeout")
		proxyReq, err := newRequestWithContext(failoverCtx, "POST", targetURL, bytes.NewReader(upstreamBody))
		if err != nil {
			failoverCancel()
			lastErr = fmt.Sprintf("attempt %d: failed to create request: %v", attempt, err)
			continue
		}

		util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
		proxyReq.Header.Set("Content-Type", "application/json")
		debuglog.Debug("proxy: sending upstream request", "method", proxyReq.Method, "url", targetURL, "content_length", len(upstreamBody), "has_api_key", candidate.apiKey != "")

		// Reuse the shared upstream Transport instead of creating a new one
		// per request. A fresh Transport spawns persistent readLoop/writeLoop
		// goroutines per connection that only die after IdleConnTimeout, so
		// creating one per request causes unbounded goroutine growth.
		// Inject per-request dial timing pointer so SafeDialer writes
		// DNS resolution time into this request's own variable, avoiding
		// cross-request race conditions on a shared atomic.
		dialCtx := context.WithValue(failoverCtx, ctxkeys.DialMsKey, &dialMs)
		proxyReq = proxyReq.WithContext(dialCtx)

		var checkRedirect func(req *http.Request, via []*http.Request) error
		if h.safeDialer != nil {
			checkRedirect = h.safeDialer.CheckRedirect
		}
		upstreamClient := &http.Client{
			Transport:     h.upstreamTransport,
			CheckRedirect: checkRedirect,
		}
		//nolint:gosec // provider URL is admin-configured, not arbitrary user input
		resp, err := upstreamClient.Do(proxyReq)
		timings.dialMs += dialMs
		dialMs = 0
		proxyOverhead = timings.proxyOverheadMs(parseMs)
		if err != nil {
			failoverCancel() // no body to consume on error
			// Determine the origin of context cancellation for actionable errors.
			// "context canceled" is opaque — we need to know if the client
			// disconnected, the failover timeout expired, or the retry timeout expired.
			// Key insight: context.Canceled means the parent (client) context was
			// canceled — always a client disconnect. context.DeadlineExceeded means
			// the derived context's deadline expired — read CancelOriginKey to
			// distinguish failover_timeout from retry_timeout.
			isContextErr := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
			if isContextErr {
				cancelOrigin := "client_disconnect"
				if errors.Is(err, context.DeadlineExceeded) {
					if v := proxyReq.Context().Value(ctxkeys.CancelOriginKey); v != nil {
						if s, ok := v.(string); ok {
							cancelOrigin = s
						}
					}
				}
				lastErr = fmt.Sprintf("attempt %d: %s", attempt, humanReadableCancelOrigin(cancelOrigin))
				debuglog.Info("proxy: context cancelled during request to provider", "provider", logData.providerName, "provider_id", candidate.provider.ID, "model", logData.modelID, "origin", cancelOrigin, "error", err)
			} else {
				lastErr = fmt.Sprintf("attempt %d: provider error: %v", attempt, err)
				debuglog.Warn("proxy: upstream request failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", err)
			}
			// Client-initiated cancellations and deadline exceeded are not
			// provider failures. If the caller disconnected (Canceled) or
			// the request timed out (DeadlineExceeded), we must not penalize
			// the circuit breaker for that.
			if !isContextErr {
				if circuitBreakerEnabled {
					h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
				}
			}
			continue
		}

		// Log upstream response metadata for debugging.
		debuglog.Debug("proxy: upstream response received", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"), "x_request_id", resp.Header.Get("X-Request-Id"), "x_ratelimit_remaining", resp.Header.Get("X-RateLimit-Remaining"), "attempt", attempt+1)

		// Auto-retry param-rejection 400s: parse the error, learn which params
		// are rejected for this model, strip them, and retry once.
		// Works universally — any LLM API mentioning "temperature" or "top_p"
		// in a 400 error can only mean the sampling parameter.
		if resp.StatusCode == 400 {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			failoverCancel() // 400 body consumed, context no longer needed
			debuglog.Debug("proxy: received 400 from upstream, checking for param rejection", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "body_length", len(body))
			// Restore the body so downstream error handling (line ~605) can read it
			// if we don't successfully retry. Must be set before any fallthrough.
			resp.Body = io.NopCloser(bytes.NewReader(body))
			if readErr == nil {
				if rejected := parseProviderParamError(body); rejected != nil {
					// Cache the learned rejections for future preemptive stripping.
					// Merge with any existing entries using CompareAndSwap to avoid
					// data races from concurrent goroutines mutating the same map.
					// NOTE: Values are stored as *map[string]bool to support CompareAndSwap
					// (maps are not comparable, so pointers are required).
					cacheKey := fmt.Sprintf("%s:%s", providerType, candidate.model.ModelID)
					for {
						existing, loaded := h.deprecationCache.LoadOrStore(cacheKey, &rejected)
						if !loaded {
							// First entry for this key — we just stored 'rejected'.
							break
						}
						// Merge with existing, creating a new map to avoid data races.
						merged := make(map[string]bool)
						existingMap, ok := existing.(*map[string]bool)
						if !ok {
							debuglog.Error("deprecationCache: unexpected type", "key", cacheKey, "type", fmt.Sprintf("%T", existing))
							break
						}
						for k := range *existingMap {
							merged[k] = true
						}
						for k := range rejected {
							merged[k] = true
						}
						if h.deprecationCache.CompareAndSwap(cacheKey, existing, &merged) {
							break
						}
						// CompareAndSwap failed — another goroutine updated it, retry.
					}
					// Rebuild the request body using the shared rewrite path.
					// This ensures stream_options injection, universal/learned param
					// stripping, and InjectProviderParams are all applied on retry,
					// preventing drift from the initial attempt path.
					rebuilt := buildUpstreamBody(proxyReqBody, providerType, candidate.model.ModelID, reqModel, isStreaming, &h.deprecationCache, rejected)
					retryCtx, rc := context.WithTimeout(r.Context(), failoverTimeout)
					retryCtx = context.WithValue(retryCtx, ctxkeys.CancelOriginKey, "retry_timeout")
					retryCtx = context.WithValue(retryCtx, ctxkeys.DialMsKey, &dialMs)
					retryCancel = rc
					streamCancelOrigin = "retry_timeout"
					retryReq, retryErr := newRequestWithContext(retryCtx, "POST", targetURL, bytes.NewReader(rebuilt))
					if retryErr != nil {
						retryCancel()
						lastErr = fmt.Sprintf("attempt %d: failed to create retry request: %v", attempt, retryErr)
						continue
					}
					util.SetProviderAuthHeaders(retryReq, providerType, candidate.apiKey)
					retryReq.Header.Set("Content-Type", "application/json")
					var retryCheckRedirect func(req *http.Request, via []*http.Request) error
					if h.safeDialer != nil {
						retryCheckRedirect = h.safeDialer.CheckRedirect
					}
					retryClient := &http.Client{Transport: h.upstreamTransport, CheckRedirect: retryCheckRedirect}
					resp, retryErr = retryClient.Do(retryReq)
					if retryErr != nil {
						retryCancel() // no body to consume on retry error
						debuglog.Warn("proxy: auto-retry request failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", retryErr)
						if errors.Is(retryErr, context.Canceled) || errors.Is(retryErr, context.DeadlineExceeded) {
							// Branch like the main failover loop: Canceled = client
							// disconnect, DeadlineExceeded = retry timeout.
							origin := "retry_timeout"
							if errors.Is(retryErr, context.Canceled) {
								origin = "client_disconnect"
							}
							lastErr = fmt.Sprintf("attempt %d: %s", attempt, humanReadableCancelOrigin(origin))
						} else {
							lastErr = fmt.Sprintf("attempt %d: retry error: %v", attempt, retryErr)
						}
						continue
					}
					failoverCancel() // original 400 body already consumed, original context no longer needed
					// Accumulate retry's dial time into total.
					timings.dialMs += dialMs
					dialMs = 0
					proxyOverhead = timings.proxyOverheadMs(parseMs)
					// retryCancel() must NOT be called here — retry resp.Body is read below.
					// Store retryCancel for deferred cleanup after body consumption.
					// Successfully retried — fall through to normal response handling
					debuglog.Info("proxy: auto-retry succeeded", "model", candidate.model.ModelID, "rejected_params", mapKeys(rejected))
				}
			}
		}

		responseHeaderMs := float64(time.Since(startTime).Microseconds()) / 1000.0

		hasMoreCandidates := attempt < len(candidates)-1
		isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

		if isFailoverEligible {
			// Determine breaker action from status code.
			// See breakerRecordAction for the full status→action mapping.
			action := breakerRecordAction(resp.StatusCode)
			if circuitBreakerEnabled {
				switch action {
				case breakerActionFailure:
					h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
				case breakerActionNoOp:
					// Model-specific client error (404/499): provider is alive
					// but rejecting this request. No-op for the breaker — neither
					// failure nor success. Recording success would erase real 5xx
					// failure history (resetting consecutiveFails in Closed state)
					// and could prematurely close a half-open circuit based on a
					// model-specific error that says nothing about provider health.
				case breakerActionSuccess:
					// Not reached for failover-eligible codes: shouldFailover only
					// returns true for {5xx,429,401,403,404,499}, all of which map to
					// failure or no-op above. Retained so the switch stays exhaustive
					// over breakerAction — if the shouldFailover/breakerRecordAction
					// mappings ever diverge, a success is recorded rather than dropped.
					h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
				}
			}
		} else if circuitBreakerEnabled && (!isStreaming || resp.StatusCode != http.StatusOK) {
			h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
		}

		shouldFailoverNow := isFailoverEligible && hasMoreCandidates
		debuglog.Debug("proxy: failover decision", "status", resp.StatusCode, "is_failover_eligible", isFailoverEligible, "has_more_candidates", hasMoreCandidates, "should_failover_now", shouldFailoverNow, "attempt", attempt+1)

		if shouldFailoverNow {
			_, _ = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			failoverCancel() // body consumed before failover continue
			if retryCancel != nil {
				retryCancel() // retry body consumed, context no longer needed
			}
			lastErr = fmt.Sprintf("attempt %d: HTTP %d", attempt, resp.StatusCode)
			debuglog.Info("proxy: failover triggered", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
			logData.failoverAttempt = attempt
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			failoverCancel() // body consumed for non-200 response
			if retryCancel != nil {
				retryCancel() // retry body consumed, context no longer needed
			}
			errMsg := util.SanitizeLogBody(string(body), 10000)
			debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "body", errMsg)
			debuglog.Debug("proxy: upstream error response", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "body_length", len(body), "attempt", attempt+1)
			logData.responseHeaderMs = responseHeaderMs
			h.failRequest(logData, resp.StatusCode, errMsg, attempt, startTime, parseMs, timings, cacheHits, proxyOverhead)

			if !hasMoreCandidates {
				// All failover candidates exhausted — return a generic error.
				// The full upstream body is logged server-side above but not
				// forwarded, as it may contain provider-specific details.
				writeOpenAIError(w, fmt.Sprintf("upstream provider returned HTTP %d", resp.StatusCode), resp.StatusCode)
				return
			}

			// Non-failover-eligible error with remaining candidates — forward
			// the upstream response so clients can react to semantic errors
			// (e.g. context_length_exceeded, rate_limit_exceeded).
			if json.Valid(body) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(resp.StatusCode)
				_, _ = w.Write(body)
			} else {
				// Body is not JSON (e.g. HTML from a CDN). Wrap in an
				// OpenAI-compatible envelope so JSON-parsing clients don't crash.
				writeOpenAIError(w, errMsg, resp.StatusCode)
			}
			return
		}

		debuglog.Debug("proxy: upstream responded OK, dispatching to handler", "stream", isStreaming, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
		if isStreaming {
			ttftTimeout := h.settingsRepo.GetDuration(r.Context(), "ttft_timeout", 60*time.Second)
			stallTimeout := h.settingsRepo.GetDuration(r.Context(), "stream_stall_timeout", 30*time.Second)

			opts := streamOptions{
				responseHeaderMs:   responseHeaderMs,
				streamStallTimeout: stallTimeout,
				providerID:         candidate.provider.ID,
				providerName:       candidate.provider.Name,
				circuitBreakerOn:   circuitBreakerEnabled,
				proxyOverheadMs:    proxyOverhead,
				parseMs:            parseMs,
				failoverLookupMs:   timings.failoverLookupMs,
				modelLookupMs:      timings.modelLookupMs,
				providerLookupMs:   timings.providerLookupMs,
				keyDecryptMs:       timings.keyDecryptMs,
				dialMs:             timings.dialMs,
				settingsReadMs:     timings.settingsReadMs,
				vkHash:             vkHash,
				attempt:            attempt,
				cancelOrigin:       streamCancelOrigin,
			}

			if ttftTimeout > 0 {
				// TTFT probe: read until first real data chunk.
				probeBuf, trueTtftMs, probeErr := h.probeFirstToken(r.Context(), resp.Body, ttftTimeout, startTime)
				if probeErr != nil {
					// Timeout or read error — failover. probeFirstToken may
					// or may not have closed the body (only on DeadlineExceeded);
					// close it unconditionally to release the connection.
					_ = resp.Body.Close()
					// Skip circuit-breaker recording when the client disconnected:
					// the probe failed because r.Context() was cancelled, not because
					// the provider was unhealthy.
					if circuitBreakerEnabled && r.Context().Err() == nil {
						h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
					}
					lastErr = fmt.Sprintf("attempt %d: %v", attempt, probeErr)
					failoverCancel()
					if retryCancel != nil {
						retryCancel()
					}
					logData.failoverAttempt = attempt
					logData.responseHeaderMs = responseHeaderMs
					debuglog.Warn("proxy: TTFT probe failed", "attempt", attempt+1, "provider", candidate.provider.Name, "error", probeErr)
					continue
				}
				// First token confirmed (or [DONE] received).
				if circuitBreakerEnabled {
					h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
				}
				opts.preReadBuf = probeBuf
				opts.trueTtftMs = trueTtftMs
			} else if circuitBreakerEnabled {
				// Disabled — immediate commit (backward compat).
				h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
			}

			h.handleStreamingResponse(w, r, logData, resp, startTime, opts)
			failoverCancel() // body consumed by handleStreamingResponse
			if retryCancel != nil {
				retryCancel()
			}
			return
		}

		h.handleNonStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, timings.failoverLookupMs, timings.modelLookupMs, timings.providerLookupMs, timings.keyDecryptMs, timings.dialMs, timings.settingsReadMs, responseHeaderMs, vkHash, attempt)
		failoverCancel() // body consumed by handleNonStreamingResponse
		if retryCancel != nil {
			retryCancel()
		}
		return
	}

	if isFailover {
		debuglog.Error("proxy: all providers exhausted", "model", logData.modelID, "provider", logData.providerName, "error", lastErr, "candidates", len(candidates), "failover_timeout", failoverTimeout)
	} else {
		debuglog.Error("proxy: provider request failed", "model", logData.modelID, "provider", logData.providerName, "error", lastErr, "request_timeout", failoverTimeout)
	}
	logData.providerID = uuid.Nil
	if isFailover {
		h.failRequest(logData, 502, fmt.Sprintf("all providers failed: %s", lastErr), len(candidates)-1, startTime, parseMs, timings, cacheHits, proxyOverhead)
		writeOpenAIError(w, fmt.Sprintf("all providers failed for model %s", reqModel), http.StatusBadGateway)
	} else {
		h.failRequest(logData, 502, fmt.Sprintf("provider request failed: %s", lastErr), len(candidates)-1, startTime, parseMs, timings, cacheHits, proxyOverhead)
		writeOpenAIError(w, fmt.Sprintf("provider request failed for model %s", reqModel), http.StatusBadGateway)
	}
}

// probeFirstToken reads from body until it finds the first real SSE data chunk
// or the timeout fires. It returns a buffer containing all bytes read (for
// replay via io.MultiReader), the true time-to-first-token in milliseconds,
// and any error.
//
// A "real data chunk" is any "data:" line where the content after "data:" is
// not "[DONE]". Keepalive comments (":"), empty lines, "event:", "id:", and
// "retry:" directives are skipped but still captured in probeBuf for replay.
func (h *Handler) probeFirstToken(
	ctx context.Context,
	body io.ReadCloser,
	ttftTimeout time.Duration,
	startTime time.Time,
) (probeBuf *bytes.Buffer, trueTtftMs float64, err error) {
	probeCtx, probeCancel := context.WithTimeout(ctx, ttftTimeout)
	defer probeCancel()

	// Signal the goroutine when the probe finishes, so it doesn't close
	// the body after a successful read. Closed explicitly on success paths
	// and via sync.Once/defer on all paths.
	probeDone := make(chan struct{})
	var closeProbeOnce sync.Once
	closeProbe := func() { closeProbeOnce.Do(func() { close(probeDone) }) }
	defer closeProbe()

	// Atomic flag set the instant a data line is detected, before any
	// string processing. The goroutine checks this as a last guard before
	// closing the body, closing a narrow race where the timer fires at the
	// same instant the scanner returns a data line.
	var probeSucceeded atomic.Bool

	// Goroutine closes body when the probe context is cancelled (TTFT timeout
	// or parent context cancellation), unblocking the scanner. The double-
	// check of probeDone handles the narrow race where the probe succeeds
	// at the same instant the context fires; probeSucceeded is the final
	// guard to prevent closing a body that's about to be replayed.
	go func() {
		select {
		case <-probeDone:
			// Probe finished — don't touch the body.
			return
		case <-probeCtx.Done():
			// Double-check: probe may have just finished between the
			// outer select and here.
			select {
			case <-probeDone:
				return
			default:
			}
			if !probeSucceeded.Load() {
				_ = body.Close()
			}
		}
	}()

	var buf bytes.Buffer
	tee := io.TeeReader(body, &buf)
	scanner := bufio.NewScanner(tee)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines, keepalive comments, and non-data directives.
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			// Signal the goroutine immediately — a data line was found,
			// the provider is healthy. This must happen before any
			// string processing so the goroutine sees it even if the
			// timer fires at the same instant.
			probeSucceeded.Store(true)
			content := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if content == "[DONE]" {
				// Stream ended before any real token.
				debuglog.Info("proxy: TTFT probe saw [DONE] before first token", "ttft_ms", float64(time.Since(startTime).Microseconds())/1000.0)
				closeProbe()
				return &buf, 0, nil
			}
			// First real data chunk found.
			ttft := float64(time.Since(startTime).Microseconds()) / 1000.0
			debuglog.Info("proxy: TTFT probe found first token", "ttft_ms", ttft, "preview", truncateString(content, 80))
			closeProbe()
			return &buf, ttft, nil
		}
		// Unknown line format — skip but captured in buf.
	}

	// Scanner exited — body closed (timeout) or read error.
	// bufio.Scanner never returns io.EOF from Err(); on clean EOF,
	// Scan() returns false with Err() == nil, handled by the fallback
	// after this block.
	if scanErr := scanner.Err(); scanErr != nil {
		// Race recovery: the goroutine may close the body between the
		// scanner reading a complete data line and probeSucceeded being
		// checked. TeeReader writes to buf before scanner.Scan() returns,
		// so the data is captured. Only return success if the probe context
		// is still valid — if it expired, the goroutine closed the body and
		// returning success would give the caller a closed body, causing
		// handleStreamingResponse to truncate the stream after buffer replay.
		if probeCtx.Err() == nil {
			probeSucceeded.Store(true) // mirror line 1680: store before any processing
			bufStr := buf.String()
			for _, rawLine := range strings.Split(bufStr, "\n") {
				if l := strings.TrimSpace(rawLine); strings.HasPrefix(l, "data:") {
					// Reject partial lines: a complete SSE line must be
					// followed by \n in the buffer. Without this guard a
					// mid-line network fragment like "data: hel" (no \n)
					// would pass HasPrefix but represent malformed data.
					if !strings.Contains(bufStr, rawLine+"\n") {
						continue
					}
					content := strings.TrimSpace(strings.TrimPrefix(l, "data:"))
					if content != "[DONE]" {
						ttft := float64(time.Since(startTime).Microseconds()) / 1000.0
						debuglog.Info("proxy: TTFT probe recovered data after scanner error", "ttft_ms", ttft, "scan_error", scanErr)
						return &buf, ttft, nil
					}
				}
			}
		}
		if probeCtx.Err() == context.DeadlineExceeded {
			return nil, 0, fmt.Errorf("TTFT timeout: no first token within %s", ttftTimeout)
		}
		return nil, 0, fmt.Errorf("TTFT probe read error: %w", scanErr)
	}

	// Scanner finished without error and without finding data — body EOF.
	return nil, 0, fmt.Errorf("TTFT probe: body closed before first data chunk")
}

// truncateString truncates a string to maxLen runes for logging.
func truncateString(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// See util.BuildProviderTargetURL for URL construction and util.SetProviderAuthHeaders for auth.

// mapKeys returns the keys of a map[string]bool for logging.
// failoverBackoff calculates exponential backoff with jitter between failover attempts.
// base is the starting delay, capacity is the maximum delay, attempt is the 1-indexed attempt number.
// Jitter of [0, base) is added to spread retries from concurrent requests hitting the same cascade.
func failoverBackoff(base, capacity time.Duration, attempt int) time.Duration {
	exp := time.Duration(float64(base) * math.Pow(2, float64(attempt-1)))
	if exp > capacity {
		exp = capacity
	}
	jitter := time.Duration(rand.Int64N(int64(base)))
	return exp + jitter
}

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

// humanReadableCancelOrigin maps internal cancel origin identifiers to
// human-readable descriptions for error messages and request logs.
// Raw Go errors like "context canceled" and "context deadline exceeded" are
// opaque — callers need to know whether the client disconnected, the failover
// timeout expired, or a param-strip retry timed out.
func humanReadableCancelOrigin(origin string) string {
	switch origin {
	case "client_disconnect":
		return "client disconnected"
	case "failover_timeout":
		return "upstream request timed out"
	case "retry_timeout":
		return "param-strip retry timed out"
	default:
		return origin
	}
}
