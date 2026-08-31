package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// A 429 is two different claims wearing one number, and they need opposite
// handling:
//
//   - SATURATED ("busy"): a concurrency slot, RPM or TPM ceiling is full and a
//     retry succeeds in seconds. Charging the circuit breaker for it benches a
//     healthy provider, which is how the 2026-08-31 hotel/glm53 run turned
//     nineteen avoidable 502s out of a provider that had free slots the whole
//     time.
//   - EXHAUSTED ("spent"): a usage window or a balance is gone and a retry
//     cannot succeed until it resets or someone pays. Spending a second request
//     to confirm it wastes the request and can tip a saturated sibling over.
//
// classifyRateLimit reads the response and says which claim it makes. Known
// provider phrases and headers are accelerators, never prerequisites: with the
// whole phrase table deleted the behavioural fallback in classify429Attempt (a
// recent 2xx on the same circuit means saturated) still converges on the right
// treatment, only more slowly. An unknown 429 keeps today's behaviour exactly.
type rateLimitClass int

const (
	// rateLimitUnknown is today's behaviour: failover-eligible, charged.
	rateLimitUnknown rateLimitClass = iota
	// rateLimitSaturated: slots/RPM/TPM busy, retry in seconds.
	rateLimitSaturated
	// rateLimitExhausted: window or balance spent, retry after reset.
	rateLimitExhausted
)

func (c rateLimitClass) String() string {
	switch c {
	case rateLimitSaturated:
		return "saturated"
	case rateLimitExhausted:
		return "exhausted"
	default:
		return "unknown"
	}
}

// rateLimitVerdict is what one 429 established, threaded from the classifier
// to the breaker outcome, the saturation retry and the terminal response.
type rateLimitVerdict struct {
	// classified says the master switch was ON and the classifier actually
	// looked, even if it found nothing (class stays unknown). It gates the
	// 429-open escalation: with rate_limit_classify_enabled off the whole
	// mechanism, escalation included, must restore today's behaviour bit for
	// bit.
	classified bool
	class      rateLimitClass
	// retryAfter is how long the provider asked us to wait: the parsed header
	// when present, else the class default (saturated: 2s; exhausted: 0, the
	// breaker's cooldown governs). Capped at rate_limit_saturation_max_wait for
	// saturated.
	retryAfter time.Duration
	// pinHint is the exhausted cooldown suggestion handed to the breaker: a
	// Retry-After beyond the saturation ceiling, or the matched phrase's
	// per-marker default. Zero means "no hint, use the ordinary cooldown". The
	// breaker clamps it with the same floor/ceiling/jitter rules as an advisor
	// pin, and an advisor reading always wins over it.
	pinHint time.Duration
	// entitled marks an exhaustion a person fixes (balance, plan) rather than
	// time; it keeps the provider_not_entitled error kind for those bodies.
	entitled bool
}

// errorKind maps the verdict onto the request-log classification. Unknown
// stays with whatever classifyUpstreamError said.
func (v rateLimitVerdict) errorKind() (ErrorKind, bool) {
	switch v.class {
	case rateLimitSaturated:
		return KindProviderSaturated, true
	case rateLimitExhausted:
		if v.entitled {
			return KindProviderNotEntitled, true
		}
		return KindProviderQuotaExhausted, true
	default:
		return "", false
	}
}

