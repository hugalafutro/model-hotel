package proxy

import (
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

func TestSanitizeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "trailing slash",
			raw:  "https://api.example.com/",
			want: "https://api.example.com",
		},
		{
			name: "no trailing slash",
			raw:  "https://api.example.com",
			want: "https://api.example.com",
		},
		{
			name: "double trailing slash - only strips one",
			raw:  "https://api.example.com//",
			want: "https://api.example.com/",
		},
		{
			name: "empty string",
			raw:  "",
			want: "",
		},
		{
			name: "just slash",
			raw:  "/",
			want: "",
		},
		{
			name: "path with trailing slash",
			raw:  "https://api.example.com/v1/",
			want: "https://api.example.com/v1",
		},
		{
			name: "path without trailing slash",
			raw:  "https://api.example.com/v1",
			want: "https://api.example.com/v1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := util.SanitizeBaseURL(tc.raw)
			if got != tc.want {
				t.Errorf("SanitizeBaseURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestBuildProviderTargetURL_AnthropicEdgeCases(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		providerType string
		want         string
	}{
		{
			name:         "anthropic with /v1 and trailing slash",
			baseURL:      "https://api.anthropic.com/v1/",
			providerType: "anthropic",
			want:         "https://api.anthropic.com/v1/chat/completions",
		},
		{
			name:         "anthropic with double slash then v1",
			baseURL:      "https://api.anthropic.com//v1",
			providerType: "anthropic",
			want:         "https://api.anthropic.com//v1/chat/completions",
		},
		{
			name:         "anthropic empty baseURL",
			baseURL:      "",
			providerType: "anthropic",
			want:         "/v1/chat/completions",
		},
		{
			name:         "anthropic just slash",
			baseURL:      "/",
			providerType: "anthropic",
			want:         "/v1/chat/completions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildProviderTargetURL(tc.baseURL, tc.providerType)
			if got != tc.want {
				t.Errorf("buildProviderTargetURL(%q, %q) = %q, want %q", tc.baseURL, tc.providerType, got, tc.want)
			}
		})
	}
}

func TestBuildProviderTargetURL_VariousProviders(t *testing.T) {
	providers := []string{"openai", "google", "cohere", "xai", "unknown"}
	baseURL := "https://api.example.com"
	expected := "https://api.example.com/chat/completions"

	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			got := buildProviderTargetURL(baseURL, provider)
			if got != expected {
				t.Errorf("buildProviderTargetURL(%q, %q) = %q, want %q", baseURL, provider, got, expected)
			}
		})
	}
}

func TestBuildProviderTargetURL_HasSuffixCheck(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		providerType string
		description  string
	}{
		{
			name:         "anthropic with /v1 suffix",
			baseURL:      "https://api.anthropic.com/v1",
			providerType: "anthropic",
			description:  "should detect /v1 suffix and not double it",
		},
		{
			name:         "anthropic with /v1/ suffix (becomes /v1 after sanitize)",
			baseURL:      "https://api.anthropic.com/v1/",
			providerType: "anthropic",
			description:  "SanitizeBaseURL strips trailing slash, then HasSuffix matches /v1",
		},
		{
			name:         "anthropic without /v1 suffix",
			baseURL:      "https://api.anthropic.com",
			providerType: "anthropic",
			description:  "should add /v1 prefix",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sanitized := util.SanitizeBaseURL(tc.baseURL)
			hasV1Suffix := strings.HasSuffix(sanitized, "/v1")

			switch {
			case tc.name == "anthropic with /v1 suffix":
				if !hasV1Suffix {
					t.Errorf("expected sanitized URL %q to have /v1 suffix", sanitized)
				}
			case tc.name == "anthropic with /v1/ suffix (becomes /v1 after sanitize)":
				if !hasV1Suffix {
					t.Errorf("expected sanitized URL %q to have /v1 suffix after stripping trailing slash", sanitized)
				}
			case tc.name == "anthropic without /v1 suffix":
				if hasV1Suffix {
					t.Errorf("did not expect sanitized URL %q to have /v1 suffix", sanitized)
				}
			}

			got := buildProviderTargetURL(tc.baseURL, tc.providerType)
			if tc.providerType == "anthropic" && hasV1Suffix {
				if strings.Contains(got, "/v1/v1/") {
					t.Errorf("double /v1 detected in result: %q", got)
				}
			}
		})
	}
}
