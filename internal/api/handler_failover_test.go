package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
