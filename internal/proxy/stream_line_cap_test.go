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
	"github.com/hugalafutro/model-hotel/internal/gemini"
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

	// An overflow after an in-stream error frame is still charged, output or
	// not, and never relabelled a stall.
	framed := &streamState{sawContent: true, deliveredBytes: 5, stalled: true, lastErrMsg: "upstream said no"}
	fl := &requestLogData{statusCode: 200}
	fmsg := deriveStreamError(framed, bufio.ErrTooLong, streamOptions{streamStallTimeout: time.Second}, fl)
	if !framed.lineCapExceeded || strings.HasPrefix(fmsg, "stream stalled") {
		t.Fatalf("overflow after an error frame: flag=%v errMsg=%q", framed.lineCapExceeded, fmsg)
	}
	if providerAtFault(fl.errorKind) {
		if v := judgeStreamForBreaker(framed, fl, fmsg, true); v.failureReason == "" {
			t.Fatalf("overflow after an error frame: verdict = %+v, want a charge", v)
		}
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
// Close ends further reads the way closing a real response body does, which
// is how the watchdog unblocks a stalled scan.
type slowBody struct {
	data   []byte
	piece  int
	pause  time.Duration
	closed atomic.Bool
}

func (s *slowBody) Read(p []byte) (int, error) {
	if s.closed.Load() {
		return 0, errors.New("read on closed body")
	}
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	time.Sleep(s.pause)
	n := min(s.piece, len(p), len(s.data))
	copy(p, s.data[:n])
	s.data = s.data[n:]
	return n, nil
}

func (s *slowBody) Close() error { s.closed.Store(true); return nil }

// Bytes arriving mid-frame keep the watchdog armed: a large frame on a slow
// link is data, not a stall, even when the whole frame outlasts the timeout.
func TestStreamReader_SlowLargeFrameIsNotAStall(t *testing.T) {
	t.Parallel()
	// 30 pieces of 16 KiB 100 ms apart: 3 s of arrival at about 160 KiB/s
	// (above slowLineRate), six times the 500 ms window, every gap a fifth.
	frame := "data: {\"x\":\"" + strings.Repeat("A", 30*(16<<10)) + "\"}\n"
	body := &slowBody{data: []byte(frame), piece: 16 << 10, pause: 100 * time.Millisecond}
	reader := newStreamReader(context.Background(), body, streamOptions{streamStallTimeout: 500 * time.Millisecond}, &requestLogData{modelID: "m", providerName: "p"}, nil)
	defer reader.Close()

	ev, ok := reader.Next()
	if !ok || ev.kind != sseData {
		t.Fatalf("slow frame not delivered: ok=%v kind=%d err=%v", ok, ev.kind, reader.err())
	}
	if reader.stalled() {
		t.Fatal("a frame whose bytes kept arriving was judged a stall")
	}
}

// An upstream dribbling a byte at a time without finishing a line never
// reaches the byte quantum, so the watchdog still fires and the stream is a
// stall, charged to the provider, not held open until the attempt deadline.
func TestStreamReader_MidLineDribbleStillStalls(t *testing.T) {
	t.Parallel()
	body := &slowBody{data: []byte("data: " + strings.Repeat("A", 200)), piece: 1, pause: 20 * time.Millisecond}
	reader := newStreamReader(context.Background(), body, streamOptions{streamStallTimeout: 300 * time.Millisecond}, &requestLogData{modelID: "m", providerName: "p"}, nil)

	start := time.Now()
	for {
		if _, ok := reader.Next(); !ok {
			break
		}
	}
	reader.Close()
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the dribble held the stream for %s; the watchdog should end it", elapsed)
	}
	if !reader.stalled() {
		t.Fatal("a mid-line dribble below the byte quantum must stall")
	}
}

