package frontdesk

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGatedTokenLogLines pins the wording of the four refusal lines Front
// Desk's two dedicated-bearer gates emit. The parser shipped in
// contrib/crowdsec/parsers/s01-parse/model-hotel-logs.yaml matches them by
// literal prefix to feed the model-hotel-admin-bf brute-force scenario, and
// Front Desk scopes them with its own "frontdesk: " prefix so a reader can tell
// the two binaries apart. Reword one and the scenario silently stops seeing
// token attacks against this binary.
func TestGatedTokenLogLines(t *testing.T) {
	refuseNext := func(t *testing.T) http.HandlerFunc {
		return func(http.ResponseWriter, *http.Request) {
			t.Error("next must not run for a refused bearer")
		}
	}

	tests := []struct {
		name        string
		gate        func(t *testing.T) http.Handler
		target      string
		wantMissing string
		wantInvalid string
	}{
		{
			name: "metrics scrape",
			gate: func(t *testing.T) http.Handler {
				s := &Server{metricsToken: "scrape-secret"}
				return s.metricsAuth(refuseNext(t))
			},
			target:      "/metrics",
			wantMissing: "frontdesk: metrics scrape missing bearer token",
			wantInvalid: "frontdesk: metrics scrape with invalid token",
		},
		{
			name: "traefik config poll",
			gate: func(t *testing.T) http.Handler {
				s := &Server{traefikToken: "poll-secret"}
				return s.traefikAuth(refuseNext(t))
			},
			target:      "/traefik/config",
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
				lines := captureAccessLines(t)
				req := httptest.NewRequest(http.MethodGet, tc.target, http.NoBody)
				if c.header != "" {
					req.Header.Set("Authorization", c.header)
				}
				tc.gate(t).ServeHTTP(httptest.NewRecorder(), req)

				found := false
				for _, l := range lines() {
					if strings.Contains(l, `msg="`+c.want+`"`) {
						found = true
					}
				}
				if !found {
					t.Errorf("no line with msg=%q, got %v", c.want, lines())
				}
			}
		})
	}
}
