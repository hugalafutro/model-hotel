package proxy

import (
	"testing"
)

// ---------------------------------------------------------------------------
// extractStreamingUsage
// ---------------------------------------------------------------------------

func TestExtractStreamingUsage_SingleChunkWithUsage(t *testing.T) {
	data := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", usage.PromptTokens)
	}
	if usage.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want 20", usage.CompletionTokens)
	}
	if usage.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, want 30", usage.TotalTokens)
	}
}

func TestExtractStreamingUsage_MultipleChunksReturnsLast(t *testing.T) {
	data := `data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":10,"total_tokens":15}}
data: {"id":"chatcmpl-2","choices":[],"usage":{"prompt_tokens":50,"completion_tokens":100,"total_tokens":150}}`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if usage.PromptTokens != 50 {
		t.Errorf("PromptTokens = %d, want 50 (last chunk)", usage.PromptTokens)
	}
	if usage.CompletionTokens != 100 {
		t.Errorf("CompletionTokens = %d, want 100 (last chunk)", usage.CompletionTokens)
	}
	if usage.TotalTokens != 150 {
		t.Errorf("TotalTokens = %d, want 150 (last chunk)", usage.TotalTokens)
	}
}

func TestExtractStreamingUsage_DoneMarker(t *testing.T) {
	data := `data: {"id":"chatcmpl-123","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}
data: [DONE]
data: {"id":"chatcmpl-456","choices":[],"usage":{"prompt_tokens":99,"completion_tokens":99,"total_tokens":99}}`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	// Should stop at [DONE] and return the last usage before it
	if usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10 (last before [DONE])", usage.PromptTokens)
	}
	if usage.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want 20 (last before [DONE])", usage.CompletionTokens)
	}
}

func TestExtractStreamingUsage_NoUsage(t *testing.T) {
	data := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[{"delta":{"content":"hello"}}]}`

	usage := extractStreamingUsage(data)
	if usage != nil {
		t.Errorf("expected nil usage when no usage field, got %+v", usage)
	}
}

func TestExtractStreamingUsage_EmptyInput(t *testing.T) {
	usage := extractStreamingUsage("")
	if usage != nil {
		t.Errorf("expected nil for empty input, got %+v", usage)
	}
}

func TestExtractStreamingUsage_NoDataLines(t *testing.T) {
	data := `some random text
not an SSE stream
nothing useful here`

	usage := extractStreamingUsage(data)
	if usage != nil {
		t.Errorf("expected nil for non-SSE input, got %+v", usage)
	}
}

func TestExtractStreamingUsage_InvalidJSON(t *testing.T) {
	data := `data: {invalid json}`

	usage := extractStreamingUsage(data)
	if usage != nil {
		t.Errorf("expected nil for invalid JSON, got %+v", usage)
	}
}

func TestExtractStreamingUsage_MixedChunks(t *testing.T) {
	data := `data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hi"}}]}
data: {"id":"chatcmpl-2","choices":[],"usage":{"prompt_tokens":25,"completion_tokens":50,"total_tokens":75}}
data: {"id":"chatcmpl-3","choices":[{"delta":{"content":"!"}}]}`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if usage.PromptTokens != 25 {
		t.Errorf("PromptTokens = %d, want 25", usage.PromptTokens)
	}
	if usage.CompletionTokens != 50 {
		t.Errorf("CompletionTokens = %d, want 50", usage.CompletionTokens)
	}
	if usage.TotalTokens != 75 {
		t.Errorf("TotalTokens = %d, want 75", usage.TotalTokens)
	}
}

func TestExtractStreamingUsage_CacheTokens(t *testing.T) {
	data := `data: {"id":"chatcmpl-123","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"prompt_cache_hit_tokens":80,"prompt_cache_miss_tokens":20}}`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if usage.PromptCacheHitTokens != 80 {
		t.Errorf("PromptCacheHitTokens = %d, want 80", usage.PromptCacheHitTokens)
	}
	if usage.PromptCacheMissTokens != 20 {
		t.Errorf("PromptCacheMissTokens = %d, want 20", usage.PromptCacheMissTokens)
	}
}

func TestExtractStreamingUsage_NullUsage(t *testing.T) {
	data := `data: {"id":"chatcmpl-123","choices":[],"usage":null}`

	usage := extractStreamingUsage(data)
	if usage != nil {
		t.Errorf("expected nil for null usage field, got %+v", usage)
	}
}

func TestExtractStreamingUsage_MultipleDoneMarkers(t *testing.T) {
	data := `data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":5,"total_tokens":10}}
data: [DONE]
data: [DONE]`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if usage.TotalTokens != 10 {
		t.Errorf("TotalTokens = %d, want 10", usage.TotalTokens)
	}
}

func TestExtractStreamingUsage_OnlyDoneMarker(t *testing.T) {
	data := `data: [DONE]`

	usage := extractStreamingUsage(data)
	if usage != nil {
		t.Errorf("expected nil when only [DONE] marker present, got %+v", usage)
	}
}

func TestExtractStreamingUsage_LinesWithoutDataPrefix(t *testing.T) {
	data := `event: usage
data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}
event: done`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if usage.TotalTokens != 30 {
		t.Errorf("TotalTokens = %d, want 30", usage.TotalTokens)
	}
}

func TestExtractStreamingUsage_PartialUsageFields(t *testing.T) {
	data := `data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":10}}`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil even with partial fields")
	}
	if usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", usage.PromptTokens)
	}
	// Unset fields should be zero-valued
	if usage.CompletionTokens != 0 {
		t.Errorf("CompletionTokens = %d, want 0 (zero value)", usage.CompletionTokens)
	}
}

func TestExtractStreamingUsage_NoSpaceAfterData(t *testing.T) {
	// LM Studio sends "data:" without a space after the colon.
	data := `data:{"id":"chatcmpl-123","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil for data: without space")
	}
	if usage.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", usage.PromptTokens)
	}
	if usage.CompletionTokens != 20 {
		t.Errorf("CompletionTokens = %d, want 20", usage.CompletionTokens)
	}
}

func TestExtractStreamingUsage_MixedDataFormats(t *testing.T) {
	// Mix of "data: " (standard) and "data:" (no space) in same stream.
	data := `data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":10,"total_tokens":15}}
data:{"id":"chatcmpl-2","choices":[],"usage":{"prompt_tokens":50,"completion_tokens":100,"total_tokens":150}}`

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil")
	}
	if usage.PromptTokens != 50 {
		t.Errorf("PromptTokens = %d, want 50 (last chunk, no-space format)", usage.PromptTokens)
	}
}

func TestExtractStreamingUsage_DataNoSpaceWithTabAfter(t *testing.T) {
	// "data:\t" (tab after colon) should also be handled.
	data := "data:\t{\"id\":\"chatcmpl-1\",\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}"

	usage := extractStreamingUsage(data)
	if usage == nil {
		t.Fatal("expected usage to be non-nil for data: with tab")
	}
	if usage.PromptTokens != 7 {
		t.Errorf("PromptTokens = %d, want 7", usage.PromptTokens)
	}
}

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

func TestParseAccumulatedError_TruncatedJSON(t *testing.T) {
	data := []byte(`{"error":{"message":"Rate limi`)
	got := parseAccumulatedError(data)
	if got != `{"error":{"message":"Rate limi` {
		t.Errorf("parseAccumulatedError(truncated JSON) = %q, want raw string", got)
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
	for i := 0; i < 100; i++ {
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
