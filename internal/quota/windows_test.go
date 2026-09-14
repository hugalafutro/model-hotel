package quota

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/provider"
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
		{"type": "TIME_LIMIT", "unit": 5, "percentage": 10, "remaining": 900},
		{"type": "TIME_LIMIT", "unit": 3, "percentage": 10},
	}}})

	ws := Windows("zai-coding", Snapshot{Payload: payload})

	if len(ws) != 2 {
		t.Fatalf("got %d windows, want 5h and mcp (weekly states no percentage, a TIME_LIMIT on unit 3 is not a window the modal shows): %+v", len(ws), ws)
	}
	w := window(t, ws, "5h")
	if !near(w.Used, 0.375) || !w.ResetsAt.Equal(reset) {
		t.Errorf("got used=%v resets=%v, want 0.375 at %v", w.Used, w.ResetsAt, reset)
	}
	if m := window(t, ws, "mcp"); !near(m.Used, 0.1) {
		t.Errorf("mcp: got used=%v, want 0.1", m.Used)
	}
}

func TestWindows_KimiCode_NamesSpansAndDerivesShareFromEitherPair(t *testing.T) {
	payload := []byte(`{
		"usage": {"limit": "1000", "used": "250", "resetTime": "2026-07-19T17:10:02Z"},
		"limits": [
			{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "100", "remaining": "42"}},
			{"window": {"duration": 7, "timeUnit": "DAY"}, "detail": {"limit": "0", "used": "5"}},
			{"window": {"duration": 90, "timeUnit": "MINUTE"}, "detail": {"limit": "10"}},
			{"window": {"duration": 1, "timeUnit": "DAY"}, "detail": {"limit": "100", "used": "10", "remaining": "20"}}
		]}`)

	ws := Windows("kimi-code", Snapshot{Payload: payload})

	if len(ws) != 3 {
		t.Fatalf("got %d windows, want weekly, 5h and 1d (a zero limit and a window stating neither used nor remaining are unreadable): %+v", len(ws), ws)
	}
	// Both fields stated and disagreeing: remaining decides, as it does for the
	// assessor, so the two never read one payload differently.
	if d := window(t, ws, "1d"); !near(d.Used, 0.8) {
		t.Errorf("1d: got used=%v, want 0.8 from remaining, not 0.1 from used", d.Used)
	}
	if c := window(t, ws, "weekly"); !near(c.Used, 0.25) || c.ResetsAt.IsZero() {
		t.Errorf("weekly (the top-level usage block): got %+v, want used 0.25 with a dated reset", c)
	}
	if h := window(t, ws, "5h"); !near(h.Used, 0.58) || !h.ResetsAt.IsZero() {
		t.Errorf("5h: got %+v, want used 0.58 from remaining and no reset", h)
	}
}

