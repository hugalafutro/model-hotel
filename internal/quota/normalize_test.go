package quota

import (
	"encoding/json"
	"strconv"
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
		{"balance type is out of scope", "deepseek", Snapshot{Kind: "balance", Payload: []byte(`{}`)}},
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
