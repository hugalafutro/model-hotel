package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/config"
)

// TestGetDemoLogin verifies the token is exposed only when DEMO_SHOW_TOKEN and
// DEMO_READONLY are both set and an admin token is configured; every other
// combination must yield an empty token (and never a non-200).
func TestGetDemoLogin(t *testing.T) {
	const adminToken = "demo-abc123"

	cases := []struct {
		name      string
		showToken bool
		readOnly  bool
		admin     string
		want      string
	}{
		{"both on with token", true, true, adminToken, adminToken},
		{"read-only off", true, false, adminToken, ""},
		{"show-token off", false, true, adminToken, ""},
		{"both off", false, false, adminToken, ""},
		{"both on but no admin token", true, true, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &Handler{
				cfg: &config.Config{
					DemoShowToken: tc.showToken,
					DemoReadOnly:  tc.readOnly,
				},
				adminMgr: &mockAdminAuth{tokenVal: tc.admin},
			}

			rec := httptest.NewRecorder()
			h.GetDemoLogin(rec, httptest.NewRequest(http.MethodGet, "/demo-login", http.NoBody))

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", rec.Code)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Errorf("Cache-Control = %q, want %q", cc, "no-store")
			}
			var got DemoLoginResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.Token != tc.want {
				t.Errorf("token = %q, want %q", got.Token, tc.want)
			}
		})
	}
}

// TestRegisterDemoLogin verifies the route is mounted and reachable end to end
// through a router.
func TestRegisterDemoLogin(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{
			DemoShowToken: true,
			DemoReadOnly:  true,
		},
		adminMgr: &mockAdminAuth{tokenVal: "demo-xyz"},
	}
	r := chi.NewRouter()
	h.RegisterDemoLogin(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/demo-login", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got DemoLoginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Token != "demo-xyz" {
		t.Errorf("token = %q, want %q via mounted route", got.Token, "demo-xyz")
	}
}
