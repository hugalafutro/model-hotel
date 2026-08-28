package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/failover"
)

// ---------------------------------------------------------------------------
// streamChunk used to type delta.content as *string and tool-call arguments as
// string. A provider sending either in a wider — but perfectly valid — shape
// failed the whole-chunk unmarshal, and the frame was discarded as though the
// bytes were corrupt.
//
// The tool-call case was the sharp one: finish_reason rides a SEPARATE frame
// that parses fine, so it survived. The caller received a completion announcing
// a tool call with no tool call in it.
//
// Widening the fields, rather than routing around the parser, is what makes the
// rest of this correct for free: the frame now takes the ordinary path, so
// masking, strip_reasoning, delivery accounting, the circuit-breaker verdict and
// the retirement verdict all apply to it exactly as they do to any other frame.
// ---------------------------------------------------------------------------

func streamTolerant(t *testing.T, h *Handler, body string, opts ...func(*http.Request) *http.Request) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	logData := streamingLog()
	logData.providerName = "wide-shape-provider"
	h.insertRequestLogAsync(logData)

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	for _, o := range opts {
		req = o(req)
	}
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10,
		providerID:       uuid.New(),
		providerName:     "wide-shape-provider",
		vkHash:           "test-hash",
		attempt:          1,
	})
	return w, logData
}

