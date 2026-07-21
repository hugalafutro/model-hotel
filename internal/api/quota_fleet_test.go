package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// cancelledRequest builds a request whose context is already cancelled, so the
// first DB call the handler makes fails, exercising the store-error branches.
func cancelledRequest(method, target, body string) *http.Request {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return httptest.NewRequest(method, target, strings.NewReader(body)).WithContext(ctx)
}

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

// TestQuotaFleetExportSkipsFailurePlaceholders: RecordFailure leaves a
// placeholder row (http_status=0, no real payload, fresh fetched_at). It must
// not be distributed, or a member would store it as source='fleet' and suppress
// its own (potentially successful) poll while that empty row looks fresh.
func TestQuotaFleetExportSkipsFailurePlaceholders(t *testing.T) {
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	failed, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "failed-only",
		BaseURL: "https://api.nano-gpt.com",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create failed provider: %v", err)
	}
	if err := h.quotaRepo.RecordFailure(context.Background(), failed.ID, "usage", "boom"); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	okProv, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    "ok-provider",
		BaseURL: "https://api.nano-gpt.com",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create ok provider: %v", err)
	}
	if err := h.quotaRepo.Upsert(context.Background(), quota.Snapshot{
		ProviderID: okProv.ID,
		Kind:       "usage",
		Payload:    json.RawMessage(`{"used":4}`),
		HTTPStatus: 200,
		Source:     "poll",
	}); err != nil {
		t.Fatalf("upsert ok snapshot: %v", err)
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
	if len(body.Snapshots) != 1 || body.Snapshots[0].ProviderName != okProv.Name {
		t.Fatalf("only the successful snapshot should export, got %+v", body.Snapshots)
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

// TestQuotaFleetRegisterMountsRoutes: Register wires the export/receive routes on
// the given router so both respond.
func TestQuotaFleetRegisterMountsRoutes(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	NewQuotaFleetHandler(h.quotaRepo, h.providerRepo).Register(r)

	for _, tc := range []struct {
		method, body string
	}{
		{http.MethodGet, ""},
		{http.MethodPost, `{"snapshots":[]}`},
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, "/config/quota-snapshots", strings.NewReader(tc.body))
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: want 200, got %d: %s", tc.method, rr.Code, rr.Body.String())
		}
	}
}

// TestQuotaFleetExportListError: a store failure listing snapshots surfaces a 500.
func TestQuotaFleetExportListError(t *testing.T) {
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	rr := httptest.NewRecorder()
	fleet.ExportSnapshots(rr, cancelledRequest(http.MethodGet, "/config/quota-snapshots", ""))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 on store error, got %d", rr.Code)
	}
}

// TestQuotaFleetReceiveInvalidBody: a malformed request body is a 400.
func TestQuotaFleetReceiveInvalidBody(t *testing.T) {
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", strings.NewReader("not json"))
	fleet.ReceiveSnapshots(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400 on bad body, got %d", rr.Code)
	}
}

// TestQuotaFleetReceiveProviderListError: a store failure listing providers (the
// body decodes fine first) surfaces a 500.
func TestQuotaFleetReceiveProviderListError(t *testing.T) {
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	rr := httptest.NewRecorder()
	fleet.ReceiveSnapshots(rr, cancelledRequest(http.MethodPost, "/config/quota-snapshots", `{"snapshots":[]}`))
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 on store error, got %d", rr.Code)
	}
}

// TestQuotaFleetReceiveSkipsUnknownProvider: a snapshot for a provider name not
// present on this member is skipped, not applied.
func TestQuotaFleetReceiveSkipsUnknownProvider(t *testing.T) {
	h := newTestHandler(t)
	fleet := NewQuotaFleetHandler(h.quotaRepo, h.providerRepo)

	body := `{"snapshots":[{"provider_name":"does-not-exist-here","kind":"usage","payload":{"used":1},"http_status":200,"fetched_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}]}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/config/quota-snapshots", strings.NewReader(body))
	fleet.ReceiveSnapshots(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Applied int `json:"applied"`
		Skipped int `json:"skipped"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Applied != 0 || out.Skipped != 1 {
		t.Fatalf("unknown provider must be skipped, got applied=%d skipped=%d", out.Applied, out.Skipped)
	}
}
