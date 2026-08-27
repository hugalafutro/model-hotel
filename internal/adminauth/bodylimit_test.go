package adminauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/httpx"
)

// TestTokenExchange_OversizedBody_413 covers this package's defining property:
// every ceremony here (token exchange, password login, TOTP, WebAuthn) is
// reachable before authentication, so their request bodies are the ones an
// anonymous caller fully controls. The body is valid JSON with a valid token,
// so only the size limit can reject it.
func TestTokenExchange_OversizedBody_413(t *testing.T) {
	sessionMgr := newTestSessionManager(t)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "sekrit" }}
	h := TokenExchange(adminMgr, sessionMgr, nil, authcookie.FrontDesk, "never", nil)

	body := `{"admin_token":"sekrit","padding":"` + strings.Repeat("a", httpx.MaxJSONBody+1) + `"}`
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(body)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%q", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("a rejected exchange must mint no cookie, got %+v", rec.Result().Cookies())
	}
}

// TestTokenExchange_MissingToken_400 pins the split of the old combined
// decode-or-empty-token condition: an empty token is still a 400, not a 413 and
// not a session.
func TestTokenExchange_MissingToken_400(t *testing.T) {
	sessionMgr := newTestSessionManager(t)
	adminMgr := &mockAdminAuth{validateFn: func(string) bool { return true }}
	h := TokenExchange(adminMgr, sessionMgr, nil, authcookie.FrontDesk, "never", nil)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(`{}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Errorf("an empty admin_token must mint no cookie, got %+v", rec.Result().Cookies())
	}
}