const (
	// defaultSaturationMaxWait is the Retry-After ceiling that separates "wait
	// for a slot" from "wait for a window": a provider asking for more than a
	// minute is telling us the window, not the slot. It also caps the
	// last-candidate saturation wait. Runtime override:
	// rate_limit_saturation_max_wait.
	defaultSaturationMaxWait = 60 * time.Second
	// defaultRecentSuccessWindow bounds the behavioural fallback: a 429 from a
	// circuit that served a 2xx this recently is saturated, because a spent
	// window does not come back in a minute. Runtime override:
	// rate_limit_recent_success_window.
	defaultRecentSuccessWindow = 60 * time.Second
	// defaultSaturatedRetryAfter is the wait used when a saturated 429 carries
	// no Retry-After. Two seconds is the shape of the gap the 2026-08-31 run
	// turned into 502s.
	defaultSaturatedRetryAfter = 2 * time.Second
	// pinHintWindow is the pin for a usage window whose reset the body names
	// but does not date (Ollama's session cap, generic quota phrases).
	pinHintWindow = 30 * time.Minute
	// pinHintWeekly is the pin for an explicitly weekly cap. Two hours, not a
	// week: the pin ceiling (circuit_breaker_quota_pin_max) and the probe on
	// expiry keep a wrong guess cheap, and the operator may top up sooner.
	pinHintWeekly = 2 * time.Hour
	// pinHintUntilPaid marks an exhaustion nothing but a payment resets. Far
	// above any configurable ceiling on purpose: the breaker clamps it to
	// circuit_breaker_quota_pin_max, so it means "pin as long as allowed".
	pinHintUntilPaid = 90 * 24 * time.Hour
)

// rateLimitPhrase is one observed provider sentence and the claim it makes.
// The table is data, not code: every entry names the provider it was observed
// on and when (a test enforces both), so an entry can be deleted with its
// provider and a phrase that stops matching has an owner to ask. Matching is a
// lowercased substring test on the already-sanitized body, the same discipline
// classifyUpstreamError uses; `also` narrows a phrase too generic on its own.
type rateLimitPhrase struct {
	phrase   string
	also     string // second required substring; "" = none
	class    rateLimitClass
	pinHint  time.Duration // exhausted only
	entitled bool          // exhausted only: fixed by a person, not by time
	provider string        // where the phrase was observed
	observed string        // when (YYYY-MM-DD)
}

// rateLimitPhrases is checked in order; first match wins. Exhausted entries
// come first because an exhaustion body can also contain saturation vocabulary
// ("rate limit"), and mistaking spent for busy retries into a certain 429.
//
// The entitled entries double as classifyUpstreamError's provider_not_entitled
// list (entitledRateLimitPhrases below): one table, two consumers, so a phrase
// cannot be added to one and not the other.
var rateLimitPhrases = []rateLimitPhrase{
	// Balance / plan: a person fixes these, so the pin holds as long as the
	// ceiling allows.
	{phrase: "insufficient balance", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "Z.ai Coding Plan (code 1113); MiniMax 1008 status_msg", observed: "2026-08-31"},
	{phrase: "no resource package", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "Z.ai Coding Plan (code 1113)", observed: "2026-08-31"},
	{phrase: "please recharge", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "Z.ai Coding Plan (code 1113)", observed: "2026-08-31"},
	{phrase: "insufficient_quota", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "exceeded your current quota", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "billing hard limit", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "credit balance is too low", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "Anthropic", observed: "2026-08-31"},
	// Usage windows: time fixes these. The weekly entry must precede the
	// session/generic ones so the longer pin is not shadowed.
	{phrase: "weekly usage limit", class: rateLimitExhausted, pinHint: pinHintWeekly, provider: "Ollama Cloud", observed: "2026-08-31"},
	{phrase: "session usage limit", class: rateLimitExhausted, pinHint: pinHintWindow, provider: "Ollama Cloud", observed: "2026-08-31"},
	{phrase: "usage limit", also: "upgrade", class: rateLimitExhausted, pinHint: pinHintWindow, provider: "Ollama Cloud (\"you have reached your session usage limit, upgrade for higher limits\")", observed: "2026-08-31"},
	{phrase: "overage_limit", class: rateLimitExhausted, pinHint: pinHintWindow, provider: "Neuralwatt (docs; not yet observed live)", observed: "2026-08-31"},
	// Saturation: capacity vocabulary, all observed on live providers.
	{phrase: "concurrent_budget_exceeded", class: rateLimitSaturated, provider: "Neuralwatt (2026-08-31 14:52 UTC)", observed: "2026-08-31"},
	{phrase: "rate_limit_error", also: "concurr", class: rateLimitSaturated, provider: "Neuralwatt", observed: "2026-08-31"},
	{phrase: "too many concurrent", class: rateLimitSaturated, provider: "Z.ai Coding Plan", observed: "2026-08-31"},
	{phrase: "concurrency limit", class: rateLimitSaturated, provider: "Kimi Code", observed: "2026-08-31"},
	{phrase: "rate limit exceeded", class: rateLimitSaturated, provider: "OpenRouter", observed: "2026-08-31"},
	{phrase: "requests per minute", class: rateLimitSaturated, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "tokens per minute", class: rateLimitSaturated, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "please try again in", class: rateLimitSaturated, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "overloaded", class: rateLimitSaturated, provider: "Anthropic", observed: "2026-08-31"},
	{phrase: "slow down", class: rateLimitSaturated, provider: "Ollama Cloud", observed: "2026-08-31"},
}

