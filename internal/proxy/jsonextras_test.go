package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// The regression this file exists for: Gemini 3 issues each tool call with an
// extra_content.google.thought_signature and rejects the follow-up turn when it
// is missing. The non-streaming path decodes the upstream body into
// ChatCompletionResponse and re-encodes it, which silently dropped the field
// and made every non-streaming tool round trip fail with an upstream 400.
func TestToolCallJSON_PreservesGeminiThoughtSignature(t *testing.T) {
	t.Parallel()

	const upstream = `{
		"id": "call_467280",
		"type": "function",
		"function": {"name": "get_temperature", "arguments": "{\"city\":\"Prague\"}"},
		"extra_content": {"google": {"thought_signature": "EnEKbwERTTIPTv3oeLaK"}}
	}`

	var call ToolCall
	if err := json.Unmarshal([]byte(upstream), &call); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if call.ID != "call_467280" || call.Function.Name != "get_temperature" {
		t.Fatalf("modelled fields lost: %+v", call)
	}

	out, err := json.Marshal(call)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	google, ok := got["extra_content"].(map[string]any)["google"].(map[string]any)
	if !ok {
		t.Fatalf("extra_content.google missing from re-encoded call: %s", out)
	}
	if google["thought_signature"] != "EnEKbwERTTIPTv3oeLaK" {
		t.Fatalf("thought_signature not round-tripped: %s", out)
	}
}

func TestMessageJSON_PreservesUnmodelledFields(t *testing.T) {
	t.Parallel()

	const upstream = `{
		"role": "assistant",
		"content": "hi",
		"refusal": null,
		"audio": {"id": "audio_123", "data": "UklGRg=="},
		"annotations": [{"type": "url_citation"}]
	}`

	var msg Message
	if err := json.Unmarshal([]byte(upstream), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Role != "assistant" || msg.Content != "hi" {
		t.Fatalf("modelled fields lost: %+v", msg)
	}

	out, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"audio"`, `"audio_123"`, `"annotations"`, `"refusal"`} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("re-encoded message dropped %s: %s", want, out)
		}
	}
}

// A field this package models and then normalises must win over the raw copy:
// re-adding the original would undo the normalisation.
func TestMessageJSON_ModelledFieldsWinOverExtras(t *testing.T) {
	t.Parallel()

	var msg Message
	if err := json.Unmarshal([]byte(`{"role":"assistant","reasoning":"raw thinking"}`), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// What handleNonStreamingResponse does before re-encoding.
	msg.ReasoningContent = msg.Reasoning

	out, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if _, dup := got["reasoning"]; !dup {
		t.Fatalf("modelled reasoning field missing: %s", out)
	}
	if string(got["reasoning_content"]) != `"raw thinking"` {
		t.Fatalf("normalisation lost: %s", out)
	}
}

// The whole response must survive the decode/re-encode, since that is the path
// the proxy actually takes.
func TestChatCompletionResponse_RoundTripKeepsToolCallExtras(t *testing.T) {
	t.Parallel()

	const upstream = `{
		"id": "resp_1", "object": "chat.completion", "created": 1, "model": "gemini-3.1-flash-lite",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": null, "tool_calls": [
			{"id": "call_1", "type": "function",
			 "function": {"name": "get_temperature", "arguments": "{}"},
			 "extra_content": {"google": {"thought_signature": "sig"}}}
		]}, "finish_reason": "tool_calls"}],
		"usage": {"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3}
	}`

	var resp ChatCompletionResponse
	if err := json.Unmarshal([]byte(upstream), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "thought_signature") {
		t.Fatalf("thought_signature lost through the response round trip: %s", out)
	}
}

// A message with nothing unmodelled must encode exactly as it did before the
// overflow existed - no empty extras object, no reordering surprises.
func TestMessageJSON_NoExtrasIsUnchanged(t *testing.T) {
	t.Parallel()

	var msg Message
	if err := json.Unmarshal([]byte(`{"role":"user","content":"hello"}`), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Extra != nil {
		t.Fatalf("expected no extras, got %v", msg.Extra)
	}
	out, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != `{"role":"user","content":"hello"}` {
		t.Fatalf("plain message re-encoded differently: %s", out)
	}
}

// A null delta is normal on the final chunk of a stream; it must not error.
func TestMessageJSON_NullDecodesClean(t *testing.T) {
	t.Parallel()

	var msg Message
	if err := json.Unmarshal([]byte(`null`), &msg); err != nil {
		t.Fatalf("unmarshal null: %v", err)
	}
	if msg.Extra != nil {
		t.Fatalf("null yielded extras: %v", msg.Extra)
	}
}

// jsonFieldNames is reflection-derived so a new field cannot desynchronise the
// overflow from the schema; pin the two shapes it guards.
func TestJSONFieldNames_CoversTaggedFieldsAndSkipsIgnored(t *testing.T) {
	t.Parallel()

	names := jsonFieldNames(Message{})
	for _, want := range []string{"role", "content", "tool_calls", "reasoning_content"} {
		if _, ok := names[want]; !ok {
			t.Fatalf("expected %q among Message JSON fields, got %v", want, names)
		}
	}
	if _, ok := names["Extra"]; ok {
		t.Fatalf("json:\"-\" field leaked into the known set: %v", names)
	}
	if _, ok := jsonFieldNames(ToolCall{})["function"]; !ok {
		t.Fatal("expected ToolCall to model a function field")
	}
}

// The overflow has to cover every level the non-streaming path decodes and
// re-encodes, not just the message: a client that asked for logprobs was
// handed a choice without them, and the aggregator fields operators route and
// bill on (system_fingerprint, provider, usage.cost) were dropped on the floor.
func TestChatCompletionResponse_PreservesUnmodelledFieldsAtEveryLevel(t *testing.T) {
	t.Parallel()

	const upstream = `{
		"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "llama-3.3-70b",
		"system_fingerprint": "fp_abc123", "provider": "Together", "service_tier": "default",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "hi"},
			"finish_reason": "stop",
			"native_finish_reason": "STOP",
			"logprobs": {"content": [{"token": "hi", "logprob": -0.25}]}
		}],
		"usage": {"prompt_tokens": 1, "completion_tokens": 2, "total_tokens": 3, "cost": 0.000123, "is_byok": false}
	}`

	var resp ChatCompletionResponse
	if err := json.Unmarshal([]byte(upstream), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.ID != "chatcmpl-1" || resp.Usage.TotalTokens != 3 || len(resp.Choices) != 1 {
		t.Fatalf("modelled fields lost: %+v", resp)
	}

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	for k, want := range map[string]any{"system_fingerprint": "fp_abc123", "provider": "Together", "service_tier": "default"} {
		if got[k] != want {
			t.Errorf("top-level %q = %v, want %v: %s", k, got[k], want, out)
		}
	}
	choice, ok := got["choices"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("choice lost: %s", out)
	}
	if _, has := choice["logprobs"]; !has {
		t.Errorf("choice logprobs dropped: %s", out)
	}
	if choice["native_finish_reason"] != "STOP" {
		t.Errorf("choice native_finish_reason dropped: %s", out)
	}
	usage, ok := got["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage lost: %s", out)
	}
	if usage["cost"] != 0.000123 {
		t.Errorf("usage cost dropped: %s", out)
	}
	if _, has := usage["is_byok"]; !has {
		t.Errorf("usage is_byok dropped: %s", out)
	}
	if usage["total_tokens"] != float64(3) {
		t.Errorf("modelled usage field lost: %s", out)
	}
}

// The invariant the overflow must never break: a field this package models wins
// over the raw copy at every level, so nothing it rewrote is undone.
func TestChatCompletionResponse_ModelledFieldsWinOverExtras(t *testing.T) {
	t.Parallel()

	var resp ChatCompletionResponse
	if err := json.Unmarshal([]byte(`{"id":"a","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"x"}}],"usage":{"total_tokens":7}}`), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Stand-ins for what handleNonStreamingResponse rewrites before re-encoding.
	resp.Model = "hotel/rewritten"
	resp.Usage.TotalTokens = 9
	resp.Choices[0].Index = 1

	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if got["model"] != "hotel/rewritten" {
		t.Errorf("modelled model overwritten by extras: %s", out)
	}
	if got["usage"].(map[string]any)["total_tokens"] != float64(9) {
		t.Errorf("modelled usage overwritten by extras: %s", out)
	}
	if got["choices"].([]any)[0].(map[string]any)["index"] != float64(1) {
		t.Errorf("modelled choice index overwritten by extras: %s", out)
	}
}

