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
// own request translators, which is the failure the normalisation prevents: a
// failover group whose next turn lands on an Anthropic or Gemini member would
// otherwise 400 for the rest of the conversation.
func TestToolArguments_NormalisedOutputIsAcceptedBackAsARequest(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	frame := `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`
	w, _ := streamToolArgs(t, h, "data: "+frame+"\n\ndata: [DONE]\n\n")

	// Lift the assistant turn out of what the caller received, the way a client
	// building its next request would.
	var args string
	for _, line := range strings.Split(w.Body.String(), "\n") {
		payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
		if !ok || payload == "[DONE]" {
			continue
		}
		var chunk streamChunk
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		for _, c := range chunk.Choices {
			if c.Delta == nil {
				continue
			}
			for _, tc := range c.Delta.ToolCalls {
				if tc.Function != nil && tc.Function.Arguments != "" {
					args = string(tc.Function.Arguments)
				}
			}
		}
	}
	if args != `{"city":"Prague"}` {
		t.Fatalf("arguments = %q, want the call's own JSON", args)
	}

	// The shape the translators need: arguments as a JSON STRING.
	req := `{"model":"m","messages":[{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":` +
		mustJSONString(t, args) + `}}]}]}`
	var probe struct {
		Messages []struct {
			ToolCalls []struct {
				Function struct {
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(req), &probe); err != nil {
		t.Fatalf("the caller's next request must decode where arguments is typed as a string: %v", err)
	}
	if got := probe.Messages[0].ToolCalls[0].Function.Arguments; got != `{"city":"Prague"}` {
		t.Errorf("round-tripped arguments = %q, want the original", got)
	}
}

func mustJSONString(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
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
