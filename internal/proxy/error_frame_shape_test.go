package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// errorFrameCorpus is the set of "error" member shapes providers actually send,
// with the one verdict this package is entitled to have about each: whether it
// leaves a caller something to read. carriesErrorObject already answers exactly
// this for a whole body, so every consumer of an error member must agree with
// it — the probe that decides a hedged race, and the stream observer that
// decides what the request log and the circuit breaker see.
var errorFrameCorpus = []struct {
	name    string
	payload string
	isError bool
	wantMsg string
}{
	{"openai object", `{"error":{"message":"rate limited"}}`, true, "rate limited"},
	{"ollama bare string", `{"error":"model not found"}`, true, "model not found"},
	{"object without message", `{"error":{"code":500}}`, true, `{"code":500}`},
	{"list", `{"error":["bad","worse"]}`, true, `["bad","worse"]`},
	{"number", `{"error":503}`, true, "503"},
	{"true", `{"error":true}`, true, "true"},
	{"error after another key", `{"model":"llama3","error":"model not found"}`, true, "model not found"},
	{"null", `{"error":null}`, false, ""},
	{"empty object", `{"error":{}}`, false, ""},
	{"empty string", `{"error":""}`, false, ""},
	{"empty list", `{"error":[]}`, false, ""},
	// The C convention: an "error" member that reports there wasn't one. Every
	// frame of every 200 stream from such a relay reaches the emptiness rule.
	{"false", `{"error":false,"choices":[{"delta":{"content":"hi"}}]}`, false, ""},
	{"zero", `{"error":0,"choices":[{"delta":{"content":"hi"}}]}`, false, ""},
	// An empty member riding alongside real output. The payload starts with
	// {"error", which is all the P1-B accumulator looks at.
	{"empty member with a delta", `{"error":"","choices":[{"delta":{"content":"hi"}}]}`, false, ""},
	{"no error member", `{"choices":[{"delta":{"content":"hi"}}]}`, false, ""},
}

// A frame the probe calls an error must also be counted as one by the stream
// observer. They are two readings of the same bytes: when they disagree the
// hedged race is lost by a provider whose error the request log then never
// records, and the circuit breaker sees a clean stream.
func TestErrorFrameShapes_ObserverMatchesProbe(t *testing.T) {
	t.Parallel()
	for _, tc := range errorFrameCorpus {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var chunk streamChunk
			if err := json.Unmarshal([]byte(tc.payload), &chunk); err != nil {
				t.Fatalf("streamChunk unmarshal %s: %v", tc.payload, err)
			}
			st := &streamState{}
			st.observeDataChunk(chunk, false, 1, &requestLogData{modelID: "m", providerName: "p"})

			gotErr := st.errorChunkCount > 0
			if gotErr != tc.isError {
				t.Errorf("observer counted error=%v, want %v", gotErr, tc.isError)
			}
			if st.lastErrMsg != tc.wantMsg {
				t.Errorf("lastErrMsg = %q, want %q", st.lastErrMsg, tc.wantMsg)
			}

			frame, probeMsg := classifyProbeFrame(tc.payload)
			probeErr := frame == probeFrameError
			if probeErr != tc.isError {
				t.Errorf("probe classified error=%v, want %v", probeErr, tc.isError)
			}
			if tc.isError && probeMsg != tc.wantMsg {
				t.Errorf("probe message = %q, want %q", probeMsg, tc.wantMsg)
			}
		})
	}
}

// The same corpus through the whole streaming pipeline, which is the only place
// the readings can actually be seen to agree. observeDataChunk is one of four
// paths that write st.lastErrMsg, and the unit test above cannot see the other
// three: the P1-B accumulator in particular took every payload starting with
// {"error" — including one whose member is empty and whose delta carries the
// model's answer — and recorded the ENTIRE payload as the error message, so a
// frame the corpus calls ordinary failed the request and wrote response content
// into the request log.
func TestErrorFrameShapes_PipelineMatchesCorpus(t *testing.T) {
	for _, tc := range errorFrameCorpus {
		t.Run(tc.name, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + tc.payload + "\n\ndata: [DONE]\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
			logData := newErrorFrameLog()

			h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			failed := logData.state == "failed"
			if failed != tc.isError {
				t.Errorf("state = %q (errorMessage %q), want failed=%v", logData.state, logData.errorMessage, tc.isError)
			}
			if !tc.isError && logData.errorMessage != "" {
				t.Errorf("an ordinary frame recorded an error: %q", logData.errorMessage)
			}
			// Whatever else it records, it never records the model's output.
			if strings.Contains(logData.errorMessage, "hi") {
				t.Errorf("response content reached the request log: %q", logData.errorMessage)
			}
		})
	}
}

// Every reading in the package hands errorMemberCarries a member that came out
// of a successful unmarshal, so bytes that are not JSON at all cannot arrive
// through them. The guard is the helper's contract for anyone who calls it
// with a raw member of their own: garbage carries nothing, rather than
// counting as an error the provider never reported.
func TestErrorMemberCarries_MalformedRaw(t *testing.T) {
	t.Parallel()
	if errorMemberCarries(json.RawMessage(`{"message":"trunc`)) {
		t.Error("unparseable bytes must not count as an error member")
	}
}