// The MiniMax base_resp codes the 200-envelope remap turns into a 429, split
// by who fixes them: 1008 (insufficient balance) needs a person to pay, 1039
// (the Token Plan's TPM budget spent) recovers with the window and gets the
// generic window pin. 1002 is a plain rate limit and is left to the
// saturation vocabulary. Regexes rather than substrings because the code is a
// JSON number whose spacing the provider owns.
var (
	miniMaxBalanceCode = regexp.MustCompile(`"status_code"\s*:\s*"?1008\b`)
	miniMaxWindowCode  = regexp.MustCompile(`"status_code"\s*:\s*"?1039\b`)
)

// entitledRateLimitPhrases is the entitled slice of the table, consumed by
// classifyUpstreamError for provider_not_entitled. Derived, never written out
// twice; TestRateLimitPhrases_EntitledSubsetOfExhausted pins the relationship.
func entitledRateLimitPhrases() []string {
	var out []string
	for _, p := range rateLimitPhrases {
		if p.entitled {
			out = append(out, p.phrase)
		}
	}
	return out
}

// classifyRateLimit reads the status, headers and sanitized body of a 429 (or
// a MiniMax remap to 429) and says which claim it makes. maxWait is the
// Retry-After ceiling separating saturation from exhaustion, and the cap on
// the saturated retryAfter.
//
// Decision order, first match wins: exhausted by body, saturated by body,
// saturated-or-exhausted by header, unknown. The caller owns the behavioural
// fallback for unknown (classify429Attempt); this function is pure so the
// table tests need no handler.
func classifyRateLimit(status int, hdr http.Header, body string, maxWait time.Duration) rateLimitVerdict {
	if status != http.StatusTooManyRequests {
		return rateLimitVerdict{}
	}
	if maxWait <= 0 {
		maxWait = defaultSaturationMaxWait
	}
	b := strings.ToLower(body)

	for _, p := range rateLimitPhrases {
		if !strings.Contains(b, p.phrase) || (p.also != "" && !strings.Contains(b, p.also)) {
			continue
		}
		switch p.class {
		case rateLimitExhausted:
			return exhaustedVerdict(hdr, maxWait, p.pinHint, p.entitled)
		case rateLimitSaturated:
			return saturatedVerdict(hdr, maxWait)
		case rateLimitUnknown:
			// Not a class the table carries; listed so the switch is exhaustive.
		}
	}
	if miniMaxBalanceCode.MatchString(b) {
		return exhaustedVerdict(hdr, maxWait, pinHintUntilPaid, true)
	}
	if miniMaxWindowCode.MatchString(b) {
		return exhaustedVerdict(hdr, maxWait, pinHintWindow, false)
	}

	// No phrase matched: let the headers speak. A Retry-After at or under the
	// ceiling is a slot freeing; above it the provider is naming the window.
	if wait, ok := rateLimitResetHint(hdr); ok {
		if wait <= maxWait {
			return rateLimitVerdict{class: rateLimitSaturated, retryAfter: wait}
		}
		return rateLimitVerdict{class: rateLimitExhausted, pinHint: wait}
	}
	return rateLimitVerdict{}
}

