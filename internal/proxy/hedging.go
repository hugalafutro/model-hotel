package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// hedgeAbandonKind classifies why an attempt's context was cancelled. The
// orchestrator flags the attempts it abandons itself; anything else that
// cancels the attempt context came from the client going away.
func hedgeAbandonKind(ctx context.Context) ErrorKind {
	return cancelOriginToKind(resolveCancelOrigin(ctx, context.Canceled))
}

// hedgeResult is the outcome of probing one candidate in a hedged streaming race.
// When won is true the candidate produced a streamable 200 with a confirmed first
// token and carries the response + pre-read buffer for the orchestrator to stream;
// otherwise reqErr holds the failover cause. timings/proxyOverhead are the winner's
// accumulated dial metrics, applied to the shared requestState only if it wins.
type hedgeResult struct {
	idx           int
	won           bool
	resp          *http.Response
	preReadBuf    *bytes.Buffer
	trueTtftMs    float64
	respHeaderMs  float64
	timings       resolveTimings
	proxyOverhead float64
	reqErr        reqError
	// rateLimit is the attempt's 429 verdict. The probe classifies on its
	// PRIVATE requestState snapshot, so without this the verdict would die
	// with the snapshot and a terminal all-busy response would fall back to
	// the class-default Retry-After instead of the provider's own ask.
	rateLimit rateLimitVerdict
	// busy marks a candidate skipped at its provider's in-flight window: no
	// request was made, so an all-busy race is worth waiting out (see the
	// orchestrator's exhaustion branch) instead of failing in milliseconds.
	busy bool
	// status and breaker feed the attempt trail: the upstream status the probe
	// reached (after the MiniMax remap, 0 when none) and what the breaker was
	// told about it, read off the probe's private log snapshot because the
	// orchestrator's real logData never sees a loser.
	status  int
	breaker string
}

// minHedgeDelay floors the configured hedge delay. The dashboard already clamps to
// 1-15s, but the settings API accepts any duration; without a floor a "0s" value
// would fire every candidate at once (a thundering herd against the providers).
const minHedgeDelay = 100 * time.Millisecond

// probeFn probes a single candidate to a ready-to-stream-or-failover state WITHOUT
// writing to the client. It is the unit-test seam for runHedgedStreaming (mirroring
// the attemptFn seam used by runFailoverLoop); the real implementation is
// probeStreamingCandidate.
type probeFn func(ctx context.Context, st *requestState, candidate modelCandidate, attempt int, ttftTimeout, stallTimeout time.Duration) hedgeResult

