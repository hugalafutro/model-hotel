package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// ChatUserContextMiddleware publishes the calling account's per-user limits to
// the admin chat routes: its provider cap (users.allowed_providers), its
// aggregate rate limits (RPS/burst/TPM) and its own id, under the ctxkeys the
// proxy and the rate limiters already read on every request, whichever surface
// the request came in on.
//
// The public /v1 proxy gets all of this from ProxyKeyMiddleware
// (internal/proxy/handler.go), which resolves a virtual key and, when the key is
// owned, reads its owner's row. /api/chat/* has no key: it authenticates a
// dashboard session, so the caller IS the account whose limits apply and there
// is no key side to combine with. The per-KEY ctxkeys
// (VirtualKeyAllowedProvidersKey, VirtualKeyRateLimit*Key) are therefore
// deliberately left unset, and each consumer already handles their absence:
// effectiveAllowedProviders reads a nil key-side cap as "this side restricts
// nothing" and returns the account cap unchanged, while the two rate limiters
// handle it differently and neither is left guessing. ratelimit.Limiter keeps
// its per-key stage on this surface: extractKey falls back to the resolved client address and
// the bucket is sized from the global settings defaults, which is exactly what
// an unkeyed request here already got before this middleware existed.
// ratelimit.TPMLimiter has NO per-key stage here at all — RegisterAdminChat
// mounts UserMiddleware rather than Middleware precisely because that same
// address-keyed fallback bucket would be admitted against and never debited
// (Debit is driven by the virtual-key hash), i.e. a cap that looks enforced and
// is not.
//
// Without this the `chat` grant was an escape hatch around all of them: it is an
// ordinary assignable non-admin grant (internal/user/grants.go), so a user could
// open the dashboard Chat page and reach a provider outside their cap, or burn
// request and token budget that their /v1 traffic is metered against, while the
// Users page still displayed the limits.
//
// What each published key buys, verified against the wiring at this commit:
//
//   - UserAllowedProvidersKey — read by resolveCandidates
//     (internal/proxy/proxy_request.go) via effectiveAllowedProviders.
//   - VirtualKeyOwnerIDKey — the shared "user:<uuid>" bucket identity for both
//     rate limiters, and the owner stamped by newPendingRequestLog on the
//     request lifecycle SSE events (which is what scopes a non-admin's live log
//     feed, eventOwnedBy in events.go) and, because this surface has no key to
//     resolve an owner through, on request_logs.owner_user_id itself, which is
//     what puts chat traffic in the caller's own REST logs and stats
//     (migration 067).
//   - UserRateLimitRPSKey / UserRateLimitBurstKey — read by
//     ratelimit.Limiter.Middleware, which RegisterAdminChat already mounts.
//   - UserRateLimitTPMKey — read by ratelimit.TPMLimiter.UserMiddleware, mounted
//     on this group by RegisterAdminChat.
//
// It lives in internal/api because reading the users row needs the user store,
// which internal/proxy deliberately does not depend on. Mount it after
// AuthMiddleware (which resolves the identity this reads) and before
// RegisterAdminChat.
func (h *Handler) ChatUserContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := user.IdentityFrom(r.Context())
		if id == nil || id.UserID == nil {
			// The env admin token and pre-multi-user admin sessions have no
			// users row and therefore no limits. Leaving the keys unset is the
			// same "no restriction" every consumer already handles, not an
			// oversight.
			next.ServeHTTP(w, r)
			return
		}
		if h.userRepo == nil {
			// Not reachable through AuthMiddleware: resolveIdentity refuses to
			// mint a UserID identity without the store. Kept because the
			// alternative under a future mis-wiring is a chat surface that
			// silently ignores every limit.
			debuglog.Error("auth: chat user context unreadable, no user store wired", "username", id.Username)
			http.Error(w, "account limits could not be determined", http.StatusInternalServerError)
			return
		}
		u, err := h.userRepo.Get(r.Context(), *id.UserID)
		switch {
		case errors.Is(err, user.ErrNotFound), err == nil && u == nil:
			// The row went away between authentication and here. There is no
			// foreign key to lean on the way virtual_keys.owner_user_id has one
			// (ON DELETE SET NULL, migration 051), and no row means no limits to
			// read, so continuing would serve the request unrestricted. Deny,
			// which is what resolveIdentity does to the same missing row one
			// layer up. The rowless-but-errorless shape is guarded too: the real
			// repository returns ErrNotFound, but an authorization input must
			// not depend on which of the two it gets.
			debuglog.Warn("auth: chat request from a user row that no longer exists", "username", id.Username)
			http.Error(w, "account no longer exists", http.StatusForbidden)
			return
		case err != nil:
			debuglog.Error("auth: failed to read the chat caller's account limits", "username", id.Username, "error", err)
			http.Error(w, "account limits could not be determined", http.StatusInternalServerError)
			return
		}
		// The cap is stored even when nil: a nil *[]string under this key is
		// exactly what effectiveAllowedProviders reads as "no cap". A non-nil
		// list restricts to exactly its members INCLUDING when empty, so an
		// account whose cap was pruned to {} (its last provider deleted, see
		// provider.PruneAllowLists) reaches nothing at all.
		ctx := context.WithValue(r.Context(), ctxkeys.UserAllowedProvidersKey, u.AllowedProviders)
		// The owner id is the bucket identity both limiters key on, and it is
		// published unconditionally: each limiter gates on its own cap being
		// non-nil, and the log/SSE attribution wants the owner regardless of
		// whether any limit is set.
		ctx = context.WithValue(ctx, ctxkeys.VirtualKeyOwnerIDKey, u.ID.String())
		ctx = context.WithValue(ctx, ctxkeys.UserRateLimitRPSKey, u.RateLimitRPS)
		ctx = context.WithValue(ctx, ctxkeys.UserRateLimitBurstKey, u.RateLimitBurst)
		ctx = context.WithValue(ctx, ctxkeys.UserRateLimitTPMKey, u.RateLimitTPM)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
