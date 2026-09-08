package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// createTwinProvider posts a provider and returns the recorder, so a test can
// assert on either the created row or the refusal.
func createTwinProvider(t *testing.T, r chi.Router, name string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"name":` + strconv.Quote(name) + `,"base_url":"https://api.example.com/v1","api_key":"sk-twin"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// renameTwinProvider puts a new name onto an existing provider.
func renameTwinProvider(t *testing.T, r chi.Router, id, name string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"name":` + strconv.Quote(name) + `}`
	req := httptest.NewRequest(http.MethodPut, "/providers/"+id, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// createdProviderID reads the id out of a 201 response.
func createdProviderID(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	return resp.ID
}

// Two names that differ only by a space where the other has a hyphen are one
// name as far as routing is concerned, so the create path refuses the second,
// and the refusal says why rather than claiming an exact duplicate.
func TestCreateProvider_RefusesNormalizedTwin(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	createdProviderID(t, createTwinProvider(t, r, "twin a"))

	rec := createTwinProvider(t, r, "twin-a")
	if rec.Code != http.StatusConflict {
		t.Fatalf("creating the twin returned %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "spaces and hyphens") {
		t.Errorf("refusal %q does not explain that spaces and hyphens are the same name", body)
	}
}

// The same rule on the rename path: a provider cannot take a name that is
// another provider's up to the normalization.
func TestUpdateProvider_RefusesNormalizedTwinOfAnotherProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	createdProviderID(t, createTwinProvider(t, r, "twin a"))
	otherID := createdProviderID(t, createTwinProvider(t, r, "other"))

	rec := renameTwinProvider(t, r, otherID, "twin-a")
	if rec.Code != http.StatusConflict {
		t.Fatalf("renaming onto the twin returned %d, want 409; body %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "spaces and hyphens") {
		t.Errorf("refusal %q does not explain that spaces and hyphens are the same name", body)
	}
}

// A provider renaming itself to its own twin spelling is not a conflict: the
// row that would collide is the row being renamed, and the normalized name it
// routes under does not change.
func TestUpdateProvider_AllowsOwnNormalizedTwinSpelling(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	id := createdProviderID(t, createTwinProvider(t, r, "twin a"))

	rec := renameTwinProvider(t, r, id, "twin-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("renaming a provider to its own twin spelling returned %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var stored string
	if err := apiTestDB.Pool().QueryRow(t.Context(), `SELECT name FROM providers WHERE id = $1`, id).Scan(&stored); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if stored != "twin-a" {
		t.Errorf("stored name = %q, want %q", stored, "twin-a")
	}
}
