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

// A member that is not a count in any spelling is still a decode failure. The
// tolerance is for how a number is written, not for a field holding something
// else entirely.
func TestUsage_NonNumericCountIsStillAnError(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		`{"prompt_tokens":"lots"}`,
		`{"prompt_tokens":{"value":12}}`,
		`{"prompt_tokens":[12]}`,
		`{"prompt_tokens":true}`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			var u Usage
			if err := json.Unmarshal([]byte(raw), &u); err == nil {
				t.Errorf("decoded %s as a count: %d", raw, u.PromptTokens)
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
