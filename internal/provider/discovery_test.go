package provider

import (
	"context"
	"encoding/json"
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

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// ---------------------------------------------------------------------------
// DetectProviderType
// ---------------------------------------------------------------------------

func TestDetectProviderType_OpenAI(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"standard openai", "https://api.openai.com/v1"},
		{"openai with path", "https://api.openai.com/v1/chat/completions"},
		{"custom openai-compatible", "https://my-custom-llm.example.com/v1"},
		{"random domain", "https://some-random-host.io/api"},
		{"localhost default", "http://localhost:3000/v1"},
		{"127.0.0.1 default", "http://127.0.0.1:8000/v1"},
		{"ipv6 loopback default", "http://[::1]:4000/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != "openai" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "openai")
			}
		})
	}
}

func TestDetectProviderType_NanoGPT(t *testing.T) {
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
			result := DetectProviderType(tc.url)
			if result != "nanogpt" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "nanogpt")
			}
		})
	}
}

func TestDetectProviderType_ZAICoding(t *testing.T) {
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
			result := DetectProviderType(tc.url)
			if result != "zai-coding" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "zai-coding")
			}
		})
	}
}

func TestDetectProviderType_DeepSeek(t *testing.T) {
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
			result := DetectProviderType(tc.url)
			if result != "deepseek" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "deepseek")
			}
		})
	}
}

func TestDetectProviderType_Anthropic(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exact host", "https://api.anthropic.com/v1"},
		{"bare domain", "https://anthropic.com/v1"},
		{"subdomain", "https://custom.anthropic.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != "anthropic" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "anthropic")
			}
		})
	}
}

func TestDetectProviderType_OllamaCloud(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exact host", "https://ollama.com/api"},
		{"subdomain", "https://custom.ollama.com/api"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != "ollama-cloud" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "ollama-cloud")
			}
		})
	}
}

func TestDetectProviderType_OpenCodeZen(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"zen path", "https://opencode.ai/zen/v1"},
		{"zen subdomain with path", "https://custom.opencode.ai/zen/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != "opencode-zen" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "opencode-zen")
			}
		})
	}
}

func TestDetectProviderType_OpenCodeGo(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"go path", "https://opencode.ai/zen/go/v1"},
		{"go subdomain with path", "https://custom.opencode.ai/zen/go/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != "opencode-go" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "opencode-go")
			}
		})
	}
}

func TestDetectProviderType_OpenCodeGoBeforeZen(t *testing.T) {
	// /zen/go/ should match opencode-go, not opencode-zen
	result := DetectProviderType("https://opencode.ai/zen/go/v1")
	if result != "opencode-go" {
		t.Errorf("DetectProviderType('/zen/go/') should be opencode-go, got %q", result)
	}
}

func TestDetectProviderType_EmptyString(t *testing.T) {
	result := DetectProviderType("")
	if result != "openai" {
		t.Errorf("DetectProviderType('') = %q, want %q (fallback)", result, "openai")
	}
}

func TestDetectProviderType_InvalidURL(t *testing.T) {
	result := DetectProviderType("://not-a-valid-url")
	if result != "openai" {
		t.Errorf("DetectProviderType('://invalid') = %q, want %q (fallback)", result, "openai")
	}
}

func TestDetectProviderType_CaseInsensitive(t *testing.T) {
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
			result := DetectProviderType(tc.url)
			if result != tc.expected {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, tc.expected)
			}
		})
	}
}

func TestDetectProviderType_Whitespace(t *testing.T) {
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
			result := DetectProviderType(tc.url)
			// Should still detect correctly after trimming
			if result != "openai" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "openai")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DetectProviderType - Additional Provider Types
// ---------------------------------------------------------------------------

func TestDetectProviderType_Cohere(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"standard cohere.com", "https://api.cohere.com/v1"},
		{"standard cohere.ai", "https://api.cohere.ai/v1"},
		{"custom cohere.com subdomain", "https://custom.cohere.com/v1"},
		{"custom cohere.ai subdomain", "https://custom.cohere.ai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != "cohere" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "cohere")
			}
		})
	}
}

func TestDetectProviderType_XAI(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"standard x.ai", "https://api.x.ai/v1"},
		{"custom x.ai subdomain", "https://custom.x.ai/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != "xai" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "xai")
			}
		})
	}
}

func TestDetectProviderType_Google(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"generativelanguage v1beta", "https://generativelanguage.googleapis.com/v1beta"},
		{"aiplatform v1", "https://aiplatform.googleapis.com/v1"},
		{"generativelanguage custom subdomain", "https://custom-generativelanguage.googleapis.com/v1"},
		{"aiplatform custom subdomain", "https://custom-aiplatform.googleapis.com/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != "google" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "google")
			}
		})
	}
}

