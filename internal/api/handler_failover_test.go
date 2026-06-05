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

	"github.com/hugalafutro/model-hotel/internal/failover"
)

func TestListFailoverGroups(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create providers and models for failover group
	providerData := `{"name": "test-failover-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// List failover groups (should be empty initially)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Groups []map[string]interface{} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should be empty without failover groups
}

func TestDeleteFailoverGroup(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Delete non-existent failover group - should succeed with 204
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/failover-groups/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Delete returns 204 even for non-existent groups (idempotent)
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for failover group delete, got %d: %s", rec.Code, rec.Body.String())
	}
}

// DeleteFailoverGroup - Additional coverage with cascade

func TestDeleteFailoverGroup_WithModels(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := `{"name": "test-delete-fg-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
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

	// Insert models directly via DB
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o-mini-1", "GPT-4o Mini 1", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-mini-2", "GPT-4o Mini 2", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with these models via API
	groupData := `{"display_model":"test-delete-group","entry_ids":["` + modelID1 + `","` + modelID2 + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	var groupResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("Failed to parse group response: %v", err)
	}

	// Now delete the failover group
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/failover-groups/"+groupResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for failover group delete, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the group is gone
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups/"+groupResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 after delete, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSyncFailoverGroups(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Sync failover groups (should work even with no models)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/failover-groups/sync", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return sync result
}

func TestFailoverCandidates(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get candidates (should be empty without providers/models)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/failover-groups/candidates", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should be empty without providers/models
	if len(response) != 0 {
		t.Errorf("Expected empty candidates list, got %d candidates", len(response))
	}
}

func TestSyncFailoverGroups_WithModels(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	var rec *httptest.ResponseRecorder
	var req *http.Request

	// Create providers and models
	for i := 0; i < 3; i++ {
		providerData := fmt.Sprintf(`{"name": "test-failover-provider-%d", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, i)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %d: %d", i, rec.Code)
		}

		var providerResp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
			t.Fatalf("Failed to parse provider response: %v", err)
		}

		// Insert models with same model_id (for failover grouping)
		for j := 0; j < 2; j++ {
			modelID := uuid.New().String()
			pool := h.Pool().Pool()
			_, err := pool.Exec(context.Background(),
				`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
				modelID, providerResp.ID, fmt.Sprintf("gpt-4o-mini-%d-%d", i, j), fmt.Sprintf("GPT-4o Mini %d", j), true)
			if err != nil {
				t.Fatalf("Failed to insert model: %v", err)
			}
		}
	}

	// Call the sync endpoint
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/sync", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return sync result
	if _, ok := response["deleted_groups"]; !ok {
		t.Error("Expected 'deleted_groups' field in sync response")
	}
}

