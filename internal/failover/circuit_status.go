package failover

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Cause is why a circuit is being charged or credited: a short operator-facing
// reason and the upstream HTTP status behind it, 0 when no response was seen (a
// connection that never completed, a stream that died before its status meant
// anything). The circuit remembers its most recent one as its last verdict, the
// open transition publishes it, and the detail endpoint lists it per circuit,
// so a status row, an SSE event and an outbound alert can all say what the
// breaker SAW rather than only what it did. Diagnosing the 2026-08-31 incident
// took an hour of polling because none of them could.
//
// Routing metadata only. The reason is always a fixed phrase chosen by the
// caller, never text copied from a provider response, so it can never carry a
// prompt echo or a credential into a status page or an alert.
type Cause struct {
	Status int
	Reason string
}

// UpstreamStatus is the cause for a response whose status alone decided the
// verdict: "upstream status 503". qualifier names a refinement of a status
// that has several readings, so a 429 reports which one the classifier reached:
// "upstream status 429 (saturated)".
func UpstreamStatus(status int, qualifier string) Cause {
	reason := fmt.Sprintf("upstream status %d", status)
	if qualifier != "" {
		reason += " (" + qualifier + ")"
	}
	return Cause{Status: status, Reason: reason}
}

// The verdicts the breaker stamps itself, where the caller has nothing more
// specific to say than which method it called.
const (
	causeSuccess          = "success"
	causeSaturated        = "saturated"
	causeExhausted        = "exhausted"
	causeUnrecognised429  = "unrecognised"
	causeAlive            = "alive"
	causePinRetargeted    = "quota pin retargeted (advisor)"
	causePinReleasedQuota = "quota pin released (provider no longer exhausted)"
	causePinReleasedOff   = "quota pin released (quota polling disabled)"
)

// note records the verdict that just landed on this circuit.
func (c *circuit) note(now time.Time, cause Cause) {
	c.lastCause = cause.Reason
	c.lastStatus = cause.Status
	c.lastAt = now
}

// RecordSaturated remembers a 429 the classifier read as load rather than a
// spent window. It is deliberately NOT a charge and not a credit: a provider at
// its concurrency ceiling is alive, and benching it benches the slots that are
// all busy serving (see recordRateLimitOutcome in the proxy). What it does is
// leave the verdict on the circuit, so a status row can say "busy" about a
// provider whose circuit is closed but whose last three answers were 429s,
// which on 2026-08-31 was the whole story and was visible nowhere.
func (cb *CircuitBreaker) RecordSaturated(providerID uuid.UUID, providerName, model string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c := cb.getOrCreate(providerID.String(), model)
	c.lastCharged = time.Now()
	c.note(c.lastCharged, UpstreamStatus(429, causeSaturated))
	debuglog.Debug("circuit-breaker: saturated 429 noted, circuit not charged", "provider", providerName, "provider_id", providerID, "model", model)
}

// CircuitStatus is one (provider, resolved upstream model) circuit as the
// detail endpoint reports it: the same fields the provider row carries, at the
// level the breaker actually keeps them, plus the last verdict that landed on
// it. The provider row is built from the most degraded of these, so a row
// saying "open" while the model an operator cares about reads "closed" here is
// not a contradiction: it is the per-model keying made visible.
type CircuitStatus struct {
	Model            string `json:"model"`
	State            string `json:"state"`
	ConsecutiveFails int    `json:"consecutive_fails"`
	OpenedAt         string `json:"opened_at,omitempty"`
	CooldownMs       int64  `json:"cooldown_ms,omitempty"`
	NextRetryAt      string `json:"next_retry_at,omitempty"`
	// QuotaPinned, PinSource and BackedOff describe the overrides governing
	// THIS circuit's cooldown, unlike the row's QuotaPinned, which is the
	// verdict's "any blocking circuit is pinned" arm.
	QuotaPinned  bool   `json:"quota_pinned,omitempty"`
	PinSource    string `json:"pin_source,omitempty"`
	BackedOff    bool   `json:"backed_off,omitempty"`
	FailedProbes int    `json:"failed_probes,omitempty"`
	// LastCause is the reason of the most recent verdict on this circuit,
	// LastStatus the upstream status behind it (0 when none was seen) and
	// LastAt when it landed. For an open circuit this is why it opened, until a
	// quota pin retarget or release records that it changed the wait.
	LastCause  string `json:"last_cause,omitempty"`
	LastStatus int    `json:"last_status,omitempty"`
	LastAt     string `json:"last_at,omitempty"`
}

// circuitStatuses lists a provider's circuits for the detail endpoint, sorted
// by model so a polling UI never sees them reshuffle. r is the walk the caller
// already started, so every circuit is judged by the same cooldown reads as the
// row built from them. Must be called with cb.mu held (read lock suffices).
func (cb *CircuitBreaker) circuitStatuses(models modelCircuits, r *cooldownReads) []CircuitStatus {
	out := make([]CircuitStatus, 0, len(models))
	for model, c := range models {
		state := cb.logicalStateWith(c, r)
		s := CircuitStatus{
			Model:            model,
			State:            state.String(),
			ConsecutiveFails: c.consecutiveFails,
			QuotaPinned:      cb.quotaPinnedForWith(c, r),
			PinSource:        cb.pinSourceForWith(c, r),
			BackedOff:        cb.backedOffForWith(c, r),
			FailedProbes:     c.failedProbes,
			LastCause:        c.lastCause,
			LastStatus:       c.lastStatus,
		}
		if !c.lastAt.IsZero() {
			s.LastAt = c.lastAt.Format(time.RFC3339)
		}
		// The same rule the provider row applies to its dominant circuit: the
		// wait is reported only while it is being enforced, and a circuit owed
		// a probe keeps its opened_at so the row can say how long it was dark.
		if state == StateOpen && !c.openedAt.IsZero() {
			cooldown := cb.effectiveCooldownForWith(c, r)
			s.OpenedAt = c.openedAt.Format(time.RFC3339)
			s.CooldownMs = cooldown.Milliseconds()
			s.NextRetryAt = c.openedAt.Add(cooldown).Format(time.RFC3339)
		}
		if state == StateHalfOpen && !c.openedAt.IsZero() {
			s.OpenedAt = c.openedAt.Format(time.RFC3339)
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b CircuitStatus) int {
		return strings.Compare(a.Model, b.Model)
	})
	return out
}
