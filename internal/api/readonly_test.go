package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestReadOnlyGuard verifies the middleware in isolation: safe methods reach the
// next handler, mutating methods are refused with 403 and never reach it.
func TestReadOnlyGuard(t *testing.T) {
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	guard := readOnlyGuard(next)

	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		called = false
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, httptest.NewRequest(m, "/providers", http.NoBody))
		if !called {
			t.Errorf("%s: expected next handler to be called", m)
		}
		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected 200, got %d", m, rec.Code)
		}
	}

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		called = false
		rec := httptest.NewRecorder()
		guard.ServeHTTP(rec, httptest.NewRequest(m, "/providers", http.NoBody))
		if called {
			t.Errorf("%s: next handler must not be called in read-only mode", m)
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: expected 403, got %d", m, rec.Code)
		}
	}

	// Acknowledging background-discovery notifications is exempt: it only flips a
	// per-row "seen" flag, so it must pass through even in read-only mode.
	called = false
	rec := httptest.NewRecorder()
	guard.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/changes/ack", http.NoBody))
	if !called {
		t.Error("POST /discovery/changes/ack: expected exemption to reach next handler")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("POST /discovery/changes/ack: expected 200, got %d", rec.Code)
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
