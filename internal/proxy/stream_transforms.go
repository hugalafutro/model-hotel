package proxy

import "encoding/json"

// stripReasoningDecision is what computeStripReasoning decided for a chunk.
type stripReasoningDecision int

const (
	stripPassthrough stripReasoningDecision = iota // payload didn't parse; fall through to reasoning-normalize
	stripKeepalive                                 // delta empty after strip; emit the minimal keep-alive
	stripForward                                   // delta still has content; emit the stripped chunk
	stripDrop                                      // keep-alive marshal failed; drop the chunk (practically unreachable)
)

// computeStripReasoning applies the per-virtual-key strip_reasoning transform.
// It removes reasoning* fields (and a redundant role-only field) from the delta,
// then decides: forward the stripped chunk (delta still has content / tool_calls
// / a non-null finish_reason), replace it with a minimal valid keep-alive
// reusing the stream's real id (delta empty — avoids hollow deltas that make
// clients like Warp disconnect), pass it through unchanged (payload didn't
// parse), or drop it (keep-alive marshal failure). On stripKeepalive/stripForward
// the returned JSON is emitted by the caller via sink.writeData (byte-identical
// to the prior hand-built "data: …\n\n"). finish_reason is normalized in place on
// the forward path via lastFinishReason.
func computeStripReasoning(payload string, lastFinishReason *string, logData *requestLogData) (stripReasoningDecision, []byte) {
	p, ok := parseChunkPayload(payload)
	if !ok {
		// parseChunkPayload failed on a chunk that passed the typed-struct guard.
		// Forward unmodified (the later blocks handle it) rather than dropping.
		return stripPassthrough, nil
	}
	deltaFields := p.delta
	delete(deltaFields, "reasoning_content")
	delete(deltaFields, "reasoning_details")
	delete(deltaFields, "reasoning")

	// Strip empty content ("") that normally accompanies reasoning-only deltas.
	if cRaw, okC := deltaFields["content"]; okC {
		var cStr string
		if json.Unmarshal(cRaw, &cStr) == nil && cStr == "" {
			delete(deltaFields, "content")
		}
	}

	// Drop a redundant role-only delta (e.g. Ollama repeats "role":"assistant"
	// on every delta); the role is already present on any content/tool_calls chunk.
	_, hasContent := deltaFields["content"]
	_, hasToolCalls := deltaFields["tool_calls"]
	if !hasContent && !hasToolCalls {
		delete(deltaFields, "role")
	}

	// Does the delta still carry meaningful data?
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
	// finish_reason lives at choices[0], not in the delta; a non-null value must
	// still be forwarded (omitting the stop signal breaks clients).
	if frRaw, okFR := p.choices[0]["finish_reason"]; okFR {
		var frStr string
		if json.Unmarshal(frRaw, &frStr) == nil && frStr != "" {
			deltaHasContent = true
		}
	}

	if !deltaHasContent {
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
			return stripDrop, nil
		}
		return stripKeepalive, keepAliveJSON
	}

	newDelta, _ := json.Marshal(deltaFields)
	p.choices[0]["delta"] = json.RawMessage(newDelta)
	// Normalize finish_reason before re-serializing so non-standard values
	// (e.g. "end_turn", "STOP") map to OpenAI equivalents.
	normalizeFinishReasonInChoices(p.choices, lastFinishReason, logData.modelID, logData.providerName)
	newChoices, _ := json.Marshal(p.choices)
	p.raw["choices"] = json.RawMessage(newChoices)
	newPayload, _ := json.Marshal(p.raw)
	return stripForward, newPayload
}

// finishReasonDecision is what computeFinishReason decided for a chunk.
type finishReasonDecision int

const (
	finishNone     finishReasonDecision = iota // no finish_reason, or already OpenAI-normalized
	finishSuppress                             // P2-2 bare-duplicate finish_reason; drop the chunk
	finishRewrite                              // finish_reason normalized; emit the rewritten payload
)

// computeFinishReason normalizes a provider-specific finish_reason (e.g. Gemini
// "STOP", Anthropic "end_turn") to the OpenAI vocabulary and decides the chunk's
// fate: finishSuppress for a P2-2 bare duplicate (same normalized value as the
// previous chunk, no content, no usage), finishRewrite with the re-serialized
// payload when normalization changed the value, or finishNone otherwise (no
// finish_reason, value unchanged, or re-serialization failed — caller forwards
// the original line). lastFinishReason is updated in place whenever a
// finish_reason is present and not suppressed (matching the prior inline order:
// update happens before, and independent of, the `written` rewrite guard the
// caller still applies). The caller owns emit, the `written` guard, and logging.
func computeFinishReason(chunk streamChunk, payload string, lastFinishReason *string) (finishReasonDecision, []byte) {
	if len(chunk.Choices) == 0 || chunk.Choices[0].FinishReason == nil {
		return finishNone, nil
	}
	original := *chunk.Choices[0].FinishReason
	normalized := normalizeFinishReason(original)

	// P2-2: suppress a bare duplicate (same finish_reason as the previous chunk,
	// no content, no usage) — it causes downstream "empty response text" errors.
	if normalized == *lastFinishReason {
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
			return finishSuppress, nil
		}
	}
	*lastFinishReason = normalized
	if normalized == original {
		return finishNone, nil
	}
	// Rewrite the line with the normalized finish_reason. Any parse/marshal
	// failure → finishNone so the caller forwards the original unchanged.
	var raw map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &raw) != nil {
		return finishNone, nil
	}
	choicesRaw, ok := raw["choices"]
	if !ok {
		return finishNone, nil
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(choicesRaw, &choices) != nil || len(choices) == 0 {
		return finishNone, nil
	}
	if frRaw, ok2 := choices[0]["finish_reason"]; ok2 {
		var newFR string
		if json.Unmarshal(frRaw, &newFR) == nil {
			choices[0]["finish_reason"] = json.RawMessage(`"` + normalized + `"`)
		}
	}
	newChoices, err := json.Marshal(choices)
	if err != nil {
		return finishNone, nil
	}
	raw["choices"] = json.RawMessage(newChoices)
	newPayload, err := json.Marshal(raw)
	if err != nil {
		return finishNone, nil
	}
	return finishRewrite, newPayload
}

