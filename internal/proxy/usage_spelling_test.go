package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// usageSpellings are the ways a count arrives that are not the plain integer the
// schema asks for. A relay that did its arithmetic in floating point emits 12.0;
// several emit the count quoted. A plain int field rejects both, and Usage has
// its own UnmarshalJSON — so the error does not merely blank the usage, it stops
// the surrounding decode where it stands.
var usageSpellings = []struct {
	name  string
	usage string
}{
	{"plain integers", `{"prompt_tokens":12,"completion_tokens":3,"total_tokens":15}`},
	{"floating point", `{"prompt_tokens":12.0,"completion_tokens":3.0,"total_tokens":15.0}`},
	{"quoted", `{"prompt_tokens":"12","completion_tokens":"3","total_tokens":"15"}`},
	{"quoted floats", `{"prompt_tokens":"12.0","completion_tokens":"3.0","total_tokens":"15.0"}`},
	{"mixed", `{"prompt_tokens":"12","completion_tokens":3.0,"total_tokens":15}`},
}

func TestUsage_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range usageSpellings {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var u Usage
			if err := json.Unmarshal([]byte(tc.usage), &u); err != nil {
				t.Fatalf("usage did not decode: %v", err)
			}
			if u.PromptTokens != 12 || u.CompletionTokens != 3 || u.TotalTokens != 15 {
				t.Errorf("got prompt=%d completion=%d total=%d, want 12/3/15", u.PromptTokens, u.CompletionTokens, u.TotalTokens)
			}
		})
	}
}

// The nested breakdowns are counts too, and the cache split is what keeps a
// cached prompt from being priced at the full input rate.
func TestUsage_NestedCountSpellings(t *testing.T) {
	t.Parallel()
	var u Usage
	raw := `{"prompt_tokens":"12","prompt_tokens_details":{"cached_tokens":"8"},"completion_tokens_details":{"reasoning_tokens":2.0}}`
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("usage did not decode: %v", err)
	}
	require.NotNil(t, u.PromptTokensDetails)
	require.NotNil(t, u.CompletionTokensDetails)
	assert.Equal(t, 8, u.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 2, u.CompletionTokensDetails.ReasoningTokens)
}

// A member that is not a count in any spelling reads as nothing. The tolerance
// is for how a number is written, not for a field holding something else — and
// no reading is invented for it, so the count stays zero and the estimator picks
// the request up. What it must not do is take the rest of the response with it.
func TestUsage_NonNumericCountReadsAsNothing(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"completion_tokens":3,"prompt_tokens":"lots"}`,
		`{"completion_tokens":3,"prompt_tokens":{"value":12}}`,
		`{"completion_tokens":3,"prompt_tokens":[12]}`,
		`{"completion_tokens":3,"prompt_tokens":true}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			var u Usage
			if err := json.Unmarshal([]byte(raw), &u); err != nil {
				t.Fatalf("usage did not decode: %v", err)
			}
			if u.PromptTokens != 0 {
				t.Errorf("invented a count of %d from %s", u.PromptTokens, raw)
			}
			if u.CompletionTokens != 3 {
				t.Errorf("the count beside it was lost: %d", u.CompletionTokens)
			}
		})
	}
}

