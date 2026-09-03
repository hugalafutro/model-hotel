package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// The circuit-breaker verdict for a non-streaming attempt lives here, beside
// the failover loop that produces it rather than inside it. The streaming
// verdict has the same arrangement in stream_finalize.go: keeping the two
// policies in named places stops a second opinion growing at a call site.

// recordBreakerOutcome records the circuit-breaker result for a completed
// upstream attempt (phase D8). It is a no-op when the breaker is disabled.
//
// The verdict is recorded against the candidate's RESOLVED UPSTREAM model, the
// key every other breaker site on this request uses: the skip in
// resolveCandidates, the success in recordAnswerOutcome, the stream verdict in
// finalizeStream. Two of them disagreeing is silent, because RecordFailure
// creates whatever circuit it is handed, so the ledger fills up under a key
// nothing ever reads and the failing model stays in rotation.
//
// For a failover-eligible status it applies the breakerRecordAction mapping
// (failure / no-op / success), refined for a CLASSIFIED 429 by rl:
//
//   - saturated: a breaker NO-OP, like 404/499, neither charge nor credit. A
//     provider at its concurrency ceiling is alive, and charging it benches
//     the very slots that are all busy serving. Not a success either:
//     RecordSuccess resets consecutiveFails, and a provider alternating 500 /
//     429-busy / 500 must still open. On a half-open probe this leaves the
//     circuit half-open with failedProbes untouched: a busy signal is not a
//     failed probe, and doubling the backoff for it benches a healthy provider
//     for the whole cooldown.
//   - exhausted: charged to the threshold at once (RecordExhausted), so one
//     spent-window 429 opens the circuit instead of wasting a second request
//     confirming it, unless circuit_breaker_open_on_exhaustion is OFF, which
//     demotes it to an unclassified charge.
//   - unknown: charged via RecordRateLimited, so the breaker can still
//     escalate a circuit that only ever opens on 429s.
//
// A 2xx is a status, not an answer, and these headers arrive before a byte of
// the body has been read. The verdict is deferred to whoever reads it:
// judgeStreamForBreaker once the stream ends, recordAnswerOutcome once the
// completion is decoded. Crediting here would record a success for a provider
// answering {"choices":[]} to every request, and RecordSuccess resets
// consecutiveFails, so its circuit could never open.
func (h *Handler) recordBreakerOutcome(ctx context.Context, st *requestState, candidate modelCandidate, statusCode int, isFailoverEligible bool, rl rateLimitVerdict) {
	if !st.circuitBreakerEnabled {
		return
	}
	if isFailoverEligible {
		if statusCode == http.StatusTooManyRequests && rl.class != rateLimitUnknown {
			h.recordRateLimitOutcome(ctx, st, candidate, rl)
			return
		}
		// A 402 is the provider stating the account cannot pay. That is the same
		// claim an entitled 429 ("insufficient balance") makes, and it gets the
		// same treatment: retrying cannot succeed until someone pays, so the
		// circuit opens on this one response and takes the until-paid pin rather
		// than serving out a threshold of failures and then half-opening back to
		// live every cooldown. Routed before breakerRecordAction, which maps 402
		// to an ordinary failure.
		if statusCode == http.StatusPaymentRequired {
			h.recordPaymentRequiredOutcome(ctx, st, candidate)
			return
		}
		// Determine breaker action from status code.
		// See breakerRecordAction for the full status→action mapping.
		switch breakerRecordAction(statusCode) {
		case breakerActionFailure:
			// The hedged probe path reaches this with no other log of the
			// upstream status, so without this line a breaker opening on
			// repeated 5xx has no recorded cause anywhere.
			debuglog.Warn("proxy: recording circuit breaker failure", "reason", "upstream status", "status", statusCode, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
			st.logData.noteBreaker(breakerCharge)
			if statusCode == http.StatusTooManyRequests && rl.classified {
				// Unclassifiable rate limit: charged like any other failure,
				// and the breaker additionally counts the open (if one
				// results) toward the 429-only escalation. Only while
				// classification is on: its master switch gates the
				// escalation too.
				h.circuitBreaker.RecordRateLimited(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), failover.UpstreamStatus(statusCode, "unrecognised"))
				return
			}
			h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), failover.UpstreamStatus(statusCode, ""))
		case breakerActionNoOp:
			// Model-specific client error (404/499): provider is alive
			// but rejecting this request. No-op for the breaker: neither
			// failure nor success. Recording success would erase real 5xx
			// failure history (resetting consecutiveFails in Closed state)
			// and could prematurely close a half-open circuit based on a
			// model-specific error that says nothing about provider health.
			st.logData.noteBreaker(breakerNoop)
		case breakerActionSuccess:
			// Not reached for failover-eligible codes: shouldFailover only
			// returns true for {5xx,429,401,403,402,404,499}, which map to
			// failure or no-op above. Retained so the switch stays exhaustive
			// over breakerAction: if the shouldFailover/breakerRecordAction
			// mappings ever diverge, a success is recorded rather than dropped.
			st.logData.noteBreaker(breakerSuccess)
			h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate))
		}
		return
	}
	// Not a 2xx. Any 2xx defers its verdict to whoever reads the body
	// (recordAnswerOutcome or judgeStreamForBreaker): RecordSuccess resets
	// consecutiveFails, so crediting here at header time erases the charge the
	// answer verdict is about to make and the circuit can never open above a
	// threshold of one.
	//
	// RecordAlive, not RecordSuccess: a plain 400 proves the provider alive
	// (same credit) but served nothing, and the 429 behavioural fallback must
	// not read it as a recent serve.
	if !servedSuccessStatus(statusCode) {
		st.logData.noteBreaker(breakerAlive)
		h.circuitBreaker.RecordAlive(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), statusCode)
	}
}

