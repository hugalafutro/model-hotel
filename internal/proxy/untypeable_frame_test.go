package proxy

import (
	"context"
	"encoding/json"
	"errors"
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

// untypeableFrames are well-formed JSON frames carrying a member in a shape
// streamChunk does not model. Every one of them was DROPPED — not forwarded, not
// observed, not masked — because the pipeline read "did this fit my structs" as
// "were these bytes even JSON".
//
// The shapes are deliberately ones no leniency in this package covers, so the
// test reads the safety net rather than a typed field's own tolerance.
var untypeableFrames = []struct {
	name    string
	payload string
}{
	// A relay that numbers its stop reasons. finish_reason is *string.
	{"numeric finish_reason", `{"choices":[{"delta":{"content":"CONTENTMARKER"},"finish_reason":0}]}`},
	// Content as an array of parts, which is what an Anthropic-shaped relay
	// emits and what the OpenAI schema itself now permits. content is *string.
	{"content as parts", `{"choices":[{"delta":{"content":[{"type":"text","text":"CONTENTMARKER"}]}}]}`},
	// Reasoning as a list of blocks. reasoning is *string.
	{"reasoning as a list", `{"choices":[{"delta":{"content":"CONTENTMARKER","reasoning":["step one"]}}]}`},
	// choices as an object rather than the array of choices.
	{"choices as an object", `{"choices":{"0":{"delta":{"content":"CONTENTMARKER"}}}}`},
}

// Not an object at all. The type error names no member, because the whole value
// is the wrong kind of thing rather than one part of it — that is not a frame
// this gateway has no struct for, it is not a chat-completion frame. Relaying
// them put a quoted sentinel into an OpenAI-shaped stream as a data event.
var notFrames = []string{
	`["CONTENTMARKER"]`,
	`"CONTENTMARKER"`,
	`42`,
	`"[DONE]"`,
}

func TestHandleStreamingResponse_TopLevelNonObjectIsNotAFrame(t *testing.T) {
	for _, payload := range notFrames {
		t.Run(payload, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			body := w.Body.String()
			if strings.Contains(body, "CONTENTMARKER") || strings.Contains(body, "42") {
				t.Errorf("a non-frame was relayed to the caller: %q", body)
			}
			// One [DONE], the gateway's own, not the provider's quoted one.
			if n := strings.Count(body, "[DONE]"); n != 1 {
				t.Errorf("[DONE] appears %d times: %q", n, body)
			}
		})
	}
}

// A frame the gateway cannot type is still the provider's answer. encoding/json
// validates the whole document before it decodes anything, so a type error
// proves the bytes are well-formed JSON — the opposite of the truncated frame
// the drop branch exists for. Dropping it silently deletes model output from the
// caller's stream.
func TestHandleStreamingResponse_UntypeableFrameIsForwarded(t *testing.T) {
	for _, tc := range untypeableFrames {
		t.Run(tc.name, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			body := "data: " + tc.payload + "\n\ndata: [DONE]\n\n"
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if !strings.Contains(w.Body.String(), "CONTENTMARKER") {
				t.Errorf("the frame never reached the caller, body = %q", w.Body.String())
			}
		})
	}
}

// The transforms rebuild the frame from the typed chunk. On a frame that only
// partly decoded, the field that failed is missing from that struct — so
// rebuilding is how the content in a content-as-parts frame would be forwarded
// as an empty delta. Forwarding the original bytes is the only honest answer
// when the gateway does not understand them.
func TestHandleStreamingResponse_UntypeableFrameIsForwardedVerbatim(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	payload := `{"choices":[{"delta":{"content":[{"type":"text","text":"CONTENTMARKER"}],"reasoning_content":"thinking"}}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))

	h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	if !strings.Contains(w.Body.String(), payload) {
		t.Errorf("the frame was rewritten rather than forwarded, body = %q", w.Body.String())
	}
}

// The observers only read, and encoding/json keeps decoding the siblings after
// it records a type error, so everything that did fit is there to be read. A
// frame whose error member is intact and whose finish_reason is not must still
// fail the request: it is the provider saying why it stopped.
func TestHandleStreamingResponse_UntypeableFrameIsStillObserved(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	payload := `{"choices":[{"finish_reason":0}],"error":{"message":"upstream capacity"}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Errorf("state = %q, want failed", logData.state)
	}
	if !strings.Contains(logData.errorMessage, "upstream capacity") {
		t.Errorf("errorMessage = %q, want the provider's error", logData.errorMessage)
	}
}

