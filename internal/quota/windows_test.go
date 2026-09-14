package quota

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func window(t *testing.T, ws []Window, name string) Window {
	t.Helper()
	for _, w := range ws {
		if w.Name == name {
			return w
		}
	}
	t.Fatalf("window %q missing from %+v", name, ws)
	return Window{}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestWindows_ZaiCoding_PercentageDecidesAndRemainingIsIgnored(t *testing.T) {
	reset := time.Now().Add(2 * time.Hour).Truncate(time.Millisecond)
	payload, _ := json.Marshal(map[string]any{"data": map[string]any{"limits": []map[string]any{
		{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "percentage": 37.5, "nextResetTime": reset.UnixMilli()},
		{"type": "TOKENS_LIMIT", "unit": 6, "remaining": 0, "nextResetTime": reset.UnixMilli()},
		{"type": "TIME_LIMIT", "unit": 3, "percentage": 10},
	}}})

	ws := Windows("zai-coding", Snapshot{Payload: payload})

	if len(ws) != 1 {
		t.Fatalf("got %d windows, want only the 5h one (weekly states no percentage, TIME_LIMIT is not a token window): %+v", len(ws), ws)
	}
	w := window(t, ws, "5h")
	if !near(w.Used, 0.375) || !w.ResetsAt.Equal(reset) {
		t.Errorf("got used=%v resets=%v, want 0.375 at %v", w.Used, w.ResetsAt, reset)
	}
}

func TestWindows_KimiCode_NamesSpansAndDerivesShareFromEitherPair(t *testing.T) {
	payload := []byte(`{
		"usage": {"limit": "1000", "used": "250", "resetTime": "2026-07-19T17:10:02Z"},
		"limits": [
			{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "100", "remaining": "42"}},
			{"window": {"duration": 7, "timeUnit": "DAY"}, "detail": {"limit": "0", "used": "5"}},
			{"window": {"duration": 90, "timeUnit": "MINUTE"}, "detail": {"limit": "10"}}
		]}`)

	ws := Windows("kimi-code", Snapshot{Payload: payload})

	if len(ws) != 2 {
		t.Fatalf("got %d windows, want cycle and 5h (a zero limit and a window stating neither used nor remaining are unreadable): %+v", len(ws), ws)
	}
	if c := window(t, ws, "cycle"); !near(c.Used, 0.25) || c.ResetsAt.IsZero() {
		t.Errorf("cycle: got %+v, want used 0.25 with a dated reset", c)
	}
	if h := window(t, ws, "5h"); !near(h.Used, 0.58) || !h.ResetsAt.IsZero() {
		t.Errorf("5h: got %+v, want used 0.58 from remaining and no reset", h)
	}
}

func TestWindows_OpenCodeGo_SkipsWindowsThePayloadDoesNotCarry(t *testing.T) {
	payload := []byte(`{"usage": {
		"rolling": {"status": "ok", "percent": 80, "resetsAt": "2026-07-19T17:10:02Z"},
		"weekly": {"status": "exceeded", "percent": 100, "resetsAt": "bad"}}}`)

	ws := Windows("opencode-go", Snapshot{Payload: payload})

	if len(ws) != 2 {
		t.Fatalf("got %d windows, want rolling and weekly (monthly is absent, not untouched): %+v", len(ws), ws)
	}
	if r := window(t, ws, "rolling"); !near(r.Used, 0.8) || r.ResetsAt.IsZero() {
		t.Errorf("rolling: got %+v", r)
	}
	if w := window(t, ws, "weekly"); !near(w.Used, 1) || !w.ResetsAt.IsZero() {
		t.Errorf("weekly: got %+v, want spent with an undatable reset left zero", w)
	}
}

func TestWindows_MiniMax_PerModelCountsThenPercent(t *testing.T) {
	end := time.Now().Add(time.Hour).Truncate(time.Second)
	payload, _ := json.Marshal(map[string]any{"base_resp": map[string]any{"status_code": 0}, "model_remains": []map[string]any{
		{"model_name": "M2", "current_interval_status": 1, "current_interval_total_count": 200, "current_interval_usage_count": 50, "end_time": end.Unix(),
			"current_weekly_status": 1, "current_weekly_total_count": 0, "current_weekly_remaining_percent": 30, "weekly_end_time": end.Unix()},
		{"model_name": "T2A", "current_interval_status": 3, "current_interval_total_count": 10, "current_interval_usage_count": 10},
	}})

	ws := Windows("minimax", Snapshot{Payload: payload})

	if len(ws) != 2 {
		t.Fatalf("got %d windows, want M2 interval and weekly only (status 3 is a class the plan does not cover): %+v", len(ws), ws)
	}
	if i := window(t, ws, "M2 interval"); !near(i.Used, 0.25) || !i.ResetsAt.Equal(end) {
		t.Errorf("interval: got %+v", i)
	}
	if w := window(t, ws, "M2 weekly"); !near(w.Used, 0.7) {
		t.Errorf("weekly: got used=%v, want 0.7 from the remaining percent", w.Used)
	}
	if got := Windows("minimax", Snapshot{Payload: []byte(`{"base_resp":{"status_code":1004},"model_remains":[]}`)}); got != nil {
		t.Errorf("a business error inside a 200 must report nothing, got %+v", got)
	}
}

func TestWindows_Neuralwatt_EnergyAndCredits(t *testing.T) {
	payload := []byte(`{"balance": {"total_credits_usd": 20, "credits_used_usd": 5},
		"subscription": {"kwh_included": 4, "kwh_used": 5, "current_period_end": "2026-08-01T00:00:00Z"}}`)

	ws := Windows("neuralwatt", Snapshot{Payload: payload})

	if e := window(t, ws, "energy"); !near(e.Used, 1.25) || e.ResetsAt.IsZero() {
		t.Errorf("energy: got %+v, want overage above 1 with the period end", e)
	}
	if c := window(t, ws, "credits"); !near(c.Used, 0.25) || !c.ResetsAt.IsZero() {
		t.Errorf("credits: got %+v, want 0.25 and no reset", c)
	}
	if got := Windows("neuralwatt", Snapshot{Payload: []byte(`{"balance":{},"subscription":{}}`)}); got != nil {
		t.Errorf("no totals means nothing to measure against, got %+v", got)
	}
}

func TestWindows_UnknownTypeOrEmptyPayloadReportsNothing(t *testing.T) {
	if got := Windows("openai", Snapshot{Payload: []byte(`{"anything": 1}`)}); got != nil {
		t.Errorf("unknown type: got %+v", got)
	}
	if got := Windows("zai-coding", Snapshot{}); got != nil {
		t.Errorf("empty payload: got %+v", got)
	}
	if got := Windows("zai-coding", Snapshot{Payload: []byte(`not json`)}); got != nil {
		t.Errorf("garbage: got %+v", got)
	}
}
