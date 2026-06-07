package proxy

import (
	"encoding/json"
	"testing"
)

func parseStreamChunk(t *testing.T, payload string) streamChunk {
	t.Helper()
	var c streamChunk
	if err := json.Unmarshal([]byte(payload), &c); err != nil {
		t.Fatalf("unmarshal %q: %v", payload, err)
	}
	return c
}

// TestObserveDataChunk_Usage verifies the usage observer records the last usage
// chunk's token counts into streamState.
func TestObserveDataChunk_Usage(t *testing.T) {
	t.Parallel()
	st := &streamState{}
	ld := &requestLogData{modelID: "m", providerName: "p"}

	c := parseStreamChunk(t, `{"usage":{"prompt_tokens":3,"completion_tokens":5,"completion_tokens_details":{"reasoning_tokens":2}}}`)
	st.observeDataChunk(c, false, 1, ld)

	if st.promptTokens != 3 || st.completionTokens != 5 || st.reasoningTokens != 2 {
		t.Errorf("tokens = (%d,%d,%d), want (3,5,2)", st.promptTokens, st.completionTokens, st.reasoningTokens)
	}

	// A later usage chunk wins (last-usage semantics).
	st.observeDataChunk(parseStreamChunk(t, `{"usage":{"prompt_tokens":3,"completion_tokens":9}}`), false, 2, ld)
	if st.completionTokens != 9 {
		t.Errorf("completionTokens = %d after second usage, want 9", st.completionTokens)
	}
}

// TestObserveDataChunk_Error verifies chunk.Error capture, the errorChunkCount
// increment, the errAccum clear, and that the P1-C de-dup flag suppresses
// double-counting.
func TestObserveDataChunk_Error(t *testing.T) {
	t.Parallel()
	ld := &requestLogData{modelID: "m", providerName: "p"}

	st := &streamState{errAccum: []byte(`{"error"`)}
	st.observeDataChunk(parseStreamChunk(t, `{"error":{"message":"boom"}}`), false, 1, ld)
	if st.lastErrMsg != "boom" || st.errorChunkCount != 1 {
		t.Errorf("got lastErrMsg=%q errorChunkCount=%d, want boom/1", st.lastErrMsg, st.errorChunkCount)
	}
	if st.errAccum != nil {
		t.Errorf("errAccum should be cleared, got %q", st.errAccum)
	}

	// anthropicErrorCounted=true → P1-C already counted it; do not re-count.
	st2 := &streamState{}
	st2.observeDataChunk(parseStreamChunk(t, `{"error":{"message":"dup"}}`), true, 1, ld)
	if st2.lastErrMsg != "" || st2.errorChunkCount != 0 {
		t.Errorf("anthropicErrorCounted should suppress: got lastErrMsg=%q count=%d", st2.lastErrMsg, st2.errorChunkCount)
	}
}

// TestObserveDataChunk_RepeatedContent verifies the P2-5 consecutive-identical
// content counter increments on repeats and resets on a change.
func TestObserveDataChunk_RepeatedContent(t *testing.T) {
	t.Parallel()
	st := &streamState{}
	ld := &requestLogData{modelID: "m", providerName: "p"}

	same := `{"choices":[{"delta":{"content":"x"}}]}`
	st.observeDataChunk(parseStreamChunk(t, same), false, 1, ld) // first: establishes lastContent, count stays 0
	if st.repeatedCount != 0 || st.lastContent != "x" {
		t.Fatalf("after first: count=%d lastContent=%q, want 0/x", st.repeatedCount, st.lastContent)
	}
	st.observeDataChunk(parseStreamChunk(t, same), false, 2, ld) // repeat: count→1
	if st.repeatedCount != 1 {
		t.Errorf("after repeat: count=%d, want 1", st.repeatedCount)
	}
	st.observeDataChunk(parseStreamChunk(t, `{"choices":[{"delta":{"content":"y"}}]}`), false, 3, ld) // change: reset
	if st.repeatedCount != 0 || st.lastContent != "y" {
		t.Errorf("after change: count=%d lastContent=%q, want 0/y", st.repeatedCount, st.lastContent)
	}
}

// TestObserveDataChunk_NativeFinishReason verifies native_finish_reason is
// recorded and only updated when it changes.
func TestObserveDataChunk_NativeFinishReason(t *testing.T) {
	t.Parallel()
	st := &streamState{}
	ld := &requestLogData{modelID: "m", providerName: "p"}

	st.observeDataChunk(parseStreamChunk(t, `{"choices":[{"native_finish_reason":"STOP"}]}`), false, 1, ld)
	if st.lastNativeFinishReason != "STOP" {
		t.Errorf("lastNativeFinishReason = %q, want STOP", st.lastNativeFinishReason)
	}
}
