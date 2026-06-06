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

	// Progressive stall timeout: after this many chunks, the stream is
	// clearly alive — extend the watchdog timeout to tolerate tool-call
	// pauses and long reasoning chains.
	const progressiveChunkThreshold = 50
	const progressiveStallMultiplier = 3

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

	flusher, canFlush := w.(http.Flusher)

	// Create scanner: if TTFT probe was used, replay probed bytes first.
	var scanner *bufio.Scanner
	if opts.preReadBuf != nil {
		scanner = bufio.NewScanner(io.MultiReader(bytes.NewReader(opts.preReadBuf.Bytes()), resp.Body))
	} else {
		scanner = bufio.NewScanner(resp.Body)
	}
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024) // 4MB per line
	debuglog.Debug("proxy: streaming scanner created", "model", logData.modelID, "provider", logData.providerName, "replaying_probe", opts.preReadBuf != nil)

	// Stall watchdog: if streamStallTimeout > 0, start a goroutine that
	// closes resp.Body on timeout, unblocking the scanner.
	var streamStalled int32 // set to 1 by watchdog on timeout
	var stallCh chan time.Duration
	var watchdogDone chan struct{}
	if opts.streamStallTimeout > 0 {
		stallCh = make(chan time.Duration, 1)
		watchdogDone = make(chan struct{})
		go func() {
			timer := time.NewTimer(opts.streamStallTimeout)
			defer timer.Stop()
			for {
				select {
				case d := <-stallCh:
					if !timer.Stop() {
						<-timer.C
					}
					timer.Reset(d)
				case <-timer.C:
					atomic.StoreInt32(&streamStalled, 1)
					_ = resp.Body.Close() // unblock scanner
					return
				case <-watchdogDone:
					return
				}
			}
		}()
	}
	var promptTokens, completionTokens, reasoningTokens int
	var promptCacheHitTokens, promptCacheMissTokens int
	var lastErrMsg string
	clientDisconnected := false
	sawDone := false
	chunkCount := 0
	errorChunkCount := 0
	var bytesWritten int64
	// Periodic streaming progress logging (every 50 chunks) to give
	// visibility into stream health without flooding logs.
	const chunkLogInterval = 50
	// When strip_reasoning skips a chunk, we also need to suppress the
	// following SSE separator (empty line). Otherwise bare \n events
	// reach the client, which breaks parsers like openai-go's ssestream.
	skipNextEmptyLine := false
	// P1-B: Error accumulation buffer. Some providers (e.g. go-openai) split
	// error JSON across multiple SSE data lines. We accumulate bytes until a
	// non-error chunk arrives, then try to unmarshal the full accumulated error.
	var errAccum []byte
	// P1-C: Tracks the last Anthropic SSE event type (e.g. "error") so we can
	// extract error messages from the subsequent data line.
	var lastAnthropicEvent string

	var emptyLines int
	const emptyMessagesLimit = 1000
	// P2-2: Track last finish_reason to suppress duplicate finish chunks.
	// Some providers (notably OpenRouter routing to certain models) send
	// two consecutive chunks with the same finish_reason, where the second
	// has no content or usage — just a bare finish_reason repeat. Suppressing
	// avoids downstream "empty response text" errors.
	var lastFinishReason string
	// P2-5: Detect repeated identical content chunks. Some models (notably
	// xAI Grok reasoning) send the same reasoning text in consecutive deltas,
	// causing infinite-style loops in downstream processors. We track
	// consecutive identical content and log a warning when the threshold
	// is exceeded.
	var lastContent string
	var repeatedCount int
	const repeatedContentLimit = 10
	// P2-7: Track native_finish_reason from OpenRouter for logging.
	// OpenRouter includes this field alongside the normalized finish_reason,
	// preserving the original provider's value for debugging.
	var lastNativeFinishReason string
	// Track whether we've seen reasoning_content (thinking) in this stream
	// for first-occurrence debug logging.
	sawThinking := false
	// Read strip_reasoning flag from context once before the scanner loop.
	// The value is set by ProxyKeyMiddleware and never changes mid-stream.
	stripReasoning := false
	if v := r.Context().Value(ctxkeys.VirtualKeyStripReasoningKey); v != nil {
		if sr, ok := v.(bool); ok {
			stripReasoning = sr
		}
	}
	debuglog.Debug("proxy: strip_reasoning flag", "enabled", stripReasoning, "model", logData.modelID, "provider", logData.providerName)

	for scanner.Scan() {
		line := scanner.Bytes()
		chunkCount++

		// Ping stall watchdog after each successful scan.
		// After progressiveChunkThreshold chunks the stream is clearly
		// alive — extend the timeout to tolerate tool-call pauses and
		// long reasoning.
		if stallCh != nil {
			effectiveStall := opts.streamStallTimeout
			if chunkCount > progressiveChunkThreshold {
				effectiveStall = opts.streamStallTimeout * progressiveStallMultiplier
			}
			select {
			case stallCh <- effectiveStall:
			default:
			}
		}

		// Periodic streaming progress log for observability.
		if chunkCount%chunkLogInterval == 0 {
			debuglog.Debug("proxy: streaming progress", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten, "prompt_tokens", promptTokens, "completion_tokens", completionTokens, "thinking", sawThinking)
		}

		select {
		case <-r.Context().Done():
			clientDisconnected = true
			goto logUpdate
		default:
		}

		lineStr := string(line)
		// P2-11: Strip UTF-8 BOM (\uFEFF) that some providers send at the
		// start of a stream. Only check on the first chunk.
		if chunkCount == 1 {
			lineStr = strings.TrimPrefix(lineStr, "\uFEFF")
		}
		// P2-3: Trim leading \r and \n that some providers (notably Gemini)
		// send before data: lines. SSE spec allows CR, LF, or CRLF as line
		// terminators, but bufio.Scanner may leave a stray \r if the
		// provider uses \r\r or \r\n\r\n between events.
		lineStr = strings.TrimLeft(lineStr, "\r\n ")

		if lineStr == "" {
			// P2-4: Safety valve against streams that send only empty lines.
			// go-openai uses ErrTooManyEmptyStreamMessages for this.
			emptyLines++
			if emptyLines > emptyMessagesLimit {
				debuglog.Warn("proxy: too many empty SSE lines, aborting stream", "model", logData.modelID, "provider", logData.providerName, "limit", emptyMessagesLimit, "chunks", chunkCount)
				lastErrMsg = "stream interrupted: too many empty lines"
				break
			}
			// When strip_reasoning skips a reasoning chunk, the SSE
			// separator (empty line) that followed it must also be
			// suppressed. Bare \n events break parsers like openai-go's
			// ssestream (Warp's backend). Only forward the separator
			// when the preceding data line was actually forwarded.
			if skipNextEmptyLine {
				skipNextEmptyLine = false
				continue
			}
			// Forward empty lines — they are SSE event separators required by
			// the spec. Clients like eventsource-parser dispatch events on
			// blank lines; omitting them causes all data lines to be
			// concatenated into one invalid event.
			var n int
			var err error
			if n, err = w.Write([]byte("\n")); err != nil {
				clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream (blank line)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten)
				goto logUpdate
			}
			bytesWritten += int64(n)
			if canFlush {
				flusher.Flush()
			}
			continue
		}
		emptyLines = 0

		// Match "data: " (standard) or "data:" (LM Studio and some proxies
		// send SSE without a space after the colon). Strip leading whitespace
		// from the payload so both forms yield the same JSON.
		var payload string
		//nolint:gocritic // if-else chain is clearer than switch for SSE prefix matching
		if strings.HasPrefix(lineStr, "data: ") {
			payload = lineStr[6:]
		} else if strings.HasPrefix(lineStr, "data:") && len(lineStr) > 5 {
			// "data:" with no space — LM Studio compatibility.
			// Find where the JSON starts after optional whitespace.
			payload = strings.TrimLeft(lineStr[5:], " \t")
		} else {
			// Not a data line — could be an SSE comment (": ..."),
			// an event line, or a blank line. Pass through without parsing.
			// P1-C: Detect Anthropic-style "event: error" lines for logging.
			// Anthropic streams use typed events like:
			//   event: error
			//   data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}
			// We track "event: error" so the next data line is known to be an
			// error payload, allowing us to extract the message for logging.
			if strings.HasPrefix(lineStr, "event:") {
				evt := strings.TrimSpace(lineStr[6:])
				if evt == "error" {
					lastAnthropicEvent = "error"
				} else {
					lastAnthropicEvent = ""
				}
			}
			// Flush any accumulated error when a non-data line arrives
			// (the error payload has already been captured in the data line).
			if len(errAccum) > 0 {
				if accumulatedMsg := parseAccumulatedError(errAccum); accumulatedMsg != "" {
					lastErrMsg = accumulatedMsg
					errorChunkCount++
					debuglog.Warn("proxy: accumulated SSE error", "error_message", accumulatedMsg, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
				}
				errAccum = nil
			}
			var n int
			var err error
			if n, err = w.Write(line); err != nil {
				clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten)
				goto logUpdate
			}
			bytesWritten += int64(n)
			if n, err = w.Write([]byte("\n")); err != nil {
				clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten)
				goto logUpdate
			}
			bytesWritten += int64(n)
			if canFlush {
				flusher.Flush()
			}
			continue
		}
		if payload == "[DONE]" {
			sawDone = true
			// Write [DONE] sentinel to the downstream client.
			var n int
			var err error
			if n, err = w.Write(line); err != nil {
				clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten)
				goto logUpdate
			}
			bytesWritten += int64(n)
			if n, err = w.Write([]byte("\n\n")); err != nil {
				clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten)
				goto logUpdate
			}
			bytesWritten += int64(n)
			if canFlush {
				flusher.Flush()
			}
			debuglog.Debug("proxy: received [DONE] sentinel", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
			break
		}

		// Parse the JSON payload to extract usage, errors, and finish_reason.
		// If finish_reason needs normalization, rewrite the line; otherwise
		// forward the original bytes to avoid unnecessary allocation.
		//
		// P1-B: Error accumulation. Some providers split error JSON across
		// multiple data lines (e.g. a network boundary splits
		//   data: {"error":{"message":"Rate limit
		// into two chunks). We detect lines starting with {"error" and
		// accumulate them until a non-error line arrives, then try to parse
		// the accumulated bytes as a complete error object.
		//
		// P1-C: Anthropic-style errors. When an "event: error" SSE line
		// precedes a data line, the payload is an Anthropic error event:
		//   {"type":"error","error":{"type":"overloaded_error","message":"..."}}
		// We detect this and extract the error message for logging.
		isErrorPrefix := strings.HasPrefix(payload, `{"error"`)
		if isErrorPrefix {
			// P1-B: Accumulate error JSON bytes. Some providers split error
			// responses across multiple SSE data lines. We buffer bytes until
			// a non-error chunk arrives, then try to parse the full object.
			errAccum = append(errAccum, []byte(payload)...)
		} else if len(errAccum) > 0 {
			// Non-error line arrived — flush the accumulated error.
			if accumulatedMsg := parseAccumulatedError(errAccum); accumulatedMsg != "" {
				lastErrMsg = accumulatedMsg
				errorChunkCount++
				debuglog.Warn("proxy: accumulated SSE error", "error_message", accumulatedMsg, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
			}
			errAccum = nil
		}

		// P1-C: If the preceding event was "event: error", this data line
		// is an Anthropic error payload. Extract the message regardless of
		// whether it starts with {"error" (Anthropic wraps it as
		// {"type":"error","error":{...}}).
		// Track whether P1-C already counted this error so we don't
		// double-count when chunk.Error fires for the same line.
		anthropicErrorCounted := false
		if lastAnthropicEvent == "error" {
			lastAnthropicEvent = ""
			// Try Anthropic error format: {"type":"error","error":{"type":"...","message":"..."}}
			var anthErr struct {
				Type  string `json:"type"`
				Error *struct {
					Type    string `json:"type"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(payload), &anthErr) == nil && anthErr.Error != nil {
				lastErrMsg = anthErr.Error.Message
				anthropicErrorCounted = true
				errorChunkCount++
				debuglog.Warn("proxy: Anthropic SSE error event", "error_type", anthErr.Error.Type, "error_message", anthErr.Error.Message, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
			}
		}

		var written bool
		var chunk struct {
			Choices []struct {
				Delta *struct {
					Content          *string `json:"content"`
					ReasoningContent *string `json:"reasoning_content"`
				} `json:"delta"`
				FinishReason       *string `json:"finish_reason"`
				NativeFinishReason *string `json:"native_finish_reason"` // P2-7: OpenRouter passthrough
			} `json:"choices"`
			Usage *Usage                    `json:"usage"`
			Error *struct{ Message string } `json:"error"`
		}
		jsonValid := json.Unmarshal([]byte(payload), &chunk) == nil
		if jsonValid {
			// When strip_reasoning is enabled, remove reasoning fields from the chunk.
			// If the delta becomes empty after stripping (i.e. the chunk only
			// contained reasoning tokens), skip the chunk entirely rather than
			// forwarding a hollow delta. Clients like Warp.dev disconnect when
			// they receive long sequences of empty-content chunks during a
			// thinking phase. We DO forward chunks that carry a non-empty
			// "content" field, a "role" field (first assistant chunk), or
			// "tool_calls".
			if stripReasoning && len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
				p, ok := parseChunkPayload(payload)
				if !ok {
					// parseChunkPayload failed on a chunk that passed the
					// typed-struct guard. Forward the chunk unmodified instead
					// of silently dropping it. This preserves the original
					// pass-through semantics where a parse failure forwards the
					// chunk rather than discarding it.
					goto stripReasoningDone
				}
				deltaFields := p.delta
				// Remove reasoning fields from delta.
				delete(deltaFields, "reasoning_content")
				delete(deltaFields, "reasoning_details")
				delete(deltaFields, "reasoning")

				// Strip empty content ("") that normally
				// accompanies reasoning-only deltas.
				if cRaw, okC := deltaFields["content"]; okC {
					var cStr string
					if json.Unmarshal(cRaw, &cStr) == nil && cStr == "" {
						delete(deltaFields, "content")
					}
				}

				// Some providers (notably Ollama) include
				// "role":"assistant" in every delta, not
				// just the first one. When strip_reasoning
				// is enabled and the only remaining field
				// besides content is "role", remove it
				// too — the role is already present in any
				// subsequent content or tool_calls chunk,
				// and forwarding 20+ role-only deltas
				// defeats the purpose of stripping.
				_, hasContent := deltaFields["content"]
				_, hasToolCalls := deltaFields["tool_calls"]
				if !hasContent && !hasToolCalls {
					delete(deltaFields, "role")
				}

				// Check if the delta still carries
				// meaningful data. If not, skip this chunk
				// entirely — the client has no use for an
				// empty delta. We DO forward chunks that
				// carry a finish_reason even if the delta
				// is empty; omitting the stop signal breaks
				// clients that depend on it.
				deltaHasContent := false
				if cRaw, okC := deltaFields["content"]; okC {
					var cStr string
					if json.Unmarshal(cRaw, &cStr) == nil && cStr != "" {
						deltaHasContent = true
					}
				}
				if _, okR := deltaFields["role"]; okR {
					deltaHasContent = true
				}
				if _, okT := deltaFields["tool_calls"]; okT {
					deltaHasContent = true
				}
				// finish_reason lives at the choices[0]
				// level, not inside delta. A chunk with
				// an empty delta but a finish_reason must
				// still be forwarded. Note: "finish_reason":null
				// is present on every streaming chunk, so
				// we must check the value is actually non-null.
				if frRaw, okFR := p.choices[0]["finish_reason"]; okFR {
					var frStr string
					if json.Unmarshal(frRaw, &frStr) == nil && frStr != "" {
						deltaHasContent = true
					}
				}

				if !deltaHasContent {
					// Delta is empty after stripping
					// reasoning — skip the chunk entirely
					// and send a minimal valid JSON keep-alive
					// instead of an SSE comment or nothing:
					// Warp's Go backend uses the openai-go
					// ssestream package which crashes on SSE
					// comment lines and also times out if no
					// data: lines arrive for several seconds.
					// A valid data: line with an empty delta
					// keeps the connection alive without
					// exposing reasoning.
					// Use the stream's real completion ID from
					// the parsed chunk so clients that validate
					// ID consistency don't reject the keep-alive.
					keepAliveID := "chatcmpl"
					if idRaw, ok := p.raw["id"]; ok {
						var idStr string
						if json.Unmarshal(idRaw, &idStr) == nil && idStr != "" {
							keepAliveID = idStr
						}
					}
					keepAlivePayload := map[string]interface{}{
						"id":     keepAliveID,
						"object": "chat.completion.chunk",
						"choices": []map[string]interface{}{
							{"index": 0, "delta": map[string]interface{}{}},
						},
					}
					keepAliveJSON, err := json.Marshal(keepAlivePayload)
					if err != nil {
						continue
					}
					keepAlive := append([]byte("data: "), keepAliveJSON...)
					keepAlive = append(keepAlive, "\n\n"...)
					n, err := w.Write(keepAlive)
					bytesWritten += int64(n)
					if err != nil {
						clientDisconnected = true
						debuglog.Warn("proxy: client write failed during reasoning keep-alive", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
						goto logUpdate
					}
					if canFlush {
						flusher.Flush()
					}
					skipNextEmptyLine = true
					written = true
					continue
				}

				newDelta, _ := json.Marshal(deltaFields)
				p.choices[0]["delta"] = json.RawMessage(newDelta)
				// Normalize finish_reason in-place before
				// re-serializing, so non-standard values
				// (e.g., "end_turn", "STOP") are mapped to
				// OpenAI equivalents.
				normalizeFinishReasonInChoices(p.choices, &lastFinishReason, logData.modelID, logData.providerName)
				newChoices, _ := json.Marshal(p.choices)
				p.raw["choices"] = json.RawMessage(newChoices)
				newPayload, _ := json.Marshal(p.raw)
				if err := writeSSEDataChunk(w, newPayload, &bytesWritten); err != nil {
					clientDisconnected = true
					debuglog.Warn("proxy: client write failed during reasoning strip", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
					goto logUpdate
				}
				if canFlush {
					flusher.Flush()
				}
				written = true
				skipNextEmptyLine = true
				continue
			}
		stripReasoningDone:

			// Reasoning field normalization: ensure reasoning_content is
			// always populated regardless of upstream provider format.
			// Handles: delta.reasoning (Ollama), delta.reasoning_details
			// (OpenRouter/MiniMax), <thinking> tags in delta.content.
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
				delta := chunk.Choices[0].Delta
				// Build a map from the delta fields for normalization.
				deltaMap := make(map[string]interface{})
				if delta.Content != nil {
					deltaMap["content"] = *delta.Content
				}
				if delta.ReasoningContent != nil {
					deltaMap["reasoning_content"] = *delta.ReasoningContent
				}
				// Parse the raw payload once to capture reasoning/reasoning_details
				// which aren't in the typed struct, and reuse the parsed result
				// for re-serialization below.
				chunkParsed, chunkParsedOk := parseChunkPayload(payload)
				if chunkParsedOk {
					// Extract reasoning field (Ollama, OpenRouter).
					if rRaw, ok3 := chunkParsed.delta["reasoning"]; ok3 {
						var rStr string
						if json.Unmarshal(rRaw, &rStr) == nil && rStr != "" {
							deltaMap["reasoning"] = rStr
						}
					}
					// Extract reasoning_details (OpenRouter, MiniMax).
					if rdRaw, ok3 := chunkParsed.delta["reasoning_details"]; ok3 {
						var rdArr []interface{}
						if json.Unmarshal(rdRaw, &rdArr) == nil {
							deltaMap["reasoning_details"] = rdArr
						}
					}
				}
				if NormalizeReasoningFields(deltaMap) {
					if chunkParsedOk {
						// Patch reasoning_content into the delta.
						if rc, ok3 := deltaMap["reasoning_content"]; ok3 {
							if rcStr, ok4 := rc.(string); ok4 {
								escaped, _ := json.Marshal(rcStr)
								chunkParsed.delta["reasoning_content"] = json.RawMessage(escaped)
							}
						}
						// Patch content if it was modified (tag extraction).
						if c, ok3 := deltaMap["content"]; ok3 {
							if cStr, ok4 := c.(string); ok4 {
								escaped, _ := json.Marshal(cStr)
								chunkParsed.delta["content"] = json.RawMessage(escaped)
							}
						}
						newDelta, _ := json.Marshal(chunkParsed.delta)
						chunkParsed.choices[0]["delta"] = json.RawMessage(newDelta)
						// Normalize finish_reason in-place before
						// re-serializing. The written=true below
						// would skip the finish_reason normalization
						// block later in this loop iteration.
						normalizeFinishReasonInChoices(chunkParsed.choices, &lastFinishReason, logData.modelID, logData.providerName)
						newChoices, _ := json.Marshal(chunkParsed.choices)
						chunkParsed.raw["choices"] = json.RawMessage(newChoices)
						newPayload, _ := json.Marshal(chunkParsed.raw)
						if err := writeSSEDataChunk(w, newPayload, &bytesWritten); err != nil {
							clientDisconnected = true
							debuglog.Warn("proxy: client write failed during reasoning normalization", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
							goto logUpdate
						}
						if canFlush {
							flusher.Flush()
						}
						written = true
						skipNextEmptyLine = true
						debuglog.Debug("proxy: normalized reasoning fields", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
					}
				}
			}

			// Always strip empty content:"" from reasoning chunks to avoid noise.
			// When a chunk has reasoning_content set and content is empty "", remove content.
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
				delta := chunk.Choices[0].Delta
				hasReasoning := delta.ReasoningContent != nil && *delta.ReasoningContent != ""
				hasEmptyContent := delta.Content != nil && *delta.Content == ""
				if hasReasoning && hasEmptyContent {
					if p, ok := parseChunkPayload(payload); ok {
						delete(p.delta, "content")
						newDelta, _ := json.Marshal(p.delta)
						p.choices[0]["delta"] = json.RawMessage(newDelta)
						// Normalize finish_reason in-place before
						// re-serializing. The written=true below
						// would skip the finish_reason normalization
						// block later in this loop iteration.
						normalizeFinishReasonInChoices(p.choices, &lastFinishReason, logData.modelID, logData.providerName)
						newChoices, _ := json.Marshal(p.choices)
						p.raw["choices"] = json.RawMessage(newChoices)
						newPayload, _ := json.Marshal(p.raw)
						if err := writeSSEDataChunk(w, newPayload, &bytesWritten); err != nil {
							clientDisconnected = true
							debuglog.Warn("proxy: client write failed during empty content strip", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
							goto logUpdate
						}
						if canFlush {
							flusher.Flush()
						}
						written = true
						skipNextEmptyLine = true
						debuglog.Debug("proxy: stripped empty content from reasoning chunk", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
					}
				}
			}

			if chunk.Usage != nil {
				promptTokens = chunk.Usage.PromptTokens
				completionTokens = chunk.Usage.CompletionTokens
				if chunk.Usage.CompletionTokensDetails != nil && chunk.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
					reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
				}
				if hit, miss := extractCacheTokens(*chunk.Usage); hit > 0 || miss > 0 {
					promptCacheHitTokens = hit
					promptCacheMissTokens = miss
				}
			}
			// P2-7: Log native_finish_reason from OpenRouter for debugging.
			// OpenRouter includes this field alongside the normalized finish_reason,
			// preserving the original provider's value (e.g. "STOP" instead of "stop").
			if len(chunk.Choices) > 0 && chunk.Choices[0].NativeFinishReason != nil {
				if *chunk.Choices[0].NativeFinishReason != lastNativeFinishReason {
					lastNativeFinishReason = *chunk.Choices[0].NativeFinishReason
					debuglog.Debug("proxy: native_finish_reason", "native_finish_reason", lastNativeFinishReason, "model", logData.modelID, "provider", logData.providerName)
				}
			}
			// P2-5: Detect repeated identical content. Some models (notably
			// xAI Grok reasoning) send the same reasoning text in consecutive
			// deltas, causing "Thinking... Thinking... Thinking..." loops.
			// We track consecutive identical content and log a warning when
			// the threshold is exceeded.
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
				delta := chunk.Choices[0].Delta
				currentContent := ""
				if delta.Content != nil {
					currentContent = *delta.Content
				}
				if delta.ReasoningContent != nil && currentContent == "" {
					currentContent = *delta.ReasoningContent
					if !sawThinking {
						sawThinking = true
						debuglog.Debug("proxy: thinking/reasoning block started", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
					}
				}
				if currentContent == lastContent && currentContent != "" {
					repeatedCount++
					if repeatedCount == repeatedContentLimit {
						preview := currentContent
						if len(preview) > 50 {
							runes := []rune(preview)
							if len(runes) > 50 {
								preview = string(runes[:50]) + "..."
							}
						}
						debuglog.Warn("proxy: repeated content detected in stream", "repeated_count", repeatedCount, "content_preview", preview, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
					}
				} else {
					repeatedCount = 0
				}
				lastContent = currentContent
			}
			if chunk.Error != nil && !anthropicErrorCounted {
				// Only count if P1-C didn't already handle this as an
				// Anthropic error event (which shares the same data line).
				lastErrMsg = chunk.Error.Message
				errorChunkCount++
				debuglog.Warn("proxy: SSE error chunk", "model", logData.modelID, "provider", logData.providerName, "error_message", chunk.Error.Message, "chunk_number", chunkCount)
				// Clear errAccum: chunk.Error already captured this error,
				// so P1-B's next flush must not re-count it.
				errAccum = nil
			}
			// Normalize provider-specific finish_reason values (e.g.,
			// "STOP" from Gemini, "end_turn" from Anthropic) to OpenAI
			// equivalents so downstream clients see consistent values.
			if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != nil {
				normalized := normalizeFinishReason(*chunk.Choices[0].FinishReason)

				// P2-2: Suppress duplicate finish_reason chunks. Some providers
				// (notably OpenRouter routing to Gemini models) send two
				// consecutive chunks with the same finish_reason, where the
				// second has no content or usage. This causes downstream
				// "Model stream ended with empty response text" errors.
				// Only suppress if: same finish_reason as previous chunk,
				// no content (delta is empty or absent), and no usage.
				if normalized == lastFinishReason {
					hasContent := false
					if chunk.Choices[0].Delta != nil {
						delta := chunk.Choices[0].Delta
						if delta.Content != nil && *delta.Content != "" {
							hasContent = true
						}
						if delta.ReasoningContent != nil && *delta.ReasoningContent != "" {
							hasContent = true
						}
					}
					if !hasContent && chunk.Usage == nil {
						debuglog.Debug("proxy: suppressing duplicate finish_reason chunk", "finish_reason", normalized, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
						// Skip writing this chunk — it's a bare duplicate.
						// Also skip the following SSE separator to avoid
						// an orphaned empty-line event.
						skipNextEmptyLine = true
						continue
					}
				}
				lastFinishReason = normalized
				if normalized != *chunk.Choices[0].FinishReason && !written {
					// Rewrite the line with normalized finish_reason.
					// Re-serialize the entire JSON payload with the fix.
					// This is the uncommon path — most providers already
					// send OpenAI-compatible values.
					var raw map[string]json.RawMessage
					if json.Unmarshal([]byte(payload), &raw) == nil {
						if choicesRaw, ok := raw["choices"]; ok {
							var choices []map[string]json.RawMessage
							if json.Unmarshal(choicesRaw, &choices) == nil && len(choices) > 0 {
								if frRaw, ok2 := choices[0]["finish_reason"]; ok2 {
									// Replace the finish_reason value.
									var newFR string
									if json.Unmarshal(frRaw, &newFR) == nil {
										choices[0]["finish_reason"] = json.RawMessage(`"` + normalized + `"`)
									}
								}
								// Re-serialize choices and patch into the payload map.
								if newChoices, err2 := json.Marshal(choices); err2 == nil {
									raw["choices"] = json.RawMessage(newChoices)
									if newPayload, err3 := json.Marshal(raw); err3 == nil {
										if err := writeSSEDataChunk(w, newPayload, &bytesWritten); err != nil {
											clientDisconnected = true
											debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
											goto logUpdate
										}
										if canFlush {
											flusher.Flush()
										}
										written = true
										skipNextEmptyLine = true
										debuglog.Debug("proxy: normalized finish_reason", "original", *chunk.Choices[0].FinishReason, "normalized", normalized, "model", logData.modelID, "provider", logData.providerName)
									}
								}
							}
						}
					}
				}
			}
		}
		if !written && !jsonValid {
			// Skip invalid/truncated JSON chunks instead of forwarding them.
			// Forwarding broken JSON causes downstream clients (e.g. Warp.dev)
			// to fail with JSON parse errors, crashing the entire stream.
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
			skipNextEmptyLine = true
			continue
		}
		if !written {
			// No normalization needed — forward the original line.
			var n int
			var err error
			if n, err = w.Write(line); err != nil {
				clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten)
				goto logUpdate
			}
			bytesWritten += int64(n)
			if n, err = w.Write([]byte("\n\n")); err != nil {
				clientDisconnected = true
				debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten)
				goto logUpdate
			}
			bytesWritten += int64(n)
			if canFlush {
				flusher.Flush()
			}
			skipNextEmptyLine = true
		}
	}

	// Flush any remaining accumulated error bytes at stream end.
	if len(errAccum) > 0 {
		if accumulatedMsg := parseAccumulatedError(errAccum); accumulatedMsg != "" {
			lastErrMsg = accumulatedMsg
			errorChunkCount++
			debuglog.Warn("proxy: accumulated SSE error (stream end)", "error_message", accumulatedMsg, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
		}
	}

logUpdate:
	// Signal watchdog to stop on all exit paths (goto or normal loop exit).
	if watchdogDone != nil {
		close(watchdogDone)
	}
	totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
	var tps float64
	// Use total output tokens (text + reasoning) for TPS numerator,
	// and generation time as denominator. Prefer true TTFT (first token)
	// when the probe measured it; fall back to response header time.
	totalOutputTokens := completionTokens + reasoningTokens
	ttftForTPS := opts.responseHeaderMs
	if opts.trueTtftMs > 0 {
		ttftForTPS = opts.trueTtftMs
	}
	generationDuration := totalDuration - ttftForTPS
	// Avoid absurd TPS when generation time is negligible
	// (e.g. non-streaming where response_header_ms ≈ duration_ms).
	minGeneration := max(1.0, totalDuration*0.05)
	if totalOutputTokens > 0 && generationDuration >= minGeneration {
		tps = float64(totalOutputTokens) / float64(generationDuration) * 1000
	} else if totalOutputTokens > 0 && totalDuration > 0 {
		tps = float64(totalOutputTokens) / float64(totalDuration) * 1000
	}

	// Check for stream stall before error classification.
	stalled := atomic.LoadInt32(&streamStalled) == 1

	errMsg := lastErrMsg
	if errMsg == "" && scanner.Err() != nil {
		scannerErr := scanner.Err()
		switch {
		case errors.Is(scannerErr, context.Canceled):
			// The scanner caught the cancellation before the select between
			// iterations could. This is always a client disconnect — the
			// parent request context was cancelled.
			clientDisconnected = true
		case errors.Is(scannerErr, context.DeadlineExceeded):
			// A derived context's deadline expired (failover or retry timeout).
			// Use cancelOrigin to produce a human-readable message.
			switch opts.cancelOrigin {
			case "retry_timeout":
				errMsg = "stream interrupted: param-strip retry timed out"
			case "failover_timeout":
				errMsg = "stream interrupted: upstream request timed out"
			default:
				// Unknown origin — preserve the value rather than guessing.
				errMsg = fmt.Sprintf("stream interrupted: %s", humanReadableCancelOrigin(opts.cancelOrigin))
			}
		default:
			errMsg = scannerErr.Error()
		}
	}
	if clientDisconnected {
		errMsg = "client disconnected"
		debuglog.Warn("proxy: client disconnected during streaming", "model", logData.modelID)
	}
	// Stall detection takes precedence over the raw IO error produced by
	// the watchdog's body.Close(). Replace it with a descriptive message.
	// Only flag a stall when we did NOT see [DONE] — if the stream completed
	// normally, a late timer fire is a false positive. Also skip when the
	// client disconnected, which is a more meaningful diagnosis.
	if stalled && !sawDone && !clientDisconnected {
		effectiveStall := opts.streamStallTimeout
		if chunkCount > progressiveChunkThreshold {
			effectiveStall = opts.streamStallTimeout * progressiveStallMultiplier
		}
		errMsg = fmt.Sprintf("stream stalled: no data for %s", effectiveStall)
		debuglog.Warn("proxy: stream stall detected", "model", logData.modelID, "provider", logData.providerName, "stall_timeout", effectiveStall, "base_timeout", opts.streamStallTimeout, "chunks", chunkCount)
	}
	if errMsg == "" && !sawDone {
		// Upstream closed without [DONE] sentinel. If we received content and
		// the scanner didn't error, inject the sentinel for the downstream
		// client so the frontend knows the stream completed normally.
		if !clientDisconnected && scanner.Err() == nil && chunkCount > 0 {
			debuglog.Info("proxy: upstream omitted [DONE] sentinel; injecting for downstream", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
			if _, err := w.Write([]byte("data: [DONE]\n\n")); err != nil {
				debuglog.Warn("proxy: failed to write injected [DONE]", "model", logData.modelID, "provider", logData.providerName, "error", err)
			} else if canFlush {
				flusher.Flush()
			}
			// Stream was complete; the missing sentinel is benign.
			debuglog.Info("proxy: stream completed (upstream omitted [DONE])", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
		} else {
			// No content received or scanner error - genuinely truncated.
			errMsg = "stream truncated: upstream closed connection without [DONE] sentinel"
			debuglog.Warn("proxy: stream ended without [DONE] sentinel", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
		}
	}

	logData.statusCode = resp.StatusCode
	logData.durationMs = totalDuration
	logData.proxyOverheadMs = opts.proxyOverheadMs
	logData.parseMs = opts.parseMs
	logData.failoverLookupMs = opts.failoverLookupMs
	logData.modelLookupMs = opts.modelLookupMs
	logData.providerLookupMs = opts.providerLookupMs
	logData.keyDecryptMs = opts.keyDecryptMs
	logData.dialMs = opts.dialMs
	logData.responseHeaderMs = opts.responseHeaderMs
	logData.tokensPerSecond = tps
	logData.tokensPrompt = promptTokens
	logData.tokensCompletion = completionTokens
	logData.tokensCompletionReasoning = reasoningTokens
	logData.tokensPromptCacheHit = promptCacheHitTokens
	logData.tokensPromptCacheMiss = promptCacheMissTokens
	logData.errorMessage = errMsg
	logData.failoverAttempt = opts.attempt
	if errMsg != "" {
		logData.statusCode = 0
		logData.state = "failed"
	} else {
		logData.state = "completed"
	}
	h.updateRequestLog(logData)

	// Record circuit breaker failure for stream stalls.
	// Guard with !sawDone to avoid penalising a provider whose stream completed
	// normally but whose stall timer fired concurrently with [DONE].
	if stalled && !sawDone && !clientDisconnected && opts.circuitBreakerOn {
		h.circuitBreaker.RecordFailure(opts.providerID, opts.providerName)
		debuglog.Debug("proxy: recorded circuit breaker failure for stream stall", "provider", opts.providerName, "provider_id", opts.providerID)
	}

	debuglog.Info("proxy: streaming finished", "model", logData.modelID, "provider", logData.providerName, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "true_ttft_ms", opts.trueTtftMs, "duration_ms", totalDuration, "chunks", chunkCount, "bytes_written", bytesWritten, "prompt_tokens", promptTokens, "completion_tokens", completionTokens, "error_chunks", errorChunkCount, "has_error", errMsg != "")
	if errMsg != "" {
		debuglog.Warn("proxy: streaming error", "model", logData.modelID, "provider", logData.providerName, "error", errMsg, "upstream_status", resp.StatusCode, "attempt", opts.attempt, "duration_ms", totalDuration)
	} else {
		debuglog.Debug("proxy: streaming completed successfully", "model", logData.modelID, "provider", logData.providerName, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "duration_ms", totalDuration)
	}

	if opts.vkHash != "" && !clientDisconnected {
		h.recordTokenUsage(opts.vkHash, promptTokens, completionTokens, reasoningTokens, logData.virtualKeyName)
	}
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
		h.updateRequestLog(logData)

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
		h.updateRequestLog(logData)
		debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName)
		debuglog.Debug("proxy: non-streaming error details", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "error", errMsg, "duration_ms", totalDuration)
		writeOpenAIError(w, fmt.Sprintf("upstream provider returned HTTP %d", resp.StatusCode), resp.StatusCode)
	}
}

// failRequest populates logData with failure details and updates the request log.
// Always populates all timing fields from timings - if zero-valued, they record as 0ms.
func (h *Handler) failRequest(logData *requestLogData, statusCode int, errMsg string, attempt int, startTime time.Time, parseMs float64, timings resolveTimings, proxyOverhead float64) {
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
	logData.failoverAttempt = attempt
	logData.state = "failed"
	h.updateRequestLog(logData)
}

// ChatCompletions handles OpenAI-compatible chat completion requests with failover support.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
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
				h.failRequest(logData, 400, "failed to read request body", 0, startTime, parseMs, resolveTimings{}, 0)
				writeOpenAIError(w, "failed to read request body", http.StatusBadRequest)
				return
			}
			_ = r.Body.Close()
		}

		var req ChatCompletionRequest
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			debuglog.Warn("proxy: failed to parse request body", "error", err)
			publishRequestStartedEvent(logData)
			h.failRequest(logData, 400, "invalid request body", 0, startTime, parseMs, resolveTimings{}, 0)
			writeOpenAIError(w, "invalid request body", http.StatusBadRequest)
			return
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
		h.failRequest(logData, 400, "model is required", 0, startTime, parseMs, resolveTimings{}, 0)
		writeOpenAIError(w, "model is required", http.StatusBadRequest)
		return
	}

	debuglog.Info("proxy: request start", "model", reqModel, "stream", isStreaming, "key", vkName, "client_ip", r.RemoteAddr)
	debuglog.Debug("proxy: request details", "model", reqModel, "stream", isStreaming, "key", vkName, "vk_id", vkID, "has_hash", vkHash != "", "body_length", len(bodyBytes))

	var candidates []modelCandidate
	var timings resolveTimings
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
	case strings.HasPrefix(reqModel, "hotel/"):
		isFailover = true
		debuglog.Debug("proxy: model resolution path", "type", "hotel", "model", reqModel)
		displayModel := strings.ToLower(strings.TrimPrefix(reqModel, "hotel/"))
		candidates, timings, err = h.resolveHotelModel(r.Context(), displayModel)
		if err != nil {
			h.failRequest(logData, 404, err.Error(), 0, startTime, parseMs, timings, 0)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return
		}
		if len(candidates) == 0 {
			h.failRequest(logData, 502, "no available provider for hotel/"+displayModel, 0, startTime, parseMs, timings, 0)
			writeOpenAIError(w, "no available provider for hotel/"+displayModel, http.StatusBadGateway)
			return
		}
	case strings.Contains(reqModel, "/") && !strings.HasPrefix(reqModel, "hotel/"):
		debuglog.Debug("proxy: model resolution path", "type", "specific_provider", "model", reqModel)
		parts := strings.SplitN(reqModel, "/", 2)
		providerName, modelID := parts[0], parts[1]
		candidates, timings, err = h.resolveSpecificProvider(r.Context(), providerName, modelID)
		if err != nil {
			h.failRequest(logData, 404, err.Error(), 0, startTime, parseMs, timings, 0)
			writeOpenAIError(w, err.Error(), http.StatusNotFound)
			return
		}
	default:
		h.failRequest(logData, 400, "invalid model format: "+reqModel, 0, startTime, parseMs, timings, 0)
		writeOpenAIError(w, "invalid model format, expected provider/model or hotel/model", http.StatusBadRequest)
		return
	}

	// Normalize logData fields after resolution: split the raw request model
	// (e.g. "NanoGPT/deepseek-ai/DeepSeek-R1-0528") into provider name and
	// model-only components so log lines are human-readable.
	if parts := strings.SplitN(reqModel, "/", 2); len(parts) == 2 && !strings.HasPrefix(reqModel, "hotel/") {
		logData.providerName = parts[0]
		logData.modelID = parts[1]
	} else {
		logData.providerName = "hotel"
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
				h.failRequest(logData, 403, "virtual key does not have access to any provider for this model", 0, startTime, parseMs, timings, 0)
				writeOpenAIError(w, "virtual key does not have access to any provider for this model", http.StatusForbidden)
				return
			}
			debuglog.Info("proxy: filtered candidates by allowed_providers", "before", len(candidates), "after", len(filtered), "key", vkName)
			candidates = filtered
		}
	}

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
				h.failRequest(logData, 499, "client disconnected during failover", attempt-1, startTime, parseMs, timings, proxyOverhead)
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
			var raw map[string]interface{}
			if json.Unmarshal(proxyReqBody, &raw) == nil {
				if reqModel != candidate.model.ModelID {
					raw["model"] = candidate.model.ModelID
				}
				// Inject stream_options for streaming requests to OpenAI-compatible
				// providers. This must happen AFTER providerUnsupportedParams stripping
				// so that providers which reject stream_options (Anthropic, Google, etc.)
				// never receive it. The function below handles the ordering.
				if isStreaming && providerSupportsStreamOptions(providerType) {
					raw["stream_options"] = map[string]interface{}{
						"include_usage": true,
					}
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
				// Inject provider-specific params required for reasoning/thinking.
				// Clients don't know which upstream provider they're talking to,
				// so the proxy must add these automatically.
				InjectProviderParams(raw, providerType, candidate.model.ModelID)
				if b, err := json.Marshal(raw); err == nil {
					upstreamBody = b
				}
			}
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
			}
		}

		responseHeaderMs := float64(time.Since(startTime).Microseconds()) / 1000.0

		hasMoreCandidates := attempt < len(candidates)-1
		isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)
		isProviderHealthFailure := resp.StatusCode >= 500 || resp.StatusCode == 429

		if isFailoverEligible {
			// Record failure for circuit breaker only for provider-level errors.
			// 5xx = server error (provider unhealthy), 429 = rate limit (provider overloaded).
			// Client errors (401/403/404/499) mean the provider is alive but rejecting
			// this specific request/model — they should open failover, not trip the breaker.
			if circuitBreakerEnabled && isProviderHealthFailure {
				h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
			} else if circuitBreakerEnabled {
				// Provider returned a client error (4xx) — it's alive and responding,
				// just not with what we wanted. Record as a success so one stale model
				// in a failover group doesn't take an entire provider offline.
				h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
			}
		} else {
			// Provider responded (even with a non-failover error like 400) —
			// it's alive from a health perspective.
			// For streaming 200 responses, recording is deferred until after the
			// TTFT probe succeeds, to avoid double-counting with the stall watchdog.
			// Streaming non-200 responses never reach the TTFT probe (they continue
			// before it), so record now to maintain parity with non-streaming paths.
			if circuitBreakerEnabled && (!isStreaming || resp.StatusCode != http.StatusOK) {
				h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
			}
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
			h.failRequest(logData, resp.StatusCode, errMsg, attempt, startTime, parseMs, timings, proxyOverhead)

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
		h.failRequest(logData, 502, fmt.Sprintf("all providers failed: %s", lastErr), len(candidates)-1, startTime, parseMs, timings, proxyOverhead)
		writeOpenAIError(w, fmt.Sprintf("all providers failed for model %s", reqModel), http.StatusBadGateway)
	} else {
		h.failRequest(logData, 502, fmt.Sprintf("provider request failed: %s", lastErr), len(candidates)-1, startTime, parseMs, timings, proxyOverhead)
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
