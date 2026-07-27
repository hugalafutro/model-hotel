package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/quota"
)

func TestQuotaAdvisor_ReplaceAndRead(t *testing.T) {
	adv := NewQuotaAdvisor()
	id := uuid.New()

	if _, ok := adv.ResetsAt(id); ok {
		t.Error("empty advisor must decline")
	}

	want := time.Now().Add(time.Hour).Truncate(time.Second)
	adv.Replace(map[uuid.UUID]time.Time{id: want})

	got, ok := adv.ResetsAt(id)
	if !ok {
		t.Fatal("advisor must report a stored deadline")
	}
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	if _, ok := adv.ResetsAt(uuid.New()); ok {
		t.Error("unknown provider must decline")
	}
}

func TestQuotaAdvisor_ReplaceDropsStaleEntries(t *testing.T) {
	adv := NewQuotaAdvisor()
	gone := uuid.New()
	adv.Replace(map[uuid.UUID]time.Time{gone: time.Now().Add(time.Hour)})

	adv.Replace(map[uuid.UUID]time.Time{})

	if _, ok := adv.ResetsAt(gone); ok {
		t.Error("Replace must swap the map wholesale, not merge into it")
	}
}

func TestQuotaAdvisor_NilSafeReplace(t *testing.T) {
	adv := NewQuotaAdvisor()
	adv.Replace(nil)
	if _, ok := adv.ResetsAt(uuid.New()); ok {
		t.Error("nil replace must leave the advisor empty, not panic")
	}
}

// TestBuildQuotaAdvice_DropsStaleUnassessableAndHealthy covers all three
// reasons a snapshot must NOT be advised — too old, no normalizer for the
// provider type, and (distinct from both) a provider whose quota simply
// isn't exhausted. The healthy case uses the exact same zai-coding payload
// shape as the advised one, differing only in `remaining`, so a regression
// that filtered on `a.OK` alone (ignoring `a.Exhausted`) would still pass
// every other branch here but fail this one specifically.
func TestBuildQuotaAdvice_DropsStaleUnassessableAndHealthy(t *testing.T) {
	now := time.Now()
	reset := now.Add(4 * time.Hour).UnixMilli()
	exhausted, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": reset},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	healthy, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 1000, "nextResetTime": reset},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	fresh, stale, unknownType, notExhausted := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	snaps := []quota.Snapshot{
		{ProviderID: fresh, Kind: "usage", Payload: exhausted, FetchedAt: now.Add(-time.Minute)},
		{ProviderID: stale, Kind: "usage", Payload: exhausted, FetchedAt: now.Add(-time.Hour)},
		{ProviderID: unknownType, Kind: "usage", Payload: exhausted, FetchedAt: now},
		{ProviderID: notExhausted, Kind: "usage", Payload: healthy, FetchedAt: now},
	}
	typeByID := map[uuid.UUID]string{
		fresh:        "zai-coding",
		stale:        "zai-coding",
		unknownType:  "openai",
		notExhausted: "zai-coding",
	}

	got := buildQuotaAdvice(snaps, typeByID, 15*time.Minute, now)

	if len(got) != 1 {
		t.Fatalf("got %d advised providers, want 1: %v", len(got), got)
	}
	if _, ok := got[fresh]; !ok {
		t.Error("a fresh exhausted snapshot must be advised")
	}
	if _, ok := got[stale]; ok {
		t.Error("a snapshot older than maxAge must be dropped")
	}
	if _, ok := got[unknownType]; ok {
		t.Error("a provider type with no normalizer must be dropped")
	}
	if _, ok := got[notExhausted]; ok {
		t.Error("a provider whose quota is not exhausted must be dropped")
	}
}

// TestBuildQuotaAdvice_AgeEqualToMaxAgeIsKept and
// TestBuildQuotaAdvice_AgeOneNanosecondPastMaxAgeIsDropped probe the exact
// staleness boundary (`now.Sub(FetchedAt) > maxAge`, strict greater-than) so
// an off-by-one (`>=` vs `>`) or a units mistake shows up immediately instead
// of hiding behind a coarse minute/hour gap.
func TestBuildQuotaAdvice_AgeEqualToMaxAgeIsKept(t *testing.T) {
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": now.Add(time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	id := uuid.New()
	const maxAge = 15 * time.Minute

	got := buildQuotaAdvice(
		[]quota.Snapshot{{ProviderID: id, Kind: "usage", Payload: payload, FetchedAt: now.Add(-maxAge)}},
		map[uuid.UUID]string{id: "zai-coding"},
		maxAge,
		now,
	)

	if _, ok := got[id]; !ok {
		t.Error("a snapshot exactly maxAge old must be kept (boundary is inclusive)")
	}
}

func TestBuildQuotaAdvice_AgeOneNanosecondPastMaxAgeIsDropped(t *testing.T) {
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": now.Add(time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	id := uuid.New()
	const maxAge = 15 * time.Minute

	got := buildQuotaAdvice(
		[]quota.Snapshot{{ProviderID: id, Kind: "usage", Payload: payload, FetchedAt: now.Add(-maxAge - time.Nanosecond)}},
		map[uuid.UUID]string{id: "zai-coding"},
		maxAge,
		now,
	)

	if _, ok := got[id]; ok {
		t.Error("a snapshot one nanosecond past maxAge must be dropped")
	}
}

// TestBuildQuotaAdvice_ZeroMaxAgeAdvisesNothing pins the human-ruled inversion:
// quota_refresh_interval_min <= 0 (polling disabled) resolves to maxAge <= 0,
// and that must mean "advise nothing", not "no staleness filter". The
// snapshot here is deliberately pristine (FetchedAt = now, a genuinely
// exhausted payload) so this cannot pass because the data happened to be
// stale or unassessable — only the maxAge<=0 short-circuit can drop it.
func TestBuildQuotaAdvice_ZeroMaxAgeAdvisesNothing(t *testing.T) {
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 0, "nextResetTime": now.Add(time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	id := uuid.New()

	got := buildQuotaAdvice(
		[]quota.Snapshot{{ProviderID: id, Kind: "usage", Payload: payload, FetchedAt: now}},
		map[uuid.UUID]string{id: "zai-coding"},
		0,
		now,
	)

	if len(got) != 0 {
		t.Errorf("maxAge<=0 must advise nothing (polling disabled), got %v", got)
	}
}
