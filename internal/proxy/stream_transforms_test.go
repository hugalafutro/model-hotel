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