// Nothing unmodelled must mean byte-identical output, so an ordinary
// completion is not reshaped just because the overflow exists.
func TestChatCompletionResponse_NoExtrasIsUnchanged(t *testing.T) {
	t.Parallel()

	const plain = `{"id":"a","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	var resp ChatCompletionResponse
	if err := json.Unmarshal([]byte(plain), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Extra != nil || resp.Usage.Extra != nil || resp.Choices[0].Extra != nil {
		t.Fatalf("plain completion produced extras: %+v", resp)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != plain {
		t.Fatalf("plain completion re-encoded differently:\n got %s\nwant %s", out, plain)
	}
}

// The custom marshalers alias their own type; a missed alias would recurse
// until the stack died rather than fail a comparison, so pin it.
func TestExtrasMarshalersDoNotRecurse(t *testing.T) {
	t.Parallel()

	resp := ChatCompletionResponse{
		Model:   "m",
		Choices: []Choice{{Message: Message{Role: "assistant", Content: "hi"}, Extra: jsonExtras{"logprobs": json.RawMessage(`null`)}}},
		Usage:   Usage{TotalTokens: 3, Extra: jsonExtras{"cost": json.RawMessage(`0.5`)}},
		Extra:   jsonExtras{"provider": json.RawMessage(`"Together"`)},
	}
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"cost":0.5`) || !strings.Contains(string(out), `"provider":"Together"`) {
		t.Fatalf("extras missing from re-encode: %s", out)
	}
}
