package frontdesk

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestGetVersion verifies GET /api/version reports the running build and a
// normalized commit. newTestServer leaves Version unset, so NewServer defaults it
// to "dev"; buildCommit is the un-stamped "unknown" sentinel under test.
func TestGetVersion(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/api/version", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/version = %d (%s)", rec.Code, rec.Body.String())
	}
	var v map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v["app_version"] != "dev" {
		t.Errorf("app_version = %q, want dev", v["app_version"])
	}
	if v["app_commit"] != "unknown" {
		t.Errorf("app_commit = %q, want unknown", v["app_commit"])
	}
}

// TestGetVersionRequiresAuth confirms the endpoint sits behind the admin-or-
// session gate like the rest of the control-plane API.
func TestGetVersionRequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	if rec := do(t, srv, http.MethodGet, "/api/version", "", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/version = %d, want 401", rec.Code)
	}
}

// TestVersionStamped confirms a stamped build flows through to the response and
// that a long commit SHA is shortened for display.
func TestVersionStamped(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.version = "v1.2.3"
	orig := buildCommit
	buildCommit = "0123456789abcdef0123456789abcdef01234567"
	t.Cleanup(func() { buildCommit = orig })

	rec := do(t, srv, http.MethodGet, "/api/version", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/version = %d", rec.Code)
	}
	var v map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v["app_version"] != "v1.2.3" {
		t.Errorf("app_version = %q, want v1.2.3", v["app_version"])
	}
	if v["app_commit"] != "0123456789ab" {
		t.Errorf("app_commit = %q, want 0123456789ab (12-char prefix)", v["app_commit"])
	}
}
