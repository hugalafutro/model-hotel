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
// back every quota.status_changed event queued since the previous drain.
// Bus.Publish delivers into the subscriber channel synchronously, so whatever
// the call under test published is already there; no waiting is needed.
func drainQuotaStatusEvents(t *testing.T) func() []events.Event {
	t.Helper()
	ch := events.DefaultBus.Subscribe()
	t.Cleanup(func() { events.DefaultBus.Unsubscribe(ch) })
	return func() []events.Event {
		var got []events.Event
		for {
			select {
			case ev := <-ch:
				if ev.Type == EventTypeQuotaStatusChanged {
					got = append(got, ev)
				}
			default:
				return got
			}
		}
	}
}

// TestPublishQuotaStatusChange covers the announce rule on its own: a first
// snapshot and an unchanged status are silent, a reading coming back is a
// success, a reading going away is a warning, and both carry the pair of
// statuses the dashboard and the alert text are built from.
func TestPublishQuotaStatusChange(t *testing.T) {
	prov := &provider.Provider{ID: uuid.New(), Name: "Go", BaseURL: "https://opencode.ai/zen/go/v1"}
	cases := []struct {
		name     string
		prev     *quota.Snapshot
		status   int
		want     int
		severity string
		phrase   string
	}{
		{name: "first snapshot", prev: nil, status: 200, want: 0},
		{name: "same status", prev: &quota.Snapshot{HTTPStatus: 200}, status: 200, want: 0},
		{name: "reading back", prev: &quota.Snapshot{HTTPStatus: 204}, status: 200, want: 1, severity: "success", phrase: "is back"},
		{name: "reading gone", prev: &quota.Snapshot{HTTPStatus: 200}, status: 204, want: 1, severity: "warning", phrase: "stopped"},
		{name: "key rejected", prev: &quota.Snapshot{HTTPStatus: 200}, status: 424, want: 1, severity: "warning", phrase: "stopped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			drain := drainQuotaStatusEvents(t)
			publishQuotaStatusChange(prov, "usage", tc.prev, tc.status)
			got := drain()
			if len(got) != tc.want {
				t.Fatalf("want %d events, got %d: %+v", tc.want, len(got), got)
			}
			if tc.want == 0 {
				return
			}
			ev := got[0]
			if ev.Severity != tc.severity || !strings.Contains(ev.Message, tc.phrase) || !strings.Contains(ev.Message, "Go") {
				t.Fatalf("want %s event mentioning %q and the provider, got %s %q", tc.severity, tc.phrase, ev.Severity, ev.Message)
			}
			if ev.Source != "quota" || ev.Metadata["provider_id"] != prov.ID.String() || ev.Metadata["provider_type"] != "opencode-go" ||
				ev.Metadata["kind"] != "usage" || ev.Metadata["http_status"] != tc.status || ev.Metadata["previous_http_status"] != tc.prev.HTTPStatus {
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
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{ProviderID: id, Kind: "usage", Payload: json.RawMessage("null"), HTTPStatus: http.StatusNoContent, Source: "poll"}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	h.newDiscovery = func() *provider.DiscoveryService { return nanoGPTPollDiscovery(9) }
	drain := drainQuotaStatusEvents(t)

	h.PollQuotasOnce(ctx)
	got := drain()
	if len(got) != 1 {
		t.Fatalf("want one status event after the flip, got %d: %+v", len(got), got)
	}
	if got[0].Metadata["provider_id"] != id.String() || got[0].Metadata["previous_http_status"] != http.StatusNoContent || got[0].Metadata["http_status"] != http.StatusOK {
		t.Fatalf("unexpected metadata: %+v", got[0].Metadata)
	}

	h.PollQuotasOnce(ctx)
	if again := drain(); len(again) != 0 {
		t.Fatalf("an unchanged status must not be announced again, got %+v", again)
	}
}

// TestQuotaFleetReceiveSnapshots_PublishesStatusFlip: a member learns of the
// flip from the row the primary pushes, so its own dashboard moves too. A push
// that is not written (older than the stored row) announces nothing.
func TestQuotaFleetReceiveSnapshots_PublishesStatusFlip(t *testing.T) {
	ctx := context.Background()
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)
	prov, err := h.providerRepo.Create(ctx, provider.CreateProviderRequest{Name: "nano-flip", BaseURL: "https://api.nano-gpt.com"}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	stored := time.Now().Add(-time.Hour).UTC()
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{ProviderID: prov.ID, Kind: "usage", Payload: json.RawMessage("null"), HTTPStatus: http.StatusNoContent, Source: "fleet", FetchedAt: stored}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	drain := drainQuotaStatusEvents(t)

	push := func(status int, at time.Time) {
		body := `{"snapshots":[{"provider_name":"nano-flip","kind":"usage","payload":{"used":8},"http_status":` +
			strconv.Itoa(status) + `,"fetched_at":"` + at.Format(time.RFC3339) + `"}]}`
		rr := httptest.NewRecorder()
		fleet.ReceiveSnapshots(rr, httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", strings.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
		}
	}

	push(http.StatusOK, time.Now().UTC())
	got := drain()
	if len(got) != 1 || got[0].Metadata["previous_http_status"] != http.StatusNoContent || got[0].Metadata["http_status"] != http.StatusOK {
		t.Fatalf("want one 204->200 event, got %+v", got)
	}

	push(http.StatusNoContent, stored.Add(-time.Hour))
	if again := drain(); len(again) != 0 {
		t.Fatalf("a push that is not written must not be announced, got %+v", again)
	}
}
