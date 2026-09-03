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
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// rateLimitClass says which of two claims a 429 makes:
//
//   - SATURATED ("busy"): a concurrency slot, RPM or TPM ceiling is full and a
//     retry succeeds in seconds. Charging the circuit breaker for it benches a
//     healthy provider.
//   - EXHAUSTED ("spent"): a usage window or a balance is gone and a retry
//     cannot succeed until it resets or someone pays. Spending a second request
//     to confirm it wastes the request and can tip a saturated sibling over.
//
// classifyRateLimit reads the response and says which claim it makes. Provider
// phrases and headers are accelerators, never prerequisites: the behavioural
// fallback in classify429Attempt (a recent 2xx on the same circuit means
// saturated) reaches the same verdict without them, only more slowly.
type rateLimitClass int

const (
	// rateLimitUnknown is failover-eligible and charged to the breaker.
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
	// classified says the classifier ran, even if it found nothing (class stays
	// unknown). It gates the 429-open escalation, which is off whenever
	// rate_limit_classify_enabled is off.
	classified bool
	class      rateLimitClass
	// phrase is the phrase-table entry that decided the class, "" when the
	// headers or the behavioural fallback did. It rides into the attempt trail
	// for the phrase staleness report.
	phrase string
	// detail is the sanitized body the classifier read, kept for the attempt
	// trail (which caps and masks it); never forwarded anywhere else.
	detail string
	// retryAfter is how long the provider asked us to wait: the parsed header
	// when present, else the class default (saturated: 2s; exhausted: 0, the
	// breaker's cooldown governs). Capped at rate_limit_saturation_max_wait for
	// saturated.
	retryAfter time.Duration
	// pinHint is the exhausted cooldown suggestion handed to the breaker: a
	// Retry-After beyond the saturation ceiling, or the matched phrase's
	// per-marker default. Zero means "no hint, use the ordinary cooldown". The
	// breaker clamps it with the same floor/ceiling/jitter rules as an advisor
	// pin, and an advisor reading wins over it.
	pinHint time.Duration
	// entitled marks an exhaustion a person fixes (balance, plan) rather than
	// time; it keeps the provider_not_entitled error kind for those bodies.
	entitled bool
	// account marks an entitled refusal that is about the account behind the
	// provider, not the model asked for: a balance spent, a plan with no
	// credit. Such a refusal darkens the whole provider at once. A phrase the
	// table marks perModel (a plan that excludes one model) is entitled but
	// not account-wide, since the plan's other models still serve.
	account bool
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
	// minute is naming the window, not the slot. It also caps the
	// last-candidate saturation wait. Runtime override:
	// rate_limit_saturation_max_wait.
	defaultSaturationMaxWait = 60 * time.Second
	// defaultRecentSuccessWindow bounds the behavioural fallback: a 429 from a
	// circuit that served a 2xx this recently is saturated, because a spent
	// window does not come back in a minute. Runtime override:
	// rate_limit_recent_success_window.
	defaultRecentSuccessWindow = 60 * time.Second
	// defaultSaturatedRetryAfter is the wait used when a saturated 429 carries
	// no Retry-After. Two seconds is the typical slot-freeing gap.
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
// Every entry names the provider it was observed on and the date, so an entry
// can be deleted with its provider and a phrase that stops matching has an
// owner to ask. Matching is a lowercased substring test on the already
// sanitized body; `also` narrows a phrase too generic on its own.
type rateLimitPhrase struct {
	phrase   string
	also     string // second required substring; "" = none
	class    rateLimitClass
	pinHint  time.Duration // exhausted only
	entitled bool          // exhausted only: fixed by a person, not by time
	perModel bool          // entitled only: the plan excludes THIS model, not the account
	provider string        // where the phrase was observed
	observed string        // when (YYYY-MM-DD)
}

// rateLimitPhrases is checked in order; first match wins. Exhausted entries
// must precede saturated ones: an exhaustion body can also contain saturation
// vocabulary ("rate limit"), and mistaking spent for busy retries into a
// certain 429.
//
// The entitled entries double as classifyUpstreamError's provider_not_entitled
// list (entitledRateLimitPhrases), so a phrase cannot be added to one and not
// the other.
var rateLimitPhrases = []rateLimitPhrase{
	// Balance / plan: a person fixes these, so the pin holds as long as the
	// ceiling allows.
	// Ahead of "insufficient balance", which Z.ai puts in the same sentence
	// ("Insufficient balance or no resource package"): the resource package
	// is the per-model plan unit, so that body is read as being about the
	// model asked for and the plan's other models still serve; a balance
	// spent for real still reaches the provider through the span rule.
	{phrase: "no resource package", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, perModel: true, provider: "Z.ai Coding Plan (code 1113)", observed: "2026-08-31"},
	{phrase: "insufficient balance", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "Z.ai Coding Plan (code 1113); MiniMax 1008 status_msg", observed: "2026-08-31"},
	{phrase: "please recharge", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "Z.ai Coding Plan (code 1113)", observed: "2026-08-31"},
	{phrase: "insufficient_quota", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "exceeded your current quota", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "billing hard limit", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "OpenAI", observed: "2026-08-31"},
	{phrase: "credit balance is too low", class: rateLimitExhausted, pinHint: pinHintUntilPaid, entitled: true, provider: "Anthropic", observed: "2026-08-31"},
	// Usage windows: time fixes these. The weekly entries must precede the
	// session/generic ones so the longer pin is not shadowed.
	//
	// "limit exhausted" swallows "concurrency limit exhausted" and runs ahead
	// of every saturated entry, so it also requires the body to name a reset: a spent
	// window says when it comes back, a busy provider says "retry shortly", and
	// "concurrency limit exhausted" must keep reaching the saturated entries
	// below. The Z.ai entry pins pinHintWeekly rather than the named reset
	// instant: nothing here parses that date format.
	{phrase: "limit exhausted", also: "reset", class: rateLimitExhausted, pinHint: pinHintWeekly, provider: "Z.ai Coding Plan (code 1310, \"Weekly/Monthly Limit Exhausted. Your limit will reset at ...\")", observed: "2026-09-01"},
	{phrase: "weekly usage limit", class: rateLimitExhausted, pinHint: pinHintWeekly, provider: "Ollama Cloud", observed: "2026-08-31"},
	{phrase: "session usage limit", class: rateLimitExhausted, pinHint: pinHintWindow, provider: "Ollama Cloud", observed: "2026-08-31"},
	{phrase: "usage limit", also: "upgrade", class: rateLimitExhausted, pinHint: pinHintWindow, provider: "Ollama Cloud (\"you have reached your session usage limit, upgrade for higher limits\")", observed: "2026-08-31"},
	{phrase: "overage_limit", class: rateLimitExhausted, pinHint: pinHintWindow, provider: "Neuralwatt (docs; not yet observed live)", observed: "2026-08-31"},
	// Saturation: capacity vocabulary.
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

// The phrase names the two MiniMax code matches report in the attempt trail,
// so the staleness report counts them beside the table's entries.
const (
	miniMaxBalancePhrase = "minimax status_code 1008"
	miniMaxWindowPhrase  = "minimax status_code 1039"
)

// entitledRateLimitPhrases is the entitled slice of the phrase table, consumed
// by classifyUpstreamError for provider_not_entitled.
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
// saturated-or-exhausted by header, unknown. The behavioural fallback for
// unknown lives in classify429Attempt.
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
		var v rateLimitVerdict
		switch p.class {
		case rateLimitExhausted:
			v = exhaustedVerdict(hdr, b, maxWait, p.pinHint, p.entitled)
		case rateLimitSaturated:
			v = saturatedVerdict(hdr, b, maxWait)
		case rateLimitUnknown:
			// Not a class the table carries; listed so the switch is exhaustive.
			continue
		}
		v.phrase = p.phrase
		v.account = v.class == rateLimitExhausted && v.entitled && !p.perModel
		return v
	}
	if miniMaxBalanceCode.MatchString(b) {
		v := exhaustedVerdict(hdr, b, maxWait, pinHintUntilPaid, true)
		v.phrase = miniMaxBalancePhrase
		v.account = v.entitled
		return v
	}
	if miniMaxWindowCode.MatchString(b) {
		v := exhaustedVerdict(hdr, b, maxWait, pinHintWindow, false)
		v.phrase = miniMaxWindowPhrase
		return v
	}

	// No phrase matched: fall back to the provider's own wait, from the headers
	// or the body. A wait at or under the ceiling is a slot freeing; above it
	// the provider is naming the window.
	if wait, ok := providerResetHint(hdr, b); ok {
		if wait <= maxWait {
			return rateLimitVerdict{class: rateLimitSaturated, retryAfter: wait}
		}
		return rateLimitVerdict{class: rateLimitExhausted, pinHint: wait}
	}
	return rateLimitVerdict{}
}

