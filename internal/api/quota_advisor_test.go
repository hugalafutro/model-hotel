package api

import (
	"encoding/json"
	"strconv"
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

	got, _ := buildQuotaAdvice(snaps, typeByID, 15*time.Minute, now)

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

	got, _ := buildQuotaAdvice(
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

	got, _ := buildQuotaAdvice(
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

	got, _ := buildQuotaAdvice(
		[]quota.Snapshot{{ProviderID: id, Kind: "usage", Payload: payload, FetchedAt: now}},
		map[uuid.UUID]string{id: "zai-coding"},
		0,
		now,
	)

	if len(got) != 0 {
		t.Errorf("maxAge<=0 must advise nothing (polling disabled), got %v", got)
	}
}

// TestBuildQuotaAdvice_RecoveredSetIsAffirmativeOnly is the unit-level statement
// of the release rule: the recovered set carries only providers a fresh snapshot
// was successfully assessed for and found not exhausted. The three ways a
// provider goes missing from the advice map — stale, unassessable, still spent —
// must each stay out of it too, because that set is what lifts a pin already in
// force, and lifting one on "we don't know" puts a genuinely exhausted provider
// straight back into rotation.
func TestBuildQuotaAdvice_RecoveredSetIsAffirmativeOnly(t *testing.T) {
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

	fresh, staleSpent, staleHealthy, unassessable, spent := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	snaps := []quota.Snapshot{
		{ProviderID: fresh, Kind: "usage", Payload: healthy, FetchedAt: now.Add(-time.Minute)},
		{ProviderID: staleSpent, Kind: "usage", Payload: exhausted, FetchedAt: now.Add(-time.Hour)},
		{ProviderID: staleHealthy, Kind: "usage", Payload: healthy, FetchedAt: now.Add(-time.Hour)},
		{ProviderID: unassessable, Kind: "usage", Payload: json.RawMessage(`{"data":{"limits":"unexpected shape"}}`), FetchedAt: now},
		{ProviderID: spent, Kind: "usage", Payload: exhausted, FetchedAt: now},
	}
	typeByID := map[uuid.UUID]string{
		fresh: "zai-coding", staleSpent: "zai-coding", staleHealthy: "zai-coding",
		unassessable: "zai-coding", spent: "zai-coding",
	}

	_, recovered := buildQuotaAdvice(snaps, typeByID, 15*time.Minute, now)

	if len(recovered) != 1 {
		t.Fatalf("got %d recovered providers, want 1: %v", len(recovered), recovered)
	}
	if _, ok := recovered[fresh]; !ok {
		t.Error("a fresh, assessed, not-exhausted snapshot is recovery evidence")
	}
	if _, ok := recovered[staleSpent]; ok {
		t.Error("a stale snapshot must not count as recovery — this is the case that unpins a still-exhausted provider")
	}
	if _, ok := recovered[staleHealthy]; ok {
		t.Error("a stale snapshot must not count as recovery even when it reads healthy; it is too old to trust either way")
	}
	if _, ok := recovered[unassessable]; ok {
		t.Error("a payload that could not be assessed must not count as recovery")
	}
	if _, ok := recovered[spent]; ok {
		t.Error("a provider whose window is still spent must not count as recovery")
	}
	// A provider with no snapshot at all is trivially absent, which is the
	// whole point: only listed providers can ever be reported recovered.
	if _, ok := recovered[uuid.New()]; ok {
		t.Error("a provider with no snapshot must not appear in the recovered set")
	}
}

// TestBuildQuotaAdvice_ZeroMaxAgeRecoversNothing is the release-side twin of
// TestBuildQuotaAdvice_ZeroMaxAgeAdvisesNothing. With polling disabled there is
// no cadence to bound snapshot age against, so a pristine-looking healthy
// snapshot is no more trustworthy as evidence of health than as evidence of
// exhaustion, and must not lift a pin.
func TestBuildQuotaAdvice_ZeroMaxAgeRecoversNothing(t *testing.T) {
	now := time.Now()
	payload, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 1000, "nextResetTime": now.Add(time.Hour).UnixMilli()},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	id := uuid.New()

	_, recovered := buildQuotaAdvice(
		[]quota.Snapshot{{ProviderID: id, Kind: "usage", Payload: payload, FetchedAt: now}},
		map[uuid.UUID]string{id: "zai-coding"},
		0,
		now,
	)

	if len(recovered) != 0 {
		t.Errorf("maxAge<=0 must recover nothing (polling disabled), got %v", recovered)
	}
}

// TestBuildQuotaAdvice_ExhaustionWinsOverRecoveryForTheSameProvider covers the
// one way a provider can land in both halves: a base URL edited from one
// quota-capable type to another leaves the old snapshot row behind, so two rows
// of different kinds can disagree. Keeping the pin one pass too long costs a
// delayed probe; dropping it wrongly puts a spent provider back in rotation.
func TestBuildQuotaAdvice_ExhaustionWinsOverRecoveryForTheSameProvider(t *testing.T) {
	now := time.Now()
	id := uuid.New()
	snaps := []quota.Snapshot{
		{ProviderID: id, Kind: "usage", FetchedAt: now, Payload: json.RawMessage(
			`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":1000,"nextResetTime":0}]}}`)},
		{ProviderID: id, Kind: "balance", FetchedAt: now, Payload: json.RawMessage(
			`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":0,"nextResetTime":` +
				strconv.FormatInt(now.Add(4*time.Hour).UnixMilli(), 10) + `}]}}`)},
	}

	advice, recovered := buildQuotaAdvice(snaps, map[uuid.UUID]string{id: "zai-coding"}, 15*time.Minute, now)

	if _, ok := advice[id]; !ok {
		t.Fatal("setup: the exhausted row must be advised")
	}
	if _, ok := recovered[id]; ok {
		t.Error("a provider read as exhausted anywhere must not also be reported recovered")
	}
}
