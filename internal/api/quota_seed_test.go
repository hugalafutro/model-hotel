package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// Seeding the breaker from stored quota snapshots: the whole path from a row in
// quota_snapshots through Assess and buildQuotaAdvice into a circuit that was
// closed a moment earlier. Circuit state is in-memory, so this is what a
// restart, a fresh member, and a provider nothing has requested since its
// window went spent all look like.

// TestRefreshQuotaAdvice_SeedsAPinOnAClosedCircuit is the property the quota
// badge and the Failover page disagreed on before: a stored exhausted snapshot
// must pin the provider on the first advice pass, without waiting for two real
// refusals to open a circuit.
func TestRefreshQuotaAdvice_SeedsAPinOnAClosedCircuit(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-spent", "https://api.z.ai", true)
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: id, Kind: "usage", HTTPStatus: 200, Source: "poll",
		Payload:   exhaustedZaiCodingPayload(t, time.Now().Add(4*time.Hour)),
		FetchedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}

	cb := failover.NewCircuitBreaker(nil)
	h.SetCircuitBreaker(cb)
	h.SetQuotaAdvisor(NewQuotaAdvisor())

	if cb.IsOpen(id, "zai-spent", "glm-5.3") {
		t.Fatal("setup: a fresh breaker must have every circuit closed")
	}

	h.RefreshQuotaAdvice(ctx)

	if !cb.IsOpen(id, "zai-spent", "glm-5.3") {
		t.Error("a stored exhausted snapshot must pin the provider on the advice pass, not on the next two refusals")
	}
	statuses := cb.Status()
	if len(statuses) != 1 {
		t.Fatalf("got %d breaker rows, want 1", len(statuses))
	}
	if !statuses[0].QuotaPinned || !statuses[0].ProviderOpen {
		t.Errorf("got quota_pinned=%v provider_open=%v, want both true", statuses[0].QuotaPinned, statuses[0].ProviderOpen)
	}
	// Roughly the snapshot's own reset, not the configured cooldown.
	if statuses[0].CooldownMs < (3 * time.Hour).Milliseconds() {
		t.Errorf("got CooldownMs=%d, want the snapshot's 4h reset", statuses[0].CooldownMs)
	}
}

