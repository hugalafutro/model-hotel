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
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// newRequestWithContext is injectable for testing request creation errors.
var newRequestWithContext = http.NewRequestWithContext

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, failoverLookupMs, modelLookupMs, providerLookupMs, keyDecryptMs, dialMs, settingsReadMs, ttft float64, vkHash string, attempt int) {
	defer func() {
		// Drain remaining bytes so the Transport reuses the connection.
		// Skip drain if the client already disconnected: the upstream body
		// could be large and we'd block the goroutine for no benefit.
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	debuglog.Debug("proxy: handleStreamingResponse entered", "model", logData.modelID, "provider", logData.providerName, "upstream_status", resp.StatusCode, "attempt", attempt, "ttft_ms", ttft)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	debuglog.Debug("proxy: streaming headers sent", "model", logData.modelID, "provider", logData.providerName)

	logData.statusCode = resp.StatusCode
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.failoverLookupMs = failoverLookupMs
	logData.modelLookupMs = modelLookupMs
	logData.providerLookupMs = providerLookupMs
	logData.keyDecryptMs = keyDecryptMs
	logData.dialMs = dialMs
	logData.settingsReadMs = settingsReadMs
	logData.ttftMs = ttft
	logData.failoverAttempt = attempt
	logData.state = "streaming"
	h.updateRequestLog(logData)

	flusher, canFlush := w.(http.Flusher)

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024) // 4MB per line
	debuglog.Debug("proxy: streaming scanner created", "model", logData.modelID, "provider", logData.providerName)
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

	for scanner.Scan() {
		line := scanner.Bytes()
		chunkCount++

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
		if json.Unmarshal([]byte(payload), &chunk) == nil {
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
				// Parse the raw payload to capture reasoning/reasoning_details
				// which aren't in the typed struct.
				var rawDelta map[string]json.RawMessage
				if json.Unmarshal([]byte(payload), &rawDelta) == nil {
					if choicesRaw, ok := rawDelta["choices"]; ok {
						var choices []map[string]json.RawMessage
						if json.Unmarshal(choicesRaw, &choices) == nil && len(choices) > 0 {
							if deltaRaw, ok2 := choices[0]["delta"]; ok2 {
								var deltaFields map[string]json.RawMessage
								if json.Unmarshal(deltaRaw, &deltaFields) == nil {
									// Extract reasoning field (Ollama, OpenRouter).
									if rRaw, ok3 := deltaFields["reasoning"]; ok3 {
										var rStr string
										if json.Unmarshal(rRaw, &rStr) == nil && rStr != "" {
											deltaMap["reasoning"] = rStr
										}
									}
									// Extract reasoning_details (OpenRouter, MiniMax).
									if rdRaw, ok3 := deltaFields["reasoning_details"]; ok3 {
										var rdArr []interface{}
										if json.Unmarshal(rdRaw, &rdArr) == nil {
											deltaMap["reasoning_details"] = rdArr
										}
									}
								}
							}
						}
					}
				}
				if NormalizeReasoningFields(deltaMap) {
					// Re-serialize the chunk with normalized reasoning fields.
					// Parse the full payload as a raw map, patch the delta,
					// and write the modified JSON.
					var raw map[string]json.RawMessage
					if json.Unmarshal([]byte(payload), &raw) == nil {
						if choicesRaw, ok := raw["choices"]; ok {
							var choices []map[string]json.RawMessage
							if json.Unmarshal(choicesRaw, &choices) == nil && len(choices) > 0 {
								if deltaRaw, ok2 := choices[0]["delta"]; ok2 {
									var deltaFields map[string]json.RawMessage
									if json.Unmarshal(deltaRaw, &deltaFields) == nil {
										// Patch reasoning_content into the delta.
										if rc, ok3 := deltaMap["reasoning_content"]; ok3 {
											if rcStr, ok4 := rc.(string); ok4 {
												escaped, _ := json.Marshal(rcStr)
												deltaFields["reasoning_content"] = json.RawMessage(escaped)
											}
										}
										// Patch content if it was modified (tag extraction).
										if c, ok3 := deltaMap["content"]; ok3 {
											if cStr, ok4 := c.(string); ok4 {
												escaped, _ := json.Marshal(cStr)
												deltaFields["content"] = json.RawMessage(escaped)
											}
										}
										newDelta, _ := json.Marshal(deltaFields)
										choices[0]["delta"] = json.RawMessage(newDelta)
										newChoices, _ := json.Marshal(choices)
										raw["choices"] = json.RawMessage(newChoices)
										newPayload, _ := json.Marshal(raw)
										n, err := w.Write([]byte("data: "))
										bytesWritten += int64(n)
										if err != nil {
											clientDisconnected = true
											debuglog.Warn("proxy: client write failed during reasoning normalization", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
											goto logUpdate
										}
										n, err = w.Write(newPayload)
										bytesWritten += int64(n)
										if err != nil {
											clientDisconnected = true
											debuglog.Warn("proxy: client write failed during reasoning normalization", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
											goto logUpdate
										}
										n, err = w.Write([]byte("\n\n"))
										bytesWritten += int64(n)
										if err != nil {
											clientDisconnected = true
											debuglog.Warn("proxy: client write failed during reasoning normalization (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
											goto logUpdate
										}
										if canFlush {
											flusher.Flush()
										}
										written = true
										debuglog.Debug("proxy: normalized reasoning fields", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
									}
								}
							}
						}
					}
				}
			}
			if chunk.Usage != nil {
				promptTokens = chunk.Usage.PromptTokens
				completionTokens = chunk.Usage.CompletionTokens
				if chunk.Usage.CompletionTokensDetails != nil && chunk.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
					reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
				}
				if chunk.Usage.PromptCacheHitTokens > 0 {
					promptCacheHitTokens = chunk.Usage.PromptCacheHitTokens
					promptCacheMissTokens = chunk.Usage.PromptTokens - chunk.Usage.PromptCacheHitTokens
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
							preview = preview[:50] + "..."
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
										n, err := w.Write([]byte("data: "))
										bytesWritten += int64(n)
										if err != nil {
											clientDisconnected = true
											debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
											goto logUpdate
										}
										n, err = w.Write(newPayload)
										bytesWritten += int64(n)
										if err != nil {
											clientDisconnected = true
											debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
											goto logUpdate
										}
										n, err = w.Write([]byte("\n\n"))
										bytesWritten += int64(n)
										if err != nil {
											clientDisconnected = true
											debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", bytesWritten)
											goto logUpdate
										}
										if canFlush {
											flusher.Flush()
										}
										written = true
										debuglog.Debug("proxy: normalized finish_reason", "original", *chunk.Choices[0].FinishReason, "normalized", normalized, "model", logData.modelID, "provider", logData.providerName)
									}
								}
							}
						}
					}
				}
			}
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
	totalDuration := float64(time.Since(startTime).Microseconds()) / 1000.0
	var tps float64
	// Use total output tokens (text + reasoning) for TPS numerator,
	// and generation time (duration minus TTFT) as denominator.
	// TTFT includes thinking/reasoning time which isn't generation throughput.
	totalOutputTokens := completionTokens + reasoningTokens
	generationDuration := totalDuration - ttft
	if totalOutputTokens > 0 && generationDuration > 0 {
		tps = float64(totalOutputTokens) / float64(generationDuration) * 1000
	} else if totalOutputTokens > 0 && totalDuration > 0 {
		tps = float64(totalOutputTokens) / float64(totalDuration) * 1000
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
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.failoverLookupMs = failoverLookupMs
	logData.modelLookupMs = modelLookupMs
	logData.providerLookupMs = providerLookupMs
	logData.keyDecryptMs = keyDecryptMs
	logData.dialMs = dialMs
	logData.ttftMs = ttft
	logData.tokensPerSecond = tps
	logData.tokensPrompt = promptTokens
	logData.tokensCompletion = completionTokens
	logData.tokensCompletionReasoning = reasoningTokens
	logData.tokensPromptCacheHit = promptCacheHitTokens
	logData.tokensPromptCacheMiss = promptCacheMissTokens
	logData.errorMessage = errMsg
	logData.failoverAttempt = attempt
	if errMsg != "" {
		logData.statusCode = 0
		logData.state = "failed"
	} else {
		logData.state = "completed"
	}
	h.updateRequestLog(logData)

	debuglog.Info("proxy: streaming finished", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "ttft_ms", ttft, "duration_ms", totalDuration, "chunks", chunkCount, "bytes_written", bytesWritten, "prompt_tokens", promptTokens, "completion_tokens", completionTokens, "error_chunks", errorChunkCount, "has_error", errMsg != "")
	if errMsg != "" {
		debuglog.Warn("proxy: streaming error", "model", logData.modelID, "provider", logData.providerName, "error", errMsg, "upstream_status", resp.StatusCode, "attempt", attempt, "duration_ms", totalDuration)
	} else {
		debuglog.Debug("proxy: streaming completed successfully", "model", logData.modelID, "provider", logData.providerName, "attempt", attempt, "ttft_ms", ttft, "duration_ms", totalDuration)
	}

	if vkHash != "" && !clientDisconnected {
		totalTokens := promptTokens + completionTokens + reasoningTokens
		tokCtx, tokCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer tokCancel()
		if err := h.virtualKeyRepo.AddTokens(tokCtx, vkHash, totalTokens); err != nil {
			keyLabel := vkHash
			if logData.virtualKeyName != "" {
				keyLabel = logData.virtualKeyName
			}
			events.Publish(events.Event{
				Type:     "tokens.error",
				Severity: "error",
				Source:   "proxy",
				Message:  fmt.Sprintf("Token counting failed for key %q", keyLabel),
				Metadata: map[string]interface{}{"error": err.Error(), "key": keyLabel},
			})
		}
	}
}