// Counts are re-encoded as the integers the schema asks for, whatever spelling
// arrived — the non-streaming path decodes and re-emits the whole body, and the
// caller is entitled to the shape it asked this gateway for.
func TestUsage_ReencodesAsIntegers(t *testing.T) {
	t.Parallel()
	var u Usage
	require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":"12","completion_tokens":3.0,"cost":0.004}`), &u))
	out, err := json.Marshal(u)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"prompt_tokens":12`)
	assert.Contains(t, string(out), `"completion_tokens":3`)
	// The unmodelled members still ride through untouched.
	assert.Contains(t, string(out), `"cost":0.004`)
}

// End to end on the non-streaming path, which is where the damage is worst:
// Usage.UnmarshalJSON returning an error aborts the decode of the response
// object around it, so a quoted count cost the caller its answer.
func TestHandleNonStreamingResponse_QuotedUsageStillAnswers(t *testing.T) {
	vkRepo := &mockVirtualKeyRepo{}
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	h.virtualKeyRepo = vkRepo

	upstreamBody := `{"id":"x","object":"chat.completion","usage":{"prompt_tokens":"12","completion_tokens":"3"},"choices":[{"index":0,"message":{"role":"assistant","content":"Hello, world!"}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(upstreamBody)), Header: make(http.Header)}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := &requestLogData{
		modelID:        "gpt-test",
		providerID:     uuid.New(),
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "pending",
	}
	h.handleNonStreamingResponse(w, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Hello, world!")
	assert.Equal(t, 12, logData.tokensPrompt)
	assert.Equal(t, 3, logData.tokensCompletion)
	// Measured usage, so nothing is estimated: 12+3 reaches the meter.
	assert.Equal(t, 15, singleAddTokens(t, vkRepo))
}

// The streaming path meters from the usage chunk. A count it could not read was
// no longer costing the caller the frame (an untypeable frame is forwarded now),
// but it still put the request on an estimate when the provider had said exactly
// what it billed.
func TestHandleStreamingResponse_QuotedUsageIsMetered(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	body := `data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n" +
		`data: {"choices":[],"usage":{"prompt_tokens":"12","completion_tokens":3.0}}` + "\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	assert.Equal(t, 12, logData.tokensPrompt)
	assert.Equal(t, 3, logData.tokensCompletion)
}

// The retry bound has to clear the struct it exists for. Usage carries nine
// integer members and each pass fixes exactly one, and a relay that quotes one
// count quotes all of them -- so a bound of eight failed on the archetypal
// caller, which is the case the whole helper was written for.
func TestUsage_EveryCountQuoted(t *testing.T) {
	t.Parallel()
	raw := `{"prompt_tokens":"1","completion_tokens":"2","total_tokens":"3",` +
		`"prompt_cache_hit_tokens":"4","prompt_cache_miss_tokens":"5",` +
		`"cache_read_input_tokens":"6","cache_creation_input_tokens":"7",` +
		`"prompt_tokens_details":{"cached_tokens":"8"},` +
		`"completion_tokens_details":{"reasoning_tokens":"9"}}`
	var u Usage
	require.NoError(t, json.Unmarshal([]byte(raw), &u))
	assert.Equal(t, 1, u.PromptTokens)
	assert.Equal(t, 2, u.CompletionTokens)
	assert.Equal(t, 3, u.TotalTokens)
	assert.Equal(t, 4, u.PromptCacheHitTokens)
	assert.Equal(t, 5, u.PromptCacheMissTokens)
	assert.Equal(t, 6, u.CacheReadInputTokens)
	assert.Equal(t, 7, u.CacheCreationInputTokens)
	require.NotNil(t, u.PromptTokensDetails)
	require.NotNil(t, u.CompletionTokensDetails)
	assert.Equal(t, 8, u.PromptTokensDetails.CachedTokens)
	assert.Equal(t, 9, u.CompletionTokensDetails.ReasoningTokens)
}

// A usage member in a shape this gateway has no struct for is the same defect
// the streaming path fixes one level out, and it had the same cost: Usage
// returned before assigning, so counts that decoded perfectly were thrown away
// with it -- and on the non-streaming path that error stops the decode of the
// response object and the caller loses the answer.
//
// [] where an object belongs is a routine relay habit, not an exotic one.
func TestUsage_KeepsWhatItCouldRead(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":[]}`,
		`{"prompt_tokens":12,"completion_tokens":3,"completion_tokens_details":""}`,
		`{"prompt_tokens":12,"completion_tokens":3,"total_tokens":"lots"}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			var u Usage
			if err := json.Unmarshal([]byte(raw), &u); err != nil {
				t.Fatalf("usage did not decode: %v", err)
			}
			assert.Equal(t, 12, u.PromptTokens)
			assert.Equal(t, 3, u.CompletionTokens)
		})
	}
}

// Bytes that are not JSON are still an error: there is nothing to keep.
func TestUsage_MalformedIsStillAnError(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{`{"prompt_tokens":12`, `{"prompt_tokens":}`} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			var u Usage
			if err := json.Unmarshal([]byte(raw), &u); err == nil {
				t.Errorf("decoded malformed usage as %+v", u)
			}
		})
	}
}

// The whole point, end to end: a usage member the gateway cannot type must not
// cost the caller the answer the model already produced.
func TestHandleNonStreamingResponse_UnreadableUsageStillAnswers(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	h.virtualKeyRepo = &mockVirtualKeyRepo{}

	upstreamBody := `{"id":"x","object":"chat.completion","usage":{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":[]},"choices":[{"index":0,"message":{"role":"assistant","content":"Hello, world!"}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewBufferString(upstreamBody)), Header: make(http.Header)}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := &requestLogData{modelID: "gpt-test", providerID: uuid.New(), virtualKeyName: "k", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "pending"}

	h.handleNonStreamingResponse(w, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Hello, world!")
	assert.Equal(t, 12, logData.tokensPrompt)
	// And what it says about the member it could NOT read: nothing. The decoder
	// allocated that struct before it reached the value, so keeping it would
	// have the gateway emit {"cached_tokens":0} — a positive claim that nothing
	// was cached, which a cost calculator acts on — in place of whatever the
	// provider actually wrote. Absent is the honest report.
	assert.NotContains(t, w.Body.String(), "prompt_tokens_details")
}

// The pass-through surfaces (/v1/embeddings, /v1/images/*, /v1/audio/*, rerank)
// read usage with a decoder of their own, and it had the same defect: a count
// quoted or written with a fraction on it failed the whole decode and the
// request metered as zero. Same package, same fix.
func TestExtractPassthroughUsage_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		body            string
		wantIn, wantOut int
	}{
		{"plain", `{"usage":{"prompt_tokens":12,"completion_tokens":3}}`, 12, 3},
		{"quoted", `{"usage":{"prompt_tokens":"12","completion_tokens":"3"}}`, 12, 3},
		{"fractional", `{"usage":{"input_tokens":12.0,"output_tokens":3.0}}`, 12, 3},
		{"quoted total only", `{"usage":{"total_tokens":"15"}}`, 15, 0},
		// One member unreadable must not cost the counts beside it.
		{"partly unreadable", `{"usage":{"prompt_tokens":12,"completion_tokens":3,"detail":{"x":1},"total_tokens":["nope"]}}`, 12, 3},
		{"no usage", `{"data":[]}`, 0, 0},
		{"not json", `{`, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in, out := extractPassthroughUsage([]byte(tc.body))
			if in != tc.wantIn || out != tc.wantOut {
				t.Errorf("got %d/%d, want %d/%d", in, out, tc.wantIn, tc.wantOut)
			}
		})
	}
}

// One breakdown unreadable must not take the readable one with it, and a usage
// block with no breakdown at all must come through untouched.
func TestUsage_KeepsTheBreakdownItCouldRead(t *testing.T) {
	t.Parallel()
	var both Usage
	require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":12,"prompt_tokens_details":{"cached_tokens":8},"completion_tokens_details":[]}`), &both))
	require.NotNil(t, both.PromptTokensDetails, "the breakdown that decoded was dropped with the one that did not")
	assert.Equal(t, 8, both.PromptTokensDetails.CachedTokens)
	assert.Nil(t, both.CompletionTokensDetails, "a breakdown that did not decode must be reported absent")

	// A shape error with no breakdown present at all.
	var neither Usage
	require.NoError(t, json.Unmarshal([]byte(`{"prompt_tokens":12,"total_tokens":[1]}`), &neither))
	assert.Equal(t, 12, neither.PromptTokens)
	assert.Nil(t, neither.PromptTokensDetails)
}