// TestRefreshQuotaAdvice_UnusableSnapshotSeedsNothing applies the same rule at
// the breaker rather than at the maps: only a datable future reset is a
// reading, so a payload that is no reading at all, and one whose reset cannot
// be dated, must leave a closed circuit closed.
// Seeding is the first thing that could act on a bad verdict without a request
// to check it against, so this is where it matters most.
func TestRefreshQuotaAdvice_UnusableSnapshotSeedsNothing(t *testing.T) {
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"204 null payload", `null`},
		{"spent window with an unreadable reset", `{"usage":{"rolling":{"status":"ok","percent":100,"resetsAt":"soon"}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHandler(t)
			ctx := context.Background()

			id := insertQuotaPollProvider(t, h.dbPool.Pool(), "ocgo-"+tc.name, "https://opencode.ai/zen/v1", true)
			if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
				ProviderID: id, Kind: "usage", HTTPStatus: 200, Source: "poll",
				Payload:   json.RawMessage(tc.payload),
				FetchedAt: time.Now().Add(-time.Minute),
			}); err != nil {
				t.Fatalf("seed snapshot: %v", err)
			}

			cb := failover.NewCircuitBreaker(nil)
			h.SetCircuitBreaker(cb)
			h.SetQuotaAdvisor(NewQuotaAdvisor())

			h.RefreshQuotaAdvice(ctx)

			if cb.IsOpen(id, "ocgo", "some-model") {
				t.Error("an unusable snapshot is no verdict: it must not darken a provider")
			}
			if len(cb.Status()) != 0 {
				t.Errorf("got %d breaker rows, want none tracked", len(cb.Status()))
			}
		})
	}
}

// TestRefreshQuotaAdvice_SeededPinReleasedWhenTheSnapshotReadsHealthy closes
// the loop the user asked for in both directions: the same pass that seeds the
// pin lifts it once the provider's own payload says the window is back.
func TestRefreshQuotaAdvice_SeededPinReleasedWhenTheSnapshotReadsHealthy(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-recovers", "https://api.z.ai", true)
	store := func(payload json.RawMessage) {
		t.Helper()
		if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
			ProviderID: id, Kind: "usage", HTTPStatus: 200, Source: "poll",
			Payload: payload, FetchedAt: time.Now().Add(-time.Minute),
		}); err != nil {
			t.Fatalf("store snapshot: %v", err)
		}
	}

	cb := failover.NewCircuitBreaker(nil)
	h.SetCircuitBreaker(cb)
	h.SetQuotaAdvisor(NewQuotaAdvisor())

	store(exhaustedZaiCodingPayload(t, time.Now().Add(4*time.Hour)))
	h.RefreshQuotaAdvice(ctx)
	if !cb.IsOpen(id, "zai-recovers", "glm-5.3") {
		t.Fatal("setup: the exhausted snapshot must seed a pin")
	}

	// A window with room left in it: the same shape, percentage well under 100.
	healthy, err := json.Marshal(map[string]any{
		"data": map[string]any{"limits": []map[string]any{
			{"type": "TOKENS_LIMIT", "unit": 3, "remaining": 900, "percentage": 10.0},
		}},
	})
	if err != nil {
		t.Fatalf("marshal healthy payload: %v", err)
	}
	store(healthy)
	h.RefreshQuotaAdvice(ctx)

	if cb.IsOpen(id, "zai-recovers", "glm-5.3") {
		t.Error("a healthy snapshot must release the seeded pin and let the provider route again")
	}
	if len(cb.Status()) != 0 {
		t.Errorf("got %d breaker rows, want the seeded circuit retired", len(cb.Status()))
	}
}

// TestReceiveSnapshots_RebuildsQuotaAdvice covers the member half of the
// requirement. A member only self-polls every quota_refresh_interval_min, so
// without this hook a fleet-distributed exhaustion reaches its breaker minutes
// after it reached the primary's and the fleet disagrees about which providers
// are pinned. The push itself must rebuild the advice.
func TestReceiveSnapshots_RebuildsQuotaAdvice(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()

	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-member", "https://api.z.ai", true)

	cb := failover.NewCircuitBreaker(nil)
	h.SetCircuitBreaker(cb)
	h.SetQuotaAdvisor(NewQuotaAdvisor())

	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)
	fleet.onApplied = h.RefreshQuotaAdvice

	body, err := json.Marshal(map[string]any{"snapshots": []QuotaSnapshotWire{{
		ProviderName: "zai-member",
		Type:         "zai-coding",
		Kind:         "usage",
		Payload:      exhaustedZaiCodingPayload(t, time.Now().Add(4*time.Hour)),
		HTTPStatus:   200,
		FetchedAt:    time.Now().Add(-time.Minute),
	}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", bytes.NewReader(body)).WithContext(ctx)
	fleet.ReceiveSnapshots(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if !cb.IsOpen(id, "zai-member", "glm-5.3") {
		t.Error("a fleet snapshot push must rebuild the advice, so every member pins the same providers")
	}
}

// TestReceiveSnapshots_NoAppliedRowsSkipsTheRebuild: the pass walks every
// snapshot and every circuit, and a distribution that wrote nothing new changed
// no evidence to act on.
func TestReceiveSnapshots_NoAppliedRowsSkipsTheRebuild(t *testing.T) {
	h := newTestHandler(t)

	called := 0
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)
	fleet.onApplied = func(context.Context) { called++ }

	// A name no local provider carries: skipped, nothing written.
	body := `{"snapshots":[{"provider_name":"absent","type":"zai-coding","kind":"usage","payload":{},"http_status":200}]}`
	rr := httptest.NewRecorder()
	fleet.ReceiveSnapshots(rr, httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", bytes.NewReader([]byte(body))))
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if called != 0 {
		t.Errorf("got %d rebuilds, want 0: nothing was applied", called)
	}
}

// TestReceiveSnapshots_RebuildDetachedFromThePushingPeer: the rebuild fails
// closed, so a read it cannot finish clears this member's advice for every
// provider, not just the pushed one. Running it on the peer's request context
// would hand a Front Desk that times out, or a connection that drops, the power
// to wipe the whole map mid-pass. The rows are already stored by then, so the
// pass must carry a budget of its own.
func TestReceiveSnapshots_RebuildDetachedFromThePushingPeer(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-disconnect", "https://api.z.ai", true)

	var passErr error
	var ran bool
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)
	fleet.onApplied = func(c context.Context) {
		ran = true
		// The peer disconnects the instant the pass starts.
		cancel()
		passErr = c.Err()
	}

	body, err := json.Marshal(map[string]any{"snapshots": []QuotaSnapshotWire{{
		ProviderName: "zai-disconnect",
		Type:         "zai-coding",
		Kind:         "usage",
		Payload:      exhaustedZaiCodingPayload(t, time.Now().Add(4*time.Hour)),
		HTTPStatus:   200,
		FetchedAt:    time.Now().Add(-time.Minute),
	}}})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", bytes.NewReader(body)).WithContext(ctx)
	fleet.ReceiveSnapshots(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if !ran {
		t.Fatal("setup: the applied push must run the rebuild")
	}
	if passErr != nil {
		t.Errorf("got pass context error %v after the peer disconnected, want the pass to keep running", passErr)
	}
}
