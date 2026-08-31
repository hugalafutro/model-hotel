package proxy

import (
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// Provider-reported usage is the one input on the metering path the gateway
// does not author. Before the clamp a negative member drew tokens_used down, a
// member at 2^31 put an owner's TPM bucket weeks in debt, and a plain JSON
// integer near 2^63 wrapped the charge sum and failed the int4 request-log
// UPDATE. These tests pin the bound at every reader and at the charge.

// The clamp's own table lives in internal/util, where the shared definition
// is. This pins only that the proxy's alias is that definition, so a
// re-definition here could not drift from it unnoticed.
func TestClampTokenCount_IsTheSharedDefinition(t *testing.T) {
	if maxSaneTokenCount != util.MaxSaneTokenCount {
		t.Errorf("ceiling = %d, want the shared %d", maxSaneTokenCount, util.MaxSaneTokenCount)
	}
	for _, n := range []int{0, 1, maxSaneTokenCount, maxSaneTokenCount + 1, -500, math.MaxInt64, math.MinInt64} {
		if got, want := clampTokenCount(n), util.ClampTokenCount(n); got != want {
			t.Errorf("clampTokenCount(%d) = %d, want util's %d", n, got, want)
		}
	}
}

func TestSanitizeUsageCounts(t *testing.T) {
	p, c, r := sanitizeUsageCounts(-500, math.MaxInt64, 7)
	if p != 0 || c != maxSaneTokenCount || r != 7 {
		t.Errorf("got (%d, %d, %d), want (0, %d, 7)", p, c, r, maxSaneTokenCount)
	}
}

func TestIsTokenReading(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want bool
	}{
		{0, false}, {-1, false}, {1, true}, {maxSaneTokenCount, true}, {maxSaneTokenCount + 1, false}, {math.MaxInt64, false},
	} {
		if got := isTokenReading(tc.in); got != tc.want {
			t.Errorf("isTokenReading(%d) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// The decoder accepts a quoted count and a quoted negative; the bound must
// hold for whatever spelling the decoder let through.
func TestSanitizeUsageCounts_SpelledForms(t *testing.T) {
	var u Usage
	if err := u.UnmarshalJSON([]byte(`{"prompt_tokens":"-500","completion_tokens":"2147483647","total_tokens":0}`)); err != nil {
		t.Fatalf("decode: %v", err)
	}
	p, c, _ := sanitizeUsageCounts(u.PromptTokens, u.CompletionTokens, 0)
	if p != 0 || c != maxSaneTokenCount {
		t.Errorf("got (%d, %d), want (0, %d)", p, c, maxSaneTokenCount)
	}
}

func TestRecordTokenUsage_ClampsTheCharge(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo
	logData := &requestLogData{virtualKeyName: "test-key"}

	h.recordTokenUsage("test-hash", logData, -500, -100, 0)
	if n := len(vkRepo.addTokensCalls); n != 1 || vkRepo.addTokensCalls[0].tokens != 0 {
		t.Fatalf("negative members: calls=%+v, want one call charging 0", vkRepo.addTokensCalls)
	}

	vkRepo.addTokensCalls = nil
	h.recordTokenUsage("test-hash", logData, math.MaxInt64, math.MaxInt64, math.MaxInt64)
	if n := len(vkRepo.addTokensCalls); n != 1 || vkRepo.addTokensCalls[0].tokens != maxSaneTokenCount {
		t.Fatalf("overflowing members: calls=%+v, want one call charging the ceiling %d", vkRepo.addTokensCalls, maxSaneTokenCount)
	}

	vkRepo.addTokensCalls = nil
	h.recordTokenUsage("test-hash", logData, 60_000_000, 60_000_000, 0)
	if vkRepo.addTokensCalls[0].tokens != maxSaneTokenCount {
		t.Errorf("in-range members whose sum is over the ceiling charged %d, want %d", vkRepo.addTokensCalls[0].tokens, maxSaneTokenCount)
	}

	vkRepo.addTokensCalls = nil
	h.recordTokenUsage("test-hash", logData, 50, 25, 5)
	if vkRepo.addTokensCalls[0].tokens != 80 {
		t.Errorf("a real charge changed: %d, want 80", vkRepo.addTokensCalls[0].tokens)
	}
}

func TestObserveUsage_RefusesOutOfRangeMembers(t *testing.T) {
	st := &streamState{}
	st.observeUsage(&Usage{PromptTokens: 12, CompletionTokens: 34, CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: 3}})
	if st.promptTokens != 12 || st.completionTokens != 34 || st.reasoningTokens != 3 {
		t.Fatalf("a real reading did not land: %+v", st)
	}
	st.observeUsage(&Usage{PromptTokens: -500, CompletionTokens: -100, CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: -7}})
	st.observeUsage(&Usage{PromptTokens: math.MaxInt32, CompletionTokens: math.MaxInt64, CompletionTokensDetails: &CompletionTokensDetails{ReasoningTokens: maxSaneTokenCount + 1}})
	if st.promptTokens != 12 || st.completionTokens != 34 || st.reasoningTokens != 3 {
		t.Errorf("an out-of-range member replaced a good reading: %+v", st)
	}
}

func TestExtractCacheTokens_Clamps(t *testing.T) {
	hit, miss := extractCacheTokens(Usage{PromptTokens: math.MaxInt64, PromptCacheHitTokens: math.MaxInt64})
	if hit != maxSaneTokenCount || miss != 0 {
		t.Errorf("OpenAI split = (%d, %d), want (%d, 0)", hit, miss, maxSaneTokenCount)
	}
	hit, miss = extractCacheTokens(Usage{PromptTokens: -5, CacheReadInputTokens: 10})
	if hit != 10 || miss != 0 {
		t.Errorf("negative prompt with a cache read = (%d, %d), want (10, 0)", hit, miss)
	}
	hit, miss = extractCacheTokens(Usage{PromptTokens: 100, PromptTokensDetails: &PromptTokensDetails{CachedTokens: 40}})
	if hit != 40 || miss != 60 {
		t.Errorf("a real split changed: (%d, %d), want (40, 60)", hit, miss)
	}
	if hit, miss = extractCacheTokens(Usage{PromptTokens: 100}); hit != 0 || miss != 0 {
		t.Errorf("no cache members = (%d, %d), want (0, 0)", hit, miss)
	}
}

func TestExtractPassthroughUsage_Clamps(t *testing.T) {
	p, c := extractPassthroughUsage([]byte(`{"usage":{"prompt_tokens":-5,"completion_tokens":9223372036854775807}}`))
	if p != 0 || c != maxSaneTokenCount {
		t.Errorf("got (%d, %d), want (0, %d)", p, c, maxSaneTokenCount)
	}
	if p, c = extractPassthroughUsage([]byte(`{"usage":{"input_tokens":7,"output_tokens":3}}`)); p != 7 || c != 3 {
		t.Errorf("a real block changed: (%d, %d), want (7, 3)", p, c)
	}
}

// nonStreamingUsageFixture drives handleNonStreamingResponse with a 200 whose
// usage block is under test and returns the row figures, the charge, and the
// body the caller was served.
func nonStreamingUsageFixture(t *testing.T, usage string) (logData *requestLogData, charged []addTokensCall, clientBody []byte) {
	t.Helper()
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	body := `{"id":"chatcmpl_x","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}],"usage":` + usage + `}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
	req := withAuthContext(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", http.NoBody))
	logData = &requestLogData{modelID: "m", providerName: "p", virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "pending"}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	rec := httptest.NewRecorder()
	h.handleNonStreamingResponse(rec, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)
	return logData, vkRepo.addTokensCalls, rec.Body.Bytes()
}

func TestHandleNonStreamingResponse_NegativeUsageNeverCredits(t *testing.T) {
	logData, charged, _ := nonStreamingUsageFixture(t, `{"prompt_tokens":-500,"completion_tokens":-100,"total_tokens":-600}`)
	if logData.tokensPrompt != 0 || logData.tokensCompletion != 0 {
		t.Errorf("row recorded negative figures: prompt=%d completion=%d", logData.tokensPrompt, logData.tokensCompletion)
	}
	if len(charged) != 1 || charged[0].tokens < 0 {
		t.Fatalf("charge = %+v, want one non-negative call", charged)
	}
	// The completion was delivered, so the estimator charges for it rather
	// than letting a negative report make the answer free.
	if charged[0].tokens == 0 {
		t.Errorf("a delivered answer with a negative report was charged nothing")
	}
}

func TestHandleNonStreamingResponse_OverflowUsageCappedEverywhere(t *testing.T) {
	logData, charged, clientBody := nonStreamingUsageFixture(t, `{"prompt_tokens":9223372036854775807,"completion_tokens":"2147483647","total_tokens":9223372036854775807,"completion_tokens_details":{"reasoning_tokens":9223372036854775807}}`)
	if logData.tokensPrompt != maxSaneTokenCount || logData.tokensCompletion != maxSaneTokenCount {
		t.Errorf("row = (%d, %d), want both at the ceiling %d", logData.tokensPrompt, logData.tokensCompletion, maxSaneTokenCount)
	}
	if logData.tokensCompletionReasoning != maxSaneTokenCount {
		t.Errorf("row reasoning = %d, want the ceiling %d", logData.tokensCompletionReasoning, maxSaneTokenCount)
	}
	if len(charged) != 1 || charged[0].tokens != maxSaneTokenCount {
		t.Errorf("charge = %+v, want one call at the ceiling %d", charged, maxSaneTokenCount)
	}
	// The caller's body is NOT rewritten. The bound is on what becomes this
	// gateway's state; the provider's own block goes through as it was sent,
	// because a partial rewrite of a nine-member block hands the caller an
	// arithmetic no provider produced, and the gateway has no standing to
	// restate someone else's usage report.
	var served struct {
		Usage struct {
			PromptTokens            int64 `json:"prompt_tokens"`
			TotalTokens             int64 `json:"total_tokens"`
			CompletionTokensDetails *struct {
				ReasoningTokens int64 `json:"reasoning_tokens"`
			} `json:"completion_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(clientBody, &served); err != nil {
		t.Fatalf("client body did not decode: %v; body: %.200s", err, clientBody)
	}
	if served.Usage.PromptTokens != math.MaxInt64 || served.Usage.TotalTokens != math.MaxInt64 {
		t.Errorf("client usage = (prompt %d, total %d), want the provider's own figures untouched",
			served.Usage.PromptTokens, served.Usage.TotalTokens)
	}
	if served.Usage.CompletionTokensDetails == nil || served.Usage.CompletionTokensDetails.ReasoningTokens != math.MaxInt64 {
		t.Errorf("client reasoning detail = %+v, want the provider's own figure untouched", served.Usage.CompletionTokensDetails)
	}
}

func TestHandleNativeNonStreaming_ClampsUsage(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	// input_tokens, cache_read_input_tokens and cache_creation_input_tokens are
	// summed into the prompt figure and differenced into the cache split, so
	// all five columns this path writes are under test at once.
	body := `{"id":"msg_up","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":-9,"cache_read_input_tokens":2147483647,"cache_creation_input_tokens":2147483647,"output_tokens":9223372036854775807}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
	native := true
	aw := newAnthropicResponseWriter(httptest.NewRecorder(), "msg_ignored", "m")
	aw.bindNativeFlag(&native)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", http.NoBody)
	logData := &requestLogData{id: uuid.New().String(), modelID: "claude-x", virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming"}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	if outcome := h.handleNativeNonStreaming(aw, req, st, resp, 1, 10.0); outcome != outcomeServed {
		t.Fatalf("outcome = %v, want outcomeServed", outcome)
	}
	aw.Finalize()

	if logData.tokensPrompt != maxSaneTokenCount || logData.tokensCompletion != maxSaneTokenCount {
		t.Errorf("row = (%d, %d), want both at the ceiling %d", logData.tokensPrompt, logData.tokensCompletion, maxSaneTokenCount)
	}
	// The cache split is written from the same parse and lands in two more
	// int4 columns: an unclamped miss (input + cache_creation) failed the
	// whole terminal UPDATE and stranded the row.
	if logData.tokensPromptCacheHit != maxSaneTokenCount || logData.tokensPromptCacheMiss != maxSaneTokenCount {
		t.Errorf("cache split = (%d, %d), want both at the ceiling %d", logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss, maxSaneTokenCount)
	}
	if len(vkRepo.addTokensCalls) != 1 || vkRepo.addTokensCalls[0].tokens != maxSaneTokenCount {
		t.Errorf("charge = %+v, want one call at the ceiling", vkRepo.addTokensCalls)
	}
}

// TestHandleNativeNonStreaming_NegativeUsageNeverCredits is the other
// direction at the same site: with no overflowing addend to swamp them, the
// negative members must fold to zero rather than through it.
func TestHandleNativeNonStreaming_NegativeUsageNeverCredits(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	body := `{"id":"msg_up","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":-500,"cache_read_input_tokens":10,"output_tokens":-100}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
	native := true
	aw := newAnthropicResponseWriter(httptest.NewRecorder(), "msg_ignored", "m")
	aw.bindNativeFlag(&native)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", http.NoBody)
	logData := &requestLogData{id: uuid.New().String(), modelID: "claude-x", virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming"}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	if outcome := h.handleNativeNonStreaming(aw, req, st, resp, 1, 10.0); outcome != outcomeServed {
		t.Fatalf("outcome = %v, want outcomeServed", outcome)
	}
	aw.Finalize()

	// prompt is -500 + 10 = -490, miss is -500, completion is -100: every one
	// folds to zero, and none of the five columns goes negative.
	for name, got := range map[string]int{
		"tokensPrompt":          logData.tokensPrompt,
		"tokensCompletion":      logData.tokensCompletion,
		"tokensPromptCacheMiss": logData.tokensPromptCacheMiss,
	} {
		if got != 0 {
			t.Errorf("%s = %d, want 0", name, got)
		}
	}
	if logData.tokensPromptCacheHit != 10 {
		t.Errorf("tokensPromptCacheHit = %d, want the real reading 10", logData.tokensPromptCacheHit)
	}
	if len(vkRepo.addTokensCalls) != 1 || vkRepo.addTokensCalls[0].tokens < 0 {
		t.Errorf("charge = %+v, want one non-negative call", vkRepo.addTokensCalls)
	}
}

// TestEmitRawData_RefusesOutOfRangeNativeStreamFigures pins that the native
// stream judges a usage member exactly as the translated stream's observer
// does: an out-of-range figure is REFUSED, not clamped.
//
// The distinction is the whole point. Clamping here let a message_delta
// carrying MaxInt64 charge the ceiling on /v1/messages while the byte-identical
// figure on an OpenAI-shaped stream was discarded and the estimator charged
// the delivered bytes: the same attack, two answers, depending only on which
// dialect the caller used.
func TestEmitRawData_RefusesOutOfRangeNativeStreamFigures(t *testing.T) {
	h := &Handler{}
	sink := newStreamSink(httptest.NewRecorder())
	st := &streamState{}
	logData := &requestLogData{}
	emit := func(payload string) {
		t.Helper()
		if stop := h.emitRawData(sink, st, sseEvent{raw: []byte("data: " + payload), payload: payload}, 1, logData); stop {
			t.Fatalf("emitRawData stopped on %s", payload)
		}
	}

	// A real reading lands.
	emit(`{"type":"message_start","message":{"usage":{"input_tokens":100,"cache_read_input_tokens":40,"output_tokens":5}}}`)
	if st.promptTokens != 140 || st.completionTokens != 5 {
		t.Fatalf("a real reading did not land: prompt=%d completion=%d", st.promptTokens, st.completionTokens)
	}
	if st.promptCacheHitTokens != 40 || st.promptCacheMissTokens != 100 {
		t.Fatalf("cache split = (%d, %d), want (40, 100)", st.promptCacheHitTokens, st.promptCacheMissTokens)
	}

	// A prompt summed past the bound from members the decoder each accepted is
	// not a reading, so the earlier one stands. Each member is judged on its
	// own, exactly as the translated observer judges them, so this event's
	// in-range output IS a reading and replaces the earlier 5.
	emit(`{"type":"message_start","message":{"usage":{"input_tokens":2147483647,"cache_read_input_tokens":2147483647,"cache_creation_input_tokens":2147483647,"output_tokens":7}}}`)
	if st.promptTokens != 140 {
		t.Errorf("an out-of-range summed prompt replaced the reading: %d", st.promptTokens)
	}
	if st.completionTokens != 7 {
		t.Errorf("an in-range output beside a refused prompt was dropped: %d", st.completionTokens)
	}

	// Neither a negative nor an absurd output figure is a reading.
	for _, payload := range []string{
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":-700}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":9223372036854775807}}`,
	} {
		emit(payload)
		if st.completionTokens != 7 {
			t.Errorf("%s replaced the reading: %d", payload, st.completionTokens)
		}
	}

	// The same figures through the translated observer reach the same verdict.
	other := &streamState{}
	other.observeUsage(&Usage{PromptTokens: 140, CompletionTokens: 7})
	other.observeUsage(&Usage{PromptTokens: math.MaxInt64, CompletionTokens: math.MaxInt64})
	if other.promptTokens != st.promptTokens || other.completionTokens != st.completionTokens {
		t.Errorf("dialects disagree: native (%d, %d) vs translated (%d, %d)",
			st.promptTokens, st.completionTokens, other.promptTokens, other.completionTokens)
	}
}
