package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
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
	candidates, ok := h.resolveCandidates(w, r, st)
	if !ok {
		return
	}
	h.loadFailoverConfig(r, st)

	debuglog.Debug("proxy: model resolved (pre-loop)", "model", st.logData.modelID, "provider", st.logData.providerName, "candidates", len(candidates), "overhead_ms", st.proxyOverhead)

	for attempt, candidate := range candidates {
		// Overall deadline check: stop failover if the total time budget
		// across all candidates has been exceeded. This prevents N candidates
		// from holding a goroutine for N×failoverTimeout when the client
		// is silent but connected (no TCP reset).
		if time.Now().After(st.overallDeadline) && attempt > 0 {
			debuglog.Warn("proxy: overall request deadline exceeded, stopping failover", "model", st.logData.modelID, "attempt", attempt+1, "total_candidates", len(candidates), "deadline", st.overallDeadline)
			st.lastErr = fmt.Sprintf("attempt %d: overall request deadline exceeded", attempt)
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
				debuglog.Info("proxy: client disconnected during failover backoff", "model", st.logData.modelID, "provider", st.logData.providerName, "attempt", attempt+1)
				h.failRequest(st.logData, 499, "client disconnected during failover", attempt-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
				writeOpenAIError(w, "client disconnected", http.StatusRequestTimeout)
				return
			}
		}

		// One failover attempt. attemptCandidate owns its request contexts
		// (cancelled via defer after body consumption) and reports whether to
		// try the next candidate, that the response was served, or that a
		// terminal error was written.
		if h.attemptCandidate(w, r, st, candidate, attempt, len(candidates)) != outcomeFailover {
			return
		}
	}

	h.failAllExhausted(w, st, len(candidates))
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
