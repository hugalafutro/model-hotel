package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"sync"
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
		{
			name:         "nanogpt provider",
			baseURL:      "https://api.nanogpt.example.com",
			providerType: "nanogpt",
			want:         "https://api.nanogpt.example.com/chat/completions",
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
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
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
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
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
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	setProviderAuthHeaders(req, "openai", "sk-test-key")
	if v := req.Header.Get("Authorization"); v != "Bearer sk-test-key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer sk-test-key")
	}
	if v := req.Header.Get("x-api-key"); v != "" {
		t.Errorf("x-api-key should not be set for openai, got %q", v)
	}
}

func TestSetProviderAuthHeaders_Google(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	setProviderAuthHeaders(req, "google", "test-key")
	if v := req.Header.Get("Authorization"); v != "Bearer test-key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer test-key")
	}
}

func TestSetProviderAuthHeaders_EmptyProvider(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	setProviderAuthHeaders(req, "", "key")
	if v := req.Header.Get("Authorization"); v != "Bearer key" {
		t.Errorf("Authorization = %q, want %q", v, "Bearer key")
	}
}

// ---------------------------------------------------------------------------
// getCachedRejectedParams
// ---------------------------------------------------------------------------

func TestGetCachedRejectedParams_EmptyCache(t *testing.T) {
	var cache sync.Map
	got := getCachedRejectedParams(&cache, "nonexistent")
	if got != nil {
		t.Errorf("expected nil for empty cache, got %v", got)
	}
}

func TestGetCachedRejectedParams_KeyExists(t *testing.T) {
	var cache sync.Map
	expected := map[string]bool{"top_p": true, "temperature": true}
	cache.Store("anthropic:claude-3-opus", expected)

	got := getCachedRejectedParams(&cache, "anthropic:claude-3-opus")
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestGetCachedRejectedParams_WrongType(t *testing.T) {
	var cache sync.Map
	cache.Store("key", "not a map")

	got := getCachedRejectedParams(&cache, "key")
	if got != nil {
		t.Errorf("expected nil for wrong type value, got %v", got)
	}
}

func TestGetCachedRejectedParams_NilMapValue(_ *testing.T) {
	var cache sync.Map
	cache.Store("key", map[string]bool(nil))

	got := getCachedRejectedParams(&cache, "key")
	// A nil map[string]bool passes the type assertion, so Load returns it.
	// This just verifies it doesn't panic.
	_ = got
}

func TestGetCachedRejectedParams_MultipleKeys(t *testing.T) {
	var cache sync.Map
	cache.Store("provider-a:model-1", map[string]bool{"top_p": true})
	cache.Store("provider-b:model-2", map[string]bool{"temperature": true, "top_k": true})

	got := getCachedRejectedParams(&cache, "provider-b:model-2")
	expected := map[string]bool{"temperature": true, "top_k": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

// ---------------------------------------------------------------------------
// parseProviderParamError
// ---------------------------------------------------------------------------

func TestParseProviderParamError_InvalidJSON(t *testing.T) {
	got := parseProviderParamError([]byte(`{invalid json`))
	if got != nil {
		t.Errorf("expected nil for invalid JSON, got %v", got)
	}
}

func TestParseProviderParamError_ValidJSONNoMatch(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error": {"message": "some unrelated error"}}`))
	if got != nil {
		t.Errorf("expected nil for unrelated error, got %v", got)
	}
}

func TestParseProviderParamError_TemperatureBacktick(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`temperature`" + ` is deprecated for this model"}}`))
	expected := map[string]bool{"temperature": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TopPQuoted(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"\"top_p\" is not supported"}}`))
	expected := map[string]bool{"top_p": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TopKBacktick(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`top_k`" + ` is not supported on this endpoint"}}`))
	expected := map[string]bool{"top_k": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_CannotBothBeSpecified(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"cannot both be specified"}}`))
	expected := map[string]bool{"top_p": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TemperatureAndTopP(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`temperature` and `top_p`" + ` cannot both be specified"}}`))
	expected := map[string]bool{"temperature": true, "top_p": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TemperatureNotWrapped(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"invalid request for temperature value"}}`))
	if got != nil {
		t.Errorf("expected nil when temperature is not backtick/quote-wrapped, got %v", got)
	}
}

func TestParseProviderParamError_ShortParamN(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`n`" + ` must be exactly 1"}}`))
	expected := map[string]bool{"n": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_ShortParamNNotWrapped(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"n completions not supported"}}`))
	if got != nil {
		t.Errorf("expected nil when n is not wrapped, got %v", got)
	}
}