// exhaustedVerdict builds the exhausted verdict for a matched phrase. A wait
// the provider states outranks the phrase's default pin. A Retry-After beyond
// the saturation ceiling replaces the pin and leaves entitled alone, since a
// proxy in front of the provider may stamp the header on a refusal a person
// still has to fix. Otherwise a wait written into the body decides, whatever
// the headers say: above the ceiling it replaces the pin and clears entitled,
// because a body that names a retry instant is describing a window that time
// reopens; at or under the ceiling it is saturation, but only when it comes
// from a structured retry detail, since an aggregator's "try again in 30
// seconds" boilerplate can wrap an out-of-credit refusal that no retry fixes.
func exhaustedVerdict(hdr http.Header, body string, maxWait, phraseHint time.Duration, entitled bool) rateLimitVerdict {
	v := rateLimitVerdict{class: rateLimitExhausted, pinHint: phraseHint, entitled: entitled}
	if wait, ok := rateLimitResetHint(hdr); ok && wait > maxWait {
		v.pinHint = wait
		return v
	}
	wait, structured, ok := bodyResetHint(body)
	switch {
	case !ok:
	case wait > maxWait:
		v.pinHint = wait
		v.entitled = false
	case structured:
		return rateLimitVerdict{class: rateLimitSaturated, retryAfter: wait}
	}
	return v
}

