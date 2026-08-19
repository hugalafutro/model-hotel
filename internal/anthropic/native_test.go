package anthropic

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestRewriteModel(t *testing.T) {
	body := []byte(`{"model":"hotel/claude","max_tokens":10,"system":"hi","messages":[{"role":"user","content":"x"}]}`)
	out := RewriteModel(body, "claude-haiku-4-5-20251001")
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	if m["model"] != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %v, want rewritten", m["model"])
	}
	// Everything else preserved.
	if m["max_tokens"].(float64) != 10 || m["system"] != "hi" {
		t.Errorf("other fields altered: %v", m)
	}
	if msgs := m["messages"].([]any); len(msgs) != 1 {
		t.Errorf("messages altered: %v", m["messages"])
	}
}

func TestRewriteModel_InvalidBodyUnchanged(t *testing.T) {
	body := []byte(`not json`)
	if out := RewriteModel(body, "x"); string(out) != "not json" {
		t.Errorf("invalid body should be returned unchanged, got %q", out)
	}
}

func TestParseResponseUsage(t *testing.T) {
	body := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":42,"output_tokens":7}}`)
	u := ParseResponseUsage(body)
	if u.PromptTokens != 42 || u.CompletionTokens != 7 {
		t.Errorf("usage = %+v, want prompt=42 completion=7", u)
	}
	// No cache fields: no cache information at all. Calling the whole prompt a
	// miss is a claim the translated egress path cannot make, and would light up
	// the dashboard's cache panel for every uncached Anthropic request.
	if u.CacheHitTokens != 0 || u.CacheMissTokens != 0 {
		t.Errorf("uncached split = (%d,%d), want (0,0)", u.CacheHitTokens, u.CacheMissTokens)
	}
	// Invalid body yields zeros.
	if u := ParseResponseUsage([]byte(`not json`)); u != (ResponseUsage{}) {
		t.Errorf("invalid usage = %+v, want a zero ResponseUsage", u)
	}
}

// Anthropic's cache counts are disjoint additions to input_tokens, not a
// breakdown of it, so a warm-cache request reports a tiny input_tokens beside a
// huge cache_read_input_tokens. Metering the bare field under-reports the
// prompt by the whole cached figure; metering the sum alone then prices every
// cached token at the full input rate. Both halves have to be right.
func TestParseResponseUsage_SplitsCachedInput(t *testing.T) {
	body := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":4,"output_tokens":50,"cache_creation_input_tokens":30,"cache_read_input_tokens":20000}}`)
	u := ParseResponseUsage(body)
	if u.PromptTokens != 20034 {
		t.Errorf("prompt tokens = %d, want 20034 (4 + 20000 + 30)", u.PromptTokens)
	}
	if u.CompletionTokens != 50 {
		t.Errorf("completion tokens = %d, want 50", u.CompletionTokens)
	}
	// Only the cache READ is a hit. Cache creation is processed on this
	// request and surcharged, so it belongs on the miss side.
	if u.CacheHitTokens != 20000 {
		t.Errorf("cache hit tokens = %d, want 20000", u.CacheHitTokens)
	}
	if u.CacheMissTokens != 34 {
		t.Errorf("cache miss tokens = %d, want 34 (4 + 30)", u.CacheMissTokens)
	}
	if u.CacheHitTokens+u.CacheMissTokens != u.PromptTokens {
		t.Errorf("split %d+%d does not sum back to the prompt %d", u.CacheHitTokens, u.CacheMissTokens, u.PromptTokens)
	}
}

// Writing a cache entry without reading one reports no cache counts: the
// translated egress path meters this same response off the cache-READ fields
// alone and can only ever record (0,0), and one Anthropic path claiming cache
// data the other cannot is the inconsistency this split exists to remove. The
// creation tokens still count inside the prompt, which is what is billed.
func TestParseResponseUsage_CacheCreationOnly(t *testing.T) {
	body := []byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":4,"output_tokens":7,"cache_creation_input_tokens":30}}`)
	u := ParseResponseUsage(body)
	if u.PromptTokens != 34 {
		t.Errorf("prompt tokens = %d, want 34 (4 + 30)", u.PromptTokens)
	}
	if u.CacheHitTokens != 0 || u.CacheMissTokens != 0 {
		t.Errorf("cache split = (%d,%d), want (0,0)", u.CacheHitTokens, u.CacheMissTokens)
	}
}