// Masking is on the emit path, and the drop branch masked nothing because
// nothing was emitted. Now that the frame goes out, it goes out scrubbed: the
// exact key on every frame, and the key-shape regex on the ones whose error
// member carries something. The token here is not the configured credential, so
// only the shape scrub can catch it — and it carries a digit, without which the
// regex drops the match as prose and the assertion reads nothing at all.
func TestHandleStreamingResponse_UntypeableErrorFrameIsMasked(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	payload := `{"choices":[{"finish_reason":0}],"error":{"message":"rejected by sk-proj-1234567890abcdefghij"}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	// The positive half first. Both assertions below are satisfied by a frame
	// that never went out at all, and a dropped frame runs no observers either —
	// so without this the test passes with the whole forward reverted.
	if !strings.Contains(body, "rejected by") {
		t.Fatalf("the error frame never reached the caller, so nothing was masked: %q", body)
	}
	if !strings.Contains(body, "[redacted]") {
		t.Errorf("the credential was not redacted for the caller: %q", body)
	}
	if strings.Contains(body, "sk-proj-1234") {
		t.Errorf("a credential quoted in an untypeable error frame reached the caller: %q", body)
	}
	if !strings.Contains(logData.errorMessage, "rejected by") {
		t.Fatalf("the observer never read the error member: %q", logData.errorMessage)
	}
	if strings.Contains(logData.errorMessage, "sk-proj-1234") {
		t.Errorf("a credential reached the request log: %q", logData.errorMessage)
	}
}

// The content of an untypeable frame is not an error, and the key-shape regex
// matches prose. It must not run over a frame whose error member carries
// nothing, whatever else about the frame failed to type.
//
// Every case here has an error member PRESENT. Without one the gate is never
// read at all — a json.RawMessage for an absent member is nil, so the correct
// gate and the presence gate that once rewrote model output both answer no, and
// the test passes whichever is in the code.
func TestHandleStreamingResponse_UntypeableFrameContentIsNotKeyShapeMasked(t *testing.T) {
	for _, member := range []string{`null`, `{}`, `""`, `false`, `{"code":0,"message":""}`} {
		t.Run(member, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			payload := `{"error":` + member + `,"choices":[{"delta":{"content":"use sk-proj-1234567890abcdefghij in the header"},"finish_reason":0}]}`
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if !strings.Contains(w.Body.String(), "sk-proj-1234567890abcdefghij") {
				t.Errorf("the model's answer was rewritten mid-stream: %q", w.Body.String())
			}
		})
	}
}

// The drop branch still exists, and this is what it is for: bytes that are not
// JSON at all. Forwarding a frame cut in half hands the caller a parse error in
// place of a clean end.
func TestHandleStreamingResponse_MalformedFrameIsStillDropped(t *testing.T) {
	for _, payload := range []string{
		`{"choices":[{"delta":{"content":"CONTENTMARKER"`,
		`{"choices":[{"delta":{"content":"CONTENTMARKER"}}],}`,
		`not json at all CONTENTMARKER`,
	} {
		t.Run(payload, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if strings.Contains(w.Body.String(), "CONTENTMARKER") {
				t.Errorf("broken bytes were forwarded to the caller: %q", w.Body.String())
			}
		})
	}
}

// P1-C reads the Anthropic error event with its own decoder, one struct away
// from the member the rest of the gateway now shares a rule for. Two defects
// followed from that.
//
// It typed the member as an object, so any other shape failed the whole decode
// and the branch recorded nothing — the event's TYPE went with it, and the
// observer beneath had to catch what it could from a member it reads correctly.
//
// Worse, it counted an error whose message is empty. errorChunkCount>0 is what
// tells writeTerminalError the client has already seen the provider's error, so
// an empty one suppressed the terminal frame while leaving lastErrMsg blank: the
// caller's stream simply stopped, with no error frame and no [DONE].
var anthropicErrorEvents = []struct {
	name     string
	payload  string
	wantMsg  string
	isError  bool
	wantType string
}{
	{"object with a message", `{"type":"error","error":{"type":"overloaded_error","message":"overloaded"}}`, "overloaded", true, "overloaded_error"},
	{"bare string", `{"type":"error","error":"overloaded"}`, "overloaded", true, ""},
	{"list", `{"type":"error","error":["overloaded"]}`, `["overloaded"]`, true, ""},
	// A type and no message: something to report, and the caller must still be
	// told the stream ended because of it.
	{"type only", `{"type":"error","error":{"type":"overloaded_error"}}`, `{"type":"overloaded_error"}`, true, "overloaded_error"},
	// Nothing to report. Counting it suppressed the terminal frame.
	{"empty object", `{"type":"error","error":{}}`, "", false, ""},
	{"null", `{"type":"error","error":null}`, "", false, ""},
	{"no error member", `{"type":"error"}`, "", false, ""},
}

// Not parallel: it reads the shared default logger, which captureProxyLogs
// swaps out for the duration of each case.
func TestCaptureSSEError_AnthropicErrorEventShapes(t *testing.T) {
	for _, tc := range anthropicErrorEvents {
		t.Run(tc.name, func(t *testing.T) {
			capture := captureProxyLogs(t)
			st := &streamState{lastAnthropicEvent: "error"}
			counted := st.captureSSEError(tc.payload, &st.lastAnthropicEvent, 1, &requestLogData{modelID: "m", providerName: "p"})

			if counted != tc.isError {
				t.Errorf("counted an Anthropic error = %v, want %v", counted, tc.isError)
			}
			if (st.errorChunkCount > 0) != tc.isError {
				t.Errorf("errorChunkCount = %d, want an error = %v", st.errorChunkCount, tc.isError)
			}
			if st.lastErrMsg != tc.wantMsg {
				t.Errorf("lastErrMsg = %q, want %q", st.lastErrMsg, tc.wantMsg)
			}
			if st.lastAnthropicEvent != "" {
				t.Errorf("the event carry was not consumed: %q", st.lastAnthropicEvent)
			}
			// The event's own type is the thing the old private struct lost on
			// every shape but one, and it is logged rather than stored — so the
			// log is where it has to be read.
			var loggedType string
			for _, rec := range capture.all() {
				if strings.Contains(rec.msg, "Anthropic SSE error event") {
					loggedType = rec.attrs["error_type"]
				}
			}
			if loggedType != tc.wantType {
				t.Errorf("logged error_type = %q, want %q", loggedType, tc.wantType)
			}
		})
	}
}

// The consequence, end to end. errorChunkCount is what tells writeTerminalError
// the client has already seen the provider's error, so counting an error with no
// message spends that budget on nothing: when the stream then really does fail,
// the frame that would have told the caller why is suppressed, and the caller is
// left holding a cut connection.
func TestHandleStreamingResponse_EmptyAnthropicErrorDoesNotEatTheTerminalFrame(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	// A delta, an error event carrying nothing, then the upstream connection
	// breaks. The broken read is the error the caller has to be told about.
	body := io.MultiReader(
		strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\nevent: error\ndata: {\"type\":\"error\",\"error\":{}}\n\n"),
		&errorReader{err: errors.New("connection reset by peer")},
	)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(body),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	if logData.state != "failed" {
		t.Fatalf("state = %q, want failed", logData.state)
	}
	if !strings.Contains(w.Body.String(), `"server_error"`) {
		t.Errorf("the caller got no terminal error frame: %q", w.Body.String())
	}
}

// An error the P1-C branch does record must not be counted twice by the observer
// reading the same member off the same line.
func TestHandleStreamingResponse_AnthropicErrorIsCountedOnce(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("event: error\ndata: {\"type\":\"error\",\"error\":\"overloaded\"}\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	if logData.state != "failed" || !strings.Contains(logData.errorMessage, "overloaded") {
		t.Errorf("state = %q, errorMessage = %q, want the provider's error", logData.state, logData.errorMessage)
	}
}

// The transforms are skipped for an untypeable frame because they rebuild it
// from the struct. normalizeToolArguments does not — it works over the payload
// as a map of raw members, so everything it does not rewrite survives verbatim,
// and it is the one rewrite an untypeable frame still needs.
//
// Forwarding the object form is not a smaller loss than dropping the frame, it
// is a different one: the caller echoes the assistant turn into its next
// request, and a failover group whose next turn lands on an Anthropic or Gemini
// member 400s on it for the life of the conversation.
func TestHandleStreamingResponse_UntypeableFrameStillNormalisesToolArguments(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	payload := `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"lookup","arguments":{"city":"Oslo"}}}]},"finish_reason":0}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))

	h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	body := w.Body.String()
	if !strings.Contains(body, `"arguments":"{\"city\":\"Oslo\"}"`) {
		t.Errorf("tool-call arguments left in the object form: %q", body)
	}
	// The member that could not be typed still rides through untouched.
	if !strings.Contains(body, `"finish_reason":0`) {
		t.Errorf("the untypeable member was not preserved: %q", body)
	}
}

