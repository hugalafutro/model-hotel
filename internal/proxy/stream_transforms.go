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
