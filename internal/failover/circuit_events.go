package failover

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// The breaker's SSE event and the sentence an alert renders from it.

// publishEvent builds the SSE event for a circuit breaker state transition and
// hands it to after, for the caller to publish once cb.mu is released. r is the
// walk the caller already started, so the flags here are the ones its log line
// reported. Must be called with cb.mu held: everything the event says is read
// from the breaker here, and nothing is read once it leaves.
func (cb *CircuitBreaker) publishEvent(after *afterUnlock, providerID uuid.UUID, providerName, state, model string, c *circuit, r *cooldownReads) {
	// quota_pinned and backed_off report the overrides governing this circuit,
	// not whether it is blocking traffic; these are the predicates
	// ProviderStatus uses. A half-open circuit that has banked a probe still
	// carries its override until RecordSuccess closes it.
	pinned := cb.quotaPinnedForWith(c, r)
	backedOff := cb.backedOffForWith(c, r)
	cooldown := cb.effectiveCooldownForWith(c, r)
	providerOpen := cb.providerOpen(cb.circuits[providerID.String()])
	meta := map[string]any{
		"provider_id": providerID.String(),
		"provider":    providerName,
		"model":       model,
		"state":       state,
		// cause and status are the verdict that produced this transition: why
		// the circuit opened ("upstream status 429 (saturated)") or what closed
		// it ("success"), and the upstream status behind it, 0 when none was
		// seen.
		"cause":  c.lastCause,
		"status": c.lastStatus,
		// pin_source tells a measured pin ("advisor") from one inferred out of
		// the exhausted response itself ("response") or out of a response that
		// refused the whole account ("account"); empty when no pin governs.
		"pin_source": cb.pinSourceForWith(c, r),
		// provider_open is the derived verdict as it stands after this
		// transition. The event names one model, so without this flag a consumer
		// would have to re-derive the verdict from a span setting it cannot see.
		"provider_open":     providerOpen,
		"consecutive_fails": c.consecutiveFails,
		"quota_pinned":      pinned,
		// backed_off and failed_probes explain a cooldown longer than the
		// setting: the flag is what governs, the count is what happened.
		"backed_off":    backedOff,
		"failed_probes": c.failedProbes,
	}
	// model_id is the identity the alert dispatcher debounces on. An open
	// carries it only while the provider is still serving, so the first model to
	// fail does not suppress its siblings. Once the verdict says the provider
	// itself is skipped, an open is about the provider, and the dispatcher falls
	// back to provider_id rather than notifying once per model. A close always
	// carries it: two models coming back inside one window are two recoveries.
	if state != "open" || !providerOpen {
		meta["model_id"] = model
	}
	if state == "open" {
		// cooldown_ms is the wait enforced. next_retry_at accompanies it whenever
		// something other than the configured cooldown governs. It is not
		// "resets_at": under a pin it is openedAt plus the ceiling-clamped and
		// jittered pin, not the provider's quota reset, which can lie days beyond
		// a 24h-capped pin.
		meta["cooldown_ms"] = cooldown.Milliseconds()
		if pinned || backedOff {
			meta["next_retry_at"] = c.openedAt.Add(cooldown).Format(time.RFC3339)
		}
	}
	msg := breakerEventMessage(providerName, state, model, providerOpen, c.lastCause)
	// The suffix attributes the wait to the backoff, so it is added only when the
	// backoff is the value in force. A circuit can be backed off and pinned at
	// once, and when the pin reaches further it is quota that holds the circuit.
	if state == "open" && backedOff && cooldown == c.cooldownBackoff {
		msg += backoffSuffix(cooldown, c.failedProbes)
	}
	// Stamped under the lock rather than by Publish: two goroutines transitioning
	// the same circuit publish in whichever order they resume, and a consumer
	// ordering by timestamp must see the transitions in the order they happened.
	ev := events.Event{
		Type:      "circuit_breaker." + state,
		Severity:  cb.severityForState(state),
		Source:    "failover",
		Message:   msg,
		Metadata:  meta,
		Timestamp: time.Now(),
	}
	after.add(func() { events.Publish(ev) })
}

// backoffSuffix extends the open message when the probe backoff governs. The
// message is all an outbound alert renders, so without it a circuit fifteen
// minutes from its next retry reads identically to one sixty seconds from it.
func backoffSuffix(cooldown time.Duration, failedProbes int) string {
	noun := "retries"
	if failedProbes == 1 {
		noun = "retry"
	}
	return fmt.Sprintf(" (backing off after %d failed %s, next retry in %s)", failedProbes, noun, shortDuration(cooldown))
}

// shortDuration renders a cooldown the way an operator reads it: "4m" rather
// than Duration.String's "4m0s", "1h30m" rather than "1h30m0s" or "90m", and
// never "0s" for a cooldown that is merely short.
func shortDuration(d time.Duration) string {
	if d < time.Second || d%time.Second != 0 {
		return d.String()
	}
	if d%time.Minute != 0 {
		return d.Round(time.Second).String()
	}
	h, m := d/time.Hour, (d%time.Hour)/time.Minute
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh%dm", h, m)
	}
}

// breakerEventMessage is the sentence an operator reads in a dashboard toast and
// in an Apprise alert. It names the model, because the breaker charges one model
// circuit at a time and the provider usually keeps serving the rest. The
// provider-wide verdict is spelled out when it flips, since that is what takes
// the remaining models out of rotation.
//
// An open names its cause after a colon ("open for model glm-5.3: upstream
// status 429 (saturated)"), the message being all an alert renders. The closed
// form says nothing about the verdict or the cause: a recovery says only what
// recovered.
func breakerEventMessage(providerName, state, model string, providerOpen bool, cause string) string {
	msg := fmt.Sprintf("Provider %s circuit breaker: %s", providerName, state)
	if model == "" {
		return msg
	}
	msg += " for model " + model
	if state == "open" && providerOpen {
		msg += " (provider skipped)"
	}
	if state == "open" && cause != "" {
		msg += ": " + cause
	}
	return msg
}

func (cb *CircuitBreaker) severityForState(state string) string {
	switch state {
	case "open":
		return "warning"
	case "closed":
		return "success"
	default:
		return "info"
	}
}
