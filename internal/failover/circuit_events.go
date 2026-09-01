package failover

import (
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// The breaker's SSE event and the sentence an alert renders from it. Split out
// of circuitbreaker.go when that file reached the size ceiling.

// publishEvent builds the SSE event for a circuit breaker state transition and
// hands it to after, for the caller to publish once cb.mu is released. r is the
// walk the caller already started, so the flags here are the ones its log line
// reported. Must be called with cb.mu held: everything the event says is read
// from the breaker here, and nothing is read once it leaves.
func (cb *CircuitBreaker) publishEvent(after *afterUnlock, providerID uuid.UUID, providerName, state, model string, c *circuit, r *cooldownReads) {
	// quota_pinned and backed_off report the overrides currently governing this
	// circuit, not a claim about whether the circuit is blocking traffic right
	// now — the same predicates ProviderStatus uses. With the default
	// HalfOpenMaxProbes of 1 the distinction never surfaces, but a half-open
	// circuit that has banked a probe still carries its override until
	// RecordSuccess closes it.
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
		// seen. They are what an alert has to say for an operator to act on it
		// without opening a terminal.
		"cause":  c.lastCause,
		"status": c.lastStatus,
		// pin_source tells a measured pin ("advisor") from one inferred out of
		// the exhausted response itself ("response"); empty when no pin governs.
		"pin_source": cb.pinSourceForWith(c, r),
		// provider_open is the derived verdict as it stands after this
		// transition. The event names one model, and at the default span of 2 the
		// first model to open leaves the provider serving everything else, so
		// without this flag a consumer would have to re-derive the verdict from a
		// span setting it cannot see and circuits it is not shown.
		"provider_open":     providerOpen,
		"consecutive_fails": c.consecutiveFails,
		"quota_pinned":      pinned,
		// backed_off and failed_probes explain a cooldown longer than the
		// setting: the flag is what governs, the count is what happened.
		"backed_off":    backedOff,
		"failed_probes": c.failedProbes,
	}
	// model_id is the identity the alert dispatcher debounces on. An open
	// carries it only while the provider is still serving: then the event is
	// about one model, and keying it on the provider would let the first model
	// to fail suppress every sibling that fails beside it. Once the verdict says
	// the provider itself is skipped, an open is about the provider: the verdict
	// lapses every time a blocking circuit's cooldown elapses, which lets one
	// more sibling through to fail and open, and a fifty-model provider outage
	// keyed per model would notify fifty times inside one alert window for what
	// is one fact. Without the key the dispatcher falls back to provider_id. A
	// close always carries it: a recovery is about the model that recovered
	// whatever the verdict still says about its siblings, and two models coming
	// back inside one window are two recoveries, not one.
	if state != "open" || !providerOpen {
		meta["model_id"] = model
	}
	if state == "open" {
		// cooldown_ms is the wait actually enforced. next_retry_at accompanies
		// it whenever something other than the configured cooldown governs, so
		// a consumer never has to add the two itself, and it is deliberately not
		// called "resets_at": under a pin it is openedAt plus the ceiling-clamped
		// and jittered pin, exactly the instant the status API publishes under
		// that name, not the provider's quota reset, which on a weekly plan lies
		// days beyond a 24h-capped pin.
		meta["cooldown_ms"] = cooldown.Milliseconds()
		if pinned || backedOff {
			meta["next_retry_at"] = c.openedAt.Add(cooldown).Format(time.RFC3339)
		}
	}
	msg := breakerEventMessage(providerName, state, model, providerOpen, c.lastCause)
	// The suffix attributes the wait to the backoff, so it is added only when
	// the backoff is the value in force. A circuit can be backed off and pinned
	// at once, and when the pin reaches further it is quota, not failed retries,
	// that holds the circuit; saying "backing off, next retry in 10h" there
	// would blame the model for a provider's spent window.
	if state == "open" && backedOff && cooldown == c.cooldownBackoff {
		msg += backoffSuffix(cooldown, c.failedProbes)
	}
	// Stamped here, under the lock, rather than by Publish: two goroutines
	// transitioning the same circuit publish once each has unlocked, in
	// whichever order they resume, and a consumer ordering by timestamp must
	// still see the transitions in the order they happened.
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
// message is all an outbound alert renders, so without it the notification for
// a circuit fifteen minutes from its next retry is byte-identical to one sixty
// seconds from it, and the operator has no way to tell a blip from a model that
// has been failing its retries all afternoon.
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
// in an Apprise alert. It names the model because the breaker charges one model
// circuit at a time: at the default span of 2 the first model to open leaves the
// provider serving everything else, and "Provider X circuit breaker: open" alone
// reports an outage that is not happening. The provider-wide verdict is spelled
// out when it flips, because that is the transition that takes the remaining
// models out of rotation, and it is the part worth acting on.
//
// An open names its cause after a colon ("open for model glm-5.3: upstream
// status 429 (saturated)"), because the message is all an alert renders and
// "open" alone sent the operator to a terminal on 2026-08-31. The model id goes
// before it, and the closed form deliberately says nothing about the verdict or
// the cause: a recovery says only what recovered.
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
