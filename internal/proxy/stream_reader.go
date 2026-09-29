package proxy

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/egress"
)

// emptyMessagesLimit caps how many consecutive blank SSE lines we tolerate
// before aborting the stream (go-openai's ErrTooManyEmptyStreamMessages guard).
// Lifted to package scope from handleStreamingResponse in Phase 3.
const emptyMessagesLimit = 1000

// sseEventKind classifies one line yielded by streamReader.
type sseEventKind int

const (
	sseBlank   sseEventKind = iota // an empty separator line
	sseComment                     // a non-data line: ": ...", "event:", "id:", "retry:"
	sseData                        // "data: <json>"
	sseDone                        // "data: [DONE]"
)

// sseEvent is one classified line from the upstream stream. raw is the original
// scanner line (pre-cleanup) used for verbatim forwarding; clean is the
// BOM/CR-trimmed form (only set for sseComment, where the orchestrator inspects
// the "event:" directive); payload is the JSON after "data:" (sseData/sseDone).
type sseEvent struct {
	kind    sseEventKind
	raw     []byte
	clean   string
	payload string
}

// sseLineCap bounds one SSE line on both chat stream readers (this one and
// the TTFT probe); egress.MaxSSEEventBytes is the same figure for a translated
// upstream's event. The scanner grows its buffer on demand, so the cap costs
// nothing until a frame needs it. It has to hold a whole image: an image model
// streams each picture as one base64 data URL in a single delta (OpenRouter
// `images`, a Responses partial image), and a 2K PNG runs past 10 MiB encoded.
// The old 4 MiB cap failed such streams as bufio.ErrTooLong, reported as an
// upstream connection error. A frame past this cap is not an image any model
// produces: the stream ends, the row and the client name the limit, and the
// provider is charged for it like any other broken stream.
const sseLineCap = egress.MaxSSEEventBytes

// lineCapErrMsg is the row's and the client's message when a frame exceeds
// sseLineCap, on the stream path and the probe path alike.
var lineCapErrMsg = fmt.Sprintf("stream failed: a frame exceeded the gateway's %d MiB line limit", sseLineCap>>20)

// isLineCapErr reports a frame past the cap, from this package's scanners
// (bufio.ErrTooLong) or from a translated upstream's adapter
// (egress.ErrEventTooLarge).
func isLineCapErr(err error) bool {
	return errors.Is(err, bufio.ErrTooLong) || errors.Is(err, egress.ErrEventTooLarge)
}

// streamReader owns the upstream side of handleStreamingResponse: the scanner
// (replaying the TTFT probe buffer when present), the stall watchdog goroutine,
// the chunk counter, the empty-line limit, client-disconnect detection, BOM/CR
// line cleanup, and SSE classification. It was extracted in Phase 3 of the
// streaming-pipeline refactor (plans/refactor-streaming-pipeline.md). Next()
// yields classified sseEvents; the orchestrator owns emits and transforms.
//
// Behavior matches the prior inline loop. One sanctioned wrinkle: the every-50-
// chunks *debug* progress log reads transform state, so it stays in the
// orchestrator and fires just after Next() returns rather than just before the
// disconnect check — unobservable on the wire and in the DB.
type streamReader struct {
	scanner      *bufio.Scanner
	ctx          context.Context
	body         io.ReadCloser // closed by the watchdog on stall, to unblock the scanner
	stallTimeout time.Duration

	streamStalledFlag atomic.Bool // set by the watchdog on timeout
	interruptedFlag   atomic.Bool // set by the watchdog on process shutdown
	shutdown          <-chan struct{}
	shutdownGrace     time.Duration // captured from shutdownStreamGrace at construction
	stallCh           chan time.Duration
	watchdogDone      chan struct{}

	chunkCount int
	emptyLines int
	// midLineBytes counts bytes read since the watchdog was last re-armed and
	// lineStarted is when the line now being read began arriving; both are
	// written only on the scanner's goroutine.
	midLineBytes int
	lineBytes    int
	lineStarted  time.Time

	// disconnected is set when the client's context is cancelled between
	// iterations; abortErrMsg is set when the empty-line limit is exceeded.
	// Both cause Next() to return ok=false.
	disconnected bool
	abortErrMsg  string

	modelID      string
	providerName string
}

