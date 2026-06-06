package proxy

import "testing"

func TestInjectProviderParams_ZaiCoding(t *testing.T) {
	raw := map[string]interface{}{}
	modified := InjectProviderParams(raw, "zai-coding", "test-model")
	if !modified {
		t.Error("Expected modification for zai-coding")
	}
	thinking, ok := raw["thinking"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected 'thinking' key to be injected")
	}
	if thinking["type"] != "enabled" {
		t.Errorf("Expected thinking.type='enabled', got %v", thinking["type"])
	}
}

func TestInjectProviderParams_ZaiCoding_ExistingThinking(t *testing.T) {
	raw := map[string]interface{}{
		"thinking": map[string]interface{}{"type": "disabled"},
	}
	modified := InjectProviderParams(raw, "zai-coding", "test-model")
	if modified {
		t.Error("Expected no modification when thinking already exists")
	}
}

func TestInjectProviderParams_OpenCodeZen(t *testing.T) {
	raw := map[string]interface{}{}
	modified := InjectProviderParams(raw, "opencode-zen", "test-model")
	if !modified {
		t.Error("Expected modification for opencode-zen")
	}
	args, ok := raw["chat_template_args"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected 'chat_template_args' key to be injected")
	}
	if args["enable_thinking"] != true {
		t.Errorf("Expected enable_thinking=true, got %v", args["enable_thinking"])
	}
}

func TestInjectProviderParams_OpenCodeGo(t *testing.T) {
	raw := map[string]interface{}{}
	modified := InjectProviderParams(raw, "opencode-go", "test-model")
	if !modified {
		t.Error("Expected modification for opencode-go")
	}
}

func TestInjectProviderParams_DeepSeekV4(t *testing.T) {
	raw := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "Hello",
			},
		},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-v4")
	if !modified {
		t.Error("Expected modification for deepseek v4 reasoning model")
	}
}

func TestInjectProviderParams_DeepSeekNonReasoning(t *testing.T) {
	raw := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "Hello",
			},
		},
	}
	modified := InjectProviderParams(raw, "deepseek", "deepseek-chat")
	if modified {
		t.Error("Expected no modification for non-reasoning deepseek model")
	}
}

func TestInjectProviderParams_UnknownProvider(t *testing.T) {
	raw := map[string]interface{}{}
	modified := InjectProviderParams(raw, "openai", "gpt-4")
	if modified {
		t.Error("Expected no modification for unknown provider")
	}
}

func TestBackfillDeepSeekReasoning_NoMessages(t *testing.T) {
	raw := map[string]interface{}{}
	if backfillDeepSeekReasoning(raw) {
		t.Error("Expected false when no messages key")
	}
}

func TestBackfillDeepSeekReasoning_NoAssistantMessages(t *testing.T) {
	raw := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "Hello",
			},
		},
	}
	if backfillDeepSeekReasoning(raw) {
		t.Error("Expected false when no assistant messages")
	}
}

func TestBackfillDeepSeekReasoning_BackfillsEmpty(t *testing.T) {
	raw := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "assistant",
				"content": "Hello",
			},
		},
	}
	if !backfillDeepSeekReasoning(raw) {
		t.Error("Expected true when backfilling reasoning_content")
	}
	msgs := raw["messages"].([]interface{})
	msg := msgs[0].(map[string]interface{})
	if rc, ok := msg["reasoning_content"]; !ok || rc != "" {
		t.Errorf("Expected reasoning_content='', got %v", rc)
	}
}

func TestBackfillDeepSeekReasoning_ExistingKeyNotOverwritten(t *testing.T) {
	raw := map[string]interface{}{
		"messages": []interface{}{
			map[string]interface{}{
				"role":              "assistant",
				"content":           "Hello",
				"reasoning_content": "I thought about this",
			},
		},
	}
	if backfillDeepSeekReasoning(raw) {
		t.Error("Expected false when reasoning_content already exists")
	}
}
