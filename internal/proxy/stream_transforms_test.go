package proxy

import (
	"encoding/json"
	"testing"
)

// TestStripEmptyReasoningContent covers the empty-content-strip transform's
// compute in isolation (Phase 4): the noise content:"" field is removed,
// reasoning_content is preserved, finish_reason is normalized in place, and an
// unparseable payload returns ok=false so the caller forwards it unchanged.
func TestStripEmptyReasoningContent(t *testing.T) {
	t.Parallel()
	ld := &requestLogData{modelID: "m", providerName: "p"}

	// Helper to pull choices[0] delta + finish_reason out of a re-serialized chunk.
	decode := func(b []byte) (delta map[string]json.RawMessage, finish string) {
		var raw struct {
			Choices []struct {
				Delta        map[string]json.RawMessage `json:"delta"`
				FinishReason *string                    `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("re-decode %q: %v", b, err)
		}
		if len(raw.Choices) == 0 {
			t.Fatalf("no choices in %q", b)
		}
		fr := ""
		if raw.Choices[0].FinishReason != nil {
			fr = *raw.Choices[0].FinishReason
		}
		return raw.Choices[0].Delta, fr
	}

	t.Run("strips empty content, keeps reasoning_content", func(t *testing.T) {
		var lastFR string
		in := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"","reasoning_content":"r"},"finish_reason":null}]}`
		out, ok := stripEmptyReasoningContent(in, &lastFR, ld)
		if !ok {
			t.Fatal("expected ok=true")
		}
		delta, _ := decode(out)
		if _, hasContent := delta["content"]; hasContent {
			t.Errorf("content should be removed, delta=%v", delta)
		}
		if _, hasRC := delta["reasoning_content"]; !hasRC {
			t.Errorf("reasoning_content should be preserved, delta=%v", delta)
		}
	})

	t.Run("normalizes finish_reason in place", func(t *testing.T) {
		var lastFR string
		in := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"","reasoning_content":"r"},"finish_reason":"STOP"}]}`
		out, ok := stripEmptyReasoningContent(in, &lastFR, ld)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if _, finish := decode(out); finish != "stop" {
			t.Errorf("finish_reason = %q, want stop", finish)
		}
		if lastFR != "stop" {
			t.Errorf("lastFinishReason = %q, want stop (mutated in place)", lastFR)
		}
	})

	t.Run("unparseable payload returns false", func(t *testing.T) {
		var lastFR string
		out, ok := stripEmptyReasoningContent(`{}`, &lastFR, ld)
		if ok || out != nil {
			t.Errorf("expected (nil,false) for no-choices payload, got (%q,%v)", out, ok)
		}
	})
}

// TestNormalizeReasoningChunk covers the reasoning-normalization transform's
// compute (Phase 4): folding provider-specific reasoning fields into
// reasoning_content, the no-op case, and the unparseable case.
func TestNormalizeReasoningChunk(t *testing.T) {
	t.Parallel()
	ld := &requestLogData{modelID: "m", providerName: "p"}
	strptr := func(s string) *string { return &s }

	deltaOf := func(b []byte) map[string]json.RawMessage {
		var raw struct {
			Choices []struct {
				Delta map[string]json.RawMessage `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(b, &raw); err != nil || len(raw.Choices) == 0 {
			t.Fatalf("re-decode %q: %v", b, err)
		}
		return raw.Choices[0].Delta
	}

	t.Run("folds Ollama reasoning into reasoning_content", func(t *testing.T) {
		var lastFR string
		in := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning":"r"},"finish_reason":null}]}`
		out, ok := normalizeReasoningChunk(nil, nil, in, &lastFR, ld)
		if !ok {
			t.Fatal("expected ok=true")
		}
		delta := deltaOf(out)
		rc, has := delta["reasoning_content"]
		if !has || string(rc) != `"r"` {
			t.Errorf("reasoning_content = %s (has=%v), want \"r\"", rc, has)
		}
	})

	t.Run("plain content is a no-op", func(t *testing.T) {
		var lastFR string
		in := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`
		out, ok := normalizeReasoningChunk(strptr("hi"), nil, in, &lastFR, ld)
		if ok || out != nil {
			t.Errorf("expected (nil,false) for a plain content chunk, got (%q,%v)", out, ok)
		}
	})

	t.Run("unparseable payload returns false", func(t *testing.T) {
		var lastFR string
		out, ok := normalizeReasoningChunk(nil, nil, `not json`, &lastFR, ld)
		if ok || out != nil {
			t.Errorf("expected (nil,false), got (%q,%v)", out, ok)
		}
	})
}

// TestComputeStripReasoning covers the strip_reasoning transform's decisions:
// keep-alive when the delta is empty after stripping, forward when content (or a
// non-null finish_reason) remains, and passthrough when the payload won't parse.
func TestComputeStripReasoning(t *testing.T) {
	t.Parallel()
	ld := &requestLogData{modelID: "m", providerName: "p"}

	t.Run("reasoning-only delta → keep-alive with real id", func(t *testing.T) {
		var lastFR string
		in := `{"id":"cid","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"r","content":""},"finish_reason":null}]}`
		d, out := computeStripReasoning(in, &lastFR, ld)
		if d != stripKeepalive {
			t.Fatalf("decision = %v, want stripKeepalive", d)
		}
		want := `{"choices":[{"delta":{},"index":0}],"id":"cid","object":"chat.completion.chunk"}`
		if string(out) != want {
			t.Errorf("keep-alive payload mismatch\n got: %s\nwant: %s", out, want)
		}
	})

	t.Run("content survives strip → forward", func(t *testing.T) {
		var lastFR string
		in := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"r","content":"hello"},"finish_reason":null}]}`
		d, out := computeStripReasoning(in, &lastFR, ld)
		if d != stripForward {
			t.Fatalf("decision = %v, want stripForward", d)
		}
		var raw struct {
			Choices []struct {
				Delta map[string]json.RawMessage `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(out, &raw); err != nil {
			t.Fatalf("decode forward payload: %v", err)
		}
		delta := raw.Choices[0].Delta
		if _, hasRC := delta["reasoning_content"]; hasRC {
			t.Errorf("reasoning_content should be stripped, delta=%v", delta)
		}
		if string(delta["content"]) != `"hello"` {
			t.Errorf("content = %s, want \"hello\"", delta["content"])
		}
	})

	t.Run("empty delta but finish_reason → forward (keeps stop signal)", func(t *testing.T) {
		var lastFR string
		in := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"r"},"finish_reason":"stop"}]}`
		if d, _ := computeStripReasoning(in, &lastFR, ld); d != stripForward {
			t.Errorf("decision = %v, want stripForward (finish_reason keeps it)", d)
		}
	})

	t.Run("unparseable payload → passthrough", func(t *testing.T) {
		var lastFR string
		if d, out := computeStripReasoning(`{}`, &lastFR, ld); d != stripPassthrough || out != nil {
			t.Errorf("got (%v,%q), want (stripPassthrough,nil)", d, out)
		}
	})
}

// TestComputeFinishReason covers the finish_reason transform's decisions:
// rewrite (provider value → OpenAI), no-op (already normalized / absent), and
// the P2-2 bare-duplicate suppression with its content/usage exceptions.
func TestComputeFinishReason(t *testing.T) {
	t.Parallel()

	t.Run("STOP → rewrite to stop, updates lastFinishReason", func(t *testing.T) {
		lastFR := ""
		c := parseStreamChunk(t, `{"id":"x","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"STOP"}]}`)
		d, out := computeFinishReason(c, `{"id":"x","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"STOP"}]}`, &lastFR)
		if d != finishRewrite {
			t.Fatalf("decision = %v, want finishRewrite", d)
		}
		if lastFR != "stop" {
			t.Errorf("lastFinishReason = %q, want stop", lastFR)
		}
		var raw struct {
			Choices []struct {
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(out, &raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if raw.Choices[0].FinishReason != "stop" {
			t.Errorf("finish_reason = %q, want stop", raw.Choices[0].FinishReason)
		}
	})

	t.Run("already-normalized → none", func(t *testing.T) {
		lastFR := ""
		c := parseStreamChunk(t, `{"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`)
		if d, _ := computeFinishReason(c, `{}`, &lastFR); d != finishNone {
			t.Errorf("decision = %v, want finishNone", d)
		}
		if lastFR != "stop" {
			t.Errorf("lastFinishReason = %q, want stop (still updated)", lastFR)
		}
	})

	t.Run("bare duplicate → suppress", func(t *testing.T) {
		lastFR := "stop"
		c := parseStreamChunk(t, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		if d, _ := computeFinishReason(c, `{}`, &lastFR); d != finishSuppress {
			t.Errorf("decision = %v, want finishSuppress", d)
		}
	})

	t.Run("duplicate WITH content → not suppressed", func(t *testing.T) {
		lastFR := "stop"
		c := parseStreamChunk(t, `{"choices":[{"delta":{"content":"more"},"finish_reason":"stop"}]}`)
		if d, _ := computeFinishReason(c, `{}`, &lastFR); d != finishNone {
			t.Errorf("decision = %v, want finishNone (content present blocks suppression)", d)
		}
	})

	t.Run("no finish_reason → none", func(t *testing.T) {
		lastFR := ""
		c := parseStreamChunk(t, `{"choices":[{"delta":{"content":"hi"},"finish_reason":null}]}`)
		if d, _ := computeFinishReason(c, `{}`, &lastFR); d != finishNone {
			t.Errorf("decision = %v, want finishNone", d)
		}
	})
}
