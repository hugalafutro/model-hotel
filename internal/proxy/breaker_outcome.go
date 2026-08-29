package proxy

import (
	"errors"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/util"
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
// except for a SUCCESS status (any 2xx) — on either path.
//
// A 2xx is a status, not an answer, and these headers arrive before a byte of
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
		case breakerActionDeferred:
			// The status does not decide; recordClassifiedOutcome does, once the
			// body has been read. Every path that reaches here goes on to
			// classify that body — the drain when there is another candidate,
			// forwardUpstreamError when there is not, and the hedge race's own
			// drain — so the verdict is never simply dropped.
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
	// Not a 2xx. A success of ANY 2xx defers its verdict to whoever reads the
	// body — recordAnswerOutcome or judgeStreamForBreaker — for the reason the
	// comment above gives: RecordSuccess resets consecutiveFails, so crediting
	// here at header time erases the charge the answer verdict is about to make
	// and the circuit can never open above a threshold of one.
	if !servedSuccessStatus(statusCode) {
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
	}
}

// recordClassifiedOutcome finishes a verdict recordBreakerOutcome deferred,
// once the upstream body has been classified.
//
// It charges unless the kind is a refusal ABOUT THE MODEL rather than about the
// provider. Those say nothing about whether the provider can serve anything
// else, and charging the provider-wide breaker for one takes its healthy models
// down with the refused one. That is the same argument breakerRecordAction
// already makes for a 404, whose comment reads "model-specific, not provider
// health"; this extends it to the refusal that arrives as a 429.
//
// Deliberately not a RecordSuccess: a refusal is not evidence of health either,
// and crediting one would reset consecutiveFails and erase real failures the
// provider had accrued — the hole #805 closed.
func (h *Handler) recordClassifiedOutcome(st *requestState, candidate modelCandidate, statusCode int, kind ErrorKind, body string) {
	if !st.circuitBreakerEnabled || breakerRecordAction(statusCode) != breakerActionDeferred {
		return
	}
	if refusalIsAboutTheModel(kind, body) {
		return
	}
	h.chargeBreaker(st, candidate, "upstream status")
}

// refusalIsAboutTheModel reports whether a classified upstream refusal concerns
// the requested model rather than the provider's health.
//
// An entitlement refusal is only half of one. "Your account has no credit" is
// provider-wide and its circuit should open; "your plan does not include this
// model" is not, and opening the circuit for it takes the models the plan DOES
// include down with it. Both arrive as KindProviderNotEntitled, so the kind
// alone cannot separate them.
//
// A resource package is the per-model unit a plan is sold in, so naming one is
// positive evidence that the refusal is about which model was asked for. That is
// what separates the two in practice: Z.ai answers "Insufficient balance or no
// resource package. Please recharge." for a model outside the coding plan, while
// MiniMax's genuinely account-wide 1008 says only "insufficient balance". The
// test is deliberately for the presence of the per-model concept rather than the
// absence of the account-wide one, because Z.ai's sentence contains both.
//
// Requiring positive evidence is also the safe direction: an unrecognised
// entitlement refusal keeps charging the breaker, as it always did.
func refusalIsAboutTheModel(kind ErrorKind, body string) bool {
	if kind == KindProviderModelGone {
		return true
	}
	return kind == KindProviderNotEntitled && strings.Contains(strings.ToLower(body), "resource package")
}

