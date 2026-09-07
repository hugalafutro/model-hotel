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
			gate := BearerTokenGate("secret-token", "metrics", "auth: metrics scrape", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	gate := BearerTokenGate("", "traefik", "frontdesk: traefik config poll", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("next must not run without a configured token")
	}))
	req := httptest.NewRequest(http.MethodGet, "/traefik/config", http.NoBody)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// TestBearerTokenGateLogLines pins the rendered rejection lines for the three
// subjects the binaries pass. The CrowdSec collection shipped in
// contrib/crowdsec matches these by literal prefix, so the wording is a
// published contract, not an implementation detail: reword one and the
// model-hotel-admin-bf brute-force scenario silently stops seeing token
// attacks. Each binary keeps its own scope prefix.
func TestBearerTokenGateLogLines(t *testing.T) {
	tests := []struct {
		name        string
		what        string
		logSubject  string
		wantMissing string
		wantInvalid string
	}{
		{
			name:        "gateway metrics",
			what:        "metrics",
			logSubject:  "auth: metrics scrape",
			wantMissing: "auth: metrics scrape missing bearer token",
			wantInvalid: "auth: metrics scrape with invalid token",
		},
		{
			name:        "front desk metrics",
			what:        "metrics",
			logSubject:  "frontdesk: metrics scrape",
			wantMissing: "frontdesk: metrics scrape missing bearer token",
			wantInvalid: "frontdesk: metrics scrape with invalid token",
		},
		{
			name:        "front desk traefik poll",
			what:        "traefik",
			logSubject:  "frontdesk: traefik config poll",
			wantMissing: "frontdesk: traefik config poll missing bearer token",
			wantInvalid: "frontdesk: traefik config poll with invalid token",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, c := range []struct {
				header string
				want   string
			}{
				{"", tc.wantMissing},
				{"Bearer wrong", tc.wantInvalid},
			} {
				lines := captureLogLines(t)
				gate := BearerTokenGate("right", tc.what, tc.logSubject, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Error("next must not run for a refused bearer")
				}))
				req := httptest.NewRequest(http.MethodGet, "/gated", http.NoBody)
				if c.header != "" {
					req.Header.Set("Authorization", c.header)
				}
				gate.ServeHTTP(httptest.NewRecorder(), req)

				if _, ok := findLine(lines(), c.want); !ok {
					t.Errorf("no line with msg=%q, got %v", c.want, lines())
				}
			}
		})
	}
}
