package frontdesk

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestGetObservability verifies GET /api/observability reflects the log-export
// integration state derived from the environment. With neither LOG_FORMAT=json
// nor an OTLP endpoint set, both flags are false.
func TestGetObservability(t *testing.T) {
	srv, _ := newTestServer(t)
	t.Setenv("LOG_FORMAT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")

	rec := do(t, srv, http.MethodGet, "/api/observability", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/observability = %d (%s)", rec.Code, rec.Body.String())
	}
	var v map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v["log_export_json"] {
		t.Errorf("log_export_json = true, want false")
	}
	if v["log_export_otel"] {
		t.Errorf("log_export_otel = true, want false")
	}
	if v["log_export_metrics"] {
		t.Errorf("log_export_metrics = true, want false (no scrape token configured)")
	}
}

// TestGetObservabilityMetricsFlag confirms log_export_metrics flips on when a
// dedicated scrape token is configured.
func TestGetObservabilityMetricsFlag(t *testing.T) {
	srv, _ := newMetricsTestServer(t, "scrape-secret")

	rec := do(t, srv, http.MethodGet, "/api/observability", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/observability = %d (%s)", rec.Code, rec.Body.String())
	}
	var v map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !v["log_export_metrics"] {
		t.Errorf("log_export_metrics = false, want true")
	}
}

// TestGetObservabilityReflectsEnv confirms the flags flip on when the enabling
// environment variables are present.
func TestGetObservabilityReflectsEnv(t *testing.T) {
	srv, _ := newTestServer(t)
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")

	rec := do(t, srv, http.MethodGet, "/api/observability", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/observability = %d (%s)", rec.Code, rec.Body.String())
	}
	var v map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !v["log_export_json"] {
		t.Errorf("log_export_json = false, want true")
	}
	if !v["log_export_otel"] {
		t.Errorf("log_export_otel = false, want true")
	}
}

// TestGetObservabilityRequiresAuth confirms the endpoint sits behind the admin-
// or-session gate like the rest of the control-plane API.
func TestGetObservabilityRequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	if rec := do(t, srv, http.MethodGet, "/api/observability", "", false); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /api/observability = %d, want 401", rec.Code)
	}
}
