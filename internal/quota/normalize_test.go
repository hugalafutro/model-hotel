package quota

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestEpochToTime_DetectsUnits(t *testing.T) {
	// 1e11 seconds is year 5138; 1e11 ms is year 1973. Every realistic reset
	// timestamp is below the boundary in seconds and above it in milliseconds.
	got, ok := epochToTime(1_800_000_000)
	if !ok || got.Year() != 2027 {
		t.Fatalf("seconds: got %v ok=%v, want year 2027", got, ok)
	}
	got, ok = epochToTime(1_800_000_000_000)
	if !ok || got.Year() != 2027 {
		t.Fatalf("millis: got %v ok=%v, want year 2027", got, ok)
	}
	if _, ok := epochToTime(0); ok {
		t.Error("zero epoch must not be accepted")
	}
	if _, ok := epochToTime(-5); ok {
		t.Error("negative epoch must not be accepted")
	}
}

func TestParseResetString(t *testing.T) {
	// Not exercised via Assess in this task (zai-coding sends numeric epochs
	// only) but declared as a shared helper for the string-encoded parsers
	// (Kimi's resetTime, Task 2) that land later. Covered directly here so it
	// isn't dead code and its contract is pinned before anything depends on it.
	rfc3339 := time.Now().Add(time.Hour).Truncate(time.Second)
	got, ok := parseResetString(rfc3339.Format(time.RFC3339))
	if !ok || !got.Equal(rfc3339.UTC()) {
		t.Fatalf("rfc3339: got %v ok=%v, want %v", got, ok, rfc3339.UTC())
	}

	epochMillis := time.Now().Add(time.Hour).UnixMilli()
	got, ok = parseResetString(strconv.FormatInt(epochMillis, 10))
	if !ok || got.UnixMilli() != epochMillis {
		t.Fatalf("epoch-as-string: got %v ok=%v, want millis %d", got, ok, epochMillis)
	}

	if _, ok := parseResetString("   "); ok {
		t.Error("blank string must not be accepted")
	}
	if _, ok := parseResetString("not-a-timestamp"); ok {
		t.Error("garbage string must not be accepted")
	}
}

