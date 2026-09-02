package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// buildNativeAnthropicRequest forwards the original Messages body (model
// rewritten) to the provider's native /v1/messages with anthropic auth headers.
func TestBuildNativeAnthropicRequest(t *testing.T) {
	h := &Handler{}
	st := &requestState{anthropicRawBody: []byte(`{"model":"hotel/claude-x","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)}
	cand := modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "claude-opus-4-8"},
		provider: &provider.Provider{ID: uuid.New(), Name: "Anthropic", BaseURL: "https://api.anthropic.com"},
		apiKey:   "sk-ant-test",
	}
	req, ptype, url, err := h.buildNativeAnthropicRequest(context.Background(), st, cand, "anthropic")
	if err != nil {
		t.Fatalf("buildNativeAnthropicRequest: %v", err)
	}
	if ptype != "anthropic" {
		t.Errorf("ptype = %q, want anthropic", ptype)
	}
	if !strings.HasSuffix(url, "/v1/messages") {
		t.Errorf("url = %q, want suffix /v1/messages", url)
	}
	body, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(body), `"claude-opus-4-8"`) || strings.Contains(string(body), "hotel/claude-x") {
		t.Errorf("body model not rewritten: %s", body)
	}
	if req.Header.Get("x-api-key") == "" {
		t.Error("missing x-api-key header")
	}
	if req.Header.Get("Content-Type") != "application/json" {
		t.Errorf("content-type = %q", req.Header.Get("Content-Type"))
	}
}

// handleNativeNonStreaming forwards the Anthropic message verbatim and meters
// from its usage block.
func TestHandleNativeNonStreaming(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	anthropicBody := `{"id":"msg_up","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":3}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(anthropicBody)), Header: make(http.Header)}

	rec := httptest.NewRecorder()
	native := true
	aw := newAnthropicResponseWriter(rec, "msg_ignored", "m")
	aw.bindNativeFlag(&native)

	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "claude-x",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	st := &requestState{startTime: time.Now(), logData: logData}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	outcome := h.handleNativeNonStreaming(aw, req, st, resp, 1, 10.0)
	aw.Finalize()

	if outcome != outcomeServed {
		t.Errorf("outcome = %v, want outcomeServed", outcome)
	}
	if logData.state != "completed" {
		t.Errorf("state = %q, want completed", logData.state)
	}
	if logData.tokensPrompt != 9 || logData.tokensCompletion != 3 {
		t.Errorf("usage = (%d,%d), want (9,3)", logData.tokensPrompt, logData.tokensCompletion)
	}
	if rec.Body.String() != anthropicBody {
		t.Errorf("verbatim body mismatch:\n got %s\nwant %s", rec.Body.String(), anthropicBody)
	}
}