// exhaustedVerdict builds the exhausted verdict for a matched phrase: a
// Retry-After beyond the saturation ceiling overrides the phrase's own pin
// hint, because the provider dating its window beats our per-marker default.
func exhaustedVerdict(hdr http.Header, maxWait, phraseHint time.Duration, entitled bool) rateLimitVerdict {
	v := rateLimitVerdict{class: rateLimitExhausted, pinHint: phraseHint, entitled: entitled}
	if wait, ok := rateLimitResetHint(hdr); ok && wait > maxWait {
		v.pinHint = wait
	}
	return v
}

// saturatedVerdict builds the saturated verdict: the provider's Retry-After
// capped at the ceiling, else the class default.
func saturatedVerdict(hdr http.Header, maxWait time.Duration) rateLimitVerdict {
	v := rateLimitVerdict{class: rateLimitSaturated, retryAfter: defaultSaturatedRetryAfter}
	if wait, ok := rateLimitResetHint(hdr); ok {
		v.retryAfter = min(wait, maxWait)
	}
	return v
}

// rateLimitResetHint reads how long the provider asked us to wait: Retry-After
// when present, else the largest of the OpenAI-style X-RateLimit-Reset*
// headers (the binding budget is the one that resets last). ok is false when
// no header parses to a positive duration.
func rateLimitResetHint(hdr http.Header) (time.Duration, bool) {
	if d, ok := parseRetryAfter(hdr.Get("Retry-After")); ok {
		return d, true
	}
	var (
		longest time.Duration
		found   bool
	)
	for _, key := range []string{"X-RateLimit-Reset-Requests", "X-RateLimit-Reset-Tokens", "X-RateLimit-Reset"} {
		if d, ok := parseResetValue(hdr.Get(key)); ok && d > longest {
			longest, found = d, true
		}
	}
	return longest, found
}

// parseRetryAfter reads the RFC 9110 forms: delta-seconds or an HTTP date.
func parseRetryAfter(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs <= 0 {
			return 0, false
		}
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
	}
	return 0, false
}

// parseResetValue reads the shapes X-RateLimit-Reset* arrives in: a Go-style
// duration ("6m0s", OpenAI), a bare number of seconds, or a unix epoch in
// seconds or milliseconds (heuristic on magnitude; a wait is never years).
func parseResetValue(v string) (time.Duration, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, false
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		switch {
		case n <= 0:
			return 0, false
		case n < 1e9: // a delta in seconds
			return time.Duration(n) * time.Second, true
		case n < 1e12: // unix epoch, seconds
			d := time.Until(time.Unix(n, 0))
			return d, d > 0
		default: // unix epoch, milliseconds
			d := time.Until(time.UnixMilli(n))
			return d, d > 0
		}
	}
	if d, err := time.ParseDuration(v); err == nil && d > 0 {
		return d, true
	}
	return 0, false
}

// rebufferedBody hands back the bytes the classifier already read, then the
// rest of the upstream stream, closing the real body. The same restore trick
// the MiniMax envelope check uses, for the same reason: the downstream drains
// and forwards must see the response exactly as it arrived.
type rebufferedBody struct {
	io.Reader
	io.Closer
}

