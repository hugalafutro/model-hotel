package quota

import (
	"fmt"
	"testing"
	"time"
)

// nanoGPTPayload builds a usage body with a weekly cap of 100 tokens and a
// daily cap of 10, each counter used as given and resetting at the given
// epoch seconds. A zero reset leaves the counter undated.
func nanoGPTPayload(weeklyUsed, weeklyReset, dailyUsed, dailyReset int64, overage bool) []byte {
	return fmt.Appendf(nil, `{"providerStatus":"active","allowOverage":%t,
		"limits":{"weeklyInputTokens":100,"dailyInputTokens":10},
		"weeklyInputTokens":{"used":%d,"resetAt":%d},
		"dailyInputTokens":{"used":%d,"resetAt":%d}}`,
		overage, weeklyUsed, weeklyReset, dailyUsed, dailyReset)
}

func TestWindows_NanoGPT_WeeklyAndDailyShares(t *testing.T) {
	ws := Windows("nanogpt", Snapshot{Payload: nanoGPTPayload(90, 1704672000, 12, 1704067200, false)})

	w := window(t, ws, "weekly")
	if !near(w.Used, 0.9) || !w.Governs || !w.ResetsAt.Equal(time.Unix(1704672000, 0)) {
		t.Errorf("weekly: got %+v, want 0.9 governing with its reset", w)
	}
	d := window(t, ws, "daily")
	if !near(d.Used, 1.2) || !d.Governs || !d.ResetsAt.Equal(time.Unix(1704067200, 0)) {
		t.Errorf("daily: got %+v, want overage above 1 with its reset", d)
	}
}

func TestWindows_NanoGPT_SkipsCounterWithoutCapOrCapWithoutCounter(t *testing.T) {
	got := Windows("nanogpt", Snapshot{Payload: []byte(`{"limits":{"weeklyInputTokens":100},
		"dailyInputTokens":{"used":5,"resetAt":1704067200}}`)})
	if got != nil {
		t.Errorf("a cap with no counter and a counter with no cap measure nothing, got %+v", got)
	}
	if got := Windows("nanogpt", Snapshot{Payload: []byte(`{"limits":{"weeklyInputTokens":0},"weeklyInputTokens":{"used":5}}`)}); got != nil {
		t.Errorf("a zero cap measures nothing, got %+v", got)
	}
}

func TestAssess_NanoGPT_SpentWeeklyPinsToItsReset(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	got := Assess("nanogpt", Snapshot{Kind: "usage", Payload: nanoGPTPayload(100, reset.Unix(), 1, 0, false)})

	if !got.OK || !got.Exhausted || !got.ResetsAt.Equal(reset) {
		t.Errorf("got %+v, want exhausted pinned to %v", got, reset)
	}
}

func TestAssess_NanoGPT_EarliestSpentCounterWins(t *testing.T) {
	weekly := time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)
	daily := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	got := Assess("nanogpt", Snapshot{Kind: "usage", Payload: nanoGPTPayload(100, weekly.Unix(), 10, daily.Unix(), false)})

	if !got.Exhausted || !got.ResetsAt.Equal(daily) {
		t.Errorf("got %+v, want the daily reset %v", got, daily)
	}
}

func TestAssess_NanoGPT_OveragePlanIsNeverExhausted(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).Unix()
	got := Assess("nanogpt", Snapshot{Kind: "usage", Payload: nanoGPTPayload(150, reset, 15, reset, true)})

	if !got.OK || got.Exhausted {
		t.Errorf("got %+v, want OK and not exhausted: an overage plan keeps serving", got)
	}
}

func TestAssess_NanoGPT_HealthyCountersAreNotExhausted(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).Unix()
	got := Assess("nanogpt", Snapshot{Kind: "usage", Payload: nanoGPTPayload(99, reset, 9, reset, false)})

	if !got.OK || got.Exhausted {
		t.Errorf("got %+v, want OK and not exhausted", got)
	}
}

func TestAssess_NanoGPT_SpentButUndatedIsNoOpinion(t *testing.T) {
	got := Assess("nanogpt", Snapshot{Kind: "usage", Payload: nanoGPTPayload(100, 0, 0, 0, false)})

	if got.OK || got.Exhausted {
		t.Errorf("got %+v, want no opinion: spent with no reset must neither pin nor release", got)
	}
}

func TestAssess_NanoGPT_PastResetIsNoPin(t *testing.T) {
	got := Assess("nanogpt", Snapshot{Kind: "usage", Payload: nanoGPTPayload(100, time.Now().Add(-time.Hour).Unix(), 0, 0, false)})

	if !got.OK || got.Exhausted {
		t.Errorf("got %+v, want OK and not exhausted: the window has rolled over", got)
	}
}

func TestAssessWithReserve_NanoGPT_PinsAtTheLineEvenOnOverage(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	for _, overage := range []bool{false, true} {
		snap := Snapshot{Kind: "usage", Payload: nanoGPTPayload(90, reset.Unix(), 1, 0, overage)}

		if got := AssessWithReserve("nanogpt", snap, 0, time.Now()); got.Exhausted {
			t.Errorf("overage=%t: no reserve drains fully, got %+v", overage, got)
		}
		got := AssessWithReserve("nanogpt", snap, 0.1, time.Now())
		if !got.Exhausted || !got.ResetsAt.Equal(reset) {
			t.Errorf("overage=%t: 90%% used against a 10%% reserve must pin to %v, got %+v", overage, reset, got)
		}
		under := Snapshot{Kind: "usage", Payload: nanoGPTPayload(80, reset.Unix(), 1, 0, overage)}
		if got := AssessWithReserve("nanogpt", under, 0.1, time.Now()); got.Exhausted {
			t.Errorf("overage=%t: 80%% used is under a 10%% reserve line, got %+v", overage, got)
		}
	}
}

// TestAssessWithReserve_NanoGPT_OverageSpentDailyStillPins: the reserve line
// sits on the weekly counter, but a plan serving into overage never reads as
// exhausted on its own, so a daily counter at its cap must still pin under a
// reserve, and as exhaustion rather than a reserve verdict: the reserve is
// not what stopped it.
func TestAssessWithReserve_NanoGPT_OverageSpentDailyStillPins(t *testing.T) {
	weekly := time.Now().Add(5 * 24 * time.Hour).Truncate(time.Second)
	daily := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	payload := nanoGPTPayload(50, weekly.Unix(), 10, daily.Unix(), true)
	snap := Snapshot{Kind: "usage", Payload: payload}
	if got := AssessWithReserve("nanogpt", snap, 0, time.Now()); got.Exhausted {
		t.Errorf("no reserve on an overage plan drains fully, got %+v", got)
	}
	got := AssessWithReserve("nanogpt", snap, 0.1, time.Now())
	if !got.Exhausted || got.Reserved || !got.ResetsAt.Equal(daily) {
		t.Errorf("daily spent under a reserve: got %+v, want exhausted (not reserved) until %v", got, daily)
	}
}
