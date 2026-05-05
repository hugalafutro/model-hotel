package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// buildProviderTargetURL
// ---------------------------------------------------------------------------

func TestBuildProviderTargetURL(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		providerType string
		want         string
	}{
		{
			name:         "anthropic basic",
			baseURL:      "https://api.anthropic.com",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "anthropic trailing slash",
			baseURL:      "https://api.anthropic.com/",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "anthropic already /v1",
			baseURL:      "https://api.anthropic.com/v1",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "anthropic already /v1/",
			baseURL:      "https://api.anthropic.com/v1/",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "openai standard",
			baseURL:      "https://api.openai.com/v1",
			providerType: "openai",
			want:         "https://api.openai.com/v1/chat/completions",
		},
		{
			name:         "google provider",
			baseURL:      "https://generativelanguage.googleapis.com/v1beta/openai",
			providerType: "google",
			want:         "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
		},
		{
			name:         "cohere provider",
			baseURL:      "https://api.cohere.ai/compatibility/v1",
			providerType: "cohere",
			want:         "https://api.cohere.ai/compatibility/v1/chat/completions",
		},
		{
			name:         "empty providerType",
			baseURL:      "https://example.com/v1",
			providerType: "",
			want:         "https://example.com/v1/chat/completions",
		},
		{
			name:         "deepseek provider",
			baseURL:      "https://api.deepseek.com",
			providerType: "deepseek",
			want:         "https://api.deepseek.com/chat/completions",
		},
		{
			name:         "xai provider",
			baseURL:      "https://api.x.ai",
			providerType: "xai",
			want:         "https://api.x.ai/chat/completions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildProviderTargetURL(tt.baseURL, tt.providerType)
			if got != tt.want {
				t.Errorf("buildProviderTargetURL(%q, %q) = %q, want %q", tt.baseURL, tt.providerType, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// setProviderAuthHeaders
// ---------------------------------------------------------------------------

func TestSetProviderAuthHeaders_EmptyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	setProviderAuthHeaders(req, "anthropic", "")
	if req.Header.Get("x-api-key") != "" {
		t.Error("expected no x-api-key header for empty key")
	}
	if req.Header.Get("anthropic-version") != "" {
		t.Error("expected no anthropic-version header for empty key")
	}
	if req.Header.Get("Authorization") != "" {
		t.Error("expected no Authorization header for empty key")
	}
}

func TestSetProviderAuthHeaders_Anthropic(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	setProviderAuthHeaders(req, "anthropic", "sk-test-key")
	if v := req.Header.Get("x-api-key"); v != "sk-test-key" {
		t.Errorf("x-api-key = %q, want %q", v, "sk-test-key")
	}
	if v := req.Header.Get("anthropic-version"); v != "2023-06-01" {
		t.Errorf("anthropic-version = %q, want %q", v, "2023-06-01")
	}
	if v := req.Header.Get("Authorization"); v != "" {
		t.Errorf("Authorization should not be set for anthropic, got %q", v)
	}
}

func TestSetProviderAuthHeaders_OpenAI(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	setProviderAuthHeaders(req, "openai", "sk-test-key")
	if v := req.Header.Get("Authorization"); v != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer sk-test-key")
	}
	if v := req.Header.Get("x-api-key"); v != "" {
		t.Errorf("x-api-key should not be set for openai, got %q", v)
	}
}

func TestSetProviderAuthHeaders_Google(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	setProviderAuthHeaders(req, "google", "test-key")
	if v := req.Header.Get("Authorization"); v != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer test-key")
	}
}

func TestSetProviderAuthHeaders_EmptyProvider(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	setProviderAuthHeaders(req, "", "key")
	if v := req.Header.Get("Authorization"); v != "Bearer key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer key")
	}
}
