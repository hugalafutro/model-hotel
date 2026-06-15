package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/config"
)

// TestGetPublicConfig verifies the unauthenticated endpoint reflects the
// DemoReadOnly flag and emits the expected JSON shape. It uses a minimal
// handler (cfg only) since GetPublicConfig touches nothing else — no DB needed.
func TestGetPublicConfig(t *testing.T) {
	for _, readOnly := range []bool{true, false} {
		h := &Handler{cfg: &config.Config{DemoReadOnly: readOnly}}

		rec := httptest.NewRecorder()
		h.GetPublicConfig(rec, httptest.NewRequest(http.MethodGet, "/public-config", http.NoBody))

		if rec.Code != http.StatusOK {
			t.Fatalf("read_only=%v: expected 200, got %d", readOnly, rec.Code)
		}
		var got PublicConfigResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("read_only=%v: decode response: %v", readOnly, err)
		}
		if got.ReadOnly != readOnly {
			t.Errorf("read_only=%v: response ReadOnly=%v", readOnly, got.ReadOnly)
		}
	}
}

// TestRegisterPublicConfig verifies the route is mounted and reachable end to
// end through a router (the handler test above calls GetPublicConfig directly).
func TestRegisterPublicConfig(t *testing.T) {
	h := &Handler{cfg: &config.Config{DemoReadOnly: true}}
	r := chi.NewRouter()
	h.RegisterPublicConfig(r)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/public-config", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var got PublicConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.ReadOnly {
		t.Error("expected ReadOnly true via mounted route")
	}
}
