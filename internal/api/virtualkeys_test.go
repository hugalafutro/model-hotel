package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// ---------------------------------------------------------------------------
// CreateVirtualKey additional tests
// ---------------------------------------------------------------------------

func TestCreateVirtualKey_EmptyBody(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", nil)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateVirtualKey_InvalidJSON(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{invalid json`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestCreateVirtualKey_DBError(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		createFn: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int) (*virtualkey.VirtualKey, error) {
			return nil, errors.New("db connection lost")
		},
	}
	h := testHandler(nil, mockVK, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"fail-key"}`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestCreateVirtualKey_ReservedName(t *testing.T) {
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, nil, nil, auth, nil)

	for _, name := range []string{"chat", "arena", "completions", "admin", "Chat", "ARENA"} {
		t.Run(name, func(t *testing.T) {
			body := bytes.NewReader([]byte(`{"name":"` + name + `"}`))
			req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

			h.CreateVirtualKey(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("reserved name %q: expected status %d, got %d", name, http.StatusBadRequest, w.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ListVirtualKeys additional tests
// ---------------------------------------------------------------------------

func TestListVirtualKeys_EmptyList(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		listFn: func(ctx context.Context) ([]*virtualkey.VirtualKey, error) {
			return []*virtualkey.VirtualKey{}, nil
		},
	}
	h := testHandler(nil, mockVK, nil, nil, nil)
	req, w := newChiRequest(http.MethodGet, "/virtual-keys", nil)

	h.ListVirtualKeys(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestListVirtualKeys_DBError(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		listFn: func(ctx context.Context) ([]*virtualkey.VirtualKey, error) {
			return nil, errors.New("db unavailable")
		},
	}
	h := testHandler(nil, mockVK, nil, nil, nil)
	req, w := newChiRequest(http.MethodGet, "/virtual-keys", nil)

	h.ListVirtualKeys(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

// ---------------------------------------------------------------------------
// GetVirtualKey additional tests
// ---------------------------------------------------------------------------

func TestGetVirtualKey_InvalidUUID(t *testing.T) {
	h := testHandler(nil, nil, nil, nil, nil)
	req, w := newChiRequest(http.MethodGet, "/virtual-keys/not-a-uuid", nil)
	req = setChiURLParam(req, "id", "not-a-uuid")

	h.GetVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// ---------------------------------------------------------------------------
// DeleteVirtualKey additional tests
// ---------------------------------------------------------------------------

func TestDeleteVirtualKey_InvalidUUID(t *testing.T) {
	h := testHandler(nil, nil, nil, nil, nil)
	req, w := newChiRequest(http.MethodDelete, "/virtual-keys/not-a-uuid", nil)
	req = setChiURLParam(req, "id", "not-a-uuid")

	h.DeleteVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// ---------------------------------------------------------------------------
// virtualKeyToResponse tests
// ---------------------------------------------------------------------------

func TestVirtualKeyToResponse_WithKey(t *testing.T) {
	now := time.Now()
	vk := &virtualkey.VirtualKey{
		ID:         uuid.New(),
		Name:       "test-with-key",
		KeyHash:    "hash123",
		KeyPreview: "sk-...ab",
		TokensUsed: 10,
		LastUsedAt: &now,
		CreatedAt:  now,
	}
	rawKey := "sk-abc123rawkey"

	resp := virtualKeyToResponse(vk, true, rawKey)

	if resp.ID != vk.ID.String() {
		t.Errorf("ID = %q, want %q", resp.ID, vk.ID.String())
	}
	if resp.Name != "test-with-key" {
		t.Errorf("Name = %q, want %q", resp.Name, "test-with-key")
	}
	if resp.Key != rawKey {
		t.Errorf("Key = %q, want %q (includeKey=true)", resp.Key, rawKey)
	}
	if resp.KeyPreview != "sk-...ab" {
		t.Errorf("KeyPreview = %q, want %q", resp.KeyPreview, "sk-...ab")
	}
	if resp.TokensUsed != 10 {
		t.Errorf("TokensUsed = %d, want %d", resp.TokensUsed, 10)
	}
	if resp.LastUsedAt == nil {
		t.Error("LastUsedAt should not be nil when vk.LastUsedAt is set")
	}
	if resp.CreatedAt != now.Format(time.RFC3339) {
		t.Errorf("CreatedAt = %q, want %q", resp.CreatedAt, now.Format(time.RFC3339))
	}
}

func TestVirtualKeyToResponse_WithoutKey(t *testing.T) {
	now := time.Now()
	vk := &virtualkey.VirtualKey{
		ID:        uuid.New(),
		Name:      "test-no-key",
		CreatedAt: now,
	}

	resp := virtualKeyToResponse(vk, false, "sk-raw-key")

	if resp.Key != "" {
		t.Errorf("Key = %q, want empty (includeKey=false)", resp.Key)
	}
}

func TestVirtualKeyToResponse_NilLastUsedAt(t *testing.T) {
	vk := &virtualkey.VirtualKey{
		ID:         uuid.New(),
		Name:       "unused-key",
		LastUsedAt: nil,
		CreatedAt:  time.Now(),
	}

	resp := virtualKeyToResponse(vk, false, "")

	if resp.LastUsedAt != nil {
		t.Error("LastUsedAt should be nil when vk.LastUsedAt is nil")
	}
}

func TestVirtualKeyToResponse_WithLastUsedAt(t *testing.T) {
	ts := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	vk := &virtualkey.VirtualKey{
		ID:         uuid.New(),
		Name:       "used-key",
		LastUsedAt: &ts,
		CreatedAt:  time.Now(),
	}

	resp := virtualKeyToResponse(vk, false, "")

	if resp.LastUsedAt == nil {
		t.Fatal("LastUsedAt should not be nil")
	}
	expected := ts.Format(time.RFC3339)
	if *resp.LastUsedAt != expected {
		t.Errorf("LastUsedAt = %q, want %q", *resp.LastUsedAt, expected)
	}
}

// ---------------------------------------------------------------------------
// cond function tests
// ---------------------------------------------------------------------------

func TestCond_True(t *testing.T) {
	result := cond("hello", true)
	if result != "hello" {
		t.Errorf("cond(%q, true) = %q, want %q", "hello", result, "hello")
	}
}

func TestCond_False(t *testing.T) {
	result := cond("hello", false)
	if result != "" {
		t.Errorf("cond(%q, false) = %q, want empty string", "hello", result)
	}
}

func TestCond_EmptyStringTrue(t *testing.T) {
	result := cond("", true)
	if result != "" {
		t.Errorf("cond(%q, true) = %q, want %q", "", result, "")
	}
}

// ---------------------------------------------------------------------------
// validateRateLimits
// ---------------------------------------------------------------------------

func TestValidateRateLimits_NilBoth(t *testing.T) {
	w := httptest.NewRecorder()
	err := validateRateLimits(nil, nil, w)
	if err != nil {
		t.Errorf("nil rps and nil burst should return nil, got %v", err)
	}
}

func TestValidateRateLimits_NegativeRPS(t *testing.T) {
	w := httptest.NewRecorder()
	rps := -1.0
	err := validateRateLimits(&rps, nil, w)
	if err == nil {
		t.Error("negative rps should return error")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestValidateRateLimits_NegativeBurst(t *testing.T) {
	w := httptest.NewRecorder()
	burst := -1
	err := validateRateLimits(nil, &burst, w)
	if err == nil {
		t.Error("negative burst should return error")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestValidateRateLimits_ZeroBurst(t *testing.T) {
	w := httptest.NewRecorder()
	burst := 0
	err := validateRateLimits(nil, &burst, w)
	if err == nil {
		t.Error("burst=0 should return error")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestValidateRateLimits_ValidRPSAndBurst(t *testing.T) {
	w := httptest.NewRecorder()
	rps := 10.0
	burst := 20
	err := validateRateLimits(&rps, &burst, w)
	if err != nil {
		t.Errorf("valid rps and burst should return nil, got %v", err)
	}
	// w.Code defaults to 200, but we only write status on error, so check it wasn't changed from default
	// For valid input, no status should be written - but NewRecorder() defaults to 200
	// The key is that err is nil, which we already checked
}

func TestValidateRateLimits_ZeroRPS_ValidBurst(t *testing.T) {
	w := httptest.NewRecorder()
	rps := 0.0
	burst := 1
	err := validateRateLimits(&rps, &burst, w)
	if err != nil {
		t.Errorf("rps=0 with burst>=1 should return nil, got %v", err)
	}
}

func TestValidateRateLimits_NilRPS_ValidBurst(t *testing.T) {
	w := httptest.NewRecorder()
	burst := 5
	err := validateRateLimits(nil, &burst, w)
	if err != nil {
		t.Errorf("nil rps with valid burst should return nil, got %v", err)
	}
}

func TestValidateRateLimits_ValidRPS_NilBurst(t *testing.T) {
	w := httptest.NewRecorder()
	rps := 5.0
	err := validateRateLimits(&rps, nil, w)
	if err != nil {
		t.Errorf("valid rps with nil burst should return nil, got %v", err)
	}
}

func TestCond_EmptyStringFalse(t *testing.T) {
	result := cond("", false)
	if result != "" {
		t.Errorf("cond(%q, false) = %q, want empty string", "", result)
	}
}
