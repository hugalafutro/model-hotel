package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
)

const exchangeAdminToken = "test-admin-token"

// cookieByName returns the response cookie with the given name, or nil.
func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// TestRegisterAuthExchange_MountsRoutes is the dashboard's stake in the shared
// exchange: both auth-exempt routes answer, and the exchange mints the
// dashboard's own session cookie rather than Front Desk's. The handler's own
// behaviour (bad token, missing token, TOTP refusal, mint failure, body limit)
// is covered where it lives, in internal/adminauth.
func TestRegisterAuthExchange_MountsRoutes(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{
		validateFn: func(tok string) bool { return tok == exchangeAdminToken },
	}, nil)
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		createFn: func(_ context.Context, _, _ []byte) (string, error) { return "admin-sess", nil },
	})

	r := chi.NewRouter()
	h.RegisterAuthExchange(r)

	rec := httptest.NewRecorder()
	body := `{"admin_token":"` + exchangeAdminToken + `"}`
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/admin-exchange", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/admin-exchange = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	c := cookieByName(rec, authcookie.SessionCookie)
	if c == nil || c.Value != "admin-sess" || !c.HttpOnly {
		t.Fatalf("want an HttpOnly %s cookie carrying the minted token, got %+v", authcookie.SessionCookie, rec.Result().Cookies())
	}

	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody))
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST /auth/logout = %d, want 200 (%s)", rec2.Code, rec2.Body.String())
	}
}