// saturatedVerdict builds the saturated verdict: the provider's stated wait
// capped at the ceiling, else the class default.
func saturatedVerdict(hdr http.Header, body string, maxWait time.Duration) rateLimitVerdict {
	v := rateLimitVerdict{class: rateLimitSaturated, retryAfter: defaultSaturatedRetryAfter}
	if wait, ok := providerResetHint(hdr, body); ok {
		v.retryAfter = min(wait, maxWait)
	}
	return v
}

// providerResetHint is how long the provider asked us to wait: the headers
// first, then the body. body is the sanitized, lowercased text the classifier
// already reads.
func providerResetHint(hdr http.Header, body string) (time.Duration, bool) {
	if d, ok := rateLimitResetHint(hdr); ok {
		return d, true
	}
	d, _, ok := bodyResetHint(body)
	return d, ok
}

// Google states a 429's wait twice in the body and never in a header: a
// google.rpc.RetryInfo detail ("retryDelay": "9303s", protobuf's JSON
// duration) and the sentence "Please retry in 2h35m3.27s". Other providers
// write "try again in 30 seconds". The body is lowercased by the time it is
// read. ms is listed before m: the alternation is leftmost-first, and "20ms"
// read as twenty minutes would pin a model for a wait of milliseconds.
var (
	bodyRetryDelayRe  = regexp.MustCompile(`"retrydelay"\s*:\s*"(\d+(?:\.\d+)?)s"`)
	bodyRetryGoDurRe  = regexp.MustCompile(`(?:retry|try again)\s+(?:in|after)\s+((?:\d+(?:\.\d+)?(?:ms|h|m|s))+)(?:[^a-z]|$)`)
	bodyRetryWordsRe  = regexp.MustCompile(`(?:retry|try again)\s+(?:in|after)\s+(\d+(?:\.\d+)?)\s+([a-z]+)`)
	bodyUnitDurations = map[string]time.Duration{"second": time.Second, "seconds": time.Second, "sec": time.Second, "secs": time.Second, "minute": time.Minute, "minutes": time.Minute, "min": time.Minute, "mins": time.Minute, "hour": time.Hour, "hours": time.Hour, "day": 24 * time.Hour, "days": 24 * time.Hour}
)

// bodyResetHintMax bounds a wait read out of a body. A quota window is hours
// or days; anything longer is a number that overflowed or a sentence that was
// not a wait, and neither should pin a model.
const bodyResetHintMax = 30 * 24 * time.Hour

// bodyResetHint reads a wait the provider wrote into a 429 body: the
// structured retry detail first, then the prose. structured reports which
// one answered, since only the detail is trusted to shorten a refusal. A
// statement that fails the plausibility bound does not hide the next one:
// the two Google statements sit in one body, and a zeroed or overflowed
// detail must not discard the sentence beside it. ok is false when nothing
// parses to a positive, plausible duration.
func bodyResetHint(body string) (wait time.Duration, structured, ok bool) {
	if m := bodyRetryDelayRe.FindStringSubmatch(body); m != nil {
		if secs, err := strconv.ParseFloat(m[1], 64); err == nil {
			if d, ok := plausibleWait(time.Duration(secs * float64(time.Second))); ok {
				return d, true, true
			}
		}
	}
	if m := bodyRetryGoDurRe.FindStringSubmatch(body); m != nil {
		if d, err := time.ParseDuration(m[1]); err == nil {
			if d, ok := plausibleWait(d); ok {
				return d, false, true
			}
		}
	}
	if m := bodyRetryWordsRe.FindStringSubmatch(body); m != nil {
		n, err := strconv.ParseFloat(m[1], 64)
		if per, known := bodyUnitDurations[m[2]]; err == nil && known {
			if d, ok := plausibleWait(time.Duration(n * float64(per))); ok {
				return d, false, true
			}
		}
	}
	return 0, false, false
}