func TestAssess_ZaiCoding_ExhaustedPinsEarliestReset(t *testing.T) {
	// Epoch milliseconds: the encoding Z.ai actually sends.
	fiveHour := time.Now().Add(2 * time.Hour).UnixMilli()
	weekly := time.Now().Add(72 * time.Hour).UnixMilli()
	payload, err := json.Marshal(map[string]any{
		"code":    200,
		"success": true,
		"data": map[string]any{
			"level": "pro",
			"limits": []map[string]any{
				{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": fiveHour},
				{"type": "TOKENS_LIMIT", "unit": 6, "remaining": 0, "nextResetTime": weekly},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("zai-coding", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK || !got.Exhausted {
		t.Fatalf("got OK=%v Exhausted=%v, want both true", got.OK, got.Exhausted)
	}
	if got.ResetsAt.UnixMilli() != fiveHour {
		t.Errorf("got ResetsAt=%d, want earliest exhausted window %d", got.ResetsAt.UnixMilli(), fiveHour)
	}
}

func TestAssess_ZaiCoding_HealthyWindowNotExhausted(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 5000, "nextResetTime": time.Now().Add(time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("zai-coding", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("remaining>0 must not be exhausted")
	}
}

// TestAssess_ZaiCoding_AbsentRemainingIsNotExhausted is the same defect the
// MiniMax parser was fixed for: a field that is simply not in the payload must
// never be read as "0 left". The fixture omits `remaining` entirely (it is not
// set to 0), pairing it with a future nextResetTime so a parser that misread the
// absence would pin this healthy provider for up to the 24h ceiling.
func TestAssess_ZaiCoding_AbsentRemainingIsNotExhausted(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "nextResetTime": time.Now().Add(6 * time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), "remaining") {
		t.Fatalf("fixture must omit the remaining key entirely, got %s", payload)
	}

	got := Assess("zai-coding", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("an absent remaining must never be read as a spent window")
	}
}

// TestAssess_ZaiCoding_ExplicitZeroRemainingIsExhausted is the other half of the
// absent-vs-zero distinction: making absence safe must not make a genuinely
// spent window unreadable, or the feature stops working entirely.
func TestAssess_ZaiCoding_ExplicitZeroRemainingIsExhausted(t *testing.T) {
	reset := time.Now().Add(6 * time.Hour).UnixMilli()
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": reset},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("zai-coding", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK || !got.Exhausted {
		t.Fatalf("got OK=%v Exhausted=%v, want an explicit zero to read as exhausted", got.OK, got.Exhausted)
	}
	if got.ResetsAt.UnixMilli() != reset {
		t.Errorf("got ResetsAt=%d, want %d", got.ResetsAt.UnixMilli(), reset)
	}
}

// TestAssess_ZaiCoding_PercentageBelow100OverridesZeroRemaining pins the shape
// the live API actually sends: remaining is a constant 0 even on a window that
// is only partially used, while percentage (percent consumed) matches the
// account page. A parser that believes remaining=0 here pins a healthy
// provider shut for the rest of its window.
func TestAssess_ZaiCoding_PercentageBelow100OverridesZeroRemaining(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "percentage": 63, "nextResetTime": time.Now().Add(4 * time.Hour).UnixMilli()},
			{"type": "TOKENS_LIMIT", "unit": 6, "remaining": 0, "percentage": 48, "nextResetTime": time.Now().Add(72 * time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("zai-coding", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("percentage<100 must not be exhausted, whatever remaining says")
	}
}

func TestAssess_ZaiCoding_PercentageAt100IsExhausted(t *testing.T) {
	reset := time.Now().Add(3 * time.Hour).UnixMilli()
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "percentage": 100, "nextResetTime": reset},
			{"type": "TOKENS_LIMIT", "unit": 6, "remaining": 0, "percentage": 48, "nextResetTime": time.Now().Add(72 * time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("zai-coding", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK || !got.Exhausted {
		t.Fatalf("got OK=%v Exhausted=%v, want a 100%% window to read as exhausted", got.OK, got.Exhausted)
	}
	if got.ResetsAt.UnixMilli() != reset {
		t.Errorf("got ResetsAt=%d, want the spent window's reset %d, not the healthy one's", got.ResetsAt.UnixMilli(), reset)
	}
}

// TestAssess_ZaiCoding_NonsensePercentageFallsBackToRemaining: a percentage
// outside [0, 100] is not a signal, so the remaining rule decides as it did
// before the field existed. For an over-100 overage reading that fallback
// still pins, because Z.ai always sends remaining: 0 alongside it.
func TestAssess_ZaiCoding_NonsensePercentageFallsBackToRemaining(t *testing.T) {
	for _, pct := range []float64{-7, 150} {
		reset := time.Now().Add(2 * time.Hour).UnixMilli()
		payload, err := json.Marshal(map[string]any{
			"data": map[string]any{"limits": []map[string]any{
				{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "percentage": pct, "nextResetTime": reset},
			}},
		})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		got := Assess("zai-coding", Snapshot{Kind: "usage", Payload: payload})

		if !got.OK || !got.Exhausted {
			t.Fatalf("percentage=%v: got OK=%v Exhausted=%v, want remaining=0 to decide when percentage is out of range", pct, got.OK, got.Exhausted)
		}
	}
}

// NeuralWatt is a balance model, not a window model: exhausting the included
// monthly energy does not make it refuse requests — it keeps serving in
// overage, debiting the credit balance. Only when BOTH the included energy and
// the credits are affirmatively spent do requests actually start failing, and
// the only scheduled recovery is the billing period end. The tests below pin
// that two-sided rule.
func TestAssess_Neuralwatt_EnergyAndCreditsSpentPinsToPeriodEnd(t *testing.T) {
	periodEnd := time.Now().Add(20 * 24 * time.Hour).Truncate(time.Second)
	payload, err := json.Marshal(map[string]any{
		"balance":      map[string]any{"credits_remaining_usd": 0},
		"subscription": map[string]any{"status": "active", "kwh_remaining": 0, "in_overage": true, "current_period_end": periodEnd.Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("neuralwatt", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK || !got.Exhausted {
		t.Fatalf("got OK=%v Exhausted=%v, want both true", got.OK, got.Exhausted)
	}
	if !got.ResetsAt.Equal(periodEnd.UTC()) {
		t.Errorf("got ResetsAt=%v, want period end %v", got.ResetsAt, periodEnd.UTC())
	}
}

// TestAssess_Neuralwatt_SubCentResidueIsSpent pins the real reading taken from
// prod on 2026-09-01 at the moment NeuralWatt began answering 402
// payment_required: kwh_remaining 0 and credits_remaining_usd 0.0035. The
// account was affirmatively blocked ("a usage or billing limit was previously
// reached") while a third of a cent still sat in the balance, so an exact
// <= 0 test never fires in practice and the provider stayed unpinned and
// live in its failover group.
func TestAssess_Neuralwatt_SubCentResidueIsSpent(t *testing.T) {
	periodEnd := time.Now().Add(26 * 24 * time.Hour).Truncate(time.Second)
	payload, err := json.Marshal(map[string]any{
		"balance":      map[string]any{"credits_remaining_usd": 0.0035},
		"subscription": map[string]any{"status": "active", "kwh_remaining": 0, "in_overage": true, "current_period_end": periodEnd.Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("neuralwatt", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK || !got.Exhausted {
		t.Fatalf("got OK=%v Exhausted=%v, want both true", got.OK, got.Exhausted)
	}
	if !got.ResetsAt.Equal(periodEnd.UTC()) {
		t.Errorf("got ResetsAt=%v, want period end %v", got.ResetsAt, periodEnd.UTC())
	}
}

// TestAssess_Neuralwatt_OneCentIsNotSpent holds the other side of the floor: a
// balance that can still buy a request must not be read as spent, or the
// residue rule would sideline a provider that is still serving.
func TestAssess_Neuralwatt_OneCentIsNotSpent(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"balance":      map[string]any{"credits_remaining_usd": 0.01},
		"subscription": map[string]any{"status": "active", "kwh_remaining": 0, "current_period_end": time.Now().Add(time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("neuralwatt", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("a full cent of credit must not read as spent")
	}
}

func TestAssess_Neuralwatt_CreditsRemainingIsNotExhausted(t *testing.T) {
	// Included energy spent but credits cover overage: the provider keeps
	// serving, so pinning here would sideline a working provider.
	payload, err := json.Marshal(map[string]any{
		"balance":      map[string]any{"credits_remaining_usd": 28.9},
		"subscription": map[string]any{"status": "active", "kwh_remaining": 0, "current_period_end": time.Now().Add(time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("neuralwatt", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("credits remaining must not read as exhausted: overage keeps serving")
	}
}

func TestAssess_Neuralwatt_EnergyRemainingIsNotExhausted(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"balance":      map[string]any{"credits_remaining_usd": 0},
		"subscription": map[string]any{"status": "active", "kwh_remaining": 1.92, "current_period_end": time.Now().Add(time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("neuralwatt", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("included energy remaining must not read as exhausted")
	}
}

// TestAssess_Neuralwatt_AbsentFieldsAreNotExhausted: the same absent-vs-zero
// contract every parser in this file honors. A payload missing either decisive
// field (free tier, older API shape) must fail open, never pin.
func TestAssess_Neuralwatt_AbsentFieldsAreNotExhausted(t *testing.T) {
	cases := []map[string]any{
		{"subscription": map[string]any{"status": "active", "kwh_remaining": 0, "current_period_end": time.Now().Add(time.Hour).Format(time.RFC3339)}},
		{"balance": map[string]any{"credits_remaining_usd": 0}},
		{},
	}
	for i, c := range cases {
		payload, err := json.Marshal(c)
		if err != nil {
			t.Fatalf("case %d marshal: %v", i, err)
		}
		got := Assess("neuralwatt", Snapshot{Kind: "usage", Payload: payload})
		if !got.OK {
			t.Fatalf("case %d: a well-formed payload must assess OK", i)
		}
		if got.Exhausted {
			t.Errorf("case %d: absent decisive fields must never read as exhausted", i)
		}
	}
}

func TestAssess_Neuralwatt_UnparseablePayloadIsNotOK(t *testing.T) {
	// A payload that does not decode must report OK=false so the caller falls
	// back to the default cooldown, never a guessed pin.
	got := Assess("neuralwatt", Snapshot{Kind: "usage", Payload: []byte(`{"subscription": "not-an-object"}`)})
	if got.OK {
		t.Error("an undecodable payload must not assess OK")
	}
	if got.Exhausted {
		t.Error("an undecodable payload must never read as exhausted")
	}
}

func TestAssess_Neuralwatt_PastPeriodEndIsNoPin(t *testing.T) {
	// A stale snapshot whose period already rolled over must not pin: the
	// window it describes is gone.
	payload, err := json.Marshal(map[string]any{
		"balance":      map[string]any{"credits_remaining_usd": 0},
		"subscription": map[string]any{"status": "active", "kwh_remaining": 0, "current_period_end": time.Now().Add(-time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("neuralwatt", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("a period that already ended must not pin")
	}
}

// TestAssess_KimiCode_AbsentRemainingIsNotExhausted guards the same hole in the
// Kimi parser. A block that carries neither remaining nor used says nothing
// about consumption, whatever its limit reads, so it must not pin. Pinning that
// behaviour here means a future switch to a numeric field (which is exactly the
// kind of reshape the drift watch exists for) fails this test rather than
// silently sidelining a healthy provider.
func TestAssess_KimiCode_AbsentRemainingIsNotExhausted(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"limits": []map[string]any{{
			"window": map[string]any{"duration": 300, "timeUnit": "MINUTE"},
			"detail": map[string]any{"limit": "1000", "resetTime": time.Now().Add(6 * time.Hour).UTC().Format(time.RFC3339)},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(payload), "remaining") {
		t.Fatalf("fixture must omit the remaining key entirely, got %s", payload)
	}

	got := Assess("kimi-code", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("an absent remaining must never be read as a spent window")
	}
}

func TestAssess_FailOpenCases(t *testing.T) {
	past, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": time.Now().Add(-time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	cases := []struct {
		name     string
		provider string
		snap     Snapshot
	}{
		{"unknown provider type", "openai", Snapshot{Kind: "usage", Payload: []byte(`{}`)}},
		{"balance-type provider has no parser", "deepseek", Snapshot{Kind: "balance", Payload: []byte(`{}`)}},
		{"malformed json", "zai-coding", Snapshot{Kind: "usage", Payload: []byte(`{"data":`)}},
		{"nil payload", "zai-coding", Snapshot{Kind: "usage", Payload: nil}},
		{"no limits", "zai-coding", Snapshot{Kind: "usage", Payload: []byte(`{"data":{"limits":[]}}`)}},
		{"reset already past", "zai-coding", Snapshot{Kind: "usage", Payload: past}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Assess(tc.provider, tc.snap)
			if got.OK && got.Exhausted {
				t.Errorf("got OK=%v Exhausted=%v, want no usable pin", got.OK, got.Exhausted)
			}
		})
	}
}

// TestAssess_ZaiCoding_UnwatchedTypeOrUnitIsIgnored verifies the parser only
// judges the two token windows the dashboard reads. Z.ai sends other limit
// entries in the same array — a spend cap and windows on units the failover
// circuit knows nothing about — and a spent one of those says nothing about
// whether a request would succeed. Every entry here is spent with a future
// reset, so a parser that stopped filtering would pin this provider shut.
func TestAssess_ZaiCoding_UnwatchedTypeOrUnitIsIgnored(t *testing.T) {
	reset := time.Now().Add(6 * time.Hour).UnixMilli()
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "COST_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": reset},
			{"type": "TOKENS_LIMIT", "unit": 1, "remaining": 0, "nextResetTime": reset},
			{"type": "TOKENS_LIMIT", "unit": 9, "remaining": 0, "nextResetTime": reset},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("zai-coding", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("a well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("only TOKENS_LIMIT on unit 3 or 6 may pin; a spent entry of any other kind must be ignored")
	}
}

// TestAssess_MalformedBodyFailsOpenForEveryParser verifies every provider
// parser refuses a body it cannot decode (OK=false) rather than reading it as
// evidence. A truncated body is what an upstream incident or a proxy actually
// produces, and OK=false is what makes the caller keep its normal cooldown. The
// zai case was covered from the start; asserting all three together means a
// parser that dropped the check would be caught rather than inheriting the
// coverage of its neighbours.
func TestAssess_MalformedBodyFailsOpenForEveryParser(t *testing.T) {
	for _, providerType := range []string{"zai-coding", "kimi-code", "minimax"} {
		t.Run(providerType, func(t *testing.T) {
			got := Assess(providerType, Snapshot{Kind: "usage", Payload: []byte(`{"limits":`)})

			if got.OK {
				t.Errorf("a truncated body must not be reported as understood, got %+v", got)
			}
			if got.Exhausted {
				t.Error("a truncated body must never mark a window spent")
			}
		})
	}
}

func TestAssess_KimiCode_RFC3339AndStringNumbers(t *testing.T) {
	fiveHour := time.Now().Add(90 * time.Minute).UTC().Format(time.RFC3339)
	weekly := time.Now().Add(100 * time.Hour).UTC().Format(time.RFC3339)
	payload, err := json.Marshal(map[string]any{
		"limits": []map[string]any{
			{
				"window": map[string]any{"duration": 300, "timeUnit": "MINUTE"},
				"detail": map[string]any{"limit": "1000", "remaining": "0", "resetTime": fiveHour},
			},
			{
				"window": map[string]any{"duration": 7, "timeUnit": "DAY"},
				"detail": map[string]any{"limit": "50000", "remaining": "0", "resetTime": weekly},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("kimi-code", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK || !got.Exhausted {
		t.Fatalf("got OK=%v Exhausted=%v, want both true", got.OK, got.Exhausted)
	}
	if got.ResetsAt.Format(time.RFC3339) != fiveHour {
		t.Errorf("got ResetsAt=%s, want earliest %s", got.ResetsAt.Format(time.RFC3339), fiveHour)
	}
}

func TestAssess_KimiCode_RemainingIsParsedNotCompared(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"limits": []map[string]any{{
			"window": map[string]any{"duration": 300, "timeUnit": "MINUTE"},
			"detail": map[string]any{"limit": "1000", "remaining": "250", "resetTime": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("kimi-code", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error(`remaining "250" must not be treated as exhausted`)
	}
}

func TestAssess_KimiCode_UnparseableRemainingIsNotExhausted(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"limits": []map[string]any{{
			"detail": map[string]any{"limit": "1000", "remaining": "n/a", "resetTime": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("kimi-code", Snapshot{Kind: "usage", Payload: payload})

	if got.Exhausted {
		t.Error("an unparseable remaining must never be read as exhausted")
	}
}

func TestAssess_KimiCode_EpochStringResetTime(t *testing.T) {
	reset := time.Now().Add(3 * time.Hour).Unix()
	payload, err := json.Marshal(map[string]any{
		"limits": []map[string]any{{
			"detail": map[string]any{"limit": "1000", "remaining": "0", "resetTime": strconv.FormatInt(reset, 10)},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("kimi-code", Snapshot{Kind: "usage", Payload: payload})

	if !got.Exhausted || got.ResetsAt.Unix() != reset {
		t.Errorf("got Exhausted=%v ResetsAt=%d, want true/%d", got.Exhausted, got.ResetsAt.Unix(), reset)
	}
}

// kimiEnvelope is the live Kimi Code /usages response (prod fetch 2026-08-07)
// with the two blocks under test substituted in: the top-level cycle counter
// and the rolling window's detail. Everything around them is the payload
// verbatim, so a decode regression in the envelope surfaces here too.
const kimiEnvelope = `{
	"user": {"userId": "d9ebt1t22qd2lajpoiig", "region": "REGION_OVERSEA", "membership": {"level": "LEVEL_BASIC"}},
	"limited": true,
	"usage": %s,
	"limits": [
		{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"},
		 "detail": %s}
	],
	"parallel": {"limit": "10"},
	"totalQuota": {},
	"subType": "TYPE_PURCHASE"
}`

// TestAssess_KimiCode_Proto3OmitsZeroValuedFields covers the shape Kimi
// actually sends. /usages is proto3 JSON and drops zero-valued fields, so an
// exhausted window carries used="100" and no remaining at all, while a fresh
// window carries remaining and no used. Reading remaining alone therefore goes
// blind at exactly the moment the pin exists for. Reset timestamps are relative
// to now because a reset in the past never pins.
func TestAssess_KimiCode_Proto3OmitsZeroValuedFields(t *testing.T) {
	usageReset := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	windowReset := time.Now().Add(90 * time.Minute).UTC().Format(time.RFC3339)
	healthyUsage := fmt.Sprintf(`{"limit": "100", "used": "23", "remaining": "77", "resetTime": %q}`, usageReset)
	healthyWindow := fmt.Sprintf(`{"limit": "100", "used": "23", "remaining": "77", "resetTime": %q}`, windowReset)

	cases := []struct {
		name          string
		usage         string
		detail        string
		wantExhausted bool
		wantReset     string
	}{
		{
			name:          "spent window omits remaining and reports used",
			usage:         healthyUsage,
			detail:        fmt.Sprintf(`{"limit": "100", "used": "100", "resetTime": %q}`, windowReset),
			wantExhausted: true,
			wantReset:     windowReset,
		},
		{
			name:   "healthy window reports remaining",
			usage:  healthyUsage,
			detail: healthyWindow,
		},
		{
			name:   "untouched window omits used",
			usage:  healthyUsage,
			detail: fmt.Sprintf(`{"limit": "100", "remaining": "100", "resetTime": %q}`, windowReset),
		},
		{
			name:   "neither remaining nor used parses",
			usage:  healthyUsage,
			detail: fmt.Sprintf(`{"limit": "100", "remaining": "", "used": "", "resetTime": %q}`, windowReset),
		},
		{
			name:          "spent cycle counter pins while the window is healthy",
			usage:         fmt.Sprintf(`{"limit": "100", "used": "100", "resetTime": %q}`, usageReset),
			detail:        healthyWindow,
			wantExhausted: true,
			wantReset:     usageReset,
		},
		{
			name:   "explicit remaining wins over the used derivation",
			usage:  healthyUsage,
			detail: fmt.Sprintf(`{"limit": "100", "used": "100", "remaining": "5", "resetTime": %q}`, windowReset),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			payload := fmt.Sprintf(kimiEnvelope, tc.usage, tc.detail)

			got := Assess("kimi-code", Snapshot{Kind: "usage", Payload: json.RawMessage(payload)})

			if !got.OK {
				t.Fatalf("a well-formed payload must assess OK, got %+v", got)
			}
			if got.Exhausted != tc.wantExhausted {
				t.Fatalf("got Exhausted=%v, want %v", got.Exhausted, tc.wantExhausted)
			}
			if !tc.wantExhausted {
				return
			}
			if got.ResetsAt.Format(time.RFC3339) != tc.wantReset {
				t.Errorf("got ResetsAt=%s, want %s", got.ResetsAt.Format(time.RFC3339), tc.wantReset)
			}
		})
	}
}

func TestAssess_MiniMax_EarliestAcrossModelsAndWindows(t *testing.T) {
	soon := time.Now().Add(45 * time.Minute).UnixMilli()
	later := time.Now().Add(30 * time.Hour).UnixMilli()
	payload, err := json.Marshal(map[string]any{
		"model_remains": []map[string]any{
			{
				"model_name":                   "abab7",
				"end_time":                     later,
				"current_interval_status":      1,
				"current_interval_total_count": 100,
				"current_interval_usage_count": 100,
				"weekly_end_time":              later,
				"current_weekly_status":        1,
				"current_weekly_total_count":   1000,
				"current_weekly_usage_count":   10,
			},
			{
				"model_name":                   "abab6",
				"end_time":                     soon,
				"current_interval_status":      1,
				"current_interval_total_count": 100,
				"current_interval_usage_count": 100,
				"weekly_end_time":              later,
				"current_weekly_status":        1,
				"current_weekly_total_count":   1000,
				"current_weekly_usage_count":   20,
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("minimax", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK || !got.Exhausted {
		t.Fatalf("got OK=%v Exhausted=%v, want both true", got.OK, got.Exhausted)
	}
	if got.ResetsAt.UnixMilli() != soon {
		t.Errorf("got ResetsAt=%d, want earliest exhausted window %d", got.ResetsAt.UnixMilli(), soon)
	}
}

func TestAssess_MiniMax_HealthyModelNotExhausted(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"model_remains": []map[string]any{{
			"model_name":                   "abab6",
			"end_time":                     time.Now().Add(time.Hour).UnixMilli(),
			"current_interval_status":      1,
			"current_interval_total_count": 100,
			"current_interval_usage_count": 3,
			"weekly_end_time":              time.Now().Add(50 * time.Hour).UnixMilli(),
			"current_weekly_status":        1,
			"current_weekly_total_count":   1000,
			"current_weekly_usage_count":   40,
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("minimax", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("no window is spent; must not be exhausted")
	}
}

func TestAssess_MiniMax_ZeroTotalIsNotExhausted(t *testing.T) {
	// A zero total means "no limit reported", not "limit fully consumed" —
	// and on this plan tier the percent fallback is absent from the payload
	// too (never sent as an explicit 0), so the window must fail open rather
	// than be misread as 0% remaining.
	payload, err := json.Marshal(map[string]any{
		"model_remains": []map[string]any{{
			"model_name":                   "abab6",
			"end_time":                     time.Now().Add(time.Hour).UnixMilli(),
			"current_interval_status":      1,
			"current_interval_total_count": 0,
			"current_interval_usage_count": 0,
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("minimax", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("total_count=0 with no percent field present must not be read as exhausted")
	}
}

func TestAssess_MiniMax_PercentFallback_ZeroRemainingIsExhausted(t *testing.T) {
	// Some Token Plan tiers report all-zero counts even on an active window
	// (plans/already-implemented/2026-07-19-minimax-provider-research.md:58-86);
	// the remaining-percentage field is then the only real signal.
	resetAt := time.Now().Add(2 * time.Hour).UnixMilli()
	payload, err := json.Marshal(map[string]any{
		"model_remains": []map[string]any{{
			"model_name":                         "general",
			"end_time":                           resetAt,
			"current_interval_status":            1,
			"current_interval_total_count":       0,
			"current_interval_usage_count":       0,
			"current_interval_remaining_percent": 0,
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("minimax", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK || !got.Exhausted {
		t.Fatalf("got OK=%v Exhausted=%v, want both true", got.OK, got.Exhausted)
	}
	if got.ResetsAt.UnixMilli() != resetAt {
		t.Errorf("got ResetsAt=%d, want %d", got.ResetsAt.UnixMilli(), resetAt)
	}
}

func TestAssess_MiniMax_PercentFallback_FullRemainingNotExhausted(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"model_remains": []map[string]any{{
			"model_name":                         "general",
			"end_time":                           time.Now().Add(2 * time.Hour).UnixMilli(),
			"current_interval_status":            1,
			"current_interval_total_count":       0,
			"current_interval_usage_count":       0,
			"current_interval_remaining_percent": 100,
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("minimax", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("100% remaining must not be read as exhausted")
	}
}

func TestAssess_MiniMax_StatusNotActiveIsNotExhausted(t *testing.T) {
	// status 3 means the model class is not covered by the plan; the research
	// notes flag that such entries "read 100 misleadingly" on the percent
	// field — filter them out entirely rather than trust either signal.
	payload, err := json.Marshal(map[string]any{
		"model_remains": []map[string]any{{
			"model_name":                         "video",
			"end_time":                           time.Now().Add(2 * time.Hour).UnixMilli(),
			"current_interval_status":            3,
			"current_interval_total_count":       0,
			"current_interval_usage_count":       0,
			"current_interval_remaining_percent": 0,
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("minimax", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("status 3 (not covered by plan) must not be treated as exhausted")
	}
}

func TestAssess_MiniMax_CountsWinOverContradictoryPercent(t *testing.T) {
	// total > 0 means the count fields are meaningful; a contradictory
	// percent must never override them.
	payload, err := json.Marshal(map[string]any{
		"model_remains": []map[string]any{{
			"model_name":                         "abab6",
			"end_time":                           time.Now().Add(2 * time.Hour).UnixMilli(),
			"current_interval_status":            1,
			"current_interval_total_count":       100,
			"current_interval_usage_count":       50,
			"current_interval_remaining_percent": 0,
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	got := Assess("minimax", Snapshot{Kind: "usage", Payload: payload})

	if !got.OK {
		t.Fatal("well-formed payload must assess OK")
	}
	if got.Exhausted {
		t.Error("counts (50/100, not spent) must win over a contradictory 0% remaining")
	}
}
