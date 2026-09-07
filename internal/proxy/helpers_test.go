package proxy

import (
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
)

// ---------------------------------------------------------------------------
// normalizeFinishReason
// ---------------------------------------------------------------------------

func TestNormalizeFinishReason_Anthropic(t *testing.T) {
	tests := []struct{ in, want string }{
		{"end_turn", "stop"},
		{"stop_sequence", "stop"},
		{"tool_use", "tool_calls"},
		{"refusal", "content_filter"},
		{"max_tokens", "length"}, // Anthropic uses same name as OpenAI
	}
	for _, tt := range tests {
		got := normalizeFinishReason(tt.in)
		if got != tt.want {
			t.Errorf("normalizeFinishReason(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeFinishReason_Gemini(t *testing.T) {
	tests := []struct{ in, want string }{
		{"STOP", "stop"},
		{"MAX_TOKENS", "length"},
		{"SAFETY", "content_filter"},
		{"RECITATION", "content_filter"},
		{"BLOCKED", "content_filter"},
	}
	for _, tt := range tests {
		got := normalizeFinishReason(tt.in)
		if got != tt.want {
			t.Errorf("normalizeFinishReason(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeFinishReason_Cohere(t *testing.T) {
	if got := normalizeFinishReason("COMPLETE"); got != "stop" {
		t.Errorf("normalizeFinishReason(COMPLETE) = %q, want stop", got)
	}
	if got := normalizeFinishReason("ERROR_TOXIC"); got != "content_filter" {
		t.Errorf("normalizeFinishReason(ERROR_TOXIC) = %q, want content_filter", got)
	}
}

func TestNormalizeFinishReason_DeepSeek_xAI(t *testing.T) {
	if got := normalizeFinishReason("insufficient_system_resource"); got != "length" {
		t.Errorf("normalizeFinishReason(insufficient_system_resource) = %q, want length", got)
	}
}

func TestNormalizeFinishReason_PassThrough(t *testing.T) {
	// Standard OpenAI values should pass through unchanged.
	for _, v := range []string{"stop", "length", "content_filter", "tool_calls"} {
		if got := normalizeFinishReason(v); got != v {
			t.Errorf("normalizeFinishReason(%q) = %q, want %q (passthrough)", v, got, v)
		}
	}
	// Unknown values should also pass through unchanged.
	if got := normalizeFinishReason("unknown_value"); got != "unknown_value" {
		t.Errorf("normalizeFinishReason(unknown_value) = %q, want unknown_value", got)
	}
}

func TestNormalizeFinishReason_HuggingFace(t *testing.T) {
	if got := normalizeFinishReason("eos_token"); got != "stop" {
		t.Errorf("normalizeFinishReason(eos_token) = %q, want stop", got)
	}
	if got := normalizeFinishReason("eos"); got != "stop" {
		t.Errorf("normalizeFinishReason(eos) = %q, want stop", got)
	}
}

func TestNormalizeFinishReason_Bedrock(t *testing.T) {
	if got := normalizeFinishReason("guardrail_intervened"); got != "content_filter" {
		t.Errorf("normalizeFinishReason(guardrail_intervened) = %q, want content_filter", got)
	}
}

// ---------------------------------------------------------------------------
// parseChunkPayload
// ---------------------------------------------------------------------------

func TestParseChunkPayload_ValidChunk(t *testing.T) {
	payload := `{"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`
	p, ok := parseChunkPayload(payload)
	if !ok {
		t.Fatal("expected parseChunkPayload to succeed on valid chunk")
	}
	if len(p.raw) == 0 {
		t.Error("expected raw map to be populated")
	}
	if len(p.choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(p.choices))
	}
	if len(p.delta) == 0 {
		t.Error("expected delta map to be populated")
	}
	if _, ok := p.delta["content"]; !ok {
		t.Error("expected delta to contain 'content' key")
	}
	if _, ok := p.choices[0]["finish_reason"]; !ok {
		t.Error("expected choices[0] to contain 'finish_reason' key")
	}
}

func TestParseChunkPayload_NoChoicesKey(t *testing.T) {
	payload := `{"id":"chatcmpl-123","object":"chat.completion.chunk"}`
	_, ok := parseChunkPayload(payload)
	if ok {
		t.Error("expected parseChunkPayload to fail when no choices key")
	}
}

func TestParseChunkPayload_EmptyChoicesArray(t *testing.T) {
	payload := `{"id":"chatcmpl-123","choices":[]}`
	_, ok := parseChunkPayload(payload)
	if ok {
		t.Error("expected parseChunkPayload to fail when choices array is empty")
	}
}

func TestParseChunkPayload_NoDeltaKey(t *testing.T) {
	payload := `{"id":"chatcmpl-123","choices":[{"index":0}]}`
	_, ok := parseChunkPayload(payload)
	if ok {
		t.Error("expected parseChunkPayload to fail when no delta key in choices[0]")
	}
}

func TestParseChunkPayload_MalformedJSON(t *testing.T) {
	_, ok := parseChunkPayload("{invalid}")
	if ok {
		t.Error("expected parseChunkPayload to fail on malformed JSON")
	}
}

func TestParseChunkPayload_EmptyString(t *testing.T) {
	_, ok := parseChunkPayload("")
	if ok {
		t.Error("expected parseChunkPayload to fail on empty string")
	}
}

func TestParseChunkPayload_MultipleChoices(t *testing.T) {
	payload := `{"choices":[{"delta":{"content":"a"}},{"delta":{"content":"b"}}]}`
	p, ok := parseChunkPayload(payload)
	if !ok {
		t.Fatal("expected parseChunkPayload to succeed with multiple choices")
	}
	if len(p.choices) != 2 {
		t.Errorf("expected 2 choices, got %d", len(p.choices))
	}
	if _, ok := p.delta["content"]; !ok {
		t.Error("expected delta to come from choices[0]")
	}
}

func TestParseChunkPayload_DeltaWithReasoningFields(t *testing.T) {
	payload := `{"id":"chatcmpl-1","choices":[{"delta":{"reasoning_content":"thinking...","reasoning":"hmm","content":"hello"}}]}`
	p, ok := parseChunkPayload(payload)
	if !ok {
		t.Fatal("expected parseChunkPayload to succeed")
	}
	if _, ok := p.delta["reasoning_content"]; !ok {
		t.Error("expected delta to contain 'reasoning_content'")
	}
	if _, ok := p.delta["reasoning"]; !ok {
		t.Error("expected delta to contain 'reasoning'")
	}
	if _, ok := p.delta["content"]; !ok {
		t.Error("expected delta to contain 'content'")
	}
}

func TestParseChunkPayload_DeltaEmptyObject(t *testing.T) {
	payload := `{"id":"chatcmpl-1","choices":[{"delta":{}}]}`
	p, ok := parseChunkPayload(payload)
	if !ok {
		t.Fatal("expected parseChunkPayload to succeed with empty delta")
	}
	if len(p.delta) != 0 {
		t.Errorf("expected empty delta map, got %d fields", len(p.delta))
	}
}

func TestParseChunkPayload_ChoicesNotArray(t *testing.T) {
	payload := `{"choices":"not an array"}`
	_, ok := parseChunkPayload(payload)
	if ok {
		t.Error("expected parseChunkPayload to fail when choices is not an array")
	}
}

func TestParseChunkPayload_DeltaNotObject(t *testing.T) {
	payload := `{"choices":[{"delta":"not an object"}]}`
	_, ok := parseChunkPayload(payload)
	if ok {
		t.Error("expected parseChunkPayload to fail when delta is not an object")
	}
}

// ---------------------------------------------------------------------------
// parseAccumulatedError
// ---------------------------------------------------------------------------

func TestParseAccumulatedError_Empty(t *testing.T) {
	got := parseAccumulatedError(nil)
	if got != "" {
		t.Errorf("parseAccumulatedError(nil) = %q, want empty string", got)
	}
	got = parseAccumulatedError([]byte{})
	if got != "" {
		t.Errorf("parseAccumulatedError([]) = %q, want empty string", got)
	}
}

func TestParseAccumulatedError_OpenAIFormat(t *testing.T) {
	data := []byte(`{"error":{"message":"Rate limit exceeded","type":"rate_limit_error","param":null,"code":"rate_limit_exceeded"}}`)
	got := parseAccumulatedError(data)
	if got != "Rate limit exceeded" {
		t.Errorf("parseAccumulatedError(OpenAI format) = %q, want %q", got, "Rate limit exceeded")
	}
}

func TestParseAccumulatedError_AnthropicFormat(t *testing.T) {
	data := []byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	got := parseAccumulatedError(data)
	if got != "Overloaded" {
		t.Errorf("parseAccumulatedError(Anthropic format) = %q, want %q", got, "Overloaded")
	}
}

func TestParseAccumulatedError_AnthropicOverloaded(t *testing.T) {
	data := []byte(`{"type":"error","error":{"type":"overloaded_error","message":"API is temporarily overloaded"}}`)
	got := parseAccumulatedError(data)
	if got != "API is temporarily overloaded" {
		t.Errorf("parseAccumulatedError() = %q, want %q", got, "API is temporarily overloaded")
	}
}

// A truncated member yields its own bytes — the best that can be said about
// bytes nothing can parse — and not the frame that wrapped it.
func TestParseAccumulatedError_TruncatedJSON(t *testing.T) {
	data := []byte(`{"error":{"message":"Rate limi`)
	got := parseAccumulatedError(data)
	if got != `{"message":"Rate limi` {
		t.Errorf("parseAccumulatedError(truncated JSON) = %q, want the member's bytes", got)
	}
}

// The reason the member is read rather than the payload: a frame cut off
// mid-content, on a provider that puts "error" first, used to record the
// model's answer as the error message.
func TestParseAccumulatedError_TruncatedFrameKeepsContentOut(t *testing.T) {
	for _, data := range []string{
		`{"error":null,"choices":[{"delta":{"content":"THE SECRET ANSWER`,
		`{"error":"","choices":[{"delta":{"content":"THE SECRET ANSWER`,
		`{"choices":[{"delta":{"content":"THE SECRET ANSWER`,
		`{"choices":[{"delta":{"content":"THE SECRET ANSWER"}}],"error":`,
		// Not truncated — MALFORMED. A raw tab in the provider's error string
		// is enough (JSON forbids it unescaped), and so is an ANSI escape from
		// a local server echoing stderr. The decode fails in the same place a
		// truncation does, but here the bytes after the member really are the
		// rest of the frame.
		"{\"error\":\"rate limit\thit\",\"choices\":[{\"delta\":{\"content\":\"THE SECRET ANSWER\"}}]}",
		"{\"error\":\"\x1b[31mred\",\"choices\":[{\"delta\":{\"content\":\"THE SECRET ANSWER\"}}]}",
		`{"error":"bad \q escape","choices":[{"delta":{"content":"THE SECRET ANSWER"}}]}`,
		`{"error":NaN,"choices":[{"delta":{"content":"THE SECRET ANSWER"}}]}`,
		`{"error":'single',"choices":[{"delta":{"content":"THE SECRET ANSWER"}}]}`,
	} {
		if got := parseAccumulatedError([]byte(data)); strings.Contains(got, "SECRET") {
			t.Errorf("content escaped into the error message: %q", got)
		}
	}
}

// A frame truncated around a relay's no-error stamp is not a failed request.
// The member decoded whole, so the shared emptiness rule judges it, exactly as
// it would on a frame that arrived intact.
func TestParseAccumulatedError_EmptyMemberInTruncatedFrame(t *testing.T) {
	for _, data := range []string{
		`{"error":false,"choices":[{"delta":{"content":"hi`,
		`{"error":0,"choices":[{"delta":{"content":"hi`,
		`{"error":[],"choices":[{"delta":{"content":"hi`,
		`{"error":{},"choices":[{"delta":{"content":"hi`,
		`{"error":{"code":0,"message":"","type":""},"choices":[{"delta":{"content":"hi`,
	} {
		if got := parseAccumulatedError([]byte(data)); got != "" {
			t.Errorf("%s recorded an error: %q", data, got)
		}
	}
}

// A duplicate key resolves the way json.Unmarshal resolves it everywhere else
// this member is read: the last one wins.
func TestParseAccumulatedError_LastErrorKeyWins(t *testing.T) {
	if got := parseAccumulatedError([]byte(`{"error":"first","error":"second","x":`)); got != "second" {
		t.Errorf("got %q, want second", got)
	}
}

func TestParseAccumulatedError_NonJSONObject(t *testing.T) {
	data := []byte(`not json at all`)
	got := parseAccumulatedError(data)
	if got != "" {
		t.Errorf("parseAccumulatedError(non-JSON) = %q, want empty string", got)
	}
}

func TestParseAccumulatedError_OpenAISimpleError(t *testing.T) {
	data := []byte(`{"error":{"message":"Internal server error"}}`)
	got := parseAccumulatedError(data)
	if got != "Internal server error" {
		t.Errorf("parseAccumulatedError(simple error) = %q, want %q", got, "Internal server error")
	}
}

// ---------------------------------------------------------------------------
// generateRequestHash
// ---------------------------------------------------------------------------

func TestGenerateRequestHash_NonEmpty(t *testing.T) {
	hash := generateRequestHash()
	if hash == "" {
		t.Error("generateRequestHash should return a non-empty string")
	}
}

func TestGenerateRequestHash_CorrectLength(t *testing.T) {
	// 8 random bytes → 16 hex characters
	hash := generateRequestHash()
	if len(hash) != 16 {
		t.Errorf("generateRequestHash should return 16 hex chars (8 bytes), got %d chars: %q", len(hash), hash)
	}
}

func TestGenerateRequestHash_IsHexString(t *testing.T) {
	hash := generateRequestHash()
	for _, c := range hash {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("generateRequestHash should return hex string, found non-hex char %q in %q", c, hash)
			break
		}
	}
}

func TestGenerateRequestHash_Unique(t *testing.T) {
	hashes := make(map[string]bool)
	for range 100 {
		hash := generateRequestHash()
		if hashes[hash] {
			t.Errorf("generateRequestHash produced duplicate hash: %q", hash)
		}
		hashes[hash] = true
	}
}

func TestGenerateRequestHash_MultipleCallsDiffer(t *testing.T) {
	hash1 := generateRequestHash()
	hash2 := generateRequestHash()
	// Statistically these should differ (collision probability: 2^-64 per pair)
	if hash1 == hash2 {
		t.Errorf("two consecutive hashes should be different: %q == %q", hash1, hash2)
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_test.go
// ---------------------------------------------------------------------------

// TestParseAccumulatedError_Nil tests that parseAccumulatedError with nil
// error returns nil.
func TestParseAccumulatedError_Nil(t *testing.T) {
	t.Helper()
	result := parseAccumulatedError(nil)
	if result != "" {
		t.Errorf("expected empty string for nil input, got %q", result)
	}
}

// TestParseAccumulatedError_NonAccumulated tests that parseAccumulatedError
// with a regular error (not from accumulation) handles various inputs.
func TestParseAccumulatedError_NonAccumulated(t *testing.T) {
	t.Helper()
	// Regular error that doesn't match OpenAI or Anthropic error formats
	data := []byte("some random error message")
	result := parseAccumulatedError(data)
	// Should return empty string since it doesn't start with {
	if result != "" {
		t.Errorf("expected empty string for non-JSON error, got %q", result)
	}

	// JSON with no error member has no error message to report; returning the
	// body would be reporting whatever else the provider put in it.
	jsonData := []byte(`{"foo":"bar"}`)
	result = parseAccumulatedError(jsonData)
	if result != "" {
		t.Errorf("expected empty string for a body with no error member, got %q", result)
	}
}

func TestProviderSupportsStreamOptions(t *testing.T) {
	t.Parallel()

	// Providers with non-OpenAI APIs that reject stream_options.
	for _, pt := range []string{"anthropic", "google", "cohere", "opencode-go", "opencode-zen"} {
		t.Run(pt+"_unsupported", func(t *testing.T) {
			if paramrewrite.ProviderSupportsStreamOptions(pt) {
				t.Errorf("paramrewrite.ProviderSupportsStreamOptions(%q) = true, want false", pt)
			}
		})
	}

	// OpenAI-compatible providers that accept or ignore stream_options.
	for _, pt := range []string{
		"openai", "deepseek", "xai", "openrouter",
		"ollama", "ollama-cloud", "nanogpt", "zai-coding",
		"lmstudio", "koboldcpp", "neuralwatt", "bedrock",
	} {
		t.Run(pt+"_supported", func(t *testing.T) {
			if !paramrewrite.ProviderSupportsStreamOptions(pt) {
				t.Errorf("paramrewrite.ProviderSupportsStreamOptions(%q) = false, want true", pt)
			}
		})
	}

	// Unknown provider types default to supported (OpenAI-compatible assumption).
	t.Run("unknown_supported", func(t *testing.T) {
		if !paramrewrite.ProviderSupportsStreamOptions("some-new-provider") {
			t.Error("unknown provider type should default to supported=true")
		}
	})
}
