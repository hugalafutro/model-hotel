package proxy

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// The per-attempt trail (request_logs.attempts): one element per failover
// attempt, written once at terminal time. It exists because request_logs kept
// only the TERMINAL attempt, so on 2026-08-31 every Neuralwatt 429 that was
// attempt 0 of a request another provider then served left no trace at all.
//
// Lifecycle: every attempt path opens a record when it commits to a candidate
// (beginAttempt, the hedged launch, the breaker skip at resolve time) and
// closes it when the attempt's fate is known. Non-terminal fates (failover,
// busy skip, the saturation wait) close explicitly at the site that decides
// them; the terminal attempt is closed by updateRequestLog from the flat
// columns, so every terminal path in the package gets it for free.

// attemptRecord is one element of the trail. Field names are the JSON the
// logs API returns and the dashboard renders.
type attemptRecord struct {
	// Attempt is the loop's index, the same numbering as failover_attempt.
	// -1 marks a candidate the circuit breaker refused before any request was
	// made (Breaker "skipped"): it was considered, never attempted.
	Attempt    int    `json:"attempt"`
	ProviderID string `json:"provider_id"`
	Provider   string `json:"provider"`
	Model      string `json:"model"`
	// Status is the upstream HTTP status the attempt reached (after the
	// MiniMax envelope remap); 0 when no response was seen.
	Status    int    `json:"status,omitempty"`
	ErrorKind string `json:"error_kind,omitempty"`
	// Detail is at most maxAttemptDetailRunes of the sanitized, credential-
	// masked upstream error: the provider's error code or first sentence,
	// never request content (see attemptDetail).
	Detail string `json:"detail,omitempty"`
	// Phrase is the rate-limit phrase-table entry a 429 matched, when one did.
	// It is what the phrase staleness report counts: a phrase absent from
	// every trail for 90 days has stopped earning its place in the table.
	Phrase     string  `json:"phrase,omitempty"`
	DurationMs float64 `json:"duration_ms"`
	TTFTMs     float64 `json:"ttft_ms,omitempty"`
	Hedged     bool    `json:"hedged,omitempty"`
	// Breaker says what the attempt did to the circuit: charge, noop, success,
	// alive (a non-failover status credited the circuit without serving),
	// skipped (refused before a request), disabled. Empty when the attempt
	// ended before the breaker was consulted (a client that hung up).
	Breaker string `json:"breaker,omitempty"`
}

// The Breaker verdicts an attempt can record.
const (
	breakerCharge   = "charge"
	breakerNoop     = "noop"
	breakerSuccess  = "success"
	breakerAlive    = "alive"
	breakerSkipped  = "skipped"
	breakerDisabled = "disabled"
)

// maxAttemptDetailRunes bounds attemptRecord.Detail. A provider's error code
// or first sentence fits; a provider quoting the prompt back does not, which
// with the credential masker is the second of the two fences the design asks
// for.
const maxAttemptDetailRunes = 160

// attemptDetail reduces an upstream error text to what the trail may carry:
// credential-masked, whitespace-collapsed and capped at maxAttemptDetailRunes
// on a rune boundary. The input is expected to be already sanitized
// (util.SanitizeLogBody or errString); masking here is the fence for a path
// that forgot.
func attemptDetail(masker credentialMasker, s string) string {
	if s == "" {
		return ""
	}
	s = string(masker.mask([]byte(s)))
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= maxAttemptDetailRunes {
		return s
	}
	return string([]rune(s)[:maxAttemptDetailRunes]) + "…"
}

// openAttemptRecord starts the record for an attempt that is committing to a
// candidate. startedAt is when the attempt began (a hedged probe's launch
// instant, not when its result arrived). cbEnabled false records the breaker
// as disabled up front, because no verdict site will run to say otherwise.
//
// Every method here tolerates a nil receiver: unit tests drive attempt paths
// with a bare requestState, and a trail nobody will read is not worth a panic.
func (l *requestLogData) openAttemptRecord(attempt int, candidate modelCandidate, hedged bool, startedAt time.Time, cbEnabled bool) {
	if l == nil {
		return
	}
	l.openAttempt = &attemptRecord{
		Attempt:    attempt,
		ProviderID: candidate.provider.ID.String(),
		Provider:   candidate.provider.Name,
		Model:      candidateModelID(candidate),
		Hedged:     hedged,
	}
	l.attemptStarted = startedAt
	l.attemptBreaker = ""
	l.attemptStatus = 0
	l.attemptPhrase = ""
	if !cbEnabled {
		l.attemptBreaker = breakerDisabled
	}
}

// noteAttemptStatus records the upstream status the attempt in flight
// reached, once the MiniMax envelope remap has had its say. The terminal
// close prefers it to the client-facing statusCode.
func (l *requestLogData) noteAttemptStatus(status int) {
	if l == nil {
		return
	}
	l.attemptStatus = status
}

// noteAttemptPhrase records the rate-limit phrase a 429 on the attempt in
// flight matched, for the terminal close; non-terminal closes take it from
// the verdict directly.
func (l *requestLogData) noteAttemptPhrase(phrase string) {
	if l == nil {
		return
	}
	l.attemptPhrase = phrase
}

// closeTerminalAttempt closes the attempt in flight from the flat columns at
// the terminal write, preferring the upstream status and the phrase stamped
// on the attempt over what the client was answered.
func (l *requestLogData) closeTerminalAttempt() {
	if l == nil || l.openAttempt == nil {
		return
	}
	status := l.attemptStatus
	if status == 0 {
		status = l.statusCode
	}
	l.closeAttemptRecord(status, l.errorKind, l.errorMessage, l.attemptPhrase, l.ttftMs)
}

