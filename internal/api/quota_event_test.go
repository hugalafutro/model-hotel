package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// drainQuotaStatusEvents subscribes to the bus and returns a drain that hands
// back every quota.status_* event queued since the previous drain. Bus.Publish
// delivers into the subscriber channel synchronously, so whatever the call
// under test published is already there; no waiting is needed.
func drainQuotaStatusEvents(t *testing.T) func() []events.Event {
	t.Helper()
	ch := events.DefaultBus.Subscribe()
	t.Cleanup(func() { events.DefaultBus.Unsubscribe(ch) })
	return func() []events.Event {
		var got []events.Event
		for {
			select {
			case ev := <-ch:
				if strings.HasPrefix(ev.Type, "quota.status_") {
					got = append(got, ev)
				}
			default:
				return got
			}
		}
	}
}

// TestPublishQuotaStatusChange covers the announce rule on its own: no prior
// reading (no row, or RecordFailure's 0 placeholder), an unchanged status and
// a move between two non-200 answers are silent; a reading coming back is a
// restored/success, a reading going away is a lost/warning; on a managed fleet
// member the flip keeps message and severity under the uncatalogued synced
// type; every event carries the pair of statuses.
func TestPublishQuotaStatusChange(t *testing.T) {
	prov := &provider.Provider{ID: uuid.New(), Name: "Go", BaseURL: "https://opencode.ai/zen/go/v1"}
	own := func(status int) quota.Prior { return quota.Prior{HTTPStatus: status} }
	cases := []struct {
		name     string
		prior    quota.Prior
		status   int
		member   bool
		wantType string
		severity string
		phrase   string
	}{
		{name: "no previous row", status: 200},
		{name: "failure placeholder", prior: own(0), status: 204},
		{name: "same status", prior: own(200), status: 200},
		{name: "same status on a member", prior: own(204), status: 204, member: true},
		{name: "no plan to dead key changes no badge", prior: own(204), status: 424},
		{name: "dead key to no plan changes no badge", prior: own(424), status: 204},
		{name: "reading back", prior: own(204), status: 200, wantType: EventTypeQuotaStatusRestored, severity: "success", phrase: "is back"},
		{name: "reading gone", prior: own(200), status: 204, wantType: EventTypeQuotaStatusLost, severity: "warning", phrase: "stopped"},
		{name: "key rejected", prior: own(200), status: 424, wantType: EventTypeQuotaStatusLost, severity: "warning", phrase: "stopped"},
		{name: "reading back on a member", prior: own(204), status: 200, member: true, wantType: EventTypeQuotaStatusSynced, severity: "success", phrase: "is back"},
		{name: "reading gone on a member", prior: own(200), status: 204, member: true, wantType: EventTypeQuotaStatusSynced, severity: "warning", phrase: "stopped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drain := drainQuotaStatusEvents(t)
			publishQuotaStatusChange(prov, "usage", tc.prior, tc.status, tc.member)
			got := drain()
			if tc.wantType == "" {
				if len(got) != 0 {
					t.Fatalf("want silence, got %+v", got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want one event, got %d: %+v", len(got), got)
			}
			ev := got[0]
			if ev.Type != tc.wantType || ev.Severity != tc.severity || !strings.Contains(ev.Message, tc.phrase) || !strings.Contains(ev.Message, "Go") {
				t.Fatalf("want %s/%s mentioning %q and the provider, got %s/%s %q", tc.wantType, tc.severity, tc.phrase, ev.Type, ev.Severity, ev.Message)
			}
			if ev.Source != "quota" || ev.Metadata["provider_id"] != prov.ID.String() || ev.Metadata["provider_type"] != "opencode-go" ||
				ev.Metadata["kind"] != "usage" || ev.Metadata["http_status"] != tc.status || ev.Metadata["previous_http_status"] != tc.prior.HTTPStatus {
				t.Fatalf("unexpected source/metadata: %s %+v", ev.Source, ev.Metadata)
			}
		})
	}
}

// TestPollQuotasOnce_PublishesStatusFlip: a poll that turns a 204 row (nothing
// to read, as a lapsed subscription leaves it) into a 200 announces the flip
// once, and the next poll with the same answer stays quiet.
func TestPollQuotasOnce_PublishesStatusFlip(t *testing.T) {
	ctx := context.Background()
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-flip", "https://api.nano-gpt.com/v1", true)
	if _, err := h.quotaRepo.Replace(ctx, quota.Snapshot{ProviderID: id, Kind: "usage", Payload: json.RawMessage("null"), HTTPStatus: http.StatusNoContent, Source: "poll"}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	h.newDiscovery = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(9) }
	drain := drainQuotaStatusEvents(t)

	h.PollQuotasOnce(ctx)
	got := drain()
	if len(got) != 1 || got[0].Type != EventTypeQuotaStatusRestored {
		t.Fatalf("want one restored event after the flip, got %+v", got)
	}
	if got[0].Metadata["provider_id"] != id.String() || got[0].Metadata["previous_http_status"] != http.StatusNoContent || got[0].Metadata["http_status"] != http.StatusOK {
		t.Fatalf("unexpected metadata: %+v", got[0].Metadata)
	}

	h.PollQuotasOnce(ctx)
	if again := drain(); len(again) != 0 {
		t.Fatalf("an unchanged status must not be announced again, got %+v", again)
	}
}

// TestPollQuotasOnce_FirstAnswerAfterFailureIsSilent: a provider whose only
// row is RecordFailure's placeholder (its first fetches timed out) gets no
// "is back" when the first fetch succeeds; there was never a reading to lose.
func TestPollQuotasOnce_FirstAnswerAfterFailureIsSilent(t *testing.T) {
	ctx := context.Background()
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-first", "https://api.nano-gpt.com/v1", true)
	if err := h.quotaRepo.RecordFailure(ctx, id, "usage", "context deadline exceeded"); err != nil {
		t.Fatalf("seed failure: %v", err)
	}
	h.newDiscovery = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(9) }
	drain := drainQuotaStatusEvents(t)

	h.PollQuotasOnce(ctx)
	if got := drain(); len(got) != 0 {
		t.Fatalf("a first successful fetch is not a flip, got %+v", got)
	}
	snap, _ := h.quotaRepo.Get(ctx, id, "usage")
	if snap == nil || snap.HTTPStatus != http.StatusOK {
		t.Fatalf("the fetch itself must still be stored, got %+v", snap)
	}
}

