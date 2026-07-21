package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

func TestQuotaFleetExportSnapshots(t *testing.T) {
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "nano-export",
		BaseURL: "https://api.nano-gpt.com",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := h.quotaRepo.Upsert(context.Background(), quota.Snapshot{
		ProviderID: prov.ID,
		Kind:       "usage",
		Payload:    json.RawMessage(`{"used":4}`),
		HTTPStatus: 200,
		Source:     "poll",
	}); err != nil {
		t.Fatalf("upsert snapshot: %v", err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/config/quota-snapshots", http.NoBody)
	fleet.ExportSnapshots(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Snapshots []QuotaSnapshotWire `json:"snapshots"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Snapshots) != 1 {
		t.Fatalf("want 1 snapshot, got %d: %+v", len(body.Snapshots), body.Snapshots)
	}
	if body.Snapshots[0].ProviderName != prov.Name || body.Snapshots[0].Kind != "usage" {
		t.Fatalf("unexpected export: %+v", body.Snapshots[0])
	}
}

func TestQuotaFleetReceiveSnapshots_UpsertsAsFleet(t *testing.T) {
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "nano-recv",
		BaseURL: "https://api.nano-gpt.com",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	body := `{"snapshots":[{"provider_name":"` + prov.Name + `","kind":"usage","payload":{"used":8},"http_status":200,"fetched_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}]}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", strings.NewReader(body))
	fleet.ReceiveSnapshots(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	snap, _ := h.quotaRepo.Get(context.Background(), prov.ID, "usage")
	if snap == nil || snap.Source != "fleet" {
		t.Fatalf("receive should upsert a fleet snapshot, got %+v", snap)
	}
	// JSONB re-serializes (adds a space after ':'), so compare by value.
	var got struct {
		Used int `json:"used"`
	}
	if err := json.Unmarshal(snap.Payload, &got); err != nil || got.Used != 8 {
		t.Fatalf("expected used=8 fleet payload, got %s (err %v)", snap.Payload, err)
	}
}

func TestQuotaFleetReceiveSnapshots_SkipsOlder(t *testing.T) {
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	prov, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "nano-recv-older",
		BaseURL: "https://api.nano-gpt.com",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	// A newer local manual snapshot must survive an older incoming fleet write.
	if err := h.quotaRepo.Upsert(context.Background(), quota.Snapshot{
		ProviderID: prov.ID,
		Kind:       "usage",
		Payload:    json.RawMessage(`{"used":99}`),
		HTTPStatus: 200,
		Source:     "manual",
		FetchedAt:  time.Now(),
	}); err != nil {
		t.Fatalf("seed manual snapshot: %v", err)
	}

	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	body := `{"snapshots":[{"provider_name":"` + prov.Name + `","kind":"usage","payload":{"used":1},"http_status":200,"fetched_at":"` + old + `"}]}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", strings.NewReader(body))
	fleet.ReceiveSnapshots(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	snap, _ := h.quotaRepo.Get(context.Background(), prov.ID, "usage")
	if snap == nil || snap.Source != "manual" {
		t.Fatalf("older fleet write must not clobber newer manual, got %+v", snap)
	}
	// JSONB re-serializes (adds a space after ':'), so compare by value.
	var got struct {
		Used int `json:"used"`
	}
	if err := json.Unmarshal(snap.Payload, &got); err != nil || got.Used != 99 {
		t.Fatalf("expected the newer manual payload used=99 to survive, got %s (err %v)", snap.Payload, err)
	}
}