func TestDetectProviderType_DeepSeekSubdomain(t *testing.T) {
	result := DetectProviderType("https://api.custom.deepseek.com/v1")
	if result != "deepseek" {
		t.Errorf("DetectProviderType('https://api.custom.deepseek.com/v1') = %q, want %q", result, "deepseek")
	}
}

func TestDetectProviderType_NanoGPTSubdomain(t *testing.T) {
	result := DetectProviderType("https://custom.nano-gpt.com/v1")
	if result != "nanogpt" {
		t.Errorf("DetectProviderType('https://custom.nano-gpt.com/v1') = %q, want %q", result, "nanogpt")
	}
}

func TestDetectProviderType_OpenRouterSubdomain(t *testing.T) {
	result := DetectProviderType("https://custom.openrouter.ai/v1")
	if result != "openrouter" {
		t.Errorf("DetectProviderType('https://custom.openrouter.ai/v1') = %q, want %q", result, "openrouter")
	}
}

func TestDetectProviderType_OllamaCloudSubdomain(t *testing.T) {
	result := DetectProviderType("https://custom.ollama.com/v1")
	if result != "ollama-cloud" {
		t.Errorf("DetectProviderType('https://custom.ollama.com/v1') = %q, want %q", result, "ollama-cloud")
	}
}

func TestDetectProviderType_OpenCodeZenSubdomain(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"zen path subdomain", "https://custom.opencode.ai/zen/v1"},
		{"zen go path subdomain", "https://custom.opencode.ai/zen/go/v1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			expected := "opencode-zen"
			if strings.Contains(tc.url, "/zen/go") {
				expected = "opencode-go"
			}
			if result != expected {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, expected)
			}
		})
	}
}

