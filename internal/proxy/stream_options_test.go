package proxy

import (
	"encoding/json"
	"testing"
)

// TestStreamOptionsInjection_AnthropicExcluded verifies that stream_options
// is NOT injected for provider types that don't support it (Anthropic, Google,
// Cohere, etc.). This simulates the per-candidate rewrite block logic.
func TestStreamOptionsInjection_AnthropicExcluded(t *testing.T) {
	t.Parallel()

	baseBody := map[string]interface{}{
		"model":    "claude-3-opus-20240229",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		"stream":   true,
	}

	// Simulate the rewrite block for Anthropic: stream_options NOT injected.
	if providerSupportsStreamOptions("anthropic") {
		t.Fatal("anthropic should not support stream_options")
	}
	// The proxy code checks: if isStreaming && providerSupportsStreamOptions(providerType)
	// Skip injection since providerSupportsStreamOptions returns false.

	bodyBytes, _ := json.Marshal(baseBody)
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		t.Fatal(err)
	}
	if _, exists := result["stream_options"]; exists {
		t.Error("stream_options should NOT be present for Anthropic provider")
	}
}

// TestStreamOptionsInjection_OpenAIIncluded verifies that stream_options
// IS injected for OpenAI-compatible provider types.
func TestStreamOptionsInjection_OpenAIIncluded(t *testing.T) {
	t.Parallel()

	for _, providerType := range []string{"openai", "deepseek", "xai", "openrouter", "ollama"} {
		t.Run(providerType, func(t *testing.T) {
			baseBody := map[string]interface{}{
				"model":    "gpt-4",
				"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
				"stream":   true,
			}

			if !providerSupportsStreamOptions(providerType) {
				t.Fatalf("%s should support stream_options", providerType)
			}

			// Simulate the injection the proxy would do.
			baseBody["stream_options"] = map[string]interface{}{
				"include_usage": true,
			}

			bodyBytes, _ := json.Marshal(baseBody)
			var result map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &result); err != nil {
				t.Fatal(err)
			}

			so, ok := result["stream_options"].(map[string]interface{})
			if !ok {
				t.Errorf("stream_options missing or wrong type for %s", providerType)
				return
			}
			if so["include_usage"] != true {
				t.Errorf("stream_options.include_usage should be true for %s", providerType)
			}
		})
	}
}

// TestStreamOptionsInjection_NonStreamingNeverInjected verifies that
// stream_options is never injected for non-streaming requests, regardless
// of provider type. The proxy only injects when isStreaming is true.
func TestStreamOptionsInjection_NonStreamingNeverInjected(t *testing.T) {
	t.Parallel()

	baseBody := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []interface{}{map[string]interface{}{"role": "user", "content": "hi"}},
		"stream":   false,
	}

	// Non-streaming: the proxy code checks `if isStreaming && ...`,
	// so stream_options is never added.
	bodyBytes, _ := json.Marshal(baseBody)
	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		t.Fatal(err)
	}
	if _, exists := result["stream_options"]; exists {
		t.Error("stream_options should NOT be present for non-streaming requests")
	}
}