// classify429Attempt classifies one attempt's 429 and stamps the verdict on
// st.rateLimit for the breaker outcome, the saturation retry and the terminal
// response. It reads (and restores) at most failoverErrorClassifyCap of the
// body — a 429 body is never forwarded verbatim (forwardableErrorStatus denies
// the status), so the peek changes nothing downstream.
//
// The behavioural fallback lives here, not in classifyRateLimit: an unknown
// 429 from a (provider, model) circuit that served a 2xx inside
// rate_limit_recent_success_window is saturated — it was fine a moment ago and
// the load rose, and a spent window does not come back in a minute. This is
// the rule that keeps the whole mechanism working with the phrase table
// deleted; the phrases only make the verdict earlier. With no recent success
// the verdict stays unknown and the 429 is charged exactly as today.
//
// rate_limit_classify_enabled OFF restores today's behaviour bit for bit: no
// body peek, no verdict, every consumer sees rateLimitUnknown.
func (h *Handler) classify429Attempt(ctx context.Context, st *requestState, candidate modelCandidate, resp *http.Response) rateLimitVerdict {
	st.rateLimit = rateLimitVerdict{}
	if resp.StatusCode != http.StatusTooManyRequests {
		return st.rateLimit
	}
	if !h.settingsRepo.GetBool(ctx, "rate_limit_classify_enabled", true) {
		return st.rateLimit
	}
	maxWait := h.settingsRepo.GetDuration(ctx, "rate_limit_saturation_max_wait", defaultSaturationMaxWait)

	head, _ := io.ReadAll(io.LimitReader(resp.Body, failoverErrorClassifyCap))
	rest := resp.Body
	resp.Body = rebufferedBody{Reader: io.MultiReader(bytes.NewReader(head), rest), Closer: rest}

	body := util.SanitizeLogBody(string(head), 10000)
	v := classifyRateLimit(resp.StatusCode, resp.Header, body, maxWait)
	if v.class == rateLimitUnknown && st.circuitBreakerEnabled {
		window := h.settingsRepo.GetDuration(ctx, "rate_limit_recent_success_window", defaultRecentSuccessWindow)
		if h.circuitBreaker.LastSuccessWithin(candidate.provider.ID, candidateModelID(candidate), window) {
			v = rateLimitVerdict{class: rateLimitSaturated, retryAfter: defaultSaturatedRetryAfter}
		}
	}
	v.classified = true
	st.rateLimit = v
	return v
}

// failNoAvailableProvider answers a request whose failover group resolved to
// zero candidates. When the circuit breaker did the emptying, "502 bad
// gateway" is not true — nothing upstream faulted on THIS request — so the
// answer is a 429 with a Retry-After naming the earliest instant a circuit
// comes back, capped at a minute so a long quota pin never tells a client to
// go away for hours (a lapsed verdict frees the group far sooner than the
// last circuit expires). The kind is exhaustion only when every skipped
// candidate is waiting out a quota pin; a genuine upstream fault (no breaker
// skips at all: everything disabled or missing) keeps today's 502, and so
// does switching failover_exhaustion_status_429 off.
func (h *Handler) failNoAvailableProvider(w http.ResponseWriter, r *http.Request, st *requestState, displayModel string, timings resolveTimings, cacheHits resolveCacheHits, skips breakerSkipSummary) {
	msg := "no available provider for hotel/" + displayModel
	if skips.skips == 0 || !h.settingsRepo.GetBool(r.Context(), "failover_exhaustion_status_429", true) {
		h.failRequest(st.logData, http.StatusBadGateway, KindProviderError, msg, 0, st.startTime, st.parseMs, timings, cacheHits, 0)
		writeOpenAIError(w, msg, http.StatusBadGateway)
		return
	}
	retryIn := max(time.Until(skips.earliestRetry), time.Second)
	if retryIn > defaultSaturationMaxWait {
		retryIn = defaultSaturationMaxWait
	}
	secs := retryAfterSeconds(retryIn)
	kind := KindProviderSaturated
	if skips.allPinned {
		kind = KindProviderQuotaExhausted
	}
	msg = fmt.Sprintf("%s; earliest retry in %ds", msg, secs)
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	h.failRequest(st.logData, http.StatusTooManyRequests, kind, msg, 0, st.startTime, st.parseMs, timings, cacheHits, 0)
	writeOpenAIError(w, msg, http.StatusTooManyRequests)
}

