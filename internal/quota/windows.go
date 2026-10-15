package quota

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// Window is one quota window as the dashboard reads it: how much of it is
// consumed and when it rolls over. Used is a share, 0 for untouched and 1 for
// spent, and can exceed 1 on a provider that serves into overage. ResetsAt is
// zero when the payload does not date the reset.
type Window struct {
	Name     string
	Used     float64
	ResetsAt time.Time
	// Governs marks a window the assessor judges on its own: spent means the
	// provider is exhausted. Z.ai's MCP call window and NeuralWatt's two
	// balances are shown but do not govern (NeuralWatt refuses only once both
	// are gone), so a reserve leaves them alone too.
	Governs bool
	// Span is the window's length. The operator's quota reserve applies to the
	// windows whose span equals the type's reserveSpan (AssessWithReserve),
	// the longest window the type carries, since that is the allowance the
	// reserve keeps back while the shorter ones refill on their own. Zero when
	// the payload does not state the length; such a window never matches.
	Span time.Duration
}

// Windows reads every quota window a stored snapshot carries, for the Prometheus
// quota gauges. It is the observational counterpart of Assess: Assess answers
// one question (is the account spent, and until when) under fail-open rules
// that never pin a healthy provider on a guess, while Windows reports every
// figure the payload states and skips only what it cannot read. Provider types
// without windows (a plain balance, an unknown type) report nothing.
func Windows(providerType string, s Snapshot) []Window {
	r, ok := quotaReaders[providerType]
	if !ok || noPayload(s) {
		return nil
	}
	return r.windows(s.Payload)
}

// zaiCodingWindows reports the 5-hour and weekly token windows and the MCP
// call window, the three the quota modal shows (getZaiCodingFiveHourLimit and
// its siblings in web-shared/quota/zai.ts). Only the
// percentage is trusted, for the reason assessZaiCoding gives: the live API
// sends remaining: 0 on windows that are only partly used. A percentage outside
// [0, 100] is nonsense rather than overage (Z.ai has no overage mode) and is
// skipped under the assessor's bound, so a junk figure cannot flatten the axis.
func zaiCodingWindows(payload json.RawMessage) []Window {
	var res zaiCodingQuotaPayload
	if err := json.Unmarshal(payload, &res); err != nil {
		return nil
	}
	var out []Window
	for _, l := range res.Data.Limits {
		if l.Percentage == nil || *l.Percentage < 0 || *l.Percentage > 100 {
			continue
		}
		var name string
		switch {
		case l.Type == "TOKENS_LIMIT" && l.Unit == 3:
			name = "5h"
		case l.Type == "TOKENS_LIMIT" && l.Unit == 6:
			name = "weekly"
		case l.Type == "TIME_LIMIT" && l.Unit == 5:
			name = "mcp"
		default:
			continue
		}
		w := Window{Name: name, Used: *l.Percentage / 100, Governs: l.Type == "TOKENS_LIMIT", Span: spanByName[name]}
		if t, ok := epochToTime(l.NextResetTime); ok {
			w.ResetsAt = t
		}
		out = append(out, w)
	}
	return out
}

// kimiCodeWindows reports the top-level usage block, which is the weekly
// window (getKimiCodeWeeklyLimit in web-shared/quota/kimi.ts reads it so), and
// every rolling limit named by its span (5h, 7d). Kimi refuses at the limit,
// so its share stops at 1: kimiRemaining reports zero left once used reaches
// the limit. Kimi's figures are decimal strings and a
// spent window omits remaining while a fresh one omits used, so the share
// comes from kimiRemaining, the same read the assessor makes.
func kimiCodeWindows(payload json.RawMessage) []Window {
	var res provider.KimiCodeQuotaResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return nil
	}
	var out []Window
	if w, ok := kimiWindow("weekly", 7*24*time.Hour, res.Usage); ok {
		out = append(out, w)
	}
	for _, l := range res.Limits {
		if w, ok := kimiWindow(kimiWindowName(l.Window), kimiWindowSpan(l.Window), l.Detail); ok {
			out = append(out, w)
		}
	}
	return out
}

func kimiWindow(name string, span time.Duration, d provider.KimiCodeQuotaDetail) (Window, bool) {
	limit, err := strconv.ParseInt(strings.TrimSpace(d.Limit), 10, 64)
	if err != nil || limit <= 0 {
		return Window{}, false
	}
	remaining, ok := kimiRemaining(d)
	if !ok {
		return Window{}, false
	}
	w := Window{Name: name, Used: 1 - float64(remaining)/float64(limit), Governs: true, Span: span}
	if t, ok := parseResetString(d.ResetTime); ok {
		w.ResetsAt = t
	}
	return w, true
}

