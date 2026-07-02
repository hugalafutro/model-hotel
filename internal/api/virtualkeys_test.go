package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

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
		createFn: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
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

func TestGetVirtualKey_NotFound(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		getFn: func(_ context.Context, _ uuid.UUID) (*virtualkey.VirtualKey, error) {
			return nil, virtualkey.ErrNotFound
		},
	}
	h := testHandler(nil, mockVK, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	id := uuid.New()
	req, w := newChiRequest(http.MethodGet, "/virtual-keys/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.GetVirtualKey(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestGetVirtualKey_DBError(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		getFn: func(_ context.Context, _ uuid.UUID) (*virtualkey.VirtualKey, error) {
			return nil, errors.New("db connection lost")
		},
	}
	h := testHandler(nil, mockVK, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	id := uuid.New()
	req, w := newChiRequest(http.MethodGet, "/virtual-keys/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.GetVirtualKey(w, req)

	// A non-ErrNotFound error must surface as 500, not be masked as 404.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
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
	err := validateRateLimits(nil, nil, nil, w)
	if err != nil {
		t.Errorf("nil rps and nil burst should return nil, got %v", err)
	}
}

func TestValidateRateLimits_NegativeRPS(t *testing.T) {
	w := httptest.NewRecorder()
	rps := -1.0
	err := validateRateLimits(&rps, nil, nil, w)
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
	err := validateRateLimits(nil, &burst, nil, w)
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
	err := validateRateLimits(nil, &burst, nil, w)
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
	err := validateRateLimits(&rps, &burst, nil, w)
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
	err := validateRateLimits(&rps, &burst, nil, w)
	if err != nil {
		t.Errorf("rps=0 with burst>=1 should return nil, got %v", err)
	}
}

func TestValidateRateLimits_NilRPS_ValidBurst(t *testing.T) {
	w := httptest.NewRecorder()
	burst := 5
	err := validateRateLimits(nil, &burst, nil, w)
	if err != nil {
		t.Errorf("nil rps with valid burst should return nil, got %v", err)
	}
}

func TestValidateRateLimits_ValidRPS_NilBurst(t *testing.T) {
	w := httptest.NewRecorder()
	rps := 5.0
	err := validateRateLimits(&rps, nil, nil, w)
	if err != nil {
		t.Errorf("valid rps with nil burst should return nil, got %v", err)
	}
}

func TestValidateRateLimits_NegativeTPM(t *testing.T) {
	w := httptest.NewRecorder()
	tpm := -1
	err := validateRateLimits(nil, nil, &tpm, w)
	if err == nil {
		t.Error("negative tpm should return error")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestValidateRateLimits_ZeroTPM(t *testing.T) {
	w := httptest.NewRecorder()
	tpm := 0
	err := validateRateLimits(nil, nil, &tpm, w)
	if err == nil {
		t.Error("tpm=0 should return error (use null for no cap)")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestValidateRateLimits_ValidTPM(t *testing.T) {
	w := httptest.NewRecorder()
	tpm := 10000
	err := validateRateLimits(nil, nil, &tpm, w)
	if err != nil {
		t.Errorf("valid tpm should return nil, got %v", err)
	}
}

func TestCond_EmptyStringFalse(t *testing.T) {
	result := cond("", false)
	if result != "" {
		t.Errorf("cond(%q, false) = %q, want empty string", "", result)
	}
}

// TestCreateVirtualKey_DuplicateName tests that CreateVirtualKey returns
// an error from the repo when creating a key with a name that already exists.
// The unique constraint violation surfaces as a 500 (repo error).
func TestCreateVirtualKey_DuplicateName(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		createFn: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			return nil, &pgconn.PgError{Code: "23505"}
		},
	}
	h := testHandler(nil, mockVK, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"duplicate-key"}`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// TestCreateVirtualKey_NameTooLong tests that CreateVirtualKey returns 400
// when the name exceeds the maximum length.
func TestCreateVirtualKey_NameTooLong(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	longName := strings.Repeat("a", 101)
	body := bytes.NewReader([]byte(fmt.Sprintf(`{"name":"%s"}`, longName)))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// DeleteVirtualKey DB error tests
// ---------------------------------------------------------------------------

// TestDeleteVirtualKey_DBError tests that DeleteVirtualKey returns 500
// when the database is unavailable.
func TestDeleteVirtualKey_DBError(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		deleteFn: func(ctx context.Context, vid uuid.UUID) error {
			return errors.New("db connection lost")
		},
	}
	h := testHandler(nil, mockVK, nil, nil, nil)

	id := uuid.New()
	req, w := newChiRequest(http.MethodDelete, "/virtual-keys/"+id.String(), nil)
	req = setChiURLParam(req, "id", id.String())

	h.DeleteVirtualKey(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// UpdateVirtualKey tests
// ---------------------------------------------------------------------------

// TestUpdateVirtualKey_MalformedJSON tests that UpdateVirtualKey returns 400
// when the request body contains malformed JSON.
func TestUpdateVirtualKey_MalformedJSON(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	id := uuid.New()
	body := bytes.NewReader([]byte(`{invalid json`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid request body") {
		t.Errorf("expected body to contain %q, got %q", "invalid request body", w.Body.String())
	}
}

// TestUpdateVirtualKey_DBError tests that UpdateVirtualKey returns 500
// when the database is unavailable.
func TestUpdateVirtualKey_DBError(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		updateFn: func(ctx context.Context, vid uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			return nil, errors.New("db connection lost")
		},
	}
	h := testHandler(nil, mockVK, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)

	id := uuid.New()
	body := bytes.NewReader([]byte(`{"name":"updated-name"}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// UpdateVirtualKey - allowed_providers tests
// ---------------------------------------------------------------------------

// TestUpdateVirtualKey_WithAllowedProviders tests that UpdateVirtualKey
// correctly handles setting allowed_providers on an existing key.
func TestUpdateVirtualKey_WithAllowedProviders(t *testing.T) {
	id := uuid.New()
	mockVK := &mockVirtualKeyStore{
		getFn: func(ctx context.Context, vid uuid.UUID) (*virtualkey.VirtualKey, error) {
			return &virtualkey.VirtualKey{ID: vid, Name: "updated-key", KeyHash: "hash123", KeyPreview: "sk-...up", StripReasoning: false}, nil
		}, updateFn: func(ctx context.Context, vid uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			if vid != id {
				return nil, errors.New("unexpected ID")
			}
			if name != "updated-key" {
				return nil, errors.New("unexpected name")
			}
			if allowedProviders == nil || len(*allowedProviders) != 1 || (*allowedProviders)[0] != "p3" {
				return nil, errors.New("allowedProviders not passed correctly")
			}
			return &virtualkey.VirtualKey{
				ID:               vid,
				Name:             name,
				KeyHash:          "hash123",
				KeyPreview:       "sk-...up",
				AllowedProviders: allowedProviders,
			}, nil
		},
	}
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, mockVK, nil, auth, nil)

	body := bytes.NewReader([]byte(`{"name":"updated-key","allowed_providers":["p3"]}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp virtualkey.VirtualKeyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.AllowedProviders == nil {
		t.Fatal("response AllowedProviders should not be nil")
	}
	if len(*resp.AllowedProviders) != 1 || (*resp.AllowedProviders)[0] != "p3" {
		t.Errorf("response AllowedProviders = %v, want [p3]", *resp.AllowedProviders)
	}
}

// TestUpdateVirtualKey_ToClearAllowedProviders tests that UpdateVirtualKey
// correctly clears allowed_providers when set to null.
func TestUpdateVirtualKey_ToClearAllowedProviders(t *testing.T) {
	id := uuid.New()
	mockVK := &mockVirtualKeyStore{
		getFn: func(ctx context.Context, vid uuid.UUID) (*virtualkey.VirtualKey, error) {
			return &virtualkey.VirtualKey{ID: vid, Name: "cleared-key", KeyHash: "hash123", KeyPreview: "sk-...cl", StripReasoning: false}, nil
		},
		updateFn: func(ctx context.Context, vid uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			if vid != id {
				return nil, errors.New("unexpected ID")
			}
			if allowedProviders != nil {
				return nil, errors.New("allowedProviders should be nil when clearing")
			}
			return &virtualkey.VirtualKey{
				ID:               vid,
				Name:             name,
				KeyHash:          "hash123",
				KeyPreview:       "sk-...cl",
				AllowedProviders: nil,
			}, nil
		},
	}
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, mockVK, nil, auth, nil)

	body := bytes.NewReader([]byte(`{"name":"cleared-key","allowed_providers":null}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp virtualkey.VirtualKeyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.AllowedProviders != nil {
		t.Errorf("response AllowedProviders should be nil, got %v", *resp.AllowedProviders)
	}
}

// TestUpdateVirtualKey_OmitAllowedProvidersPreservesExisting tests that
// omitting allowed_providers from the update body preserves the existing value
// instead of clearing it (which would silently drop a security restriction).
func TestUpdateVirtualKey_OmitAllowedProvidersPreservesExisting(t *testing.T) {
	id := uuid.New()
	existingProviders := []string{"p1", "p2"}

	mockVK := &mockVirtualKeyStore{
		getFn: func(ctx context.Context, vid uuid.UUID) (*virtualkey.VirtualKey, error) {
			if vid != id {
				return nil, errors.New("unexpected ID")
			}
			return &virtualkey.VirtualKey{
				ID:               vid,
				Name:             "existing-key",
				KeyHash:          "hash123",
				KeyPreview:       "sk-...ex",
				AllowedProviders: &existingProviders,
			}, nil
		},
		updateFn: func(ctx context.Context, vid uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			if vid != id {
				return nil, errors.New("unexpected ID")
			}
			// The handler should have preserved existing AllowedProviders
			if allowedProviders == nil {
				return nil, errors.New("allowedProviders should not be nil — existing value should be preserved")
			}
			if len(*allowedProviders) != 2 || (*allowedProviders)[0] != "p1" || (*allowedProviders)[1] != "p2" {
				return nil, fmt.Errorf("expected [p1, p2], got %v", allowedProviders)
			}
			return &virtualkey.VirtualKey{
				ID:               vid,
				Name:             name,
				KeyHash:          "hash123",
				KeyPreview:       "sk-...up",
				AllowedProviders: allowedProviders,
			}, nil
		},
	}
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, mockVK, nil, auth, nil)

	// Body omits allowed_providers entirely
	body := bytes.NewReader([]byte(`{"name":"updated-key"}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestCreateVirtualKey_WithAllowedProviders tests that CreateVirtualKey
// correctly handles the allowed_providers field.
func TestCreateVirtualKey_WithAllowedProviders(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		createFn: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			if name != "test-key-ap" {
				return nil, errors.New("unexpected name")
			}
			if allowedProviders == nil || len(*allowedProviders) != 2 || (*allowedProviders)[0] != "p1" || (*allowedProviders)[1] != "p2" {
				return nil, errors.New("allowedProviders not passed correctly")
			}
			return &virtualkey.VirtualKey{
				ID:               uuid.New(),
				Name:             name,
				KeyHash:          keyHash,
				KeyPreview:       keyPreview,
				AllowedProviders: allowedProviders,
			}, nil
		},
	}
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, mockVK, nil, auth, nil)

	body := bytes.NewReader([]byte(`{"name":"test-key-ap","allowed_providers":["p1","p2"]}`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var resp virtualkey.VirtualKeyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp.AllowedProviders == nil {
		t.Fatal("response AllowedProviders should not be nil")
	}
	if len(*resp.AllowedProviders) != 2 {
		t.Errorf("response AllowedProviders length = %d, want 2", len(*resp.AllowedProviders))
	}
}

// ---------------------------------------------------------------------------
// CreateVirtualKey - empty allowed_providers rejection tests
// ---------------------------------------------------------------------------

// TestCreateVirtualKey_EmptyAllowedProvidersArray tests that CreateVirtualKey
// rejects an empty allowed_providers array (non-nil but len==0).
func TestCreateVirtualKey_EmptyAllowedProvidersArray(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		createFn: func(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			t.Error("create should not be called when allowed_providers is empty array")
			return nil, nil
		},
	}
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, mockVK, nil, auth, nil)

	body := bytes.NewReader([]byte(`{"name":"test-key-empty","allowed_providers":[]}`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "allowed_providers must be null or contain at least one provider ID") {
		t.Errorf("expected error message about allowed_providers, got: %s", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// UpdateVirtualKeyRequest.UnmarshalJSON tests
// ---------------------------------------------------------------------------

func TestUpdateVirtualKeyRequest_UnmarshalJSON_BothFieldsPresent(t *testing.T) {
	data := `{"name":"test","allowed_providers":["p1"],"strip_reasoning":true}`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req.allowedProvidersPresent {
		t.Error("expected allowedProvidersPresent=true when allowed_providers is in JSON")
	}
	if !req.stripReasoningPresent {
		t.Error("expected stripReasoningPresent=true when strip_reasoning is in JSON")
	}
	if req.AllowedProviders == nil || len(*req.AllowedProviders) != 1 || (*req.AllowedProviders)[0] != "p1" {
		t.Errorf("AllowedProviders = %v, want [p1]", req.AllowedProviders)
	}
	if req.StripReasoning == nil || !*req.StripReasoning {
		t.Error("StripReasoning should be true")
	}
}

func TestUpdateVirtualKeyRequest_UnmarshalJSON_NeitherFieldPresent(t *testing.T) {
	data := `{"name":"test"}`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.allowedProvidersPresent {
		t.Error("expected allowedProvidersPresent=false when allowed_providers is absent")
	}
	if req.stripReasoningPresent {
		t.Error("expected stripReasoningPresent=false when strip_reasoning is absent")
	}
}

func TestUpdateVirtualKeyRequest_UnmarshalJSON_AllowedProvidersNull(t *testing.T) {
	data := `{"name":"test","allowed_providers":null}`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req.allowedProvidersPresent {
		t.Error("expected allowedProvidersPresent=true when allowed_providers:null is explicitly in JSON")
	}
	if req.AllowedProviders != nil {
		t.Errorf("AllowedProviders should be nil when JSON value is null, got %v", req.AllowedProviders)
	}
}

func TestUpdateVirtualKeyRequest_UnmarshalJSON_StripReasoningFalse(t *testing.T) {
	data := `{"name":"test","strip_reasoning":false}`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req.stripReasoningPresent {
		t.Error("expected stripReasoningPresent=true when strip_reasoning is in JSON (even if false)")
	}
	if req.StripReasoning == nil || *req.StripReasoning {
		t.Error("StripReasoning should be false")
	}
}

func TestUpdateVirtualKeyRequest_UnmarshalJSON_InvalidJSON(t *testing.T) {
	data := `{invalid json}`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestUpdateVirtualKeyRequest_UnmarshalJSON_OnlyAllowedProvidersPresent(t *testing.T) {
	data := `{"name":"test","allowed_providers":["p1"]}`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !req.allowedProvidersPresent {
		t.Error("expected allowedProvidersPresent=true")
	}
	if req.stripReasoningPresent {
		t.Error("expected stripReasoningPresent=false when strip_reasoning absent")
	}
}

func TestUpdateVirtualKeyRequest_UnmarshalJSON_OnlyStripReasoningPresent(t *testing.T) {
	data := `{"name":"test","strip_reasoning":true}`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.allowedProvidersPresent {
		t.Error("expected allowedProvidersPresent=false when allowed_providers absent")
	}
	if !req.stripReasoningPresent {
		t.Error("expected stripReasoningPresent=true")
	}
}

// TestUpdateVirtualKey_NotFound tests that UpdateVirtualKey returns 404 when
// the key does not exist in the database.
func TestUpdateVirtualKey_NotFound(t *testing.T) {
	mockVK := &mockVirtualKeyStore{
		updateFn: func(ctx context.Context, vid uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			return nil, virtualkey.ErrNotFound
		},
	}
	h := testHandler(nil, mockVK, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)

	id := uuid.New()
	// Include allowed_providers and strip_reasoning in the request to skip the Get call
	body := bytes.NewReader([]byte(`{"name":"updated-name","allowed_providers":null,"strip_reasoning":false}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusNotFound, w.Code, w.Body.String())
	}
}

// TestUpdateVirtualKey_PartialUpdate_NameOnly tests that UpdateVirtualKey
// can update only the name while preserving existing allowed_providers and
// strip_reasoning values from the existing key.
func TestUpdateVirtualKey_PartialUpdate_NameOnly(t *testing.T) {
	id := uuid.New()
	existingProviders := []string{"prov-a", "prov-b"}

	mockVK := &mockVirtualKeyStore{
		getFn: func(ctx context.Context, vid uuid.UUID) (*virtualkey.VirtualKey, error) {
			return &virtualkey.VirtualKey{
				ID:               vid,
				Name:             "old-name",
				KeyHash:          "hash123",
				KeyPreview:       "sk-...ex",
				AllowedProviders: &existingProviders,
				StripReasoning:   true,
			}, nil
		},
		updateFn: func(ctx context.Context, vid uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			if name != "new-name" {
				t.Errorf("expected name 'new-name', got %q", name)
			}
			// Verify existing providers were preserved
			if allowedProviders == nil || len(*allowedProviders) != 2 {
				t.Errorf("expected 2 allowed providers preserved, got %v", allowedProviders)
			}
			// Verify existing strip_reasoning was preserved
			if stripReasoning == nil || !*stripReasoning {
				t.Error("expected strip_reasoning=true to be preserved")
			}
			return &virtualkey.VirtualKey{
				ID:               vid,
				Name:             name,
				KeyHash:          "hash123",
				KeyPreview:       "sk-...up",
				AllowedProviders: allowedProviders,
				StripReasoning:   true,
			}, nil
		},
	}
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, mockVK, nil, auth, nil)

	// Body only contains name — allowed_providers and strip_reasoning are omitted
	body := bytes.NewReader([]byte(`{"name":"new-name"}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestUpdateVirtualKey_ReservedName tests that UpdateVirtualKey rejects
// reserved names.
func TestUpdateVirtualKey_ReservedName(t *testing.T) {
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, nil, nil, auth, nil)

	for _, name := range []string{"chat", "arena", "completions", "admin"} {
		t.Run(name, func(t *testing.T) {
			id := uuid.New()
			body := bytes.NewReader([]byte(fmt.Sprintf(`{"name":"%s"}`, name)))
			req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
			req = setChiURLParam(req, "id", id.String())

			h.UpdateVirtualKey(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("reserved name %q: expected status %d, got %d", name, http.StatusBadRequest, w.Code)
			}
		})
	}
}

// TestUpdateVirtualKey_InvalidUUID tests that UpdateVirtualKey returns 400
// when the URL contains an invalid UUID.
func TestUpdateVirtualKey_InvalidUUID(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"test"}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/not-a-uuid", body)
	req = setChiURLParam(req, "id", "not-a-uuid")

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestUpdateVirtualKey_GetKeyError tests that UpdateVirtualKey returns 500 when
// fetching the existing key fails with a non-ErrNotFound error (during the
// allowed_providers/strip_reasoning preservation lookup).
func TestUpdateVirtualKey_GetKeyError(t *testing.T) {
	id := uuid.New()
	mockVK := &mockVirtualKeyStore{
		getFn: func(ctx context.Context, vid uuid.UUID) (*virtualkey.VirtualKey, error) {
			return nil, errors.New("db connection lost")
		},
	}
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, mockVK, nil, auth, nil)

	// Omit allowed_providers to trigger the Get call for preservation
	body := bytes.NewReader([]byte(`{"name":"updated-key"}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// UpdateVirtualKey - empty allowed_providers rejection tests
// ---------------------------------------------------------------------------

// TestUpdateVirtualKey_EmptyAllowedProvidersArray tests that UpdateVirtualKey
// rejects an empty allowed_providers array (non-nil but len==0).
func TestUpdateVirtualKey_EmptyAllowedProvidersArray(t *testing.T) {
	id := uuid.New()
	mockVK := &mockVirtualKeyStore{
		updateFn: func(ctx context.Context, vid uuid.UUID, name string, rps *float64, burst, tpm *int, allowedProviders *[]string, stripReasoning *bool, owner *uuid.UUID) (*virtualkey.VirtualKey, error) {
			t.Error("update should not be called when allowed_providers is empty array")
			return nil, nil
		},
	}
	auth := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := testHandler(nil, mockVK, nil, auth, nil)

	body := bytes.NewReader([]byte(`{"name":"update-key-empty","allowed_providers":[]}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "allowed_providers must be null or contain at least one provider ID") {
		t.Errorf("expected error message about allowed_providers, got: %s", w.Body.String())
	}
}

// TestUpdateVirtualKeyRequest_UnmarshalJSON_ArrayInput tests that UnmarshalJSON
// returns an error when given a JSON array instead of an object. This exercises
// the second json.Unmarshal(data, &raw) error path.
func TestUpdateVirtualKeyRequest_UnmarshalJSON_ArrayInput(t *testing.T) {
	data := `[1,2,3]`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err == nil {
		t.Error("expected error for JSON array input, got nil")
	}
}

// TestUpdateVirtualKeyRequest_UnmarshalJSON_NonObjectInput tests that
// UnmarshalJSON returns an error when given a JSON value that is not an
// object (e.g., a number), which fails the map[string]json.RawMessage
// unmarshal in the second pass.
func TestUpdateVirtualKeyRequest_UnmarshalJSON_NonObjectInput(t *testing.T) {
	data := `42`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err == nil {
		t.Error("expected error for non-object JSON input, got nil")
	}
}

// ---------------------------------------------------------------------------
// 11. filterContainers — AppGroup filter branch
// ---------------------------------------------------------------------------

// The Docker filter tests are in internal/util/docker_wrapper_test.go since
// filterContainers and ContainerFilter are in that package.
// Instead, test vkScope from stats.go which is in this package.

// TestVKScope exercises the vkScope helper for both branches.
func TestVKScope(t *testing.T) {
	t.Run("excludeDeleted=true", func(t *testing.T) {
		join, filter := vkScope(true)
		if join == "" {
			t.Error("expected non-empty join for excludeDeleted=true")
		}
		if filter == "" {
			t.Error("expected non-empty filter for excludeDeleted=true")
		}
	})
	t.Run("excludeDeleted=false", func(t *testing.T) {
		join, filter := vkScope(false)
		if join != "" {
			t.Errorf("expected empty join for excludeDeleted=false, got %q", join)
		}
		if filter != "" {
			t.Errorf("expected empty filter for excludeDeleted=false, got %q", filter)
		}
	})
}

// ---------------------------------------------------------------------------
// 10. UnmarshalJSON (UpdateVirtualKeyRequest) — string input
// ---------------------------------------------------------------------------

func TestUpdateVirtualKeyRequest_UnmarshalJSON_StringInput(t *testing.T) {
	data := `"hello"`
	var req UpdateVirtualKeyRequest
	if err := json.Unmarshal([]byte(data), &req); err == nil {
		t.Error("expected error for JSON string input, got nil")
	}
}

// ---------------------------------------------------------------------------
// 18. CreateVirtualKey — empty name after trim
// ---------------------------------------------------------------------------

func TestCreateVirtualKey_NameEmptyAfterTrim(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"   "}`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestCreateVirtualKey_InvalidRateLimitBurst covers the validateRateLimits
// rejection branch in CreateVirtualKey (rate_limit_burst must be >= 1).
func TestCreateVirtualKey_InvalidRateLimitBurst(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	body := bytes.NewReader([]byte(`{"name":"valid-key","rate_limit_burst":0}`))
	req, w := newChiRequest(http.MethodPost, "/virtual-keys", body)

	h.CreateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "rate_limit_burst") {
		t.Errorf("expected rate_limit_burst error, got: %s", w.Body.String())
	}
}

// TestUpdateVirtualKey_InvalidRateLimit covers the validateRateLimits rejection
// branch in UpdateVirtualKey (rate_limit_rps must be >= 0). Both allowed_providers
// and strip_reasoning are present so the handler skips the existing-key fetch and
// reaches rate-limit validation without touching the repo.
func TestUpdateVirtualKey_InvalidRateLimit(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	id := uuid.New()
	body := bytes.NewReader([]byte(`{"name":"valid-key","allowed_providers":["p1"],"strip_reasoning":false,"rate_limit_rps":-1}`))
	req, w := newChiRequest(http.MethodPut, "/virtual-keys/"+id.String(), body)
	req = setChiURLParam(req, "id", id.String())

	h.UpdateVirtualKey(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "rate_limit_rps") {
		t.Errorf("expected rate_limit_rps error, got: %s", w.Body.String())
	}
}