func TestDetectProviderType_LocalhostWithPorts(t *testing.T) {
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
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := DetectProviderType(tc.url)
			if result != tc.expected {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, tc.expected)
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

func TestMaskAPIKey_LongKey(t *testing.T) {
	result := MaskAPIKey("sk-abcdefghijklmnop1234567890")
	if result != "sk...90" {
		t.Errorf("MaskAPIKey(long key) = %q, want %q", result, "sk...90")
	}
}

func TestMaskAPIKey_ShortKey(t *testing.T) {
	// Keys ≤ 4 chars are masked entirely to "***"
	result := MaskAPIKey("abcd")
	if result != "***" {
		t.Errorf("MaskAPIKey(4 chars) = %q, want %q", result, "***")
	}
}

func TestMaskAPIKey_ThreeChars(t *testing.T) {
	result := MaskAPIKey("abc")
	if result != "***" {
		t.Errorf("MaskAPIKey(3 chars) = %q, want %q", result, "***")
	}
}

func TestMaskAPIKey_TwoChars(t *testing.T) {
	result := MaskAPIKey("ab")
	if result != "***" {
		t.Errorf("MaskAPIKey(2 chars) = %q, want %q", result, "***")
	}
}

func TestMaskAPIKey_OneChar(t *testing.T) {
	result := MaskAPIKey("x")
	if result != "***" {
		t.Errorf("MaskAPIKey(1 char) = %q, want %q", result, "***")
	}
}

func TestMaskAPIKey_EmptyString(t *testing.T) {
	result := MaskAPIKey("")
	if result != "***" {
		t.Errorf("MaskAPIKey('') = %q, want %q", result, "***")
	}
}

func TestMaskAPIKey_FiveChars(t *testing.T) {
	// Keys > 4 chars show first 2 and last 2 chars
	result := MaskAPIKey("abcde")
	if result != "ab...de" {
		t.Errorf("MaskAPIKey(5 chars) = %q, want %q", result, "ab...de")
	}
}

func TestMaskAPIKey_DoesNotRevealMiddle(t *testing.T) {
	key := "sk-proj-abc123def456ghi789"
	result := MaskAPIKey(key)
	if result == key {
		t.Error("MaskAPIKey should not return the full key")
	}
	if len(result) >= len(key) {
		t.Error("MaskAPIKey result should be shorter than the original key")
	}
	// Should start with first 2 chars and end with last 2
	if result[:2] != "sk" {
		t.Errorf("MaskAPIKey should start with first 2 chars, got %q", result[:2])
	}
	if result[len(result)-2:] != "89" {
		t.Errorf("MaskAPIKey should end with last 2 chars, got %q", result[len(result)-2:])
	}
}

// ---------------------------------------------------------------------------
// ToResponse
// ---------------------------------------------------------------------------

func TestToResponse_WithMaskedKey(t *testing.T) {
	masked := "sk...90"
	p := &Provider{
		ID:           uuid.New(),
		Name:         "test-provider",
		BaseURL:      "https://api.test.com/v1",
		EncryptedKey: []byte("encrypted-data"),
		MaskedKey:    &masked,
		Enabled:      true,
		CreatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
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
}

func TestToResponse_KeylessProvider(t *testing.T) {
	p := &Provider{
		ID:           uuid.New(),
		Name:         "keyless-provider",
		BaseURL:      "https://opencode.ai/zen",
		EncryptedKey: nil,
		MaskedKey:    nil,
		Enabled:      true,
		CreatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.MaskedKey != "N/A" {
		t.Errorf("keyless provider MaskedKey should be 'N/A', got %q", resp.MaskedKey)
	}
}

func TestToResponse_KeylessWithEmptyEncryptedKey(t *testing.T) {
	p := &Provider{
		ID:           uuid.New(),
		Name:         "keyless-provider",
		BaseURL:      "https://opencode.ai/zen",
		EncryptedKey: []byte{},
		MaskedKey:    nil,
		Enabled:      true,
		CreatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.MaskedKey != "N/A" {
		t.Errorf("keyless provider with empty EncryptedKey should have MaskedKey 'N/A', got %q", resp.MaskedKey)
	}
}

func TestToResponse_NilMaskedKeyButHasEncryptedKey(t *testing.T) {
	p := &Provider{
		ID:           uuid.New(),
		Name:         "test-provider",
		BaseURL:      "https://api.test.com/v1",
		EncryptedKey: []byte("some-encrypted-data"),
		MaskedKey:    nil,
		Enabled:      true,
		CreatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.MaskedKey != "***" {
		t.Errorf("encrypted key with nil MaskedKey should show '***', got %q", resp.MaskedKey)
	}
}

func TestToResponse_EmptyStringMaskedKey(t *testing.T) {
	emptyMasked := ""
	p := &Provider{
		ID:           uuid.New(),
		Name:         "test-provider",
		BaseURL:      "https://api.test.com/v1",
		EncryptedKey: []byte("encrypted"),
		MaskedKey:    &emptyMasked,
		Enabled:      true,
		CreatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
		UpdatedAt:    mustParseTime("2024-01-01T00:00:00Z"),
	}

	resp := ToResponse(p)
	if resp.MaskedKey != "***" {
		t.Errorf("empty MaskedKey with encrypted key should show '***', got %q", resp.MaskedKey)
	}
}

// ---------------------------------------------------------------------------
// Provider Cache
// ---------------------------------------------------------------------------

func TestCacheProvider_NilProvider(t *testing.T) {
	// Should not panic
	cacheProvider(nil)

	// Verify nil provider was not cached
	testUUID := uuid.New()
	_, ok := GetCachedByID(testUUID)
	if ok {
		t.Error("GetCachedByID should return ok=false after cacheProvider(nil)")
	}
}

func TestCacheProvider_RoundTrip(t *testing.T) {
	// Clear cache
	InvalidateProviderCache()

	id := uuid.New()
	p := &Provider{
		ID:   id,
		Name: "cache-test-provider",
	}

	cacheProvider(p)

	// Should be retrievable by ID
	found, ok := GetCachedByID(id)
	if !ok {
		t.Fatal("GetCachedByID should find cached provider")
	}
	if found.ID != id {
		t.Errorf("GetCachedByID: expected ID %v, got %v", id, found.ID)
	}

	// Should be retrievable by Name
	found, ok = GetCachedByName("cache-test-provider")
	if !ok {
		t.Fatal("GetCachedByName should find cached provider")
	}
	if found.Name != "cache-test-provider" {
		t.Errorf("GetCachedByName: expected Name %q, got %q", "cache-test-provider", found.Name)
	}

	// Should be retrievable by normalized name
	found, ok = GetCachedByName("cache-test-provider")
	if !ok {
		t.Fatal("GetCachedByName should find cached provider via normalized name")
	}
	if found.ID != id {
		t.Errorf("GetCachedByName normalized: expected ID %v, got %v", id, found.ID)
	}
}

func TestCacheProvider_ExpiredEntry(t *testing.T) {
	InvalidateProviderCache()

	id := uuid.New()
	p := &Provider{
		ID:   id,
		Name: "expired-provider",
	}

	// Manually insert an expired entry
	providerCacheMu.Lock()
	providerByIDCache[id] = providerCacheEntry{
		provider:  p,
		expiresAt: mustParseTime("2020-01-01T00:00:00Z"), // expired
	}
	providerByNameCache["expired-provider"] = providerCacheEntry{
		provider:  p,
		expiresAt: mustParseTime("2020-01-01T00:00:00Z"),
	}
	providerCacheMu.Unlock()

	// Expired entries should not be found
	_, ok := GetCachedByID(id)
	if ok {
		t.Error("GetCachedByID should not return expired entry")
	}
	_, ok = GetCachedByName("expired-provider")
	if ok {
		t.Error("GetCachedByName should not return expired entry")
	}
}

func TestInvalidateProviderCache(t *testing.T) {
	id := uuid.New()
	p := &Provider{
		ID:   id,
		Name: "to-be-invalidated",
	}

	cacheProvider(p)

	// Should exist before invalidation
	_, ok := GetCachedByID(id)
	if !ok {
		t.Fatal("provider should be in cache before invalidation")
	}

	InvalidateProviderCache()

	// Should not exist after invalidation
	_, ok = GetCachedByID(id)
	if ok {
		t.Error("provider should not be in cache after invalidation")
	}
}

func TestWarmProviderCache(t *testing.T) {
	InvalidateProviderCache()

	providers := []*Provider{
		{ID: uuid.New(), Name: "warm-a"},
		{ID: uuid.New(), Name: "warm-b"},
		{ID: uuid.New(), Name: "warm-c"},
	}

	WarmProviderCache(providers)

	for _, p := range providers {
		found, ok := GetCachedByID(p.ID)
		if !ok {
			t.Errorf("provider %s should be in cache after WarmProviderCache", p.Name)
		}
		if found.Name != p.Name {
			t.Errorf("cached provider name mismatch: got %q, want %q", found.Name, p.Name)
		}
	}
}

func TestNormalizeName_RoundTripWithCache(t *testing.T) {
	InvalidateProviderCache()

	// Provider with spaces in name
	p := &Provider{
		ID:   uuid.New(),
		Name: "My Provider",
	}
	cacheProvider(p)

	// Should be findable by normalized name (spaces → hyphens)
	normalized := NormalizeName("My Provider")
	found, ok := GetCachedByName(normalized)
	if !ok {
		t.Errorf("GetCachedByName(%q) should find provider cached under name %q", normalized, p.Name)
	}
	if found.ID != p.ID {
		t.Errorf("wrong provider found via normalized name")
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels
// ---------------------------------------------------------------------------

func TestDiscoverModels_EmptyBaseURL(t *testing.T) {
	svc := NewDiscoveryService()
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
	svc := NewDiscoveryService()
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
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// ---------------------------------------------------------------------------
// isTransientNetworkError
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

// timeoutError implements net.Error with Timeout()=true
type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

func TestIsTransientNetworkError_NetErrorTimeout(t *testing.T) {
	if !isTransientNetworkError(timeoutError{}) {
		t.Error("isTransientNetworkError(net.Error with Timeout=true) should be true")
	}
}

// noTimeoutError implements net.Error with Timeout()=false
type noTimeoutError struct{}

func (noTimeoutError) Error() string   { return "not a timeout" }
func (noTimeoutError) Timeout() bool   { return false }
func (noTimeoutError) Temporary() bool { return false }

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

func TestQuotaCircuitState_ClosedByDefault(t *testing.T) {
	s := &quotaCircuitState{}
	if s.isCircuitOpen() {
		t.Error("new circuit should be closed")
	}
}

func TestQuotaCircuitState_OpensAfterThreshold(t *testing.T) {
	s := &quotaCircuitState{}
	for i := 0; i < quotaBreakerThreshold-1; i++ {
		if s.recordFailure() {
			t.Errorf("circuit should not open at failure %d (threshold=%d)", i+1, quotaBreakerThreshold)
		}
	}
	// The threshold-th failure should open the circuit.
	if !s.recordFailure() {
		t.Error("circuit should open on threshold-th failure")
	}
	if !s.isCircuitOpen() {
		t.Error("circuit should be open after reaching threshold")
	}
}

func TestQuotaCircuitState_SuccessResets(t *testing.T) {
	s := &quotaCircuitState{}
	// Fail a few times (not enough to open).
	for i := 0; i < quotaBreakerThreshold-1; i++ {
		s.recordFailure()
	}
	s.recordSuccess()
	// consecFailures reset to 0, so threshold more failures needed.
	for i := 0; i < quotaBreakerThreshold-1; i++ {
		if s.isCircuitOpen() {
			t.Error("circuit should not be open yet")
		}
		s.recordFailure()
	}
	// One more should open it.
	if !s.recordFailure() {
		t.Error("circuit should open after threshold failures post-reset")
	}
}

func TestQuotaCircuitState_HalfOpenAfterReset(t *testing.T) {
	s := &quotaCircuitState{}
	// Open the circuit.
	for i := 0; i < quotaBreakerThreshold; i++ {
		s.recordFailure()
	}
	if !s.isCircuitOpen() {
		t.Fatal("circuit should be open")
	}
	// Manually set openUntil to the past to simulate expiry.
	s.mu.Lock()
	s.openUntil = time.Now().Add(-1 * time.Second)
	s.mu.Unlock()
	// isCircuitOpen should transition to half-open (returns false).
	if s.isCircuitOpen() {
		t.Error("expired circuit should transition to half-open (return false)")
	}
	// A success should fully close it.
	s.recordSuccess()
	if s.isCircuitOpen() {
		t.Error("circuit should be closed after success")
	}
}

func TestQuotaCircuitState_HalfOpenFailureReopens(t *testing.T) {
	s := &quotaCircuitState{}
	// Open the circuit.
	for i := 0; i < quotaBreakerThreshold; i++ {
		s.recordFailure()
	}
	// Expire the open window.
	s.mu.Lock()
	s.openUntil = time.Now().Add(-1 * time.Second)
	s.mu.Unlock()
	// Transition to half-open.
	s.isCircuitOpen()
	// A failure should re-open the circuit immediately.
	s.recordFailure()
	if !s.isCircuitOpen() {
		t.Error("circuit should re-open after failure in half-open state")
	}
}

// ---------------------------------------------------------------------------
// doQuotaRequestWithRetry (integration-ish)
// ---------------------------------------------------------------------------

func TestDoQuotaRequestWithRetry_CircuitBreakerShortCircuits(t *testing.T) {
	svc := NewDiscoveryService()
	providerID := "test-provider-123"

	// Open the circuit by recording enough failures.
	circuit := svc.getOrCreateCircuit(providerID)
	for i := 0; i < quotaBreakerThreshold; i++ {
		circuit.recordFailure()
	}

	req, _ := http.NewRequest("GET", "http://example.com/quota", http.NoBody)
	ctx := context.Background()
	_, err := svc.doQuotaRequestWithRetry(ctx, req, providerID, "zai-coding")
	if err == nil {
		t.Fatal("expected error when circuit breaker is open")
	}
	if !strings.Contains(err.Error(), "circuit breaker open") {
		t.Errorf("expected circuit breaker error, got: %v", err)
	}
}

func TestDoQuotaRequestWithRetry_Retries429(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"quota": 100}`))
	}))
	defer server.Close()

	svc := NewDiscoveryService()
	svc.httpClient = server.Client()
	req, _ := http.NewRequest("GET", server.URL+"/quota", http.NoBody)
	ctx := context.Background()
	_, err := svc.doQuotaRequestWithRetry(ctx, req, "test-provider-429", "zai-coding")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls (2x429 + 1x200), got %d", callCount)
	}
}

func TestDoQuotaRequestWithRetry_Retries5xx(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("maintenance"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"quota": 100}`))
	}))
	defer server.Close()

	svc := NewDiscoveryService()
	svc.httpClient = server.Client()
	req, _ := http.NewRequest("GET", server.URL+"/quota", http.NoBody)
	ctx := context.Background()
	_, err := svc.doQuotaRequestWithRetry(ctx, req, "test-provider-5xx", "zai-coding")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func TestDoQuotaRequestWithRetry_NonRetryableStatusNoRetry(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	svc := NewDiscoveryService()
	svc.httpClient = server.Client()
	req, _ := http.NewRequest("GET", server.URL+"/quota", http.NoBody)
	ctx := context.Background()
	resp, err := svc.doQuotaRequestWithRetry(ctx, req, "test-provider-403", "zai-coding")
	if err != nil {
		t.Fatalf("expected no error for non-retryable status, got: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()
	if callCount != 1 {
		t.Errorf("expected 1 call (no retry for 403), got %d", callCount)
	}
}

// ---------------------------------------------------------------------------
// DiscoverModels - Additional Tests
// ---------------------------------------------------------------------------

func TestDiscoverModels_UnsupportedProviderTypeFallsBackToOpenAI(t *testing.T) {
	// Test that an unknown provider type falls back to OpenAI discovery
	// Note: There's no "unsupported" error - unknown types default to OpenAI
	mockResponse := `{
		"data": [
			{
				"id": "fallback-model",
				"object": "model",
				"owned_by": "fallback"
			}
		]
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
		Name:         "unknown-provider",
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels should fall back to OpenAI, got error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model from fallback, got %d", len(models))
	}
	if models[0].ModelID != "fallback-model" {
		t.Errorf("expected model ID 'fallback-model', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverModels_NilProvider(t *testing.T) {
	svc := NewDiscoveryService()
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

func TestDiscoverModels_OpenAIProviderType(t *testing.T) {
	// Test explicit OpenAI provider type
	mockResponse := `{
		"data": [
			{
				"id": "gpt-4-test",
				"object": "model",
				"owned_by": "openai"
			}
		]
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
		Name:         "openai-provider",
		BaseURL:      server.URL,
		EncryptedKey: []byte{},
	}

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels for OpenAI should succeed, got error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "gpt-4-test" {
		t.Errorf("expected model ID 'gpt-4-test', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverModels_AnthropicProviderType(t *testing.T) {
	// Test Anthropic provider type - uses different endpoint
	// Anthropic uses pagination with "data" array and has_more/last_id
	mockResponse := `{
		"data": [
			{
				"id": "claude-3-opus-20240229",
				"display_name": "Claude 3 Opus",
				"capabilities": {},
				"max_input_tokens": 200000,
				"max_tokens": 4096
			}
		],
		"has_more": false,
		"last_id": ""
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

	// Use the test server's client, but override the BaseURL to trigger anthropic detection
	// The httpClient will still connect to the test server
	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:   uuid.New(),
		Name: "anthropic-provider",
		// Use anthropic.com domain to trigger anthropic provider type detection
		// The test server's transport will handle the actual connection
		BaseURL:      "https://api.anthropic.com",
		EncryptedKey: []byte{},
	}

	// Override the transport to redirect all requests to test server
	svc.httpClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		newURL := server.URL + req.URL.Path
		if req.URL.RawQuery != "" {
			newURL += "?" + req.URL.RawQuery
		}
		newReq := req.Clone(req.Context())
		newReq.URL, _ = url.Parse(newURL)
		newReq.Host = newReq.URL.Host
		return http.DefaultTransport.RoundTrip(newReq)
	})

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels for Anthropic should succeed, got error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "claude-3-opus-20240229" {
		t.Errorf("expected model ID 'claude-3-opus-20240229', got '%s'", models[0].ModelID)
	}
}

// roundTripperFunc wraps a function to implement http.RoundTripper
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDiscoverModels_OllamaProviderType(t *testing.T) {
	// Test Ollama provider type - uses /api/tags endpoint
	// Ollama also calls /api/show for each model to get details
	mockTagsResponse := `{
		"models": [
			{
				"name": "llama3.2:latest",
				"model": "llama3.2:latest"
			}
		]
	}`
	mockShowResponse := `{
		"details": {
			"family": "llama3"
		},
		"model_info": {}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockTagsResponse))
			return
		}
		if r.URL.Path == "/api/show" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(mockShowResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	// Use ollama.com domain to trigger ollama-cloud provider type detection
	svc := &DiscoveryService{httpClient: server.Client()}
	provider := &Provider{
		ID:           uuid.New(),
		Name:         "ollama-provider",
		BaseURL:      "https://api.ollama.com",
		EncryptedKey: []byte{},
	}

	// Override the transport to redirect all requests to test server
	svc.httpClient.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		newURL := server.URL + req.URL.Path
		if req.URL.RawQuery != "" {
			newURL += "?" + req.URL.RawQuery
		}
		newReq := req.Clone(req.Context())
		newReq.URL, _ = url.Parse(newURL)
		newReq.Host = newReq.URL.Host
		return http.DefaultTransport.RoundTrip(newReq)
	})

	ctx := context.Background()
	models, err := svc.DiscoverModels(ctx, provider, "test-master-key")
	if err != nil {
		t.Fatalf("DiscoverModels for Ollama should succeed, got error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ModelID != "llama3.2:latest" {
		t.Errorf("expected model ID 'llama3.2:latest', got '%s'", models[0].ModelID)
	}
}

func TestDiscoverModels_NetworkErrorPropagated(t *testing.T) {
	// Test that network errors from provider discovery are propagated
	svc := NewDiscoveryService()
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

func TestAnthropicDiscovery_Non200Status(t *testing.T) {
	t.Parallel()

	// Create test server that returns 403
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := svc.discoverAnthropic(context.Background(), provider, "test-key")
	if err == nil {
		t.Error("Expected error for non-200 status, got nil")
	}
	if !strings.Contains(err.Error(), "unexpected status code") {
		t.Errorf("Expected 'unexpected status code' error, got: %v", err)
	}
}

func TestAnthropicDiscovery_JSONDecodeError(t *testing.T) {
	t.Parallel()

	// Create test server with invalid JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json "))
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := svc.discoverAnthropic(context.Background(), provider, "test-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("Expected 'failed to decode response' error, got: %v", err)
	}
}

func TestAnthropicDiscovery_RequestCreationError(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.anthropic.com",
	}

	// Create cancelled context to trigger request creation error
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := svc.discoverAnthropic(ctx, provider, "test-key")
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestAnthropicDiscovery_PDFWithoutVision(t *testing.T) {
	t.Parallel()

	// Model with PDF capability but NOT vision - should trigger the modality switch
	pageResponse := `{
		"data": [
			{
				"id": "claude-pdf-only",
				"type": "model",
				"display_name": "Claude PDF Only",
				"created_at": "2025-01-01T00:00:00Z",
				"max_input_tokens": 200000,
				"max_tokens": 32768,
				"capabilities": {
					"image_input": {"supported": false},
					"pdf_input": {"supported": true},
					"structured_outputs": {"supported": false},
					"batch": {"supported": false},
					"citations": {"supported": false},
					"code_execution": {"supported": false}
				}
			}
		],
		"has_more": false,
		"first_id": "claude-pdf-only",
		"last_id": "claude-pdf-only"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(pageResponse))
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := svc.discoverAnthropic(context.Background(), provider, "test-key")
	if err != nil {
		t.Fatalf("discoverAnthropic failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	m := models[0]
	// Should have PDFUpload capability
	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.PDFUpload {
		t.Error("Expected PDFUpload=true for model with pdf_input capability")
	}
	// Modality should be switched to "vision" even though image_input is false
	if m.Modality != "vision" {
		t.Errorf("Expected modality 'vision' for PDF-capable model, got '%s'", m.Modality)
	}
	// Input modalities should include image
	if !strings.Contains(m.InputModalities, "image") {
		t.Errorf("Expected input modalities to include 'image', got '%s'", m.InputModalities)
	}
}

// =============================================================================
// Cohere Discovery Tests
// =============================================================================

func TestDiscoverCohere_RequestCreationError(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.cohere.ai/compatibility/v1",
	}

	// Create cancelled context to trigger request creation error
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := svc.discoverCohere(ctx, provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestDiscoverCohere_ReadBodyError(t *testing.T) {
	t.Parallel()

	// Create a custom RoundTripper that returns a response with failing body
	errorRoundTripper := &errorBodyRoundTripper{}
	client := &http.Client{
		Transport: errorRoundTripper,
	}

	svc := &DiscoveryService{
		httpClient: client,
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.cohere.ai/compatibility/v1",
	}

	_, err := svc.discoverCohere(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for body read failure, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read response") {
		t.Errorf("Expected 'failed to read response' error, got: %v", err)
	}
}

// errorBodyRoundTripper returns a response with a body that fails on read
type errorBodyRoundTripper struct{}

func (e *errorBodyRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(&failingReader{}),
	}, nil
}

// failingReader returns valid JSON once, then returns an error.
// Without state tracking, the reader would satisfy the entire read in one
// call (when len(p) >= len(data)), never triggering the error path.
type failingReader struct{ called bool }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.called {
		return 0, io.ErrUnexpectedEOF
	}
	f.called = true
	data := []byte(`{"models":[],"next_page_token":""}`)
	copy(p, data)
	return len(data), nil
}

func TestDiscoverCohere_JSONDecodeError(t *testing.T) {
	t.Parallel()

	// Create test server with 200 status but invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("{ invalid json for cohere "))
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	_, err := svc.discoverCohere(context.Background(), provider, "test-api-key")
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decode response") {
		t.Errorf("Expected 'failed to decode response' error, got: %v", err)
	}
}

func TestDiscoverCohere_ModelWithPricing(t *testing.T) {
	t.Parallel()

	// Create test server with a model that matches the pricing catalog
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := CohereModelsResponse{
			Models: []CohereNativeModel{
				{
					Name:          "command-r-plus-08-2024",
					Endpoints:     []string{"chat"},
					ContextLength: 128000,
					Features:      []string{"tools", "vision"},
					IsDeprecated:  false,
				},
			},
			NextPageToken: "",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := svc.discoverCohere(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverCohere failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	m := models[0]
	// Should have pricing from catalog
	if m.InputPricePerMillion == nil {
		t.Error("Expected InputPricePerMillion to be set for model in pricing catalog")
	} else if *m.InputPricePerMillion != 2.50 {
		t.Errorf("Expected input price 2.50, got %.2f", *m.InputPricePerMillion)
	}
	if m.OutputPricePerMillion == nil {
		t.Error("Expected OutputPricePerMillion to be set for model in pricing catalog")
	} else if *m.OutputPricePerMillion != 10.00 {
		t.Errorf("Expected output price 10.00, got %.2f", *m.OutputPricePerMillion)
	}
	if m.DisplayName != "Command R+" {
		t.Errorf("Expected DisplayName 'Command R+', got '%s'", m.DisplayName)
	}
	if m.MaxOutputTokens == nil {
		t.Error("Expected MaxOutputTokens to be set")
	} else if *m.MaxOutputTokens != 4096 {
		t.Errorf("Expected MaxOutputTokens 4096, got %d", *m.MaxOutputTokens)
	}
}

// =============================================================================
// Ollama Discovery Tests
// =============================================================================

func TestDiscoverOllama_ShowModelFailure(t *testing.T) {
	t.Parallel()

	// Create test server where /api/tags succeeds but /api/show fails for one model
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/tags" && r.Method == "GET":
			response := OllamaTagsResponse{
				Models: []OllamaTagsModel{
					{Name: "llama3.2"},
					{Name: "failing-model"},
					{Name: "mistral"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		case r.URL.Path == "/api/show" && r.Method == "POST":
			// Read the request body to get model name
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), "failing-model") {
				http.Error(w, "Model not found", http.StatusNotFound)
				return
			}
			// Successful response for other models
			response := OllamaShowResponse{
				Capabilities: []string{"tools"},
				ModelInfo: map[string]interface{}{
					"llama.context_length": float64(8192),
				},
				Details: OllamaShowDetails{
					Family: "llama",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	provider := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	models, err := svc.discoverOllama(context.Background(), provider, "test-api-key")
	if err != nil {
		t.Fatalf("discoverOllama failed: %v", err)
	}

	// Should have 2 models (1 skipped due to show failure)
	if len(models) != 2 {
		t.Errorf("Expected 2 models (1 skipped), got %d", len(models))
	}
}

func TestBuildOllamaModel_ThinkingCapability(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{}

	provider := &Provider{
		ID: uuid.New(),
	}

	showResponse := &OllamaShowResponse{
		Capabilities: []string{"tools", "thinking", "vision"},
		ModelInfo: map[string]interface{}{
			"llama.context_length": float64(32768),
		},
		Details: OllamaShowDetails{
			Family: "llama",
		},
	}

	m := svc.buildOllamaModel(provider, "test-model-thinking", showResponse)

	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}

	if !caps.Reasoning {
		t.Error("Expected Reasoning=true for 'thinking' capability")
	}
	if !caps.ToolCalling {
		t.Error("Expected ToolCalling=true for 'tools' capability")
	}
	if !caps.Vision {
		t.Error("Expected Vision=true for 'vision' capability")
	}
	if m.Modality != "vision" {
		t.Errorf("Expected modality 'vision', got '%s'", m.Modality)
	}
	if m.OwnedBy != "llama" {
		t.Errorf("Expected ownedBy 'llama', got '%s'", m.OwnedBy)
	}
}

func TestBuildOllamaModel_EmptyFamily(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{}

	provider := &Provider{
		ID: uuid.New(),
	}

	showResponse := &OllamaShowResponse{
		Capabilities: []string{"tools"},
		ModelInfo: map[string]interface{}{
			"llama.context_length": float64(8192),
		},
		Details: OllamaShowDetails{
			Family: "", // Empty family should default to "ollama"
		},
	}

	m := svc.buildOllamaModel(provider, "test-model-empty-family", showResponse)

	if m.OwnedBy != "ollama" {
		t.Errorf("Expected ownedBy 'ollama' for empty family, got '%s'", m.OwnedBy)
	}
}

func TestGetOllamaCloudAccount_RequestCreationError(t *testing.T) {
	t.Parallel()

	svc := &DiscoveryService{
		httpClient: http.DefaultClient,
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      "https://ollama.com/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	// Create cancelled context to trigger request creation error
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err = svc.GetOllamaCloudAccount(ctx, provider, masterKey)
	if err == nil {
		t.Error("Expected error for cancelled context, got nil")
	}
}

func TestGetOllamaCloudAccount_JSONDecodeError(t *testing.T) {
	t.Parallel()

	// Create test server that returns 200 with invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/me" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("{ invalid json for ollama cloud "))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := &DiscoveryService{
		httpClient: server.Client(),
	}

	masterKey := "test-master-key-1234567890123456"
	apiKey := "test-api-key"

	kp, err := auth.Encrypt(apiKey, masterKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	provider := &Provider{
		ID:           uuid.New(),
		BaseURL:      server.URL + "/v1",
		EncryptedKey: kp.Ciphertext,
		KeyNonce:     kp.Nonce,
		KeySalt:      kp.Salt,
	}

	_, err = svc.GetOllamaCloudAccount(context.Background(), provider, masterKey)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "failed to decode account response") {
		t.Errorf("Expected 'failed to decode account response' error, got: %v", err)
	}
}
