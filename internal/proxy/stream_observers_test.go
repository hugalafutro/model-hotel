package proxy

import (
	"encoding/json"
	"strings"
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

// TestCaptureSSEError covers P1-B split-error accumulation+flush and P1-C
// Anthropic typed error events.
func TestCaptureSSEError(t *testing.T) {
	t.Parallel()
	ld := &requestLogData{modelID: "m", providerName: "p"}

	t.Run("P1-B accumulate then flush on non-error line", func(t *testing.T) {
		st := &streamState{}
		ev := ""
		// A FRAGMENT: what P1-B exists for, and the only thing it takes. A
		// whole frame is the observer's to read, however it starts — routing
		// one here instead put it in front of parseAccumulatedError's raw
		// fallback, which records the entire payload as the error message.
		if counted := st.captureSSEError(`{"error":{"message":"bo`, &ev, 1, ld); counted {
			t.Error("error-prefixed line should accumulate, not count as Anthropic")
		}
		if st.lastErrMsg != "" || len(st.errAccum) == 0 {
			t.Errorf("after accumulate: lastErrMsg=%q errAccum=%q (want empty msg, non-empty accum)", st.lastErrMsg, st.errAccum)
		}
		// A non-error line flushes the accumulated error. Nothing can parse a
		// truncated object, so the fragment itself is the best available
		// message — recovering that is the whole point of the accumulator.
		st.captureSSEError(`{"id":"x","choices":[]}`, &ev, 2, ld)
		if !strings.Contains(st.lastErrMsg, "bo") || st.errorChunkCount != 1 {
			t.Errorf("after flush: lastErrMsg=%q errorChunkCount=%d, want the fragment/1", st.lastErrMsg, st.errorChunkCount)
		}
		if st.errAccum != nil {
			t.Errorf("errAccum should be cleared, got %q", st.errAccum)
		}
	})

	// A complete error frame is not a fragment, whatever it starts with: it
	// goes to the observer, and the accumulator must not also hold it.
	t.Run("P1-B ignores a whole frame", func(t *testing.T) {
		st := &streamState{}
		ev := ""
		st.captureSSEError(`{"error":"","choices":[{"delta":{"content":"secret"}}]}`, &ev, 1, ld)
		if len(st.errAccum) != 0 {
			t.Errorf("a parseable frame must not accumulate, got %q", st.errAccum)
		}
		st.captureSSEError(`{"id":"x","choices":[]}`, &ev, 2, ld)
		if st.lastErrMsg != "" || st.errorChunkCount != 0 {
			t.Errorf("nothing to flush, got lastErrMsg=%q count=%d", st.lastErrMsg, st.errorChunkCount)
		}
	})

	t.Run("P1-C Anthropic error event counts and consumes the carry", func(t *testing.T) {
		st := &streamState{}
		ev := "error"
		counted := st.captureSSEError(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, &ev, 1, ld)
		if !counted {
			t.Error("expected anthropicErrorCounted=true")
		}
		if st.lastErrMsg != "Overloaded" || st.errorChunkCount != 1 {
			t.Errorf("got lastErrMsg=%q count=%d, want Overloaded/1", st.lastErrMsg, st.errorChunkCount)
		}
		if ev != "" {
			t.Errorf("lastAnthropicEvent should be consumed (reset to \"\"), got %q", ev)
		}
	})

	t.Run("P1-C carry consumed even when payload isn't an error", func(t *testing.T) {
		st := &streamState{}
		ev := "error"
		if counted := st.captureSSEError(`{"choices":[{"delta":{"content":"hi"}}]}`, &ev, 1, ld); counted {
			t.Error("non-error payload should not count")
		}
		if ev != "" {
			t.Errorf("carry should still be consumed, got %q", ev)
		}
		if st.errorChunkCount != 0 {
			t.Errorf("errorChunkCount = %d, want 0", st.errorChunkCount)
		}
	})
}

// TestObserveDataChunk_RecordsThatContentFlowed pins the signal the retirement
// verdict relies on to decide that a stream actually answered.
//
// It exists because the two signals that used to stand in for it are both
// optional: a provider can omit the usage chunk, and the TTFT probe can be
// switched off. With only those, a provider streaming a perfectly good answer on
// a gateway with the probe disabled reads as having produced nothing, the
// success fails to clear the strike streak, and later refusals retire a model
// whose failures were never consecutive.
func TestObserveDataChunk_RecordsThatContentFlowed(t *testing.T) {
	ld := &requestLogData{}

	t.Run("a content delta counts", func(t *testing.T) {
		st := &streamState{}
		st.observeDataChunk(parseStreamChunk(t, `{"choices":[{"delta":{"content":"Hello"}}]}`), false, 1, ld)
		if !st.sawContent {
			t.Error("a delta carrying text must record that the model answered")
		}
	})

	t.Run("a reasoning delta counts", func(t *testing.T) {
		st := &streamState{}
		st.observeDataChunk(parseStreamChunk(t, `{"choices":[{"delta":{"reasoning_content":"thinking"}}]}`), false, 1, ld)
		if !st.sawContent {
			t.Error("reasoning is still the model producing output")
		}
	})

	// The negatives matter as much: crediting these would clear a retirement
	// streak on the strength of a stream that delivered nothing.
	t.Run("an empty delta does not count", func(t *testing.T) {
		st := &streamState{}
		st.observeDataChunk(parseStreamChunk(t, `{"choices":[{"delta":{"content":""}}]}`), false, 1, ld)
		if st.sawContent {
			t.Error("an empty content delta is not output")
		}
	})

	t.Run("a role-only opening delta does not count", func(t *testing.T) {
		st := &streamState{}
		st.observeDataChunk(parseStreamChunk(t, `{"choices":[{"delta":{"role":"assistant"}}]}`), false, 1, ld)
		if st.sawContent {
			t.Error("opening a stream is not answering it")
		}
	})

	t.Run("an error chunk does not count", func(t *testing.T) {
		st := &streamState{}
		st.observeDataChunk(parseStreamChunk(t, `{"error":{"message":"Model gemini-2.0-flash is no longer available"}}`), false, 1, ld)
		if st.sawContent {
			t.Error("a refusal must never read as the model having answered")
		}
	})
}

// An image-only delta is the whole answer of an image model: it satisfies the
// breaker's delivery bar without counting as content or delivered bytes,
// which the retirement verdict and the usage estimate read.
func TestObserveDataChunk_ImageDeltaIsDeliveryForTheBreakerOnly(t *testing.T) {
	t.Parallel()
	st := &streamState{}
	ld := &requestLogData{modelID: "m", providerName: "p"}
	st.observeDataChunk(parseStreamChunk(t, `{"choices":[{"delta":{"role":"assistant","content":"","images":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}}]}`), false, 1, ld)
	if !st.sawImage || st.sawContent || st.deliveredBytes != 0 {
		t.Fatalf("sawImage=%v sawContent=%v deliveredBytes=%d, want true/false/0", st.sawImage, st.sawContent, st.deliveredBytes)
	}
	if !streamDeliveredOutput(st) {
		t.Fatal("an image-only stream read as undelivered")
	}
	empty := &streamState{}
	empty.observeDataChunk(parseStreamChunk(t, `{"choices":[{"delta":{"role":"assistant","content":"","images":[]}}]}`), false, 1, ld)
	if empty.sawImage || streamDeliveredOutput(empty) {
		t.Fatal("an empty images list counted as delivery")
	}
}