func (h *Handler) handleNonStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, proxyOverhead, parseMs, failoverLookupMs, modelLookupMs, providerLookupMs, keyDecryptMs, dialMs, settingsReadMs, ttft float64, vkHash string, attempt int) {
	defer func() {
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	debuglog.Debug("proxy: handleNonStreamingResponse entered", "model", logData.modelID, "provider", logData.providerName, "upstream_status", resp.StatusCode, "attempt", attempt, "ttft_ms", ttft)

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
		generationDuration := totalDuration - ttft
		if totalOutputTokens > 0 && generationDuration > 0 {
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
		logData.ttftMs = ttft
		logData.tokensPerSecond = tps
		logData.tokensPrompt = chatResp.Usage.PromptTokens
		logData.tokensCompletion = chatResp.Usage.CompletionTokens
		logData.tokensCompletionReasoning = reasoningTokens
		if chatResp.Usage.PromptCacheHitTokens > 0 {
			logData.tokensPromptCacheHit = chatResp.Usage.PromptCacheHitTokens
			logData.tokensPromptCacheMiss = chatResp.Usage.PromptTokens - chatResp.Usage.PromptCacheHitTokens
		}
		logData.failoverAttempt = attempt
		logData.state = "completed"
		h.updateRequestLog(logData)

		if vkHash != "" {
			totalTokens := chatResp.Usage.PromptTokens + chatResp.Usage.CompletionTokens + reasoningTokens
			tokCtx, tokCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer tokCancel()
			if err := h.virtualKeyRepo.AddTokens(tokCtx, vkHash, totalTokens); err != nil {
				keyLabel := vkHash
				if logData.virtualKeyName != "" {
					keyLabel = logData.virtualKeyName
				}
				events.Publish(events.Event{
					Type:     "tokens.error",
					Severity: "error",
					Source:   "proxy",
					Message:  fmt.Sprintf("Token counting failed for key %q", keyLabel),
					Metadata: map[string]interface{}{"error": err.Error(), "key": keyLabel},
				})
			}
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
		errMsg := util.SanitizeLogBody(string(body), 500)
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
		logData.ttftMs = ttft
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
	if reqModel == "" {
		parseStart := time.Now()
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

	if reqModel == "" {
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

	debuglog.Info("proxy: request start", "model", reqModel, "stream", isStreaming, "key", vkName, "client_ip", r.RemoteAddr)
	debuglog.Debug("proxy: request details", "model", reqModel, "stream", isStreaming, "key", vkName, "vk_id", vkID, "has_hash", vkHash != "", "body_length", len(bodyBytes))

	logData := &requestLogData{
		modelID:         reqModel,
		streaming:       isStreaming,
		virtualKeyName:  vkName,
		virtualKeyID:    vkID,
		failoverAttempt: 0,
		state:           "pending",
	}
	h.insertRequestLogAsync(logData)

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

	var proxyReqBody []byte
	if isStreaming {
		var raw map[string]interface{}
		if json.Unmarshal(bodyBytes, &raw) == nil {
			raw["stream_options"] = map[string]interface{}{
				"include_usage": true,
			}
			if b, err := json.Marshal(raw); err == nil {
				proxyReqBody = b
				debuglog.Debug("proxy: injected stream_options into request", "model", logData.modelID, "provider", logData.providerName)
			}
		}
	}
	if proxyReqBody == nil {
		proxyReqBody = bodyBytes
	}

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
		needsRewrite := reqModel != candidate.model.ModelID || providerType == "anthropic" || NeedsProviderInjection(providerType)
		debuglog.Debug("proxy: request rewrite check", "needs_rewrite", needsRewrite, "request_model", logData.modelID, "provider", logData.providerName, "resolved_model", candidate.model.ModelID, "provider_type", providerType)
		if needsRewrite {
			var raw map[string]interface{}
			if json.Unmarshal(proxyReqBody, &raw) == nil {
				if reqModel != candidate.model.ModelID {
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

		upstreamClient := &http.Client{
			Transport: h.upstreamTransport,
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
				lastErr = fmt.Sprintf("attempt %d: %s: %v", attempt, cancelOrigin, err)
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
							retryCtx, rc := context.WithTimeout(r.Context(), failoverTimeout)
							retryCtx = context.WithValue(retryCtx, ctxkeys.CancelOriginKey, "retry_timeout")
							retryCtx = context.WithValue(retryCtx, ctxkeys.DialMsKey, &dialMs)
							retryCancel = rc
							retryReq, retryErr := newRequestWithContext(retryCtx, "POST", targetURL, bytes.NewReader(rebuilt))
							if retryErr != nil {
								retryCancel()
								lastErr = fmt.Sprintf("attempt %d: failed to create retry request: %v", attempt, retryErr)
								continue
							}
							util.SetProviderAuthHeaders(retryReq, providerType, candidate.apiKey)
							retryReq.Header.Set("Content-Type", "application/json")
							retryClient := &http.Client{Transport: h.upstreamTransport}
							resp, retryErr = retryClient.Do(retryReq)
							if retryErr != nil {
								retryCancel() // no body to consume on retry error
								debuglog.Warn("proxy: auto-retry request failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", retryErr)
								lastErr = fmt.Sprintf("attempt %d: retry error: %v", attempt, retryErr)
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

		ttft := float64(time.Since(startTime).Microseconds()) / 1000.0

		hasMoreCandidates := attempt < len(candidates)-1
		isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

		if isFailoverEligible {
			// Upstream is unhealthy — record failure for circuit breaker.
			if circuitBreakerEnabled {
				h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
			}
		} else {
			// Provider responded (even with a non-failover error like 400) —
			// it's alive from a health perspective.
			if circuitBreakerEnabled {
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
			errMsg := util.SanitizeLogBody(string(body), 2000)
			debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "body", errMsg)
			debuglog.Debug("proxy: upstream error response", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "body_length", len(body), "attempt", attempt+1)
			logData.ttftMs = ttft
			h.failRequest(logData, resp.StatusCode, errMsg, attempt, startTime, parseMs, timings, proxyOverhead)
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

		debuglog.Debug("proxy: upstream responded OK, dispatching to handler", "stream", isStreaming, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
		if isStreaming {
			h.handleStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, timings.failoverLookupMs, timings.modelLookupMs, timings.providerLookupMs, timings.keyDecryptMs, timings.dialMs, timings.settingsReadMs, ttft, vkHash, attempt)
			failoverCancel() // body consumed by handleStreamingResponse
			if retryCancel != nil {
				retryCancel()
			}
			return
		}

		h.handleNonStreamingResponse(w, r, logData, resp, startTime, proxyOverhead, parseMs, timings.failoverLookupMs, timings.modelLookupMs, timings.providerLookupMs, timings.keyDecryptMs, timings.dialMs, timings.settingsReadMs, ttft, vkHash, attempt)
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
