package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestHandleStreamingResponse_GoldenSeparators is the byte-exact characterization
// net for the streaming-pipeline refactor (Phase 0 of
// plans/refactor-streaming-pipeline.md). The pre-existing suite asserts output
// with strings.Contains, which does NOT pin the blank-line/separator rule — the
// single most regression-prone behavior. This test captures the exact emitted
// bytes for a representative multi-event stream so any phase that shifts the
// separator handling (esp. Phase 5, which collapses skipNextEmptyLine) fails
// loudly.
//
// Invariants pinned (see §9 of the plan):
//   - a forwarded data chunk owns its own "\n\n"; the upstream's following blank
//     separator is swallowed (skipNextEmptyLine);
//   - a non-data line (SSE comment) forwards as "line\n", and a standalone blank
//     that does NOT follow a data chunk forwards as a single "\n";
//   - "[DONE]" forwards as "data: [DONE]\n\n", then the loop stops.
func TestHandleStreamingResponse_GoldenSeparators(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const chunk = `{"id":"chatcmpl-x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":null}]}`

	// Upstream emits: a plain data chunk, an SSE comment line, then [DONE].
	// Each event is terminated by the spec's blank line ("\n\n"), so the
	// scanner sees an interleaving blank after every payload line.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		fmt.Fprint(w, ": keep-alive\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withAuthContext(req)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:        "test-model",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1})

	// Expected downstream bytes:
	//   - "data: <chunk>\n\n"  (chunk owns its separator; upstream blank swallowed)
	//   - ": keep-alive\n"     (comment forwarded verbatim + single newline)
	//   - "\n"                 (the standalone blank after the comment forwards)
	//   - "data: [DONE]\n\n"   (sentinel)
	want := "data: " + chunk + "\n\n" +
		": keep-alive\n" +
		"\n" +
		"data: [DONE]\n\n"
	if got := inner.Body.String(); got != want {
		t.Errorf("downstream bytes mismatch\n--- got  ---\n%q\n--- want ---\n%q", got, want)
	}
	if logData.state != "completed" {
		t.Errorf("expected state=completed, got %q (err=%q)", logData.state, logData.errorMessage)
	}
}

// goldenStream runs an upstream-emitted SSE body through handleStreamingResponse
// and returns the exact downstream bytes. stripReasoning toggles the per-VK flag
// via context. It is the shared rig for the byte-exact transform golden tests.
func goldenStream(t *testing.T, h *Handler, upstreamBody string, stripReasoning bool) string {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, upstreamBody)
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	if stripReasoning {
		req = withStripReasoningContext(req, true)
	} else {
		req = withAuthContext(req)
	}
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:        "test-model",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)
	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1})
	return inner.Body.String()
}

// TestHandleStreamingResponse_StripReasoning_UsageOnFinalChunk is a regression
// test for the metering bug Greptile flagged on PR #161: strip_reasoning's
// emit-and-return early exit (stripForward) bypassed the usage observer, so a
// provider that rides `usage` on the SAME chunk as a reasoning delta +
// finish_reason had its tokens silently zeroed (provider bills, VK quota doesn't).
// observeDataChunk now runs before the transforms, so the usage is counted.
func TestHandleStreamingResponse_StripReasoning_UsageOnFinalChunk(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// reasoning delta + finish_reason + usage on one chunk → the stripForward path.
		fmt.Fprint(w, `data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"thinking","content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":11,"total_tokens":18}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	req, err := http.NewRequest("POST", upstream.URL+"/v1/chat/completions", strings.NewReader(`{"model":"test","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req = withStripReasoningContext(req, true)
	resp, err := upstream.Client().Do(req)
	if err != nil {
		t.Fatalf("failed to contact upstream: %v", err)
	}
	defer resp.Body.Close()

	inner := httptest.NewRecorder()
	logData := &requestLogData{
		modelID:        "test-model",
		streaming:      true,
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
		state:          "streaming",
	}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)
	h.handleStreamingResponse(inner, req, logData, resp, time.Now(), streamOptions{vkHash: "test-hash", attempt: 1})

	if logData.tokensPrompt != 7 || logData.tokensCompletion != 11 {
		t.Errorf("usage dropped under strip_reasoning: tokensPrompt=%d tokensCompletion=%d, want 7/11", logData.tokensPrompt, logData.tokensCompletion)
	}
}

// TestHandleStreamingResponse_GoldenStripReasoningKeepaliveThenComment closes the
// gap Greptile noted: a keep-alive followed by an SSE comment before [DONE]. It
// pins that the keep-alive's swallowBlank consumes the FIRST blank (its own
// separator), while the comment's trailing blank is still forwarded — the exact
// invariant that breaks if the blank handler's swallowBlank reset is ever touched.
func TestHandleStreamingResponse_GoldenStripReasoningKeepaliveThenComment(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := `data: {"id":"k","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"r"},"finish_reason":null}]}` + "\n\n" +
		": ping\n\n" +
		"data: [DONE]\n\n"

	got := goldenStream(t, h, upstream, true)

	want := `data: {"choices":[{"delta":{},"index":0}],"id":"k","object":"chat.completion.chunk"}` + "\n\n" + // keep-alive (swallows the next blank)
		": ping\n" + // comment forwarded verbatim + single newline
		"\n" + // the comment's blank IS forwarded (swallowBlank was consumed by the keep-alive)
		"data: [DONE]\n\n"
	if got != want {
		t.Errorf("keep-alive+comment bytes mismatch\n--- got  ---\n%q\n--- want ---\n%q", got, want)
	}
}

// TestHandleStreamingResponse_GoldenStripReasoningKeepalive pins the exact bytes
// of the strip_reasoning keep-alive path (§9 invariant 4): a reasoning-only delta
// is replaced by a minimal valid data chunk reusing the stream's real id, and the
// upstream's trailing blank separator is swallowed. Gates Phase 4/5.
func TestHandleStreamingResponse_GoldenStripReasoningKeepalive(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := `data: {"id":"chatcmpl-keep","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"thinking"},"finish_reason":null}]}` + "\n\n" +
		"data: [DONE]\n\n"

	got := goldenStream(t, h, upstream, true)

	// The keep-alive is a marshalled map, so its keys are alphabetised by
	// encoding/json: choices (delta,index) < id < object.
	want := `data: {"choices":[{"delta":{},"index":0}],"id":"chatcmpl-keep","object":"chat.completion.chunk"}` + "\n\n" +
		"data: [DONE]\n\n"
	if got != want {
		t.Errorf("strip_reasoning keep-alive bytes mismatch\n--- got  ---\n%q\n--- want ---\n%q", got, want)
	}
}

// TestHandleStreamingResponse_GoldenFinishReasonNormalize pins the exact bytes of
// the finish_reason normalization rewrite (§9 invariant 7): a provider value like
// "STOP" is rewritten to the OpenAI "stop", re-serialised via map[string]RawMessage
// (so the chunk's keys are alphabetised while the delta RawMessage is preserved
// verbatim). Gates Phase 4.
func TestHandleStreamingResponse_GoldenFinishReasonNormalize(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := `data: {"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"STOP"}]}` + "\n\n" +
		"data: [DONE]\n\n"

	got := goldenStream(t, h, upstream, false)

	want := `data: {"choices":[{"delta":{"content":"hi"},"finish_reason":"stop","index":0}],"id":"x","object":"chat.completion.chunk"}` + "\n\n" +
		"data: [DONE]\n\n"
	if got != want {
		t.Errorf("finish_reason normalize bytes mismatch\n--- got  ---\n%q\n--- want ---\n%q", got, want)
	}
}
