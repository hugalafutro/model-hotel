package proxy

import "testing"

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
		{"google", false},
		{"ollama-cloud", false},
		{"openrouter", false},
		{"cohere", false},
		{"xai", false},
		{"nanogpt", false},
		{"ollama", false},
		{"koboldcpp", false},
		{"lmstudio", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.providerType, func(t *testing.T) {
			if got := NeedsProviderInjection(tc.providerType); got != tc.want {
				t.Errorf("NeedsProviderInjection(%q) = %v, want %v", tc.providerType, got, tc.want)
			}
		})
	}
}
