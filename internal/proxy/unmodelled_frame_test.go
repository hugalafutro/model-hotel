package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
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
