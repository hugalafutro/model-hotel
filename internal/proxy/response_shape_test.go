package proxy

import (
	"encoding/json"
	"testing"
)

// choiceKeys re-parses one encoded choice so a test can ask which JSON keys the
// client actually receives, rather than substring-matching the whole body.
func choiceKeys(t *testing.T, encoded []byte) map[string]json.RawMessage {
	t.Helper()

	var envelope struct {
		Choices []map[string]json.RawMessage `json:"choices"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(envelope.Choices) != 1 {
		t.Fatalf("expected exactly one choice, got %d: %s", len(envelope.Choices), encoded)
	}
	return envelope.Choices[0]
}

// A non-streaming completion has no delta in the OpenAI schema. The proxy
// decodes the upstream body into ChatCompletionResponse and re-encodes it, so a
// Choice that always serialises a delta invents a key on every response the
// gateway serves.
func TestChatCompletionResponse_NonStreamingHasNoDelta(t *testing.T) {
	t.Parallel()

	const upstream = `{
		"id": "chatcmpl-1", "object": "chat.completion", "created": 1, "model": "gpt-4o-mini",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hi"}, "finish_reason": "stop"}],
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

	keys := choiceKeys(t, out)
	if raw, present := keys["delta"]; present {
		t.Fatalf("non-streaming choice carries a delta key (%s): %s", raw, out)
	}
	if _, present := keys["message"]; !present {
		t.Fatalf("non-streaming choice lost its message: %s", out)
	}
}

// The streaming shape is the mirror image: a chunk's delta must survive the
// round trip even when it carries nothing but the role, which is exactly what
// the first chunk of every stream looks like.
func TestChatCompletionResponse_StreamingChunkKeepsDelta(t *testing.T) {
	t.Parallel()

	const chunk = `{
		"id": "chatcmpl-1", "object": "chat.completion.chunk", "created": 1, "model": "gpt-4o-mini",
		"choices": [{"index": 0, "delta": {"role": "assistant"}}]
	}`

	var resp ChatCompletionResponse
	if err := json.Unmarshal([]byte(chunk), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	keys := choiceKeys(t, out)
	raw, present := keys["delta"]
	if !present {
		t.Fatalf("streaming chunk lost its delta: %s", out)
	}
	var delta struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(raw, &delta); err != nil {
		t.Fatalf("re-parse delta: %v", err)
	}
	if delta.Role != "assistant" {
		t.Fatalf("delta role not round-tripped: %s", raw)
	}
}