// deliveredContent is what clears a model's gone-strike streak, so a native 200
// is judged on content and not on bytes. `200 {"content":[]}` is what an
// aggregator in front of a retired model returns between its refusals, and
// crediting it would stop a streak ever reaching three CONSECUTIVE strikes — the
// model would then never be nominated and never probed.
func TestHandleNativeNonStreaming_JudgesContentNotBytes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"content block", `{"id":"m","type":"message","content":[{"type":"text","text":"hi"}],"usage":{"input_tokens":9,"output_tokens":3}}`, true},
		// No block came back but the provider reported output: a model that
		// spent its whole budget on reasoning still answered.
		{"tokens without a block", `{"id":"m","type":"message","content":[],"usage":{"input_tokens":9,"output_tokens":3}}`, true},
		{"empty message", `{"id":"m","type":"message","content":[],"usage":{"input_tokens":9,"output_tokens":0}}`, false},
		// Nonempty bytes carrying nothing at all, which the byte-counting
		// version credited as the model answering.
		{"not a message", `{}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			t.Cleanup(func() { stopUnitHandler(h) })

			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}
			rec := httptest.NewRecorder()
			native := true
			aw := newAnthropicResponseWriter(rec, "msg_j", "m")
			aw.bindNativeFlag(&native)
			req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
			logData := &requestLogData{
				id:             uuid.New().String(),
				modelID:        "claude-x",
				virtualKeyName: "test-key",
				virtualKeyID:   "00000000-0000-0000-0000-000000000001",
				state:          "streaming",
			}
			st := &requestState{startTime: time.Now(), logData: logData}

			if got := h.handleNativeNonStreaming(aw, req, st, resp, 1, 10.0); got != outcomeServed {
				t.Fatalf("outcome = %v, want outcomeServed", got)
			}
			aw.Finalize()

			if logData.deliveredContent != tc.want {
				t.Errorf("deliveredContent = %v, want %v", logData.deliveredContent, tc.want)
			}
			// The body still reaches the client verbatim whatever the verdict:
			// this judgement is about the retirement streak, not about what is
			// forwarded.
			if rec.Body.String() != tc.body {
				t.Errorf("body = %s, want %s", rec.Body.String(), tc.body)
			}
		})
	}
}

// errorReadCloser fails on Read, simulating an upstream body that drops
// mid-transfer after a 200 header.
type errorReadCloser struct{}

func (errorReadCloser) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (errorReadCloser) Close() error             { return nil }

// A read failure on the native non-streaming 200 body must finalize the log row
// (state=failed) instead of leaving it orphaned in the in-flight state.
func TestHandleNativeNonStreaming_ReadErrorFinalizesLog(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	resp := &http.Response{StatusCode: http.StatusOK, Body: errorReadCloser{}, Header: make(http.Header)}
	rec := httptest.NewRecorder()
	native := true
	aw := newAnthropicResponseWriter(rec, "msg_e", "m")
	aw.bindNativeFlag(&native)
	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "claude-x",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	st := &requestState{startTime: time.Now(), logData: logData}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	outcome := h.handleNativeNonStreaming(aw, req, st, resp, 1, 10.0)
	aw.Finalize()

	if outcome != outcomeFatal {
		t.Errorf("outcome = %v, want outcomeFatal", outcome)
	}
	if logData.state != "failed" {
		t.Errorf("state = %q, want failed (log row must not orphan)", logData.state)
	}
	if logData.errorKind != KindProviderError {
		t.Errorf("errorKind = %v, want KindProviderError", logData.errorKind)
	}
	if logData.statusCode != http.StatusBadGateway {
		t.Errorf("statusCode = %d, want 502", logData.statusCode)
	}
	if rec.Code != http.StatusBadGateway {
		t.Errorf("client status = %d, want 502", rec.Code)
	}
}

// runNativeStream drives a complete Anthropic SSE body through the real streaming
// pipeline with rawPassthrough enabled (the native passthrough path), returning
// the forwarded client bytes and the finalized log row. It mirrors the harness in
// ttft_stall_test.go.
func runNativeStream(t *testing.T, sseBody string) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	return runNativeStreamWith(t, sseBody, credentialMasker{})
}

func runNativeStreamWith(t *testing.T, sseBody string, masker credentialMasker) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(sseBody))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "claude-test",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	opts := streamOptions{
		responseHeaderMs: 10.0,
		vkHash:           "test-hash",
		attempt:          1,
		rawPassthrough:   true,
		masker:           masker,
	}
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), opts)
	return w, logData
}

const nativeStreamHead = `event: message_start
data: {"type":"message_start","message":{"id":"msg_up","type":"message","role":"assistant","model":"claude","content":[],"usage":{"input_tokens":12,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

`

// Happy path: a full stream ending in message_stop logs completed, meters usage
// from message_start/message_delta, and forwards the Anthropic frames verbatim.
func TestNativeStream_CompletedWithUsage(t *testing.T) {
	body := nativeStreamHead + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	w, logData := runNativeStream(t, body)

	if logData.state != "completed" {
		t.Errorf("state = %q, want completed (err: %s)", logData.state, logData.errorMessage)
	}
	if logData.tokensPrompt != 12 || logData.tokensCompletion != 5 {
		t.Errorf("usage = (%d,%d), want (12,5)", logData.tokensPrompt, logData.tokensCompletion)
	}
	out := w.Body.String()
	// Verbatim framing reconstruction: event lines AND data lines pass through,
	// and no OpenAI [DONE] sentinel is injected.
	for _, want := range []string{"event: message_start", `"type":"text_delta"`, "Hello", "event: message_stop"} {
		if !strings.Contains(out, want) {
			t.Errorf("forwarded body missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "[DONE]") {
		t.Errorf("native stream must not inject [DONE]:\n%s", out)
	}
}

// A clean EOF that never delivered message_stop is a mid-stream truncation: it
// must log failed (not completed), or the partial output would be billed as a
// complete response.
func TestNativeStream_TruncatedBeforeMessageStop(t *testing.T) {
	_, logData := runNativeStream(t, nativeStreamHead) // no message_stop

	if logData.state != "failed" {
		t.Errorf("state = %q, want failed (truncated before message_stop)", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "message_stop") {
		t.Errorf("errorMessage = %q, want it to mention message_stop", logData.errorMessage)
	}
	if logData.errorKind != KindProviderError {
		t.Errorf("errorKind = %v, want KindProviderError", logData.errorKind)
	}
}

// A provider-sent error event must be both forwarded to the client (verbatim)
// AND recorded so the request logs failed with the provider's message.
func TestNativeStream_ProviderErrorEvent(t *testing.T) {
	body := nativeStreamHead + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	w, logData := runNativeStream(t, body)

	if logData.state != "failed" {
		t.Errorf("state = %q, want failed (provider error event)", logData.state)
	}
	if logData.errorMessage != "Overloaded" {
		t.Errorf("errorMessage = %q, want Overloaded", logData.errorMessage)
	}
	if logData.errorKind != KindProviderError {
		t.Errorf("errorKind = %v, want KindProviderError", logData.errorKind)
	}
	// The client still sees the error frame.
	if !strings.Contains(w.Body.String(), "overloaded_error") {
		t.Errorf("error frame not forwarded to client:\n%s", w.Body.String())
	}
}

// The native passthrough forwards the error frame raw, so it needs the same
// credential scrub as the translated path; the request log keeps the original.
func TestNativeStream_ProviderErrorEventMasksKeyShapedTokens(t *testing.T) {
	const planted = "sk-ant-api03-STANDARDKEY1234567890abcdef1234567890"
	body := nativeStreamHead + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"invalid x-api-key " + planted + "\"}}\n\n"
	w, logData := runNativeStream(t, body)

	if strings.Contains(w.Body.String(), planted) {
		t.Fatalf("operator credential reached the client via the native error frame:\n%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `data: {"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key [redacted]"}}`) {
		t.Errorf("masked error frame not forwarded to client:\n%s", w.Body.String())
	}
	if logData.state != "failed" {
		t.Errorf("state = %q, want failed", logData.state)
	}
	if strings.Contains(logData.errorMessage, planted) {
		t.Errorf("key-shaped token reached the request log, got %q", logData.errorMessage)
	}
}

// Same contract on the native passthrough: a custom-shaped key is scrubbed by
// exact value, since no prefix would catch it.
func TestNativeStream_ProviderErrorEventMasksExactProviderKey(t *testing.T) {
	const secret = "myCustomGatewayKey2024x9z8"
	body := nativeStreamHead +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"your key is " + secret + "\"}}\n\n" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"authentication_error\",\"message\":\"invalid x-api-key " + secret + "\"}}\n\n"
	w, logData := runNativeStreamWith(t, body, newCredentialMasker(secret))

	if strings.Contains(w.Body.String(), secret) {
		t.Fatalf("exact provider key reached the client via the native stream:\n%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"text":"your key is [redacted]"`) {
		t.Errorf("content event not exact-masked:\n%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"message":"invalid x-api-key [redacted]"`) {
		t.Errorf("masked error frame not forwarded to client:\n%s", w.Body.String())
	}
	if strings.Contains(logData.errorMessage, secret) || !strings.Contains(logData.errorMessage, "[redacted]") {
		t.Errorf("request log must carry the error with the exact key redacted, got %q", logData.errorMessage)
	}
}

// The native non-streaming body is content; a gateway quoting its key there is
// scrubbed by exact value from the per-attempt masker stamped on the log row.
func TestHandleNativeNonStreaming_MasksExactProviderKey(t *testing.T) {
	const secret = "myCustomGatewayKey2024x9z8"
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"your key is ` + secret + `"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":4}}`
	rec, _ := runNativeNonStreamingWith(t, body, newCredentialMasker(secret))
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("exact provider key reached the client via the native non-streaming body:\n%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"text":"your key is [redacted]"`) {
		t.Errorf("content not exact-masked:\n%s", rec.Body.String())
	}
}

// warmCacheStream is a native stream whose message_start reports a warm cache:
// a tiny uncached remainder beside a large cache read and a cache write.
const warmCacheStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_up","type":"message","role":"assistant","model":"claude","content":[],"usage":{"input_tokens":4,"output_tokens":0,"cache_creation_input_tokens":30,"cache_read_input_tokens":20000}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`

// The native passthrough meters the cache-inclusive prompt, so it must record
// the hit/miss split too — without it every cached token is priced at the full
// input rate, which is a different skew from under-counting, not a fixed one.
// The translated path reaches these same fields via extractCacheTokens.
func TestNativeStream_RecordsCacheSplit(t *testing.T) {
	_, logData := runNativeStream(t, warmCacheStream)

	if logData.state != "completed" {
		t.Fatalf("state = %q, want completed (err: %s)", logData.state, logData.errorMessage)
	}
	if logData.tokensPrompt != 20034 {
		t.Errorf("tokensPrompt = %d, want 20034 (4 + 20000 + 30)", logData.tokensPrompt)
	}
	if logData.tokensPromptCacheHit != 20000 {
		t.Errorf("tokensPromptCacheHit = %d, want 20000", logData.tokensPromptCacheHit)
	}
	if logData.tokensPromptCacheMiss != 34 {
		t.Errorf("tokensPromptCacheMiss = %d, want 34 (4 + 30)", logData.tokensPromptCacheMiss)
	}
}

// runNativeNonStreaming serves one native Anthropic 200 body through
// handleNativeNonStreaming and returns the finalized log row.
func runNativeNonStreaming(t *testing.T, anthropicBody string) *requestLogData {
	t.Helper()
	_, logData := runNativeNonStreamingWith(t, anthropicBody, credentialMasker{})
	return logData
}

func runNativeNonStreamingWith(t *testing.T, anthropicBody string, masker credentialMasker) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(anthropicBody)), Header: make(http.Header)}
	rec := httptest.NewRecorder()
	native := true
	aw := newAnthropicResponseWriter(rec, "msg_ignored", "m")
	aw.bindNativeFlag(&native)

	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	logData := &requestLogData{
		id:             uuid.New().String(),
		modelID:        "claude-x",
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
		masker:         masker,
	}
	st := &requestState{startTime: time.Now(), logData: logData}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	if outcome := h.handleNativeNonStreaming(aw, req, st, resp, 1, 10.0); outcome != outcomeServed {
		t.Fatalf("outcome = %v, want outcomeServed", outcome)
	}
	aw.Finalize()
	return rec, logData
}

