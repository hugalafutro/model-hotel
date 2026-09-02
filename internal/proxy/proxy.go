package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// newRequestWithContext is injectable for testing request creation errors.
var newRequestWithContext = http.NewRequestWithContext

// failRequest populates logData with failure details and updates the request log.
// Always populates all timing fields from timings - if zero-valued, they record as 0ms.
// kind is the machine-readable classification (a required argument so no failure
// path can silently omit it); it is stored in request_logs.error_kind.
func (h *Handler) failRequest(logData *requestLogData, statusCode int, kind ErrorKind, errMsg string, attempt int, startTime time.Time, parseMs float64, timings resolveTimings, cacheHits resolveCacheHits, proxyOverhead float64) {
	logData.statusCode = statusCode
	logData.errorKind = kind
	logData.errorMessage = errMsg
	logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
	logData.proxyOverheadMs = proxyOverhead
	logData.parseMs = parseMs
	logData.modelLookupMs = timings.modelLookupMs
	logData.providerLookupMs = timings.providerLookupMs
	logData.keyDecryptMs = timings.keyDecryptMs
	logData.dialMs = timings.dialMs
	logData.failoverLookupMs = timings.failoverLookupMs
	logData.settingsReadMs = timings.settingsReadMs
	logData.cacheHits = cacheHits
	logData.failoverAttempt = attempt
	logData.state = "failed"
	// Fire-and-forget: skip WaitForInsert to avoid blocking before error response.
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
}

// ChatCompletions handles OpenAI-compatible chat completion requests with failover support.
func (h *Handler) ChatCompletions(w http.ResponseWriter, r *http.Request) {
	st, ok := h.ingestRequest(w, r, endpointTypeChat)
	if !ok {
		return
	}
	candidates, ok := h.resolveCandidates(w, r, st)
	if !ok {
		return
	}
	h.loadFailoverConfig(r, st)

	debuglog.Debug("proxy: model resolved (pre-loop)", "model", st.logData.modelID, "provider", st.logData.providerName, "candidates", len(candidates), "overhead_ms", st.proxyOverhead)

	// Request hedging (opt-in, streaming failover groups only): race a backup
	// provider's first-token probe instead of trying members strictly in
	// sequence. Everything else keeps the sequential failover loop unchanged.
	if st.hedgingEnabled && st.isStreaming && len(candidates) > 1 {
		h.runHedgedStreaming(w, r, st, candidates, h.probeStreamingCandidate)
		return
	}

	h.runFailoverLoop(w, r, st, candidates, h.attemptCandidate)
}

// attemptFn runs one failover attempt against a single candidate and reports
// whether the loop should try the next candidate (outcomeFailover) or stop.
// ChatCompletions uses attemptCandidate; the multimodal endpoints use
// attemptPassthroughCandidate.
type attemptFn func(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, attempt, totalCandidates int) candidateOutcome