// plausibleWait accepts a parsed wait that is positive and inside the bound;
// a float that overflowed into a negative duration fails the first test.
func plausibleWait(d time.Duration) (time.Duration, bool) {
	if d <= 0 || d > bodyResetHintMax {
		return 0, false
	}
	return d, true
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
// rest of the upstream stream, closing the real body, so the downstream drains
// and forwards see the response exactly as it arrived.
type rebufferedBody struct {
	io.Reader
	io.Closer
}

// classify429Attempt classifies one attempt's 429 and stamps the verdict on
// st.rateLimit for the breaker outcome, the saturation retry and the terminal
// response. It reads (and restores) at most failoverErrorClassifyCap of the
// body; a 429 body is never forwarded verbatim (forwardableErrorStatus denies
// the status), so the peek changes nothing downstream.
//
// The behavioural fallback lives here rather than in classifyRateLimit because
// it reads the breaker: an unknown 429 from a (provider, model) circuit that
// served a 2xx inside rate_limit_recent_success_window is saturated, since a
// spent window does not come back in a minute. With no recent success the
// verdict stays unknown and the 429 is charged.
//
// rate_limit_classify_enabled OFF: no body peek, no verdict, every consumer
// sees rateLimitUnknown.
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
	v.detail = body
	st.rateLimit = v
	st.logData.noteAttemptPhrase(v.phrase)
	return v
}

