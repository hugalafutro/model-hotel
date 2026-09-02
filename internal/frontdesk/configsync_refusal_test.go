package frontdesk

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// A member's refusal reason reaches the operator: the 400 body naming the
// field is carried into the result and the failure event, bounded and
// scrubbed, while a body with no reason in it (an HTML page) leaves the
// status-only message.
func TestConfigSyncCarriesTheMembersRefusalReason(t *testing.T) {
	const reason = `configsync: refusing to apply an invalid provider: provider "mock-a": max_in_flight must be between 1 and 10000, or null for no ceiling, got -5`
	for _, tc := range []struct {
		name, body, want string
		code             int
	}{
		{"plain-text refusal", reason + "\n", "this member rejected the request (HTTP 400): " + reason, http.StatusBadRequest},
		{"coded JSON refusal", `{"code":"primary_required","error":"a primary must be designated first"}`, "this member rejected the request (HTTP 400): a primary must be designated first", http.StatusBadRequest},
		{"HTML page carries no reason", "<!doctype html><html><body>404</body></html>", "this member rejected the request (HTTP 404)", http.StatusNotFound},
		{"credential in the body is masked", "invalid base_url for key sk-proj-0123456789abcdef0123456789abcdef\n", "this member rejected the request (HTTP 400): invalid base_url for key [redacted]", http.StatusBadRequest},
		{"control characters stripped, first line only", "bad\x1b[31m field\nsecond line", "this member rejected the request (HTTP 400): bad[31m field", http.StatusBadRequest},
		{"unicode line separator and format characters", "ok\u202eevil\u200bmore\u2028second line", "this member rejected the request (HTTP 400): okevilmore", http.StatusBadRequest},
		{"userinfo in a quoted URL is redacted", `configsync: refusing to apply a setting with an invalid URL "oidc_issuer_url": invalid URL: parse "https://admin:sup3rs3cr3t@example.com/%zz": invalid URL escape "%zz"`, `this member rejected the request (HTTP 400): configsync: refusing to apply a setting with an invalid URL "oidc_issuer_url": invalid URL: parse "https://example.com/%zz": invalid URL escape "%zz"`, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, store := newTestServer(t)
			primary := newStubConfigMember(t, "ptoken")
			replica := newStubConfigMember(t, "rtoken")
			replica.importCode = tc.code
			replica.importBody = tc.body
			pm, _ := store.CreateMember(t.Context(), "primary", primary.srv.URL, "ptoken")
			enableAutoSync(t, store, pm.ID)
			rm, _ := store.CreateMember(t.Context(), "replica", replica.srv.URL, "rtoken")
			alignFleetVersions(t, srv, store, "dev")

			rec := do(t, srv, http.MethodPost, "/api/config/sync", `{"primary_id":"`+pm.ID+`"}`, true)
			if rec.Code != http.StatusOK {
				t.Fatalf("sync = %d", rec.Code)
			}
			var resp struct {
				Results []syncResultItem `json:"results"`
			}
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if len(resp.Results) != 1 || resp.Results[0].OK {
				t.Fatalf("expected a failure result, got %+v", resp.Results)
			}
			if got := resp.Results[0].Error; got != tc.want {
				t.Fatalf("error = %q\nwant  %q", got, tc.want)
			}
			evs, _, _ := store.ListEvents(t.Context(), EventFilter{})
			saw := false
			for _, e := range evs {
				if e.Type == "config.sync_failed" && e.MemberID == rm.ID {
					saw = true
					if !strings.Contains(e.Message, tc.want) {
						t.Fatalf("event message %q does not carry the reason %q", e.Message, tc.want)
					}
				}
			}
			if !saw {
				t.Fatal("no sync_failed event for the replica")
			}
		})
	}
}

// The reason is bounded in runes, not bytes: a page-sized body yields 240
// runes and an ellipsis whatever script it is written in.
func TestRefusalReasonIsBounded(t *testing.T) {
	for _, long := range []string{strings.Repeat("x", 5000), strings.Repeat("é", 400), strings.Repeat("配置", 300)} {
		got := refusalReason([]byte(long))
		runes := []rune(got)
		if len(runes) != 241 || runes[240] != '…' {
			t.Fatalf("reason is %d runes ending %q, want 240 plus the ellipsis", len(runes), string(runes[len(runes)-1]))
		}
	}
	if got := refusalReason([]byte(strings.Repeat("y", 240))); len([]rune(got)) != 240 || strings.HasSuffix(got, "…") {
		t.Fatalf("a reason exactly at the bound was cut: %q", got)
	}
	if refusalReason(nil) != "" || refusalReason([]byte("   ")) != "" || refusalReason([]byte(`{"code":"x"}`)) != "" {
		t.Fatal("an empty or reason-less body must yield no reason")
	}
}