// newStreamReader builds the scanner (replaying opts.preReadBuf first if the
// TTFT probe captured bytes) and starts the watchdog when a stall timeout is
// configured or a shutdown channel is given. shutdown, when closed, makes the
// watchdog close the upstream body and mark the stream interrupted so the
// finalize path can hand the client a terminal error frame before the
// process exits; nil means no shutdown signal.
func newStreamReader(ctx context.Context, body io.ReadCloser, opts streamOptions, logData *requestLogData, shutdown <-chan struct{}) *streamReader {
	r := &streamReader{
		ctx:           ctx,
		body:          body,
		stallTimeout:  opts.streamStallTimeout,
		shutdown:      shutdown,
		shutdownGrace: shutdownStreamGrace,
		modelID:       logData.modelID,
		providerName:  logData.providerName,
	}
	if opts.streamStallTimeout > 0 {
		r.stallCh = make(chan time.Duration, 1)
	}
	// The watchdog hears reads mid-line as well as finished lines: an image
	// frame of tens of MiB on a slow link is still data arriving, and must
	// not read as a stall halfway through. Mid-line bytes re-arm it only in
	// volume (stallByteQuantum), so an upstream dribbling a byte at a time
	// without finishing a line still stalls.
	// A translated upstream's adapter buffers a whole event before handing on
	// a byte, so its own upstream reads are what the watchdog hears.
	var src io.Reader = body
	if tap, ok := body.(interface{ OnUpstreamBytes(func(int)) }); ok {
		tap.OnUpstreamBytes(r.noteBytes)
	} else {
		src = progressReader{r: body, progress: r.noteBytes}
	}
	if opts.preReadBuf != nil {
		src = io.MultiReader(bytes.NewReader(opts.preReadBuf.Bytes()), src)
	}
	r.scanner = bufio.NewScanner(src)
	r.scanner.Buffer(make([]byte, 64*1024), sseLineCap)
	debuglog.Debug("proxy: streaming scanner created", "model", logData.modelID, "provider", logData.providerName, "replaying_probe", opts.preReadBuf != nil)
	if opts.streamStallTimeout > 0 || shutdown != nil {
		r.watchdogDone = make(chan struct{})
		go r.runWatchdog()
	}
	return r
}

// shutdownStreamGrace is how long an in-flight stream is allowed to keep going
// after the process starts shutting down before its upstream body is closed and
// the client gets a terminal "gateway restarting" frame. Kept under the HTTP
// server's own shutdown deadline so the frame is written while the connection
// is still live.
var shutdownStreamGrace = 8 * time.Second

// runWatchdog closes the upstream body if no scan pings arrive within the
// (progressively extended) stall timeout, unblocking a hung scanner, or when
// the process starts shutting down. Without a stall timeout the timer never
// arms and only the shutdown case can fire.
func (r *streamReader) runWatchdog() {
	var timerC <-chan time.Time
	var timer *time.Timer
	if r.stallTimeout > 0 {
		timer = time.NewTimer(r.stallTimeout)
		defer timer.Stop()
		timerC = timer.C
	}
	for {
		select {
		case d := <-r.stallCh:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(d)
		case <-timerC:
			r.streamStalledFlag.Store(true)
			_ = r.body.Close() // unblock scanner
			return
		case <-r.shutdown:
			// Give an in-flight stream a moment to finish on its own before
			// cutting it: a stream seconds from [DONE] should complete with
			// real content, not a restart frame. If it finishes first,
			// watchdogDone fires and this goroutine exits without cutting.
			r.shutdown = nil // don't re-select the closed channel
			grace := time.NewTimer(r.shutdownGrace)
			select {
			case <-grace.C:
				r.interruptedFlag.Store(true)
				_ = r.body.Close() // unblock scanner
				return
			case <-r.watchdogDone:
				grace.Stop()
				return
			}
		case <-r.watchdogDone:
			return
		}
	}
}

// Next scans, classifies, and returns the next SSE event. It returns ok=false
// when the scanner is exhausted, the client disconnected (r.disconnected), or
// the empty-line limit was hit (r.abortErrMsg). The returned event's raw bytes
// are valid only until the following Next() call.
func (r *streamReader) Next() (sseEvent, bool) {
	if !r.scanner.Scan() {
		return sseEvent{}, false
	}
	line := r.scanner.Bytes()
	r.chunkCount++
	r.midLineBytes, r.lineBytes, r.lineStarted = 0, 0, time.Time{}
	r.pingWatchdog()

	// Client-disconnect check between iterations: abandon the scanned line.
	select {
	case <-r.ctx.Done():
		r.disconnected = true
		return sseEvent{}, false
	default:
	}

	lineStr := string(line)
	// P2-11: Strip UTF-8 BOM (\uFEFF) that some providers send at the
	// start of a stream. Only check on the first chunk.
	if r.chunkCount == 1 {
		lineStr = strings.TrimPrefix(lineStr, "\uFEFF")
	}
	// P2-3: Trim leading \r and \n that some providers (notably Gemini) send
	// before data: lines. SSE spec allows CR, LF, or CRLF as line terminators,
	// but bufio.Scanner may leave a stray \r if the provider uses \r\r or
	// \r\n\r\n between events.
	lineStr = strings.TrimLeft(lineStr, "\r\n ")

	if lineStr == "" {
		// P2-4: Safety valve against streams that send only empty lines.
		r.emptyLines++
		if r.emptyLines > emptyMessagesLimit {
			debuglog.Warn("proxy: too many empty SSE lines, aborting stream", "model", r.modelID, "provider", r.providerName, "limit", emptyMessagesLimit, "chunks", r.chunkCount)
			r.abortErrMsg = "stream interrupted: too many empty lines"
			return sseEvent{}, false
		}
		return sseEvent{kind: sseBlank, raw: line}, true
	}
	r.emptyLines = 0

	// Match "data: " (standard) or "data:" (LM Studio and some proxies send
	// SSE without a space after the colon). The standard form gives up exactly
	// its one separator space; the bare form has its leading whitespace
	// stripped so both yield the same JSON.
	if rest, ok := strings.CutPrefix(lineStr, "data: "); ok {
		return dataEvent(line, rest), true
	}
	if rest, ok := strings.CutPrefix(lineStr, "data:"); ok && rest != "" {
		return dataEvent(line, strings.TrimLeft(rest, " \t")), true
	}
	// Not a data line — an SSE comment (": ..."), event/id/retry directive, or
	// other. Carry the cleaned form so the orchestrator can inspect "event:".
	return sseEvent{kind: sseComment, raw: line, clean: lineStr}, true
}

