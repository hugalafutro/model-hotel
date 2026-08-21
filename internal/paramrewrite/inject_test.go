package paramrewrite

import (
	"encoding/json"
	"testing"
)

func TestNeedsProviderInjection(t *testing.T) {
	tests := []struct {
		providerType string
		want         bool
	}{
		{"zai-coding", true},
		// opencode-zen is false on purpose: Zen rejects chat_template_args (see
		// TestInjectProviderParams_OpencodeZen_NotInjected).
		{"opencode-zen", false},
		{"opencode-go", true},
		{"deepseek", true},
		{"openai", false},
		{"anthropic", false},
		{"ollama-cloud", false},
		{"google", false},
		{"openrouter", false},
		{"cohere", false},
		{"xai", false},
		{"nanogpt", false},
		{"ollama", false},
		{"koboldcpp", false},
		{"lmstudio", false},
	}

	for _, tt := range tests {
		t.Run(tt.providerType, func(t *testing.T) {
			if got := NeedsProviderInjection(tt.providerType); got != tt.want {
				t.Errorf("NeedsProviderInjection(%q) = %v, want %v", tt.providerType, got, tt.want)
			}
		})
	}
}

func TestInjectProviderParams_ZaiCoding(t *testing.T) {
	raw := map[string]any{
		"model":    "glm-5.1",
		"messages": []any{},
	}
	modified := InjectProviderParams(raw, "zai-coding", "glm-5.1")
	if !modified {
		t.Fatal("expected modification for zai-coding")
	}
	thinking, ok := raw["thinking"].(map[string]any)
	if !ok {
		t.Fatal("expected thinking map to be injected")
	}
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v, want enabled", thinking["type"])
	}
	if thinking["clear_thinking"] != false {
		t.Errorf("thinking.clear_thinking = %v, want false", thinking["clear_thinking"])
	}
}

func TestInjectProviderParams_ZaiCoding_AlreadyPresent(t *testing.T) {
	raw := map[string]any{
		"model":    "glm-5.1",
		"thinking": map[string]any{"type": "disabled"},
		"messages": []any{},
	}
	modified := InjectProviderParams(raw, "zai-coding", "glm-5.1")
	if modified {
		t.Error("should not modify when thinking already present")
	}
}

// TestInjectProviderParams_OpencodeZen_NotInjected pins a live-verified
// regression: OpenCode Zen used to be injected with chat_template_args like Go,
// but Zen's backend now rejects it. A/B testing every Zen model on 2026-07-30
// showed glm-5, glm-5.1, glm-5.2, minimax-m3, kimi-k2.6, kimi-k3 and
// deepseek-v4-pro all fail with "Upstream request failed" when the parameter is
// present and all succeed without it; only qwen3.6-plus tolerated it. Zen's
// error is too generic for the 400 param-strip retry to recognise as a
// parameter fault, so injecting it made those models unusable outright.
func TestInjectProviderParams_OpencodeZen_NotInjected(t *testing.T) {
	raw := map[string]any{
		"model":    "glm-5.2",
		"messages": []any{},
	}
	modified := InjectProviderParams(raw, "opencode-zen", "glm-5.2")
	if modified {
		t.Fatal("opencode-zen must not be modified: Zen rejects chat_template_args")
	}
	if _, present := raw["chat_template_args"]; present {
		t.Error("chat_template_args must never be injected for opencode-zen")
	}
}

func TestInjectProviderParams_OpencodeGo(t *testing.T) {
	raw := map[string]any{
		"model":    "glm-4.6",
		"messages": []any{},
	}
	modified := InjectProviderParams(raw, "opencode-go", "glm-4.6")
	if !modified {
		t.Fatal("expected modification for opencode-go")
	}
	args, ok := raw["chat_template_args"].(map[string]any)
	if !ok {
		t.Fatal("expected chat_template_args map to be injected")
	}
	if args["enable_thinking"] != true {
		t.Errorf("chat_template_args.enable_thinking = %v, want true", args["enable_thinking"])
	}
}

func TestInjectProviderParams_Opencode_AlreadyPresent(t *testing.T) {
	raw := map[string]any{
		"model":              "kimi-k2-thinking",
		"chat_template_args": map[string]any{"enable_thinking": false},
		"messages":           []any{},
	}
	modified := InjectProviderParams(raw, "opencode-zen", "kimi-k2-thinking")
	if modified {
		t.Error("should not modify when chat_template_args already present")
	}
}

func TestInjectProviderParams_DeepSeekV4(t *testing.T) {
	raw := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
			map[string]any{"role": "assistant", "content": "Hi there"},
			map[string]any{"role": "assistant", "content": "More info", "reasoning_content": "thinking..."},
			map[string]any{"role": "user", "content": "Follow up"},
		},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-v4-pro")
	if !modified {
		t.Fatal("expected modification for deepseek v4")
	}
	messages := raw["messages"].([]any)
	// First assistant message should have reasoning_content backfilled
	first := messages[1].(map[string]any)
	if _, exists := first["reasoning_content"]; !exists {
		t.Error("first assistant message missing reasoning_content")
	}
	if first["reasoning_content"] != "" {
		t.Errorf("first assistant reasoning_content = %v, want empty string", first["reasoning_content"])
	}
	// Second assistant message already had reasoning_content, should be unchanged
	second := messages[2].(map[string]any)
	if second["reasoning_content"] != "thinking..." {
		t.Errorf("second assistant reasoning_content = %v, want 'thinking...'", second["reasoning_content"])
	}
}

