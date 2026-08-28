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

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// ---------------------------------------------------------------------------
// A frame this gateway has no Go type for is still the provider's answer.
//
// streamChunk types delta.content as *string and tool_calls[].function.arguments
// as string. A provider sending either in a wider — but perfectly valid — JSON
// shape fails the typed unmarshal, and the whole frame used to be discarded as
// though the bytes were corrupt. The client lost real output and was told
// nothing.
//
// The tool-call case is the sharp one: finish_reason rides on a SEPARATE frame
// that parses fine, so it survives. The caller receives a completion announcing
// a tool call with no tool call in it.
// ---------------------------------------------------------------------------

func streamThrough(t *testing.T, h *Handler, body string) string {
	t.Helper()
	logData := streamingLog()
	logData.providerName = "shape-provider"
	h.insertRequestLogAsync(logData)

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10,
		providerID:       uuid.New(),
		providerName:     "shape-provider",
		vkHash:           "test-hash",
		attempt:          1,
	})
	return w.Body.String()
}

func TestHandleStreamingResponse_ForwardsFramesItCannotModel(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	tests := map[string]struct{ frame, want string }{
		// The shape that loses a whole tool call.
		"tool arguments as an object": {
			`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`,
			"get_weather",
		},
		// Content as an array of parts.
		"content as parts": {
			`{"choices":[{"index":0,"delta":{"content":[{"type":"text","text":"hello"}]}}]}`,
			"hello",
		},
		// A member whose type we simply do not model.
		"unmodelled member": {
			`{"choices":[{"index":0,"delta":{"content":"hi"}}],"usage":{"completion_tokens":"7"}}`,
			`"hi"`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			out := streamThrough(t, h, "data: "+tc.frame+"\n\ndata: [DONE]\n\n")
			if !strings.Contains(out, tc.want) {
				t.Errorf("the provider's answer must reach the caller.\n got: %s\nwant it to contain: %s", out, tc.want)
			}
		})
	}
}