// encoding/json writes a number's literal into UnmarshalTypeError.Value, and a
// relay sending the model's numeric answer where the schema wants a string is
// exactly how a frame reaches the untypeable path. Only the shape is loggable.
func TestJSONShapeName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ value, want string }{
		{"array", "array"},
		{"object", "object"},
		{"string", "string"},
		{"bool", "bool"},
		{"number 42", "number"},
		{"number -3.5e10", "number"},
		{"", ""},
	} {
		if got := jsonShapeName(tc.value); got != tc.want {
			t.Errorf("jsonShapeName(%q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

// strip_reasoning is a promise to the caller, and forwarding a frame this
// gateway cannot read is no way to keep it. computeStripReasoning works over the
// payload, so it would run — but its "does this delta still carry anything"
// verdict reads content as a plain string, so a content-as-parts delta looks
// empty to it and becomes a keep-alive, and the answer is gone.
//
// So a key that asked for reasoning to be stripped gets the frame dropped, which
// is what it got before an untypeable frame was forwarded at all. The delivery
// fix is for the streams where nothing was promised.
func TestHandleStreamingResponse_StripReasoningDropsUntypeableFrames(t *testing.T) {
	for _, payload := range []string{
		`{"choices":[{"delta":{"content":[{"type":"text","text":"CONTENTMARKER"}],"reasoning_content":"THINKINGMARKER"}}]}`,
		`{"choices":[{"delta":{"content":"CONTENTMARKER","reasoning":["THINKINGMARKER"]}}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
			req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyStripReasoningKey, true))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if strings.Contains(w.Body.String(), "THINKINGMARKER") {
				t.Errorf("reasoning reached a caller that asked for it stripped: %q", w.Body.String())
			}
		})
	}
}

// The inference the forward path rests on — a type error proves the bytes are
// well-formed — is a property of json.Unmarshal, which validates the whole
// document before decoding any of it. json.Decoder does not promise it, and
// GOEXPERIMENT=jsonv2 decodes streaming and so can report a type error on an
// early member before reaching a syntax error later on.
//
// The predicate therefore checks rather than infers. If that guarantee ever goes
// away this branch keeps dropping truncated bytes instead of forwarding them to
// callers, which is the whole point of the drop branch existing.
func TestShapeError_ChecksValidityRatherThanInferringIt(t *testing.T) {
	t.Parallel()
	typeErr := &json.UnmarshalTypeError{Value: "number", Field: "choices.0.finish_reason"}
	if shapeError([]byte(`{"choices":[{"finish_reason":0}]}`), typeErr) == nil {
		t.Error("a type error on well-formed JSON is an untypeable frame")
	}
	if got := shapeError([]byte(`{"choices":[{"finish_reason":0`), typeErr); got != nil {
		t.Errorf("a type error on truncated bytes must not be forwarded, got %v", got)
	}
	if got := shapeError([]byte(`{"choices":[]}`), nil); got != nil {
		t.Errorf("a clean decode is not an untypeable frame, got %v", got)
	}
	if got := shapeError([]byte(`{"choices":[`), &json.SyntaxError{}); got != nil {
		t.Errorf("a syntax error is not an untypeable frame, got %v", got)
	}
}

// A frame whose usage member could not be read leaves chunk.Usage a valid
// pointer to an all-zero Usage: encoding/json allocates it before it calls the
// custom unmarshaler. The observer gated on the pointer alone, so such a frame
// wrote zeros over the counts an earlier usage chunk had already reported --
// and a provider that rides usage on EVERY chunk gives it that chance on every
// frame it sends.
func TestHandleStreamingResponse_UnreadableUsageDoesNotZeroWhatWasCounted(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	body := `data: {"choices":[{"delta":{"content":"hi"}}],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}` + "\n\n" +
		`data: {"choices":[{"delta":{"content":"there"}}],"usage":{"completion_tokens_details":["reasoning"]}}` + "\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	if logData.tokensPrompt != 100 || logData.tokensCompletion != 50 {
		t.Errorf("got prompt=%d completion=%d, want the counts the provider reported (100/50)", logData.tokensPrompt, logData.tokensCompletion)
	}
}

// The warn that reports an untypeable frame must name the mismatch and nothing
// else. The commonest reason a frame lands there is that the model's own output
// was written in a shape this gateway has no struct for, so the payload is
// response content and the app log, the live viewer and the OTLP export are the
// three places it must never reach.
//
// Asserted over every attribute rather than the two known ones, because the next
// attribute added here is the one that would leak.
func TestHandleStreamingResponse_UntypeableWarnCarriesNoneOfTheFrame(t *testing.T) {
	capture := captureProxyLogs(t)
	h := newUnitHandler()
	defer stopUnitHandler(h)

	const secret = "MODELANSWERTHATMUSTNOTBELOGGED"
	payload := `{"choices":[{"delta":{"content":"` + secret + `"},"finish_reason":0}],"note":8675309}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
		Header:     make(http.Header),
	}
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	h.handleStreamingResponse(httptest.NewRecorder(), req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	var sawWarn bool
	for _, rec := range capture.all() {
		if strings.Contains(rec.msg, "does not model") {
			sawWarn = true
		}
		for k, text := range rec.attrs {
			if strings.Contains(text, secret) || strings.Contains(text, "8675309") {
				t.Errorf("attribute %q carried the frame: %q", k, text)
			}
		}
	}
	if !sawWarn {
		t.Fatal("the untypeable warn never fired, so nothing was asserted")
	}
}

// A stream whose frames this gateway forwarded but could not read tells the
// breaker nothing, in either direction. It is not emptiness — the caller
// received them — and it is not health either, because what was in them is
// unknown here.
//
// Both wrong answers have been tried. Crediting such a stream meant a relay that
// numbers its stop reasons cleared its failure streak on every empty response,
// so its circuit could never open; charging it would penalise a provider that
// answered perfectly well in a shape this gateway has no struct for. Silence is
// the honest verdict, and a stream that delivered anything READABLE alongside is
// credited on the strength of that.
func TestHandleStreamingResponse_UnreadableFramesTellTheBreakerNothing(t *testing.T) {
	partsOnly := "data: {\"choices\":[{\"delta\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}]}\n\ndata: [DONE]\n\n"
	alsoReadable := "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n" + partsOnly

	for _, tc := range []struct {
		name        string
		body        string
		wantCleared bool
	}{
		{"nothing this gateway could read", partsOnly, false},
		{"a readable answer beside it", alsoReadable, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newIntegrationHandler()
			defer stopUnitHandlerIntegration(h)
			withBreakerThreshold(t, h, "2")

			providerID := uuid.New()
			logData := streamingLog()
			logData.providerName = "wide-shape-provider"
			h.insertRequestLogAsync(logData)
			// One failure on the clock. Only a recorded SUCCESS clears it, so
			// whether the second failure opens the circuit reports the verdict.
			h.circuitBreaker.RecordFailure(providerID, "wide-shape-provider", "", failover.Cause{})

			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body))}
			h.handleStreamingResponse(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), logData, resp, time.Now(), streamOptions{
				responseHeaderMs: 10,
				providerID:       providerID,
				providerName:     "wide-shape-provider",
				circuitBreakerOn: true,
				vkHash:           "test-hash",
				attempt:          1,
			})

			if got := h.circuitBreaker.GetState(providerID, ""); got == failover.StateOpen {
				t.Fatal("a frame the gateway forwarded must never be charged to the provider")
			}
			h.circuitBreaker.RecordFailure(providerID, "wide-shape-provider", "", failover.Cause{})
			cleared := h.circuitBreaker.GetState(providerID, "") != failover.StateOpen
			if cleared != tc.wantCleared {
				t.Errorf("failure streak cleared = %v, want %v", cleared, tc.wantCleared)
			}
		})
	}
}

// Forwarding a frame is not the same as knowing it carried anything. The
// terminal chunk is exactly where a relay numbers its stop reason, and that
// frame carries no output at all — so treating a forwarded frame as delivery in
// its own right meant every empty response from such a provider cleared its
// failure streak and its circuit could never open.
//
// What the counter is for is the other direction: it must not let an unreadable
// frame CHARGE the provider either. Silence is the verdict when nothing typed
// arrived; a stream that plainly answered is still credited.
func TestJudgeStreamForBreaker_UntypeableFrames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		st          *streamState
		wantSuccess bool
		wantCharge  bool
	}{
		{"nothing but an unreadable frame", &streamState{sawDone: true, unparsedChunks: 1}, false, false},
		{"an unreadable frame beside a real answer", &streamState{sawDone: true, unparsedChunks: 1, sawContent: true}, true, false},
		{"an unreadable frame beside counted output", &streamState{sawDone: true, unparsedChunks: 1, deliveredBytes: 40}, true, false},
		{"a genuinely empty stream", &streamState{sawDone: true}, false, true},
		{"an ordinary answered stream", &streamState{sawDone: true, sawContent: true}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := judgeStreamForBreaker(tc.st, &requestLogData{}, "", true)
			if got.success != tc.wantSuccess {
				t.Errorf("success = %v, want %v", got.success, tc.wantSuccess)
			}
			if (got.failureReason != "") != tc.wantCharge {
				t.Errorf("failureReason = %q, want a charge = %v", got.failureReason, tc.wantCharge)
			}
		})
	}
}

// The strip is owed only where there is reasoning to strip. Dropping every
// untypeable frame deleted the answer out of an ordinary content delta whose
// finish_reason happened to be a number, for no gain at all.
func TestHandleStreamingResponse_StripReasoningKeepsFramesWithNoneOfIt(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	payload := `{"choices":[{"delta":{"content":"CONTENTMARKER"},"finish_reason":0}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyStripReasoningKey, true))

	h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	if !strings.Contains(w.Body.String(), "CONTENTMARKER") {
		t.Errorf("an answer with no reasoning in it was dropped: %q", w.Body.String())
	}
}

// The retirement verdict asks the same question the breaker does and used a
// narrower signal to answer it. sawContent watches content and reasoning on
// choices[0], so a stream whose whole answer is a tool call read as having
// produced nothing, and the gone-strike streak a real answer should have
// cleared stayed on the model.
//
// A stream delivered ONLY in a shape this gateway cannot read still reads as
// nothing, because nothing here can say otherwise — the TTFT probe covers it
// wherever it is on, and it is listed with the rest of what content-as-parts
// owes.
func TestFinalizeStream_DeliveredContentCountsEveryShapeOfOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"a tool call and no text", `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"lookup","arguments":"{}"}}]}}]}`},
		{"reasoning and no text", `{"choices":[{"delta":{"reasoning":"working on it"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + tc.body + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
			logData := newErrorFrameLog()

			h.handleStreamingResponse(httptest.NewRecorder(), req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if !logData.deliveredContent {
				t.Error("the model answered and the retirement verdict was told it did not")
			}
		})
	}
}

// The consequence that made it a blocker rather than a rounding error: a
// provider failing every single request must still open its circuit. The empty
// opening delta credited delivery, delivery suppressed the error charge, and the
// breaker never counted a failure.
func TestHandleStreamingResponse_EmptyUntypeableDeltaDoesNotShieldAFailingProvider(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)
	withBreakerThresholdOne(t, h)

	providerID := uuid.New()
	// An untypeable opening delta that carries nothing, then the provider's error.
	body := `data: {"choices":[{"delta":{"role":"assistant","content":""},"finish_reason":0}]}` + "\n\n" +
		`data: {"error":{"message":"model not found"}}` + "\n\ndata: [DONE]\n\n"

	logData := streamingLog()
	logData.providerName = "failing-provider"
	h.insertRequestLogAsync(logData)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	h.handleStreamingResponse(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody), logData, resp, time.Now(), streamOptions{
		responseHeaderMs: 10,
		providerID:       providerID,
		providerName:     "failing-provider",
		circuitBreakerOn: true,
		vkHash:           "test-hash",
		attempt:          1,
	})

	if got := h.circuitBreaker.GetState(providerID, ""); got != failover.StateOpen {
		t.Errorf("circuit = %v, want open: an empty delta must not shield a provider that failed", got)
	}
}