// recordRateLimitOutcome is the classified-429 arm of recordBreakerOutcome:
// saturated is a logged no-op, exhausted opens at once (behind its setting).
func (h *Handler) recordRateLimitOutcome(ctx context.Context, st *requestState, candidate modelCandidate, rl rateLimitVerdict) {
	switch rl.class {
	case rateLimitSaturated:
		// Info, not warn: the provider is healthy and at capacity, which is a
		// routing fact, not an incident. The circuit is neither charged nor
		// credited; it only remembers the verdict, so the status page can say
		// "busy" about a closed circuit whose last answers were all 429s.
		debuglog.Info("proxy: 429 classified saturated, circuit breaker not charged", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "retry_after", rl.retryAfter, "model", candidateModelID(candidate))
		st.logData.noteBreaker(breakerNoop)
		h.circuitBreaker.RecordSaturated(candidate.provider.ID, candidateModelID(candidate))
	case rateLimitExhausted:
		st.logData.noteBreaker(breakerCharge)
		if !h.settingsRepo.GetBool(ctx, "circuit_breaker_open_on_exhaustion", true) {
			debuglog.Warn("proxy: recording circuit breaker failure", "reason", "upstream status", "status", http.StatusTooManyRequests, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
			h.circuitBreaker.RecordRateLimited(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), failover.UpstreamStatus(http.StatusTooManyRequests, "exhausted, not opened by setting"))
			return
		}
		if rl.account {
			// The account behind the provider is what refused, so the pin
			// speaks for every model of the provider: the next request to a
			// sibling is skipped rather than sent to draw the same refusal.
			debuglog.Warn("proxy: recording circuit breaker exhaustion", "reason", "upstream 429 (account exhausted)", "status", http.StatusTooManyRequests, "pin_hint_ms", rl.pinHint.Milliseconds(), "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
			h.circuitBreaker.RecordExhaustedAccount(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), http.StatusTooManyRequests, rl.pinHint)
			return
		}
		debuglog.Warn("proxy: recording circuit breaker exhaustion", "reason", "upstream 429 (exhausted)", "status", http.StatusTooManyRequests, "pin_hint_ms", rl.pinHint.Milliseconds(), "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
		h.circuitBreaker.RecordExhausted(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), http.StatusTooManyRequests, rl.pinHint)
	case rateLimitUnknown:
		// Not reached: the caller only routes classified verdicts here. Listed
		// so the switch stays exhaustive over rateLimitClass.
	}
}

// recordPaymentRequiredOutcome is the 402 arm of recordBreakerOutcome, and the
// status-code sibling of the entitled half of recordRateLimitOutcome: both are
// the provider saying a person has to pay before anything works again.
//
// It shares that arm's setting deliberately. circuit_breaker_open_on_exhaustion
// is the operator's answer to "may one refusal open a circuit"; a 402 is the
// same question with a different number on it, so switching the setting off
// restores the ordinary charge here too rather than leaving one exhaustion path
// still opening at once.
//
// This does NOT confine the damage to one model, nor darken the provider at
// once the way its 429 sibling does on an account-wide body: a 402 carries
// no body the classifier reads, so the charge lands on the model that drew
// it, and with SpanModels at its default of 2 the second model to take a 402
// darkens the whole provider; if the quota advisor already holds an
// exhaustion reading for it, applyQuotaPin prefers the advisor source and the
// provider-wide arm can trip on the first one. That is the right default for a
// billing block, which is account-wide by nature, and the wrong one for a
// provider that returns 402 per-request (OpenRouter answers 402 when a single
// request's max cost exceeds the balance), where two large requests can bench
// models that would have served. The operator's lever for that is
// circuit_breaker_open_on_exhaustion.
func (h *Handler) recordPaymentRequiredOutcome(ctx context.Context, st *requestState, candidate modelCandidate) {
	st.logData.noteBreaker(breakerCharge)
	if !h.settingsRepo.GetBool(ctx, "circuit_breaker_open_on_exhaustion", true) {
		debuglog.Warn("proxy: recording circuit breaker failure", "reason", "upstream status", "status", http.StatusPaymentRequired, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
		h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), failover.UpstreamStatus(http.StatusPaymentRequired, "payment required, not opened by setting"))
		return
	}
	debuglog.Warn("proxy: recording circuit breaker exhaustion", "reason", "upstream 402 (payment required)", "status", http.StatusPaymentRequired, "pin_hint_ms", pinHintUntilPaid.Milliseconds(), "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
	h.circuitBreaker.RecordExhausted(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), http.StatusPaymentRequired, pinHintUntilPaid)
}

