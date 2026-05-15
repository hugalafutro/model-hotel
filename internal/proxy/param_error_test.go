package proxy

import (
	"encoding/json"
	"testing"
)

func TestProviderUnsupportedParams_ReasoningEffort(t *testing.T) {
	// Providers that should strip reasoning_effort
	providersWithReasoningEffort := []string{
		"anthropic",
		"google",
		"cohere",
		"deepseek",
		"ollama",
		"ollama-cloud",
		"koboldcpp",
		"lmstudio",
		"nanogpt",
		"zai-coding",
		"openrouter",
		"opencode-zen",
		"opencode-go",
	}

	for _, provider := range providersWithReasoningEffort {
		params, ok := providerUnsupportedParams[provider]
		if !ok {
			t.Errorf("provider %q: missing from providerUnsupportedParams", provider)
			continue
		}
		found := false
		for _, p := range params {
			if p == "reasoning_effort" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("provider %q: reasoning_effort not listed in unsupported params", provider)
		}
	}
}

func TestProviderUnsupportedParams_OpenAISupportsReasoningEffort(t *testing.T) {
	// OpenAI and xAI support reasoning_effort — it should NOT be in their unsupported list
	for _, provider := range []string{"openai", "xai"} {
		params, ok := providerUnsupportedParams[provider]
		if !ok {
			continue // no entry is fine (means nothing is unsupported)
		}
		for _, p := range params {
			if p == "reasoning_effort" {
				t.Errorf("provider %q: reasoning_effort should NOT be in unsupported params (this provider supports it)", provider)
			}
		}
	}
}

func TestProviderUnsupportedParams_StripsFromRequestBody(t *testing.T) {
	// Verify that the stripping logic actually removes reasoning_effort from a request body
	body := map[string]any{
		"model":            "gpt-4",
		"messages":         []any{"hello"},
		"reasoning_effort": "high",
		"temperature":      0.7,
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("failed to marshal body: %v", err)
	}

	// Simulate the stripping logic from proxy.go
	var rawMap map[string]any
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		t.Fatalf("failed to unmarshal body: %v", err)
	}

	// Strip anthropic-unsupported params (includes reasoning_effort)
	if params, ok := providerUnsupportedParams["anthropic"]; ok {
		for _, p := range params {
			delete(rawMap, p)
		}
	}

	// reasoning_effort should be gone
	if _, exists := rawMap["reasoning_effort"]; exists {
		t.Error("reasoning_effort should have been stripped for anthropic provider")
	}
	// temperature should remain
	if _, exists := rawMap["temperature"]; !exists {
		t.Error("temperature should NOT have been stripped for anthropic provider")
	}
}

func TestParseProviderParamError_ReasoningEffort(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    map[string]bool
		wantNot []string
	}{
		{
			name: "backtick-wrapped reasoning_effort",
			body: "{\"error\":{\"message\":\"Unknown parameter: `reasoning_effort`\"}}",
			want: map[string]bool{"reasoning_effort": true},
		},
		{
			name: "quote-wrapped reasoning_effort",
			body: "{\"error\":{\"message\":\"Parameter \\\"reasoning_effort\\\" is not supported\"}}",
			want: map[string]bool{"reasoning_effort": true},
		},
		{
			name:    "reasoning_effort not quoted or backticked (should NOT match)",
			body:    "{\"error\":{\"message\":\"reasoning_effort is not supported\"}}",
			wantNot: []string{"reasoning_effort"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseProviderParamError([]byte(tt.body))
			for k := range tt.want {
				if !got[k] {
					t.Errorf("expected %q to be in rejected params, got %v", k, got)
				}
			}
			for _, k := range tt.wantNot {
				if got[k] {
					t.Errorf("did NOT expect %q to be in rejected params, but it was", k)
				}
			}
		})
	}
}
