package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestReadOnlyGuard verifies the api wrapper delegates to httpx.ReadOnlyGuard:
// safe methods reach the next handler, mutating methods are refused with 403,
// and the discovery-ack exemption still passes through. The full method matrix
// and the exempt-path list are tested in internal/httpx.
func TestReadOnlyGuard(t *testing.T) {
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	guard := readOnlyGuard(next)

	cases := []struct {
		method     string
		path       string
		wantCalled bool
		wantCode   int
	}{
		{http.MethodGet, "/providers", true, http.StatusOK},
		{http.MethodPost, "/providers", false, http.StatusForbidden},
		{http.MethodPost, "/api/discovery/changes/ack", true, http.StatusOK},
	}
	for _, tc := range cases {
		called = false
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, http.NoBody))
		if called != tc.wantCalled {
			t.Errorf("%s %s: next handler called = %v, want %v", tc.method, tc.path, called, tc.wantCalled)
		}
		if rec.Code != tc.wantCode {
			t.Errorf("%s %s: status = %d, want %d", tc.method, tc.path, rec.Code, tc.wantCode)
		}
	}
}

// TestHandlerRegister_ReadOnly verifies the wiring in Register: when
// DemoReadOnly is set, a mutating admin request is refused after auth while a
// GET still succeeds.
func TestHandlerRegister_ReadOnly(t *testing.T) {
	h := newTestHandler(t) // skips if no test DB
	h.cfg.DemoReadOnly = true

	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers",
		strings.NewReader(`{"name":"x","base_url":"http://localhost:1234"}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("read-only POST /providers: expected 403, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("read-only GET /providers: expected 200, got %d", rec.Code)
	}

	// The discovery-changes ack is exempt from the guard so the Models badge can
	// be cleared on a demo instance: it must not be refused with a 403.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/discovery/changes/ack", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("read-only POST /discovery/changes/ack: must be exempt, got 403")
	}
}

// TestHandlerRegister_ReadOnlyDisabled confirms the default: with DemoReadOnly
// off, the guard is not mounted and a mutating request reaches the handler
// (i.e. it is not rejected with the read-only 403).
func TestHandlerRegister_ReadOnlyDisabled(t *testing.T) {
	h := newTestHandler(t) // skips if no test DB

	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers",
		strings.NewReader(`{"name":"ro-off","base_url":"http://localhost:1234"}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden {
		t.Fatalf("read-only disabled: POST should not be refused with 403")
	}
}
