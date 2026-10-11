package quota

import (
	"encoding/json"
	"time"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// nanoGPTCounter is one NanoGPT token counter beside the cap it counts against.
// The subscription usage endpoint reports a weekly and a daily input-token
// counter, each with used, remaining and an epoch-seconds resetAt, and a limits
// block that carries the caps; a plan without a cap on one of them omits it.
type nanoGPTCounter struct {
	name    string
	limit   *int64
	counter *provider.NanoGPTUsageCounter
}

func nanoGPTCounters(res provider.NanoGPTUsageResponse) []nanoGPTCounter {
	return []nanoGPTCounter{
		{"weekly", res.Limits.WeeklyInputTokens, res.WeeklyInputTokens},
		{"daily", res.Limits.DailyInputTokens, res.DailyInputTokens},
	}
}

// window reads the counter as a quota window. A counter without a positive cap,
// or a cap without a counter, measures nothing and is skipped. The share is
// used over the cap and exceeds 1 on an overage plan that keeps serving past
// its allowance. Both windows govern: NanoGPT refuses chat at the cap unless
// the plan allows overage, which assessNanoGPT reads from the flag.
func (c nanoGPTCounter) window() (Window, bool) {
	if c.limit == nil || *c.limit <= 0 || c.counter == nil {
		return Window{}, false
	}
	w := Window{Name: c.name, Used: float64(c.counter.Used) / float64(*c.limit), Governs: true}
	if t, ok := epochToTime(c.counter.ResetAt); ok {
		w.ResetsAt = t
	}
	return w, true
}

// assessNanoGPT reads a NanoGPT usage snapshot. A counter at or past its cap is
// spent and pins to its resetAt, the earliest across the counters. An
// allowOverage plan is never exhausted, the same rule isNanoGptQuotaSpent in
// web-shared/quota/spent.ts applies to the badge: NanoGPT keeps serving and
// bills the overage, so the only line that stops routing there is the
// operator's reserve, which AssessWithReserve draws over the same windows.
func assessNanoGPT(payload json.RawMessage) Assessment {
	var res provider.NanoGPTUsageResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return Assessment{}
	}
	if res.AllowOverage {
		return Assessment{OK: true}
	}
	var e earliestReset
	for _, c := range nanoGPTCounters(res) {
		w, ok := c.window()
		if !ok || w.Used < 1 {
			continue
		}
		e.add(epochToTime(c.counter.ResetAt))
	}
	return e.result(time.Now())
}

// nanoGPTWindows reports the weekly and daily input-token windows the payload
// carries caps for.
func nanoGPTWindows(payload json.RawMessage) []Window {
	var res provider.NanoGPTUsageResponse
	if err := json.Unmarshal(payload, &res); err != nil {
		return nil
	}
	var out []Window
	for _, c := range nanoGPTCounters(res) {
		if w, ok := c.window(); ok {
			out = append(out, w)
		}
	}
	return out
}