// stream_transforms.go holds the emit-bearing SSE transforms' *compute* logic,
// extracted from handleStreamingResponse one at a time (Phase 4). Each function
// is pure except for the finish_reason normalization it shares via the
// lastFinishReason pointer; none writes to the client. The orchestrator still
// owns the emit + written/skipNextEmptyLine bookkeeping, so behavior is
// byte-identical — this isolates and unit-tests the transform without touching
// the separator rule (that collapse is Phase 5).

// stripEmptyReasoningContent rewrites a reasoning chunk that carries an empty
// content:"" alongside reasoning_content: it removes the noise content field and
// normalizes finish_reason in place (via lastFinishReason). It returns the
// re-serialized payload and true on success, or (nil, false) when the payload
// does not parse — in which case the caller forwards the original line unchanged,
// matching the prior inline behavior. Callers guard on
// hasReasoning && hasEmptyContent before calling.
func stripEmptyReasoningContent(payload string, lastFinishReason *string, logData *requestLogData) ([]byte, bool) {
	p, ok := parseChunkPayload(payload)
	if !ok {
		return nil, false
	}
	delete(p.delta, "content")
	newDelta, _ := json.Marshal(p.delta)
	p.choices[0]["delta"] = json.RawMessage(newDelta)
	// Normalize finish_reason in-place before re-serializing; the caller sets
	// written=true after emitting, which would otherwise skip the later
	// finish_reason normalization block for this chunk.
	normalizeFinishReasonInChoices(p.choices, lastFinishReason, logData.modelID, logData.providerName)
	newChoices, _ := json.Marshal(p.choices)
	p.raw["choices"] = json.RawMessage(newChoices)
	newPayload, _ := json.Marshal(p.raw)
	return newPayload, true
}

// normalizeReasoningChunk ensures reasoning_content is populated regardless of
// upstream provider format: delta.reasoning (Ollama), delta.reasoning_details
// (OpenRouter/MiniMax), or <thinking> tags in delta.content. content and
// reasoningContent are the typed delta fields; the raw reasoning/reasoning_details
// are pulled from the parsed payload. It returns the re-serialized payload and
// true only when NormalizeReasoningFields changed something AND the payload
// parsed; otherwise (nil, false), and the caller leaves the chunk for the later
// blocks (matching the prior `if Normalize... { if parsedOk { … } }` nesting).
// finish_reason is normalized in place via lastFinishReason.
func normalizeReasoningChunk(content, reasoningContent *string, payload string, lastFinishReason *string, logData *requestLogData) ([]byte, bool) {
	// Build a map from the typed delta fields for normalization.
	deltaMap := make(map[string]interface{})
	if content != nil {
		deltaMap["content"] = *content
	}
	if reasoningContent != nil {
		deltaMap["reasoning_content"] = *reasoningContent
	}
	// Parse the raw payload once to capture reasoning/reasoning_details which
	// aren't in the typed struct, and reuse the parsed result for re-serialization.
	chunkParsed, chunkParsedOk := parseChunkPayload(payload)
	if chunkParsedOk {
		// Extract reasoning field (Ollama, OpenRouter).
		if rRaw, ok := chunkParsed.delta["reasoning"]; ok {
			var rStr string
			if json.Unmarshal(rRaw, &rStr) == nil && rStr != "" {
				deltaMap["reasoning"] = rStr
			}
		}
		// Extract reasoning_details (OpenRouter, MiniMax).
		if rdRaw, ok := chunkParsed.delta["reasoning_details"]; ok {
			var rdArr []interface{}
			if json.Unmarshal(rdRaw, &rdArr) == nil {
				deltaMap["reasoning_details"] = rdArr
			}
		}
	}
	if !NormalizeReasoningFields(deltaMap) || !chunkParsedOk {
		return nil, false
	}
	// Patch reasoning_content into the delta.
	if rc, ok := deltaMap["reasoning_content"]; ok {
		if rcStr, ok2 := rc.(string); ok2 {
			escaped, _ := json.Marshal(rcStr)
			chunkParsed.delta["reasoning_content"] = json.RawMessage(escaped)
		}
	}
	// Patch content if it was modified (tag extraction).
	if c, ok := deltaMap["content"]; ok {
		if cStr, ok2 := c.(string); ok2 {
			escaped, _ := json.Marshal(cStr)
			chunkParsed.delta["content"] = json.RawMessage(escaped)
		}
	}
	newDelta, _ := json.Marshal(chunkParsed.delta)
	chunkParsed.choices[0]["delta"] = json.RawMessage(newDelta)
	normalizeFinishReasonInChoices(chunkParsed.choices, lastFinishReason, logData.modelID, logData.providerName)
	newChoices, _ := json.Marshal(chunkParsed.choices)
	chunkParsed.raw["choices"] = json.RawMessage(newChoices)
	newPayload, _ := json.Marshal(chunkParsed.raw)
	return newPayload, true
}
