package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestPurgeLogs(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	testCases := []struct {
		name       string
		olderThan  string
		expectCode int
	}{
		{"Invalid time range", "invalid", http.StatusBadRequest},
		{"1 hour", "1h", http.StatusNoContent},
		{"1 day", "1d", http.StatusNoContent},
		{"1 week", "1w", http.StatusNoContent},
		{"1 month", "1m", http.StatusNoContent},
		{"All logs", "all", http.StatusNoContent},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			purgeData := fmt.Sprintf(`{"older_than": %q}`, tc.olderThan)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(purgeData))
			req.Header.Set("Authorization", "Bearer test-admin-token")
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			if rec.Code != tc.expectCode {
				t.Errorf("Expected %d for %s, got %d: %s", tc.expectCode, tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestListLogs(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []map[string]any `json:"entries"`
		Total   int              `json:"total"`
		Page    int              `json:"page"`
		PerPage int              `json:"per_page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return empty list when no logs exist
	if len(response.Entries) != 0 {
		t.Errorf("Expected empty log list, got %d entries", len(response.Entries))
	}
	if response.Total != 0 {
		t.Errorf("Expected total 0, got %d", response.Total)
	}
}

// Stats Tests with data

// TestGetStats_WithLogs tests the stats endpoint with actual request logs

func TestListLogs_WithProviderIDFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create two providers
	body1 := `{"name":"test-logs-provider-1","base_url":"https://api.openai.com","api_key":"sk-test1"}`
	req1 := httptest.NewRequest("POST", "/providers", strings.NewReader(body1))
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	body2 := `{"name":"test-logs-provider-2","base_url":"https://api.anthropic.com","api_key":"sk-test2"}`
	req2 := httptest.NewRequest("POST", "/providers", strings.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var resp1, resp2 map[string]any
	json.NewDecoder(w1.Body).Decode(&resp1)
	json.NewDecoder(w2.Body).Decode(&resp2)
	providerID1 := resp1["id"].(string)
	providerID2 := resp2["id"].(string)

	// Insert test logs for provider 1
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID1), "gpt-4", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Insert test logs for provider 2
	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID2), "claude-3", 200, 1500, 150, 250, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by provider_id
	req := httptest.NewRequest("GET", "/logs?provider_id="+providerID1, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	json.NewDecoder(w.Body).Decode(&response)
	entries := response["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry for provider 1, got %d", len(entries))
	}
}

// TestListLogs_WithEndpointTypeFilter tests filtering logs by endpoint_type
// and that the field is exposed in the response.

func TestListLogs_WithEndpointTypeFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	body := `{"name":"test-logs-provider-ep","base_url":"https://api.openai.com","api_key":"sk-test-ep"}`
	reqCreate := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	reqCreate.Header.Set("Authorization", "Bearer test-admin-token")
	reqCreate.Header.Set("Content-Type", "application/json")
	wCreate := httptest.NewRecorder()
	r.ServeHTTP(wCreate, reqCreate)

	var created map[string]any
	json.NewDecoder(wCreate.Body).Decode(&created)
	providerID := created["id"].(string)

	// One embeddings request and one chat request (default endpoint_type).
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, endpoint_type, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.MustParse(providerID), "text-embedding-3-small", 200, 50, 8, 0, "embeddings", time.Now())
	if err != nil {
		t.Fatalf("Failed to insert embeddings log: %v", err)
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert chat log: %v", err)
	}

	// Filter by endpoint_type=embeddings (scoped to this provider so the
	// shared test DB doesn't bleed rows from other tests).
	req := httptest.NewRequest("GET", "/logs?endpoint_type=embeddings&provider_id="+providerID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		Entries []struct {
			ModelID      string `json:"model_id"`
			EndpointType string `json:"endpoint_type"`
		} `json:"entries"`
	}
	json.NewDecoder(w.Body).Decode(&response)
	if len(response.Entries) != 1 {
		t.Fatalf("expected 1 embeddings log entry, got %d", len(response.Entries))
	}
	if response.Entries[0].EndpointType != "embeddings" {
		t.Errorf("endpoint_type = %q, want embeddings", response.Entries[0].EndpointType)
	}
	if response.Entries[0].ModelID != "text-embedding-3-small" {
		t.Errorf("model_id = %q, want text-embedding-3-small", response.Entries[0].ModelID)
	}

	// chat filter must return the chat row (legacy rows default to 'chat').
	req2 := httptest.NewRequest("GET", "/logs?endpoint_type=chat&provider_id="+providerID, http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var response2 struct {
		Entries []struct {
			EndpointType string `json:"endpoint_type"`
		} `json:"entries"`
	}
	json.NewDecoder(w2.Body).Decode(&response2)
	if len(response2.Entries) != 1 {
		t.Fatalf("expected 1 chat log entry, got %d", len(response2.Entries))
	}
	if response2.Entries[0].EndpointType != "chat" {
		t.Errorf("endpoint_type = %q, want chat", response2.Entries[0].EndpointType)
	}

	// Unknown endpoint_type values are ignored (no filter): both rows return.
	req3 := httptest.NewRequest("GET", "/logs?endpoint_type=bogus&provider_id="+providerID, http.NoBody)
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	var response3 struct {
		Entries []json.RawMessage `json:"entries"`
	}
	json.NewDecoder(w3.Body).Decode(&response3)
	if len(response3.Entries) != 2 {
		t.Errorf("expected 2 entries with unknown endpoint_type filter (ignored), got %d", len(response3.Entries))
	}
}

// TestListLogs_WithModelIDFilter tests filtering logs by model_id

func TestListLogs_WithModelIDFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-model-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert test logs with different models
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4-turbo", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-3.5-turbo", 200, 800, 80, 160, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by model_id (partial match)
	req2 := httptest.NewRequest("GET", "/logs?model_id=gpt-4", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]any
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry for gpt-4 model, got %d", len(entries))
	}
}

// TestListLogs_WithVirtualKeyIDFilter tests filtering logs by virtual_key_id

func TestListLogs_WithVirtualKeyFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-vk-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Create a virtual key
	vkBody := `{"name":"test-vk-logs"}`
	req2 := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(vkBody))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var vkResp map[string]any
	json.NewDecoder(w2.Body).Decode(&vkResp)
	virtualKeyID := vkResp["id"].(string)
	virtualKeyName := vkResp["name"].(string)

	// Insert test log with virtual key
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, virtual_key_id, virtual_key_name, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uuid.MustParse(providerID), "gpt-4", uuid.MustParse(virtualKeyID), virtualKeyName, 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Test logs endpoint (should include virtual key info)
	req3 := httptest.NewRequest("GET", "/logs", http.NoBody)
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w3.Code, w3.Body.String())
	}

	var response map[string]any
	json.NewDecoder(w3.Body).Decode(&response)
	entries := response["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(entries))
	}
}

// TestGetProviderUsage_Error tests the error path when discovery service returns an error

func TestListLogs_WithStatusCodeFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-status-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert test logs with different status codes
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.MustParse(providerID), "gpt-4", 500, 2000, 0, 0, time.Now(), "Internal error")
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by 5xx status codes
	req2 := httptest.NewRequest("GET", "/logs?status_code=5xx", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]any
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry with 5xx status, got %d", len(entries))
	}
}

// TestListLogs_WithDateRangeFilter tests filtering logs by date range

func TestListLogs_WithDateRangeFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-date-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert test logs with different timestamps
	now := time.Now().UTC()
	// Use specific times that are clearly separated
	oldTime := now.Add(-2 * time.Hour)
	newTime := now.Add(2 * time.Hour)

	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, oldTime)
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, newTime)
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by date range - only logs from 1 hour ago onwards (should get only newTime log)
	fromTime := now.Add(-1 * time.Hour)
	req2 := httptest.NewRequest("GET", "/logs?from="+fromTime.Format(time.RFC3339), http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]any
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]any)
	// Should only get the newTime log (1 entry)
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry in date range, got %d", len(entries))
	}
}

// TestDiscoverProviderModels_WithInvalidProviderType tests discovery on a provider with unsupported type

func TestListLogs_WithPagination(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-pagination","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert multiple test logs
	pool := h.Pool().Pool()
	for range 5 {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, time.Now())
		if err != nil {
			t.Fatalf("Failed to insert test log: %v", err)
		}
	}

	// Test with page=2, per_page=2
	req2 := httptest.NewRequest("GET", "/logs?page=2&per_page=2", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]any
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]any)
	total := response["total"].(float64)
	page := response["page"].(float64)
	perPage := response["per_page"].(float64)

	if total != 5 {
		t.Errorf("expected total=5, got %v", total)
	}
	if page != 2 {
		t.Errorf("expected page=2, got %v", page)
	}
	if perPage != 2 {
		t.Errorf("expected per_page=2, got %v", perPage)
	}
	// Page 2 with per_page=2 should return 2 entries (entries 3-4)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries on page 2, got %d", len(entries))
	}
}

// TestListLogs_With4xxStatusCodeFilter tests filtering by 4xx status codes

func TestListLogs_With4xxStatusCodeFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-4xx","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert test logs with different status codes
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.MustParse(providerID), "gpt-4", 429, 500, 0, 0, time.Now(), "Rate limit exceeded")
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by 4xx status codes
	req2 := httptest.NewRequest("GET", "/logs?status_code=4xx", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]any
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]any)
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry with 4xx status, got %d", len(entries))
	}
}

// TestDiscoverProviderModels_SuccessPath tests the success path where discovery works

func TestPurgeLogs_BeforeTimestamp(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first (request_logs has FK to providers)
	providerData := fmt.Sprintf(`{"name":"test-purge-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert some old request logs
	pool := h.Pool().Pool()
	now := time.Now().UTC()
	oldTime := now.Add(-48 * time.Hour) // 2 days ago

	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		providerResp.ID, "gpt-4", 200, 100, oldTime)
	if err != nil {
		t.Fatalf("Failed to insert old log: %v", err)
	}

	// Purge logs before 24 hours ago
	purgeData := `{"older_than": "1d"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(purgeData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for purge logs before timestamp, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPurgeLogs_KeepDays tests purging logs older than 1 week

func TestPurgeLogs_KeepDays(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first (request_logs has FK to providers)
	providerData := fmt.Sprintf(`{"name":"test-keep-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert some old request logs
	pool := h.Pool().Pool()
	now := time.Now().UTC()
	oldTime := now.Add(-10 * 24 * time.Hour) // 10 days ago

	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		providerResp.ID, "gpt-4", 200, 100, oldTime)
	if err != nil {
		t.Fatalf("Failed to insert old log: %v", err)
	}

	// Purge logs older than 2024-01-01
	purgeData := `{"older_than":"1w"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(purgeData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for purge logs keep days, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPurgeLogs_InvalidData tests purge with invalid request data

func TestPurgeLogs_InvalidData(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	t.Run("InvalidJSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(`{invalid json}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for invalid JSON, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("EmptyBody", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for empty body, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestUpdateProvider_EnableDisable tests enabling and disabling a provider

// TestListLogs_SortByProvider covers the tier-expression branch of the ListLogs
// ORDER BY builder (sortColumns["provider"] has a non-empty tierExpr).
func TestListLogs_SortByProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs?sort_by=provider&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListLogs_SortByStatus covers the status-specific secondary ORDER BY clause
// in ListLogs (sortBy == "status").
func TestListLogs_SortByStatus(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/logs?sort_by=status", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// createLogTestKey creates a virtual key via the API and returns its id.
func createLogTestKey(t *testing.T, r chi.Router, name string) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(fmt.Sprintf(`{"name":%q}`, name)))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("create virtual key: %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode virtual key: %v", err)
	}
	return resp["id"].(string)
}

// insertLogRowForKey inserts a minimal request_logs row bound to a virtual key,
// optionally carrying a client IP.
func insertLogRowForKey(t *testing.T, h *Handler, vkID, vkName, clientIP string) string {
	t.Helper()
	id := uuid.NewString()
	var ip any
	if clientIP != "" {
		ip = clientIP
	}
	_, err := h.Pool().Pool().Exec(context.Background(), `
		INSERT INTO request_logs (id, model_id, virtual_key_id, virtual_key_name, client_ip, status_code, state, created_at)
		VALUES ($1, 'gpt-4', $2, $3, $4, 200, 'completed', $5)`,
		id, uuid.MustParse(vkID), vkName, ip, time.Now())
	if err != nil {
		t.Fatalf("insert request log: %v", err)
	}
	return id
}

// TestListLogs_FilterByVirtualKeyID verifies ?virtual_key_id= narrows the offset
// list to that key's rows only.
func TestListLogs_FilterByVirtualKeyID(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	vk1 := createLogTestKey(t, r, "vk-logs-filter-a")
	vk2 := createLogTestKey(t, r, "vk-logs-filter-b")
	insertLogRowForKey(t, h, vk1, "vk-logs-filter-a", "")
	insertLogRowForKey(t, h, vk2, "vk-logs-filter-b", "")

	req := httptest.NewRequest(http.MethodGet, "/logs?virtual_key_id="+vk1, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	entries := resp["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for virtual_key_id filter, got %d", len(entries))
	}
	if got := entries[0].(map[string]any)["virtual_key_id"]; got != vk1 {
		t.Errorf("entry virtual_key_id = %v, want %s", got, vk1)
	}
}

// TestListLogsCursor_FilterByVirtualKeyID verifies the same filter on the
// cursor (scroll-mode) endpoint.
func TestListLogsCursor_FilterByVirtualKeyID(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	vk1 := createLogTestKey(t, r, "vk-cursor-filter-a")
	vk2 := createLogTestKey(t, r, "vk-cursor-filter-b")
	insertLogRowForKey(t, h, vk1, "vk-cursor-filter-a", "")
	insertLogRowForKey(t, h, vk2, "vk-cursor-filter-b", "")

	req := httptest.NewRequest(http.MethodGet, "/logs/cursor?virtual_key_id="+vk2, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	entries := resp["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for virtual_key_id filter, got %d", len(entries))
	}
	if got := entries[0].(map[string]any)["virtual_key_id"]; got != vk2 {
		t.Errorf("entry virtual_key_id = %v, want %s", got, vk2)
	}
	if total := resp["total"].(float64); total != 1 {
		t.Errorf("total = %v, want 1", total)
	}
}

// TestListLogs_IncludesClientIP verifies the stored client IP is projected into
// list responses (empty string for rows predating the column).
func TestListLogs_IncludesClientIP(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	vk := createLogTestKey(t, r, "vk-client-ip")
	insertLogRowForKey(t, h, vk, "vk-client-ip", "203.0.113.7")

	req := httptest.NewRequest(http.MethodGet, "/logs?virtual_key_id="+vk, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	entries := resp["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if got := entries[0].(map[string]any)["client_ip"]; got != "203.0.113.7" {
		t.Errorf("client_ip = %v, want 203.0.113.7", got)
	}
}

// TestGetLog_IncludesClientIP verifies the single-row endpoint carries the
// client IP for the detail modal.
func TestGetLog_IncludesClientIP(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	vk := createLogTestKey(t, r, "vk-client-ip-get")
	id := insertLogRowForKey(t, h, vk, "vk-client-ip-get", "198.51.100.42")

	req := httptest.NewRequest(http.MethodGet, "/logs/"+id, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var entry map[string]any
	json.NewDecoder(w.Body).Decode(&entry)
	if got := entry["client_ip"]; got != "198.51.100.42" {
		t.Errorf("client_ip = %v, want 198.51.100.42", got)
	}
}

// TestListLogs_FilterByClientIP verifies ?client_ip= narrows both listing
// endpoints to rows from that exact address.
func TestListLogs_FilterByClientIP(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	vk := createLogTestKey(t, r, "vk-ip-filter")
	insertLogRowForKey(t, h, vk, "vk-ip-filter", "198.51.100.9")
	insertLogRowForKey(t, h, vk, "vk-ip-filter", "203.0.113.7")

	for _, path := range []string{
		"/logs?client_ip=198.51.100.9",
		"/logs/cursor?client_ip=198.51.100.9",
	} {
		req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(w.Body).Decode(&resp)
		entries := resp["entries"].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s: expected 1 entry for client_ip filter, got %d", path, len(entries))
		}
		if got := entries[0].(map[string]any)["client_ip"]; got != "198.51.100.9" {
			t.Errorf("%s: entry client_ip = %v, want 198.51.100.9", path, got)
		}
	}
}

// TestListLogs_SortByIP covers the ip sort key (tier expression puts rows
// without an IP last).
func TestListLogs_SortByIP(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	vk := createLogTestKey(t, r, "vk-sort-ip")
	insertLogRowForKey(t, h, vk, "vk-sort-ip", "")
	insertLogRowForKey(t, h, vk, "vk-sort-ip", "192.0.2.1")

	req := httptest.NewRequest(http.MethodGet, "/logs?sort_by=ip&sort_dir=asc&virtual_key_id="+vk, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	entries := resp["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if got := entries[0].(map[string]any)["client_ip"]; got != "192.0.2.1" {
		t.Errorf("first entry client_ip = %v, want 192.0.2.1 (empty IPs sort last)", got)
	}
}
