package proxy

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
		{"opencode-zen", true},
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
	raw := map[string]interface{}{
		"model":    "glm-5.1",
		"messages": []interface{}{},
	}
	modified := InjectProviderParams(raw, "zai-coding", "glm-5.1")
	if !modified {
		t.Fatal("expected modification for zai-coding")
	}
	thinking, ok := raw["thinking"].(map[string]interface{})
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
	raw := map[string]interface{}{
		"model":    "glm-5.1",
		"thinking": map[string]interface{}{"type": "disabled"},
		"messages": []interface{}{},
	}
	modified := InjectProviderParams(raw, "zai-coding", "glm-5.1")
	if modified {
		t.Error("should not modify when thinking already present")
	}
}

func TestInjectProviderParams_OpencodeZen(t *testing.T) {
	raw := map[string]interface{}{
		"model":    "kimi-k2-thinking",
		"messages": []interface{}{},
	}
	modified := InjectProviderParams(raw, "opencode-zen", "kimi-k2-thinking")
	if !modified {
		t.Fatal("expected modification for opencode-zen")
	}
	args, ok := raw["chat_template_args"].(map[string]interface{})
	if !ok {
		t.Fatal("expected chat_template_args map to be injected")
	}
	if args["enable_thinking"] != true {
		t.Errorf("chat_template_args.enable_thinking = %v, want true", args["enable_thinking"])
	}
}

func TestInjectProviderParams_OpencodeGo(t *testing.T) {
	raw := map[string]interface{}{
		"model":    "glm-4.6",
		"messages": []interface{}{},
	}
	modified := InjectProviderParams(raw, "opencode-go", "glm-4.6")
	if !modified {
		t.Fatal("expected modification for opencode-go")
	}
	args, ok := raw["chat_template_args"].(map[string]interface{})
	if !ok {
		t.Fatal("expected chat_template_args map to be injected")
	}
	if args["enable_thinking"] != true {
		t.Errorf("chat_template_args.enable_thinking = %v, want true", args["enable_thinking"])
	}
}

func TestInjectProviderParams_Opencode_AlreadyPresent(t *testing.T) {
	raw := map[string]interface{}{
		"model":              "kimi-k2-thinking",
		"chat_template_args": map[string]interface{}{"enable_thinking": false},
		"messages":           []interface{}{},
	}
	modified := InjectProviderParams(raw, "opencode-zen", "kimi-k2-thinking")
	if modified {
		t.Error("should not modify when chat_template_args already present")
	}
}

func TestInjectProviderParams_DeepSeekV4(t *testing.T) {
	raw := map[string]interface{}{
		"model": "deepseek-v4-pro",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
			map[string]interface{}{"role": "assistant", "content": "Hi there"},
			map[string]interface{}{"role": "assistant", "content": "More info", "reasoning_content": "thinking..."},
			map[string]interface{}{"role": "user", "content": "Follow up"},
		},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-v4-pro")
	if !modified {
		t.Fatal("expected modification for deepseek v4")
	}
	messages := raw["messages"].([]interface{})
	// First assistant message should have reasoning_content backfilled
	first := messages[1].(map[string]interface{})
	if _, exists := first["reasoning_content"]; !exists {
		t.Error("first assistant message missing reasoning_content")
	}
	if first["reasoning_content"] != "" {
		t.Errorf("first assistant reasoning_content = %v, want empty string", first["reasoning_content"])
	}
	// Second assistant message already had reasoning_content, should be unchanged
	second := messages[2].(map[string]interface{})
	if second["reasoning_content"] != "thinking..." {
		t.Errorf("second assistant reasoning_content = %v, want 'thinking...'", second["reasoning_content"])
	}
}

func TestInjectProviderParams_DeepSeekR1(t *testing.T) {
	raw := map[string]interface{}{
		"model": "deepseek-r1",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Hello"},
			map[string]interface{}{"role": "assistant", "content": "Thinking hard"},
		},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-r1")
	if !modified {
		t.Fatal("expected modification for deepseek r1")
	}
}

func TestInjectProviderParams_DeepSeekNonReasoning(t *testing.T) {
	raw := map[string]interface{}{
		"model":    "deepseek-chat",
		"messages": []interface{}{},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-chat")
	if modified {
		t.Error("should not modify for non-reasoning deepseek model")
	}
}

func TestInjectProviderParams_DeepSeekV4_AllAssistantBackfilled(t *testing.T) {
	raw := map[string]interface{}{
		"model": "deepseek-v4-pro",
		"messages": []interface{}{
			map[string]interface{}{"role": "user", "content": "Q1"},
			map[string]interface{}{"role": "assistant", "content": "A1"},
			map[string]interface{}{"role": "user", "content": "Q2"},
			map[string]interface{}{"role": "assistant", "content": "A2"},
			map[string]interface{}{"role": "user", "content": "Q3"},
		},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-v4-pro")
	if !modified {
		t.Fatal("expected modification")
	}
	messages := raw["messages"].([]interface{})
	for i, msg := range messages {
		m := msg.(map[string]interface{})
		if m["role"] == "assistant" {
			if _, exists := m["reasoning_content"]; !exists {
				t.Errorf("assistant message at index %d missing reasoning_content", i)
			}
		}
	}
}

func TestInjectProviderParams_UnsupportedProvider(t *testing.T) {
	raw := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []interface{}{},
	}
	modified := InjectProviderParams(raw, "openai", "gpt-4")
	if modified {
		t.Error("should not modify for unsupported provider type")
	}
}

func TestBackfillDeepSeekReasoning_NoMessages(t *testing.T) {
	raw := map[string]interface{}{
		"model": "deepseek-v4",
	}
	modified := backfillDeepSeekReasoning(raw)
	if modified {
		t.Error("should not modify when no messages array")
	}
}

func TestBackfillDeepSeekReasoning_InvalidMessages(t *testing.T) {
	raw := map[string]interface{}{
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
	raw := map[string]interface{}{
		"model":    "glm-5.1",
		"messages": []interface{}{},
	}
	InjectProviderParams(raw, "zai-coding", "glm-5.1")

	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	thinking, ok := decoded["thinking"].(map[string]interface{})
	if !ok {
		t.Fatal("thinking field lost in round-trip")
	}
	if thinking["type"] != "enabled" {
		t.Errorf("thinking.type = %v after round-trip, want enabled", thinking["type"])
	}
}
