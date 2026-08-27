package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

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

// TestClearAppLogs_OptionalBodyStaysOptionalButBounded pins all three halves of
// the optional decode on the one endpoint that uses it. No body still means
// "clear everything"; an oversized body is refused; and a body that was sent but
// cannot be read is refused rather than falling back to the clear-everything
// default, which is the difference between clearing an hour of logs and
// clearing all of them.
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

	// A truncated "clear the last hour" must not become "clear everything".
	rec = httptest.NewRecorder()
	h.ClearAppLogs(rec, httptest.NewRequest(http.MethodPost, "/api/logs/app/clear", strings.NewReader(`{"older_than":"1h"`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("truncated body status = %d, want 400; body=%q", rec.Code, rec.Body.String())
	}
}

// TestBulkDeleteWorstCaseFitsDefaultLimit ties together the two numbers that
// have to stay in step. A full bulk model delete is the largest body any route
// bounded by the package default can legitimately carry, so raising
// maxBulkDeleteIDs without revisiting httpx.MaxJSONBody would start answering
// 413 to a request the Models page can really send: the user selects everything
// and the clear silently fails. Sized with the real encoder rather than an
// estimate of how long a UUID is.
func TestBulkDeleteWorstCaseFitsDefaultLimit(t *testing.T) {
	ids := make([]string, maxBulkDeleteIDs)
	for i := range ids {
		ids[i] = uuid.New().String()
	}
	body, err := json.Marshal(BulkDeleteRequest{IDs: ids})
	if err != nil {
		t.Fatalf("marshal worst-case bulk delete: %v", err)
	}

	// Decoded through the real helper at the real limit, so this fails the same
	// way the endpoint would.
	rec := httptest.NewRecorder()
	var got BulkDeleteRequest
	if !decodeJSON(rec, httptest.NewRequest(http.MethodPost, "/api/models/bulk-delete", strings.NewReader(string(body))), &got) {
		t.Fatalf("a full %d-ID bulk delete (%d bytes) does not fit the %d-byte default limit; status %d",
			maxBulkDeleteIDs, len(body), httpx.MaxJSONBody, rec.Code)
	}
	if len(got.IDs) != maxBulkDeleteIDs {
		t.Errorf("decoded %d ids, want %d", len(got.IDs), maxBulkDeleteIDs)
	}
}