// noteBreaker records what the breaker was just told about the attempt in
// flight. Called beside every Record* call on the request path; the hedged
// probe reads it back off its private log snapshot into hedgeResult.breaker.
func (l *requestLogData) noteBreaker(verdict string) {
	if l == nil {
		return
	}
	l.attemptBreaker = verdict
}

// closeAttemptRecord finishes the open record with the attempt's fate and
// appends it to the trail. A no-op when nothing is open, so a terminal path
// that follows an explicit close (all providers exhausted after the last
// failover) cannot append the same attempt twice.
func (l *requestLogData) closeAttemptRecord(status int, kind ErrorKind, detail, phrase string, ttftMs float64) {
	if l == nil || l.openAttempt == nil {
		return
	}
	rec := l.openAttempt
	l.openAttempt = nil
	rec.Status = status
	rec.ErrorKind = string(kind)
	rec.Detail = attemptDetail(l.masker, detail)
	rec.Phrase = phrase
	rec.TTFTMs = ttftMs
	rec.Breaker = l.attemptBreaker
	if !l.attemptStarted.IsZero() {
		rec.DurationMs = float64(time.Since(l.attemptStarted).Microseconds()) / 1000.0
	}
	l.attempts = append(l.attempts, *rec)
}

// appendAttemptRecord adds a record whose fate is already known in full: a
// hedged loser reported by the orchestrator, or a candidate the breaker
// refused at resolve time.
func (l *requestLogData) appendAttemptRecord(rec attemptRecord) {
	if l == nil {
		return
	}
	l.attempts = append(l.attempts, rec)
}

// hedgeLoserRecord is the trail record for a hedged probe that did not win:
// its fate is known in full when the orchestrator receives the result. The
// detail is the probe's own error text (already masked by the probe's
// classifier or errString) or, for a 429, the classifier's body excerpt;
// masked again here with the loser's own credential, since the shared log
// entry's masker belongs to whichever candidate last ran the sequential path.
func hedgeLoserRecord(res hedgeResult, candidate modelCandidate, launchedAt time.Time) attemptRecord {
	detail := res.reqErr.Underlying
	if detail == "" {
		detail = res.reqErr.Detail
	}
	if res.rateLimit.detail != "" {
		detail = res.rateLimit.detail
	}
	return attemptRecord{
		Attempt:    res.idx,
		ProviderID: candidate.provider.ID.String(),
		Provider:   candidate.provider.Name,
		Model:      candidateModelID(candidate),
		Status:     res.status,
		ErrorKind:  string(res.reqErr.Kind),
		Detail:     attemptDetail(newCredentialMasker(candidate.apiKey), detail),
		Phrase:     res.rateLimit.phrase,
		DurationMs: float64(time.Since(launchedAt).Microseconds()) / 1000.0,
		TTFTMs:     res.trueTtftMs,
		Hedged:     true,
		Breaker:    res.breaker,
	}
}

// hedgeAbandonedRecord is the trail entry for a hedged attempt still in flight
// when the race ended: launched, cancelled, never resolved. kind and detail say
// why the race ended (another candidate won, the failover deadline, the client
// left). Nothing is known about what it would have answered, so it carries no
// status, and its breaker verdict, if the cancelled goroutine records one, is
// not the trail's to claim. The duration is launch to abandonment.
func hedgeAbandonedRecord(idx int, candidate modelCandidate, launchedAt time.Time, kind ErrorKind, detail string) attemptRecord {
	return attemptRecord{
		Attempt:    idx,
		ProviderID: candidate.provider.ID.String(),
		Provider:   candidate.provider.Name,
		Model:      candidateModelID(candidate),
		ErrorKind:  string(kind),
		Detail:     detail,
		DurationMs: float64(time.Since(launchedAt).Microseconds()) / 1000.0,
		Hedged:     true,
	}
}

// appendBreakerSkip records a candidate the circuit breaker refused before any
// request was made, so the trail can say "Z.ai: skipped (circuit open)" ahead
// of the providers that were actually tried.
func (l *requestLogData) appendBreakerSkip(providerID uuid.UUID, providerName, model string) {
	if l == nil {
		return
	}
	l.attempts = append(l.attempts, attemptRecord{
		Attempt:    -1,
		ProviderID: providerID.String(),
		Provider:   providerName,
		Model:      model,
		Detail:     "circuit breaker open",
		Breaker:    breakerSkipped,
	})
}

// attemptsJSON renders the trail for the request_logs.attempts column: nil (a
// SQL NULL) when the request never committed to a candidate, so a validation
// failure or an unknown model leaves the column exactly as an older binary
// would.
func (l *requestLogData) attemptsJSON() []byte {
	if l == nil || len(l.attempts) == 0 {
		return nil
	}
	// Skips first, then by attempt index: a hedged race appends its losers in
	// arrival order and the winner last, so attempt 0 can follow attempt 1.
	// Stable, so the saturation retry keeps its place behind the attempt it
	// repeats. The column is documented as being in order; this is where.
	sort.SliceStable(l.attempts, func(i, j int) bool { return l.attempts[i].Attempt < l.attempts[j].Attempt })
	b, err := json.Marshal(l.attempts)
	if err != nil {
		return nil
	}
	return b
}

// failoverProviders names the provider of every attempt after the first, in
// trail order, for the per-provider failover counter. Hedged launches count:
// they are the fan-out to a fallback entry the counter exists to show. Breaker
// skips (attempt -1) were never attempts and do not.
func (l *requestLogData) failoverProviders() []string {
	var out []string
	for _, a := range l.attempts {
		if a.Attempt >= 1 {
			out = append(out, a.Provider)
		}
	}
	return out
}
