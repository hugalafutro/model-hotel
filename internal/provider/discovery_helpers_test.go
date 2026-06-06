package provider

import "testing"

func TestToCohereNativeURL_CompatEndpoint(t *testing.T) {
	got := toCohereNativeURL("https://api.cohere.ai/v1")
	want := "https://api.cohere.com"
	if got != want {
		t.Errorf("toCohereNativeURL(%q) = %q, want %q", "https://api.cohere.ai/v1", got, want)
	}
}

func TestToCohereNativeURL_AlreadyNative(t *testing.T) {
	got := toCohereNativeURL("https://api.cohere.com")
	if got != "https://api.cohere.com" {
		t.Errorf("Expected native URL unchanged, got %q", got)
	}
}

func TestToCohereNativeURL_CustomURL(t *testing.T) {
	got := toCohereNativeURL("https://custom-cohere.example.com")
	if got != "https://custom-cohere.example.com" {
		t.Errorf("Expected custom URL unchanged, got %q", got)
	}
}

func TestToCohereNativeURL_TrailingSlash(t *testing.T) {
	got := toCohereNativeURL("https://api.cohere.ai/v1/")
	want := "https://api.cohere.com"
	if got != want {
		t.Errorf("toCohereNativeURL with trailing slash = %q, want %q", got, want)
	}
}

func TestParseOpenRouterPricing(t *testing.T) {
	tests := []struct {
		name     string
		pricing  OpenRouterPricing
		wantIn   float64
		wantOut  float64
	}{
		{
			"valid pricing",
			OpenRouterPricing{Prompt: "0.001", Completion: "0.002"},
			1000.0, 2000.0,
		},
		{
			"zero pricing",
			OpenRouterPricing{Prompt: "0", Completion: "0"},
			0.0, 0.0,
		},
		{
			"empty strings parse to zero",
			OpenRouterPricing{Prompt: "", Completion: ""},
			0.0, 0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in, out := parseOpenRouterPricing(tc.pricing)
			if in != tc.wantIn {
				t.Errorf("input price = %v, want %v", in, tc.wantIn)
			}
			if out != tc.wantOut {
				t.Errorf("output price = %v, want %v", out, tc.wantOut)
			}
		})
	}
}
