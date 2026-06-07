package proxy

import "encoding/json"

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