// runHedgedStreaming serves a streaming failover group by racing the candidates'
// first-token probes instead of trying them strictly in sequence. Candidate 0 is
// launched immediately; each hedgeDelay with no winner launches the next candidate
// in parallel (a freed slot from a failed attempt launches the next one eagerly).
// The first attempt to confirm a first token wins: the orchestrator cancels every
// other in-flight attempt, stamps the winner's identity onto the shared logData,
// and streams it. This is the standard request-hedging pattern; it trades duplicate
// upstream load on slow starts for lower tail latency.
//
// Only this orchestrator goroutine writes to w and mutates the shared requestState /
// logData. Each attempt runs probeOne on a value copy of st (see
// probeStreamingCandidate), so concurrent attempts never race on the shared timing
// fields, and a loser never touches the client connection.
func (h *Handler) runHedgedStreaming(w http.ResponseWriter, r *http.Request, st *requestState, candidates []modelCandidate, probeOne probeFn) {
	ttftTimeout := h.settingsRepo.GetDuration(r.Context(), "ttft_timeout", 60*time.Second)
	stallTimeout := h.settingsRepo.GetDuration(r.Context(), "stream_stall_timeout", 30*time.Second)

	// Buffered to len(candidates): every launched attempt can deliver its result
	// without blocking even after the orchestrator has returned, so no goroutine
	// leaks on a send to an unread channel.
	results := make(chan hedgeResult, len(candidates))
	cancels := make([]context.CancelFunc, len(candidates))
	// When each probe was launched: the attempt trail's duration for a loser
	// runs from its launch to its result, and the winner's from its launch.
	launchedAt := make([]time.Time, len(candidates))
	// Marks an attempt the orchestrator itself abandoned, so the cancellation it
	// causes is not misread as the client hanging up.
	superseded := make([]*atomic.Bool, len(candidates))
	// Which launched attempts have delivered a result: the ones that have not
	// when a winner arrives were abandoned in flight, and the trail records
	// them as such rather than forgetting they were launched.
	settled := make([]bool, len(candidates))
	launched := 0
	inFlight := 0

	launch := func(idx int) {
		// failover_timeout origin so doUpstream classifies a deadline the same way
		// the sequential path does; failoverTimeout is the per-attempt budget
		// (request_timeout x10 for streaming).
		ctx, cancel := context.WithCancel(r.Context())
		ctx = context.WithValue(ctx, ctxkeys.CancelOriginKey, "failover_timeout")
		sup := &atomic.Bool{}
		superseded[idx] = sup
		ctx = context.WithValue(ctx, ctxkeys.HedgeSupersededKey, sup)
		ctx, timeoutCancel := context.WithTimeout(ctx, st.failoverTimeout)
		cancels[idx] = func() { timeoutCancel(); cancel() }
		launchedAt[idx] = time.Now()
		launched++
		inFlight++
		// Snapshot requestState here in the single-threaded orchestrator, NOT
		// inside the probe goroutine: the orchestrator keeps writing st.lastReqErr
		// via setReqErr as other results arrive, so copying *st concurrently in the
		// goroutine would be a data race on the multi-field reqError. Each attempt
		// gets its own private copy that nothing else touches.
		snap := *st
		// logData is a pointer, so the struct copy still aliases it. The probe path
		// (buildCandidateRequest/doUpstream) only reads providerName/modelID for
		// debug logs, while serveHedgeWinner writes the winner's identity onto the
		// real st.logData; alias them and those reads race those writes. Give each
		// probe a private throwaway logData so they never overlap. (A plain
		// *st.logData copy is impossible: requestLogData embeds a sync.WaitGroup.
		// The orchestrator keeps using the real st for all terminal logging.)
		//
		// endpointType is load-bearing, not decoration: a losing candidate's
		// refusal is classified against this throwaway (see
		// probeStreamingCandidate), and an empty endpoint family switches
		// auto-retirement off SILENTLY — noteModelGone's family gate rejects it
		// before recording a strike and says so only at Debug. It is safe to copy
		// where the rest is not, because ingest stamps it once and nothing writes
		// it afterwards, so it cannot race serveHedgeWinner the way providerName
		// and the timings would.
		//
		// Constraint on future edits: this throwaway must never reach
		// noteStreamOutcome, which judges a finished stream from fields the probe
		// never fills in (upstreamKind, the content flag). serveHedgeWinner
		// re-binds logData to the real st.logData before judging the model.
		snap.logData = &requestLogData{modelID: st.logData.modelID, providerName: candidates[idx].provider.Name, endpointType: st.logData.endpointType}
		go func() {
			results <- probeOne(ctx, &snap, candidates[idx], idx, ttftTimeout, stallTimeout)
		}()
	}
	// superseded is true only when another candidate won the race. The deferred
	// safety-net call passes false: an attempt still live at the overall deadline,
	// or when the client hung up, was not beaten by anything, and flagging it
	// would relabel those causes as a hedge loss.
	cancelExcept := func(except int, supersede bool) {
		for i := range cancels {
			if i != except && cancels[i] != nil {
				// Flag before cancelling: the attempt goroutine races us to
				// classify the resulting context.Canceled.
				if supersede && superseded[i] != nil {
					superseded[i].Store(true)
				}
				cancels[i]()
			}
		}
	}
	// Safety net: cancel any still-live attempt context on return. For the winner
	// this fires only after handleStreamingResponse has finished, so it does not
	// truncate the served stream.
	defer cancelExcept(-1, false)

	launch(0)
	nextIdx := 1
	hedgeTimer := time.NewTimer(st.hedgeDelay)
	defer hedgeTimer.Stop()
	deadlineTimer := time.NewTimer(time.Until(st.overallDeadline))
	defer deadlineTimer.Stop()

	// Preserve any provider stall across later non-stall failures: results arrive
	// out of order, so a provider_timeout can be overwritten in st.lastReqErr by a
	// subsequent provider_error. If the connection then drops, the silent stall is
	// still the honest cause (an intermediary, not the client, cut it), so a stall
	// seen at any point classifies a later disconnect as 502 rather than 499.
	var providerStall reqError
	// Candidates whose probes lost the race without a request because their
	// provider sat at its in-flight window: worth one bounded wait for a slot
	// before answering anyone with an all-busy error, exactly as the
	// sequential loop does. Turning hedging on must never make a merely-busy
	// group FAIL faster than leaving it off.
	var busyCandidates []modelCandidate

	for {
		select {
		case res := <-results:
			inFlight--
			settled[res.idx] = true
			if res.won {
				// Attempts still in flight were launched and lost, not skipped:
				// each gets a superseded record, so the trail and the per-provider
				// failover counter show the whole fan-out, which is exactly the
				// part of a hedge that cost the most. Settled BEFORE the cancel:
				// a cancelled probe answers at once, and a result provoked by our
				// own cancel must not be mistaken for one the provider gave.
				inFlight = settleHedgeLaunches(st.logData, results, candidates, launchedAt, settled, inFlight, res.idx, KindHedgeSuperseded, "superseded by the winner while in flight")
				cancelExcept(res.idx, true)
				// A runner-up that also produced a first token sent a live
				// *http.Response we will never stream; drain the still-outstanding
				// attempts in the background and close their bodies so the
				// connection is released promptly instead of leaking until the
				// transport idle timeout. Backgrounded so it never delays the
				// winner's first byte to the client.
				if inFlight > 0 {
					go drainHedgeResults(results, inFlight)
				}
				// The winner's trail record opens here and is closed by the
				// stream's terminal write, like a sequential attempt's.
				st.logData.openAttemptRecord(res.idx, candidates[res.idx], true, launchedAt[res.idx], st.circuitBreakerEnabled)
				st.logData.noteAttemptStatus(res.status)
				h.serveHedgeWinner(w, r, st, candidates[res.idx], res, stallTimeout)
				return
			}
			st.setReqErr(res.reqErr)
			st.logData.appendAttemptRecord(hedgeLoserRecord(res, candidates[res.idx], launchedAt[res.idx]))
			// Carry the loser's 429 verdict onto the shared state beside its
			// reqError, so a terminal all-busy exhaustion answers with the
			// provider's own Retry-After rather than the class default.
			st.rateLimit = res.rateLimit
			if res.busy {
				busyCandidates = append(busyCandidates, candidates[res.idx])
			}
			if res.reqErr.Kind == KindProviderTimeout {
				providerStall = res.reqErr
			}
			// A slot just freed: launch the next candidate eagerly rather than
			// waiting for the hedge tick.
			if nextIdx < len(candidates) {
				launch(nextIdx)
				nextIdx++
			}
			if inFlight == 0 && nextIdx >= len(candidates) {
				// Every probe has resolved. If some candidates were skipped at
				// their in-flight window, wait for the first slot to free and
				// serve there through the ordinary sequential attempt (the
				// race is over, so the shared state is single-threaded again).
				if len(busyCandidates) > 0 && h.retryAfterSlotFrees(w, r, st, busyCandidates, launched, h.attemptCandidate) {
					return
				}
				h.failAllExhausted(w, st, launched)
				return
			}
		case <-hedgeTimer.C:
			if nextIdx < len(candidates) {
				launch(nextIdx)
				nextIdx++
				if nextIdx < len(candidates) {
					hedgeTimer.Reset(st.hedgeDelay)
				}
			}
		case <-deadlineTimer.C:
			debuglog.Warn("proxy: overall request deadline exceeded during hedged streaming", "model", st.logData.modelID, "launched", launched, "deadline", st.overallDeadline)
			st.setReqErr(reqError{Kind: KindFailoverTimeout, Attempt: launched - 1, Provider: st.logData.providerName, Underlying: st.lastReqErr.Underlying})
			// The launches still running at the deadline are the most expensive
			// part of a hedge that timed out: the trail names them.
			inFlight = settleHedgeLaunches(st.logData, results, candidates, launchedAt, settled, inFlight, -1, KindFailoverTimeout, "still in flight at the failover deadline")
			if inFlight > 0 {
				go drainHedgeResults(results, inFlight)
			}
			h.failAllExhausted(w, st, launched)
			return
		case <-r.Context().Done():
			inFlight = settleHedgeLaunches(st.logData, results, candidates, launchedAt, settled, inFlight, -1, KindClientDisconnect, "client disconnected while in flight")
			if inFlight > 0 {
				go drainHedgeResults(results, inFlight)
			}
			h.failHedgeDisconnect(w, st, launched, providerStall)
			return
		}
	}
}

