package proxy

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// The circuit-breaker verdict for a non-streaming attempt lives here, beside
// the failover loop that produces it rather than inside it — the streaming
// verdict has the same arrangement in stream_finalize.go, and keeping the two
// policies in named places is what stops a second opinion growing at a call
// site. Split out of proxy_failover.go when that file hit the size ceiling.

// recordBreakerOutcome records the circuit-breaker result for a completed
// upstream attempt (phase D8). It is a no-op when the breaker is disabled.
//
// For a failover-eligible status it applies the breakerRecordAction mapping
// (failure / no-op / success). For a non-eligible status it records a success,
// except for a 200 — on either path.
//
// A 200 is a status, not an answer, and these headers arrive before a byte of
// the body has been read. The verdict is deferred to whoever reads it:
// judgeStreamForBreaker once the stream ends, recordAnswerOutcome once the
// completion is decoded. Crediting here meant a provider answering
// {"choices":[]} to every request recorded a success every time — and
// RecordSuccess resets consecutiveFails, so its circuit could never open. #805
// made that argument for streams and this is the same argument for completions.
func (h *Handler) recordBreakerOutcome(st *requestState, candidate modelCandidate, statusCode int, isFailoverEligible bool) {
	if !st.circuitBreakerEnabled {
		return
	}
	if isFailoverEligible {
		// Determine breaker action from status code.
		// See breakerRecordAction for the full status→action mapping.
		switch breakerRecordAction(statusCode) {
		case breakerActionFailure:
			// The hedged probe path reaches this with no other log of the
			// upstream status, so without this line a breaker opening on
			// repeated 5xx has no recorded cause anywhere.
			debuglog.Warn("proxy: recording circuit breaker failure", "reason", "upstream status", "status", statusCode, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
			h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
		case breakerActionNoOp:
			// Model-specific client error (404/499): provider is alive
			// but rejecting this request. No-op for the breaker — neither
			// failure nor success. Recording success would erase real 5xx
			// failure history (resetting consecutiveFails in Closed state)
			// and could prematurely close a half-open circuit based on a
			// model-specific error that says nothing about provider health.
		case breakerActionSuccess:
			// Not reached for failover-eligible codes: shouldFailover only
			// returns true for {5xx,429,401,403,402,404,499}, all of which map
			// to failure or no-op above. Retained so the switch stays exhaustive
			// over breakerAction — if the shouldFailover/breakerRecordAction
			// mappings ever diverge, a success is recorded rather than dropped.
			h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
		}
		return
	}
	if statusCode != http.StatusOK {
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
	}
}

// recordAnswerOutcome records the circuit-breaker verdict for a finished
// non-streaming attempt, and is the completion-side sibling of
// judgeStreamForBreaker.
//
// The bar is producedOutput, the same one the retirement verdict uses two lines
// from each call site — text, content parts, reasoning, a tool call, or usage
// reporting completion tokens. `200 {"choices":[]}` is what an aggregator in
// front of a retired model returns between its refusals, and it is not an
// answer whichever question is being asked of it.
//
// A state other than "completed" means the attempt failed after the headers: a
// body read that died, a body the gateway could not decode, an upstream that
// sent a success shape behind a failure status. The old code had already
// credited every one of those before the read was even attempted.
func (h *Handler) recordAnswerOutcome(st *requestState, candidate modelCandidate, logData *requestLogData, clientGone bool) {
	if !st.circuitBreakerEnabled {
		return
	}
	// A client that hangs up mid-read is not a provider failure. The upstream
	// body is read under the caller's context, so a user pressing stop, or a
	// load balancer's client-side timeout, surfaces here as a failed read — and
	// charging for it would open the circuit for every tenant after N impatient
	// cancels. doUpstream, judgeStreamForBreaker, classifyProbeError and the
	// pass-through first-byte probe all refuse to charge for this; this was the
	// one verdict that did not.
	if clientGone {
		return
	}
	if logData.state != "completed" {
		// The attempt failed after the headers. Only a fault that is the
		// PROVIDER'S counts — the same predicate judgeStreamForBreaker uses, and
		// for the same reason it uses it. A 2xx this gateway could not decode is
		// classified provider_bad_request and still holds the model's text (a
		// relay quoting its token counts is the named cause), so it says nothing
		// about whether the provider is up; the streaming side calls recording
		// nothing the honest verdict there, and it is the honest verdict here.
		if providerAtFault(logData.errorKind) {
			h.chargeBreaker(st, candidate, "response failed after headers")
		}
		return
	}
	if producedOutput(logData) {
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
		return
	}
	h.chargeBreaker(st, candidate, "response completed without delivering content")
}

// chargeBreaker records one breaker failure with its cause in the log. The
// cause is worth the line: a breaker that opens on anything other than an
// upstream status has no other record of why, which is the gap the
// "recording circuit breaker failure" warn was added to close.
func (h *Handler) chargeBreaker(st *requestState, candidate modelCandidate, reason string) {
	if !st.circuitBreakerEnabled {
		return
	}
	debuglog.Warn("proxy: recording circuit breaker failure", "reason", reason, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
	h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
}

// rejectUntranslatableBody is the single outcome all three egress adapters have
// for a 200 whose body they cannot turn into a completion.
//
// One place because the outcome has four parts — the log, the breaker charge,
// the request error and the failover — and three copies of it is how the charge
// came to be missing from all three: a 200 was credited at header time, before
// any translation was attempted, and each adapter's own comment already called
// this a provider fault. Mutation testing then showed two of the three copies
// had no test behind them.
func (h *Handler) rejectUntranslatableBody(st *requestState, candidate modelCandidate, logData *requestLogData, adapter string, err error, attempt int) candidateOutcome {
	debuglog.Warn("proxy: upstream body translation failed", "adapter", adapter, "error", err, "model", logData.modelID, "provider", logData.providerName)
	h.chargeBreaker(st, candidate, "upstream body could not be translated")
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)})
	logData.failoverAttempt = attempt
	return outcomeFailover
}