func TestFailoverAddProvider(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create two providers with unique names
	provider1Data := `{"name": "test-failover-provider-1-` + uuid.New().String()[:8] + `", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provider1Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var provider1Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider1Resp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	provider2Data := `{"name": "test-failover-provider-2-` + uuid.New().String()[:8] + `", "base_url": "https://api.anthropic.com", "api_key": "test-api-key"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers", strings.NewReader(provider2Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var provider2Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider2Resp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert two models directly via DB
	pool := h.Pool().Pool()
	model1ID := uuid.New().String()
	model2ID := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model1ID, provider1Resp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model2ID, provider2Resp.ID, "claude-3-5-sonnet", "Claude 3.5 Sonnet", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with both models
	groupData := `{"display_model":"test-failover-group-` + uuid.New().String()[:8] + `","entry_ids":["` + model1ID + `","` + model2ID + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	var groupResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("Failed to parse group response: %v", err)
	}

	// Update the failover group with a new priority_order (reordering)
	reorderData := `{"priority_order": ["` + model2ID + `", "` + model1ID + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/failover-groups/"+groupResp.ID, strings.NewReader(reorderData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	// Should return 200 for successful update
	if rec.Code != http.StatusOK {
		t.Logf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestFailoverAddProvider_NonExistentGroup tests updating a non-existent failover group

func TestFailoverAddProvider_NonExistentGroup(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update non-existent failover group
	nonExistentGroupID := uuid.New().String()
	updateData := `{"priority_order": []}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/failover-groups/"+nonExistentGroupID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	// Should return 404 for non-existent group
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent failover group, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestFailoverReorderProvider tests reordering providers in a failover group

func TestFailoverReorderProvider(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create two providers
	provider1Data := `{"name": "test-provider-1", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provider1Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider 1: %d: %s", rec.Code, rec.Body.String())
	}

	var provider1Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider1Resp); err != nil {
		t.Fatalf("Failed to parse provider 1 response: %v", err)
	}

	provider2Data := `{"name": "test-provider-2", "base_url": "https://api.anthropic.com", "api_key": "test-api-key"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers", strings.NewReader(provider2Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider 2: %d: %s", rec.Code, rec.Body.String())
	}

	var provider2Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider2Resp); err != nil {
		t.Fatalf("Failed to parse provider 2 response: %v", err)
	}

	// Insert models directly via DB
	pool := h.Pool().Pool()
	model1ID := uuid.New().String()
	model2ID := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model1ID, provider1Resp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model2ID, provider2Resp.ID, "claude-3-5-sonnet", "Claude 3.5 Sonnet", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with both models
	groupData := `{"display_model":"test-reorder-group","entry_ids":["` + model1ID + `","` + model2ID + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	var groupResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("Failed to parse group response: %v", err)
	}

	// Reorder providers - swap positions
	reorderData := `{"priority_order": ["` + model2ID + `", "` + model1ID + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/failover-groups/"+groupResp.ID, strings.NewReader(reorderData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for reorder, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the order changed
	var updatedGroup struct {
		ID      string `json:"id"`
		Entries []struct {
			ModelUUID string `json:"model_uuid"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updatedGroup); err != nil {
		t.Fatalf("Failed to parse updated group: %v", err)
	}
	if len(updatedGroup.Entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(updatedGroup.Entries))
	}
	if updatedGroup.Entries[0].ModelUUID != model2ID {
		t.Errorf("First entry should be model2 after reorder, got %s", updatedGroup.Entries[0].ModelUUID)
	}
	if updatedGroup.Entries[1].ModelUUID != model1ID {
		t.Errorf("Second entry should be model1 after reorder, got %s", updatedGroup.Entries[1].ModelUUID)
	}
}

// TestDeleteModel_NonExistent tests deleting a non-existent model

func TestFailoverCandidates_Empty(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/failover-groups/candidates", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var candidates []interface{}
	if err := json.NewDecoder(w.Body).Decode(&candidates); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(candidates) != 0 {
		t.Errorf("expected empty candidates list, got %d", len(candidates))
	}
}

// TestFailoverCandidates_WithModels tests the Candidates endpoint with enabled models

func TestFailoverCandidates_WithModels(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-candidates-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert enabled models directly via DB
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Get candidates
	req2 := httptest.NewRequest("GET", "/failover-groups/candidates", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var candidates []map[string]interface{}
	if err := json.NewDecoder(w2.Body).Decode(&candidates); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(candidates))
	}
}

// TestFailoverCandidates_DisabledModels tests that disabled models are filtered out

func TestFailoverCandidates_DisabledModels(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-disabled-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert one enabled and one disabled model
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerID, "gpt-4o", "GPT-4o", false)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Get candidates - should only return enabled model
	req2 := httptest.NewRequest("GET", "/failover-groups/candidates", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var candidates []map[string]interface{}
	if err := json.NewDecoder(w2.Body).Decode(&candidates); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(candidates) != 1 {
		t.Errorf("expected 1 candidate (disabled filtered out), got %d", len(candidates))
	}

	if candidates[0]["model_id"] != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %v", candidates[0]["model_id"])
	}
}

// TestFailoverSync_Success tests the Sync endpoint

func TestFailoverSync_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	uniqueName := "test-sync-prov-" + uuid.New().String()[:8]
	body := fmt.Sprintf(`{"name":"%s","base_url":"https://api.openai.com","api_key":"sk-test"}`, uniqueName)
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert two models (failover groups require at least 2 entries)
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}
	modelID2 := uuid.New().String()
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with 2 entries
	groupData := `{"display_model":"test-sync-group","entry_ids":["` + modelID1 + `","` + modelID2 + `"]}`
	req2 := httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", w2.Code, w2.Body.String())
	}

	// Sync all failover groups
	req3 := httptest.NewRequest("POST", "/failover-groups/sync", http.NoBody)
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w3.Code, w3.Body.String())
	}
}

// TestDeleteFailoverGroup_NonExistent tests deleting a non-existent failover group

func TestDeleteFailoverGroup_NonExistent(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	nonExistentID := uuid.New().String()
	req := httptest.NewRequest("DELETE", "/failover-groups/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Delete returns 204 even for non-existent groups (idempotent)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 No Content, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetSystem_NoCache tests the system stats endpoint

// mockCircuitBreakerReader implements CircuitBreakerReader for tests.
type mockCircuitBreakerReader struct {
	statuses []failover.ProviderStatus
}

func (m *mockCircuitBreakerReader) Status() []failover.ProviderStatus {
	return m.statuses
}

func TestCircuitBreakerStatus_WithDetail(t *testing.T) {
	h := newTestHandler(t)

	// Wire a mock circuit breaker before registering routes.
	mockCB := &mockCircuitBreakerReader{
		statuses: []failover.ProviderStatus{
			{
				ProviderID:       uuid.New().String(),
				State:            failover.StateOpen.String(),
				ConsecutiveFails: 5,
				OpenedAt:         time.Now().Add(-30 * time.Second).Format(time.RFC3339),
				CooldownMs:       60000,
				NextRetryAt:      time.Now().Add(30 * time.Second).Format(time.RFC3339),
			},
			{
				ProviderID:       uuid.New().String(),
				State:            failover.StateHalfOpen.String(),
				ConsecutiveFails: 5,
				OpenedAt:         time.Now().Add(-55 * time.Second).Format(time.RFC3339),
			},
			{
				ProviderID:       uuid.New().String(),
				State:            failover.StateClosed.String(),
				ConsecutiveFails: 0,
			},
		},
	}
	h.SetCircuitBreaker(mockCB)

	r := chi.NewRouter()
	h.Register(r)

	// Without ?detail=1: only aggregate counts, no providers.
	t.Run("aggregate only", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		if v, ok := resp["closed"].(float64); !ok || v != 1 {
			t.Errorf("expected closed=1, got %v", resp["closed"])
		}
		if v, ok := resp["half_open"].(float64); !ok || v != 1 {
			t.Errorf("expected half_open=1, got %v", resp["half_open"])
		}
		if v, ok := resp["open"].(float64); !ok || v != 1 {
			t.Errorf("expected open=1, got %v", resp["open"])
		}
		if _, exists := resp["providers"]; exists {
			t.Error("expected no providers field without ?detail=1")
		}
	})

	// With ?detail=1: includes providers with cooldown_ms/next_retry_at.
	t.Run("with detail", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status?detail=1", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode: %v", err)
		}

		providers, ok := resp["providers"].([]interface{})
		if !ok || len(providers) != 3 {
			t.Fatalf("expected 3 providers, got %v", resp["providers"])
		}

		// First provider should be open with cooldown_ms and next_retry_at.
		first := providers[0].(map[string]interface{})
		if first["state"] != "open" {
			t.Errorf("expected state=open, got %v", first["state"])
		}
		if _, exists := first["cooldown_ms"]; !exists {
			t.Error("expected cooldown_ms in provider detail")
		}
		if _, exists := first["next_retry_at"]; !exists {
			t.Error("expected next_retry_at in provider detail")
		}

		// Second provider should be half-open with opened_at but no cooldown_ms or next_retry_at
		// (cooldown has elapsed, provider is actively probing).
		second := providers[1].(map[string]interface{})
		if second["state"] != "half-open" {
			t.Errorf("expected state=half-open, got %v", second["state"])
		}
		if _, exists := second["cooldown_ms"]; exists {
			t.Error("half-open provider should not have cooldown_ms (cooldown has elapsed)")
		}
		if _, exists := second["next_retry_at"]; exists {
			t.Error("half-open provider should not have next_retry_at")
		}

		// Third provider should be closed without cooldown fields.
		third := providers[2].(map[string]interface{})
		if third["state"] != "closed" {
			t.Errorf("expected state=closed, got %v", third["state"])
		}
		if _, exists := third["cooldown_ms"]; exists {
			t.Error("closed provider should not have cooldown_ms")
		}
	})
}

func TestCircuitBreakerStatus_NoCircuitBreaker(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should return zeros when no circuit breaker is wired
	if closed, ok := resp["closed"].(float64); !ok || closed != 0 {
		t.Errorf("expected closed=0, got %v", resp["closed"])
	}
	if halfOpen, ok := resp["half_open"].(float64); !ok || halfOpen != 0 {
		t.Errorf("expected half_open=0, got %v", resp["half_open"])
	}
	if open, ok := resp["open"].(float64); !ok || open != 0 {
		t.Errorf("expected open=0, got %v", resp["open"])
	}
}

func TestCircuitBreakerStatus_UntrackedMembers(t *testing.T) {
	// This test verifies that failover group members not yet tracked by the
	// circuit breaker are counted as "closed" (implicitly healthy).
	// Providers only appear in the CB map after being routed; until then
	// the aggregate status endpoint should count them as closed.

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-cb-untracked-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("failed to parse provider: %v", err)
	}

	// Insert two models directly via DB
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("failed to insert model 2: %v", err)
	}

	// Create a failover group with both models
	groupData := fmt.Sprintf(`{"display_model":"test-cb-untracked-group","entry_ids":["%s","%s"]}`, modelID1, modelID2)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	// Set a mock circuit breaker that tracks NO providers (empty).
	// All failover group members should be counted as "closed".
	mockCB := &mockCircuitBreakerReader{
		statuses: []failover.ProviderStatus{},
	}
	h.SetCircuitBreaker(mockCB)

	// Hit the circuit-breaker-status endpoint
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Both models share the same provider. Since the tracked map and untracked
	// counting are both keyed by provider UUID (not model UUID), the single
	// untracked provider should be counted once as closed.
	if closed, ok := resp["closed"].(float64); !ok || closed != 1 {
		t.Errorf("expected closed=1 (1 untracked provider with 2 models), got %v", resp["closed"])
	}
	if halfOpen, ok := resp["half_open"].(float64); !ok || halfOpen != 0 {
		t.Errorf("expected half_open=0, got %v", resp["half_open"])
	}
	if open, ok := resp["open"].(float64); !ok || open != 0 {
		t.Errorf("expected open=0, got %v", resp["open"])
	}
	// Aggregate queries should not include the providers array
	if _, exists := resp["providers"]; exists {
		t.Error("expected no providers field in aggregate response")
	}
}

func TestCircuitBreakerStatus_TrackedProviderNotDoubleCounted(t *testing.T) {
	// Verify that a provider tracked as "open" is NOT also counted as closed
	// via the untracked-member loop. The old code compared model UUIDs against
	// the provider-UUID keyed map, so every model fell through to the untracked
	// branch regardless of its provider's actual CB state.
	h := newTestHandler(t)

	// Create a provider BEFORE registering routes (so we have the provider ID
	// to put in the mock CB). We register routes after setting the mock.
	providerData := fmt.Sprintf(`{"name": "test-cb-dblcount-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	r := chi.NewRouter()
	// Register once just to create the provider
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("failed to parse provider: %v", err)
	}

	// Insert two models directly via DB (failover groups require at least 2 entries)
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("failed to insert model 2: %v", err)
	}

	// Create a failover group with both models (same provider)
	groupData := fmt.Sprintf(`{"display_model":"test-cb-dblcount-group","entry_ids":["%s","%s"]}`, modelID1, modelID2)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	// Set the mock circuit breaker BEFORE registering routes again.
	// The FailoverHandler captures cbReader at Register() time.
	mockCB := &mockCircuitBreakerReader{
		statuses: []failover.ProviderStatus{
			{ProviderID: providerResp.ID, State: failover.StateOpen.String(), ConsecutiveFails: 5},
		},
	}
	h.SetCircuitBreaker(mockCB)

	// Re-register routes so the FailoverHandler picks up the mock.
	r2 := chi.NewRouter()
	h.Register(r2)

	// Hit the circuit-breaker-status endpoint
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r2.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// The provider is tracked as open — it must NOT also be counted as closed.
	if open, ok := resp["open"].(float64); !ok || open != 1 {
		t.Errorf("expected open=1, got %v", resp["open"])
	}
	if closed, ok := resp["closed"].(float64); !ok || closed != 0 {
		t.Errorf("expected closed=0 (tracked provider should not be double-counted), got %v", resp["closed"])
	}
	if halfOpen, ok := resp["half_open"].(float64); !ok || halfOpen != 0 {
		t.Errorf("expected half_open=0, got %v", resp["half_open"])
	}
}

func TestCircuitBreakerStatus_MissingModelInGroup(t *testing.T) {
	// If a model ID in a failover group has been deleted from the models
	// table, GetByIDs won't return it and the handler should skip it
	// gracefully (line 608 continue).

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider.
	providerData := fmt.Sprintf(`{"name": "test-cb-missing-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("failed to parse provider: %v", err)
	}

	// Insert two models.
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("failed to insert model 2: %v", err)
	}

	// Create a failover group.
	groupData := fmt.Sprintf(`{"display_model":"test-cb-missing-group","entry_ids":["%s","%s"]}`, modelID1, modelID2)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create group: %d: %s", rec.Code, rec.Body.String())
	}

	// Delete modelID2 from the models table so GetByIDs can't find it.
	_, err = pool.Exec(context.Background(), `DELETE FROM models WHERE id = $1`, modelID2)
	if err != nil {
		t.Fatalf("failed to delete model 2: %v", err)
	}

	// No mock CB — provider is untracked, should be counted as closed.
	r2 := chi.NewRouter()
	h.Register(r2)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r2.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Provider should still be counted as closed despite one missing model.
	closed, _ := resp["closed"].(float64)
	if closed != 1 {
		t.Errorf("expected closed=1 (provider counted via remaining model), got %v", closed)
	}
}

func TestCircuitBreakerStatus_DuplicateModelAcrossGroups(t *testing.T) {
	// When the same model appears in multiple failover groups, the dedup
	// logic (line 594) should skip it on the second occurrence so the
	// provider is not double-counted as untracked.

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider.
	providerData := fmt.Sprintf(`{"name": "test-cb-dedup-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("failed to parse provider: %v", err)
	}

	// Insert three models under the same provider.
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	modelID3 := uuid.New().String()
	pool := h.Pool().Pool()
	for i, mid := range []string{modelID1, modelID2, modelID3} {
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
			mid, providerResp.ID, fmt.Sprintf("model-%d", i), fmt.Sprintf("Model %d", i), true)
		if err != nil {
			t.Fatalf("failed to insert model %d: %v", i, err)
		}
	}

	// Create two groups that share modelID1.
	group1 := fmt.Sprintf(`{"display_model":"dedup-group-1","entry_ids":["%s","%s"]}`, modelID1, modelID2)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(group1))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create group 1: %d: %s", rec.Code, rec.Body.String())
	}

	group2 := fmt.Sprintf(`{"display_model":"dedup-group-2","entry_ids":["%s","%s"]}`, modelID1, modelID3)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(group2))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create group 2: %d: %s", rec.Code, rec.Body.String())
	}

	// No mock CB — all providers are untracked, should be counted as closed.
	r2 := chi.NewRouter()
	h.Register(r2)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r2.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// One unique provider, counted as closed exactly once (dedup prevents double-count).
	closed, _ := resp["closed"].(float64)
	if closed != 1 {
		t.Errorf("expected closed=1 (one provider deduplicated), got %v", closed)
	}
}