// recordAnswerOutcome records the circuit-breaker verdict for a finished
// non-streaming attempt, and is the completion-side sibling of
// judgeStreamForBreaker.
//
// The bar is emptyCompletion — did ANYTHING come back — and deliberately not the
// retirement verdict's bar two lines from each call site. See
// answerCarriesSomething for why the two questions differ and why the answer to
// this one cannot be `len(Choices) == 0`.
//
// A state other than "completed" means the attempt failed after the headers: a
// body read that died, a body the gateway could not decode, an upstream that
// sent a success shape behind a failure status. The old code had already
// credited every one of those before the read was even attempted.
func (h *Handler) recordAnswerOutcome(st *requestState, candidate modelCandidate, logData *requestLogData, r *http.Request) {
	if !st.circuitBreakerEnabled {
		return
	}
	// Read here rather than at the call sites: two copies of the same expression
	// is how one of them comes to be missing it, which is what happened to the
	// third charge site this change added.
	if logData.state != "completed" {
		// The attempt failed after the headers.
		//
		// The KIND decides, and nothing else. Every way an attempt can fail after
		// the headers already has one: a caller hanging up, this gateway's own
		// request_timeout, a body it could not decode, a body that died on the
		// wire. providerAtFault excludes all but the last, which is the same
		// predicate judgeStreamForBreaker uses.
		//
		// A separate client-gone guard used to sit here as well. It read the
		// CLIENT's context, so it was blind to a failover-timeout cancel, and it
		// was a second rule that had to agree with the first — the shape three
		// review rounds kept finding a hole in.
		if !providerAtFault(logData.errorKind) {
			return
		}
		h.chargeBreaker(st, candidate, "response failed after headers")
		return
	}
	// A completed answer is credited even if the caller has since gone: the body
	// was read, the provider answered, and withholding the credit leaves older
	// failures on the clock to open a healthy circuit later.
	if logData.emptyCompletion {
		h.chargeBreaker(st, candidate, "response completed without delivering content")
		return
	}
	h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
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
// for a success whose body they cannot turn into a completion.
//
// One place because the outcome has four parts — the log, the breaker charge,
// the request error and the failover — and three copies of it is how the charge
// came to be missing from all three: a 200 was credited at header time, before
// any translation was attempted, and each adapter's own comment already called
// this a provider fault. Mutation testing then showed two of the three copies
// had no test behind them.
func (h *Handler) rejectUntranslatableBody(st *requestState, candidate modelCandidate, logData *requestLogData, adapter string, err error, attempt int, r *http.Request) candidateOutcome {
	debuglog.Warn("proxy: upstream body translation failed", "adapter", adapter, "error", err, "model", logData.modelID, "provider", logData.providerName)
	// Both translators read the body with an unbounded ReadAll under the
	// attempt's context, so an interrupted request arrives here as a translation
	// failure — a caller hanging up or this gateway's own request_timeout, and
	// neither is the provider's doing.
	if _, aborted := cancelKind(r.Context(), err); !aborted && translationIsProviderFault(err) {
		h.chargeBreaker(st, candidate, "upstream body could not be translated")
	}
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)})
	logData.failoverAttempt = attempt
	return outcomeFailover
}

// translationIsProviderFault separates "these bytes are not the object this
// adapter expects" from "the provider answered, and its answer was a refusal".
//
// A Gemini prompt blocked by its safety filter returns 200 with an empty
// candidate list, which BuildChatCompletion cannot turn into a completion — but
// the body is a perfectly good Gemini object and the provider is plainly alive.
// Charging for it took a healthy provider out of rotation for every tenant after
// five blocked prompts, which is exactly what a client retries.
func translationIsProviderFault(err error) bool {
	return !errors.Is(err, gemini.ErrPromptBlocked)
}

// answerCarriesSomething reports whether a completion carries anything at all
// from the provider. Its negation is requestLogData.emptyCompletion, the one
// shape a 200 is charged for.
//
// Deliberately NOT chatAnswerCarriesContent, which backs the retirement verdict
// and stays narrow: `refusal` is the likeliest field for an aggregator to write
// "this model is gone" into behind a 200, and letting the retirement bar count
// it would clear such a provider's gone-strikes forever. That bar gained exactly
// one thing on this branch — it reads ReasoningDetails, which it had been
// judging BEFORE the normalisation that folds them into ReasoningContent, so a
// reasoning-only answer read as nothing at all.
//
// And deliberately not `len(Choices) == 0` either, which is what this was first
// narrowed to. Every egress translator synthesises a one-element Choices literal
// on success, so an emptied Gemini, Anthropic-egress or Responses answer always
// had a choice and the charge could never fire for any of them — the fix was a
// no-op for three whole dialects.
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
	// answer is the provider answering — Gemini's SAFETY maps here too — and a
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
	// WHOLE answer, so a relay forwarding one without a usage block would have
	// had a correct answer charged against it.
	//
	// An allowlist rather than "any member that carries", because a relay
	// stamping a bookkeeping field on the assistant message would otherwise make
	// the breaker inert with nothing to show for it.
	for _, key := range []string{"refusal", "audio", "function_call", "annotations", "images", "citations", "thinking_blocks"} {
		if util.ValueCarries(msg.Extra[key]) {
			return true
		}
	}
	return false
}
