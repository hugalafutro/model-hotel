package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// An image model streams a whole picture as one base64 data URL in a single
// delta, well past the old 4 MiB line cap. The reader must hand such a frame
// on as one data event rather than ending the stream on bufio.ErrTooLong.
func TestStreamReader_ImageFrameLargerThanFourMiBIsOneEvent(t *testing.T) {
	t.Parallel()
	frame := `{"choices":[{"delta":{"images":[{"image_url":{"url":"data:image/png;base64,` + strings.Repeat("A", 5<<20) + `"}}]}}]}`
	body := io.NopCloser(strings.NewReader("data: " + frame + "\n\ndata: [DONE]\n"))
	reader := newStreamReader(context.Background(), body, streamOptions{}, &requestLogData{modelID: "m", providerName: "p"}, nil)
	defer reader.Close()

	ev, ok := reader.Next()
	if !ok || ev.kind != sseData || len(ev.payload) != len(frame) {
		t.Fatalf("frame of %d bytes was not delivered whole: ok=%v kind=%d len=%d", len(frame), ok, ev.kind, len(ev.payload))
	}
	for {
		ev, ok = reader.Next()
		if !ok {
			break
		}
		if ev.kind == sseDone {
			break
		}
	}
	if err := reader.err(); err != nil {
		t.Fatalf("reader error after a large frame: %v", err)
	}
}

// A frame past sseLineCap names the limit on the row and to the client rather
// than reading as a connection error, and is charged like any broken stream:
// no model produces such a frame, and a provider that keeps sending endless
// lines has to leave rotation.
func TestDeriveStreamError_LineCapNamesTheLimitAndChargesTheProvider(t *testing.T) {
	t.Parallel()
	st := &streamState{}
	logData := &requestLogData{statusCode: 200}

	got := deriveStreamError(st, bufio.ErrTooLong, streamOptions{}, logData)
	want := "stream failed: a frame exceeded the gateway's 32 MiB line limit"
	if got != want {
		t.Fatalf("errMsg = %q, want %q", got, want)
	}
	if st.clientErrMsg != want {
		t.Fatalf("client message = %q, want the same limit", st.clientErrMsg)
	}
	if logData.errorKind != KindProviderError {
		t.Fatalf("errorKind = %s, want %s", logData.errorKind, KindProviderError)
	}
	if v := judgeStreamForBreaker(st, logData, got, true); v.failureReason == "" {
		t.Fatalf("breaker verdict = %+v, want a charge", v)
	}

	// Charged even after output: a provider must not stay in rotation by
	// emitting one token before the endless line.
	delivered := &streamState{sawContent: true, deliveredBytes: 5}
	dl := &requestLogData{statusCode: 200}
	msg := deriveStreamError(delivered, bufio.ErrTooLong, streamOptions{}, dl)
	if v := judgeStreamForBreaker(delivered, dl, msg, true); v.failureReason == "" {
		t.Fatalf("breaker verdict after output = %+v, want a charge", v)
	}

	// A watchdog firing after the overflow does not relabel it a stall.
	late := &streamState{stalled: true}
	ll := &requestLogData{statusCode: 200}
	if got := deriveStreamError(late, bufio.ErrTooLong, streamOptions{streamStallTimeout: time.Second}, ll); got != lineCapErrMsg || ll.errorKind != KindProviderError {
		t.Fatalf("overflow then stall: errMsg=%q kind=%s", got, ll.errorKind)
	}

	// A translated upstream's overflow is the same fault.
	tr := &streamState{}
	tl := &requestLogData{statusCode: 200}
	if got := deriveStreamError(tr, fmt.Errorf("gemini: %w", egress.ErrEventTooLarge), streamOptions{}, tl); got != lineCapErrMsg || tl.errorKind != KindProviderError {
		t.Fatalf("egress overflow: errMsg=%q kind=%s", got, tl.errorKind)
	}
}