// Same split on the non-streaming native path, so the two agree.
func TestHandleNativeNonStreaming_RecordsCacheSplit(t *testing.T) {
	logData := runNativeNonStreaming(t, `{"id":"msg_up","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":50,"cache_creation_input_tokens":30,"cache_read_input_tokens":20000}}`)

	if logData.tokensPrompt != 20034 || logData.tokensCompletion != 50 {
		t.Errorf("usage = (%d,%d), want (20034,50)", logData.tokensPrompt, logData.tokensCompletion)
	}
	if logData.tokensPromptCacheHit != 20000 || logData.tokensPromptCacheMiss != 34 {
		t.Errorf("cache split = (%d,%d), want (20000,34)", logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss)
	}
}

// creationOnlyStream is a native stream whose message_start reports a cache
// WRITE and no read.
const creationOnlyStream = `event: message_start
data: {"type":"message_start","message":{"id":"msg_up","type":"message","role":"assistant","model":"claude","content":[],"usage":{"input_tokens":4,"output_tokens":0,"cache_creation_input_tokens":30}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`

// Writing a cache entry without reading one records NO cache split, on both
// native paths. The translated egress path meters the identical response
// through extractCacheTokens, which keys off the cache-READ fields alone and
// can only ever record (0,0) here — so recording a split would put the two
// Anthropic paths back into disagreement. The creation tokens are not lost:
// they count inside tokensPrompt, which is what the request is priced on.
func TestNative_CacheCreationOnlyRecordsNoCacheSplit(t *testing.T) {
	t.Run("non-streaming", func(t *testing.T) {
		logData := runNativeNonStreaming(t, `{"id":"msg_up","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":7,"cache_creation_input_tokens":30}}`)

		if logData.tokensPrompt != 34 {
			t.Errorf("tokensPrompt = %d, want 34 (4 + 30)", logData.tokensPrompt)
		}
		if logData.tokensPromptCacheHit != 0 || logData.tokensPromptCacheMiss != 0 {
			t.Errorf("cache split = (%d,%d), want (0,0)", logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss)
		}
	})

	t.Run("streaming", func(t *testing.T) {
		_, logData := runNativeStream(t, creationOnlyStream)

		if logData.tokensPrompt != 34 {
			t.Errorf("tokensPrompt = %d, want 34 (4 + 30)", logData.tokensPrompt)
		}
		if logData.tokensPromptCacheHit != 0 || logData.tokensPromptCacheMiss != 0 {
			t.Errorf("cache split = (%d,%d), want (0,0)", logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss)
		}
	})
}

