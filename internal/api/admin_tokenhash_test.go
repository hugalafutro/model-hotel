package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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
	mgr := &fakeTokenMgr{setErr: errInvalidHash}
	srv := tokenHashRouter(mgr)
	req := httptest.NewRequest(http.MethodPost, "/admin/token-hash", strings.NewReader(`{"hash":"short"}`))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// errInvalidHash is a stand-in for admin.Manager.SetHash's validation error.
var errInvalidHash = &stubErr{"invalid hash"}

type stubErr struct{ s string }

func (e *stubErr) Error() string { return e.s }