// runFailoverLoop drives the shared failover loop (phase D): the overall
// deadline check, exponential backoff between attempts, one attempt call per
// candidate, and the all-exhausted failure path.
func (h *Handler) runFailoverLoop(w http.ResponseWriter, r *http.Request, st *requestState, candidates []modelCandidate, attemptOne attemptFn) {
	// Candidates skipped at their provider's learned in-flight window: no
	// request was made, so when everything else is spent too they are worth
	// waiting for — a slot freeing on any of them is the request's way through.
	var busyCandidates []modelCandidate
	// contacted counts candidates a request was actually sent to: the
	// exponential backoff protects providers that answered with failures, and
	// a busy skip contacted nothing, so it must neither pay a backoff nor
	// escalate the next one.
	contacted := 0
	for attempt, candidate := range candidates {
		// Overall deadline check: stop failover if the total time budget
		// across all candidates has been exceeded. This prevents N candidates
		// from holding a goroutine for N×failoverTimeout when the client
		// is silent but connected (no TCP reset).
		if time.Now().After(st.overallDeadline) && attempt > 0 {
			debuglog.Warn("proxy: overall request deadline exceeded, stopping failover", "model", st.logData.modelID, "attempt", attempt+1, "total_candidates", len(candidates), "deadline", st.overallDeadline)
			st.setReqErr(reqError{Kind: KindFailoverTimeout, Attempt: attempt - 1, Provider: st.logData.providerName, Underlying: st.lastReqErr.Underlying})
			break
		}

		// Exponential backoff between failover attempts: 0ms, ~100ms, ~200ms, ~400ms...
		// Capped at 2s, with ±50ms jitter to avoid thundering herd.
		// No delay before the first contact, and none after a busy skip.
		if contacted > 0 {
			backoff := failoverBackoff(100*time.Millisecond, 2*time.Second, contacted)
			debuglog.Info("proxy: failover backoff", "backoff", backoff, "attempt", attempt+1)
			select {
			case <-time.After(backoff):
			case <-r.Context().Done():
				// If the prior attempt was a zero-token provider stall, the
				// silent connection was most likely dropped by an intermediary
				// (reverse proxy / LB / CDN) idle-read timeout rather than the
				// client. Preserve the provider stall as the terminal cause
				// instead of overwriting it with a client disconnect.
				if st.lastReqErr.Kind == KindProviderTimeout {
					status := st.lastReqErr.terminalStatus()
					// Only `attempt` providers were actually contacted before the
					// connection dropped during this backoff; the remaining
					// candidates were never tried. Pass the attempted count (not
					// len(candidates)) so the log does not claim "all N providers
					// failed" when only the first stalled.
					logMsg := st.lastReqErr.terminalLogMessage(st.isFailover, attempt)
					clientMsg := st.lastReqErr.terminalClientMessage(st.reqModel, st.isFailover)
					debuglog.Info("proxy: connection closed during failover backoff after provider stall", "model", st.logData.modelID, "provider", st.logData.providerName, "attempt", attempt+1, "kind", string(st.lastReqErr.Kind), "status", status)
					h.failRequest(st.logData, status, st.lastReqErr.Kind, logMsg, attempt-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
					writeOpenAIError(w, clientMsg, status)
					return
				}
				debuglog.Info("proxy: client disconnected during failover backoff", "model", st.logData.modelID, "provider", st.logData.providerName, "attempt", attempt+1)
				// Carry the prior attempt's provider error (if any) so the log
				// shows what was failing when the client gave up. 499 (client
				// closed request) on both the log and the wire — see plan §7.
				st.setReqErr(reqError{Kind: KindClientDisconnect, Attempt: attempt - 1, Provider: st.logData.providerName, Underlying: st.lastReqErr.Underlying})
				h.failRequest(st.logData, statusClientClosedRequest, KindClientDisconnect, st.lastErr, attempt-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
				writeOpenAIError(w, "client disconnected", statusClientClosedRequest)
				return
			}
		}

		// One failover attempt. The attempt fn owns its request contexts
		// (cancelled via defer after body consumption) and reports whether to
		// try the next candidate, that the response was served, that a
		// terminal error was written, or that the last candidate is merely
		// saturated and worth one short wait.
		switch attemptOne(w, r, st, candidate, attempt, len(candidates)) {
		case outcomeFailover:
			contacted++
			continue
		case outcomeBusy:
			busyCandidates = append(busyCandidates, candidate)
			continue
		case outcomeSkipped:
			// Contacted nothing, so no backoff; nothing to come back to.
			continue
		case outcomeRetrySaturated:
			if h.retrySaturatedCandidate(w, r, st, candidate, len(candidates), attemptOne) {
				return
			}
		default:
			return
		}
		break
	}

	if len(busyCandidates) > 0 && h.retryAfterSlotFrees(w, r, st, busyCandidates, len(candidates), attemptOne) {
		return
	}
	h.failAllExhausted(w, st, len(candidates))
}

// retryAfterSlotFrees is the all-busy arm of the loop: every remaining live
// candidate sat at its provider's learned in-flight window, so instead of
// failing a request nothing upstream refused, wait for the first slot to free
// on ANY of them (bounded by rate_limit_saturation_max_wait and the overall
// deadline) and send there. This is the only place strict priority order is
// not honoured, and only because the preferred entry had no slot to honour it
// with: the walk below still prefers earlier entries whenever both have room.
// Reports true when the request was answered.
func (h *Handler) retryAfterSlotFrees(w http.ResponseWriter, r *http.Request, st *requestState, busy []modelCandidate, numCandidates int, attemptOne attemptFn) bool {
	deadline := time.Now().Add(h.settingsRepo.GetDuration(r.Context(), "rate_limit_saturation_max_wait", defaultSaturationMaxWait))
	if st.overallDeadline.Before(deadline) {
		deadline = st.overallDeadline
	}
	attempt := numCandidates
	for time.Now().Before(deadline) {
		idx := -1
		ok := h.inflight.waitForSlot(r.Context(), deadline, func() bool {
			for i, cand := range busy {
				if h.inflight.canAdmit(cand.provider.ID, providerCeiling(cand)) {
					idx = i
					return true
				}
			}
			return false
		})
		if r.Context().Err() != nil {
			// The client left while every provider was full: a 499, never an
			// "all providers busy" it was not around to receive.
			return h.failWaitDisconnect(w, st, attempt-1, st.logData.providerName)
		}
		if !ok || idx < 0 {
			return false
		}
		debuglog.Info("proxy: slot freed, retrying a busy candidate", "provider", busy[idx].provider.Name, "model", st.logData.modelID, "attempt", attempt+1)
		switch attemptOne(w, r, st, busy[idx], attempt, attempt+1) {
		case outcomeBusy:
			// Lost the acquisition race to a concurrent request; keep waiting
			// inside the same bounded window.
			attempt++
		case outcomeFailover, outcomeRetrySaturated, outcomeSkipped:
			// The freed slot answered with a real failure; that verdict stands
			// and the exhaustion path renders it.
			return false
		default:
			return true
		}
	}
	return false
}

// failWaitDisconnect ends a request whose client hung up while the loop was
// waiting (for a saturated provider's Retry-After, or for an in-flight slot):
// 499 and client_disconnect, the same rule the ordinary failover backoff
// applies. Always returns true — the response is written.
func (h *Handler) failWaitDisconnect(w http.ResponseWriter, st *requestState, attempt int, providerName string) bool {
	debuglog.Info("proxy: client disconnected while waiting out saturation", "model", st.logData.modelID, "provider", providerName)
	st.setReqErr(reqError{Kind: KindClientDisconnect, Attempt: attempt, Provider: providerName, Underlying: st.lastReqErr.Underlying})
	h.failRequest(st.logData, statusClientClosedRequest, KindClientDisconnect, st.lastErr, attempt, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
	writeOpenAIError(w, "client disconnected", statusClientClosedRequest)
	return true
}

// retrySaturatedCandidate is the one extra attempt a saturated last candidate
// earns: wait the provider's Retry-After (capped at
// rate_limit_saturation_max_wait and at the remaining overall deadline), then
// send the same candidate again. One retry, never a loop — the attempt fn
// consults st.saturationRetried and cannot return outcomeRetrySaturated twice.
// The retry is an ordinary failover attempt: its index is one past the
// candidate list, so the request log shows it as a further failover_attempt.
// Reports true when the request was answered (served or terminal), false when
// the loop should fall through to the exhaustion path.
func (h *Handler) retrySaturatedCandidate(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, numCandidates int, attemptOne attemptFn) bool {
	wait := st.rateLimit.retryAfter
	if wait <= 0 {
		wait = defaultSaturatedRetryAfter
	}
	if maxWait := h.settingsRepo.GetDuration(r.Context(), "rate_limit_saturation_max_wait", defaultSaturationMaxWait); wait > maxWait {
		wait = maxWait
	}
	// Never wait past the overall deadline: a slot that frees after the
	// request's own budget is spent frees for someone else.
	if remaining := time.Until(st.overallDeadline); wait >= remaining {
		debuglog.Info("proxy: no time budget left for saturation retry", "model", st.logData.modelID, "retry_after", wait, "remaining", remaining)
		return false
	}
	debuglog.Info("proxy: waiting out provider saturation before final retry", "model", st.logData.modelID, "provider", candidate.provider.Name, "wait", wait)
	select {
	case <-time.After(wait):
	case <-r.Context().Done():
		// The client left during the wait: a 499 client disconnect, never the
		// "all providers busy" the exhaustion path would render for a caller
		// that is not around to receive it.
		return h.failWaitDisconnect(w, st, numCandidates-1, candidate.provider.Name)
	}
	switch attemptOne(w, r, st, candidate, numCandidates, numCandidates+1) {
	case outcomeFailover, outcomeRetrySaturated, outcomeSkipped:
		// outcomeRetrySaturated cannot recur (saturationRetried is set), and a
		// failed retry falls through to the exhaustion path like any other
		// last-candidate failure.
		return false
	default:
		return true
	}
}

// upstreamFrameError reports that the provider's first SSE data frame carried an
// error envelope rather than a token. It is distinct from every other probe
// failure because the provider answered: the fault is never the client's, so it
// is charged to the provider whatever the downstream connection was doing. See
// classifyProbeError.
type upstreamFrameError struct{ msg string }

func (e *upstreamFrameError) Error() string { return e.msg }

// emptyStreamError reports that the provider's stream ended at its very FIRST
// data frame — a bare [DONE] with no chunk before it — so it produced nothing at
// all. Like upstreamFrameError it means the provider answered, so it is charged
// to the provider rather than blamed on the client.
//
// The bar is deliberately "no chunks whatever". A provider that sends any real
// frame and then finishes has answered, even if the answer is empty, and keeps
// its win.
type emptyStreamError struct{}

func (e *emptyStreamError) Error() string {
	return "provider ended the stream without producing any content"
}

// errorEnvelopeMessage reports the provider's own message when an SSE data frame
// is an error envelope instead of a token, and ok == false for every ordinary
// frame.
//
// Whether the frame IS an error is util.ErrorMemberCarries. question, not a second
// opinion: this package already decided what counts (a populated error member of
// any shape, including Ollama's bare string; not null/{}/""/[]/false/0, which
// leave a caller nothing to read). Answering it twice is how the two drift, and
// either direction is a bug — a miss lets a broken provider win a hedged race, a
// false positive fails over a healthy stream.
//
// Only the message is extracted here, and util.ErrorMemberMessage renders shapes
// wider than {"error":{"message":...}}, so its fallbacks are not decoration.
func errorEnvelopeMessage(content string) (msg string, ok bool) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &envelope); err != nil {
		return "", false
	}
	raw := envelope["error"]
	if !util.ErrorMemberCarries(raw) {
		return "", false
	}
	// A bare string, a list, a number, or an object without a "message": render
	// what the provider put there rather than dropping a frame already judged to
	// be an error. Bounded by the caller's sanitizer.
	return util.ErrorMemberMessage(raw), true
}

// probeFrame is what one SSE data payload means to the first-token probe.
type probeFrame int

const (
	// probeFrameNotAToken is a data line carrying nothing — an empty or
	// whitespace-only field. Skipped exactly like a keepalive comment: it is not
	// a token, but it is not a verdict either, so a real frame after it wins.
	//
	// This exists because the probe and the stream reader used to disagree about
	// what a chunk IS. streamReader.classify treats a bare "data:" as a comment
	// and an empty payload as delivering nothing, while the probe accepted any
	// non-[DONE] content — so a stream of "data:" then "data: [DONE]" won the
	// race while producing zero chunks downstream. That is the empty-stream bug
	// wearing a different spelling.
	probeFrameNotAToken probeFrame = iota
	// probeFrameToken is a real first token: the provider is answering.
	probeFrameToken
	// probeFrameEmptyStream is the [DONE] terminator with no chunk before it.
	probeFrameEmptyStream
	// probeFrameError is an error envelope: the provider reported its failure.
	probeFrameError
)

// classifyProbeFrame decides what a "data:" payload tells the probe. content is
// expected already trimmed. The returned message is the provider's own text, and
// is only populated for probeFrameError.
//
// One classifier, used by both the main scanner loop and the scanner-error
// recovery branch, so the two cannot drift — and the recovery branch is the one
// no test can reach.
func classifyProbeFrame(content string) (probeFrame, string) {
	switch content {
	case "":
		return probeFrameNotAToken, ""
	case "[DONE]":
		return probeFrameEmptyStream, ""
	}
	if msg, isErr := errorEnvelopeMessage(content); isErr {
		return probeFrameError, msg
	}
	return probeFrameToken, ""
}

// recoverProbeFrame finds the first COMPLETE, meaningful SSE data line in a
// probe buffer and classifies it. found is false when the buffer holds none.
//
// Extracted from probeFirstToken's scanner-error recovery branch so the logic
// can be tested at all: that branch needs the watchdog to close the body in the
// same instant the scanner yields a line, which no test can arrange
// deterministically. The decision is testable even where the path is not.
func recoverProbeFrame(bufStr string) (verdict probeFrame, msg string, found bool) {
	for rawLine := range strings.SplitSeq(bufStr, "\n") {
		l := strings.TrimSpace(rawLine)
		if !strings.HasPrefix(l, "data:") {
			continue
		}
		// Reject partial lines: a complete SSE line must be followed by \n in
		// the buffer. Without this guard a mid-line network fragment like
		// "data: hel" (no \n) would pass HasPrefix but represent malformed data.
		if !strings.Contains(bufStr, rawLine+"\n") {
			continue
		}
		// Same classifier as the main loop, so a frame recovered from the buffer
		// is judged exactly as one read straight off the scanner.
		v, m := classifyProbeFrame(strings.TrimSpace(strings.TrimPrefix(l, "data:")))
		if v == probeFrameNotAToken {
			// Carries nothing; keep looking for a frame that does.
			continue
		}
		return v, m, true
	}
	return probeFrameNotAToken, "", false
}

// recoverFirstToken turns a scanner-error recovery buffer into probeFirstToken's
// return triple. recovered is false when the buffer holds nothing usable, and
// the caller falls through to its ordinary error returns.
//
// This is the whole recovery branch, extracted so it can be tested. The branch
// is reached only when the watchdog closes the body in the same instant the
// scanner yields a line — a race no test can arrange deterministically (see
// TestProbeFirstToken_ScannerErrorRecovery_PipeRace, which documents the same
// thing). Leaving it inline meant every verdict it can reach was unverifiable,
// and reverting it left the whole package green.
//
//nolint:revive // the error is one of four coordinated results, not a trailing status
func recoverFirstToken(buf *bytes.Buffer, startTime time.Time, scanErr error) (probeBuf *bytes.Buffer, ttftMs float64, err error, recovered bool) {
	verdict, msg, found := recoverProbeFrame(buf.String())
	if !found {
		return nil, 0, nil, false
	}
	// Every outcome logs, including the ones that refuse: an operator has no
	// other way to learn this branch fired.
	switch verdict {
	case probeFrameEmptyStream:
		debuglog.Warn("proxy: TTFT probe recovered [DONE] before any first token after scanner error", "scan_error", scanErr)
		return nil, 0, &emptyStreamError{}, true
	case probeFrameError:
		// Provider text withheld for the same reason as the main loop: this
		// function never saw the api key, so it cannot mask it.
		debuglog.Warn("proxy: TTFT probe recovered an error envelope after scanner error", "message_bytes", len(msg), "scan_error", scanErr)
		return nil, 0, &upstreamFrameError{msg: msg}, true
	case probeFrameNotAToken, probeFrameToken:
	}
	ttft := float64(time.Since(startTime).Microseconds()) / 1000.0
	debuglog.Info("proxy: TTFT probe recovered data after scanner error", "ttft_ms", ttft, "scan_error", scanErr)
	return buf, ttft, nil, true
}

// probeFirstToken reads from body until it finds the first real SSE data chunk
// or the timeout fires. It returns a buffer containing all bytes read (for
// replay via io.MultiReader), the true time-to-first-token in milliseconds,
// and any error.
//
// A "real data chunk" is any "data:" line where the content after "data:" is
// neither "[DONE]" nor an error envelope. Keepalive comments (":"), empty lines,
// "event:", "id:", and "retry:" directives are skipped but still captured in
// probeBuf for replay.
//
// Both exclusions exist for the same reason and have the same consequence: a
// provider that reports an error, and one that finishes without producing a
// single chunk, have each given the caller nothing.
//
// The error-envelope case exists because this probe picks the winner of a hedged
// race. Treating a provider's error frame as a first token means the fastest
// FAILURE wins: the broken candidate is committed to, every healthy rival still
// in flight is cancelled as superseded, and the request has no second chance.
func (h *Handler) probeFirstToken(
	ctx context.Context,
	body io.ReadCloser,
	ttftTimeout time.Duration,
	startTime time.Time,
) (probeBuf *bytes.Buffer, trueTtftMs float64, err error) {
	probeCtx, probeCancel := context.WithTimeout(ctx, ttftTimeout)
	defer probeCancel()

	// Signal the goroutine when the probe finishes, so it doesn't close
	// the body after a successful read. Closed explicitly on success paths
	// and via sync.Once/defer on all paths.
	probeDone := make(chan struct{})
	var closeProbeOnce sync.Once
	closeProbe := func() { closeProbeOnce.Do(func() { close(probeDone) }) }
	defer closeProbe()

	// Atomic flag set the instant a data line is detected, before any
	// string processing. The goroutine checks this as a last guard before
	// closing the body, closing a narrow race where the timer fires at the
	// same instant the scanner returns a data line.
	var probeSucceeded atomic.Bool

	// Goroutine closes body when the probe context is cancelled (TTFT timeout
	// or parent context cancellation), unblocking the scanner. The double-
	// check of probeDone handles the narrow race where the probe succeeds
	// at the same instant the context fires; probeSucceeded is the final
	// guard to prevent closing a body that's about to be replayed.
	go func() {
		select {
		case <-probeDone:
			// Probe finished — don't touch the body.
			return
		case <-probeCtx.Done():
			// Double-check: probe may have just finished between the
			// outer select and here.
			select {
			case <-probeDone:
				return
			default:
			}
			if !probeSucceeded.Load() {
				_ = body.Close()
			}
		}
	}()

	var buf bytes.Buffer
	tee := io.TeeReader(body, &buf)
	scanner := bufio.NewScanner(tee)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines, keepalive comments, and non-data directives.
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "id:") || strings.HasPrefix(line, "retry:") {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			content := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			verdict, envelopeMsg := classifyProbeFrame(content)
			if verdict == probeFrameNotAToken {
				// Carries nothing, so it decides nothing. The watchdog must
				// stay armed across it, which is why probeSucceeded is set
				// below rather than on any "data:" prefix.
				continue
			}
			// Signal the goroutine — a frame that MEANS something was found, so
			// the body must not be closed underneath us. Kept as early as
			// possible so the goroutine sees it even if the timer fires at the
			// same instant; the scanner-error recovery below covers the rest of
			// that window.
			probeSucceeded.Store(true)
			if verdict == probeFrameEmptyStream {
				// The stream ended before producing a single chunk. This used to
				// count as a win, which meant an instantly-empty provider beat a
				// slower one that would really have answered — the same way an
				// error frame did, and with the same consequence: every healthy
				// rival still racing is cancelled and the caller gets nothing.
				// Nothing is as good as an error, so it loses the same way.
				debuglog.Warn("proxy: TTFT probe saw [DONE] before any first token", "ttft_ms", float64(time.Since(startTime).Microseconds())/1000.0)
				closeProbe()
				return nil, 0, &emptyStreamError{}
			}
			if verdict == probeFrameError {
				msg := envelopeMsg
				// The provider answered, but with its own failure. Counting
				// this as a first token is what lets a broken provider win a
				// hedged race against a working one.
				// The provider's own text is NOT logged here. This function never
				// saw the api key, so it cannot mask it, and a provider is free
				// to quote the credential back inside its error. Key-SHAPE
				// masking is not enough — it only catches tokens that look like
				// keys, and an operator's key need not. Both callers log the
				// message one line later, exact-masked by classifyProbeError.
				debuglog.Warn("proxy: TTFT probe saw an error envelope instead of a first token", "message_bytes", len(msg))
				closeProbe()
				return nil, 0, &upstreamFrameError{msg: msg}
			}
			// First real data chunk found.
			ttft := float64(time.Since(startTime).Microseconds()) / 1000.0
			debuglog.Info("proxy: TTFT probe found first token", "ttft_ms", ttft)
			closeProbe()
			return &buf, ttft, nil
		}
		// Unknown line format — skip but captured in buf.
	}

	// Scanner exited — body closed (timeout) or read error.
	// bufio.Scanner never returns io.EOF from Err(); on clean EOF,
	// Scan() returns false with Err() == nil, handled by the fallback
	// after this block.
	if scanErr := scanner.Err(); scanErr != nil {
		// Race recovery: the goroutine may close the body between the
		// scanner reading a complete data line and probeSucceeded being
		// checked. TeeReader writes to buf before scanner.Scan() returns,
		// so the data is captured. Only return success if the probe context
		// is still valid — if it expired, the goroutine closed the body and
		// returning success would give the caller a closed body, causing
		// handleStreamingResponse to truncate the stream after buffer replay.
		if probeCtx.Err() == nil {
			probeSucceeded.Store(true) // mirror the main loop: store before any processing
			// Every outcome logs, including the ones that refuse. This branch
			// cannot be reached from a test (see
			// TestProbeFirstToken_ScannerErrorRecovery_PipeRace), so the log is
			// the only way an operator ever learns it fired.
			if probeBuf, ttft, err, recovered := recoverFirstToken(&buf, startTime, scanErr); recovered {
				return probeBuf, ttft, err
			}
		}
		if probeCtx.Err() == context.DeadlineExceeded {
			return nil, 0, fmt.Errorf("TTFT timeout: no first token within %s", ttftTimeout)
		}
		return nil, 0, fmt.Errorf("TTFT probe read error: %w", scanErr)
	}

	// Scanner finished without error and without finding data — body EOF.
	return nil, 0, fmt.Errorf("TTFT probe: body closed before first data chunk")
}