// slowLineRate is the average rate, in bytes per second, a line must keep
// once it has been arriving for progressiveStallMultiplier stall windows for
// its bytes to go on re-arming the watchdog. A 32 MiB frame at this floor
// lands in about eight and a half minutes, inside the streaming attempt's
// ten-minute deadline; a trickle falls below it and stalls.
const slowLineRate = 64 << 10

// stallByteQuantum is how many mid-line bytes re-arm the stall watchdog. A
// real frame on a slow link moves far more than this per stall window; an
// upstream trickling a byte every few seconds never reaches it.
const stallByteQuantum = 4 << 10

// noteBytes counts bytes read inside Scan and re-arms the watchdog once a
// quantum has arrived since it was last re-armed. For the first
// progressiveStallMultiplier stall windows of a line that is all it takes;
// past them the line must also have averaged slowLineRate since it began, so
// a large frame on a slow but working link keeps going while an upstream
// trickling quanta to hold the stream open is left to stall.
func (r *streamReader) noteBytes(n int) {
	if r.lineStarted.IsZero() {
		r.lineStarted = time.Now()
	}
	r.midLineBytes += n
	r.lineBytes += n
	if r.midLineBytes < stallByteQuantum {
		return
	}
	if elapsed := time.Since(r.lineStarted); elapsed >= r.stallTimeout*progressiveStallMultiplier && float64(r.lineBytes) < slowLineRate*elapsed.Seconds() {
		return
	}
	r.midLineBytes = 0
	r.pingWatchdog()
}

// pingWatchdog re-arms the stall watchdog. It runs on the scanner's
// goroutine (after each line, and from progressReader inside Scan), so
// chunkCount needs no lock. After progressiveChunkThreshold chunks the stream
// is clearly alive, so the timeout extends to tolerate tool-call pauses and
// long reasoning.
func (r *streamReader) pingWatchdog() {
	if r.stallCh == nil {
		return
	}
	effectiveStall := r.stallTimeout
	if r.chunkCount > progressiveChunkThreshold {
		effectiveStall = r.stallTimeout * progressiveStallMultiplier
	}
	select {
	case r.stallCh <- effectiveStall:
	default:
	}
}

// progressReader reports the size of every read that returned bytes.
type progressReader struct {
	r        io.Reader
	progress func(n int)
}

func (p progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	if n > 0 {
		p.progress(n)
	}
	return n, err
}

// dataEvent classifies a payload extracted from a "data:" line as the [DONE]
// sentinel or a regular data chunk.
func dataEvent(line []byte, payload string) sseEvent {
	if payload == "[DONE]" {
		return sseEvent{kind: sseDone, raw: line, payload: payload}
	}
	return sseEvent{kind: sseData, raw: line, payload: payload}
}

// stalled reports whether the watchdog fired. Read after Close().
func (r *streamReader) stalled() bool {
	return r.streamStalledFlag.Load()
}

// interrupted reports whether the watchdog ended the stream because the
// process is shutting down. Read after Close().
func (r *streamReader) interrupted() bool {
	return r.interruptedFlag.Load()
}

// err returns the scanner's terminal error, if any.
func (r *streamReader) err() error {
	return r.scanner.Err()
}

// Close stops the watchdog goroutine. Called once on the finalize path, before
// reading stalled(), matching the prior inline ordering.
func (r *streamReader) Close() {
	if r.watchdogDone != nil {
		close(r.watchdogDone)
	}
}