// An error member the gateway cannot type is still an error member, and the
// frame carrying it is still the provider's answer. Dropping it as unparseable
// bytes hands the caller a stream that simply stops, and leaves the request log
// with no error at all when the provider put any key before "error" (the P1-B
// accumulator only recognises a payload that STARTS with {"error").
func TestHandleStreamingResponse_BareStringErrorIsForwarded(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"error first", `{"error":"model not found"}`},
		{"error after another key", `{"model":"llama3","error":"model not found"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newUnitHandler()
			defer stopUnitHandler(h)

			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: " + tc.payload + "\n\n")),
				Header:     make(http.Header),
			}
			w := httptest.NewRecorder()
			req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
			logData := newErrorFrameLog()

			h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

			if !strings.Contains(w.Body.String(), "model not found") {
				t.Errorf("error frame was not forwarded to the caller, body = %q", w.Body.String())
			}
			if logData.state != "failed" {
				t.Errorf("state = %q, want failed", logData.state)
			}
			if !strings.Contains(logData.errorMessage, "model not found") {
				t.Errorf("errorMessage = %q, want it to carry the provider's text", logData.errorMessage)
			}
		})
	}
}

// A credential can be quoted inside an error of any shape, and the key-shape
// scrub — the layer that catches a key which is not this gateway's own — is
// gated on the frame being an error. A shape the gateway could not type failed
// that gate, and the exact-key pass with it, because the invalid-JSON branch
// masks nothing at all.
//
// The quoted token is deliberately NOT the configured credential: the exact
// mask cannot see it, so only the shape scrub can, which is what makes this
// read the gate rather than the pass beneath it. The request log is asserted
// alongside the client, because the observer takes the message before the
// masking runs.
func TestHandleStreamingResponse_BareStringErrorIsMasked(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	const ownKey = "sk-ours-0000000000000000000000"
	const quoted = "sk-theirs-1234567890abcdefghij"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`data: {"error":"invalid key ` + quoted + `"}` + "\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()
	logData.masker = newCredentialMasker(ownKey)

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		cancelOrigin: "failover_timeout",
		masker:       logData.masker,
	})

	body := w.Body.String()
	if !strings.Contains(body, "invalid key") {
		t.Fatalf("error frame was not forwarded, so nothing was masked: %q", body)
	}
	if strings.Contains(body, quoted) {
		t.Errorf("key-shaped token reached the caller: %q", body)
	}
	if !strings.Contains(body, "[redacted]") {
		t.Errorf("key-shaped token was not redacted for the caller: %q", body)
	}
	if strings.Contains(logData.errorMessage, quoted) {
		t.Errorf("key-shaped token reached the request log: %q", logData.errorMessage)
	}
}

// The observers run before the stream's masking block, so the provider text
// they hold has been scrubbed by nothing — and an application log is a
// different store with a different audience from the request log: the live log
// viewer, the app-logs API, the OTLP export.
func TestErrLogAttr_MasksAndBounds(t *testing.T) {
	t.Parallel()
	st := &streamState{masker: newCredentialMasker("sk-ours-0000000000000000000000")}

	got := st.errLogAttr("bad key sk-ours-0000000000000000000000 relayed from sk-theirs-1234567890abcdefghij")
	if strings.Contains(got, "sk-ours") || strings.Contains(got, "sk-theirs") {
		t.Errorf("credential survived: %q", got)
	}

	if bounded := st.errLogAttr(strings.Repeat("x", 5000)); len(bounded) > 1000 {
		t.Errorf("unbounded provider text: %d chars", len(bounded))
	}
}

// The same, through the pipeline, because the masking is only worth anything at
// the call site that actually logs.
func TestHandleStreamingResponse_ErrorFrameLogIsMasked(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	var logged strings.Builder
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(restore) })

	const quoted = "sk-theirs-1234567890abcdefghij"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`data: {"error":"invalid key ` + quoted + `"}` + "\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()
	logData.masker = newCredentialMasker("sk-ours-0000000000000000000000")

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{
		cancelOrigin: "failover_timeout",
		masker:       logData.masker,
	})

	out := logged.String()
	if !strings.Contains(out, "SSE error chunk") {
		t.Fatalf("the error frame was not logged at all, so nothing was masked: %q", out)
	}
	if strings.Contains(out, quoted) {
		t.Errorf("credential reached the application log: %q", out)
	}
}

// An error frame is the provider reporting its own failure, so it charges the
// breaker. The payload here puts a key before "error", which is the case the
// P1-B accumulator is blind to: with the frame also dropped as unparseable, a
// provider erroring on every single request produced a stream this gateway read
// as a clean, merely empty one — and unparsedChunks then held even the
// empty-stream charge back, so the circuit never opened.
func TestJudgeStreamForBreaker_UntypeableErrorFrameCharges(t *testing.T) {
	h := newUnitHandler()
	defer stopUnitHandler(h)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`data: {"model":"llama3","error":"model not found"}` + "\n\ndata: [DONE]\n\n")),
		Header:     make(http.Header),
	}
	w := httptest.NewRecorder()
	req := withAuthContext(httptest.NewRequest("GET", "/", http.NoBody))
	logData := newErrorFrameLog()

	h.handleStreamingResponse(w, req, logData, resp, time.Now(), streamOptions{cancelOrigin: "failover_timeout"})

	if !providerAtFault(logData.errorKind) {
		t.Fatalf("errorKind = %q, want a kind the provider answers for", logData.errorKind)
	}
}

func newErrorFrameLog() *requestLogData {
	ld := &requestLogData{
		modelID:        "test-model",
		providerID:     uuid.New(),
		streaming:      true,
		state:          "pending",
		insertWg:       sync.WaitGroup{},
		virtualKeyName: "test-key",
		virtualKeyID:   "00000000-0000-0000-0000-000000000001",
	}
	ld.insertWg.Add(1)
	return ld
}
