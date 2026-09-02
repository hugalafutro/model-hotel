package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// TestPollQuotasOnce_FutureFleetStampDoesNotSuppress pins the negative-age guard
// at the fleet-skip site. A fleet snapshot dated in the future has a NEGATIVE age,
// which a raw comparison reads as fresher than any interval, so the member's own
// poll would be skipped for as long as the stamp stood. The repository clamps a
// future stamp on write, so the row is aged past the clamp directly, the way a
// restored backup or a row predating the clamp would arrive.
func TestPollQuotasOnce_FutureFleetStampDoesNotSuppress(t *testing.T) {
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-fleet-future", "https://api.nano-gpt.com/v1", true)

	if err := h.quotaRepo.Upsert(context.Background(), quota.Snapshot{
		ProviderID: id, Kind: "usage", Payload: json.RawMessage(`{"used":1}`), HTTPStatus: 200, Source: "fleet",
	}); err != nil {
		t.Fatalf("seed fleet snapshot: %v", err)
	}
	if _, err := h.dbPool.Pool().Exec(context.Background(),
		`UPDATE provider_quota_snapshots SET fetched_at = $1 WHERE provider_id = $2`, time.Now().Add(48*time.Hour), id); err != nil {
		t.Fatalf("age the row into the future: %v", err)
	}

	h.newDiscovery = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(2) }

	h.PollQuotasOnce(context.Background())

	snap, _ := h.quotaRepo.Get(context.Background(), id, "usage")
	if snap == nil || snap.Source != "poll" {
		t.Fatalf("a future-dated fleet snapshot must not suppress the self-poll, got %+v", snap)
	}
}

// TestBuildQuotaAdvice_FutureStampIsNotEvidence pins the same guard at the
// advice site: a future stamp fails every "> maxAge" test and would count as
// evidence forever.
func TestBuildQuotaAdvice_FutureStampIsNotEvidence(t *testing.T) {
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
		[]quota.Snapshot{{ProviderID: id, Kind: "usage", Payload: payload, FetchedAt: now.Add(48 * time.Hour)}},
		map[uuid.UUID]string{id: "zai-coding"},
		15*time.Minute,
		now,
	)

	if _, ok := got[id]; ok {
		t.Error("a future-dated snapshot must not count as evidence; the negative-age guard is missing at this site")
	}
}

// TestDriftEligible_FutureStampIsNotEligible pins the negative-age guard at its
// call site: time.Sub returns a NEGATIVE duration for a future timestamp, which
// fails every "> maxAge" test, so a single future-dated snapshot would count as
// evidence forever. A snapshot at exactly maxAge is kept, as it always was.
func TestDriftEligible_FutureStampIsNotEligible(t *testing.T) {
	now := time.Now()
	const maxAge = 15 * time.Minute
	ok := quota.Snapshot{HTTPStatus: http.StatusOK, Payload: []byte(`{"a":1}`)}

	fresh := ok
	fresh.FetchedAt = now.Add(-1 * time.Minute)
	if !driftEligible(fresh, maxAge, now) {
		t.Error("a recent snapshot should be drift-eligible")
	}

	stale := ok
	stale.FetchedAt = now.Add(-1 * time.Hour)
	if driftEligible(stale, maxAge, now) {
		t.Error("an old snapshot should not be drift-eligible")
	}

	future := ok
	future.FetchedAt = now.Add(48 * time.Hour)
	if driftEligible(future, maxAge, now) {
		t.Error("a future-dated snapshot is drift-eligible forever; the negative-age guard is missing at this site")
	}

	atMaxAge := ok
	atMaxAge.FetchedAt = now.Add(-maxAge)
	if !driftEligible(atMaxAge, maxAge, now) {
		t.Error("a snapshot at exactly maxAge should still be eligible (the bound is <=, not <)")
	}
}
