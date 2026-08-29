package proxy

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
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

	// strip_reasoning on and a reasoning member present: the transform this
	// frame would otherwise be rebuilt by.
	payload := `{"choices":[{"delta":{"content":[{"type":"text","text":"CONTENTMARKER"}],"reasoning_content":"thinking"}}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: " + payload + "\n\ndata: [DONE]\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.VirtualKeyStripReasoningKey, true))

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

	if strings.Contains(w.Body.String(), "sk-proj-1234") {
		t.Errorf("a credential quoted in an untypeable error frame reached the caller: %q", w.Body.String())
	}
	if strings.Contains(logData.errorMessage, "sk-proj-1234") {
		t.Errorf("a credential reached the request log: %q", logData.errorMessage)
	}
}

// The content of an untypeable frame is not an error, and the key-shape regex
// matches prose. It must not run over a frame whose error member carries
// nothing, whatever else about the frame failed to type.
func TestHandleStreamingResponse_UntypeableFrameContentIsNotKeyShapeMasked(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	payload := `{"choices":[{"delta":{"content":"use sk-proj-1234567890abcdefghij in the header"},"finish_reason":0}]}`
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

func TestCaptureSSEError_AnthropicErrorEventShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range anthropicErrorEvents {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
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