// kimiWindowSpan is the length of a Kimi rolling limit, from the duration and
// unit the payload states; an unknown unit measures nothing.
func kimiWindowSpan(w provider.KimiCodeQuotaWindow) time.Duration {
	d := time.Duration(w.Duration)
	switch strings.ToLower(strings.TrimPrefix(w.TimeUnit, "TIME_UNIT_")) {
	case "minute":
		return d * time.Minute
	case "hour":
		return d * time.Hour
	case "day":
		return d * 24 * time.Hour
	case "week":
		return d * 7 * 24 * time.Hour
	default:
		return 0
	}
}

// kimiWindowName renders a Kimi window span the way the dashboard modal does:
// a whole number of hours reads as hours, anything else in the unit given.
// The unit arrives both bare (MINUTE) and prefixed (TIME_UNIT_MINUTE).
func kimiWindowName(w provider.KimiCodeQuotaWindow) string {
	unit := strings.ToLower(strings.TrimPrefix(w.TimeUnit, "TIME_UNIT_"))
	switch unit {
	case "minute":
		if w.Duration%60 == 0 {
			return fmt.Sprintf("%dh", w.Duration/60)
		}
		return fmt.Sprintf("%dm", w.Duration)
	case "hour":
		return fmt.Sprintf("%dh", w.Duration)
	case "day":
		return fmt.Sprintf("%dd", w.Duration)
	case "week":
		return fmt.Sprintf("%dw", w.Duration)
	default:
		return fmt.Sprintf("%d %s", w.Duration, unit)
	}
}

// openCodeGoWindows reports the rolling (named 5h, its span, as the Z.ai and
// Kimi windows are), weekly and monthly windows. A window
// the payload does not carry decodes to zeroes; its empty resetsAt tells it
// apart from a fresh one, and it is skipped rather than reported untouched. A
// window OpenCode Go refuses (any status but ok) reads as spent whatever its
// percent says, the same verdict openCodeGoWindowSpent reaches.
func openCodeGoWindows(payload json.RawMessage) []Window {
	var res provider.OpenCodeGoUsageResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return nil
	}
	var out []Window
	for _, x := range []struct {
		name string
		w    provider.OpenCodeGoUsageWindow
	}{{"5h", res.Usage.Rolling}, {"weekly", res.Usage.Weekly}, {"monthly", res.Usage.Monthly}} {
		if x.w.ResetsAt == "" && x.w.Status == "" {
			continue
		}
		w := Window{Name: x.name, Used: x.w.Percent / 100, Governs: true, Span: spanByName[x.name]}
		if openCodeGoWindowSpent(x.w) && w.Used < 1 {
			w.Used = 1
		}
		if t, ok := parseResetString(x.w.ResetsAt); ok {
			w.ResetsAt = t
		}
		out = append(out, w)
	}
	return out
}

// miniMaxWindows reports the interval and weekly window of every model class
// the plan covers, named "<model> interval" and "<model> weekly" since MiniMax
// meters per model. Counts win when present, else the remaining percent, the
// same precedence addMiniMaxWindow applies. The payload dates the interval's
// end but never states its length, so only the weekly window carries a span
// and the reserve line is drawn on every model's weekly window.
func miniMaxWindows(payload json.RawMessage) []Window {
	var res minimaxQuotaPayload
	if err := json.Unmarshal(payload, &res); err != nil || res.BaseResp.StatusCode != 0 {
		return nil
	}
	var out []Window
	for _, m := range res.ModelRemains {
		if w, ok := miniMaxWindow(m.ModelName+" interval", m.CurrentIntervalStatus, m.CurrentIntervalTotalCount, m.CurrentIntervalUsageCount, m.CurrentIntervalRemainingPercent, m.EndTime); ok {
			out = append(out, w)
		}
		if w, ok := miniMaxWindow(m.ModelName+" weekly", m.CurrentWeeklyStatus, m.CurrentWeeklyTotalCount, m.CurrentWeeklyUsageCount, m.CurrentWeeklyRemainingPercent, m.WeeklyEndTime); ok {
			w.Span = spanByName["weekly"]
			out = append(out, w)
		}
	}
	return out
}

func miniMaxWindow(name string, status int, total, used int64, remainingPercent *float64, endTime int64) (Window, bool) {
	if status != 1 {
		return Window{}, false
	}
	w := Window{Name: name, Governs: true}
	switch {
	case total > 0:
		w.Used = float64(used) / float64(total)
	case remainingPercent != nil && *remainingPercent >= 0 && *remainingPercent <= 100:
		w.Used = 1 - *remainingPercent/100
	default:
		return Window{}, false
	}
	if t, ok := epochToTime(endTime); ok {
		w.ResetsAt = t
	}
	return w, true
}

