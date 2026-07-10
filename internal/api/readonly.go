package api

import (
	"net/http"
	"strings"
)

// readOnlyGuard rejects state-changing requests when the instance runs in
// read-only mode (DEMO_READONLY=true). Safe methods (GET/HEAD/OPTIONS) pass
// through so the dashboard stays fully browsable; every mutating method —
// create, update, delete — gets a 403.
//
// It is mounted only on the admin CRUD router (Handler.Register), so the admin
// chat (/api/chat) and the public proxy (/v1) are deliberately unaffected: a
// demo visitor can still chat against the seeded providers and use a seeded
// virtual key, they just cannot add, edit, or delete anything.
//
// One exception: acknowledging background-discovery notifications. That POST
// only flips a per-row "seen" flag on the discovery_changes table — it does not
// touch the model catalog — so it is allowed even in read-only mode. Without
// this, a demo instance can show the Models nav badge but never clear it, and it
// reappears on every poll/reload.
func readOnlyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
		case http.MethodPost:
			if isReadOnlyExemptPost(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			fallthrough
		default:
			// nil err + a 4xx code: respondError does not log this (it is a
			// client-facing policy rejection, not a server fault).
			respondError(w, "this is a read-only demo: creating, editing, and deleting are disabled", nil, http.StatusForbidden)
		}
	})
}

// isReadOnlyExemptPost reports whether a POST path stays allowed in read-only
// mode. These mutate no catalog or credential data. Matched by suffix so they
// are independent of the router's mount prefix:
//   - discovery-change acknowledgement (flips a per-row "seen" flag), and
//   - WebAuthn logout (revokes the current session only; it is not admin
//     credential management like registering or deleting a passkey).
func isReadOnlyExemptPost(path string) bool {
	return strings.HasSuffix(path, "/discovery/changes/ack") ||
		strings.HasSuffix(path, "/webauthn/logout")
}