// The original protection has to survive: bytes that are not JSON at all are
// still dropped rather than relayed as broken frames.
func TestHandleStreamingResponse_StillDropsMalformedBytes(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	for name, frame := range map[string]string{
		"truncated object": `{"choices":[{"delta":{"content":`,
		"not json at all":  `<html>502 Bad Gateway</html>`,
		"unclosed string":  `{"choices":[{"delta":{"content":"hel`,
	} {
		t.Run(name, func(t *testing.T) {
			out := streamThrough(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")
			if strings.Contains(out, frame) {
				t.Errorf("malformed bytes must not be relayed, got: %s", out)
			}
		})
	}
}

// A credential quoted inside a frame we cannot model must still be scrubbed.
// The masking lives inside the typed-parse branch, so a frame forwarded around
// it would otherwise reach the caller unmasked.
func TestHandleStreamingResponse_MasksCredentialInAnUnmodelledFrame(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const apiKey = "hunter2-corp"
	logData := streamingLog()
	logData.providerName = "shape-provider"
	logData.masker = newCredentialMasker(apiKey)
	h.insertRequestLogAsync(logData)

	// Unmodelled (arguments as an object) AND carrying the key.
	frame := `{"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"name":"f","arguments":{"key":"` + apiKey + `"}}}]}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + frame + "\n\ndata: [DONE]\n\n"))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10,
		providerID:       uuid.New(),
		providerName:     "shape-provider",
		vkHash:           "test-hash",
		attempt:          1,
		masker:           newCredentialMasker(apiKey),
	})

	if strings.Contains(w.Body.String(), apiKey) {
		t.Errorf("the provider credential reached the caller in an unmodelled frame: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "get_weather") && !strings.Contains(w.Body.String(), `"f"`) {
		t.Errorf("the frame itself must still be forwarded, got: %s", w.Body.String())
	}
}

// strip_reasoning is a per-virtual-key contract: that caller must never receive
// reasoning. The transform lives inside the typed-parse branch, so a frame
// forwarded around it delivers reasoning anyway. Dropping the frame used to
// satisfy the guarantee by accident; forwarding it must satisfy the guarantee on
// purpose.
func TestHandleStreamingResponse_UnmodelledFrameStillStripsReasoning(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const secret = "SECRET-CHAIN-OF-THOUGHT"
	// Unmodelled (content as parts) AND carrying reasoning.
	frame := `{"choices":[{"index":0,"delta":{"reasoning_content":"` + secret + `","content":[{"type":"text","text":"hi"}]}}]}`

	logData := streamingLog()
	logData.providerName = "shape-provider"
	h.insertRequestLogAsync(logData)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + frame + "\n\ndata: [DONE]\n\n"))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody).
		WithContext(context.WithValue(context.Background(), ctxkeys.VirtualKeyStripReasoningKey, true))
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10, providerID: uuid.New(), providerName: "shape-provider",
		vkHash: "test-hash", attempt: 1,
	})

	out := w.Body.String()
	if strings.Contains(out, secret) {
		t.Errorf("reasoning reached a caller that asked for it to be stripped: %s", out)
	}
	// And the content it rode with must survive the strip.
	if !strings.Contains(out, "hi") {
		t.Errorf("stripping reasoning must not take the content with it, got: %s", out)
	}
}

// json.Valid admits any JSON value, but a client decodes every data: event into
// a chunk struct. Relaying a bare number or an array turns a one-frame loss into
// a decode exception that kills the whole stream, so only objects are forwarded.
func TestHandleStreamingResponse_NonObjectPayloadsAreStillDropped(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	for name, frame := range map[string]string{
		"bare number": `123`,
		"bare string": `"just a string"`,
		"bare bool":   `true`,
		"array":       `[{"choices":[]}]`,
	} {
		t.Run(name, func(t *testing.T) {
			out := streamThrough(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")
			if strings.Contains(out, "data: "+frame) {
				t.Errorf("a non-object payload must not be relayed, got: %s", out)
			}
		})
	}
}

// The key-shape pass is what catches a THIRD-PARTY key the provider quoted in
// its error — the gateway's own credential is handled by the exact mask. It runs
// only when the frame carries an error member, matching the typed branch's
// policy.
func TestHandleStreamingResponse_UnmodelledErrorFrameMasksKeyShapedTokens(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	// Unmodelled because error is a bare string, not our {message} object.
	out := streamThrough(t, h, `data: {"error":"upstream rejected key sk-abc123def456ghi789xyz"}`+"\n\ndata: [DONE]\n\n")
	if strings.Contains(out, "sk-abc123def456ghi789xyz") {
		t.Errorf("a key-shaped token in a forwarded error frame must be redacted, got: %s", out)
	}
}

// A stream delivered entirely in unmodelled frames must still be metered.
// observeDataChunk never sees them, so without the payload standing in for the
// output it carries, estimateMissingUsage bails on its first line and the
// request is charged nothing against quota or TPM while the provider bills for
// it. That is the motivating case for this whole change: an agentic tool call
// from a provider that both sends arguments as an object and omits usage.
func TestHandleStreamingResponse_UnmodelledFramesAreStillMetered(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	logs := captureProxyLogs(t)

	logData := streamingLog()
	logData.providerName = "shape-provider"
	h.insertRequestLogAsync(logData)

	// Unmodelled (arguments as an object), no usage chunk anywhere.
	frame := `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + frame + "\n\ndata: [DONE]\n\n"))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10, providerID: uuid.New(), providerName: "shape-provider",
		vkHash: "test-hash", attempt: 1,
	})

	if !strings.Contains(w.Body.String(), "get_weather") {
		t.Fatalf("test assumption broken: the frame must be delivered, got %s", w.Body.String())
	}
	// The estimate is asserted through its log line, not logData: it runs after
	// logData.tokensCompletion is assigned and its result goes to the meter and
	// quota, never back onto the request-log row.
	est := logs.find("charging estimated tokens")
	if len(est) == 0 {
		t.Fatal("a delivered answer with no usage chunk must be estimated, not metered at zero")
	}
	if got := est[0].attrs["delivered_bytes"]; got == "0" || got == "" {
		t.Errorf("delivered_bytes = %q, want > 0: the frame's output was invisible to the meter", got)
	}
}