// probeStreamingCandidate runs the per-candidate prologue (build request, send
// upstream, record the breaker outcome, probe the first token) WITHOUT writing to
// the client, returning a hedgeResult the orchestrator can either stream (won) or
// drop (reqErr). It operates on the private per-attempt requestState snapshot that
// runHedgedStreaming.launch hands it, so the shared timing fields are never raced;
// the shared logData pointer is only read here, and the winner's identity is
// stamped onto it by serveHedgeWinner. Breaker success/failure is recorded per
// attempt exactly as dispatchStreaming does.
//
// A won return always carries an OPEN body that the orchestrator must stream or
// close. If the attempt's context was already cancelled (this candidate lost the
// race) the body is closed here and the result is downgraded to a non-win, so a
// runner-up never hands back a live connection.
func (h *Handler) probeStreamingCandidate(ctx context.Context, st *requestState, candidate modelCandidate, attempt int, ttftTimeout, stallTimeout time.Duration) (res hedgeResult) {
	res = hedgeResult{idx: attempt}
	// Whatever exit below, the breaker verdict the probe noted on its private
	// snapshot rides back to the orchestrator for the attempt trail.
	defer func() { res.breaker = st.logData.attemptBreaker }()
	if !st.circuitBreakerEnabled {
		st.logData.attemptBreaker = breakerDisabled
	}

	// The same admission gate beginAttempt passes: a provider at its learned
	// in-flight window loses this race slot without a request being made.
	if !h.admitCandidate(st, candidate) {
		res.reqErr = reqError{Kind: KindProviderSaturated, Attempt: attempt, Provider: candidate.provider.Name, Detail: "at in-flight limit"}
		res.rateLimit = st.rateLimit
		res.busy = true
		st.logData.noteBreaker(breakerNoop)
		return res
	}

	// Same stamp beginAttempt makes at attempt start, before the request is
	// built: launching an attempt against this provider counts as use, whether
	// or not the probe wins the race or even reaches the wire.
	h.touchProviderLastUsed(candidate.provider.ID)

	var dialMs float64
	proxyReq, providerType, _, err := h.buildCandidateRequest(ctx, st, candidate)
	if err != nil {
		st.attemptSlot.settle(false)
		res.reqErr = reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)}
		return res
	}

	resp, ok := h.doUpstream(ctx, proxyReq, st, candidate, attempt, &dialMs)
	res.timings = st.timings
	res.proxyOverhead = st.proxyOverhead
	if !ok {
		// doUpstream set st.lastReqErr (on the private snapshot) and recorded any
		// breaker failure.
		st.attemptSlot.settle(false)
		res.reqErr = st.lastReqErr
		return res
	}
	res.respHeaderMs = float64(time.Since(st.startTime).Microseconds()) / 1000.0

	// MiniMax reports business errors (rate limit, exhausted plan balance,
	// auth failures) inside an HTTP 200 envelope; remap them to an effective
	// status so the breaker/failover paths below — all keyed on status codes —
	// see the failure. The slot rides the body only from here, with the
	// remapped status deciding the clean flag: losers are drained and closed
	// by the orchestrator, the winner's stream closes at its end, and either
	// settles it.
	resp = remapMiniMaxBusinessError(providerType, candidate.provider.Name, resp)
	h.finishAttemptAdmission(st, candidate, resp)
	res.status = resp.StatusCode

	isFailoverEligible := h.shouldFailover(ctx, resp.StatusCode)
	rl := h.judge429AndRecordBreaker(ctx, st, candidate, resp, isFailoverEligible)

	if !servedSuccessStatus(resp.StatusCode) {
		// Any non-2xx drops this candidate. The orchestrator owns the terminal
		// write if every candidate fails; drain so the connection can be reused,
		// keeping only as much as the two readers below can use.
		//
		// The cap follows the status because the two readers want different
		// amounts. classifyUpstreamError never sees past SanitizeLogBody's 10 000
		// bytes, which failoverErrorClassifyCap is sized against; but a 400 also
		// goes to learnResponsesRequirement, which json.Unmarshals it, and a
		// document cut short does not parse at all.
		readCap := int64(failoverErrorClassifyCap)
		if resp.StatusCode == http.StatusBadRequest {
			readCap = responsesLearnBodyCap
		}
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, readCap))
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusBadRequest && st.sentChatCompletionsBody() {
			// A hedged probe cannot retry in-race (a second upstream round-trip
			// inside one race slot would skew the TTFT contest), but it can
			// still LEARN the /v1/responses requirement from the 400 so every
			// subsequent request — hedged or sequential — routes preemptively.
			h.learnResponsesRequirement(st, candidate, providerType, errBody)
			// Same reasoning for rejected/renamed params: the retry is refused
			// in-race, but learning here means the next request — hedged or
			// sequential — is built without the params this model refuses.
			h.learnRejectedParams(candidate, errBody)
			// Both readings are only valid on a chat-completions attempt: a
			// dialect 400 names that dialect's fields, and a strip mislearned
			// from one poisons the compat path for this model on every later
			// request. The sequential path gates its own 400 handling the same
			// way.
		}
		if resp.StatusCode == http.StatusBadRequest && st.anthropicEgressAttempt {
			// The Messages route's own learnable 400, gated the opposite way: a
			// thinking-dialect complaint only arrives on an egress attempt, and
			// naming a dialect is the whole content of it. Learning here spares
			// every later request to this model the refusal, even though this
			// race slot cannot re-issue to recover the current one.
			if dialect, ok := anthropicegress.DialectFromError(errBody); ok {
				h.learnThinkingDialect(candidate, dialect)
			}
		}
		// A hedged race drops every candidate but the winner here, so without
		// this a dead model in a hedged group accrued strikes only on the runs it
		// happened to win — almost never, since a model answering 404 loses the
		// TTFT contest to anything that works. Same classification the sequential
		// and pass-through loops do, on a body being discarded either way.
		errBodyMsg := util.SanitizeLogBody(string(errBody), 10000)
		kind, _ := classifyUpstreamError(resp.StatusCode, errBodyMsg, candidate.model.ModelID)
		if kind == KindProviderModelGone {
			h.noteModelGone(candidate, st.logData.endpointType)
		}
		res.reqErr = failoverReqErr(rl, attempt, candidate.provider.Name, resp.StatusCode)
		res.rateLimit = rl
		return res
	}

	if st.responsesAttempt {
		// Preemptive /v1/responses attempt (learned earlier on the sequential
		// path): translate the upstream stream back to chat chunks before the
		// TTFT probe so the whole hedged pipeline sees chat-completions SSE.
		// st is this attempt's private snapshot, so the flag set by
		// buildCandidateRequest is visible right here — no shared-state race.
		resp.Body = openairesponses.NewStreamAdapter(resp.Body, st.reqModel)
	}
	if st.geminiAttempt {
		// Vertex-express candidate in a hedged race: same upstream-side
		// translation so the hedged pipeline sees chat-completions SSE.
		resp.Body = gemini.NewStreamAdapter(resp.Body, st.reqModel)
	}
	if st.anthropicEgressAttempt {
		// Anthropic egress candidate in a hedged race: same upstream-side
		// translation so the hedged pipeline sees chat-completions SSE.
		resp.Body = anthropicegress.NewStreamAdapter(resp.Body, st.reqModel)
	}

	if ttftTimeout <= 0 {
		// No TTFT probe configured: a success status is an immediate win (backward compat).
		return commitHedgeWin(ctx, res, resp, nil, 0, candidate)
	}

	probeBuf, trueTtftMs, probeErr := h.probeFirstToken(ctx, resp.Body, ttftTimeout, st.startTime)
	if probeErr != nil {
		_ = resp.Body.Close()
		// clientGone uses the attempt context: a loser the orchestrator cancelled
		// (because another candidate won) reads as a fast cancel and is correctly
		// NOT charged to the breaker, while our own TTFT timer firing or a stall
		// past the floor is a provider fault. Mirrors dispatchStreaming.
		clientGone := ctx.Err() != nil
		elapsed := time.Since(st.startTime)
		re, recordFailure := classifyProbeError(probeErr, candidate.provider.Name, newCredentialMasker(candidate.apiKey), clientGone, elapsed, stallTimeout, ttftTimeout, attempt)
		if recordFailure && st.circuitBreakerEnabled {
			debuglog.Warn("proxy: recording circuit breaker failure", "reason", "hedged TTFT probe failed", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "attempt", attempt, "kind", string(re.Kind), "duration_ms", elapsed.Milliseconds(), "error", re.Underlying)
			st.logData.noteBreaker(breakerCharge)
			h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), failover.Cause{Status: resp.StatusCode, Reason: "hedged TTFT probe failed"})
		}
		res.reqErr = re
		return res
	}

	// No breaker success here either: the winner's stream is judged by
	// finalizeStream, and a runner-up whose stream is never read is no evidence
	// of anything. See judgeStreamForBreaker.
	return commitHedgeWin(ctx, res, resp, probeBuf, trueTtftMs, candidate)
}

