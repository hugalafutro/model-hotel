package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/httpx"
)

// oversizedJSON returns a syntactically valid JSON object of at least n bytes.
// Valid on purpose: a malformed body would earn a 400 on its own, which would
// let a size test pass without the size limit doing anything.
func oversizedJSON(n int) string {
	return `{"admin_token":"x","padding":"` + strings.Repeat("a", n) + `"}`
}

// TestAdminTokenExchange_OversizedBody_413 covers the pre-auth surface: the
// exchange is the login front-end and sits in the auth-exempt route group, so
// its body is one an anonymous caller controls end to end.
func TestAdminTokenExchange_OversizedBody_413(t *testing.T) {
	h := exchangeHandler(t)
	rec := httptest.NewRecorder()
	body := oversizedJSON(httpx.MaxJSONBody + 1)
	h.AdminTokenExchange(rec, httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange", strings.NewReader(body)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "padding") {
		t.Errorf("the rejection must not echo the body: %q", rec.Body.String())
	}
}

// TestFleetAnnounce_KeepsItsOwnTighterLimit pins that decodeJSONLimit really
// threads the endpoint's ceiling rather than quietly falling back to the
// package default: this body is far under httpx.MaxJSONBody, so it would be
// accepted if announce had been folded onto the default limit, and it is well
// over the 1 KiB an announce needs.
func TestFleetAnnounce_KeepsItsOwnTighterLimit(t *testing.T) {
	h := NewFleetHandler(newFakeFleetSettings())
	rec := httptest.NewRecorder()
	body := `{"frontdesk_id":"fd-1","primary_name":"` + strings.Repeat("a", maxAnnounceBody) + `"}`
	if len(body) >= httpx.MaxJSONBody {
		t.Fatalf("test body (%d) must stay under the package default (%d)", len(body), httpx.MaxJSONBody)
	}
	h.Announce(rec, httptest.NewRequest(http.MethodPost, "/fleet/announce", strings.NewReader(body)))

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413; body=%q", rec.Code, rec.Body.String())
	}
}

// TestConfigSyncImport_AcceptsBodyAboveDefaultLimit is the other direction: the
// import's ceiling is deliberately larger than the default, so an envelope that
// a control plane could really send must not be cut off at 1 MiB. The schema
// version is wrong on purpose, so the 422 proves the body was decoded rather
// than refused for its size.
func TestConfigSyncImport_AcceptsBodyAboveDefaultLimit(t *testing.T) {
	h := &ConfigSyncHandler{}
	rec := httptest.NewRecorder()
	body := `{"schema_version":0,"padding":"` + strings.Repeat("a", httpx.MaxJSONBody+1) + `"}`
	if len(body) >= maxConfigImportBody {
		t.Fatalf("test body (%d) must stay under the import ceiling (%d)", len(body), maxConfigImportBody)
	}
	h.Import(rec, httptest.NewRequest(http.MethodPost, "/api/config/import", strings.NewReader(body)))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (decoded, wrong schema version); body=%q", rec.Code, rec.Body.String())
	}
}

// TestClearAppLogs_OptionalBodyStaysOptionalButBounded pins both halves of the
// tolerant decode: no body still means "clear everything", and an oversized one
// is still refused.
func TestClearAppLogs_OptionalBodyStaysOptionalButBounded(t *testing.T) {
	h := testHandler(nil, nil, nil, &mockAdminAuth{}, nil)

	rec := httptest.NewRecorder()
	h.ClearAppLogs(rec, httptest.NewRequest(http.MethodPost, "/api/logs/app/clear", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("empty body status = %d, want 200; body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	body := `{"older_than":"all","padding":"` + strings.Repeat("a", httpx.MaxJSONBody+1) + `"}`
	h.ClearAppLogs(rec, httptest.NewRequest(http.MethodPost, "/api/logs/app/clear", strings.NewReader(body)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d, want 413; body=%q", rec.Code, rec.Body.String())
	}
}
