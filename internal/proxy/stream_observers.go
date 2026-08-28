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
			// The output-bearing fields are RawMessage, not string, because a
			// type mismatch in ANY of them fails the whole-chunk unmarshal and
			// the frame is dropped — the provider's answer discarded as though
			// the bytes were corrupt. Real providers send content as an array of
			// parts and tool-call arguments as an object; on a tool call the
			// caller lost the call while finish_reason, which rides a separate
			// parseable frame, survived to announce it.
			//
			// Read them through deltaText/argumentsText, never directly.
			Content          json.RawMessage `json:"content"`
			ReasoningContent json.RawMessage `json:"reasoning_content"`
			// The two other spellings of the same thing. Ollama and OpenRouter
			// send "reasoning"; OpenRouter and MiniMax send "reasoning_details".
			// normalizeReasoningChunk rewrites both into reasoning_content for
			// the client, but it runs AFTER the observers — so without these the
			// caller receives a full answer while the delivery accounting sees
			// nothing, and the provider is charged for an empty response it did
			// not give.
			Reasoning        json.RawMessage `json:"reasoning"`
			ReasoningDetails json.RawMessage `json:"reasoning_details"`
			ToolCalls        []struct {
				Function *struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason       *string `json:"finish_reason"`
		NativeFinishReason *string `json:"native_finish_reason"` // P2-7: OpenRouter passthrough
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	// RawMessage for the same reason: Ollama answers with a bare
	// {"error":"model not found"}, which fails an unmarshal into an object and
	// took the whole frame — content included — down with it. Read through
	// chunkErrorMessage.
	Error json.RawMessage `json:"error"`
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
	// Every choice is delivered output, so the byte count covers all of them
	// (an n>1 stream is billed for every choice), unlike the observers below,
	// which only watch choices[0].
	for _, choice := range chunk.Choices {
		if choice.Delta == nil {
			continue
		}
		for _, raw := range []json.RawMessage{choice.Delta.Content, choice.Delta.ReasoningContent, choice.Delta.Reasoning} {
			text, readable := deltaText(raw)
			if readable {
				st.deliveredBytes += len(text)
				continue
			}
			// The frame parsed but this member is in a shape we cannot read.
			// The caller still RECEIVED it, so the provider delivered: say so,
			// and size it approximately. Treating this as "delivered nothing"
			// charged the circuit breaker for a provider answering correctly,
			// left the retirement verdict reading the model as mute, and
			// metered a served request at zero.
			st.sawContent = true
			st.deliveredBytes += approxOutputBytes(raw)
		}
		if rd := choice.Delta.ReasoningDetails; len(rd) > 0 && string(rd) != "null" {
			st.deliveredBytes += len(rd)
		}
		for _, tc := range choice.Delta.ToolCalls {
			if tc.Function != nil {
				st.deliveredBytes += len(tc.Function.Name) + len(argumentsText(tc.Function.Arguments))
			}
		}
	}
	if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
		delta := chunk.Choices[0].Delta
		currentContent, _ := deltaText(delta.Content)
		if rc, _ := deltaText(delta.ReasoningContent); rc != "" && currentContent == "" {
			currentContent = rc
			if !st.sawThinking {
				st.sawThinking = true
				debuglog.Debug("proxy: thinking/reasoning block started", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
			}
		}
		if currentContent != "" {
			// The model produced something. Recorded here, at the only place
			// that sees content itself, because the retirement verdict needs to
			// know a stream really answered and cannot learn it from usage
			// (providers omit the usage chunk) or from TTFT (the probe can be
			// switched off).
			st.sawContent = true
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
	if errMsg, isErr := chunkErrorMessage(chunk.Error); isErr && !anthropicErrorCounted {
		// Only count if P1-C didn't already handle this as an
		// Anthropic error event (which shares the same data line).
		st.lastErrMsg = errMsg
		st.errorChunkCount++
		debuglog.Warn("proxy: SSE error chunk", "model", logData.modelID, "provider", logData.providerName, "error_message", errMsg, "chunk_number", chunkCount)
		// Clear st.errAccum: chunk.Error already captured this error,
		// so P1-B's next flush must not re-count it.
		st.errAccum = nil
	}
}

// deltaText extracts the model's text from a delta field that carries output.
//
// Providers spell the same thing several ways. The plain string is the OpenAI
// spec; an array of content parts is what several send, and a part carries its
// text under "text". Anything else yields "", because the caller is sizing what
// the model produced and cannot guess at a shape it does not recognise.
//
// This exists so the fields can be json.RawMessage. Typed as *string they made a
// wider-but-valid shape fail the whole-chunk unmarshal, which dropped the frame
// and lost the answer.
func deltaText(raw json.RawMessage) (text string, readable bool) {
	if len(raw) == 0 || string(raw) == "null" {
		// Absent. Nothing to read, and nothing unreadable about that.
		return "", true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var parts []map[string]json.RawMessage
	if json.Unmarshal(raw, &parts) != nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range parts {
		// EVERY element must be understood, not merely one of them. Accepting
		// an array because a single member carried "text" let a mixed array —
		// {"type":"thinking",...} beside {"type":"text",...}, which is exactly
		// how Anthropic-style content blocks arrive — count as read, and the
		// reasoning-strip transform then forwarded the whole delta verbatim to
		// a caller whose key forbids reasoning.
		textRaw, ok := part["text"]
		if !ok {
			return "", false
		}
		var text string
		if json.Unmarshal(textRaw, &text) != nil {
			return "", false
		}
		b.WriteString(text)
	}
	return b.String(), true
}

// approxOutputBytes is a bounded stand-in for the size of an output member
// deltaText could not read: the total length of the strings inside it.
//
// The caller received those bytes, so metering the request at zero would let
// the provider's bill exceed the meter — the drift deliveredBytes exists to
// prevent. Summing the string leaves overshoots slightly (it counts the
// provider's own key vocabulary) and that is the safe direction here, where the
// alternative is charging nothing at all for a served request.
func approxOutputBytes(raw json.RawMessage) int {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return 0
	}
	var walk func(any) int
	walk = func(n any) int {
		switch t := n.(type) {
		case string:
			return len(t)
		case []any:
			sum := 0
			for _, e := range t {
				sum += walk(e)
			}
			return sum
		case map[string]any:
			sum := 0
			for _, e := range t {
				sum += walk(e)
			}
			return sum
		default:
			return 0
		}
	}
	return walk(v)
}

// deltaTextPtr adapts an output field for the transforms, which still take
// *string, and is deliberately EXACTLY what the old *string field was: a plain
// JSON string, or nil.
//
// It must not hand over extracted text. normalizeReasoningChunk patches what it
// is given back into the delta as a plain string, so feeding it the joined text
// of a parts array rewrote the array into that string and destroyed every
// non-text part — an image part vanishing from the caller's answer.
func deltaTextPtr(raw json.RawMessage) *string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}

// argumentsText sizes a tool call's arguments. The spec says a JSON string, and
// some providers send the object itself; for the object its own JSON is the
// honest measure of what the model generated.
func argumentsText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// chunkErrorMessage reports the provider's message when a chunk's error member
// holds one, and ok == false when the member is absent or empty.
//
// Shapes accepted deliberately mirror carriesErrorObject, which this package
// already uses to decide what counts as an error: an object with a message,
// Ollama's bare string, or any other populated value. null/{}/""/[] leave the
// caller nothing to read and are not errors.
func chunkErrorMessage(raw json.RawMessage) (string, bool) {
	if !carriesErrorMember(raw) {
		return "", false
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message, true
	}
	var bare string
	if json.Unmarshal(raw, &bare) == nil {
		return bare, true
	}
	// Deliberately NOT the raw member. This message is persisted to
	// request_logs.error_message, and providers routinely echo the offending
	// input inside content-filter and moderation errors — which would put the
	// caller's prompt in the log, against the rule that no prompt or response
	// content is ever logged. Naming the failure is enough.
	return "provider reported an error with no message", true
}
