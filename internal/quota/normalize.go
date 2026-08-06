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
	case "minimax":
		return assessMiniMax(s.Payload)
	default:
		return Assessment{}
	}
}

// zaiCodingQuotaLimit is the subset of a zai-coding limit entry the quota
// normalizer needs, decoded locally rather than via provider.ZAICodingQuotaLimit
// so Remaining can be *int64: that payload is passed through to the dashboard
// as-is and must not change shape, but a bare int64 cannot tell "field absent"
// apart from an explicit 0, and treating an absent remaining as a spent window
// would pin a healthy provider shut. Same reasoning as minimaxModelRemain below.
type zaiCodingQuotaLimit struct {
	Type          string   `json:"type"`
	Unit          int      `json:"unit"`
	Remaining     *int64   `json:"remaining"`
	Percentage    *float64 `json:"percentage"`
	NextResetTime int64    `json:"nextResetTime"`
}

type zaiCodingQuotaPayload struct {
	Data struct {
		Limits []zaiCodingQuotaLimit `json:"limits"`
	} `json:"data"`
}

func assessZaiCoding(payload json.RawMessage) Assessment {
	var res zaiCodingQuotaPayload
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
		// Z.ai's remaining cannot be trusted on its own: the live API sends an
		// explicit remaining: 0 on windows that are only partially used, while
		// percentage (percent of the window consumed) tracks the account page
		// exactly. A sane percentage therefore decides; remaining decides only
		// when the payload carries no usable percentage, and a missing field
		// decodes to nil, never to 0, so absence is never read as exhausted.
		exhausted := false
		switch {
		case l.Percentage != nil && *l.Percentage >= 0:
			exhausted = *l.Percentage >= 100
		case l.Remaining != nil && *l.Remaining <= 0:
			exhausted = true
		}
		if !exhausted {
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

// minimaxModelRemain is the subset of a MiniMax model_remains entry the quota
// normalizer needs, decoded locally rather than via provider.MiniMaxModelRemain
// so the remaining-percent fields can be *float64: that payload is passed
// through to the dashboard as-is and must not change shape, but a bare
// float64 cannot tell "field absent" apart from an explicit 0, and treating
// an absent percent as 0% remaining would pin a healthy provider shut.
type minimaxModelRemain struct {
	EndTime                         int64    `json:"end_time"`
	CurrentIntervalStatus           int      `json:"current_interval_status"`
	CurrentIntervalTotalCount       int64    `json:"current_interval_total_count"`
	CurrentIntervalUsageCount       int64    `json:"current_interval_usage_count"`
	CurrentIntervalRemainingPercent *float64 `json:"current_interval_remaining_percent"`
	WeeklyEndTime                   int64    `json:"weekly_end_time"`
	CurrentWeeklyStatus             int      `json:"current_weekly_status"`
	CurrentWeeklyTotalCount         int64    `json:"current_weekly_total_count"`
	CurrentWeeklyUsageCount         int64    `json:"current_weekly_usage_count"`
	CurrentWeeklyRemainingPercent   *float64 `json:"current_weekly_remaining_percent"`
}

type minimaxQuotaPayload struct {
	ModelRemains []minimaxModelRemain `json:"model_remains"`
}

func assessMiniMax(payload json.RawMessage) Assessment {
	var res minimaxQuotaPayload
	if err := json.Unmarshal(payload, &res); err != nil {
		return Assessment{}
	}
	var e earliestReset
	// MiniMax reports per model while the breaker is per provider. Collect the
	// earliest reset among every spent window across every model: that is the
	// soonest moment anything on this provider could work again.
	for _, m := range res.ModelRemains {
		addMiniMaxWindow(&e, m.CurrentIntervalStatus, m.CurrentIntervalTotalCount, m.CurrentIntervalUsageCount, m.CurrentIntervalRemainingPercent, m.EndTime)
		addMiniMaxWindow(&e, m.CurrentWeeklyStatus, m.CurrentWeeklyTotalCount, m.CurrentWeeklyUsageCount, m.CurrentWeeklyRemainingPercent, m.WeeklyEndTime)
	}
	return e.result(time.Now())
}

// addMiniMaxWindow records one window if it is spent. Only a window the plan
// actually covers is considered at all: status 1 means active, 3 means the
// model class is not in the plan and its percent reads misleadingly as 100
// (per plans/already-implemented/2026-07-19-minimax-provider-research.md).
// A missing status decodes to 0 and is skipped the same way, so an
// unrecognized shape fails open rather than guesses.
//
// Counts win when present: total > 0 means the count fields are meaningful,
// and used >= total is exhausted, matching the other providers' rule. Some
// Token Plan tiers report all-zero counts even on an active window, in which
// case (total <= 0) the remaining-percentage field is the only real signal —
// but only when it was actually present in the payload; a missing percent
// decodes to nil, never to 0, so it can never be misread as "0% remaining".
// The percent is REMAINING, not consumed, despite the neighbouring
// *_usage_count field names, and a value outside [0, 100] is skipped as
// nonsense rather than guessed at.
func addMiniMaxWindow(e *earliestReset, status int, total, used int64, remainingPercent *float64, endTime int64) {
	if status != 1 {
		return
	}
	var exhausted bool
	switch {
	case total > 0:
		exhausted = used >= total
	case remainingPercent != nil && *remainingPercent >= 0 && *remainingPercent <= 100:
		exhausted = *remainingPercent <= 0
	default:
		return
	}
	if !exhausted {
		return
	}
	if t, ok := epochToTime(endTime); ok {
		e.add(t)
	}
}
