package paramrewrite

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
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
		t.Error("\"reasoning\" must not also strip reasoning_effort; they are separate params")
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
		t.Error("provider B never rejected top_p; another openai-typed endpoint's 400 " +
			"must not strip it here")
	}
}

// OpenAI names the offending parameter in single quotes in its value-validation
// errors, which the backtick/double-quote anchors missed. Every gpt-5-family
// request carrying temperature:0 therefore failed with an unhealed 400.
func TestParseProviderParamError_SingleQuotedParam(t *testing.T) {
	t.Parallel()

	body := []byte(`{"error":{"message":"Unsupported value: 'temperature' does not support 0 with this model. Only the default (1) value is supported.","type":"invalid_request_error","param":"temperature","code":"unsupported_value"}}`)

	rejected := ParseProviderParamError(body)
	if !rejected["temperature"] {
		t.Fatalf("expected temperature to be rejected, got %v", rejected)
	}
}

func TestParseProviderParamError_SingleQuotedShortParams(t *testing.T) {
	t.Parallel()

	rejected := ParseProviderParamError([]byte(`{"error":{"message":"Unsupported value: 'n' is not supported with this model."}}`))
	if !rejected["n"] {
		t.Fatalf("expected n to be rejected, got %v", rejected)
	}
}

func TestParseProviderParamError_SingleQuotedTopVariant(t *testing.T) {
	t.Parallel()

	rejected := ParseProviderParamError([]byte(`{"error":{"message":"Unsupported value: 'top_k' is not supported with this model."}}`))
	if !rejected["top_k"] {
		t.Fatalf("expected top_k to be rejected, got %v", rejected)
	}
}

// An apostrophe inside ordinary prose must not be read as a quote pair, or a
// harmless message would start stripping parameters the model accepts.
func TestParseProviderParamError_ApostropheIsNotAQuotePair(t *testing.T) {
	t.Parallel()

	if rejected := ParseProviderParamError([]byte(`{"error":{"message":"The model isn't available in this region and can't serve n requests."}}`)); rejected != nil {
		t.Fatalf("expected no params rejected from prose, got %v", rejected)
	}
}

// A value out of range quotes the param the way an unsupported one does, but
// the model takes the param; only this caller's number was wrong. Learning a
// strip from it would remove the param for every later caller.
func TestParseProviderParamError_ValueRangeComplaintTeachesNothing(t *testing.T) {
	for _, msg := range []string{
		`Invalid 'temperature': decimal above maximum value. Expected a value <= 2, but got 3 instead.`,
		`Invalid 'n': integer below minimum value. Expected a value >= 1, but got 0 instead.`,
		`temperature: Input should be less than or equal to 1`,
		`top_p: Input should be greater than or equal to 0`,
		`temperature: Input should be less than 2`,
		`top_k: Input should be greater than 0`,
		`'max_tokens' must be less than 8193`,
		`Invalid value for 'max_tokens': must be between 1 and 8192.`,
	} {
		body := []byte(`{"error":{"message":` + fmt.Sprintf("%q", msg) + `,"type":"invalid_request_error"}}`)
		if rejected := ParseProviderParamError(body); len(rejected) != 0 {
			t.Errorf("%q: learned %v, want nothing", msg, rejected)
		}
	}
	// A value the model refuses outright is still the param's rejection.
	body := []byte(`{"error":{"message":"Unsupported value: 'temperature' does not support 0 with this model. Only the default (1) value is supported."}}`)
	if rejected := ParseProviderParamError(body); !rejected["temperature"] {
		t.Errorf("unsupported-value refusal no longer learned: %v", rejected)
	}
}