func TestParseProviderParamError_TopABacktick(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`top_a`" + ` is not supported"}}`))
	expected := map[string]bool{"top_a": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TopZQuoted(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"\"top_z\" is unsupported"}}`))
	expected := map[string]bool{"top_z": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_EmptyMessage(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":""}}`))
	if got != nil {
		t.Errorf("expected nil for empty message, got %v", got)
	}
}

func TestParseProviderParamError_BacktickBoundary(t *testing.T) {
	// Message ending right at the closing backtick
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`top_p`" + `"}}`))
	expected := map[string]bool{"top_p": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TopNotWrapped(t *testing.T) {
	// Message contains top_ but NOT backtick/quote-wrapped
	got := parseProviderParamError([]byte(`{"error":{"message":"the top_p parameter is not supported"}}`))
	if got != nil {
		t.Errorf("expected nil when top_ is not wrapped, got %v", got)
	}
}

func TestParseProviderParamError_FrequencyPenalty(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`frequency_penalty`" + ` not supported by this model"}}`))
	expected := map[string]bool{"frequency_penalty": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_PresencePenalty(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"\"presence_penalty\" is invalid for this endpoint"}}`))
	expected := map[string]bool{"presence_penalty": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_MultipleParams(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` +
		"`temperature`, `top_p`, `top_k`" +
		` are not allowed with this model"}}`))
	expected := map[string]bool{"temperature": true, "top_p": true, "top_k": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_NoErrorField(t *testing.T) {
	got := parseProviderParamError([]byte(`{"id": "123"}`))
	if got != nil {
		t.Errorf("expected nil when no error field, got %v", got)
	}
}

func TestParseProviderParamError_StopBacktick(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`stop`" + ` is not supported"}}`))
	expected := map[string]bool{"stop": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_SeedBacktick(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`seed`" + ` cannot be set on this endpoint"}}`))
	expected := map[string]bool{"seed": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_MaxTokensBacktick(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`max_tokens`" + ` exceeds model limit"}}`))
	expected := map[string]bool{"max_tokens": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_LogprobsQuoted(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"\"logprobs\" is not available"}}`))
	expected := map[string]bool{"logprobs": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_ReasoningEffortBacktick(t *testing.T) {
	got := parseProviderParamError([]byte(`{"error":{"message":"` + "`reasoning_effort`" + ` is not supported"}}`))
	expected := map[string]bool{"reasoning_effort": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

// ---------------------------------------------------------------------------
// mapKeys
// ---------------------------------------------------------------------------

func TestMapKeys_Empty(t *testing.T) {
	got := mapKeys(map[string]bool{})
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestMapKeys_OneKey(t *testing.T) {
	got := mapKeys(map[string]bool{"top_p": true})
	expected := []string{"top_p"}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestMapKeys_MultipleKeys(t *testing.T) {
	input := map[string]bool{"top_p": true, "temperature": true, "top_k": true}
	got := mapKeys(input)
	expected := []string{"top_p", "temperature", "top_k"}

	sort.Strings(got)
	sort.Strings(expected)

	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestMapKeys_NilMap(t *testing.T) {
	got := mapKeys(nil)
	if got == nil {
		t.Error("mapKeys(nil) should return empty slice, not nil")
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice for nil map, got %v", got)
	}
}
func TestWriteOpenAIError(t *testing.T) {
	rr := httptest.NewRecorder()
	writeOpenAIError(rr, "model is required", http.StatusBadRequest)

	if rr.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", rr.Header().Get("Content-Type"))
	}
	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	errObj, ok := resp["error"].(map[string]interface{})
	if !ok {
		t.Fatal("response missing 'error' object")
	}
	if errObj["message"] != "model is required" {
		t.Errorf("expected message 'model is required', got %v", errObj["message"])
	}
	if errObj["type"] != "invalid_request_error" {
		t.Errorf("expected type 'invalid_request_error', got %v", errObj["type"])
	}
}

func TestWriteOpenAIError_502(t *testing.T) {
	rr := httptest.NewRecorder()
	writeOpenAIError(rr, "all providers failed for model test", http.StatusBadGateway)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", rr.Code)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	errObj := resp["error"].(map[string]interface{})
	if errObj["message"] != "all providers failed for model test" {
		t.Errorf("unexpected message: %v", errObj["message"])
	}
}
