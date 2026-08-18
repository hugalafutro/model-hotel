package paramrewrite

import (
	"encoding/json"
	"sync"
	"testing"
)

// googleReasoningRejection is the body Google AI Studio returned in production
// for every hotel/gemma-4-31b-it request on 2026-08-18: the error object is
// wrapped in a JSON array, and it names the offending field in double quotes.
const googleReasoningRejection = `[{
  "error": {
    "code": 400,
    "message": "Invalid JSON payload received. Unknown name \"reasoning\": Cannot find field.",
    "status": "INVALID_ARGUMENT"
  }
}]`

func TestParseProviderParamError_GoogleArrayWrappedReasoning(t *testing.T) {
	rejected := ParseProviderParamError([]byte(googleReasoningRejection))
	if rejected == nil {
		t.Fatal("expected the array-wrapped Google error to be parsed, got nil")
	}
	if !rejected["reasoning"] {
		t.Errorf("expected \"reasoning\" to be learned as rejected, got %v", rejected)
	}
	if rejected["reasoning_effort"] {
		t.Error("\"reasoning\" must not also strip reasoning_effort — they are separate params")
	}
}

func TestParseProviderParamError_ReasoningEffortDoesNotMatchReasoning(t *testing.T) {
	// The inverse guard: a provider naming reasoning_effort must not cause the
	// distinct "reasoning" field to be stripped.
	body := `{"error":{"message":"Unsupported parameter: \"reasoning_effort\" is not supported"}}`
	rejected := ParseProviderParamError([]byte(body))
	if !rejected["reasoning_effort"] {
		t.Fatalf("expected reasoning_effort to be rejected, got %v", rejected)
	}
	if rejected["reasoning"] {
		t.Errorf("reasoning_effort must not imply reasoning, got %v", rejected)
	}
}

func TestParseProviderParamRename_ReadsArrayWrappedBody(t *testing.T) {
	body := `[{"error":{"message":"Use \"max_completion_tokens\" instead of \"max_tokens\", which is not supported"}}]`
	renames := ParseProviderParamRename([]byte(body))
	if renames["max_tokens"] != "max_completion_tokens" {
		t.Errorf("expected max_tokens rename to be learned from an array body, got %v", renames)
	}
}

func TestProviderErrorMessage_ObjectAndArrayForms(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"object", `{"error":{"message":"boom"}}`, "boom"},
		{"array", `[{"error":{"message":"boom"}}]`, "boom"},
		{"array skips empty", `[{"error":{}},{"error":{"message":"second"}}]`, "second"},
		{"not json", `<html>502</html>`, ""},
		{"empty array", `[]`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providerErrorMessage([]byte(tt.body)); got != tt.want {
				t.Errorf("providerErrorMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestLearnedCacheKey_ScopedPerProviderNotPerType guards the cross-provider
// leak: provider.TypeOf falls back to "openai" for every custom
// OpenAI-compatible endpoint, so keying learned rejections by TYPE lets one
// endpoint's 400 disable a param for every other endpoint serving the same
// model id.
func TestLearnedCacheKey_ScopedPerProviderNotPerType(t *testing.T) {
	const model = "gpt-4o"
	providerA := "11111111-1111-4111-8111-111111111111"
	providerB := "22222222-2222-4222-8222-222222222222"

	if LearnedCacheKey(providerA, model) == LearnedCacheKey(providerB, model) {
		t.Fatal("two providers serving the same model id must not share a learned-cache entry")
	}

	var deprecationCache, renameCache sync.Map
	// providerA teaches us that it rejects top_p.
	MergeLearnedParamCache(&deprecationCache, LearnedCacheKey(providerA, model),
		map[string]bool{"top_p": true})

	body := []byte(`{"model":"gpt-4o","messages":[],"top_p":0.9}`)
	fromA := BuildUpstreamBody(body, "openai", model, model, false, &deprecationCache, &renameCache, nil, providerA)
	fromB := BuildUpstreamBody(body, "openai", model, model, false, &deprecationCache, &renameCache, nil, providerB)

	var rawA, rawB map[string]any
	if err := json.Unmarshal(fromA, &rawA); err != nil {
		t.Fatalf("provider A body is not valid JSON: %v", err)
	}
	if err := json.Unmarshal(fromB, &rawB); err != nil {
		t.Fatalf("provider B body is not valid JSON: %v", err)
	}
	if _, present := rawA["top_p"]; present {
		t.Error("provider A taught us it rejects top_p, so its own requests must drop it")
	}
	if _, present := rawB["top_p"]; !present {
		t.Error("provider B never rejected top_p — another openai-typed endpoint's 400 " +
			"must not strip it here")
	}
}