// The streaming path reads its usage through the same summary(), so
// message_start reports the creation-only response the same way.
func TestInspectStreamEvent_CacheCreationOnly(t *testing.T) {
	ev := InspectStreamEvent([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":4,"output_tokens":1,"cache_creation_input_tokens":30}}}`))
	if !ev.HasInput || ev.InputTokens != 34 {
		t.Errorf("input tokens = %d (has=%v), want 34 (4 + 30)", ev.InputTokens, ev.HasInput)
	}
	if ev.CacheHitTokens != 0 || ev.CacheMissTokens != 0 {
		t.Errorf("cache split = (%d,%d), want (0,0)", ev.CacheHitTokens, ev.CacheMissTokens)
	}
}

// ResponseCarriesContent decides whether a native 200 counts as the model
// answering, which is what clears its gone-strike streak in the proxy. The
// distinction that matters is a nonempty body carrying an empty content array:
// an aggregator in front of a retired model returns exactly that between its
// refusals, and crediting it would stop the streak ever reaching three
// consecutive strikes.
func TestResponseCarriesContent(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"text block", `{"id":"m","type":"message","content":[{"type":"text","text":"hi"}]}`, true},
		// Any block type is the model producing something; the vocabulary is
		// Anthropic's to extend, so the blocks are deliberately not decoded.
		{"tool_use block", `{"id":"m","type":"message","content":[{"type":"tool_use","id":"toolu_1","name":"f"}]}`, true},
		{"unknown block type still counts", `{"id":"m","type":"message","content":[{"type":"something_new"}]}`, true},
		{"empty content array", `{"id":"m","type":"message","content":[]}`, false},
		{"content absent", `{"id":"m","type":"message","usage":{"output_tokens":3}}`, false},
		{"content null", `{"id":"m","type":"message","content":null}`, false},
		{"not json", `not json`, false},
		{"empty body", ``, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResponseCarriesContent([]byte(tt.body)); got != tt.want {
				t.Errorf("ResponseCarriesContent(%s) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestStreamTranslator_ToolWithoutID_AndIdempotentFinish(t *testing.T) {
	tr := NewStreamTranslator("msg_t", "m")
	// A tool-call fragment with no id forces id synthesis; arguments stream as
	// input_json_delta.
	out, err := tr.Translate(OAStreamChunk{Choices: []OAStreamChoice{{
		Delta: OAStreamDelta{ToolCalls: []OAToolCallDelta{{
			Index: 0, Function: OAFunctionDelta{Name: "f", Arguments: `{"a":1}`},
		}}},
	}}})
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if !bytes.Contains(out, []byte("toolu_")) {
		t.Errorf("expected synthesized toolu_ id in output:\n%s", out)
	}
	if !bytes.Contains(out, []byte("input_json_delta")) {
		t.Errorf("expected input_json_delta:\n%s", out)
	}
	// Finish is idempotent: the second call emits nothing.
	if _, err := tr.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	again, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish (2nd): %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second Finish should emit nothing, got %s", again)
	}
}

func TestInspectStreamEvent(t *testing.T) {
	// message_start carries input tokens.
	if ev := InspectStreamEvent([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":15,"output_tokens":0}}}`)); ev.Type != "message_start" || !ev.HasInput || ev.InputTokens != 15 {
		t.Errorf("message_start = %+v, want type=message_start input=15", ev)
	}
	// message_delta carries cumulative output tokens.
	if ev := InspectStreamEvent([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":23}}`)); ev.Type != "message_delta" || !ev.HasOutput || ev.OutputTokens != 23 {
		t.Errorf("message_delta = %+v, want type=message_delta output=23", ev)
	}
	// content_block_delta carries no usage.
	if ev := InspectStreamEvent([]byte(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`)); ev.HasInput || ev.HasOutput {
		t.Errorf("content_block_delta should report no usage, got %+v", ev)
	}
	// message_stop is the terminal marker.
	if ev := InspectStreamEvent([]byte(`{"type":"message_stop"}`)); ev.Type != "message_stop" {
		t.Errorf("message_stop type = %q, want message_stop", ev.Type)
	}
	// error event surfaces the message.
	if ev := InspectStreamEvent([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)); ev.Type != "error" || ev.ErrorMessage != "Overloaded" {
		t.Errorf("error = %+v, want type=error message=Overloaded", ev)
	}
	// garbage parses to a zero value.
	if ev := InspectStreamEvent([]byte(`not json`)); ev.Type != "" {
		t.Errorf("garbage = %+v, want zero StreamEvent", ev)
	}
}

// The streamed message_start reports the same prompt size AND the same cache
// split the non-streaming path does, so the two native paths meter and price a
// cached prompt alike.
func TestInspectStreamEvent_SplitsCachedInput(t *testing.T) {
	ev := InspectStreamEvent([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":4,"output_tokens":1,"cache_creation_input_tokens":30,"cache_read_input_tokens":20000}}}`))
	if !ev.HasInput || ev.InputTokens != 20034 {
		t.Errorf("input tokens = %d (has=%v), want 20034 (4 + 20000 + 30)", ev.InputTokens, ev.HasInput)
	}
	if ev.CacheHitTokens != 20000 || ev.CacheMissTokens != 34 {
		t.Errorf("cache split = (%d,%d), want (20000,34)", ev.CacheHitTokens, ev.CacheMissTokens)
	}
	if !ev.HasOutput || ev.OutputTokens != 1 {
		t.Errorf("output tokens = %d (has=%v), want 1", ev.OutputTokens, ev.HasOutput)
	}

	// An uncached stream reports no cache counts, so the proxy's
	// (hit>0||miss>0) guard leaves the log row's cache fields untouched — the
	// same thing every other provider's uncached request produces.
	plain := InspectStreamEvent([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":15,"output_tokens":0}}}`))
	if plain.InputTokens != 15 {
		t.Errorf("uncached input tokens = %d, want 15", plain.InputTokens)
	}
	if plain.CacheHitTokens != 0 || plain.CacheMissTokens != 0 {
		t.Errorf("uncached split = (%d,%d), want (0,0)", plain.CacheHitTokens, plain.CacheMissTokens)
	}
}
