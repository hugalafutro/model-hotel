package proxy

import (
	"encoding/json"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// captureSSEError handles the two error-extraction quirks over a data line:
// P1-B split-error accumulation (providers that split an {"error":…} object
// across multiple SSE data lines — accumulate until a non-error line arrives,
// then parse) and P1-C Anthropic typed error events (a data line following an
// "event: error" line). Any extracted message is recorded into streamState; it
// returns whether it counted an Anthropic error for this line so the later
// chunk.Error observer does not double-count it. lastAnthropicEvent is the carry
// from the preceding "event:" line and is consumed (reset) here. No client output.
func (st *streamState) captureSSEError(payload string, lastAnthropicEvent *string, chunkCount int, logData *requestLogData) bool {
	// P1-B: accumulate error JSON split across data lines; flush on a non-error line.
	if strings.HasPrefix(payload, `{"error"`) {
		st.errAccum = append(st.errAccum, []byte(payload)...)
	} else {
		st.flushAccumulatedError(chunkCount, logData)
	}

	// P1-C: a data line after "event: error" is an Anthropic error payload,
	// wrapped as {"type":"error","error":{...}} even when it doesn't start with
	// {"error". Extract the message regardless.
	anthropicErrorCounted := false
	if *lastAnthropicEvent == "error" {
		*lastAnthropicEvent = ""
		var anthErr struct {
			Type  string `json:"type"`
			Error *struct {
				Type    string `json:"type"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &anthErr) == nil && anthErr.Error != nil {
			st.lastErrMsg = anthErr.Error.Message
			anthropicErrorCounted = true
			st.errorChunkCount++
			debuglog.Warn("proxy: Anthropic SSE error event", "error_type", anthErr.Error.Type, "error_message", anthErr.Error.Message, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
		}
	}
	return anthropicErrorCounted
}

// flushAccumulatedError parses and records any P1-B accumulated split-error bytes
// (an {"error":…} object split across SSE data lines), then clears the buffer. A
// no-op when nothing is accumulated. Shared by the comment-line handler and
// captureSSEError's non-error data-line branch so the two flush sites co-evolve.
func (st *streamState) flushAccumulatedError(chunkCount int, logData *requestLogData) {
	if len(st.errAccum) == 0 {
		return
	}
	if accumulatedMsg := parseAccumulatedError(st.errAccum); accumulatedMsg != "" {
		st.lastErrMsg = accumulatedMsg
		st.errorChunkCount++
		debuglog.Warn("proxy: accumulated SSE error", "error_message", accumulatedMsg, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
	}
	st.errAccum = nil
}

// repeatedContentLimit is the consecutive-identical-content threshold (P2-5) at
// which we log a warning. Lifted to package scope from handleStreamingResponse
// in Phase 4 so observeDataChunk can reference it.
const repeatedContentLimit = 10

// streamChunk is the typed view of a streaming "data:" JSON chunk that the
// transforms and observers inspect (Phase 4). Only the fields the proxy acts on
// are modelled; everything else is ignored on unmarshal.
type streamChunk struct {
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

// observeDataChunk applies the four non-emitting, side-channel observers over a
// parsed data chunk, updating streamState in place (Phase 4 — the first pipeline
// stage extracted from handleStreamingResponse). It never writes to the client
// and never affects the emit decision; it only records metrics and detection
// state. anthropicErrorCounted reports whether the P1-C Anthropic path already
// counted an error for this line (so chunk.Error doesn't double-count it).
//
// Observers, in order:
//   - usage/token extraction (last usage chunk wins; cache hit/miss only when set)
//   - P2-7 native_finish_reason logging
//   - P2-5 repeated-content detection (and the first-thinking log)
//   - chunk.Error capture (clears errAccum so P1-B won't re-count)
func (st *streamState) observeDataChunk(chunk streamChunk, anthropicErrorCounted bool, chunkCount int, logData *requestLogData) {
	if chunk.Usage != nil {
		st.promptTokens = chunk.Usage.PromptTokens
		st.completionTokens = chunk.Usage.CompletionTokens
		if chunk.Usage.CompletionTokensDetails != nil && chunk.Usage.CompletionTokensDetails.ReasoningTokens > 0 {
			st.reasoningTokens = chunk.Usage.CompletionTokensDetails.ReasoningTokens
		}
		if hit, miss := extractCacheTokens(*chunk.Usage); hit > 0 || miss > 0 {
			st.promptCacheHitTokens = hit
			st.promptCacheMissTokens = miss
		}
	}
	// P2-7: Log native_finish_reason from OpenRouter for debugging.
	// OpenRouter includes this field alongside the normalized finish_reason,
	// preserving the original provider's value (e.g. "STOP" instead of "stop").
	if len(chunk.Choices) > 0 && chunk.Choices[0].NativeFinishReason != nil {
		if *chunk.Choices[0].NativeFinishReason != st.lastNativeFinishReason {
			st.lastNativeFinishReason = *chunk.Choices[0].NativeFinishReason
			debuglog.Debug("proxy: native_finish_reason", "native_finish_reason", st.lastNativeFinishReason, "model", logData.modelID, "provider", logData.providerName)
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
			if !st.sawThinking {
				st.sawThinking = true
				debuglog.Debug("proxy: thinking/reasoning block started", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
			}
		}
		if currentContent == st.lastContent && currentContent != "" {
			st.repeatedCount++
			if st.repeatedCount == repeatedContentLimit {
				debuglog.Warn("proxy: repeated content detected in stream", "repeated_count", st.repeatedCount, "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
			}
		} else {
			st.repeatedCount = 0
		}
		st.lastContent = currentContent
	}
	if chunk.Error != nil && !anthropicErrorCounted {
		// Only count if P1-C didn't already handle this as an
		// Anthropic error event (which shares the same data line).
		st.lastErrMsg = chunk.Error.Message
		st.errorChunkCount++
		debuglog.Warn("proxy: SSE error chunk", "model", logData.modelID, "provider", logData.providerName, "error_message", chunk.Error.Message, "chunk_number", chunkCount)
		// Clear st.errAccum: chunk.Error already captured this error,
		// so P1-B's next flush must not re-count it.
		st.errAccum = nil
	}
}
