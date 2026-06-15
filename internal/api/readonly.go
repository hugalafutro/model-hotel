package api

import "net/http"

// readOnlyGuard rejects state-changing requests when the instance runs in
// read-only mode (DEMO_READONLY=true). Safe methods (GET/HEAD/OPTIONS) pass
// through so the dashboard stays fully browsable; every mutating method —
// create, update, delete — gets a 403.
//
// It is mounted only on the admin CRUD router (Handler.Register), so the admin
// chat (/api/chat) and the public proxy (/v1) are deliberately unaffected: a
// demo visitor can still chat against the seeded providers and use a seeded
// virtual key, they just cannot add, edit, or delete anything.
func readOnlyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
		default:
			// nil err + a 4xx code: respondError does not log this (it is a
			// client-facing policy rejection, not a server fault).
			respondError(w, "this is a read-only demo — creating, editing, and deleting are disabled", nil, http.StatusForbidden)
		}
	})
}
