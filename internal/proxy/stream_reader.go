package proxy

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
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

	streamStalledFlag atomic.Int32 // set to 1 by the watchdog on timeout
	stallCh           chan time.Duration
	watchdogDone      chan struct{}

	chunkCount int
	emptyLines int

	// disconnected is set when the client's context is cancelled between
	// iterations; abortErrMsg is set when the empty-line limit is exceeded.
	// Both cause Next() to return ok=false.
	disconnected bool
	abortErrMsg  string

	modelID      string
	providerName string
}

// newStreamReader builds the scanner (replaying opts.preReadBuf first if the
// TTFT probe captured bytes) and starts the stall watchdog when configured.
func newStreamReader(ctx context.Context, body io.ReadCloser, opts streamOptions, logData *requestLogData) *streamReader {
	var scanner *bufio.Scanner
	if opts.preReadBuf != nil {
		scanner = bufio.NewScanner(io.MultiReader(bytes.NewReader(opts.preReadBuf.Bytes()), body))
	} else {
		scanner = bufio.NewScanner(body)
	}
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024) // 4MB per line
	debuglog.Debug("proxy: streaming scanner created", "model", logData.modelID, "provider", logData.providerName, "replaying_probe", opts.preReadBuf != nil)

	r := &streamReader{
		scanner:      scanner,
		ctx:          ctx,
		body:         body,
		stallTimeout: opts.streamStallTimeout,
		modelID:      logData.modelID,
		providerName: logData.providerName,
	}
	if opts.streamStallTimeout > 0 {
		r.stallCh = make(chan time.Duration, 1)
		r.watchdogDone = make(chan struct{})
		go r.runWatchdog()
	}
	return r
}

// runWatchdog closes the upstream body if no scan pings arrive within the
// (progressively extended) stall timeout, unblocking a hung scanner.
func (r *streamReader) runWatchdog() {
	timer := time.NewTimer(r.stallTimeout)
	defer timer.Stop()
	for {
		select {
		case d := <-r.stallCh:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(d)
		case <-timer.C:
			r.streamStalledFlag.Store(1)
			_ = r.body.Close() // unblock scanner
			return
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

	// Ping stall watchdog after each successful scan. After
	// progressiveChunkThreshold chunks the stream is clearly alive — extend
	// the timeout to tolerate tool-call pauses and long reasoning.
	if r.stallCh != nil {
		effectiveStall := r.stallTimeout
		if r.chunkCount > progressiveChunkThreshold {
			effectiveStall = r.stallTimeout * progressiveStallMultiplier
		}
		select {
		case r.stallCh <- effectiveStall:
		default:
		}
	}

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
	// SSE without a space after the colon). Strip leading whitespace from the
	// payload so both forms yield the same JSON.
	if strings.HasPrefix(lineStr, "data: ") {
		return dataEvent(line, lineStr[6:]), true
	}
	if strings.HasPrefix(lineStr, "data:") && len(lineStr) > 5 {
		return dataEvent(line, strings.TrimLeft(lineStr[5:], " \t")), true
	}
	// Not a data line — an SSE comment (": ..."), event/id/retry directive, or
	// other. Carry the cleaned form so the orchestrator can inspect "event:".
	return sseEvent{kind: sseComment, raw: line, clean: lineStr}, true
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
	return r.streamStalledFlag.Load() == 1
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
