package adminauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// ValidAdminOrSession is the admission half of RequireAdminOrSession, exported
// for long-lived connections (the Front Desk SSE stream) that re-ask the
// question after the middleware has run. The middleware suite covers the
// admission cases it can express as a status code; these cover the rest: the
// predicate answers on a request the middleware would have rejected for CSRF,
// and it works with no session manager at all.

// newValidAdminSession returns a session manager holding one admin session and
// one regular-user session, both DB-free.
func newValidAdminSession(t *testing.T) (mgr *webauthn.SessionManager, adminTok, userTok string) {
	t.Helper()
	mgr = webauthn.NewSessionManager(newMemStore())
	var err error
	if adminTok, err = mgr.CreateAuthToken(context.Background(), []byte("admin"), nil); err != nil {
		t.Fatalf("CreateAuthToken(admin): %v", err)
	}
	if userTok, err = mgr.CreateAuthToken(context.Background(), []byte("6f1d0f26-0f77-4a0a-9a1e-2b6b1f8f0f11"), nil); err != nil {
		t.Fatalf("CreateAuthToken(user): %v", err)
	}
	return mgr, adminTok, userTok
}

func TestValidAdminOrSession(t *testing.T) {
	mgr, adminTok, userTok := newValidAdminSession(t)
	adminOnly := &mockAdminAuth{validateFn: func(token string) bool { return token == "raw-admin" }}
	totpOn := func() bool { return true }

	tests := []struct {
		name   string
		method string
		build  func(r *http.Request)
		mgr    *webauthn.SessionManager
		totp   func() bool
		want   bool
	}{
		{
			name: "admin cookie",
			build: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: authcookie.FrontDesk.SessionCookie, Value: adminTok})
			},
			mgr:  mgr,
			want: true,
		},
		{
			name:   "admin cookie on an unsafe method without CSRF",
			method: http.MethodPost,
			build: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: authcookie.FrontDesk.SessionCookie, Value: adminTok})
			},
			mgr: mgr,
			// CSRF belongs to the middleware: the credential itself is valid, which
			// is all a re-check on an already-open stream asks about.
			want: true,
		},
		{
			name: "user cookie session",
			build: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: authcookie.FrontDesk.SessionCookie, Value: userTok})
			},
			mgr:  mgr,
			want: false,
		},
		{
			name: "admin cookie under the other app's jar name",
			build: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: authcookie.Dashboard.SessionCookie, Value: adminTok})
			},
			mgr:  mgr,
			want: false,
		},
		{
			name:  "raw admin token, TOTP off",
			build: func(r *http.Request) { r.Header.Set("Authorization", "Bearer raw-admin") },
			mgr:   mgr,
			want:  true,
		},
		{
			name:  "raw admin token, TOTP on",
			build: func(r *http.Request) { r.Header.Set("Authorization", "Bearer raw-admin") },
			mgr:   mgr,
			totp:  totpOn,
			want:  false,
		},
		{
			name:  "admin session as a bearer",
			build: func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+adminTok) },
			mgr:   mgr,
			totp:  totpOn,
			want:  true,
		},
		{
			name:  "user session as a bearer",
			build: func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+userTok) },
			mgr:   mgr,
			want:  false,
		},
		{
			name:  "unknown bearer",
			build: func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") },
			mgr:   mgr,
			want:  false,
		},
		{
			name:  "no credential at all",
			build: func(*http.Request) {},
			mgr:   mgr,
			want:  false,
		},
		{
			name:  "raw admin token with no session manager",
			build: func(r *http.Request) { r.Header.Set("Authorization", "Bearer raw-admin") },
			mgr:   nil,
			want:  true,
		},
		{
			name: "session cookie with no session manager",
			build: func(r *http.Request) {
				r.AddCookie(&http.Cookie{Name: authcookie.FrontDesk.SessionCookie, Value: adminTok})
			},
			mgr:  nil,
			want: false,
		},
		{
			name:  "unknown bearer with no session manager",
			build: func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") },
			mgr:   nil,
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			method := tc.method
			if method == "" {
				method = http.MethodGet
			}
			r := httptest.NewRequest(method, "/api/sse", http.NoBody)
			tc.build(r)
			got := ValidAdminOrSession(r, adminOnly, tc.mgr, tc.totp, authcookie.FrontDesk)
			if got != tc.want {
				t.Errorf("ValidAdminOrSession = %v, want %v", got, tc.want)
			}
		})
	}
}
