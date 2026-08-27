package frontdesk

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/httpx"
)

// TestPair_OversizedBody_413 covers the reason Front Desk's decodeJSON is
// bounded at all: the pairing exchange is unauthenticated by design (only the
// per-IP limiter stands in front of it), so without a ceiling anyone who can
// reach Front Desk could make it read a body of any size. The code is valid
// JSON and simply wrong, so a 400 here would mean the size limit did nothing.
func TestPair_OversizedBody_413(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"code":"nope","label":"` + strings.Repeat("a", httpx.MaxJSONBody+1) + `"}`
	rec := do(t, srv, http.MethodPost, "/api/pair", body, false)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "aaaa") {
		t.Errorf("the rejection must not echo the body: %q", rec.Body.String())
	}
}

// TestPair_MalformedBody_400 keeps the two failure modes distinct: malformed
// still answers 400, so the new 413 means "too much", not "unparseable".
func TestPair_MalformedBody_400(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodPost, "/api/pair", `{"code":`, false)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
}

// TestAuthenticatedEndpoint_OversizedBody_413 pins that the bound is not
// pairing-only: every handler in the package decodes through the same helper,
// so an admin-authenticated route is bounded too.
func TestAuthenticatedEndpoint_OversizedBody_413(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"name":"` + strings.Repeat("a", httpx.MaxJSONBody+1) + `","url":"http://example.com"}`
	rec := do(t, srv, http.MethodPost, "/api/members", body, true)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%q", rec.Code, rec.Body.String())
	}
}
