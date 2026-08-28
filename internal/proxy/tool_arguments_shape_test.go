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

// The non-streaming surface: an object-form tool call used to fail the whole
// ChatCompletionResponse decode, so the caller received an error envelope
// instead of its tool call.
func TestToolArguments_NonStreamingDecodesObjectForm(t *testing.T) {
	body := `{"id":"1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`
	var out ChatCompletionResponse
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("an object-form tool call must decode, got: %v", err)
	}
	if len(out.Choices) == 0 || len(out.Choices[0].Message.ToolCalls) == 0 {
		t.Fatal("the tool call did not survive the decode")
	}
	if got := string(out.Choices[0].Message.ToolCalls[0].Function.Arguments); got != `{"city":"Prague"}` {
		t.Errorf("arguments = %q, want the object's own JSON", got)
	}
}

// The object form must not leave the gateway. Accepting it on the way IN is
// what stops the frame being dropped; forwarding it on the way OUT hands the
// caller a shape this gateway's own request translators cannot read back — and
// the caller echoes the assistant turn into its next request.
func TestToolArguments_ObjectFormIsNormalisedBeforeItLeaves(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	frame := `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`
	w, _ := streamToolArgs(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")

	out := w.Body.String()
	if !strings.Contains(out, "get_weather") {
		t.Fatalf("test assumption broken: the call must be delivered, got %s", out)
	}
	if strings.Contains(out, `"arguments":{`) {
		t.Errorf("the object form reached the caller: %s", out)
	}
	if !strings.Contains(out, `"arguments":"{\"city\":\"Prague\"}"`) {
		t.Errorf("arguments were not normalised to the spec string: %s", out)
	}
}

// What the caller stores has to survive a round trip back through the gateway's
// own request decoders, which is the failure the normalisation prevents: a
// failover group whose next turn lands on an Anthropic or Gemini member would
// otherwise 400 for the rest of the conversation.
//
// The emitted frame is decoded here with a STRICT `Arguments string`, matching
// anthropicegress/gemini/openairesponses on the request side. Decoding it with
// the gateway's own streamChunk would prove nothing: util.ToolArguments absorbs
// the object form, so that version of this test passed with the normalisation
// switched off.
func TestToolArguments_NormalisedOutputIsAcceptedBackAsARequest(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	frame := `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`
	w, _ := streamToolArgs(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")

	// The shape a request-side translator insists on.
	type strictChunk struct {
		Choices []struct {
			Delta struct {
				ToolCalls []struct {
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}

	var seen string
	for _, line := range strings.Split(w.Body.String(), "\n") {
		payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		var c strictChunk
		if err := json.Unmarshal([]byte(payload), &c); err != nil {
			t.Fatalf("a request decoder could not read back what we emitted: %v\nframe: %s", err, payload)
		}
		for _, ch := range c.Choices {
			for _, tc := range ch.Delta.ToolCalls {
				if tc.Function.Arguments != "" {
					seen = tc.Function.Arguments
				}
			}
		}
	}
	if seen != `{"city":"Prague"}` {
		t.Errorf("arguments = %q, want the call's own JSON", seen)
	}
}

// A pretty-printed object must not carry its whitespace into the string the
// caller stores and replays.
func TestNormalizeToolArguments(t *testing.T) {
	for name, tc := range map[string]struct {
		payload string
		want    string
		changed bool
	}{
		"object form": {
			`{"choices":[{"delta":{"tool_calls":[{"function":{"name":"f","arguments":{"a":1}}}]}}]}`,
			`"{\"a\":1}"`, true,
		},
		"pretty printed": {
			"{\"choices\":[{\"delta\":{\"tool_calls\":[{\"function\":{\"arguments\":{\n  \"a\": 1\n}}}]}}]}",
			`"{\"a\":1}"`, true,
		},
		"already the spec form": {
			`{"choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"a\":1}"}}]}}]}`,
			`"{\"a\":1}"`, false,
		},
		"no tool calls": {`{"choices":[{"delta":{"content":"hi"}}]}`, "", false},
		"not a chunk":   {`{"error":"boom"}`, "", false},
		"malformed":     {`{"choices":`, "", false},
		// The three shapes the walk has to step over rather than trip on.
		"delta is not an object":    {`{"choices":[{"delta":"nope"}]}`, "", false},
		"function is not an object": {`{"choices":[{"delta":{"tool_calls":[{"function":"nope"}]}}]}`, "", false},
		"no arguments member":       {`{"choices":[{"delta":{"tool_calls":[{"function":{"name":"f"}}]}}]}`, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, changed := normalizeToolArguments(tc.payload)
			if changed != tc.changed {
				t.Fatalf("changed = %v, want %v (got %s)", changed, tc.changed, got)
			}
			if !tc.changed {
				if got != tc.payload {
					t.Errorf("an unchanged payload must be returned untouched, got %s", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("normalised = %s, want it to contain %s", got, tc.want)
			}
		})
	}
}

// The non-streaming surface normalises on its re-encode, and that property
// comes from util.ToolArguments being a named string type rather than from any
// method. Nothing else in this package asserted it: re-adding a raw-passthrough
// MarshalJSON leaked the object form onto the wire with the whole proxy suite
// still green. This is the wire assertion that catches it.
func TestToolArguments_NonStreamingEmitsTheSpecForm(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	upstream := `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(upstream)), Header: make(http.Header)}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
	logData := streamingLog()
	logData.streaming = false
	logData.providerName = "tool-shape-provider"
	h.insertRequestLogAsync(logData)

	h.handleNonStreamingResponse(w, req, logData, resp, time.Now(), 0, 0, 0, 0, 0, 0, 0, 0, 0, "test-hash", 1)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	if strings.Contains(out, `"arguments":{`) {
		t.Errorf("the object form reached the caller: %s", out)
	}
	if !strings.Contains(out, `"arguments":"{\"city\":\"Prague\"}"`) {
		t.Errorf("arguments were not normalised to the spec string: %s", out)
	}
}
