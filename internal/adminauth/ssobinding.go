package adminauth

import (
	"context"

	"github.com/hugalafutro/model-hotel/internal/user"
)

// SSOUserResolver looks up a user account by verified email so OIDC/GitHub
// logins can bind to a user row instead of the admin identity. Implemented by
// *user.Repository.
type SSOUserResolver interface {
	GetByEmail(ctx context.Context, email string) (*user.User, error)
}

// resolveSSOUser returns the enabled user bound to the verified email, or nil
// when there is no binding (nil resolver, no row, or a disabled account).
// Precedence is decided by the callers: the admin allowlist is checked first
// and keeps its historical meaning, so an operator whose email is allowlisted
// can never be silently downgraded to a user identity by a stray user row.
func resolveSSOUser(ctx context.Context, users SSOUserResolver, email string) *user.User {
	if users == nil {
		return nil
	}
	u, err := users.GetByEmail(ctx, email)
	if err != nil || !u.Enabled {
		return nil
	}
	return u
}