// commitHedgeWin finalizes a streamable probe success on the partially-built res
// (which already carries the attempt's timing fields). If the attempt's context
// was cancelled in the meantime (the orchestrator already picked another winner),
// the open body is closed and the result is downgraded to a client-disconnect drop
// so no runner-up connection leaks.
func commitHedgeWin(ctx context.Context, res hedgeResult, resp *http.Response, preReadBuf *bytes.Buffer, trueTtftMs float64, candidate modelCandidate) hedgeResult {
	if ctx.Err() != nil {
		_ = resp.Body.Close()
		res.reqErr = reqError{Kind: hedgeAbandonKind(ctx), Attempt: res.idx, Provider: candidate.provider.Name}
		return res
	}
	res.won = true
	res.resp = resp
	res.preReadBuf = preReadBuf
	res.trueTtftMs = trueTtftMs
	return res
}

// settleHedgeLaunches closes out the launches a hedged race is leaving behind
// when it exits, so the trail names every attempt that was made and the
// per-provider failover counter reads the whole fan-out. Two kinds of leftover:
//
//   - A result already queued in results resolved before the exit, whichever
//     the select happened to read first, so it is recorded from its own
//     verdict (a real loser with its status, error and breaker charge). A
//     queued win is a runner-up whose response will never be streamed; it is
//     abandoned like the rest.
//   - A launch with no result yet is abandoned: recorded as kind/detail with
//     no status, because nothing is known about what it would have answered.
//
// except is the winner's index (never abandoned), or -1 on an exit with no
// winner. Bodies of drained results are closed here. Returns the launches
// still in flight, for the background drain to wait on. Runs on the
// orchestrator goroutine, which is the only writer of logData.
func settleHedgeLaunches(logData *requestLogData, results <-chan hedgeResult, candidates []modelCandidate, launchedAt []time.Time, settled []bool, inFlight, except int, kind ErrorKind, detail string) int {
	for drained := false; !drained; {
		select {
		case res := <-results:
			inFlight--
			settled[res.idx] = true
			if res.resp != nil {
				_ = res.resp.Body.Close()
			}
			if res.won {
				logData.appendAttemptRecord(hedgeAbandonedRecord(res.idx, candidates[res.idx], launchedAt[res.idx], kind, detail))
			} else {
				logData.appendAttemptRecord(hedgeLoserRecord(res, candidates[res.idx], launchedAt[res.idx]))
			}
		default:
			drained = true
		}
	}
	for i := range candidates {
		if i != except && !launchedAt[i].IsZero() && !settled[i] {
			logData.appendAttemptRecord(hedgeAbandonedRecord(i, candidates[i], launchedAt[i], kind, detail))
		}
	}
	return inFlight
}