// The real probe, not a hand-wrapped error: its first frame past the cap comes
// back wrapping bufio.ErrTooLong and classifies as the stream path does.
func TestProbeFirstToken_FirstFramePastTheCap(t *testing.T) {
	h := &Handler{}
	body := io.NopCloser(io.MultiReader(strings.NewReader("data: "), io.LimitReader(neverEnding{}, sseLineCap+1), strings.NewReader("\n")))
	_, _, err := h.probeFirstToken(context.Background(), body, 30*time.Second, time.Now())
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("probe error = %v, want it to wrap bufio.ErrTooLong", err)
	}
	re, charged := classifyProbeError(err, "p", credentialMasker{}, nil, false, time.Second, time.Minute, time.Minute, 1)
	if !charged || re.Kind != KindProviderError || re.Underlying != lineCapErrMsg {
		t.Fatalf("got kind=%s charged=%v underlying=%q", re.Kind, charged, re.Underlying)
	}
}

// The probe path classifies the same error the same way: the first frame past
// the cap is the provider's broken stream, not a probe timeout.
func TestClassifyProbeError_LineCapMatchesTheStreamPath(t *testing.T) {
	t.Parallel()
	probeErr := fmt.Errorf("TTFT probe read error: %w", bufio.ErrTooLong)
	re, charged := classifyProbeError(probeErr, "p", credentialMasker{}, nil, false, time.Second, time.Minute, time.Minute, 1)
	if !charged || re.Kind != KindProviderError || re.Underlying != lineCapErrMsg {
		t.Fatalf("got kind=%s charged=%v underlying=%q", re.Kind, charged, re.Underlying)
	}
}

type closingBody struct {
	io.Reader
	closed atomic.Bool
}

func (c *closingBody) Close() error { c.closed.Store(true); return nil }

// A line past the cap ends the stream on ErrTooLong and closes the upstream
// body at once: the orchestrator drains the body before closing it, and a
// drain of an endless line would run to the attempt's deadline.
func TestStreamReader_LinePastTheCapClosesTheBody(t *testing.T) {
	t.Parallel()
	body := &closingBody{Reader: io.MultiReader(strings.NewReader("data: "), neverEnding{})}
	reader := newStreamReader(context.Background(), body, streamOptions{}, &requestLogData{modelID: "m", providerName: "p"}, nil)
	defer reader.Close()

	if _, ok := reader.Next(); ok {
		t.Fatal("a line past the cap must end the stream")
	}
	if !errors.Is(reader.err(), bufio.ErrTooLong) {
		t.Fatalf("reader error = %v, want bufio.ErrTooLong", reader.err())
	}
	if !body.closed.Load() {
		t.Fatal("the body must be closed so the drain cannot run on")
	}
}

// neverEnding is a reader that always has more of the same line.
type neverEnding struct{}

func (neverEnding) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'A'
	}
	return len(p), nil
}

// slowBody hands out one frame in small pieces with a pause between them, so
// the frame takes longer to arrive than the stall timeout.
type slowBody struct {
	data  []byte
	piece int
	pause time.Duration
}

func (s *slowBody) Read(p []byte) (int, error) {
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.pause)
	n := min(s.piece, len(p), len(s.data))
	copy(p, s.data[:n])
	s.data = s.data[n:]
	return n, nil
}

func (s *slowBody) Close() error { return nil }

// Bytes arriving mid-frame keep the watchdog armed: a large frame on a slow
// link is data, not a stall, even when the whole frame outlasts the timeout.
func TestStreamReader_SlowLargeFrameIsNotAStall(t *testing.T) {
	t.Parallel()
	frame := "data: {\"x\":\"" + strings.Repeat("A", 4000) + "\"}\n"
	body := &slowBody{data: []byte(frame), piece: 400, pause: 30 * time.Millisecond}
	reader := newStreamReader(context.Background(), body, streamOptions{streamStallTimeout: 100 * time.Millisecond}, &requestLogData{modelID: "m", providerName: "p"}, nil)
	defer reader.Close()

	ev, ok := reader.Next()
	if !ok || ev.kind != sseData {
		t.Fatalf("slow frame not delivered: ok=%v kind=%d err=%v", ok, ev.kind, reader.err())
	}
	if reader.stalled() {
		t.Fatal("a frame whose bytes kept arriving was judged a stall")
	}
}