// recordAnswerOutcome records the circuit-breaker verdict for a finished
// non-streaming attempt, and is the completion-side sibling of
// judgeStreamForBreaker.
//
// The bar is emptyCompletion (did ANYTHING come back) and deliberately not the
// retirement verdict's bar two lines from each call site. See
// answerCarriesSomething for why the two questions differ and why the answer to
// this one cannot be `len(Choices) == 0`.
//
// A state other than "completed" means the attempt failed after the headers: a
// body read that died, a body the gateway could not decode, an upstream that
// sent a success shape behind a failure status.
//
// status is the status the UPSTREAM answered, handed in by the caller rather
// than read back off logData: a failure handler may have replaced
// logData.statusCode with the 502 this gateway answers the client, and a
// translation failure never wrote it at all, and neither is what the
// provider said.
func (h *Handler) recordAnswerOutcome(st *requestState, candidate modelCandidate, logData *requestLogData, status int) {
	if !st.circuitBreakerEnabled {
		return
	}
	// Read here rather than at the call sites: two copies of the same expression
	// is how one of them comes to be missing it.
	if logData.state != "completed" {
		// The attempt failed after the headers.
		//
		// The KIND decides, and nothing else. Every way an attempt can fail after
		// the headers already has one: a caller hanging up, this gateway's own
		// request_timeout, a body it could not decode, a body that died on the
		// wire. providerAtFault excludes all but the last, which is the same
		// predicate judgeStreamForBreaker uses. No second client-gone guard
		// belongs here: one reading the CLIENT's context is blind to a
		// failover-timeout cancel and has to agree with this rule anyway.
		if !providerAtFault(logData.errorKind) {
			return
		}
		h.chargeBreaker(st, candidate, status, "response failed after headers")
		return
	}
	// A completed answer is credited even if the caller has since gone: the body
	// was read, the provider answered, and withholding the credit leaves older
	// failures on the clock to open a healthy circuit later.
	if logData.emptyCompletion {
		h.chargeBreaker(st, candidate, status, "response completed without delivering content")
		return
	}
	logData.noteBreaker(breakerSuccess)
	h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate))
}

// deferAnswerJudgement binds recordAnswerOutcome to the attempt so the
// terminal write can run it first (see requestLogData.judgeAnswer), and
// judgeAnswerNow runs it afterwards only if no terminal write did.
func (h *Handler) deferAnswerJudgement(st *requestState, candidate modelCandidate, logData *requestLogData, status int) {
	logData.judgeAnswer = func() { h.recordAnswerOutcome(st, candidate, logData, status) }
}

func judgeAnswerNow(logData *requestLogData) {
	if judge := logData.judgeAnswer; judge != nil {
		logData.judgeAnswer = nil
		judge()
	}
}

// chargeBreaker records one breaker failure with its cause in the log. The
// cause is worth the line: a breaker that opens on anything other than an
// upstream status has no other record of why.
//
// The charge lands on the candidate's resolved upstream model, the same key the
// success sites and the routing skip use. status is the upstream status the
// attempt had reached (a 200 whose body then failed is still a 200), 0 when
// none; it rides with the reason onto the circuit as its last verdict.
func (h *Handler) chargeBreaker(st *requestState, candidate modelCandidate, status int, reason string) {
	if !st.circuitBreakerEnabled {
		return
	}
	debuglog.Warn("proxy: recording circuit breaker failure", "reason", reason, "status", status, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
	st.logData.noteBreaker(breakerCharge)
	h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), failover.Cause{Status: status, Reason: reason})
}

