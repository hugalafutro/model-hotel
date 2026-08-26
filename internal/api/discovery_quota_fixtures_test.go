package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// The recorded upstream bodies the quota tests answer with.

// kimiCodeUsageSuccessBody is a well-formed /usages payload used to drive the
// kimi-code success arms.
const kimiCodeUsageSuccessBody = `{
	"user": {"userId": "u-1", "region": "REGION_OVERSEA", "membership": {"level": "LEVEL_BASIC"}},
	"usage": {"limit": "100", "remaining": "42", "resetTime": "2026-07-26T12:10:02Z"},
	"limits": [{"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "100", "remaining": "42", "resetTime": "2026-07-19T17:10:02Z"}}],
	"parallel": {"limit": "10"},
	"totalQuota": {"limit": "100", "remaining": "99"},
	"subType": "TYPE_PURCHASE"
}`

// minimaxQuotaSuccessBody is the reference /token_plan/remains payload
// (live-captured) used to drive the minimax success arms.
const minimaxQuotaSuccessBody = `{"model_remains":[{"start_time":1784473200000,"end_time":1784491200000,"remains_time":16420081,"current_interval_total_count":0,"current_interval_usage_count":0,"model_name":"general","current_weekly_total_count":0,"current_weekly_usage_count":0,"weekly_start_time":1783900800000,"weekly_end_time":1784505600000,"weekly_remains_time":30820081,"current_interval_status":1,"current_interval_remaining_percent":100,"current_weekly_status":1,"current_weekly_remaining_percent":100},{"start_time":1784419200000,"end_time":1784505600000,"remains_time":30820081,"current_interval_total_count":0,"current_interval_usage_count":0,"model_name":"video","current_weekly_total_count":0,"current_weekly_usage_count":0,"weekly_start_time":1783900800000,"weekly_end_time":1784505600000,"weekly_remains_time":30820081,"current_interval_status":3,"current_interval_remaining_percent":100,"current_weekly_status":3,"current_weekly_remaining_percent":100}],"base_resp":{"status_code":0,"status_msg":"success"}}`

// createQuotaProvider creates a provider (in the real test DB, so the quota
// snapshot FK is satisfied) via the router and returns its parsed UUID plus the
// raw string form for path building. The key is encrypted under the handler's
// MasterKey, so the read-through cold-fill can decrypt it.
func createQuotaProvider(t *testing.T, r chi.Router, baseURL string) (uuid.UUID, string) {
	t.Helper()
	providerName := fmt.Sprintf("test-quota-%s", uuid.New().String()[:8])
	body := fmt.Sprintf(`{"name": "%s", "base_url": "%s", "api_key": "fake-key"}`, providerName, baseURL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("failed to parse create response: %v", err)
	}
	id, err := uuid.Parse(createResp.ID)
	if err != nil {
		t.Fatalf("invalid provider id %q: %v", createResp.ID, err)
	}
	return id, createResp.ID
}

// doQuotaGet issues an authenticated GET to a quota endpoint path.
func doQuotaGet(t *testing.T, r chi.Router, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	return rec
}
