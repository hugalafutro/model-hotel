package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/util"
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
			got := util.BuildProviderTargetURL(tt.baseURL, tt.providerType, "/chat/completions")
			if got != tt.want {
				t.Errorf("BuildProviderTargetURL(%q, %q, %q) = %q, want %q", tt.baseURL, tt.providerType, "/chat/completions", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// paramrewrite.CachedRejectedParams
// ---------------------------------------------------------------------------

func TestGetCachedRejectedParams_EmptyCache(t *testing.T) {
	var cache sync.Map
	got := paramrewrite.CachedRejectedParams(&cache, "nonexistent")
	if got != nil {
		t.Errorf("expected nil for empty cache, got %v", got)
	}
}

func TestGetCachedRejectedParams_KeyExists(t *testing.T) {
	var cache sync.Map
	expected := map[string]bool{"top_p": true, "temperature": true}
	cache.Store("anthropic:claude-3-opus", expected)

	got := paramrewrite.CachedRejectedParams(&cache, "anthropic:claude-3-opus")
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestGetCachedRejectedParams_WrongType(t *testing.T) {
	var cache sync.Map
	cache.Store("key", "not a map")

	got := paramrewrite.CachedRejectedParams(&cache, "key")
	if got != nil {
		t.Errorf("expected nil for wrong type value, got %v", got)
	}
}

func TestGetCachedRejectedParams_NilMapValue(t *testing.T) {
	var cache sync.Map
	cache.Store("key", map[string]bool(nil))

	got := paramrewrite.CachedRejectedParams(&cache, "key")
	// A nil map[string]bool passes the type assertion in Load, but the
	// returned value is still nil (a nil map is nil in Go). Verify the
	// function returns nil rather than a sentinel or empty map.
	if got != nil {
		t.Errorf("paramrewrite.CachedRejectedParams should return nil for nil map value, got %v", got)
	}
}

func TestGetCachedRejectedParams_MultipleKeys(t *testing.T) {
	var cache sync.Map
	cache.Store("provider-a:model-1", map[string]bool{"top_p": true})
	cache.Store("provider-b:model-2", map[string]bool{"temperature": true, "top_k": true})

	got := paramrewrite.CachedRejectedParams(&cache, "provider-b:model-2")
	expected := map[string]bool{"temperature": true, "top_k": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

// ---------------------------------------------------------------------------
// paramrewrite.ParseProviderParamError
// ---------------------------------------------------------------------------

func TestParseProviderParamError_InvalidJSON(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{invalid json`))
	if got != nil {
		t.Errorf("expected nil for invalid JSON, got %v", got)
	}
}

func TestParseProviderParamError_ValidJSONNoMatch(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error": {"message": "some unrelated error"}}`))
	if got != nil {
		t.Errorf("expected nil for unrelated error, got %v", got)
	}
}

func TestParseProviderParamError_TemperatureBacktick(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`temperature`" + ` is deprecated for this model"}}`))
	expected := map[string]bool{"temperature": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TopPQuoted(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"\"top_p\" is not supported"}}`))
	expected := map[string]bool{"top_p": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TopKBacktick(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`top_k`" + ` is not supported on this endpoint"}}`))
	expected := map[string]bool{"top_k": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_CannotBothBeSpecified(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"cannot both be specified"}}`))
	expected := map[string]bool{"top_p": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TemperatureAndTopP(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`temperature` and `top_p`" + ` cannot both be specified"}}`))
	expected := map[string]bool{"temperature": true, "top_p": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TemperatureNotWrapped(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"invalid request for temperature value"}}`))
	if got != nil {
		t.Errorf("expected nil when temperature is not backtick/quote-wrapped, got %v", got)
	}
}

func TestParseProviderParamError_ShortParamN(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`n`" + ` must be exactly 1"}}`))
	expected := map[string]bool{"n": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_ShortParamNNotWrapped(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"n completions not supported"}}`))
	if got != nil {
		t.Errorf("expected nil when n is not wrapped, got %v", got)
	}
}

func TestParseProviderParamError_TopABacktick(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`top_a`" + ` is not supported"}}`))
	expected := map[string]bool{"top_a": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TopZQuoted(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"\"top_z\" is unsupported"}}`))
	expected := map[string]bool{"top_z": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_EmptyMessage(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":""}}`))
	if got != nil {
		t.Errorf("expected nil for empty message, got %v", got)
	}
}

func TestParseProviderParamError_BacktickBoundary(t *testing.T) {
	// Message ending right at the closing backtick
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`top_p`" + `"}}`))
	expected := map[string]bool{"top_p": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_TopNotWrapped(t *testing.T) {
	// Message contains top_ but NOT backtick/quote-wrapped
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"the top_p parameter is not supported"}}`))
	if got != nil {
		t.Errorf("expected nil when top_ is not wrapped, got %v", got)
	}
}

