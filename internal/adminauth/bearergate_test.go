package adminauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBearerTokenGate covers the three outcomes the gate distinguishes: the
// matching bearer passes through, a wrong one is refused, and an absent
// Authorization header is refused with the same client-visible message.
func TestBearerTokenGate(t *testing.T) {
	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantNext   bool
	}{
		{"matching token passes", "Bearer secret-token", http.StatusOK, true},
		{"wrong token refused", "Bearer wrong-token", http.StatusUnauthorized, false},
		{"empty bearer refused", "Bearer ", http.StatusUnauthorized, false},
		{"missing header refused", "", http.StatusUnauthorized, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			gate := BearerTokenGate("secret-token", "metrics", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			gate.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if called != tc.wantNext {
				t.Errorf("next called = %v, want %v", called, tc.wantNext)
			}
			if !tc.wantNext && rec.Body.String() != "invalid metrics token\n" {
				t.Errorf("body = %q, want the generic refusal", rec.Body.String())
			}
		})
	}
}

// TestBearerTokenGate_EmptyWantRefusesEverything pins the fail-closed shape: an
// unconfigured token must not turn the gate into an open door for a caller that
// sends no bearer at all.
func TestBearerTokenGate_EmptyWantRefusesEverything(t *testing.T) {
	gate := BearerTokenGate("", "traefik", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("next must not run without a configured token")
	}))
	req := httptest.NewRequest(http.MethodGet, "/traefik/config", http.NoBody)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
