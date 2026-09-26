package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/config"
)

// trailerSentinel is text a provider puts in a malformed trailer line: no
// colon, so net/http's textproto rejects it and quotes it in the error.
const trailerSentinel = "ZZTRAILERPROMPTECHOZZ"

// serveMalformedTrailer answers one request with a chunked SSE body followed
// by a malformed trailer, over a raw socket so the framing is exact.
func serveMalformedTrailer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		for {
			l, err := br.ReadString('\n')
			if err != nil || l == "\r\n" {
				break
			}
		}
		chunk := "data: {\"a\":\"b\"}\n\n"
		fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n0\r\n%s\r\n\r\n", len(chunk), chunk, trailerSentinel)
	}()
	return "http://" + ln.Addr().String()
}

// A malformed response trailer is the one body-read error that quotes the
// response, and every reader of an upstream body hands a read error on to
// error_message, the completion event and the app log. Replaced at the
// transport, it reads as errMalformedTrailer to every reader: here the stream
// scanner, the reader that used to put it in the row.
func TestUpstreamClient_ReplacesAMalformedTrailerAtTheTransport(t *testing.T) {
	h := &Handler{upstreamTransport: &http.Transport{}}
	t.Cleanup(h.upstreamTransport.CloseIdleConnections)
	url := serveMalformedTrailer(t)

	req, _ := http.NewRequest(http.MethodGet, url, http.NoBody)
	resp, err := h.upstreamClient(t.Context()).Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
	}
	scanErr := sc.Err()
	if !errors.Is(scanErr, errMalformedTrailer) {
		t.Fatalf("scanner error = %v, want errMalformedTrailer", scanErr)
	}
	if strings.Contains(scanErr.Error(), trailerSentinel) {
		t.Fatalf("the trailer line reached the read error: %v", scanErr)
	}
}

// The default scanner-error branch stores its text in error_message and the
// completion event, and an egress translator's error reaches the stream as its
// read error. That text is fenced: a translator names a provider-chosen field
// that can echo the request.
func TestDeriveStreamError_FencesAScannerErrorThatEchoesTheRequest(t *testing.T) {
	t.Parallel()
	const prompt = "ProjectNightingaleCodenameForTheLaunch"
	st := &streamState{}
	logData := &requestLogData{statusCode: 200, content: newContentFence(chatBody(prompt))}

	got := deriveStreamError(st, errors.New("anthropicegress: upstream error: "+prompt), streamOptions{}, logData)
	if strings.Contains(got, prompt) {
		t.Fatalf("a scanner error echoing the request reached the stored message: %q", got)
	}

	plain := deriveStreamError(&streamState{}, errors.New("read tcp 10.0.0.1:443: connection reset by peer"), streamOptions{}, &requestLogData{statusCode: 200, content: newContentFence(chatBody(prompt))})
	if !strings.Contains(plain, "connection reset by peer") {
		t.Fatalf("a network error that echoes nothing must keep its text: %q", plain)
	}
}

// Both TTFT-probe warn lines (failover and hedged) log classifyProbeError's
// Underlying, so its generic branch is fenced like its frame branch.
func TestClassifyProbeError_FencesTheGenericBranch(t *testing.T) {
	t.Parallel()
	const prompt = "ProjectNightingaleCodenameForTheLaunch"
	fence := newContentFence(chatBody(prompt))
	probeErr := fmt.Errorf("TTFT probe read error: %w", errors.New("anthropicegress: upstream error: "+prompt))

	re, _ := classifyProbeError(probeErr, "p", credentialMasker{}, fence, false, time.Second, time.Minute, time.Minute, 0)
	if strings.Contains(re.Underlying, prompt) {
		t.Fatalf("the generic probe branch passed the echo through: %q", re.Underlying)
	}
}

// recoverFirstToken's three lines log the scanner error by its class, since
// the probe has no request fence to hand.
func TestRecoverFirstToken_LogsTheScanErrorByClass(t *testing.T) {
	logs := captureLogsAt(t, slog.LevelDebug)
	scanErr := errors.New("anthropicegress: upstream error: " + trailerSentinel)
	for _, body := range []string{
		"data: [DONE]\n",
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n",
	} {
		recoverFirstToken(bytes.NewBufferString(body), time.Now().Add(-time.Millisecond), scanErr)
	}
	got := logs("proxy: TTFT probe recovered")
	if len(got) == 0 {
		t.Fatal("expected the recovery lines to be logged")
	}
	for _, l := range got {
		if strings.Contains(l, trailerSentinel) {
			t.Fatalf("a recovery line logged the raw scanner error: %s", l)
		}
	}
}

// A 2xx body that will not decode is described by jsonfault at its source,
// readNonStreamingBody, so both consumers (the last-candidate detail and
// rejectUntranslatableBody while a sibling remains) get the safe text. json's
// own error quotes the literal on an overflow.
func TestReadNonStreamingBody_DescribesADecodeErrorWithoutItsLiteral(t *testing.T) {
	t.Parallel()
	// An int64 overflow on "created": the decode error json itself reports
	// quotes the literal. The usage and content fields decode tolerantly and
	// would not fail at all, so the precondition pins that this fixture really
	// produces a literal-quoting error before the fix is checked against it.
	body := `{"created":24681357999999999999999}`
	var raw nonStreamingAnswer
	if rawErr := json.Unmarshal([]byte(body), &raw.chat); rawErr == nil || !strings.Contains(rawErr.Error(), "24681357") {
		t.Fatalf("the fixture must fail to decode with an error quoting the literal, got %v", rawErr)
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	ans := readNonStreamingBody(resp, credentialMasker{})
	if ans.decodeErr == nil {
		t.Fatal("expected a decode error")
	}
	if strings.Contains(ans.decodeErr.Error(), "24681357") {
		t.Fatalf("the decode error quoted the completion's literal: %v", ans.decodeErr)
	}
	if !translationIsProviderFault(ans.decodeErr) {
		t.Fatal("a body that will not decode must still be charged to the provider")
	}
}

// The three handler-level body reads log a class, not the read error: for a
// chunked request that error quotes the caller's malformed trailer line.
func TestHandlerBodyReads_LogTheFaultNotTheTrailer(t *testing.T) {
	logs := captureLogsAt(t, slog.LevelWarn)
	h := &Handler{cfg: &config.Config{}}
	readErr := textproto.ProtocolError("malformed MIME header: missing colon: " + trailerSentinel)
	newReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", errReader{readErr})
		return r
	}

	h.readRawBody(httptest.NewRecorder(), newReq())
	h.readAnthropicBody(httptest.NewRecorder(), newReq())

	for _, prefix := range []string{"responses: failed to read request body", "anthropic: failed to read request body"} {
		lines := logs(prefix)
		if len(lines) == 0 {
			t.Fatalf("no %q line was logged", prefix)
		}
		for _, l := range lines {
			if strings.Contains(l, trailerSentinel) {
				t.Fatalf("a body-read warning logged the caller's trailer line: %s", l)
			}
		}
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }
