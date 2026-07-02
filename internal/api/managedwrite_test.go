package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// memberFleetSettings seeds a fakeFleetSettings to the "member" state: a fresh
// heartbeat (well within fleetManagedTTL) from a non-primary node. isManagedMember
// uses the real clock, so the timestamp is relative to time.Now().
func memberFleetSettings() *fakeFleetSettings {
	return &fakeFleetSettings{values: map[string]string{
		keyFleetManagedSeenAt: time.Now().Add(-5 * time.Second).Format(time.RFC3339),
		keyFleetIsPrimary:     "false",
	}}
}

func TestIsManagedMember(t *testing.T) {
	fresh := func() string { return time.Now().Add(-5 * time.Second).Format(time.RFC3339) }
	stale := func() string { return time.Now().Add(-(fleetManagedTTL + time.Minute)).Format(time.RFC3339) }

	tests := []struct {
		name   string
		values map[string]string
		want   bool
	}{
		{"member: fresh non-primary", map[string]string{keyFleetManagedSeenAt: fresh(), keyFleetIsPrimary: "false"}, true},
		{"primary: fresh primary", map[string]string{keyFleetManagedSeenAt: fresh(), keyFleetIsPrimary: "true"}, false},
		{"warning: stale heartbeat", map[string]string{keyFleetManagedSeenAt: stale(), keyFleetIsPrimary: "false"}, false},
		{"standalone: never contacted", map[string]string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := &fakeFleetSettings{values: tt.values}
			if got := isManagedMember(context.Background(), fs); got != tt.want {
				t.Errorf("isManagedMember = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestManagedWriteGuard verifies the middleware in isolation: on a managed member
// it refuses the request with 403 and never calls next; otherwise it passes
// through. The guard is mounted only on write routes, so it is intentionally
// method-agnostic (every request that reaches it is already a synced-entity write).
func TestManagedWriteGuard(t *testing.T) {
	var called bool
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// Managed member: refused.
	called = false
	rec := httptest.NewRecorder()
	managedWriteGuard(memberFleetSettings())(next).ServeHTTP(
		rec, httptest.NewRequest(http.MethodPost, "/providers", http.NoBody))
	if called {
		t.Error("managed member: next handler must not be called")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("managed member: expected 403, got %d", rec.Code)
	}

	// Primary / standalone: passes through.
	for _, fs := range []*fakeFleetSettings{
		{values: map[string]string{keyFleetManagedSeenAt: time.Now().Add(-5 * time.Second).Format(time.RFC3339), keyFleetIsPrimary: "true"}},
		newFakeFleetSettings(), // standalone
	} {
		called = false
		rec = httptest.NewRecorder()
		managedWriteGuard(fs)(next).ServeHTTP(
			rec, httptest.NewRequest(http.MethodPost, "/providers", http.NoBody))
		if !called || rec.Code != http.StatusOK {
			t.Errorf("non-member: expected pass-through 200, got code=%d called=%v", rec.Code, called)
		}
	}
}

// TestManagedBlocksSyncableSettings covers the per-key settings policy: a managed
// member is blocked only when a write touches a syncable key. An instance-local
// apprise key passes (mirroring the mixed Alerts section in the dashboard), and a
// non-member is never blocked.
func TestManagedBlocksSyncableSettings(t *testing.T) {
	member := memberFleetSettings()
	primary := &fakeFleetSettings{values: map[string]string{
		keyFleetManagedSeenAt: time.Now().Add(-5 * time.Second).Format(time.RFC3339),
		keyFleetIsPrimary:     "true",
	}}

	tests := []struct {
		name string
		fs   *fakeFleetSettings
		keys []string
		want bool
	}{
		{"member + syncable key", member, []string{"alert_enabled"}, true},
		{"member + apprise-only key", member, []string{"alert_apprise_api_url"}, false},
		{"member + mixed batch", member, []string{"alert_apprise_api_url", "alert_enabled"}, true},
		{"member + no keys", member, nil, false},
		{"primary + syncable key", primary, []string{"alert_enabled"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := managedBlocksSyncableSettings(context.Background(), tt.fs, tt.keys); got != tt.want {
				t.Errorf("managedBlocksSyncableSettings(%v) = %v, want %v", tt.keys, got, tt.want)
			}
		})
	}
}

// TestHandlerRegister_ManagedMember verifies the wiring end to end on a real
// router: while this instance is a managed member, synced-entity writes are
// refused with 403, but reads, the failover sync (auto-group regeneration), and
// instance-local apprise settings stay usable. Flipping to primary lifts the lock.
func TestHandlerRegister_ManagedMember(t *testing.T) {
	h, r := newTestHandlerWithRouter(t) // skips if no test DB
	ctx := context.Background()

	// Enroll this instance as a fresh, non-primary fleet member.
	if err := h.settingsRepo.Set(ctx, keyFleetManagedSeenAt, time.Now().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	if err := h.settingsRepo.Set(ctx, keyFleetIsPrimary, "false"); err != nil {
		t.Fatal(err)
	}

	// Wire the multi-user store so the /users read handlers (ListUsers,
	// ListGrantCatalog) don't hit a nil userRepo in this harness.
	pool := h.Pool().Pool()
	h.SetUserAuth(user.NewRepository(pool), webauthn.NewRepository(pool))

	auth := func(req *http.Request) *http.Request {
		req.Header.Set("Authorization", "Bearer test-admin-token")
		return req
	}
	do := func(method, path, body string) (int, string) {
		rec := httptest.NewRecorder()
		req := auth(httptest.NewRequest(method, path, strings.NewReader(body)))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		r.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}

	// Synced-entity writes are refused across every guarded route. Bodies can be
	// minimal: the guard middleware runs before the handler parses them.
	for _, w := range []struct{ name, method, path, body string }{
		{"POST /providers", http.MethodPost, "/providers", `{"name":"x","base_url":"http://localhost:1234"}`},
		{"POST /virtual-keys", http.MethodPost, "/virtual-keys", `{}`},
		{"POST /failover-groups", http.MethodPost, "/failover-groups", `{}`},
		{"POST /users", http.MethodPost, "/users", `{"username":"x","password":"password123","role":"user"}`},
		{"PUT /users/{id}", http.MethodPut, "/users/00000000-0000-0000-0000-000000000001", `{}`},
		{"POST /users/{id}/password", http.MethodPost, "/users/00000000-0000-0000-0000-000000000001/password", `{}`},
		{"DELETE /users/{id}", http.MethodDelete, "/users/00000000-0000-0000-0000-000000000001", ""},
		{"PUT /settings (syncable)", http.MethodPut, "/settings", `{"alert_enabled":"true"}`},
		{"DELETE /settings (reset all)", http.MethodDelete, "/settings", `{}`},
	} {
		if code, _ := do(w.method, w.path, w.body); code != http.StatusForbidden {
			t.Errorf("managed %s: expected 403, got %d", w.name, code)
		}
	}

	// Reads, failover sync, and instance-local apprise settings stay usable.
	if code, _ := do(http.MethodGet, "/providers", ""); code != http.StatusOK {
		t.Errorf("managed GET /providers: expected 200, got %d", code)
	}
	// /users reads stay open on a member (writes are guarded above).
	if code, _ := do(http.MethodGet, "/users", ""); code != http.StatusOK {
		t.Errorf("managed GET /users: expected 200, got %d", code)
	}
	if code, _ := do(http.MethodGet, "/users/grants", ""); code != http.StatusOK {
		t.Errorf("managed GET /users/grants: expected 200, got %d", code)
	}
	if code, _ := do(http.MethodPost, "/failover-groups/sync", ""); code == http.StatusForbidden {
		t.Errorf("managed POST /failover-groups/sync: must be exempt, got 403")
	}
	if code, _ := do(http.MethodPut, "/settings", `{"alert_apprise_api_url":"http://apprise:8000"}`); code != http.StatusOK {
		t.Errorf("managed PUT /settings (apprise-only): expected 200, got %d", code)
	}
	// Reset of an instance-local key only (no syncable key in the batch) is allowed.
	if code, _ := do(http.MethodDelete, "/settings", `{"keys":["alert_apprise_api_url"]}`); code != http.StatusOK {
		t.Errorf("managed DELETE /settings (apprise-only): expected 200, got %d", code)
	}

	// Promotion to primary lifts the lock: the write is not just un-refused, it
	// persists (the provider is created and then listed).
	if err := h.settingsRepo.Set(ctx, keyFleetIsPrimary, "true"); err != nil {
		t.Fatal(err)
	}
	if code, _ := do(http.MethodPost, "/providers", `{"name":"primary-prov","base_url":"http://localhost:1234"}`); code != http.StatusCreated {
		t.Errorf("primary POST /providers: expected 201 Created, got %d", code)
	}
	code, body := do(http.MethodGet, "/providers", "")
	if code != http.StatusOK || !strings.Contains(body, "primary-prov") {
		t.Errorf("primary GET /providers: expected the created provider to persist, got code=%d body=%q", code, body)
	}
}