// See util.BuildProviderTargetURL for URL construction and util.SetProviderAuthHeaders for auth.

// mapKeys returns the keys of a map[string]bool for logging.
// failoverBackoff calculates exponential backoff with jitter between failover attempts.
// base is the starting delay, capacity is the maximum delay, attempt is the 1-indexed attempt number.
// Jitter of [0, base) is added to spread retries from concurrent requests hitting the same cascade.
func failoverBackoff(base, capacity time.Duration, attempt int) time.Duration {
	exp := min(time.Duration(float64(base)*math.Pow(2, float64(attempt-1))), capacity)
	jitter := time.Duration(rand.Int64N(int64(base)))
	return exp + jitter
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// writeOpenAIError writes an OpenAI-compatible JSON error response.
// All proxy error responses must be JSON, not plain text, because clients like
// SillyTavern parse responses as JSON and crash on plain text error messages.
func writeOpenAIError(w http.ResponseWriter, message string, statusCode int) {
	util.WriteOpenAIError(w, message, statusCode)
}

// humanReadableCancelOrigin maps internal cancel origin identifiers to
// human-readable descriptions for error messages and request logs.
// Raw Go errors like "context canceled" and "context deadline exceeded" are
// opaque — callers need to know whether the client disconnected, the failover
// timeout expired, or a param-strip retry timed out.
func humanReadableCancelOrigin(origin string) string {
	switch origin {
	case "client_disconnect":
		return "client disconnected"
	case "failover_timeout":
		return "upstream request timed out"
	case "retry_timeout":
		return "param-strip retry timed out"
	case "hedge_superseded":
		return "superseded by a faster hedged attempt"
	default:
		return origin
	}
}