func TestInjectProviderParams_DeepSeekR1(t *testing.T) {
	raw := map[string]any{
		"model": "deepseek-r1",
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
			map[string]any{"role": "assistant", "content": "Thinking hard"},
		},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-r1")
	if !modified {
		t.Fatal("expected modification for deepseek r1")
	}
}

func TestInjectProviderParams_DeepSeekNonReasoning(t *testing.T) {
	raw := map[string]any{
		"model":    "deepseek-chat",
		"messages": []any{},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-chat")
	if modified {
		t.Error("should not modify for non-reasoning deepseek model")
	}
}

// TestInjectProviderParams_DeepSeekReasonerAlias covers the alias whose name
// carries no version. deepseek-reasoner is absent from DeepSeek's /models
// listing and resolves to deepseek-v4-flash with thinking on, so it needs the
// same backfill as the versioned id, which the v4/r1 substring test misses.
func TestInjectProviderParams_DeepSeekReasonerAlias(t *testing.T) {
	raw := map[string]any{
		"model": "deepseek-reasoner",
		"messages": []any{
			map[string]any{"role": "user", "content": "Q1"},
			map[string]any{"role": "assistant", "content": "A1"},
			map[string]any{"role": "user", "content": "Q2"},
		},
	}
	if !InjectProviderParams(raw, "deepseek", "deepseek-reasoner") {
		t.Fatal("expected reasoning_content backfill for deepseek-reasoner")
	}
	assistant := raw["messages"].([]any)[1].(map[string]any)
	if _, exists := assistant["reasoning_content"]; !exists {
		t.Error("assistant message missing reasoning_content")
	}
}

// TestInjectProviderParams_DeepSeekChatAliasSkipped is the other half: chat is
// the non-thinking preset on the same model and returns no reasoning_content,
// so it must stay out of the backfill.
func TestInjectProviderParams_DeepSeekChatAliasSkipped(t *testing.T) {
	raw := map[string]any{
		"model": "deepseek-chat",
		"messages": []any{
			map[string]any{"role": "user", "content": "Q1"},
			map[string]any{"role": "assistant", "content": "A1"},
			map[string]any{"role": "user", "content": "Q2"},
		},
	}
	if InjectProviderParams(raw, "deepseek", "deepseek-chat") {
		t.Error("deepseek-chat is non-thinking and must not be backfilled")
	}
	assistant := raw["messages"].([]any)[1].(map[string]any)
	if _, exists := assistant["reasoning_content"]; exists {
		t.Error("deepseek-chat assistant message should not gain reasoning_content")
	}
}

func TestInjectProviderParams_DeepSeekV4_AllAssistantBackfilled(t *testing.T) {
	raw := map[string]any{
		"model": "deepseek-v4-pro",
		"messages": []any{
			map[string]any{"role": "user", "content": "Q1"},
			map[string]any{"role": "assistant", "content": "A1"},
			map[string]any{"role": "user", "content": "Q2"},
			map[string]any{"role": "assistant", "content": "A2"},
			map[string]any{"role": "user", "content": "Q3"},
		},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-v4-pro")
	if !modified {
		t.Fatal("expected modification")
	}
	messages := raw["messages"].([]any)
	for i, msg := range messages {
		m := msg.(map[string]any)
		if m["role"] == "assistant" {
			if _, exists := m["reasoning_content"]; !exists {
				t.Errorf("assistant message at index %d missing reasoning_content", i)
			}
		}
	}
}

func TestInjectProviderParams_UnsupportedProvider(t *testing.T) {
	raw := map[string]any{
		"model":    "gpt-4",
		"messages": []any{},
	}
	modified := InjectProviderParams(raw, "openai", "gpt-4")
	if modified {
		t.Error("should not modify for unsupported provider type")
	}
}

func TestBackfillDeepSeekReasoning_NoMessages(t *testing.T) {
	raw := map[string]any{
		"model": "deepseek-v4",
	}
	modified := backfillDeepSeekReasoning(raw)
	if modified {
		t.Error("should not modify when no messages array")
	}
}

func TestBackfillDeepSeekReasoning_InvalidMessages(t *testing.T) {
	raw := map[string]any{
		"model":    "deepseek-v4",
		"messages": "not an array",
	}
	modified := backfillDeepSeekReasoning(raw)
	if modified {
		t.Error("should not modify when messages is not an array")
	}
}

func TestInjectProviderParams_RoundTrip(t *testing.T) {
	// Verify that injected params survive JSON round-trip
	raw := map[string]any{
		"model":    "glm-5.1",
		"messages": []any{},
	}
	InjectProviderParams(raw, "zai-coding", "glm-5.1")

	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	thinking, ok := decoded["thinking"].(map[string]any)
	if !ok {
		t.Fatal("thinking field lost in round-trip")
	}
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v after round-trip, want enabled", thinking["type"])
	}
}