// failNoAvailableProvider answers a request whose failover group resolved to
// zero candidates. When the circuit breaker did the emptying nothing upstream
// faulted on this request, so the answer is a 429 with a Retry-After naming
// the earliest instant a circuit comes back, capped at a minute so a long
// quota pin never tells a client to go away for hours. The kind is exhaustion
// only when every skipped candidate is waiting out a quota pin; a genuine
// upstream fault (no breaker skips at all: everything disabled or missing)
// answers 502, and so does switching failover_exhaustion_status_429 off.
func (h *Handler) failNoAvailableProvider(w http.ResponseWriter, r *http.Request, st *requestState, displayModel string, timings resolveTimings, cacheHits resolveCacheHits, skips breakerSkipSummary) {
	msg := "no available provider for hotel/" + displayModel
	metrics.RecordFailoverExhausted(displayModel, "no_available_provider")
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
// attempt's own verdict, which carries what the body-driven classifier cannot
// see (headers, the behavioural fallback). It refines only the generic kind: a
// body-derived provider_not_entitled names WHO fixes it and must not be
// blurred back into "quota".
func rateLimitTerminalKind(kind ErrorKind, reason string, status int, v rateLimitVerdict) (ErrorKind, string) {
	if status != http.StatusTooManyRequests || kind != KindProviderError {
		return kind, reason
	}
	if rlKind, ok := v.errorKind(); ok {
		return rlKind, rateLimitClientReason(v.class)
	}
	return kind, reason
}

// rateLimitClientReason words a classified 429 for the client:
// gateway-authored static text, never the provider's body.
func rateLimitClientReason(class rateLimitClass) string {
	if class == rateLimitExhausted {
		return "the provider's usage quota is exhausted; retry after it resets"
	}
	return "the provider is at capacity; retry shortly"
}

// rateLimitReqErr is the structured attempt error for a classified 429, so the
// terminal renderers can tell "busy" from "spent" from "failed".
func rateLimitReqErr(v rateLimitVerdict, attempt int, providerName string) reqError {
	kind, _ := v.errorKind()
	detail := "HTTP 429 (" + v.class.String() + ")"
	return reqError{Kind: kind, Attempt: attempt, Provider: providerName, Detail: detail}
}

// failoverReqErr is the attempt error recorded when an eligible upstream error
// sends the loop to the next candidate: the classified-429 shape when a
// verdict exists, the generic one otherwise.
func failoverReqErr(rl rateLimitVerdict, attempt int, providerName string, status int) reqError {
	if status == http.StatusTooManyRequests && rl.class != rateLimitUnknown {
		return rateLimitReqErr(rl, attempt, providerName)
	}
	return reqError{Kind: KindProviderError, Attempt: attempt, Provider: providerName, Detail: fmt.Sprintf("HTTP %d", status)}
}

// judge429AndRecordBreaker is the one call an attempt path makes between
// reading the status and acting on it: classify a 429 (eligible ones only, so
// with failover_on_rate_limit OFF a 429 keeps its stay-and-forward handling)
// and record the breaker outcome with the verdict. One entry point for the
// sequential, hedged and pass-through paths, so the three cannot drift.
func (h *Handler) judge429AndRecordBreaker(ctx context.Context, st *requestState, candidate modelCandidate, resp *http.Response, isFailoverEligible bool) rateLimitVerdict {
	var rl rateLimitVerdict
	if isFailoverEligible {
		rl = h.classify429Attempt(ctx, st, candidate, resp)
	}
	// Every 429 lands here, on all three serve paths, so this is the one place
	// to count them by class and to keep the cap ledger. Both also see a 429
	// failover may not act on (failover_on_rate_limit off): that is a routing
	// choice, and it must not blind the badge that exists for the providers
	// nothing else reports on. Such a 429 is classified by a read that leaves
	// the request untouched, so the client sees exactly what it otherwise would.
	if resp.StatusCode == http.StatusTooManyRequests {
		seen := rl
		if !isFailoverEligible {
			seen = h.peekRateLimitVerdict(ctx, resp)
		}
		metrics.RecordUpstreamRateLimit(candidate.provider.Name, candidateModelID(candidate), seen.class.String())
		// An exhausted body is the one quota reading a provider with no usage
		// API ever gives; the ledger keeps the latest per provider for its badge.
		if seen.class == rateLimitExhausted {
			h.CapLedger().Note(candidate.provider.ID, provider.CapNote{Phrase: seen.phrase, Model: candidateModelID(candidate), Entitled: seen.entitled, At: time.Now()})
		}
	}
	// A saturated 429 teaches the in-flight learner: the pool is provably
	// smaller than the load that included this request, so the allowance is cut
	// and the NEXT requests spill to the other entries. Here rather than inside
	// the breaker outcome, because the limiter is its own feature:
	// circuit_breaker_enabled off must not stop it learning. The drawing
	// request's own slot settles FIRST (idempotently), so cut's arithmetic sees
	// exactly the load that fit, never a count that depends on whether the body
	// reader beat it to the release.
	if rl.class == rateLimitSaturated && st.inflightEnabled {
		st.attemptSlot.settle(false)
		h.inflight.cut(candidate.provider.ID, rl.retryAfter)
	}
	h.recordBreakerOutcome(ctx, st, candidate, resp.StatusCode, isFailoverEligible, rl)
	return rl
}

// peekRateLimitVerdict classifies a 429 that failover may not act on, for the
// 429 counter and the cap ledger only. It rebuffers what it read for whoever
// forwards the body and writes nothing onto the request state, so the response
// and its log row are untouched. No behavioural fallback: that reads the
// breaker, and a verdict nothing acts on does not earn the lookup.
// rate_limit_classify_enabled off classifies nothing.
func (h *Handler) peekRateLimitVerdict(ctx context.Context, resp *http.Response) rateLimitVerdict {
	if !h.settingsRepo.GetBool(ctx, "rate_limit_classify_enabled", true) {
		return rateLimitVerdict{}
	}
	head, _ := io.ReadAll(io.LimitReader(resp.Body, failoverErrorClassifyCap))
	rest := resp.Body
	resp.Body = rebufferedBody{Reader: io.MultiReader(bytes.NewReader(head), rest), Closer: rest}
	maxWait := h.settingsRepo.GetDuration(ctx, "rate_limit_saturation_max_wait", defaultSaturationMaxWait)
	return classifyRateLimit(resp.StatusCode, resp.Header, util.SanitizeLogBody(string(head), 10000), maxWait)
}

// deferSaturatedRetry closes out an attempt whose LAST candidate answered a
// saturated 429: the response is drained, the attempt recorded, and the loop
// told to wait and try the same candidate once more (outcomeRetrySaturated).
// It absorbs a short slot-freeing gap rather than building a queue:
// st.saturationRetried caps it at one.
func (h *Handler) deferSaturatedRetry(st *requestState, candidate modelCandidate, resp *http.Response, attempt int) candidateOutcome {
	st.saturationRetried = true
	closeDeferredAttempt(st, resp, attempt, rateLimitReqErr(st.rateLimit, attempt, candidate.provider.Name), st.rateLimit.detail, st.rateLimit.phrase)
	debuglog.Info("proxy: last candidate saturated, waiting to retry it once", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "retry_after", st.rateLimit.retryAfter, "attempt", attempt+1)
	return outcomeRetrySaturated
}
