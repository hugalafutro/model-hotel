package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