// neuralwattWindows reports the included energy of the billing period, which
// resets at the period end, and the prepaid credit balance, which does not.
// Either is skipped when the payload states no total to measure against.
func neuralwattWindows(payload json.RawMessage) []Window {
	var res struct {
		Balance      provider.NeuralWattQuotaBalance      `json:"balance"`
		Subscription provider.NeuralWattQuotaSubscription `json:"subscription"`
	}
	if err := json.Unmarshal(payload, &res); err != nil {
		return nil
	}
	var out []Window
	if res.Subscription.KWhIncluded > 0 {
		w := Window{Name: "energy", Used: res.Subscription.KWhUsed / res.Subscription.KWhIncluded}
		if t, ok := parseResetString(res.Subscription.CurrentPeriodEnd); ok {
			w.ResetsAt = t
		}
		out = append(out, w)
	}
	if res.Balance.TotalCreditsUSD > 0 {
		out = append(out, Window{Name: "credits", Used: res.Balance.CreditsUsedUSD / res.Balance.TotalCreditsUSD})
	}
	return out
}

// AssessWithReserve is Assess with the operator's reserve applied: a share of
// the provider's longest window kept back for use outside the gateway.
// reserve is that share, 0 for none, up to 0.9. A provider whose payload
// reports no exhaustion is still treated as exhausted once a dated governing
// window of the type's reserve span (reserveSpan: the month for OpenCode Go,
// the week elsewhere) has consumed 1 - reserve of itself, and the verdict
// says Reserved: the pin targets the earliest such reset, the same rule the
// assessors apply to a spent window. The span is fixed per type rather than
// read off the payload, so a payload missing that window (or carrying only
// windows whose length it does not state) places no reserve pin at all: the
// edit modal promises the week or the month, and a 5-hour window must not
// stand in for it. A shorter window refills on its own and stops routing only
// when spent, which matters on a plan the assessor never calls exhausted
// (NanoGPT with overage): there a spent window pins under a reserve too, as
// plain exhaustion. A window the assessor does not judge on its own (Z.ai's
// MCP calls, a NeuralWatt balance) is left alone, an undated window past the
// line cannot place a pin and is ignored, as an undated spent window is, and a
// payload the assessor already reads as exhausted, or cannot read at all, is
// returned as it stands.
func AssessWithReserve(providerType string, s Snapshot, reserve float64, now time.Time) Assessment {
	a := Assess(providerType, s)
	if reserve <= 0 || !a.OK || a.Exhausted {
		return a
	}
	// The line is 1 - reserve, compared with a little slack: 1 - 0.7 is a hair
	// above 30/100 in float64, and the operator's 70% must pin at exactly 30%
	// however the provider stated it. The cap gets the same slack.
	line := 1 - reserve - 1e-9
	windows := Windows(providerType, s)
	var spent earliestReset
	for _, w := range windows {
		if w.Governs && !w.ResetsAt.IsZero() && w.Used >= 1-1e-9 {
			spent.add(w.ResetsAt, true)
		}
	}
	// A spent window whose reset has already passed is no pin (the window has
	// rolled over), and it must not pre-empt the reserve pass either, or a
	// snapshot taken just before a 5-hour rollover would read as healthy and
	// release a standing reserve pin for one poll.
	if r := spent.result(now); r.Exhausted {
		return r
	}
	span, ok := reserveSpan[providerType]
	if !ok {
		return a
	}
	var e earliestReset
	for _, w := range windows {
		if !w.Governs || w.Span != span || w.Used < line || w.ResetsAt.IsZero() {
			continue
		}
		e.add(w.ResetsAt, true)
	}
	if !e.found {
		return a
	}
	r := e.result(now)
	r.Reserved = r.Exhausted
	return r
}

// reserveSpan is the window the operator's reserve is kept on, per type: the
// longest one the type carries. A 5-hour window at the line refills on its
// own within the day and is not what the reserve protects; the weekly or
// monthly allowance behind it is. Every window of that span counts, which is
// how MiniMax's per-model weekly windows each draw the line. Mirrored by
// QUOTA_RESERVE_WINDOW in web/src/pages/Providers/constants.ts, which names
// the window in the edit modal; a type absent here never pins on a reserve.
var reserveSpan = map[string]time.Duration{
	"zai-coding":  spanByName["weekly"],
	"kimi-code":   spanByName["weekly"],
	"minimax":     spanByName["weekly"],
	"nanogpt":     spanByName["weekly"],
	"opencode-go": spanByName["monthly"],
}

// spanByName is the length of the windows the readers name by their span, so
// every reader and reserveSpan speak the same lengths: OpenCode Go's month is
// the calendar-ish 30 days its resetsAt reflects, and a week is seven days
// everywhere.
var spanByName = map[string]time.Duration{
	"5h":      5 * time.Hour,
	"daily":   24 * time.Hour,
	"weekly":  7 * 24 * time.Hour,
	"monthly": 30 * 24 * time.Hour,
}
