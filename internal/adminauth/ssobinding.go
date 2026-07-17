package adminauth

import (
	"context"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// SSOUserResolver binds an OIDC/GitHub login to a user account by verified
// email, enforcing one external identity per account so a second provider
// cannot take over an account by asserting its email. Implemented by
// *user.Repository.
type SSOUserResolver interface {
	ResolveSSOIdentity(ctx context.Context, provider, subject, email string) (*user.User, error)
}

// resolveSSOUser returns the enabled user bound to (provider, subject, email),
// or nil when there is no valid binding: a nil resolver, no matching row, a
// disabled account, or -- crucially -- an account already bound to a different
// provider identity (ErrSSOMismatch). Precedence is decided by the callers: the
// admin allowlist is checked first and keeps its historical meaning, so an
// operator whose email is allowlisted can never be silently downgraded to a
// user identity by a stray user row.
func resolveSSOUser(ctx context.Context, users SSOUserResolver, provider, subject, email string) *user.User {
	if users == nil {
		return nil
	}
	u, err := users.ResolveSSOIdentity(ctx, provider, subject, email)
	if err != nil || u == nil || !u.Enabled {
		return nil
	}
	return u
}
