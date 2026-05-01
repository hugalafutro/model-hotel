package provider

import (
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestDetectProviderType_Ollama(t *testing.T) {
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
			if result != "ollama" {
				t.Errorf("DetectProviderType(%q) = %q, want %q", tc.url, result, "ollama")
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
// Helper
// ---------------------------------------------------------------------------

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