// drainHedgeResults consumes n still-outstanding hedge results and closes any body
// they carry. Only a won runner-up carries an open body (every failover result
// already closed its own); closing here releases that connection instead of
// holding it until the transport idle timeout.
func drainHedgeResults(results <-chan hedgeResult, n int) {
	for range n {
		res := <-results
		if res.resp != nil {
			_ = res.resp.Body.Close()
		}
	}
}

// serveHedgeWinner stamps the winning candidate's identity and accumulated timings
// onto the shared requestState/logData and streams its response, reusing the same
// handleStreamingResponse path as the sequential dispatch (the pre-read probe buffer
// is replayed before the live body).
func (h *Handler) serveHedgeWinner(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, res hedgeResult, stallTimeout time.Duration) {
	logData := st.logData
	logData.providerID = candidate.provider.ID
	logData.providerName = candidate.provider.Name
	logData.masker = newCredentialMasker(candidate.apiKey)
	if st.isFailover {
		logData.resolvedModelID = candidate.model.ModelID
	}
	logData.failoverAttempt = res.idx
	st.timings = res.timings
	st.proxyOverhead = res.proxyOverhead

	opts := streamOptions{
		preReadBuf:         res.preReadBuf,
		trueTtftMs:         res.trueTtftMs,
		responseHeaderMs:   res.respHeaderMs,
		streamStallTimeout: stallTimeout,
		providerID:         candidate.provider.ID,
		providerName:       candidate.provider.Name,
		model:              candidateModelID(candidate),
		circuitBreakerOn:   st.circuitBreakerEnabled,
		proxyOverheadMs:    st.proxyOverhead,
		parseMs:            st.parseMs,
		failoverLookupMs:   st.timings.failoverLookupMs,
		modelLookupMs:      st.timings.modelLookupMs,
		providerLookupMs:   st.timings.providerLookupMs,
		keyDecryptMs:       st.timings.keyDecryptMs,
		dialMs:             st.timings.dialMs,
		settingsReadMs:     st.timings.settingsReadMs,
		vkHash:             st.vkHash,
		attempt:            res.idx,
		cancelOrigin:       "failover_timeout",
		masker:             logData.masker,
	}
	// attempt is the 0-based failover_attempt this request is logged and stored
	// with; it must match the value stream_finalize reports for the same request.
	debuglog.Info("proxy: hedge winner", "provider", candidate.provider.Name, "attempt", res.idx, "true_ttft_ms", res.trueTtftMs)
	h.handleStreamingResponse(w, r, logData, res.resp, st.startTime, opts)
	// Same verdict the sequential dispatch applies: a hedged winner is still a
	// real stream, so a model reported gone mid-stream must strike here too.
	h.noteStreamOutcome(logData, candidate)
}