// An uncached response records NO cache counts on either native path. The
// translated egress path cannot express "miss = the whole prompt" — its usage
// omits the cache fields when they are zero — so recording it here would make
// every uncached Anthropic-in request show a cache panel the identical request
// through any other path does not, and feed a cache-miss stats series only
// native Anthropic traffic contributes to.
func TestNative_UncachedRecordsNoCacheSplit(t *testing.T) {
	t.Run("non-streaming", func(t *testing.T) {
		logData := runNativeNonStreaming(t, `{"id":"msg_up","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":3}}`)
		if logData.tokensPrompt != 9 {
			t.Errorf("tokensPrompt = %d, want 9", logData.tokensPrompt)
		}
		if logData.tokensPromptCacheHit != 0 || logData.tokensPromptCacheMiss != 0 {
			t.Errorf("cache split = (%d,%d), want (0,0)", logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss)
		}
	})

	t.Run("streaming", func(t *testing.T) {
		_, logData := runNativeStream(t, nativeStreamHead+"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
		if logData.tokensPrompt != 12 {
			t.Errorf("tokensPrompt = %d, want 12", logData.tokensPrompt)
		}
		if logData.tokensPromptCacheHit != 0 || logData.tokensPromptCacheMiss != 0 {
			t.Errorf("cache split = (%d,%d), want (0,0)", logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss)
		}
	})
}

// A Gemini thought signature riding on a tool_use id from an earlier
// translated turn is stripped before the body reaches an Anthropic provider,
// on the block and on the tool_result alike, with the ids still paired.
func TestBuildNativeAnthropicRequest_StripsSignedToolUseIDs(t *testing.T) {
	h := &Handler{}
	signed := "toolu_01" + "_thoughtsig_" + "tc2ln" // the carrier's shape: marker, text tag, base64url("sig")
	st := &requestState{anthropicRawBody: []byte(`{"model":"hotel/m","max_tokens":10,"messages":[` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":[{"type":"tool_use","id":"` + signed + `","name":"f","input":{}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + signed + `","content":"ok"}]}]}`)}
	cand := modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "claude-opus-4-8"},
		provider: &provider.Provider{ID: uuid.New(), Name: "Anthropic", BaseURL: "https://api.anthropic.com"},
		apiKey:   "sk-ant-test",
	}
	req, _, _, err := h.buildNativeAnthropicRequest(context.Background(), st, cand, "anthropic")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(req.Body)
	if strings.Contains(string(body), "_thoughtsig_") {
		t.Fatalf("signature suffix forwarded to Anthropic: %s", body)
	}
	if strings.Count(string(body), `"toolu_01"`) != 2 || !strings.Contains(string(body), `"claude-opus-4-8"`) {
		t.Errorf("ids not paired or model not rewritten: %s", body)
	}
}
