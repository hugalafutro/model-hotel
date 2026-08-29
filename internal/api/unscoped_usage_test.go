package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// usageScopeHarness builds a handler with real multi-user auth, so these tests
// drive the actual middleware chain with a real session token rather than a
// hand-placed identity. Placing the identity directly would exercise the guard
// in isolation and prove nothing about whether the endpoint applies it.
func usageScopeHarness(t *testing.T) (h *Handler, login func(userID string) string, mkUser func(name string) string) {
	t.Helper()
	h = newTestHandler(t)
	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, webauthn_sessions, virtual_keys CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	userRepo := user.NewRepository(pool)
	webauthnRepo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	h.SetWebAuthnSessionManager(sessionMgr)
	h.SetUserAuth(userRepo, webauthnRepo)

	adminRouter := chi.NewRouter()
	adminRouter.Use(h.AuthMiddleware)
	h.Register(adminRouter)

	login = func(userID string) string {
		token, err := sessionMgr.CreateAuthToken(context.Background(), []byte(userID), nil, webauthn.SessionMeta{})
		if err != nil {
			t.Fatalf("CreateAuthToken: %v", err)
		}
		return token
	}
	mkUser = func(name string) string {
		w := doJSON(t, adminRouter, http.MethodPost, "/users", envAdminToken,
			fmt.Sprintf(`{"username":%q,"password":"password123","role":"user","grants":["usage"]}`, name))
		if w.Code != http.StatusCreated {
			t.Fatalf("create user %s: %d %s", name, w.Code, w.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode user: %v", err)
		}
		return resp.ID
	}
	return h, login, mkUser
}

// /metrics carries modelhotel_tokens_total{provider,model,kind} and
// modelhotel_requests_total, neither of which has an owner label: they are
// fleet-wide totals across every virtual-key owner.
//
// With METRICS_TOKEN unset the endpoint fell back to AuthMiddleware, which
// admits ANY resolved identity — requireAdmin is a separate middleware that this
// path never applied. So a non-admin multi-user session could scrape every other
// tenant's token consumption. The code called that fallback "admin auth"; it was
// merely authenticated auth.
func TestMetricsHandler_FallbackAdmitsAdminsOnly(t *testing.T) {
	h, login, mkUser := usageScopeHarness(t)
	if h.cfg.MetricsToken != "" {
		t.Fatalf("test config sets METRICS_TOKEN (%q); the fallback is what is under test", h.cfg.MetricsToken)
	}

	userID := mkUser("metrics-scoper")
	r := chi.NewRouter()
	r.Handle("/metrics", h.MetricsHandler())

	for _, tc := range []struct {
		name  string
		token string
		want  int
	}{
		{"admin token", envAdminToken, http.StatusOK},
		{"non-admin session", login(userID), http.StatusForbidden},
		{"no credential", "", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

// GET /api/system/ is deliberately open to every authenticated role — its own
// comment says "routing metadata and process gauges only". requests_today was
// neither: a COUNT(*) over request_logs with no owner filter, so a non-admin
// read the fleet's total traffic volume. Its 30s cache was keyed on the `since`
// param alone, never on the caller, so scoping the query without scoping the
// cache would still have served one owner's number to the next.
func TestGetSystem_RequestsTodayIsScopedToTheCaller(t *testing.T) {
	h, login, mkUser := usageScopeHarness(t)
	pool := h.Pool().Pool()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM request_logs`); err != nil {
		t.Fatalf("clear request_logs: %v", err)
	}

	mineID, theirsID := mkUser("owner-one"), mkUser("owner-two")
	// Keyless rows carry owner_user_id directly (migration 067): one for the
	// first owner, two for the second.
	for _, owner := range []string{mineID, theirsID, theirsID} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO request_logs (id, model_id, request_hash, streaming, virtual_key_name, failover_attempt, state, endpoint_type, owner_user_id, created_at)
			VALUES ($1, 'm', $2, false, 'k', 0, 'completed', 'chat', $3, NOW())`,
			uuid.New(), uuid.NewString()[:16], owner); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	since := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	get := func(token string) int64 {
		t.Helper()
		w := doJSON(t, r, http.MethodGet, "/system/?since="+since, token, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", w.Code, w.Body.String())
		}
		var out SystemStats
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out.App.RequestsToday
	}

	mine := get(login(mineID))
	// Immediately after, inside both cache windows: a cache keyed only on
	// `since` hands this caller the number above.
	theirs := get(login(theirsID))
	all := get(envAdminToken)

	if mine != 1 {
		t.Errorf("first owner saw requests_today = %d, want 1: the count is not scoped", mine)
	}
	if theirs != 2 {
		t.Errorf("second owner saw requests_today = %d, want 2: a cached count leaked across callers", theirs)
	}
	if all != 3 {
		t.Errorf("admin saw requests_today = %d, want 3 (the fleet total)", all)
	}
}
