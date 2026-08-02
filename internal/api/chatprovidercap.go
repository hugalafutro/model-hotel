package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// ChatProviderCapMiddleware publishes the calling account's provider cap
// (users.allowed_providers) under ctxkeys.UserAllowedProvidersKey so the admin
// chat routes are filtered by it. The proxy's candidate filter
// (resolveCandidates in internal/proxy/proxy_request.go) reads that key on every
// request, whichever surface the request came in on.
//
// The public /v1 proxy gets both halves of the pair from ProxyKeyMiddleware
// (internal/proxy/handler.go), which resolves a virtual key and, when the key is
// owned, its owner's cap. /api/chat/* has neither: it authenticates a dashboard
// session, so the caller IS the account whose cap applies and there is no
// key-side list to intersect with. VirtualKeyAllowedProvidersKey is therefore
// deliberately left unset; effectiveAllowedProviders reads a nil key side as
// "this side restricts nothing" and returns the owner cap unchanged.
//
// Without this the `chat` grant was an escape hatch around the cap: it is an
// ordinary assignable non-admin grant (internal/user/grants.go), so a user
// capped to one provider could open the dashboard Chat page and reach any other
// provider, while the Users page still displayed the cap.
//
// It lives in internal/api because reading the cap needs the user store, which
// internal/proxy deliberately does not depend on. Mount it after AuthMiddleware
// (which resolves the identity this reads) and before RegisterAdminChat.
func (h *Handler) ChatProviderCapMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := user.IdentityFrom(r.Context())
		if id == nil || id.UserID == nil {
			// The env admin token and pre-multi-user admin sessions have no
			// users row and therefore no cap. Leaving the key unset is the
			// same "no restriction" the proxy already handles, not an
			// oversight.
			next.ServeHTTP(w, r)
			return
		}
		if h.userRepo == nil {
			// Not reachable through AuthMiddleware: resolveIdentity refuses to
			// mint a UserID identity without the store. Kept because the
			// alternative under a future mis-wiring is a chat surface that
			// silently ignores every cap.
			debuglog.Error("auth: chat provider cap unreadable, no user store wired", "username", id.Username)
			http.Error(w, "provider access could not be determined", http.StatusInternalServerError)
			return
		}
		u, err := h.userRepo.Get(r.Context(), *id.UserID)
		switch {
		case errors.Is(err, user.ErrNotFound), err == nil && u == nil:
			// The row went away between authentication and here. There is no
			// foreign key to lean on the way virtual_keys.owner_user_id has one
			// (ON DELETE SET NULL, migration 051), and no row means no cap to
			// read, so continuing would serve the request uncapped. Deny, which
			// is what resolveIdentity does to the same missing row one layer up.
			// The rowless-but-errorless shape is guarded too: the real
			// repository returns ErrNotFound, but an authorization input must
			// not depend on which of the two it gets.
			debuglog.Warn("auth: chat request from a user row that no longer exists", "username", id.Username)
			http.Error(w, "account no longer exists", http.StatusForbidden)
			return
		case err != nil:
			debuglog.Error("auth: failed to read the chat caller's provider cap", "username", id.Username, "error", err)
			http.Error(w, "provider access could not be determined", http.StatusInternalServerError)
			return
		}
		// Stored even when the cap is nil: a nil *[]string under this key is
		// exactly what effectiveAllowedProviders reads as "no cap". A non-nil
		// list restricts to exactly its members INCLUDING when empty, so an
		// account whose cap was pruned to {} (its last provider deleted, see
		// provider.PruneAllowLists) reaches nothing at all.
		ctx := context.WithValue(r.Context(), ctxkeys.UserAllowedProvidersKey, u.AllowedProviders)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
