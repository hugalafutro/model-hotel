package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/admin"
)

// fakeTokenMgr is an in-memory AdminTokenManager for handler tests (no DB).
type fakeTokenMgr struct {
	hash   string
	setErr error
}

func (f *fakeTokenMgr) Hash() string { return f.hash }
func (f *fakeTokenMgr) SetHash(value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.hash = value
	return nil
}

func tokenHashRouter(mgr AdminTokenManager) http.Handler {
	r := chi.NewRouter()
	NewAdminTokenHandler(mgr).Register(r)
	return r
}

func TestAdminTokenHashGet(t *testing.T) {
	mgr := &fakeTokenMgr{hash: "sha256:" + strings.Repeat("a", 64)}
	srv := tokenHashRouter(mgr)

	req := httptest.NewRequest(http.MethodGet, "/admin/token-hash", http.NoBody)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), mgr.hash) {
		t.Errorf("body should contain hash: %s", rec.Body.String())
	}
}

func TestAdminTokenHashPostOverwrites(t *testing.T) {
	mgr := &fakeTokenMgr{}
	srv := tokenHashRouter(mgr)

	newHash := "sha256:" + strings.Repeat("b", 64)
	req := httptest.NewRequest(http.MethodPost, "/admin/token-hash", strings.NewReader(`{"hash":"`+newHash+`"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if mgr.hash != newHash {
		t.Errorf("hash not set: %q", mgr.hash)
	}
}

func TestAdminTokenHashPostBadBody(t *testing.T) {
	srv := tokenHashRouter(&fakeTokenMgr{})
	req := httptest.NewRequest(http.MethodPost, "/admin/token-hash", strings.NewReader(`not json`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestAdminTokenHashPostInvalidHash(t *testing.T) {
	mgr := &fakeTokenMgr{setErr: admin.ErrInvalidTokenHash}
	srv := tokenHashRouter(mgr)
	req := httptest.NewRequest(http.MethodPost, "/admin/token-hash", strings.NewReader(`{"hash":"short"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// A non-validation SetHash failure (e.g. a file-write error) must be a 500 with
// a generic body, never echoing the underlying error, which can carry an
// os.PathError filesystem path.
func TestAdminTokenHashPostWriteError(t *testing.T) {
	leaky := errors.New("open /srv/data/admin-token.tmp: permission denied")
	mgr := &fakeTokenMgr{setErr: leaky}
	srv := tokenHashRouter(mgr)
	req := httptest.NewRequest(http.MethodPost, "/admin/token-hash",
		strings.NewReader(`{"hash":"sha256:`+strings.Repeat("a", 64)+`"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "permission denied") || strings.Contains(rec.Body.String(), "/srv/") {
		t.Errorf("response leaked the underlying error: %s", rec.Body.String())
	}
}

// TestAdminTokenHashGatedByAuthMiddleware exercises the full request path: the
// routes are mounted inside the AuthMiddleware-protected group, so an
// unauthenticated GET/POST is 401, while the admin token (TOTP off in this
// harness) is accepted. This is the gate that protects who can overwrite the
// admin-token hash.
func TestAdminTokenHashGatedByAuthMiddleware(t *testing.T) {
	h := newTestHandler(t) // skips if the test DB is unavailable
	h.SetAdminTokenManager(&fakeTokenMgr{hash: "sha256:" + strings.Repeat("a", 64)})
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	postBody := `{"hash":"sha256:` + strings.Repeat("b", 64) + `"}`

	// Unauthenticated: both verbs blocked by AuthMiddleware.
	for _, tc := range []struct{ method, body string }{
		{http.MethodGet, ""},
		{http.MethodPost, postBody},
	} {
		req := httptest.NewRequest(tc.method, "/admin/token-hash", strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without auth: status=%d, want 401 (body=%s)", tc.method, rec.Code, rec.Body.String())
		}
	}

	// Authenticated with the admin token: route is reachable (200), proving the
	// 401s above are the auth gate, not a missing mount.
	req := httptest.NewRequest(http.MethodGet, "/admin/token-hash", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET with admin token: status=%d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
}

// An oversized body is rejected (MaxBytesReader) before SetHash runs.
func TestAdminTokenHashPostBodyTooLarge(t *testing.T) {
	mgr := &fakeTokenMgr{}
	srv := tokenHashRouter(mgr)
	big := `{"hash":"` + strings.Repeat("a", 8192) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/token-hash", strings.NewReader(big))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for oversized body", rec.Code)
	}
	if mgr.hash != "" {
		t.Errorf("oversized body must not set hash, got %q", mgr.hash)
	}
}