// Reasoning markers a relay stamps on every NON-reasoning delta carry nothing,
// and gating the drop on their presence deleted the answer beside them. The
// OpenRouter family stamps exactly these.
func TestHandleStreamingResponse_StripReasoningKeepsFramesWithEmptyMarkers(t *testing.T) {
	for _, member := range []string{`"reasoning":""`, `"reasoning_details":[]`, `"reasoning_content":""`, `"reasoning":null`, `"reasoning_details":{}`} {
		t.Run(member, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			payload := `{"choices":[{"delta":{"content":"CONTENTMARKER",` + member + `},"finish_reason":0}]}`
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
			req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyStripReasoningKey, true))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if !strings.Contains(w.Body.String(), "CONTENTMARKER") {
				t.Errorf("an empty reasoning marker deleted the answer beside it: %q", w.Body.String())
			}
		})
	}
}

// Reasoning arrives inside a part array on exactly the relays that make a frame
// untypeable in the first place, so a content array cannot be read as text-only.
// A key that asked for reasoning to be hidden must not be handed a thinking part.
func TestHandleStreamingResponse_StripReasoningDropsThinkingParts(t *testing.T) {
	for _, payload := range []string{
		`{"choices":[{"delta":{"content":[{"type":"thinking","thinking":"THINKINGMARKER"},{"type":"text","text":"answer"}]}}]}`,
		`{"choices":[{"delta":{"content":[{"type":"text","text":"answer"}],"reasoning_content":"THINKINGMARKER"}}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
			req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyStripReasoningKey, true))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if strings.Contains(w.Body.String(), "THINKINGMARKER") {
				t.Errorf("reasoning reached a caller that asked for it stripped: %q", w.Body.String())
			}
		})
	}
}

// A frame whose usage member is not an object is still a frame, and the answer
// in it is still the answer. Requiring the type error to NAME a member threw
// these away whole: an error from a nested custom unmarshaler reaches the caller
// with an empty Field, so the rule could not tell them from `data: 42`.
func TestHandleStreamingResponse_UnreadableUsageDoesNotCostTheFrame(t *testing.T) {
	for _, usage := range []string{`[]`, `""`, `0`, `"none"`, `{"prompt_tokens":[]}`} {
		t.Run(usage, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			payload := `{"choices":[{"delta":{"content":"CONTENTMARKER"}}],"usage":` + usage + `}`
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if !strings.Contains(w.Body.String(), "CONTENTMARKER") {
				t.Errorf("a usage member the gateway could not read cost the caller the answer: %q", w.Body.String())
			}
		})
	}
}

// The object rule, read directly. A nested custom UnmarshalJSON returns its
// error with an empty Field, so "does the type error name a member" could not
// tell a chat frame with an unreadable usage member from a bare number.
func TestShapeError_RequiresAJSONObject(t *testing.T) {
	t.Parallel()
	typeErr := &json.UnmarshalTypeError{Value: "number", Field: "choices.0.finish_reason"}
	fieldless := &json.UnmarshalTypeError{Value: "array"}
	for _, tc := range []struct {
		data string
		err  error
		want bool
	}{
		{`{"choices":[{"finish_reason":0}]}`, typeErr, true},
		// An object whose type error names nothing: a nested unmarshaler's.
		{`{"choices":[{"delta":{"content":"hi"}}],"usage":[]}`, fieldless, true},
		// Not the document at all.
		{`[1,2,3]`, fieldless, false},
		{`"[DONE]"`, fieldless, false},
		{`42`, fieldless, false},
		{`null`, fieldless, false},
		// An object, but not sound bytes.
		{`{"choices":[`, typeErr, false},
	} {
		t.Run(tc.data, func(t *testing.T) {
			t.Parallel()
			if got := shapeError([]byte(tc.data), tc.err) != nil; got != tc.want {
				t.Errorf("shapeError(%s) non-nil = %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

// A part array of plain TEXT hides nothing, so a strip_reasoning key still gets
// it. Rejecting every content that is not a string deleted the whole answer for
// those keys on any content-as-parts stream — reinstating, for one class of key,
// the exact loss this change exists to stop.
func TestHandleStreamingResponse_StripReasoningKeepsPlainTextParts(t *testing.T) {
	for _, payload := range []string{
		`{"choices":[{"delta":{"content":[{"type":"text","text":"CONTENTMARKER"}]}}]}`,
		`{"choices":[{"delta":{"content":[{"type":"text","text":"CONTENTMARKER"},{"type":"text","text":" and more"}]}}]}`,
		`{"choices":[{"delta":{"content":[{"type":"output_text","text":"CONTENTMARKER"}]},"finish_reason":0}]}`,
	} {
		t.Run(payload, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
			req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyStripReasoningKey, true))

			h.handleStreamingResponse(w, req, newErrorFrameLog(), resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if !strings.Contains(w.Body.String(), "CONTENTMARKER") {
				t.Errorf("a text-only part array was deleted for a strip_reasoning key: %q", w.Body.String())
			}
		})
	}
}

// And a frame it withheld is not one the caller can be billed for. The
// observers run before the drop and counted whatever decoded, so without
// rolling that back a strip_reasoning caller paid for a stream it never saw.
func TestHandleStreamingResponse_ADroppedFrameIsNotBilled(t *testing.T) {
	vkRepo := &mockVirtualKeyRepo{}
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	h.virtualKeyRepo = vkRepo

	// Readable content plus reasoning, on a frame that cannot be typed: the
	// content is counted by the observers, then the frame is withheld.
	payload := `{"choices":[{"delta":{"content":"` + strings.Repeat("x", 200) + `","reasoning":"THINKING"},"finish_reason":0}]}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")), Header: make(http.Header)}
	logData := newErrorFrameLog()
	logData.promptTextBytes = 40
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyStripReasoningKey, true))

	w := httptest.NewRecorder()
	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout", vkHash: "test-hash"})

	if strings.Contains(w.Body.String(), "THINKING") {
		t.Fatalf("the frame was not withheld, so there is nothing to assert: %q", w.Body.String())
	}
	if got := singleAddTokens(t, vkRepo); got != 0 {
		t.Errorf("billed %d tokens for a frame the caller never received", got)
	}
}
