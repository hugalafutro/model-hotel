package provider

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// LegacyTypeFromURL
// ---------------------------------------------------------------------------

func TestLegacyTypeFromURL_KimiCode(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"coding endpoint", "https://api.kimi.com/coding/v1"},
		{"bare host", "https://api.kimi.com"},
		{"subdomain", "https://gateway.kimi.com/coding/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "kimi-code" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "kimi-code")
			}
		})
	}
}

func TestLegacyTypeFromURL_NanoGPT(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exact host", "https://api.nano-gpt.com/v1"},
		{"bare domain", "https://nano-gpt.com/v1"},
		{"subdomain", "https://custom.nano-gpt.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "nanogpt" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "nanogpt")
			}
		})
	}
}

func TestLegacyTypeFromURL_ZAICoding(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exact host", "https://api.z.ai/v1"},
		{"bare domain", "https://z.ai/v1"},
		{"subdomain", "https://proxy.z.ai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "zai-coding" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "zai-coding")
			}
		})
	}
}

func TestLegacyTypeFromURL_DeepSeek(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exact host", "https://api.deepseek.com/v1"},
		{"bare domain", "https://deepseek.com/v1"},
		{"subdomain", "https://custom.deepseek.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "deepseek" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "deepseek")
			}
		})
	}
}

func TestLegacyTypeFromURL_OpenCodeZen(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"zen path", "https://opencode.ai/zen/v1"},
		{"zen subdomain with path", "https://custom.opencode.ai/zen/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "opencode-zen" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "opencode-zen")
			}
		})
	}
}

func TestLegacyTypeFromURL_OpenCodeGo(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"go path", "https://opencode.ai/zen/go/v1"},
		{"go subdomain with path", "https://custom.opencode.ai/zen/go/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "opencode-go" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "opencode-go")
			}
		})
	}
}

func TestLegacyTypeFromURL_OpenCodeGoBeforeZen(t *testing.T) {
	// /zen/go/ should match opencode-go, not opencode-zen
	result := LegacyTypeFromURL("https://opencode.ai/zen/go/v1")
	if result != "opencode-go" {
		t.Errorf("LegacyTypeFromURL('/zen/go/') should be opencode-go, got %q", result)
	}
}

func TestLegacyTypeFromURL_EmptyString(t *testing.T) {
	result := LegacyTypeFromURL("")
	if result != "openai" {
		t.Errorf("LegacyTypeFromURL('') = %q, want %q (fallback)", result, "openai")
	}
}

func TestLegacyTypeFromURL_InvalidURL(t *testing.T) {
	result := LegacyTypeFromURL("://not-a-valid-url")
	if result != "openai" {
		t.Errorf("LegacyTypeFromURL('://invalid') = %q, want %q (fallback)", result, "openai")
	}
}

func TestLegacyTypeFromURL_CaseInsensitive(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"uppercase OpenAI", "https://API.OPENAI.COM/v1", "openai"},
		{"mixed case DeepSeek", "https://API.DeepSeek.COM/v1", "deepseek"},
		{"uppercase Anthropic", "HTTPS://API.ANTHROPIC.COM/v1", "anthropic"},
		{"localhost caps", "HTTP://LOCALHOST:3000/v1", "openai"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != tc.expected {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, tc.expected)
			}
		})
	}
}

func TestLegacyTypeFromURL_Whitespace(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"leading space", " https://api.openai.com/v1"},
		{"trailing space", "https://api.openai.com/v1 "},
		{"leading tab", "\thttps://api.openai.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			// Should still detect correctly after trimming
			if result != "openai" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "openai")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LegacyTypeFromURL - Additional Provider Types
// ---------------------------------------------------------------------------

func TestLegacyTypeFromURL_XAI(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"standard x.ai", "https://api.x.ai/v1"},
		{"custom x.ai subdomain", "https://custom.x.ai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != "xai" {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, "xai")
			}
		})
	}
}

func TestLegacyTypeFromURL_DeepSeekSubdomain(t *testing.T) {
	result := LegacyTypeFromURL("https://api.custom.deepseek.com/v1")
	if result != "deepseek" {
		t.Errorf("LegacyTypeFromURL('https://api.custom.deepseek.com/v1') = %q, want %q", result, "deepseek")
	}
}

func TestLegacyTypeFromURL_NanoGPTSubdomain(t *testing.T) {
	result := LegacyTypeFromURL("https://custom.nano-gpt.com/v1")
	if result != "nanogpt" {
		t.Errorf("LegacyTypeFromURL('https://custom.nano-gpt.com/v1') = %q, want %q", result, "nanogpt")
	}
}

