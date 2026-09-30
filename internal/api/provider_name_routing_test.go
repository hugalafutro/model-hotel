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
		name, want := tc.name, tc.want
		t.Run("create "+name, func(t *testing.T) {
			rec := do("POST", "/providers", fmt.Sprintf(`{"name":%q,"base_url":"https://api.openai.com","api_key":"k"}`, name))
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
				t.Errorf("create %q: %d %s, want 400 naming %q", name, rec.Code, rec.Body.String(), want)
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

	for name, want := range map[string]string{
		"Slash/Test": `"code":"provider_name_slash"`,
		"hotel":      `"code":"provider_name_reserved"`,
	} {
		rec := do("PUT", "/providers/"+created.ID, fmt.Sprintf(`{"name":%q}`, name))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), want) {
			t.Errorf("rename to %q: %d %s, want 400 naming %q", name, rec.Code, rec.Body.String(), want)
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
