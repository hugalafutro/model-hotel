package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// ---------------------------------------------------------------------------
// streamChunk typed tool-call arguments as a string. The spec says a JSON
// string, but several providers send the object itself — and that mismatch
// failed the WHOLE chunk unmarshal, so the frame was dropped as though the
// bytes were corrupt.
//
// The loss is not uniform, which is what makes it sharp: finish_reason rides a
// SEPARATE frame that parses fine, so it survives. The caller receives a
// completion announcing a tool call with no tool call in it.
// ---------------------------------------------------------------------------

func streamToolArgs(t *testing.T, h *Handler, body string) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	logData := streamingLog()
	logData.providerName = "tool-shape-provider"
	h.insertRequestLogAsync(logData)

	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10,
		providerID:       uuid.New(),
		providerName:     "tool-shape-provider",
		vkHash:           "test-hash",
		attempt:          1,
	})
	return w, logData
}

func TestToolArguments_ObjectFormReachesTheCaller(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	body := `data: {"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}` + "\n\n" +
		`data: {"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n"
	w, _ := streamToolArgs(t, h, body)

	out := w.Body.String()
	if !strings.Contains(out, "get_weather") {
		t.Errorf("the tool call must reach the caller, got: %s", out)
	}
	if strings.Contains(out, "tool_calls\"}") && !strings.Contains(out, "get_weather") {
		t.Error("the caller was told a tool call happened but never received it")
	}
}

// The spec form must be untouched, byte for byte and byte-count for byte-count.
func TestToolArguments_SpecFormUnchanged(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	frame := `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Prague\"}"}}]}}]}`
	w, _ := streamToolArgs(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")

	if !strings.Contains(w.Body.String(), frame) {
		t.Errorf("a spec-form tool call must be forwarded verbatim, got: %s", w.Body.String())
	}
}

// Both spellings of the same call must cost the same: deliveredBytes feeds the
// usage estimate, which charges quota and TPM.
func TestToolArguments_BothFormsMeterTheSame(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	measure := func(frame string) string {
		logs := captureProxyLogs(t)
		streamToolArgs(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")
		est := logs.find("charging estimated tokens")
		if len(est) == 0 {
			t.Fatalf("expected an estimate for a stream with no usage chunk: %s", frame)
		}
		return est[0].attrs["delivered_bytes"]
	}
	// name "get_weather" (11) + arguments {"city":"Prague"} (17) = 28, both ways.
	asString := measure(`{"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"name":"get_weather","arguments":"{\"city\":\"Prague\"}"}}]}}]}`)
	asObject := measure(`{"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`)

	if asString != "28" {
		t.Errorf("spec form metered %s bytes, want 28", asString)
	}
	if asObject != asString {
		t.Errorf("object form metered %s and the spec form %s: the same call must cost the same", asObject, asString)
	}
}

// A tool call is output, so a stream carrying only one must not be charged as
// an empty response.
func TestToolArguments_ObjectFormCountsAsDelivery(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	logData := streamingLog()
	logData.providerName = "tool-shape-provider"
	h.insertRequestLogAsync(logData)

	frame := `{"choices":[{"index":0,"delta":{"tool_calls":[{"function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + frame + "\n\ndata: [DONE]\n\n"))}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10, providerID: providerID, providerName: "tool-shape-provider",
		circuitBreakerOn: true, vkHash: "test-hash", attempt: 1,
	})

	if got := h.circuitBreaker.GetState(providerID); got == failover.StateOpen {
		t.Error("a stream delivering a tool call must not be charged as empty")
	}
	if !strings.Contains(w.Body.String(), "get_weather") {
		t.Errorf("test assumption broken: the call must be delivered, got %s", w.Body.String())
	}
}

// Malformed bytes are still dropped: widening one field must not weaken that.
func TestToolArguments_MalformedBytesStillDropped(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	frame := `{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":`
	w, _ := streamToolArgs(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")
	if strings.Contains(w.Body.String(), frame) {
		t.Errorf("malformed bytes must not be relayed, got: %s", w.Body.String())
	}
}

func TestArgumentsText(t *testing.T) {
	for name, tc := range map[string]struct{ raw, want string }{
		"spec form, a JSON string": {`"{\"city\":\"Prague\"}"`, `{"city":"Prague"}`},
		"object form":              {`{"city":"Prague"}`, `{"city":"Prague"}`},
		"empty string":             {`""`, ""},
		"empty object":             {`{}`, "{}"},
		"absent":                   {``, ""},
		// json.Unmarshal of "null" into a string SUCCEEDS and leaves it empty,
		// which is the right size here: a null arguments member carries none.
		"null": {`null`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := argumentsText(json.RawMessage(tc.raw)); got != tc.want {
				t.Errorf("argumentsText(%s) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