// Regression pin: a model that refuses one reasoning_effort value still takes
// the others, so the refusal is handed back to the caller and nothing is
// learned. Each phrasing below is recognised by a different arm of the rule.
func TestParseProviderParamError_EnumValueRefusalTeachesNothing(t *testing.T) {
	t.Parallel()

	for _, msg := range []string{
		`Unsupported value: 'reasoning_effort' does not support 'none' with this model.`,
		`Invalid value for 'reasoning_effort'. Supported values are: 'low' and 'high'.`,
		"`reasoning_effort` must be one of [low, medium, high]",
		`Invalid 'reasoning_effort': expected one of low, medium, high`,
		`'reasoning_effort': Input should be 'low', 'medium' or 'high'`,
		`[{'type': 'literal_error', 'loc': ('body', 'reasoning_effort'), 'input': 'none'}]`,
	} {
		body := []byte(`{"error":{"message":` + fmt.Sprintf("%q", msg) + `,"type":"invalid_request_error","param":"reasoning_effort","code":"unsupported_value"}}`)
		if rejected := ParseProviderParamError(body); rejected["reasoning_effort"] {
			t.Errorf("%q: learned reasoning_effort as a strip, want nothing", msg)
		}
	}
}

// Regression pin: a refusal of the param itself is still learned, including
// one worded "does not support '<param>'" and one sharing a 400 with another
// param's list of supported values; so is temperature's numeric refusal.
func TestParseProviderParamError_ParamRefusalStillLearned(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ msg, param string }{
		{`Unsupported value: 'temperature' does not support 0 with this model. Only the default (1) value is supported.`, "temperature"},
		{`Unrecognized request argument supplied: 'reasoning_effort'`, "reasoning_effort"},
		{`Unsupported parameter: 'reasoning_effort' is not supported with this model.`, "reasoning_effort"},
		{`This model does not support 'reasoning_effort'.`, "reasoning_effort"},
		{`Model does not support 'reasoning_effort' parameter`, "reasoning_effort"},
		{`Unsupported parameter: 'reasoning_effort' is not supported with this model. Invalid value for 'top_p'. Supported values are: 1.`, "reasoning_effort"},
		{`Invalid value for 'top_p'. Supported values are: 1.; Unsupported parameter: 'reasoning_effort' is not supported.`, "reasoning_effort"},
	} {
		body := []byte(`{"error":{"message":` + fmt.Sprintf("%q", tc.msg) + `}}`)
		if rejected := ParseProviderParamError(body); !rejected[tc.param] {
			t.Errorf("%q: learned %v, want %s", tc.msg, rejected, tc.param)
		}
	}
}

// The dashboard hides the reasoning control on a provider type whose
// PROVIDER_PARAM_INCOMPATIBILITY entry names reasoning_effort. Hiding it where
// the backend forwards the param makes None unreachable on the one type that
// honours it; showing it where the backend strips the param offers a switch
// that does nothing. Every type this package strips for must have an entry
// there that agrees.
func TestDashboardReasoningEffortMatchesStrips(t *testing.T) {
	const tablePath = "../../web/src/utils/paramCompat.ts"
	raw, err := os.ReadFile(tablePath)
	if err != nil {
		t.Fatalf("read %s: %v", tablePath, err)
	}
	source := string(raw)
	start := strings.Index(source, "PROVIDER_PARAM_INCOMPATIBILITY")
	end := -1
	if start >= 0 {
		end = strings.Index(source[start:], "\n};")
	}
	if end < 0 {
		t.Fatalf("PROVIDER_PARAM_INCOMPATIBILITY not found in %s", tablePath)
	}
	table := source[start : start+end]
	dashboard := map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\t"?([a-z0-9-]+)"?: \{([^}]*)\}`).FindAllStringSubmatch(table, -1) {
		dashboard[m[1]] = m[2]
	}
	for typ := range ProviderUnsupportedParams {
		if _, ok := dashboard[typ]; !ok {
			t.Errorf("provider type %q has no entry in %s", typ, tablePath)
		}
	}
	// A type the backend has no list for strips nothing, so the dashboard
	// must not hide reasoning_effort on it either.
	for typ, rules := range dashboard {
		strips := slices.Contains(ProviderUnsupportedParams[typ], "reasoning_effort")
		hides := strings.Contains(rules, "reasoning_effort:")
		if strips != hides {
			t.Errorf("provider type %q: backend strips reasoning_effort = %v, dashboard hides it = %v", typ, strips, hides)
		}
	}
}