// TestPollQuotasOnce_MemberFlipIsDashboardOnly: on a managed fleet member
// (Front Desk heard from recently, not the primary) a flip the node fetched
// itself, as a breaker nudge makes every member do at once, goes out under the
// synced type; the primary alerts for the fleet. A node Front Desk marks as the
// primary announces the alertable type.
func TestPollQuotasOnce_MemberFlipIsDashboardOnly(t *testing.T) {
	for _, tc := range []struct {
		name     string
		primary  string
		wantType string
	}{
		{name: "member", primary: "false", wantType: EventTypeQuotaStatusSynced},
		{name: "primary", primary: "true", wantType: EventTypeQuotaStatusRestored},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			h := newTestHandler(t)
			for k, v := range map[string]string{keyFleetManagedSeenAt: time.Now().Format(time.RFC3339), keyFleetIsPrimary: tc.primary} {
				if err := h.settingsRepo.Set(ctx, k, v); err != nil {
					t.Fatalf("set %s: %v", k, err)
				}
			}
			id := insertQuotaPollProvider(t, h.dbPool.Pool(), "nanogpt-"+tc.name, "https://api.nano-gpt.com/v1", true)
			stale := time.Now().Add(-2 * time.Hour).UTC()
			if _, err := h.quotaRepo.Replace(ctx, quota.Snapshot{ProviderID: id, Kind: "usage", Payload: json.RawMessage("null"), HTTPStatus: http.StatusNoContent, Source: "fleet", FetchedAt: stale}); err != nil {
				t.Fatalf("seed snapshot: %v", err)
			}
			h.newDiscovery = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(9) }
			drain := drainQuotaStatusEvents(t)

			h.PollQuotasOnce(ctx)
			got := drain()
			if len(got) != 1 || got[0].Type != tc.wantType || got[0].Severity != "success" {
				t.Fatalf("want one %s/success event, got %+v", tc.wantType, got)
			}
		})
	}
}

// TestQuotaFleetReceiveSnapshots_PublishesStatusFlip: a member learns of the
// flip from the row the primary pushes and announces it under the synced type,
// which the dashboard reads and the alert catalog does not (only members
// receive pushes, so the import path never asks the node's role). A newer push with
// the same status, the equal-timestamp failure-marker write, and a push that is
// not written (older than the stored row) all stay silent.
func TestQuotaFleetReceiveSnapshots_PublishesStatusFlip(t *testing.T) {
	ctx := context.Background()
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{Name: "nano-flip", BaseURL: "https://api.nano-gpt.com"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	stored := time.Now().Add(-time.Hour).UTC()
	if _, err := h.quotaRepo.Replace(ctx, quota.Snapshot{ProviderID: prov.ID, Kind: "usage", Payload: json.RawMessage("null"), HTTPStatus: http.StatusNoContent, Source: "fleet", FetchedAt: stored}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	drain := drainQuotaStatusEvents(t)

	push := func(status int, at time.Time, lastError string) {
		body := `{"snapshots":[{"provider_name":"nano-flip","kind":"usage","payload":{"used":8},"http_status":` +
			strconv.Itoa(status) + `,"fetched_at":"` + at.Format(time.RFC3339Nano) + `","last_error":"` + lastError + `"}]}`
		rr := httptest.NewRecorder()
		fleet.ReceiveSnapshots(rr, httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", strings.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
		}
	}

	flipAt := time.Now().UTC().Truncate(time.Microsecond)
	push(http.StatusOK, flipAt, "")
	got := drain()
	if len(got) != 1 || got[0].Type != EventTypeQuotaStatusSynced || got[0].Severity != "success" ||
		got[0].Metadata["previous_http_status"] != http.StatusNoContent || got[0].Metadata["http_status"] != http.StatusOK {
		t.Fatalf("want one synced 204->200 event, got %+v", got)
	}

	push(http.StatusOK, flipAt.Add(time.Minute), "")
	if again := drain(); len(again) != 0 {
		t.Fatalf("a newer row with the same status is written but not announced, got %+v", again)
	}

	push(http.StatusOK, flipAt.Add(time.Minute), "upstream timed out")
	if again := drain(); len(again) != 0 {
		t.Fatalf("the equal-timestamp failure marker is written but not announced, got %+v", again)
	}

	push(http.StatusNoContent, stored.Add(-time.Hour), "")
	if again := drain(); len(again) != 0 {
		t.Fatalf("a push that is not written must not be announced, got %+v", again)
	}
}
