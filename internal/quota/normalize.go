package quota

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// Assessment is the normalized, provider-type-agnostic view of a quota
// snapshot. OK=false means the snapshot could not be interpreted and the caller
// must fall back to its default behaviour.
type Assessment struct {
	Exhausted bool      // at least one quota window is spent
	ResetsAt  time.Time // earliest reset among exhausted windows
	OK        bool      // the payload was understood
}

// epochUnitBoundary separates second-precision from millisecond-precision epoch
// values. 1e11 seconds is year 5138 and 1e11 milliseconds is year 1973, so no
// realistic reset timestamp is ambiguous.
const epochUnitBoundary = 100_000_000_000

// epochToTime converts a numeric timestamp to UTC, detecting seconds vs
// milliseconds. Non-positive values are rejected rather than guessed.
func epochToTime(v int64) (time.Time, bool) {
	if v <= 0 {
		return time.Time{}, false
	}
	if v < epochUnitBoundary {
		return time.Unix(v, 0).UTC(), true
	}
	return time.UnixMilli(v).UTC(), true
}

// parseResetString handles reset timestamps that arrive as JSON strings: either
// RFC3339 or an epoch encoded as digits.
func parseResetString(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), true
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return epochToTime(n)
	}
	return time.Time{}, false
}

// earliestReset accumulates the soonest reset among exhausted windows. Earliest
// is deliberate: under-pinning costs one extra probe, over-pinning sidelines a
// usable provider.
type earliestReset struct {
	at    time.Time
	found bool
}

func (e *earliestReset) add(t time.Time) {
	if !e.found || t.Before(e.at) {
		e.at, e.found = t, true
	}
}

// result builds the Assessment for a payload that parsed successfully. A reset
// in the past is treated as no pin: the window has already rolled over.
func (e *earliestReset) result(now time.Time) Assessment {
	if !e.found || !e.at.After(now) {
		return Assessment{OK: true}
	}
	return Assessment{Exhausted: true, ResetsAt: e.at, OK: true}
}

// Assess normalizes a stored quota snapshot for the circuit breaker. Only
// window-quota provider types are supported; every other type, and every
// unparseable payload, returns OK=false so callers fall back to their default
// cooldown.
func Assess(providerType string, s Snapshot) Assessment {
	if len(s.Payload) == 0 {
		return Assessment{}
	}
	switch providerType {
	case "zai-coding":
		return assessZaiCoding(s.Payload)
	case "kimi-code":
		return assessKimiCode(s.Payload)
	default:
		return Assessment{}
	}
}

func assessZaiCoding(payload json.RawMessage) Assessment {
	var res provider.ZAICodingQuotaResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return Assessment{}
	}
	var e earliestReset
	for _, l := range res.Data.Limits {
		// unit 3 = 5-hour window, unit 6 = weekly. Matches
		// getZaiCodingFiveHourLimit / getZaiCodingWeeklyLimit in useQuotaData.ts.
		if l.Type != "TOKENS_LIMIT" || (l.Unit != 3 && l.Unit != 6) {
			continue
		}
		if l.Remaining > 0 {
			continue
		}
		if t, ok := epochToTime(l.NextResetTime); ok {
			e.add(t)
		}
	}
	return e.result(time.Now())
}

func assessKimiCode(payload json.RawMessage) Assessment {
	var res provider.KimiCodeQuotaResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return Assessment{}
	}
	var e earliestReset
	for _, l := range res.Limits {
		// Kimi encodes limit/remaining as JSON strings. A value we cannot parse
		// is never treated as exhausted: guessing here would pin a healthy
		// provider, which the behaviour contract forbids.
		remaining, err := strconv.ParseInt(strings.TrimSpace(l.Detail.Remaining), 10, 64)
		if err != nil || remaining > 0 {
			continue
		}
		if t, ok := parseResetString(l.Detail.ResetTime); ok {
			e.add(t)
		}
	}
	return e.result(time.Now())
}
