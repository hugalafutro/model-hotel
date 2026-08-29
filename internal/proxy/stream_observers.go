package proxy

import (
	"encoding/json"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// captureSSEError handles the two error-extraction quirks over a data line:
// P1-B truncated-error salvage (a data line the provider cut short, held until
// a line arrives that is not one, then parsed) and P1-C Anthropic typed error
// events (a data line following an "event: error" line).
//
// P1-B is named for split errors, and its comments long claimed to reassemble
// one, but it cannot: a CONTINUATION line does not start with {"error", so it
// takes the flush branch rather than the append. What the buffer actually holds
// is the last {"error"-prefixed line that would not parse — a truncated frame,
// which is the case worth salvaging and the only one it ever sees. Any extracted message is recorded into streamState; it
// returns whether it counted an Anthropic error for this line so the later
// chunk.Error observer does not double-count it. lastAnthropicEvent is the carry
// from the preceding "event:" line and is consumed (reset) here. No client output.
func (st *streamState) captureSSEError(payload string, lastAnthropicEvent *string, chunkCount int, logData *requestLogData) bool {
	// P1-B: hold a truncated error line; flush on any other line.
	//
	// Only a FRAGMENT is held. A payload that parses is a whole frame,
	// whatever it starts with, and the observer reads its error member properly;
	// handing it to the accumulator instead put it in front of
	// parseAccumulatedError, whose last resort is to return the entire payload
	// as the error message. A frame like
	// {"error":"","choices":[{"delta":{"content":…}}]} — an empty error member
	// riding alongside a real delta — therefore wrote the model's output into
	// request_logs.error_message, and marked the request failed for an error no
	// reader of that member agrees is there.
	if strings.HasPrefix(payload, `{"error"`) && !json.Valid([]byte(payload)) {
		st.errAccum = append(st.errAccum, []byte(payload)...)
	} else {
		st.flushAccumulatedError("proxy: accumulated SSE error", chunkCount, logData)
	}

	// P1-C: a data line after "event: error" is an Anthropic error payload,
	// wrapped as {"type":"error","error":{...}} even when it doesn't start with
	// {"error". Extract the message regardless.
	//
	// The member is read with the rule the rest of the gateway shares, not with a
	// private object type one struct away from it. Typed as an object, any other
	// shape failed the whole decode and this branch fell silent — the observer
	// below still caught the member, but the event's own type went unlogged.
	//
	// Emptiness matters more here than anywhere else it is read. errorChunkCount
	// is what tells writeTerminalError the client has already seen the provider's
	// error, and counting a member that carries nothing spent that on nothing: it
	// left lastErrMsg blank, so no error was derived from it, and it suppressed
	// the terminal frame for whatever really did end the stream afterwards. The
	// caller got a cut connection in place of a reason.
	anthropicErrorCounted := false
	if *lastAnthropicEvent == "error" {
		*lastAnthropicEvent = ""
		// Only the member: the wrapper's own "type":"error" is what the
		// preceding event line already said.
		var anthErr struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &anthErr) == nil && util.ErrorMemberCarries(anthErr.Error) {
			msg := util.ErrorMemberMessage(anthErr.Error)
			st.lastErrMsg = msg
			anthropicErrorCounted = true
			st.errorChunkCount++
			// The error's own type, when the member is the object that has one.
			// Provider text like any other, so it goes through the same masking
			// and bounding as the message beside it.
			var typed struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(anthErr.Error, &typed)
			debuglog.Warn("proxy: Anthropic SSE error event", "error_type", st.errLogAttr(typed.Type), "error_message", st.errLogAttr(msg), "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
		}
	}
	return anthropicErrorCounted
}

// flushAccumulatedError parses and records any P1-B held error bytes (a
// truncated {"error":…} line), then clears the buffer. A
// no-op when nothing is accumulated. Every flush site goes through here — the
// comment-line handler, captureSSEError's non-error data-line branch, and the
// stream-end sweep — so they cannot drift; `what` is the only thing that differs
// between them, and a copy of this body is how the stream-end sweep came to log
// the provider's error text unmasked while the other two did not.
func (st *streamState) flushAccumulatedError(what string, chunkCount int, logData *requestLogData) {
	if len(st.errAccum) == 0 {
		return
	}
	if accumulatedMsg := parseAccumulatedError(st.errAccum); accumulatedMsg != "" {
		st.lastErrMsg = accumulatedMsg
		st.errorChunkCount++
		debuglog.Warn(what, "error_message", st.errLogAttr(accumulatedMsg), "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
	}
	st.errAccum = nil
}

// errLogAttr prepares provider error text for an application-log attribute:
// masked, then bounded.
//
// The request log gets both of those at finalize, but the app log had neither.
// It is a different audience and a different store — the live log viewer, the
// app-logs API, the OTLP export — and provider error text is exactly where a
// relay quotes a credential ("invalid key sk-…") or runs to whatever length it
// likes. The observers also run BEFORE the stream's masking block, so the text
// they hold has been scrubbed by nothing at all.
//
// 500 bytes — SanitizeLogBody's limit is a byte count, so CJK error text from
// MiniMax or Z.ai is cut at roughly a third of that in characters. It matches
// the probe's own error sanitizer rather than the request log's 10000: a log
// attribute is for recognising the failure, and the full text is already on the
// row. SanitizeLogBody also redacts UUIDs, so a provider echoing a request id
// back inside its error does not put one in the app log.
//
// A zero-value masker (a keyless local provider) masks by shape only, which is
// what every other site on those paths does too.
func (st *streamState) errLogAttr(msg string) string {
	return util.SanitizeLogBody(string(st.masker.mask([]byte(msg))), 500)
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
			// The two other spellings of the same thing. Ollama and OpenRouter
			// send "reasoning"; OpenRouter and MiniMax send "reasoning_details".
			// normalizeReasoningChunk rewrites both into reasoning_content for
			// the client, but it runs AFTER the observers — so without these the
			// caller receives a full answer while the delivery accounting sees
			// nothing, and the provider is charged for an empty response it did
			// not give.
			Reasoning        *string         `json:"reasoning"`
			ReasoningDetails json.RawMessage `json:"reasoning_details"`
			ToolCalls        []struct {
				Function *struct {
					Name string `json:"name"`
					// util.ToolArguments, not string: several providers send the
					// argument OBJECT where the spec says a JSON string, and a
					// plain string field failed the WHOLE chunk unmarshal, so
					// the frame was dropped as though the bytes were corrupt.
					Arguments util.ToolArguments `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason       *string `json:"finish_reason"`
		NativeFinishReason *string `json:"native_finish_reason"` // P2-7: OpenRouter passthrough
	} `json:"choices"`
	Usage *Usage `json:"usage"`
	// json.RawMessage, not a typed object: providers put an error of any shape
	// here — Ollama's bare string, a list, a number — and a typed field failed
	// the WHOLE chunk unmarshal, so the frame was dropped as corrupt bytes
	// instead of forwarded, and the error it reported was recorded only when
	// the payload happened to START with {"error" (the P1-B prefix). What the
	// member means is util.ErrorMemberCarries/ErrorMemberMessage's answer, shared
	// with the probe so the two readings cannot drift.
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
		if choice.Delta.Content != nil {
			st.deliveredBytes += len(*choice.Delta.Content)
		}
		if choice.Delta.ReasoningContent != nil {
			st.deliveredBytes += len(*choice.Delta.ReasoningContent)
		}
		if choice.Delta.Reasoning != nil {
			st.deliveredBytes += len(*choice.Delta.Reasoning)
		}
		if rd := choice.Delta.ReasoningDetails; len(rd) > 0 && string(rd) != "null" {
			st.deliveredBytes += len(rd)
		}
		for _, tc := range choice.Delta.ToolCalls {
			if tc.Function != nil {
				st.deliveredBytes += len(tc.Function.Name) + len(tc.Function.Arguments)
			}
		}
	}
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
	if !anthropicErrorCounted && util.ErrorMemberCarries(chunk.Error) {
		// Only count if P1-C didn't already handle this as an
		// Anthropic error event (which shares the same data line).
		msg := util.ErrorMemberMessage(chunk.Error)
		st.lastErrMsg = msg
		st.errorChunkCount++
		debuglog.Warn("proxy: SSE error chunk", "model", logData.modelID, "provider", logData.providerName, "error_message", st.errLogAttr(msg), "chunk_number", chunkCount)
		// Clear st.errAccum: chunk.Error already captured this error,
		// so P1-B's next flush must not re-count it.
		st.errAccum = nil
	}
}