func TestWindows_OpenCodeGo_SkipsWindowsThePayloadDoesNotCarry(t *testing.T) {
	payload := []byte(`{"usage": {
		"rolling": {"status": "ok", "percent": 80, "resetsAt": "2026-07-19T17:10:02Z"},
		"weekly": {"status": "exceeded", "resetsAt": "bad"}}}`)

	ws := Windows("opencode-go", Snapshot{Payload: payload})

	if len(ws) != 2 {
		t.Fatalf("got %d windows, want rolling and weekly (monthly is absent, not untouched): %+v", len(ws), ws)
	}
	if r := window(t, ws, "rolling"); !near(r.Used, 0.8) || r.ResetsAt.IsZero() {
		t.Errorf("rolling: got %+v", r)
	}
	if w := window(t, ws, "weekly"); !near(w.Used, 1) || !w.ResetsAt.IsZero() {
		t.Errorf("weekly: got %+v, want spent on the refused status alone, with an undatable reset left zero", w)
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

func TestWindows_ZaiCoding_WeeklyNamedAndOtherUnitsSkipped(t *testing.T) {
	payload := []byte(`{"data":{"limits":[
		{"type":"TOKENS_LIMIT","unit":6,"percentage":50},
		{"type":"TOKENS_LIMIT","unit":9,"percentage":50}]}}`)
	ws := Windows("zai-coding", Snapshot{Payload: payload})
	if len(ws) != 1 || ws[0].Name != "weekly" || !near(ws[0].Used, 0.5) || !ws[0].ResetsAt.IsZero() {
		t.Errorf("got %+v, want one weekly window at 0.5 with no reset", ws)
	}
}

func TestKimiWindowName(t *testing.T) {
	for _, tc := range []struct {
		dur  int
		unit string
		want string
	}{
		{300, "TIME_UNIT_MINUTE", "5h"},
		{90, "MINUTE", "90m"},
		{3, "TIME_UNIT_HOUR", "3h"},
		{7, "DAY", "7d"},
		{2, "WEEK", "2w"},
		{4, "FORTNIGHT", "4 fortnight"},
	} {
		if got := kimiWindowName(provider.KimiCodeQuotaWindow{Duration: tc.dur, TimeUnit: tc.unit}); got != tc.want {
			t.Errorf("%d %s: got %q, want %q", tc.dur, tc.unit, got, tc.want)
		}
	}
}

func TestWindows_MiniMax_ActiveWindowWithNoFigureIsSkipped(t *testing.T) {
	payload := []byte(`{"base_resp":{"status_code":0},"model_remains":[
		{"model_name":"M2","current_interval_status":1,"current_interval_total_count":0}]}`)
	if got := Windows("minimax", Snapshot{Payload: payload}); got != nil {
		t.Errorf("no counts and no percent is nothing to report, got %+v", got)
	}
}

func TestWindows_GarbagePayloadReportsNothingForEveryType(t *testing.T) {
	for _, typ := range []string{"zai-coding", "kimi-code", "minimax", "neuralwatt", "opencode-go"} {
		if got := Windows(typ, Snapshot{Payload: []byte(`[1,2`)}); got != nil {
			t.Errorf("%s: got %+v from unparseable JSON", typ, got)
		}
	}
}

func TestWindows_ZaiCoding_PercentageAbove100IsNonsense(t *testing.T) {
	payload := []byte(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":5000}]}}`)
	if got := Windows("zai-coding", Snapshot{Payload: payload}); got != nil {
		t.Errorf("Z.ai has no overage mode, a percentage past 100 is junk, got %+v", got)
	}
	if got := Windows("zai-coding", Snapshot{Payload: []byte(`null`)}); got != nil {
		t.Errorf("a 204 row stores null and states no window, got %+v", got)
	}
}
func TestAssessWithReserve(t *testing.T) {
	reset := time.Now().Add(3 * time.Hour).Truncate(time.Millisecond)
	payload := func(pct float64) []byte {
		b, _ := json.Marshal(map[string]any{"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "percentage": pct, "nextResetTime": reset.UnixMilli()},
		}}})
		return b
	}
	now := time.Now()

	// Below the line: the assessor's verdict stands.
	if a := AssessWithReserve("zai-coding", Snapshot{Payload: payload(85)}, 0.1, now); !a.OK || a.Exhausted {
		t.Errorf("85%% used with 10%% reserve: got %+v, want healthy", a)
	}
	// On the line: pinned to the window's own reset.
	if a := AssessWithReserve("zai-coding", Snapshot{Payload: payload(90)}, 0.1, now); !a.OK || !a.Exhausted || !a.ResetsAt.Equal(reset) {
		t.Errorf("90%% used with 10%% reserve: got %+v, want exhausted until %v", a, reset)
	}
	// Exactly on a line float64 renders a hair apart (30/100 is 0.3, 1 - 0.7
	// is 0.30000000000000004): still pinned.
	if a := AssessWithReserve("zai-coding", Snapshot{Payload: payload(30)}, 0.7, now); !a.Exhausted {
		t.Errorf("30%% used with 70%% reserve: got %+v, want exhausted", a)
	}
	// No reserve: 90% is just usage.
	if a := AssessWithReserve("zai-coding", Snapshot{Payload: payload(90)}, 0, now); a.Exhausted {
		t.Errorf("no reserve: got %+v, want healthy", a)
	}
	// Spent outright: the assessor's own verdict, reserve or not.
	if a := AssessWithReserve("zai-coding", Snapshot{Payload: payload(100)}, 0.5, now); !a.Exhausted {
		t.Errorf("100%% used: got %+v, want exhausted", a)
	}
	// Unreadable stays unreadable: a reserve never invents an opinion.
	if a := AssessWithReserve("zai-coding", Snapshot{Payload: []byte(`null`)}, 0.5, now); a.OK {
		t.Errorf("null payload: got %+v, want no opinion", a)
	}
	// An undated window past the line places no pin.
	undated := []byte(`{"balance":{"total_credits_usd":10,"credits_used_usd":9.5},"subscription":{}}`)
	if a := AssessWithReserve("neuralwatt", Snapshot{Payload: undated}, 0.1, now); !a.OK || a.Exhausted {
		t.Errorf("undated credits past the line: got %+v, want the assessor's healthy verdict", a)
	}
	// A window the assessor never judges alone does not pin under a reserve
	// either: Z.ai's MCP calls at 95% with a 10% reserve leave the tokens alone.
	mcp, _ := json.Marshal(map[string]any{"data": map[string]any{"limits": []map[string]any{
		{"type": "TIME_LIMIT", "unit": 5, "percentage": 95, "nextResetTime": reset.UnixMilli()},
		{"type": "TOKENS_LIMIT", "unit": 3, "percentage": 10, "nextResetTime": reset.UnixMilli()},
	}}})
	if a := AssessWithReserve("zai-coding", Snapshot{Payload: mcp}, 0.1, now); !a.OK || a.Exhausted {
		t.Errorf("mcp window past the line: got %+v, want healthy", a)
	}
	// NeuralWatt's energy at 95% with a 10% reserve: the account still serves
	// into overage, so no pin.
	energy := []byte(`{"balance":{"total_credits_usd":10,"credits_used_usd":1},"subscription":{"kwh_included":4,"kwh_used":3.8,"current_period_end":"2030-01-01T00:00:00Z"}}`)
	if a := AssessWithReserve("neuralwatt", Snapshot{Payload: energy}, 0.1, now); !a.OK || a.Exhausted {
		t.Errorf("energy past the line: got %+v, want healthy", a)
	}
}
