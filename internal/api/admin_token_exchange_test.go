package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
)

const exchangeAdminToken = "test-admin-token"

// exchangeHandler builds a mock-backed Handler for the admin-token exchange
// endpoint: the admin authenticator accepts exchangeAdminToken, and the session
// manager mints a deterministic token so tests never touch a real session store.
func exchangeHandler(t *testing.T) *Handler {
	t.Helper()
	h := testHandler(nil, nil, nil, &mockAdminAuth{
		validateFn: func(tok string) bool { return tok == exchangeAdminToken },
	}, nil)
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		createFn: func(_ context.Context, _, _ []byte) (string, error) { return "admin-sess", nil },
	})
	return h
}

// cookieByName returns the response cookie with the given name, or nil.
func cookieByName(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestAdminTokenExchange_ValidToken_SetsCookie(t *testing.T) {
	h := exchangeHandler(t)
	rec := httptest.NewRecorder()
	body := `{"admin_token":"` + exchangeAdminToken + `"}`
	h.AdminTokenExchange(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(body)))

	if rec.Code != http.StatusOK {
		t.Fatalf("exchange = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	c := cookieByName(rec, authcookie.SessionCookie)
	if c == nil {
		t.Fatalf("expected %s cookie", authcookie.SessionCookie)
	}
	if !c.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if c.Value != "admin-sess" {
		t.Errorf("cookie value = %q, want minted session token", c.Value)
	}
	if strings.Contains(rec.Body.String(), "token") {
		t.Errorf("body must not echo any token, got %q", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "success") {
		t.Errorf("body should report success, got %q", rec.Body.String())
	}
}

func TestAdminTokenExchange_BadToken_401(t *testing.T) {
	h := exchangeHandler(t)
	rec := httptest.NewRecorder()
	h.AdminTokenExchange(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(`{"admin_token":"nope"}`)))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token = %d, want 401", rec.Code)
	}
	if cookieByName(rec, authcookie.SessionCookie) != nil {
		t.Error("no session cookie should be set for a bad token")
	}
}

func TestAdminTokenExchange_EmptyToken_400(t *testing.T) {
	h := exchangeHandler(t)
	rec := httptest.NewRecorder()
	h.AdminTokenExchange(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(`{"admin_token":""}`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty token = %d, want 400", rec.Code)
	}
}

func TestAdminTokenExchange_MalformedBody_400(t *testing.T) {
	h := exchangeHandler(t)
	rec := httptest.NewRecorder()
	h.AdminTokenExchange(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(`{not-json`)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed body = %d, want 400", rec.Code)
	}
}

func TestAdminTokenExchange_NoSessionManager_500(t *testing.T) {
	// Build a handler without SetWebAuthnSessionManager: a misconfigured build
	// must fail closed with 500 rather than panicking on a nil session manager.
	h := testHandler(nil, nil, nil, &mockAdminAuth{
		validateFn: func(tok string) bool { return tok == exchangeAdminToken },
	}, nil)

	rec := httptest.NewRecorder()
	body := `{"admin_token":"` + exchangeAdminToken + `"}`
	h.AdminTokenExchange(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(body)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("no session manager = %d, want 500 (%s)", rec.Code, rec.Body.String())
	}
	if cookieByName(rec, authcookie.SessionCookie) != nil {
		t.Error("no session cookie should be set when the session manager is unavailable")
	}
}

func TestAdminTokenExchange_CreateAuthTokenError_500(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{
		validateFn: func(tok string) bool { return tok == exchangeAdminToken },
	}, nil)
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		createFn: func(_ context.Context, _, _ []byte) (string, error) {
			return "", errors.New("session store unavailable")
		},
	})

	rec := httptest.NewRecorder()
	body := `{"admin_token":"` + exchangeAdminToken + `"}`
	h.AdminTokenExchange(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(body)))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("CreateAuthToken error = %d, want 500 (%s)", rec.Code, rec.Body.String())
	}
	if cookieByName(rec, authcookie.SessionCookie) != nil {
		t.Error("no session cookie should be set when CreateAuthToken fails")
	}
}

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

	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, httptest.NewRequest(http.MethodPost, "/auth/logout", http.NoBody))
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST /auth/logout = %d, want 200 (%s)", rec2.Code, rec2.Body.String())
	}
}

func TestAdminTokenExchange_TotpEnabled_400(t *testing.T) {
	h := exchangeHandler(t)
	// With 2FA on the admin token alone is not sufficient; callers must use the
	// TOTP login flow, so the exchange refuses to mint a session.
	h.SetTotpStatus(&stubTotpStatus{enabled: true})
	h.totpEnabled.Store(true)

	rec := httptest.NewRecorder()
	body := `{"admin_token":"` + exchangeAdminToken + `"}`
	h.AdminTokenExchange(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(body)))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("totp enabled = %d, want 400", rec.Code)
	}
	if cookieByName(rec, authcookie.SessionCookie) != nil {
		t.Error("no session cookie should be set when TOTP is enabled")
	}
}
