package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDiscoverAllModels_AllDisabled tests that DiscoverAllModels skips all
// disabled providers and returns an empty result structure.
func TestDiscoverAllModels_AllDisabled(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create providers and then disable them (CreateProviderRequest doesn't have enabled field)
	var providerIDs []string
	for i := 0; i < 3; i++ {
		providerData := fmt.Sprintf(`{"name": "test-disabled-all-%d", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`, i)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %d: %d: %s", i, rec.Code, rec.Body.String())
		}

		var createResp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		providerIDs = append(providerIDs, createResp.ID)
	}

	// Disable all providers
	for _, id := range providerIDs {
		updateData := `{"enabled": false}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/providers/"+id, strings.NewReader(updateData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Failed to disable provider %s: %d: %s", id, rec.Code, rec.Body.String())
		}
	}

	// Run discover-all
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results    []interface{} `json:"results"`
		Succeeded  int           `json:"succeeded"`
		Failed     int           `json:"failed"`
		Discovered int           `json:"discovered"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// All providers should be skipped (disabled), so results should be empty
	if len(response.Results) != 0 {
		t.Errorf("Expected empty results (all providers disabled), got %d", len(response.Results))
	}
	if response.Succeeded != 0 {
		t.Errorf("Expected succeeded=0, got %d", response.Succeeded)
	}
	if response.Failed != 0 {
		t.Errorf("Expected failed=0, got %d", response.Failed)
	}
	if response.Discovered != 0 {
		t.Errorf("Expected discovered=0, got %d", response.Discovered)
	}
}

// TestGetProviderUsage_InvalidUUID tests that GetProviderUsage returns 400 for
// an invalid UUID in the path.
func TestGetProviderUsage_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/invalid-uuid/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestGetProviderBalance_InvalidUUID tests that GetProviderBalance returns 400 for
// an invalid UUID in the path.
func TestGetProviderBalance_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/providers/invalid-uuid/balance", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestRefreshAllQuotas_AllDisabled tests that RefreshAllQuotas skips all
// disabled providers.
func TestRefreshAllQuotas_AllDisabled(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create multiple disabled providers
	for i := 0; i < 2; i++ {
		providerData := fmt.Sprintf(`{"name": "test-quota-disabled-%d", "base_url": "https://api.nanogpt.com", "api_key": "test-key", "enabled": false}`, i)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %d: %d: %s", i, rec.Code, rec.Body.String())
		}
	}

	// Run refresh-quotas
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Results   []interface{} `json:"results"`
		Refreshed int           `json:"refreshed"`
		Failed    int           `json:"failed"`
		Skipped   int           `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// All providers should be skipped (disabled) - they are silently skipped
	// without incrementing the skipped counter (which is only for unsupported types)
	if len(response.Results) != 0 {
		t.Errorf("Expected empty results (all providers disabled), got %d", len(response.Results))
	}
	if response.Refreshed != 0 {
		t.Errorf("Expected refreshed=0, got %d", response.Refreshed)
	}
	if response.Failed != 0 {
		t.Errorf("Expected failed=0, got %d", response.Failed)
	}
	// Note: skipped counter is only for unsupported provider types, not disabled ones
}