// Volume alone does not keep a line alive forever: once one line has run for
// progressiveStallMultiplier stall windows below slowLineRate, mid-line bytes
// stop re-arming the watchdog and an upstream trickling quanta without
// finishing the line (here 4 KiB per 100 ms, 40 KiB/s) stalls.
func TestStreamReader_EndlessLineInQuantaStallsAfterItsBudget(t *testing.T) {
	t.Parallel()
	const stall = 200 * time.Millisecond
	body := &slowBody{data: []byte("data: " + strings.Repeat("A", 200*stallByteQuantum)), piece: stallByteQuantum, pause: 100 * time.Millisecond}
	reader := newStreamReader(context.Background(), body, streamOptions{streamStallTimeout: stall}, &requestLogData{modelID: "m", providerName: "p"}, nil)

	start := time.Now()
	for {
		if _, ok := reader.Next(); !ok {
			break
		}
	}
	reader.Close()
	if !reader.stalled() {
		t.Fatal("a line past its time budget must stall")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the line held the stream for %s", elapsed)
	}
}

// A line past its time budget keeps the watchdog armed while it averages
// slowLineRate: a large frame on a slow but working link is not a stall.
func TestStreamReader_LongLineAboveTheRateFloorIsNotAStall(t *testing.T) {
	t.Parallel()
	const stall = 300 * time.Millisecond
	// 32 KiB per 40 ms is about 800 KiB/s, far above the floor, for 3.6 s:
	// four times the 900 ms budget, with every gap under a seventh of the
	// window.
	frame := "data: {\"x\":\"" + strings.Repeat("A", 90*(32<<10)) + "\"}\n"
	body := &slowBody{data: []byte(frame), piece: 32 << 10, pause: 40 * time.Millisecond}
	reader := newStreamReader(context.Background(), body, streamOptions{streamStallTimeout: stall}, &requestLogData{modelID: "m", providerName: "p"}, nil)
	defer reader.Close()

	ev, ok := reader.Next()
	if !ok || ev.kind != sseData {
		t.Fatalf("long frame not delivered: ok=%v err=%v", ok, reader.err())
	}
	if reader.stalled() {
		t.Fatal("a line averaging above the floor was judged a stall")
	}
}

// A shutdown landing after an overflow does not excuse it: the row names the
// provider, so the breaker is charged too.
func TestJudgeStreamForBreaker_OverflowIsChargedAcrossAShutdown(t *testing.T) {
	t.Parallel()
	st := &streamState{interrupted: true}
	logData := &requestLogData{statusCode: 200}
	msg := deriveStreamError(st, bufio.ErrTooLong, streamOptions{}, logData)
	if msg != lineCapErrMsg {
		t.Fatalf("errMsg = %q, want the overflow, not the restart", msg)
	}
	if v := judgeStreamForBreaker(st, logData, msg, true); v.failureReason == "" {
		t.Fatalf("verdict = %+v, want a charge", v)
	}
	// Without the overflow a shutdown records nothing, as before.
	plain := &streamState{interrupted: true}
	if v := judgeStreamForBreaker(plain, &requestLogData{errorKind: KindInternal}, "stream interrupted: gateway restarting", true); v.failureReason != "" || v.success {
		t.Fatalf("plain shutdown verdict = %+v, want nothing", v)
	}
}

// A translated upstream's adapter holds a whole event before its reader sees a
// byte; the watchdog hears the adapter's upstream reads, so a large event
// arriving steadily is not a stall.
func TestStreamReader_SlowTranslatedFrameIsNotAStall(t *testing.T) {
	t.Parallel()
	event := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"" + strings.Repeat("A", 30*(16<<10)) + "\"}]}}]}\n\n"
	upstream := &slowBody{data: []byte(event), piece: 16 << 10, pause: 100 * time.Millisecond}
	body := gemini.NewStreamAdapter(upstream, "m")
	reader := newStreamReader(context.Background(), body, streamOptions{streamStallTimeout: 500 * time.Millisecond}, &requestLogData{modelID: "m", providerName: "p"}, nil)
	defer reader.Close()

	ev, ok := reader.Next()
	if !ok || ev.kind != sseData {
		t.Fatalf("translated frame not delivered: ok=%v kind=%d err=%v", ok, ev.kind, reader.err())
	}
	if reader.stalled() {
		t.Fatal("a translated frame whose upstream bytes kept arriving was judged a stall")
	}
}