func TestTolerantChunk_WiderShapesReachTheCaller(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	tests := map[string]struct{ frame, want string }{
		"tool arguments as an object": {
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`,
			"get_weather",
		},
		"content as parts": {
			`{"choices":[{"index":0,"delta":{"content":[{"type":"text","text":"hello"}]}}]}`,
			"hello",
		},
		"reasoning content as parts": {
			`{"choices":[{"index":0,"delta":{"reasoning_content":[{"type":"text","text":"thinking"}]}}]}`,
			"thinking",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			w, _ := streamTolerant(t, h, "data: "+tc.frame+"\n\ndata: [DONE]\n\n")
			if !strings.Contains(w.Body.String(), tc.want) {
				t.Errorf("the provider's answer must reach the caller.\n got: %s\nwant it to contain: %s", w.Body.String(), tc.want)
			}
		})
	}
}

// The whole reason this shape mattered: the tool call vanished while the
// finish_reason announcing it survived, because they ride different frames.
func TestTolerantChunk_ToolCallAndItsFinishReasonArriveTogether(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	body := `data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
	w, _ := streamTolerant(t, h, body)

	out := w.Body.String()
	if strings.Contains(out, "tool_calls") && !strings.Contains(out, "get_weather") {
		t.Errorf("the caller was told a tool call happened but never received it: %s", out)
	}
}

// Delivery accounting must size the model's OUTPUT, not the JSON around it.
// deliveredBytes feeds estimateMissingUsage, which charges quota and TPM, so an
// envelope-sized count silently over-bills the caller.
func TestTolerantChunk_MetersTheTextNotTheEnvelope(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Same five characters of output, spelled two ways. The parts form is a
	// far bigger frame; the metered figure must not notice.
	asString := `{"choices":[{"index":0,"delta":{"content":"hello"}}]}`
	asParts := `{"choices":[{"index":0,"delta":{"content":[{"type":"text","text":"hello"}]}}]}`

	measure := func(frame string) string {
		logs := captureProxyLogs(t)
		streamTolerant(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")
		est := logs.find("charging estimated tokens")
		if len(est) == 0 {
			t.Fatalf("expected an estimate for a stream with no usage chunk, frame %s", frame)
		}
		return est[0].attrs["delivered_bytes"]
	}
	gotString, gotParts := measure(asString), measure(asParts)
	if gotString != "5" {
		t.Errorf("string form metered %s bytes, want 5", gotString)
	}
	if gotParts != gotString {
		t.Errorf("parts form metered %s bytes and the string form %s: the same output must cost the same",
			gotParts, gotString)
	}
}

// strip_reasoning is a per-virtual-key contract. Because the frame now takes the
// ordinary path, the transform applies to it with no special handling at all.
func TestTolerantChunk_StripReasoningAppliesToWiderShapes(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const secret = "SECRET-CHAIN-OF-THOUGHT"
	frame := `{"choices":[{"index":0,"delta":{"reasoning_content":"` + secret + `","content":[{"type":"text","text":"hi"}]}}]}`
	w, _ := streamTolerant(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n", func(r *http.Request) *http.Request {
		return r.WithContext(context.WithValue(r.Context(), ctxkeys.VirtualKeyStripReasoningKey, true))
	})

	out := w.Body.String()
	if strings.Contains(out, secret) {
		t.Errorf("reasoning reached a caller that asked for it to be stripped: %s", out)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("stripping reasoning must not take the content with it: %s", out)
	}
}

// Ollama answers with a bare {"error":"model not found"}. That failed the
// unmarshal into an object and took the whole frame down with it, so the client
// saw nothing and the request log recorded a clean completion. Now the error is
// recognised and the two agree.
func TestTolerantChunk_BareStringErrorIsRecognised(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	_, logData := streamTolerant(t, h, `data: {"error":"model not found"}`+"\n\ndata: [DONE]\n\n")

	if logData.state != "failed" {
		t.Errorf("state = %q, want failed: the provider reported an error", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "model not found") {
		t.Errorf("error_message = %q, want the provider's message", logData.errorMessage)
	}
}

// A stream that really delivers must still record a breaker success and read as
// served, whatever shape it used. Both verdicts read signals the observers set,
// which is precisely what the old narrow struct denied them.
func TestTolerantChunk_WiderShapesCountAsDelivery(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	logData := streamingLog()
	logData.providerName = "wide-shape-provider"
	h.insertRequestLogAsync(logData)

	frame := `{"choices":[{"index":0,"delta":{"content":[{"type":"text","text":"hello"}]}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + frame + "\n\ndata: [DONE]\n\n"))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10, providerID: providerID, providerName: "wide-shape-provider",
		circuitBreakerOn: true, vkHash: "test-hash", attempt: 1,
	})

	if got := h.circuitBreaker.GetState(providerID); got == failover.StateOpen {
		t.Errorf("circuit = %s: a stream that delivered must not be charged", got)
	}
	if !logData.deliveredContent {
		t.Error("deliveredContent = false: the retirement verdict would read a served stream as inconclusive")
	}
}

// The original protection survives: bytes that are not JSON are still dropped
// rather than relayed as broken frames.
func TestTolerantChunk_MalformedBytesStillDropped(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	for name, frame := range map[string]string{
		"truncated object": `{"choices":[{"delta":{"content":`,
		"not json at all":  `<html>502 Bad Gateway</html>`,
	} {
		t.Run(name, func(t *testing.T) {
			w, _ := streamTolerant(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")
			if strings.Contains(w.Body.String(), frame) {
				t.Errorf("malformed bytes must not be relayed, got: %s", w.Body.String())
			}
		})
	}
}

func TestDeltaText(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"plain string":      {`"hello"`, "hello"},
		"empty string":      {`""`, ""},
		"single part":       {`[{"type":"text","text":"hello"}]`, "hello"},
		"several parts":     {`[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, "ab"},
		"part without text": {`[{"type":"image_url"}]`, ""},
		"absent":            {``, ""},
		"null":              {`null`, ""},
		"unrecognised":      {`{"weird":"object"}`, ""},
		"number":            {`12345`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := deltaText(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("deltaText(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestArgumentsText(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want string
	}{
		"spec form, a JSON string": {`"{\"city\":\"Prague\"}"`, `{"city":"Prague"}`},
		"object form":              {`{"city":"Prague"}`, `{"city":"Prague"}`},
		"absent":                   {``, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := argumentsText(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("argumentsText(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestChunkErrorMessage(t *testing.T) {
	for name, tc := range map[string]struct {
		raw   string
		msg   string
		isErr bool
	}{
		"object with a message": {`{"message":"boom"}`, "boom", true},
		"ollama bare string":    {`"model not found"`, "model not found", true},
		"object without one":    {`{"code":500}`, `{"code":500}`, true},
		"absent":                {``, "", false},
		"null":                  {`null`, "", false},
		"empty object":          {`{}`, "", false},
		"empty string":          {`""`, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			msg, isErr := chunkErrorMessage(json.RawMessage(tc.raw))
			if isErr != tc.isErr {
				t.Fatalf("isErr = %v, want %v", isErr, tc.isErr)
			}
			if msg != tc.msg {
				t.Errorf("msg = %q, want %q", msg, tc.msg)
			}
		})
	}
}
