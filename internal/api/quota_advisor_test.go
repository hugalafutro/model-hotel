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

func TestBuildQuotaAdvice_DropsStaleAndUnassessable(t *testing.T) {
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

	fresh, stale, unknownType := uuid.New(), uuid.New(), uuid.New()
	snaps := []quota.Snapshot{
		{ProviderID: fresh, Kind: "usage", Payload: exhausted, FetchedAt: now.Add(-time.Minute)},
		{ProviderID: stale, Kind: "usage", Payload: exhausted, FetchedAt: now.Add(-time.Hour)},
		{ProviderID: unknownType, Kind: "usage", Payload: exhausted, FetchedAt: now},
	}
	typeByID := map[uuid.UUID]string{
		fresh:       "zai-coding",
		stale:       "zai-coding",
		unknownType: "openai",
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
}

func TestBuildQuotaAdvice_ZeroMaxAgeDisablesStalenessFilter(t *testing.T) {
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
		[]quota.Snapshot{{ProviderID: id, Kind: "usage", Payload: payload, FetchedAt: now.Add(-30 * 24 * time.Hour)}},
		map[uuid.UUID]string{id: "zai-coding"},
		0,
		now,
	)

	if _, ok := got[id]; !ok {
		t.Error("maxAge=0 means no staleness filter, so even an ancient snapshot advises")
	}
}
