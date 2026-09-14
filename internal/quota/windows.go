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
}

// Windows reads every quota window a stored snapshot carries, for the Prometheus
// quota gauges. It is the observational counterpart of Assess: Assess answers
// one question (is the account spent, and until when) under fail-open rules
// that never pin a healthy provider on a guess, while Windows reports every
// figure the payload states and skips only what it cannot read. Provider types
// without windows (a plain balance, an unknown type) report nothing.
func Windows(providerType string, s Snapshot) []Window {
	if noPayload(s) {
		return nil
	}
	switch providerType {
	case "zai-coding":
		return zaiCodingWindows(s.Payload)
	case "kimi-code":
		return kimiCodeWindows(s.Payload)
	case "minimax":
		return miniMaxWindows(s.Payload)
	case "neuralwatt":
		return neuralwattWindows(s.Payload)
	case "opencode-go":
		return openCodeGoWindows(s.Payload)
	default:
		return nil
	}
}

// zaiCodingWindows reports the 5-hour and weekly token windows. Only the
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
		if l.Type != "TOKENS_LIMIT" || l.Percentage == nil || *l.Percentage < 0 || *l.Percentage > 100 {
			continue
		}
		var name string
		switch l.Unit {
		case 3:
			name = "5h"
		case 6:
			name = "weekly"
		default:
			continue
		}
		w := Window{Name: name, Used: *l.Percentage / 100}
		if t, ok := epochToTime(l.NextResetTime); ok {
			w.ResetsAt = t
		}
		out = append(out, w)
	}
	return out
}

// kimiCodeWindows reports the subscription cycle counter and every rolling
// limit, named by its span (5h, 7d). Kimi's figures are decimal strings and a
// spent window omits remaining while a fresh one omits used, so the share
// comes from kimiRemaining, the same read the assessor makes.
func kimiCodeWindows(payload json.RawMessage) []Window {
	var res provider.KimiCodeQuotaResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return nil
	}
	var out []Window
	if w, ok := kimiWindow("cycle", res.Usage); ok {
		out = append(out, w)
	}
	for _, l := range res.Limits {
		if w, ok := kimiWindow(kimiWindowName(l.Window), l.Detail); ok {
			out = append(out, w)
		}
	}
	return out
}

func kimiWindow(name string, d provider.KimiCodeQuotaDetail) (Window, bool) {
	limit, err := strconv.ParseInt(strings.TrimSpace(d.Limit), 10, 64)
	if err != nil || limit <= 0 {
		return Window{}, false
	}
	remaining, ok := kimiRemaining(d)
	if !ok {
		return Window{}, false
	}
	w := Window{Name: name, Used: 1 - float64(remaining)/float64(limit)}
	if t, ok := parseResetString(d.ResetTime); ok {
		w.ResetsAt = t
	}
	return w, true
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

// openCodeGoWindows reports the rolling, weekly and monthly windows. A window
// the payload does not carry decodes to zeroes; its empty resetsAt tells it
// apart from a fresh one, and it is skipped rather than reported untouched.
func openCodeGoWindows(payload json.RawMessage) []Window {
	var res provider.OpenCodeGoUsageResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return nil
	}
	var out []Window
	for _, x := range []struct {
		name string
		w    provider.OpenCodeGoUsageWindow
	}{{"rolling", res.Usage.Rolling}, {"weekly", res.Usage.Weekly}, {"monthly", res.Usage.Monthly}} {
		if x.w.ResetsAt == "" && x.w.Status == "" {
			continue
		}
		w := Window{Name: x.name, Used: x.w.Percent / 100}
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
// same precedence addMiniMaxWindow applies.
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
			out = append(out, w)
		}
	}
	return out
}

func miniMaxWindow(name string, status int, total, used int64, remainingPercent *float64, endTime int64) (Window, bool) {
	if status != 1 {
		return Window{}, false
	}
	w := Window{Name: name}
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