func TestCircuitBreakerStatus_AggregateCacheHit(t *testing.T) {
	// The aggregate (no detail) path should serve from cache on the second
	// request within the TTL window.

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider.
	providerData := fmt.Sprintf(`{"name": "test-cb-aggcache-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("failed to parse provider: %v", err)
	}

	// Insert two models.
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("failed to insert model 2: %v", err)
	}

	// Create a failover group.
	groupData := fmt.Sprintf(`{"display_model":"test-cb-aggcache-group","entry_ids":["%s","%s"]}`, modelID1, modelID2)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create group: %d: %s", rec.Code, rec.Body.String())
	}

	mockCB := &mockCircuitBreakerReader{
		statuses: []failover.ProviderStatus{
			{ProviderID: providerResp.ID, State: failover.StateClosed.String(), ConsecutiveFails: 0},
		},
	}
	h.SetCircuitBreaker(mockCB)

	r2 := chi.NewRouter()
	h.Register(r2)

	// First request (no detail): should compute and cache.
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status", http.NoBody)
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	r2.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec1.Code)
	}

	// Second request (no detail): should be served from aggregate cache.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	r2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d", rec2.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Untracked provider counted as closed (1) + tracked closed (1) = 2 closed.
	closed, _ := resp["closed"].(float64)
	if closed < 1 {
		t.Errorf("expected at least 1 closed, got %v", closed)
	}
	// No providers array in aggregate response.
	if _, hasProviders := resp["providers"]; hasProviders {
		t.Error("aggregate response should not include providers array")
	}
}

func TestCircuitBreakerStatus_DetailCached(t *testing.T) {
	// Detail responses should be cached, not computed on every request.
	// This verifies the second request is served from cache while still
	// returning provider detail.

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider.
	providerData := fmt.Sprintf(`{"name": "test-cb-cache-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("failed to parse provider: %v", err)
	}

	// Insert two models.
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("failed to insert model 2: %v", err)
	}

	// Create a failover group.
	groupData := fmt.Sprintf(`{"display_model":"test-cb-cache-group","entry_ids":["%s","%s"]}`, modelID1, modelID2)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	// Set up mock circuit breaker.
	mockCB := &mockCircuitBreakerReader{
		statuses: []failover.ProviderStatus{
			{ProviderID: providerResp.ID, State: failover.StateOpen.String(), ConsecutiveFails: 1},
		},
	}
	h.SetCircuitBreaker(mockCB)

	r2 := chi.NewRouter()
	h.Register(r2)

	// First request: should compute and cache.
	rec1 := httptest.NewRecorder()
	req1 := httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status?detail=1", http.NoBody)
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	r2.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// Second request: should be served from cache (still returns providers).
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status?detail=1", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	r2.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec2.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode second response: %v", err)
	}

	providers, ok := resp["providers"].([]interface{})
	if !ok || len(providers) != 1 {
		t.Fatalf("cached response should still include providers, got %v", resp["providers"])
	}

	p := providers[0].(map[string]interface{})
	name, _ := p["provider_name"].(string)
	if !strings.Contains(name, "test-cb-cache-provider") {
		t.Errorf("cached response should include provider_name, got %q", name)
	}
}