func TestParseProviderParamError_FrequencyPenalty(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`frequency_penalty`" + ` not supported by this model"}}`))
	expected := map[string]bool{"frequency_penalty": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_PresencePenalty(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"\"presence_penalty\" is invalid for this endpoint"}}`))
	expected := map[string]bool{"presence_penalty": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_MultipleParams(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` +
		"`temperature`, `top_p`, `top_k`" +
		` are not allowed with this model"}}`))
	expected := map[string]bool{"temperature": true, "top_p": true, "top_k": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_NoErrorField(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"id": "123"}`))
	if got != nil {
		t.Errorf("expected nil when no error field, got %v", got)
	}
}

func TestParseProviderParamError_StopBacktick(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`stop`" + ` is not supported"}}`))
	expected := map[string]bool{"stop": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_SeedBacktick(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`seed`" + ` cannot be set on this endpoint"}}`))
	expected := map[string]bool{"seed": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_MaxTokensBacktick(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`max_tokens`" + ` exceeds model limit"}}`))
	expected := map[string]bool{"max_tokens": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_LogprobsQuoted(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"\"logprobs\" is not available"}}`))
	expected := map[string]bool{"logprobs": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_ReasoningEffortBacktick(t *testing.T) {
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"` + "`reasoning_effort`" + ` is not supported"}}`))
	expected := map[string]bool{"reasoning_effort": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

// chat_template_args is matched as a bare substring (issue #281): strict
// OpenCode upstreams reject the injected field with assorted message formats.

func TestParseProviderParamError_ChatTemplateArgsVLLMSingleQuote(t *testing.T) {
	// vLLM/pydantic format (e.g. opencode-go/glm-5.2): single-quoted field name.
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"Error from provider: Extra inputs are not permitted, field: 'chat_template_args'"}}`))
	expected := map[string]bool{"chat_template_args": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamError_ChatTemplateArgsBare(t *testing.T) {
	// OpenAI-style passthrough (e.g. opencode-zen/gpt-5-nano): bare field name.
	got := paramrewrite.ParseProviderParamError([]byte(`{"error":{"message":"Unrecognized request argument supplied: chat_template_args"}}`))
	expected := map[string]bool{"chat_template_args": true}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

// ---------------------------------------------------------------------------
// paramrewrite.ParseProviderParamRename
// ---------------------------------------------------------------------------

func TestParseProviderParamRename_MaxCompletionTokens(t *testing.T) {
	// OpenAI gpt-5/o-series deprecation (also reaches us via OpenCode Zen
	// passthrough): max_tokens must be renamed to max_completion_tokens, not
	// dropped — dropping would silently discard the caller's token budget.
	got := paramrewrite.ParseProviderParamRename([]byte(`{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.","type":"invalid_request_error","param":"max_tokens","code":"unsupported_parameter"}}`))
	expected := map[string]string{"max_tokens": "max_completion_tokens"}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestParseProviderParamRename_NoRenameSignal(t *testing.T) {
	// An ordinary rejection that doesn't mention max_completion_tokens must not
	// trigger a rename.
	got := paramrewrite.ParseProviderParamRename([]byte(`{"error":{"message":"Unrecognized request argument supplied: chat_template_args"}}`))
	if got != nil {
		t.Errorf("expected nil (no rename), got %v", got)
	}
}

func TestParseProviderParamRename_ValidationErrorNotRename(t *testing.T) {
	// A value-validation error that merely mentions max_completion_tokens (no
	// max_tokens, no "instead" directive) must NOT poison the rename cache —
	// otherwise a sibling model that natively accepts max_tokens would have it
	// silently renamed on every later request.
	cases := []string{
		`{"error":{"message":"Invalid 'max_completion_tokens': must not exceed 4096."}}`,
		`{"error":{"message":"max_completion_tokens must be a positive integer"}}`,
		`{"error":{"message":"Only one of max_tokens or max_completion_tokens may be specified."}}`,
	}
	for _, body := range cases {
		if got := paramrewrite.ParseProviderParamRename([]byte(body)); got != nil {
			t.Errorf("expected nil (not a rename directive) for %q, got %v", body, got)
		}
	}
}

func TestParseProviderParamRename_Unparseable(t *testing.T) {
	if got := paramrewrite.ParseProviderParamRename([]byte("not json")); got != nil {
		t.Errorf("expected nil for unparseable body, got %v", got)
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

// ---------------------------------------------------------------------------
// Tests moved from build_url_test.go
// ---------------------------------------------------------------------------

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
			got := util.BuildProviderTargetURL(tc.baseURL, tc.providerType, "/chat/completions")
			if got != tc.want {
				t.Errorf("BuildProviderTargetURL(%q, %q, %q) = %q, want %q", tc.baseURL, tc.providerType, "/chat/completions", got, tc.want)
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
			got := util.BuildProviderTargetURL(baseURL, provider, "/chat/completions")
			if got != expected {
				t.Errorf("BuildProviderTargetURL(%q, %q, %q) = %q, want %q", baseURL, provider, "/chat/completions", got, expected)
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

			switch tc.name {
			case "anthropic with /v1 suffix":
				if !hasV1Suffix {
					t.Errorf("expected sanitized URL %q to have /v1 suffix", sanitized)
				}
			case "anthropic with /v1/ suffix (becomes /v1 after sanitize)":
				if !hasV1Suffix {
					t.Errorf("expected sanitized URL %q to have /v1 suffix after stripping trailing slash", sanitized)
				}
			case "anthropic without /v1 suffix":
				if hasV1Suffix {
					t.Errorf("did not expect sanitized URL %q to have /v1 suffix", sanitized)
				}
			}

			got := util.BuildProviderTargetURL(tc.baseURL, tc.providerType, "/chat/completions")
			if tc.providerType == "anthropic" && hasV1Suffix {
				if strings.Contains(got, "/v1/v1/") {
					t.Errorf("double /v1 detected in result: %q", got)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Tests moved from param_error_test.go
// ---------------------------------------------------------------------------

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
		params, ok := paramrewrite.ProviderUnsupportedParams[provider]
		if !ok {
			t.Errorf("provider %q: missing from paramrewrite.ProviderUnsupportedParams", provider)
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
		params, ok := paramrewrite.ProviderUnsupportedParams[provider]
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
	if params, ok := paramrewrite.ProviderUnsupportedParams["anthropic"]; ok {
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

// Note: TestParseProviderParamError_ReasoningEffort was a duplicate and was dropped
