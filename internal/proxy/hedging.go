package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

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
	launched := 0
	inFlight := 0

	launch := func(idx int) {
		// failover_timeout origin so doUpstream classifies a deadline the same way
		// the sequential path does; failoverTimeout is the per-attempt budget
		// (request_timeout x10 for streaming).
		ctx, cancel := context.WithCancel(r.Context())
		ctx = context.WithValue(ctx, ctxkeys.CancelOriginKey, "failover_timeout")
		ctx, timeoutCancel := context.WithTimeout(ctx, st.failoverTimeout)
		cancels[idx] = func() { timeoutCancel(); cancel() }
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
		snap.logData = &requestLogData{modelID: st.logData.modelID, providerName: candidates[idx].provider.Name}
		go func() {
			results <- probeOne(ctx, &snap, candidates[idx], idx, ttftTimeout, stallTimeout)
		}()
	}
	cancelExcept := func(except int) {
		for i := range cancels {
			if i != except && cancels[i] != nil {
				cancels[i]()
			}
		}
	}
	// Safety net: cancel any still-live attempt context on return. For the winner
	// this fires only after handleStreamingResponse has finished, so it does not
	// truncate the served stream.
	defer cancelExcept(-1)

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

	for {
		select {
		case res := <-results:
			inFlight--
			if res.won {
				cancelExcept(res.idx)
				// A runner-up that also produced a first token sent a live
				// *http.Response we will never stream; drain the still-outstanding
				// attempts in the background and close their bodies so the
				// connection is released promptly instead of leaking until the
				// transport idle timeout. Backgrounded so it never delays the
				// winner's first byte to the client.
				if inFlight > 0 {
					go drainHedgeResults(results, inFlight)
				}
				h.serveHedgeWinner(w, r, st, candidates[res.idx], res, stallTimeout)
				return
			}
			st.setReqErr(res.reqErr)
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
			if inFlight > 0 {
				go drainHedgeResults(results, inFlight)
			}
			h.failAllExhausted(w, st, launched)
			return
		case <-r.Context().Done():
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
func (h *Handler) probeStreamingCandidate(ctx context.Context, st *requestState, candidate modelCandidate, attempt int, ttftTimeout, stallTimeout time.Duration) hedgeResult {
	res := hedgeResult{idx: attempt}

	var dialMs float64
	proxyReq, _, _, err := h.buildCandidateRequest(ctx, st, candidate)
	if err != nil {
		res.reqErr = reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)}
		return res
	}

	resp, ok := h.doUpstream(ctx, proxyReq, st, candidate, attempt, &dialMs)
	res.timings = st.timings
	res.proxyOverhead = st.proxyOverhead
	if !ok {
		// doUpstream set st.lastReqErr (on the private snapshot) and recorded any
		// breaker failure.
		res.reqErr = st.lastReqErr
		return res
	}
	res.respHeaderMs = float64(time.Since(st.startTime).Microseconds()) / 1000.0

	isFailoverEligible := h.shouldFailover(ctx, resp.StatusCode)
	h.recordBreakerOutcome(st, candidate, resp.StatusCode, isFailoverEligible)

	if resp.StatusCode != http.StatusOK {
		// Any non-200 drops this candidate. The orchestrator owns the terminal
		// write if every candidate fails; drain so the connection can be reused.
		errBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusBadRequest {
			// A hedged probe cannot retry in-race (a second upstream round-trip
			// inside one race slot would skew the TTFT contest), but it can
			// still LEARN the /v1/responses requirement from the 400 so every
			// subsequent request — hedged or sequential — routes preemptively.
			h.learnResponsesRequirement(st, candidate, provider.DetectProviderType(candidate.provider.BaseURL), errBody)
		}
		res.reqErr = reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)}
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

	if ttftTimeout <= 0 {
		// No TTFT probe configured: a 200 is an immediate win (backward compat).
		if st.circuitBreakerEnabled {
			h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
		}
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
		re, recordFailure := classifyProbeFailure(candidate.provider.Name, errString(probeErr), clientGone, elapsed, stallTimeout, ttftTimeout, attempt)
		if recordFailure && st.circuitBreakerEnabled {
			h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
		}
		res.reqErr = re
		return res
	}

	if st.circuitBreakerEnabled {
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
	}
	return commitHedgeWin(ctx, res, resp, probeBuf, trueTtftMs, candidate)
}

// commitHedgeWin finalizes a streamable probe success on the partially-built res
// (which already carries the attempt's timing fields). If the attempt's context
// was cancelled in the meantime (the orchestrator already picked another winner),
// the open body is closed and the result is downgraded to a client-disconnect drop
// so no runner-up connection leaks. The breaker success recorded by the caller is
// left intact: the provider really did produce a first token.
func commitHedgeWin(ctx context.Context, res hedgeResult, resp *http.Response, preReadBuf *bytes.Buffer, trueTtftMs float64, candidate modelCandidate) hedgeResult {
	if ctx.Err() != nil {
		_ = resp.Body.Close()
		res.reqErr = reqError{Kind: KindClientDisconnect, Attempt: res.idx, Provider: candidate.provider.Name}
		return res
	}
	res.won = true
	res.resp = resp
	res.preReadBuf = preReadBuf
	res.trueTtftMs = trueTtftMs
	return res
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
	}
	debuglog.Info("proxy: hedge winner", "provider", candidate.provider.Name, "attempt", res.idx+1, "true_ttft_ms", res.trueTtftMs)
	h.handleStreamingResponse(w, r, logData, res.resp, st.startTime, opts)
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
