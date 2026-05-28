package failover

import (
	"testing"
)

// ---------------------------------------------------------------------------
// normalizeBaseModel tests
// ---------------------------------------------------------------------------

func TestNormalizeBaseModel_SimpleNames(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"gpt-4o", "gpt-4o"},
		{"claude-3-opus", "claude-3-opus"},
		{"llama-3-70b", "llama-3-70b"},
		{"my-custom-model", "my-custom-model"},
	}
	for _, tt := range tests {
		got := normalizeBaseModel(tt.input)
		if got != tt.want {
			t.Errorf("normalizeBaseModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeBaseModel_SingleSlash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"zai-org/llama-3", "llama-3"},
		{"deepseek/deepseek-r1", "deepseek-r1"},
		{"meta-llama/llama-3-70b", "llama-3-70b"},
		{"openai/gpt-4o", "gpt-4o"},
		{"anthropic/claude-3-opus", "claude-3-opus"},
		{"wafer.ai/glm-5.1", "glm-5.1"},
		{"z-ai/glm-5.1", "glm-5.1"},
	}
	for _, tt := range tests {
		got := normalizeBaseModel(tt.input)
		if got != tt.want {
			t.Errorf("normalizeBaseModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeBaseModel_NestedSlashes(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Hosting platform + model org prefix: last segment is the model name
		{"zai-org/anthracite-org/magnum-v4-72b", "magnum-v4-72b"},
		{"z-ai/anthracite-org/magnum-v4-72b", "magnum-v4-72b"},
		{"zai-org/arcee-ai/trinity-large-preview", "trinity-large-preview"},
		// Model org prefix without hosting platform
		{"anthracite-org/magnum-v4-72b", "magnum-v4-72b"},
		// Deep nesting
		{"host/org/sub/magnum-v4-72b", "magnum-v4-72b"},
	}
	for _, tt := range tests {
		got := normalizeBaseModel(tt.input)
		if got != tt.want {
			t.Errorf("normalizeBaseModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeBaseModel_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"GLM-5.1", "glm-5.1"},
		{"glm-5.1", "glm-5.1"},
		{"zai-org/GLM-5.1", "glm-5.1"},
		{"wafer.ai/GLM-5.1", "glm-5.1"},
		{"openai/GPT-4o", "gpt-4o"},
		{"GPT-4o", "gpt-4o"},
		{"meta-llama/Llama-3-70B", "llama-3-70b"},
		{"zai-org/Anthracite-Org/Magnum-V4-72b", "magnum-v4-72b"},
	}
	for _, tt := range tests {
		got := normalizeBaseModel(tt.input)
		if got != tt.want {
			t.Errorf("normalizeBaseModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeBaseModel_EdgeCases(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"openai/", ""},
		{"/", ""},
		{"/model", "model"},
	}
	for _, tt := range tests {
		got := normalizeBaseModel(tt.input)
		if got != tt.want {
			t.Errorf("normalizeBaseModel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
