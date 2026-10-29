package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
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
	req := httptest.NewRequest(http.MethodPost, "/auth/admin-exchange", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
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

// metaCapturingSessionMgr records the device metadata a minted session is
// stored with.
type metaCapturingSessionMgr struct {
	mockWebAuthnSessionMgr
	meta webauthn.SessionMeta
}

func (m *metaCapturingSessionMgr) CreateAuthToken(_ context.Context, _, _ []byte, meta webauthn.SessionMeta) (string, error) {
	m.meta = meta
	return "admin-sess", nil
}

// fixedClientIP resolves every request to one address, as a trusted-proxy
// resolver would for a forwarded client.
type fixedClientIP string

func (f fixedClientIP) ClientIP(*http.Request) string { return string(f) }

// The exchange stores the address the wired client-IP source resolves, not the
// peer address httptest gives the request (192.0.2.1).
func TestRegisterAuthExchange_UsesClientIPSource(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{
		validateFn: func(tok string) bool { return tok == exchangeAdminToken },
	}, nil)
	mgr := &metaCapturingSessionMgr{}
	h.SetWebAuthnSessionManager(mgr)
	h.SetClientIPSource(fixedClientIP("203.0.113.7"))

	r := chi.NewRouter()
	h.RegisterAuthExchange(r)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/admin-exchange", strings.NewReader(`{"admin_token":"`+exchangeAdminToken+`"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /auth/admin-exchange = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if mgr.meta.IP != "203.0.113.7" {
		t.Errorf("session IP = %q, want the resolver's 203.0.113.7", mgr.meta.IP)
	}
}