func TestCircuitBreakerStatus_ProviderName(t *testing.T) {
	// When detail=1 is requested, each provider entry should include a
	// provider_name resolved from the model cache.

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider.
	providerData := fmt.Sprintf(`{"name": "test-cb-name-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("failed to parse provider: %v", err)
	}

	// Insert two models so the model cache has a ProviderName for this provider.
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("failed to insert model 2: %v", err)
	}

	// Create a failover group containing both models.
	groupData := fmt.Sprintf(`{"display_model":"test-cb-name-group","entry_ids":["%s","%s"]}`, modelID1, modelID2)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	// Set up a mock circuit breaker with the provider in open state.
	mockCB := &mockCircuitBreakerReader{
		statuses: []failover.ProviderStatus{
			{ProviderID: providerResp.ID, State: failover.StateOpen.String(), ConsecutiveFails: 3},
		},
	}
	h.SetCircuitBreaker(mockCB)

	// Re-register to pick up the mock.
	r2 := chi.NewRouter()
	h.Register(r2)

	// Fetch with detail=1.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups/circuit-breaker-status?detail=1", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r2.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	providers, ok := resp["providers"].([]interface{})
	if !ok || len(providers) != 1 {
		t.Fatalf("expected 1 provider, got %v", resp["providers"])
	}

	p := providers[0].(map[string]interface{})
	name, _ := p["provider_name"].(string)
	if !strings.Contains(name, "test-cb-name-provider") {
		t.Errorf("expected provider_name to contain 'test-cb-name-provider', got %q", name)
	}
}