// rejectUntranslatableBody is the single outcome all three egress adapters have
// for a success whose body they cannot turn into a completion.
//
// One place because the outcome has four parts (the log, the breaker charge,
// the request error and the failover), and three copies of it is how the charge
// comes to be missing from one.
//
// status is the 2xx the upstream answered before its body failed translation;
// logData has not been stamped with it at this point.
func (h *Handler) rejectUntranslatableBody(st *requestState, candidate modelCandidate, logData *requestLogData, adapter string, status int, err error, attempt int, r *http.Request) candidateOutcome {
	debuglog.Warn("proxy: upstream body translation failed", "adapter", adapter, "error", err, "model", logData.modelID, "provider", logData.providerName)
	// The translators read the body under the attempt's context, so an
	// interrupted request arrives here as a translation failure: a caller
	// hanging up or this gateway's own request_timeout, and neither is the
	// provider's doing.
	if _, aborted := cancelKind(r.Context(), err); !aborted && translationIsProviderFault(err) {
		h.chargeBreaker(st, candidate, status, "upstream body could not be translated")
	}
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)})
	logData.failoverAttempt = attempt
	logData.closeAttemptRecord(status, KindProviderError, errString(err), "", 0)
	return outcomeFailover
}

// translationIsProviderFault separates "these bytes are not the object this
// adapter expects" from "the provider answered, and its answer was a refusal".
//
// A Gemini prompt blocked by its safety filter returns 200 with an empty
// candidate list, which BuildChatCompletion cannot turn into a completion, but
// the body is a perfectly good Gemini object and the provider is plainly alive.
// Charging for it would take a healthy provider out of rotation for every tenant
// after five blocked prompts, which is exactly what a client retries.
//
// The speech adapter's refusals are the same two shapes: an answer without an
// audio part (a blocked prompt, a text reply) is the model answering, and a
// body past speechBodyCap is this gateway's own limit.
func translationIsProviderFault(err error) bool {
	return !errors.Is(err, gemini.ErrPromptBlocked) && !errors.Is(err, errEgressBodyOversized) &&
		!errors.Is(err, gemini.ErrSpeechNoAudio) && !errors.Is(err, errSpeechBodyOversized)
}

// answerCarriesSomething reports whether a completion carries anything at all
// from the provider. Its negation is requestLogData.emptyCompletion, the one
// shape a 200 is charged for.
//
// Deliberately NOT chatAnswerCarriesContent, which backs the retirement verdict
// and stays narrow: `refusal` is the likeliest field for an aggregator to write
// "this model is gone" into behind a 200, and letting the retirement bar count
// it would clear such a provider's gone-strikes forever.
//
// And deliberately not `len(Choices) == 0` either: every egress translator
// synthesises a one-element Choices literal on success, so an emptied Gemini,
// Anthropic-egress or Responses answer always has a choice and the charge could
// never fire for any of them.
//
// The bar is therefore: did any choice come back with SOMETHING in it. An
// allowlist for the unmodelled shapes rather than "any member that carries",
// because a relay stamping a bookkeeping field on the assistant message would
// otherwise make the breaker inert with no signal anywhere.
func answerCarriesSomething(out ChatCompletionResponse) bool {
	if out.Usage.CompletionTokens > 0 {
		return true
	}
	for _, choice := range out.Choices {
		if choiceCarriesSomething(choice) {
			return true
		}
	}
	return false
}

func choiceCarriesSomething(choice Choice) bool {
	// A stop reason the provider reported about its own output. A filtered
	// answer is the provider answering (Gemini's SAFETY maps here too), and a
	// length cut means there was output to cut.
	if choice.FinishReason != nil {
		switch *choice.FinishReason {
		case "content_filter", "length":
			return true
		}
	}
	msg := choice.Message
	if text, ok := msg.Content.(string); ok && text != "" {
		return true
	}
	if parts, ok := msg.Content.([]any); ok && len(parts) > 0 {
		return true
	}
	if msg.ReasoningContent != "" || msg.Reasoning != "" || len(msg.ReasoningDetails) > 0 {
		return true
	}
	if len(msg.ToolCalls) > 0 {
		return true
	}
	// The unmodelled shapes that ARE the answer: a safety refusal, the audio
	// object on a speech completion, a legacy function_call, OpenRouter's
	// generated `images`, Perplexity's `citations`, an Anthropic-shaped relay's
	// `thinking_blocks`. For the image and citation models those members are the
	// WHOLE answer, so a relay forwarding one without a usage block would have a
	// correct answer charged against it.
	for _, key := range []string{"refusal", "audio", "function_call", "annotations", "images", "citations", "thinking_blocks"} {
		if util.ValueCarries(msg.Extra[key]) {
			return true
		}
	}
	return false
}