func TestLegacyTypeFromURL_OpenRouterSubdomain(t *testing.T) {
	result := LegacyTypeFromURL("https://custom.openrouter.ai/v1")
	if result != "openrouter" {
		t.Errorf("LegacyTypeFromURL('https://custom.openrouter.ai/v1') = %q, want %q", result, "openrouter")
	}
}

func TestLegacyTypeFromURL_OpenCodeZenSubdomain(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"zen path subdomain", "https://custom.opencode.ai/zen/v1"},
		{"zen go path subdomain", "https://custom.opencode.ai/zen/go/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			expected := "opencode-zen"
			if strings.Contains(tc.url, "/zen/go") {
				expected = "opencode-go"
			}
			if result != expected {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, expected)
			}
		})
	}
}

// The default-port rules survive in the legacy derivation because rows created
// under them are backfilled with it. New providers carry the type the operator
// picked, so a self-hosted server is no longer tied to a port.
func TestLegacyTypeFromURL_LocalhostWithPorts(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"localhost ollama", "http://localhost:11434/v1", "ollama"},
		{"localhost koboldcpp", "http://localhost:5001/v1", "koboldcpp"},
		{"localhost lmstudio", "http://localhost:1234/v1", "lmstudio"},
		{"127.0.0.1 ollama", "http://127.0.0.1:11434/v1", "ollama"},
		{"ipv6 ollama", "http://[::1]:11434/v1", "ollama"},
		{"localhost unknown port", "http://localhost:9999/v1", "openai"},
		// Non-localhost hosts: port detection should still work (LAN, K8s, etc.)
		{"LAN ollama", "http://192.168.1.50:11434/v1", "ollama"},
		{"LAN koboldcpp", "http://192.168.1.50:5001/v1", "koboldcpp"},
		{"LAN lmstudio", "http://10.0.0.5:1234/v1", "lmstudio"},
		{"hostname ollama", "http://my-llm-server:11434/v1", "ollama"},
		// Non-localhost host with unrecognised port must fall back to openai
		{"LAN unknown port", "http://192.168.1.50:9999/v1", "openai"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := LegacyTypeFromURL(tc.url)
			if result != tc.expected {
				t.Errorf("LegacyTypeFromURL(%q) = %q, want %q", tc.url, result, tc.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// NormalizeName
// ---------------------------------------------------------------------------

func TestNormalizeName_SpacesToHyphens(t *testing.T) {
	result := NormalizeName("My Provider")
	if result != "My-Provider" {
		t.Errorf("NormalizeName(%q) = %q, want %q", "My Provider", result, "My-Provider")
	}
}

func TestNormalizeName_NoSpaces(t *testing.T) {
	result := NormalizeName("openai")
	if result != "openai" {
		t.Errorf("NormalizeName(%q) = %q, want %q", "openai", result, "openai")
	}
}

func TestNormalizeName_MultipleSpaces(t *testing.T) {
	result := NormalizeName("My Cool Provider")
	if result != "My-Cool-Provider" {
		t.Errorf("NormalizeName(%q) = %q, want %q", "My Cool Provider", result, "My-Cool-Provider")
	}
}

func TestNormalizeName_EmptyString(t *testing.T) {
	result := NormalizeName("")
	if result != "" {
		t.Errorf("NormalizeName('') = %q, want %q", result, "")
	}
}

func TestNormalizeName_AlreadyHasHyphens(t *testing.T) {
	result := NormalizeName("my-provider")
	if result != "my-provider" {
		t.Errorf("NormalizeName(%q) = %q, want %q", "my-provider", result, "my-provider")
	}
}

func TestNormalizeName_MixedSpacesAndHyphens(t *testing.T) {
	result := NormalizeName("My Cool-Provider")
	if result != "My-Cool-Provider" {
		t.Errorf("NormalizeName(%q) = %q, want %q", "My Cool-Provider", result, "My-Cool-Provider")
	}
}

// ---------------------------------------------------------------------------
// MaskAPIKey
// ---------------------------------------------------------------------------

func TestMaskAPIKey(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"sk-abcdefghijklmnop1234567890", "sk...7890"},
		{"sk-proj-abc123def456ghi789", "sk...i789"},
		// Anthropic keys all end in AA; the four-character tail still tells
		// two of them apart.
		{"sk-ant-api03-xxxxxxxxxxxxxxxxxxxxxxxxxxxx-hQAA", "sk...hQAA"},
		{"sk-ant-api03-xxxxxxxxxxxxxxxxxxxxxxxxxxxx-8QAA", "sk...8QAA"},
		// Thirteen characters is the shortest key that keeps more than half
		// of itself hidden; anything shorter shows nothing.
		{"abcdefghijklm", "ab...jklm"},
		{"abcdefghijkl", "***"},
		{"abcd", "***"},
		{"ab", "***"},
		{"a", "***"},
		{"", "***"},
	} {
		if got := MaskAPIKey(tc.key); got != tc.want {
			t.Errorf("MaskAPIKey(%q) = %q, want %q", tc.key, got, tc.want)
		}
		if tc.want != "***" && (len(tc.want) >= len(tc.key) || strings.Contains(tc.key[2:len(tc.key)-4], tc.want[5:])) {
			t.Errorf("MaskAPIKey(%q) = %q reveals more than the ends", tc.key, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// ToResponse
// ---------------------------------------------------------------------------

func TestToResponse_WithMaskedKey(t *testing.T) {
	masked := "sk...90"
	p := &Provider{
		ID:                   uuid.New(),
		Name:                 "test-provider",
		BaseURL:              "https://api.test.com/v1",
		EncryptedKey:         []byte("encrypted-data"),
		MaskedKey:            &masked,
		Enabled:              true,
		AutodiscoveryEnabled: true,
		CreatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.ID != p.ID {
		t.Errorf("ID mismatch: got %v, want %v", resp.ID, p.ID)
	}
	if resp.Name != p.Name {
		t.Errorf("Name mismatch: got %q, want %q", resp.Name, p.Name)
	}
	if resp.BaseURL != p.BaseURL {
		t.Errorf("BaseURL mismatch: got %q, want %q", resp.BaseURL, p.BaseURL)
	}
	if resp.MaskedKey != masked {
		t.Errorf("MaskedKey mismatch: got %q, want %q", resp.MaskedKey, masked)
	}
	if resp.Enabled != p.Enabled {
		t.Errorf("Enabled mismatch: got %v, want %v", resp.Enabled, p.Enabled)
	}
	if resp.AutodiscoveryEnabled != p.AutodiscoveryEnabled {
		t.Errorf("AutodiscoveryEnabled mismatch: got %v, want %v", resp.AutodiscoveryEnabled, p.AutodiscoveryEnabled)
	}
}

func TestToResponse_KeylessProvider(t *testing.T) {
	p := &Provider{
		ID:                   uuid.New(),
		Name:                 "keyless-provider",
		BaseURL:              "https://opencode.ai/zen",
		EncryptedKey:         nil,
		MaskedKey:            nil,
		Enabled:              true,
		AutodiscoveryEnabled: true,
		CreatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.MaskedKey != "N/A" {
		t.Errorf("keyless provider MaskedKey should be 'N/A', got %q", resp.MaskedKey)
	}
	if resp.AutodiscoveryEnabled != p.AutodiscoveryEnabled {
		t.Errorf("AutodiscoveryEnabled mismatch: got %v, want %v", resp.AutodiscoveryEnabled, p.AutodiscoveryEnabled)
	}
}

func TestToResponse_KeylessWithEmptyEncryptedKey(t *testing.T) {
	p := &Provider{
		ID:                   uuid.New(),
		Name:                 "keyless-provider",
		BaseURL:              "https://opencode.ai/zen",
		EncryptedKey:         []byte{},
		MaskedKey:            nil,
		Enabled:              true,
		AutodiscoveryEnabled: true,
		CreatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.MaskedKey != "N/A" {
		t.Errorf("keyless provider with empty EncryptedKey should have MaskedKey 'N/A', got %q", resp.MaskedKey)
	}
	if resp.AutodiscoveryEnabled != p.AutodiscoveryEnabled {
		t.Errorf("AutodiscoveryEnabled mismatch: got %v, want %v", resp.AutodiscoveryEnabled, p.AutodiscoveryEnabled)
	}
}

func TestToResponse_NilMaskedKeyButHasEncryptedKey(t *testing.T) {
	p := &Provider{
		ID:                   uuid.New(),
		Name:                 "test-provider",
		BaseURL:              "https://api.test.com/v1",
		EncryptedKey:         []byte("some-encrypted-data"),
		MaskedKey:            nil,
		Enabled:              true,
		AutodiscoveryEnabled: true,
		CreatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.MaskedKey != "***" {
		t.Errorf("encrypted key with nil MaskedKey should show '***', got %q", resp.MaskedKey)
	}
	if resp.AutodiscoveryEnabled != p.AutodiscoveryEnabled {
		t.Errorf("AutodiscoveryEnabled mismatch: got %v, want %v", resp.AutodiscoveryEnabled, p.AutodiscoveryEnabled)
	}
}

func TestToResponse_EmptyStringMaskedKey(t *testing.T) {
	emptyMasked := ""
	p := &Provider{
		ID:                   uuid.New(),
		Name:                 "test-provider",
		BaseURL:              "https://api.test.com/v1",
		EncryptedKey:         []byte("encrypted"),
		MaskedKey:            &emptyMasked,
		Enabled:              true,
		AutodiscoveryEnabled: true,
		CreatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:            mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.MaskedKey != "***" {
		t.Errorf("empty MaskedKey with encrypted key should show '***', got %q", resp.MaskedKey)
	}
	if resp.AutodiscoveryEnabled != p.AutodiscoveryEnabled {
		t.Errorf("AutodiscoveryEnabled mismatch: got %v, want %v", resp.AutodiscoveryEnabled, p.AutodiscoveryEnabled)
	}
}

// ---------------------------------------------------------------------------
// Provider Cache
// ---------------------------------------------------------------------------

func TestDiscoverModels_EmptyBaseURL(t *testing.T) {
	svc := NewDiscoveryService(nil, nil)
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "empty-url-provider",
		BaseURL:      "",
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	_, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err == nil {
		t.Error("DiscoverModels with empty BaseURL should return error")
	}
}

func TestDiscoverModels_InvalidBaseURL(t *testing.T) {
	svc := NewDiscoveryService(nil, nil)
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "invalid-url-provider",
		BaseURL:      "://not-a-valid-url",
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	_, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err == nil {
		t.Error("DiscoverModels with invalid BaseURL should return error")
	}
}

func TestDiscoverModels_KeylessProviderWithEmptyKey(t *testing.T) {
	// Test that keyless providers (like opencode-zen) with empty encrypted key succeed
	mockResponse := `{
		"data": [
			{
				"id": "test-model",
				"object": "model",
				"owned_by": "test",
				"created": 1700000000
			}
		],
		"object": "list"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "keyless-provider",
		BaseURL:      server.URL,
		EncryptedKey: []byte{}, // Empty key for keyless provider
	}

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels for keyless provider should succeed, got error: %v", err)
	}
	// Live "test-model" (not in catalog) is first, backfilled from the catalog (no union).
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "test-model" {
		t.Errorf("expected model ID 'test-model', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverModels_UnknownProviderType(t *testing.T) {
	// Test with a provider type that doesn't match any special case - should fall back to OpenAI
	mockResponse := `{
		"data": [
			{
				"id": "fallback-model",
				"object": "model",
				"owned_by": "test"
			}
		]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "unknown-type-provider",
		BaseURL:      server.URL + "/v1",
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels with unknown provider type should fall back to OpenAI, got error: %v", err)
	}
	// Single live model unioned with the OpenAI catalog.
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func TestIsTransientNetworkError_NilError(t *testing.T) {
	if isTransientNetworkError(nil) {
		t.Error("isTransientNetworkError(nil) should be false")
	}
}

func TestIsTransientNetworkError_DNSError(t *testing.T) {
	dnsErr := &net.DNSError{IsNotFound: true}
	wrapped := fmt.Errorf("wrapped: %w", dnsErr)
	if !isTransientNetworkError(wrapped) {
		t.Error("isTransientNetworkError(DNSError) should be true")
	}
}

func TestIsTransientNetworkError_NetErrorTimeout(t *testing.T) {
	if !isTransientNetworkError(timeoutError{}) {
		t.Error("isTransientNetworkError(net.Error with Timeout=true) should be true")
	}
}

func TestIsTransientNetworkError_NetErrorNoTimeout(t *testing.T) {
	if isTransientNetworkError(noTimeoutError{}) {
		t.Error("isTransientNetworkError(net.Error with Timeout=false) should be false")
	}
}

func TestIsTransientNetworkError_OpError(t *testing.T) {
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: io.EOF}
	if !isTransientNetworkError(opErr) {
		t.Error("isTransientNetworkError(OpError) should be true")
	}
}

func TestIsTransientNetworkError_URLErrorWrappingTransient(t *testing.T) {
	dnsErr := &net.DNSError{IsNotFound: true}
	urlErr := &url.Error{Op: "Get", URL: "http://example.com", Err: dnsErr}
	if !isTransientNetworkError(urlErr) {
		t.Error("isTransientNetworkError(url.Error wrapping DNSError) should be true")
	}
}

func TestIsTransientNetworkError_URLErrorWrappingNonTransient(t *testing.T) {
	urlErr := &url.Error{Op: "Get", URL: "http://example.com", Err: io.EOF}
	if isTransientNetworkError(urlErr) {
		t.Error("isTransientNetworkError(url.Error wrapping io.EOF) should be false")
	}
}

func TestIsTransientNetworkError_OtherError(t *testing.T) {
	if isTransientNetworkError(io.EOF) {
		t.Error("isTransientNetworkError(io.EOF) should be false")
	}
}

func TestIsTransientNetworkError_URLErrorWrappingTimeout(t *testing.T) {
	urlErr := &url.Error{Op: "Get", URL: "http://example.com", Err: timeoutError{}}
	if !isTransientNetworkError(urlErr) {
		t.Error("isTransientNetworkError(url.Error wrapping timeout net.Error) should be true")
	}
}

// ---------------------------------------------------------------------------
// isRetryableStatus
// ---------------------------------------------------------------------------

func TestIsRetryableStatus(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		expected bool
	}{
		{"429 Too Many Requests", 429, true},
		{"500 Internal Server Error", 500, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
		{"200 OK", 200, false},
		{"401 Unauthorized", 401, false},
		{"403 Forbidden", 403, false},
		{"404 Not Found", 404, false},
		{"400 Bad Request", 400, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableStatus(tt.code); got != tt.expected {
				t.Errorf("isRetryableStatus(%d) = %v, want %v", tt.code, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// retryBackoff
// ---------------------------------------------------------------------------

func TestRetryBackoff(t *testing.T) {
	base := 3 * time.Second

	// Attempt 0 should return just jitter (delay=0 + jitter in [0, base))
	b0 := retryBackoff(base, 0)
	if b0 < 0 || b0 >= base {
		t.Errorf("retryBackoff(base, 0) = %v, want [0, %v)", b0, base)
	}

	// Attempt 1: delay=3s + jitter in [0, 3s) → [3s, 6s)
	b1 := retryBackoff(base, 1)
	if b1 < 3*time.Second || b1 >= 6*time.Second {
		t.Errorf("retryBackoff(base, 1) = %v, want [3s, 6s)", b1)
	}

	// Attempt 2: delay=6s + jitter in [0, 3s) → [6s, 9s)
	b2 := retryBackoff(base, 2)
	if b2 < 6*time.Second || b2 >= 9*time.Second {
		t.Errorf("retryBackoff(base, 2) = %v, want [6s, 9s)", b2)
	}
}

// ---------------------------------------------------------------------------
// quotaCircuitState
// ---------------------------------------------------------------------------

func TestDiscoverModels_NilProvider(t *testing.T) {
	svc := NewDiscoveryService(nil, nil)
	ctx := context.Background()

	// Should panic or error with nil provider
	defer func() {
		if r := recover(); r != nil {
			// Acceptable - nil provider causes panic when accessing fields
			t.Logf("DiscoverModels panicked with nil provider (acceptable): %v", r)
		}
	}()

	_, err := svc.DiscoverModels(ctx, nil, "test-master-key")
	if err == nil {
		t.Error("DiscoverModels with nil provider should return error or panic")
	}
}

func TestDiscoverModels_NetworkErrorPropagated(t *testing.T) {
	// Test that network errors from provider discovery are propagated
	svc := NewDiscoveryService(nil, nil)
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "unreachable-provider",
		BaseURL:      "http://localhost:1", // Port 1 is typically closed
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	_, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err == nil {
		t.Error("DiscoverModels should return error for unreachable host")
	}
}

func TestDiscoverModels_HTTPErrorPropagated(t *testing.T) {
	// Test that HTTP errors from provider are propagated
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return 401 Unauthorized for all requests
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "Invalid API key"}`))
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "auth-fail-provider",
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	_, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err == nil {
		t.Error("DiscoverModels should return error for 401 response")
	}
	if !strings.Contains(err.Error(), "unexpected status") {
		t.Errorf("expected 'unexpected status' error, got: %v", err)
	}
}

func TestDiscoverModels_ContextCancellation(t *testing.T) {
	// Test that context cancellation is respected
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": []}`))
	}))
	defer server.Close()

	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "slow-provider",
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err == nil {
		t.Error("DiscoverModels should return error when context is cancelled")
	}
}

// ===========================================================================
// Tests moved from discovery_coverage_test.go
// ===========================================================================

// =============================================================================
// Anthropic Discovery Tests
// =============================================================================
