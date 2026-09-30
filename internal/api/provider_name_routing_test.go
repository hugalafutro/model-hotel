package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// A provider name the proxy's "provider/model" split can never reach is
// refused on create and on rename with a code the dashboard phrases, and the
// rename leaves the stored name alone.
func TestProviderName_UnroutableNamesRefused(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		return rec
	}

	// " hotel " is trimmed before the check, so it is refused too.
	for _, tc := range []struct{ name, want string }{
		{"Slash/Test", `"code":"provider_name_slash"`},
		{"hotel", `"code":"provider_name_reserved"`},
		{" hotel ", `"code":"provider_name_reserved"`},
	} {
		t.Run("create "+tc.name, func(t *testing.T) {
			rec := do("POST", "/providers", fmt.Sprintf(`{"name":%q,"base_url":"https://api.openai.com","api_key":"k"}`, tc.name))
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("create %q: %d %s, want 400 naming %q", tc.name, rec.Code, rec.Body.String(), tc.want)
			}
		})
	}

	original := "test-routing-name-" + uuid.New().String()[:8]
	rec := do("POST", "/providers", fmt.Sprintf(`{"name":%q,"base_url":"https://api.openai.com","api_key":"k"}`, original))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %q: %d %s", original, rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	for _, tc := range []struct{ name, want string }{
		{"Slash/Test", `"code":"provider_name_slash"`},
		{"hotel", `"code":"provider_name_reserved"`},
		{" hotel ", `"code":"provider_name_reserved"`},
	} {
		rec := do("PUT", "/providers/"+created.ID, fmt.Sprintf(`{"name":%q}`, tc.name))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.want) {
			t.Errorf("rename to %q: %d %s, want 400 naming %q", tc.name, rec.Code, rec.Body.String(), tc.want)
		}
	}

	rec = do("GET", "/providers/"+created.ID, "")
	var got struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Name != original {
		t.Errorf("stored name after refused renames = %q (err %v), want %q", got.Name, err, original)
	}
}

// A provider that already has a name the rule now refuses keeps it: a save
// that resends the name unchanged goes through, and only a rename onto such a
// name is refused.
func TestProviderName_ExistingNameIsKept(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		return rec
	}

	rec := do("POST", "/providers", fmt.Sprintf(`{"name":"legacy-%s","base_url":"https://api.openai.com","api_key":"k"}`, uuid.New().String()[:8]))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	// A name from before the rule existed, written the only way one can be now.
	legacy := "legacy/" + created.ID[:8]
	if _, err := apiTestDB.Pool().Exec(t.Context(), `UPDATE providers SET name = $1 WHERE id = $2`, legacy, created.ID); err != nil {
		t.Fatalf("seed legacy name: %v", err)
	}

	rec = do("PUT", "/providers/"+created.ID, fmt.Sprintf(`{"name":%q,"enabled":false}`, legacy))
	if rec.Code != http.StatusOK {
		t.Fatalf("resending the existing name: %d %s, want 200", rec.Code, rec.Body.String())
	}
	rec = do("PUT", "/providers/"+created.ID, `{"name":"other/name"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"provider_name_slash"`) {
		t.Fatalf("renaming onto another unroutable name: %d %s, want the slash refusal", rec.Code, rec.Body.String())
	}
}
