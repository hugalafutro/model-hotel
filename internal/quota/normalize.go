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
	case "neuralwatt":
		return assessNeuralwatt(s.Payload)
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
		// exactly. A sane percentage — within [0, 100], the same definition
		// addMiniMaxWindow uses — therefore decides; anything outside that
		// range is nonsense, not a signal, and falls back to the remaining
		// rule (where Z.ai's ever-present remaining: 0 still reads a genuine
		// overage as exhausted). A missing field decodes to nil, never to 0,
		// so absence is never read as exhausted.
		exhausted := false
		switch {
		case l.Percentage != nil && *l.Percentage >= 0 && *l.Percentage <= 100:
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

// neuralwattQuotaPayload is the subset of NeuralWatt's account payload the
// normalizer needs. Both decisive numbers are pointers for the same reason as
// zaiCodingQuotaLimit.Remaining: a bare float64 cannot tell "field absent"
// apart from an explicit 0, and treating an absent balance as spent would pin
// a healthy provider shut.
type neuralwattQuotaPayload struct {
	Balance struct {
		CreditsRemainingUSD *float64 `json:"credits_remaining_usd"`
	} `json:"balance"`
	Subscription struct {
		KwhRemaining *float64 `json:"kwh_remaining"`
		// InOverage is NeuralWatt stating the included energy is gone, the same
		// claim KwhRemaining <= 0 makes numerically. A bare bool rather than a
		// pointer because the snapshot path stores a re-marshal of the provider
		// struct, where the field is a bare bool too: absent upstream arrives
		// here as false, which is the safe direction — it can only ever add
		// exhaustion when NeuralWatt affirmatively says so.
		InOverage        bool   `json:"in_overage"`
		CurrentPeriodEnd string `json:"current_period_end"`
	} `json:"subscription"`
}

// creditsSpentFloorUSD is the balance below which NeuralWatt credits count as
// spent. It is not zero because NeuralWatt does not wait for zero:
// observed on prod 2026-09-01, the account began answering 402
// payment_required ("Subscription access is currently blocked because a usage
// or billing limit was previously reached") with credits_remaining_usd still
// at 0.0035, and it stayed there — the residue never drains, so an exact
// <= 0 test never becomes true and the provider is never pinned.
//
// What a cent is worth depends entirely on the request: the run that triggered
// this burned $9.1061 over 1007 large-context requests (~$0.009 each, so a cent
// is about one), while a 2026-08-24 probe of 49 tiny requests drew ~$0.006
// (~$0.0001 each, so a cent is nearer eighty). The floor therefore forfeits
// somewhere between one and a few dozen requests of genuine headroom, and only
// for an account already in overage with its included energy gone. Being wrong
// in the other direction leaves a provider that answers nothing but 402 sitting
// live in its failover group, which is the state this rule exists to end.
const creditsSpentFloorUSD = 0.01

// assessNeuralwatt handles NeuralWatt's balance model, which differs from the
// window models above: spending the included monthly energy does not make the
// provider refuse requests — it keeps serving in overage, debiting the credit
// balance. Requests only start failing once BOTH the included energy is gone
// and the credit balance has fallen below creditsSpentFloorUSD — not to zero,
// which NeuralWatt never reports; see that constant. The only scheduled
// recovery from that state is the billing period end, so that is the reset the
// pin targets (the
// breaker's 24h pin ceiling re-pins toward it on each open, and an
// off-schedule recovery — a top-up, a plan change — lifts the pin on the next
// poll via the recovered path in buildQuotaAdvice).
//
// That recovery path needs a datable reset to work: an account reporting no
// usable current_period_end assesses OK=false below, which reaches neither
// advice nor recovered, so nothing releases its pin early and it runs the full
// ceiling. That is the intended trade. The alternative — reporting a spent
// account healthy because it did not say when it recovers — puts it in
// recovered, where ReleaseQuotaPins drops the pin and clears the 429-open
// escalation of every circuit it owns, on every poll pass, for an account
// answering nothing but 402.
func assessNeuralwatt(payload json.RawMessage) Assessment {
	var res neuralwattQuotaPayload
	if err := json.Unmarshal(payload, &res); err != nil {
		return Assessment{}
	}
	// Either signal settles the energy side: the number when NeuralWatt reports
	// one, and the flag it sets on entering overage. Reading both means a
	// kwh_remaining that freezes at a residue instead of a clean zero — the same
	// shape the credits balance has — cannot short-circuit the conjunction and
	// leave the credits floor unreachable.
	energySpent := (res.Subscription.KwhRemaining != nil && *res.Subscription.KwhRemaining <= 0) ||
		res.Subscription.InOverage
	creditsSpent := res.Balance.CreditsRemainingUSD != nil &&
		*res.Balance.CreditsRemainingUSD < creditsSpentFloorUSD

	var e earliestReset
	if energySpent && creditsSpent {
		t, ok := parseResetString(res.Subscription.CurrentPeriodEnd)
		if !ok {
			// Spent, but the payload does not say when it recovers. Falling
			// through to e.result here would report "not exhausted", and
			// buildQuotaAdvice reads that as affirmative health: the provider
			// joins `recovered`, and ReleaseQuotaPins then clears the 429-open
			// escalation of every circuit it owns on every poll pass — for an
			// account answering nothing but 402. OK=false is the honest answer;
			// it lands the provider in neither advice nor recovered.
			return Assessment{}
		}
		e.add(t)
	}
	return e.result(time.Now())
}

func assessKimiCode(payload json.RawMessage) Assessment {
	var res provider.KimiCodeQuotaResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return Assessment{}
	}
	var e earliestReset
	// The top-level usage block is the subscription cycle counter and carries
	// its own reset. Spending it blocks the account just as a spent rolling
	// window does, so it is judged by the same rule.
	addKimiWindow(&e, res.Usage)
	for _, l := range res.Limits {
		addKimiWindow(&e, l.Detail)
	}
	return e.result(time.Now())
}

// kimiRemaining reports how much of a Kimi window is left. Kimi's /usages is
// proto3 JSON and omits zero-valued fields, so a spent window carries used and
// no remaining while a fresh one carries remaining and no used. An explicit
// remaining decides; otherwise limit minus used does, but only when both parse.
// A window we cannot read either way reports not-ok and is never treated as
// exhausted: guessing here would pin a healthy provider, which the behaviour
// contract forbids.
func kimiRemaining(d provider.KimiCodeQuotaDetail) (int64, bool) {
	if remaining, err := strconv.ParseInt(strings.TrimSpace(d.Remaining), 10, 64); err == nil {
		return remaining, true
	}
	limit, limitErr := strconv.ParseInt(strings.TrimSpace(d.Limit), 10, 64)
	used, usedErr := strconv.ParseInt(strings.TrimSpace(d.Used), 10, 64)
	if limitErr != nil || usedErr != nil {
		return 0, false
	}
	if used >= limit {
		return 0, true
	}
	return limit - used, true
}

// addKimiWindow records one Kimi quota block if it is spent.
func addKimiWindow(e *earliestReset, d provider.KimiCodeQuotaDetail) {
	remaining, ok := kimiRemaining(d)
	if !ok || remaining > 0 {
		return
	}
	if t, ok := parseResetString(d.ResetTime); ok {
		e.add(t)
	}
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