// rateLimitTerminalKind refines a terminal 429's classification with the
// attempt's own verdict: the classes carry what the body-driven classifier
// cannot see (headers, the behavioural fallback) and honour the master switch
// (no verdict is stamped when it is off, so the old labels stand bit for
// bit). It refines only the generic kind: a body-derived provider_not_entitled
// names WHO fixes it and must not be blurred back into "quota".
func rateLimitTerminalKind(kind ErrorKind, reason string, status int, v rateLimitVerdict) (ErrorKind, string) {
	if status != http.StatusTooManyRequests || kind != KindProviderError {
		return kind, reason
	}
	if rlKind, ok := v.errorKind(); ok {
		return rlKind, rateLimitClientReason(v.class)
	}
	return kind, reason
}

// rateLimitClientReason words a classified 429 for the client the way
// classifyUpstreamError words its kinds: gateway-authored static text, never
// the provider's body.
func rateLimitClientReason(class rateLimitClass) string {
	if class == rateLimitExhausted {
		return "the provider's usage quota is exhausted; retry after it resets"
	}
	return "the provider is at capacity; retry shortly"
}

// rateLimitReqErr is the structured attempt error for a classified 429: it
// keeps the terminal renderers able to tell "busy" from "spent" from "failed".
func rateLimitReqErr(v rateLimitVerdict, attempt int, providerName string) reqError {
	kind, _ := v.errorKind()
	detail := "HTTP 429 (" + v.class.String() + ")"
	return reqError{Kind: kind, Attempt: attempt, Provider: providerName, Detail: detail}
}

// failoverReqErr is the attempt error recorded when an eligible upstream error
// sends the loop to the next candidate: the classified-429 shape when a
// verdict exists, today's generic one otherwise.
func failoverReqErr(rl rateLimitVerdict, attempt int, providerName string, status int) reqError {
	if status == http.StatusTooManyRequests && rl.class != rateLimitUnknown {
		return rateLimitReqErr(rl, attempt, providerName)
	}
	return reqError{Kind: KindProviderError, Attempt: attempt, Provider: providerName, Detail: fmt.Sprintf("HTTP %d", status)}
}

// judge429AndRecordBreaker is the one call an attempt path makes between
// reading the status and acting on it: classify a 429 (eligible ones only —
// with failover_on_rate_limit OFF a 429 keeps today's stay-and-forward
// handling untouched) and record the breaker outcome with the verdict. One
// entry point for the sequential, hedged and pass-through paths, so the three
// cannot drift.
func (h *Handler) judge429AndRecordBreaker(ctx context.Context, st *requestState, candidate modelCandidate, resp *http.Response, isFailoverEligible bool) rateLimitVerdict {
	var rl rateLimitVerdict
	if isFailoverEligible {
		rl = h.classify429Attempt(ctx, st, candidate, resp)
	}
	h.recordBreakerOutcome(ctx, st, candidate, resp.StatusCode, isFailoverEligible, rl)
	return rl
}

// deferSaturatedRetry closes out an attempt whose LAST candidate answered a
// saturated 429: the response is drained, the attempt recorded, and the loop
// told to wait and try the same candidate once more (outcomeRetrySaturated).
// The point is to absorb the two-second gaps that became 502s on 2026-08-31,
// not to build a queue — st.saturationRetried caps it at one.
func (h *Handler) deferSaturatedRetry(st *requestState, candidate modelCandidate, resp *http.Response, attempt int) candidateOutcome {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	st.saturationRetried = true
	st.setReqErr(rateLimitReqErr(st.rateLimit, attempt, candidate.provider.Name))
	st.logData.failoverAttempt = attempt
	debuglog.Info("proxy: last candidate saturated, waiting to retry it once", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "retry_after", st.rateLimit.retryAfter, "attempt", attempt+1)
	return outcomeRetrySaturated
}