// failHedgeDisconnect handles r.Context() cancellation during a hedged race. It
// reuses the PR #258 classification: if the most recent attempt was a zero-token
// provider stall, the silent connection was most likely dropped by an intermediary
// (reverse proxy / LB / CDN) rather than the client, so the provider stall is
// preserved as the terminal cause (502); otherwise it is a genuine client
// disconnect (499).
func (h *Handler) failHedgeDisconnect(w http.ResponseWriter, st *requestState, launched int, providerStall reqError) {
	// Prefer a provider stall seen at any point in the race over whatever happens
	// to be the most recent result: a later non-stall failure must not relabel a
	// post-stall disconnect as a client hangup.
	cause := st.lastReqErr
	if providerStall.Kind == KindProviderTimeout {
		cause = providerStall
	}
	if cause.Kind == KindProviderTimeout {
		status := cause.terminalStatus()
		logMsg := cause.terminalLogMessage(st.isFailover, launched)
		clientMsg := cause.terminalClientMessage(st.reqModel, st.isFailover)
		debuglog.Info("proxy: connection closed during hedged streaming after provider stall", "model", st.logData.modelID, "provider", st.logData.providerName, "launched", launched, "kind", string(cause.Kind), "status", status)
		h.failRequest(st.logData, status, cause.Kind, logMsg, launched-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
		writeOpenAIError(w, clientMsg, status)
		return
	}
	debuglog.Info("proxy: client disconnected during hedged streaming", "model", st.logData.modelID, "provider", st.logData.providerName, "launched", launched)
	st.setReqErr(reqError{Kind: KindClientDisconnect, Attempt: launched - 1, Provider: st.logData.providerName, Underlying: st.lastReqErr.Underlying})
	h.failRequest(st.logData, statusClientClosedRequest, KindClientDisconnect, st.lastErr, launched-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
	writeOpenAIError(w, "client disconnected", statusClientClosedRequest)
}
