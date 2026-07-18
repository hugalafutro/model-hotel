package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestGetVirtualKey_NonExistent(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get non-existent virtual key
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/virtual-keys/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent virtual key, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateVirtualKey(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	t.Run("Success", func(t *testing.T) {
		// Create a virtual key first
		createBody := `{"name":"test-update-key"}`
		req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(createBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create virtual key: %d: %s", rec.Code, rec.Body.String())
		}

		var createResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		keyID := createResp["id"].(string)

		// Update the key's name
		updateBody := `{"name":"updated-key-name"}`
		req = httptest.NewRequest("PUT", "/virtual-keys/"+keyID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var updateResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &updateResp); err != nil {
			t.Fatalf("Failed to parse update response: %v", err)
		}
		if updateResp["name"] != "updated-key-name" {
			t.Errorf("Expected name 'updated-key-name', got %v", updateResp["name"])
		}
	})

	t.Run("ReservedName", func(t *testing.T) {
		// Create a virtual key first
		createBody := `{"name":"test-reserved-key"}`
		req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(createBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create virtual key: %d: %s", rec.Code, rec.Body.String())
		}

		var createResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		keyID := createResp["id"].(string)

		// Try to update to reserved name "admin"
		updateBody := `{"name":"admin"}`
		req = httptest.NewRequest("PUT", "/virtual-keys/"+keyID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for reserved name, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "reserved") {
			t.Errorf("Expected error about reserved name, got: %s", rec.Body.String())
		}
	})

	t.Run("EmptyName", func(t *testing.T) {
		// Create a virtual key first
		createBody := `{"name":"test-empty-name-key"}`
		req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(createBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create virtual key: %d: %s", rec.Code, rec.Body.String())
		}

		var createResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		keyID := createResp["id"].(string)

		// Try to update with empty name
		updateBody := `{"name":""}`
		req = httptest.NewRequest("PUT", "/virtual-keys/"+keyID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for empty name, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		// Try to update non-existent key
		nonExistentID := uuid.New().String()
		updateBody := `{"name":"test-name"}`
		req := httptest.NewRequest("PUT", "/virtual-keys/"+nonExistentID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected 404 for non-existent key, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("RateLimits", func(t *testing.T) {
		// Create a virtual key first
		createBody := `{"name":"test-ratelimit-key"}`
		req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(createBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create virtual key: %d: %s", rec.Code, rec.Body.String())
		}

		var createResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		keyID := createResp["id"].(string)

		// Update with rate limits
		updateBody := `{"name":"ratelimited-key","rate_limit_rps":10,"rate_limit_burst":20}`
		req = httptest.NewRequest("PUT", "/virtual-keys/"+keyID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var updateResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &updateResp); err != nil {
			t.Fatalf("Failed to parse update response: %v", err)
		}
		if updateResp["name"] != "ratelimited-key" {
			t.Errorf("Expected name 'ratelimited-key', got %v", updateResp["name"])
		}
		// Rate limits are returned as floats in JSON
		if updateResp["rate_limit_rps"] != float64(10) {
			t.Errorf("Expected rate_limit_rps=10, got %v", updateResp["rate_limit_rps"])
		}
		if updateResp["rate_limit_burst"] != float64(20) {
			t.Errorf("Expected rate_limit_burst=20, got %v", updateResp["rate_limit_burst"])
		}
	})
}

// Stats Query Parameter Tests
