package user

import (
	"context"

	"github.com/google/uuid"
)

// Identity is the resolved caller of an authenticated dashboard request,
// stashed in the request context by the auth middleware and consumed by the
// requireAdmin/requireGrant route guards.
type Identity struct {
	// Role is admin for the env admin token, legacy admin sessions, and
	// admin-role user rows; user otherwise.
	Role Role
	// Grants is the user's feature grants; empty for admins (they bypass).
	Grants []string
	// UserID is set only when the identity maps to a users row.
	UserID *uuid.UUID
	// Username is "admin" for the break-glass/legacy identity.
	Username string
}

// AdminIdentity is the identity for the env admin token and legacy admin
// sessions (passkey/TOTP/SSO logins minted before multi-user existed).
func AdminIdentity() *Identity {
	return &Identity{Role: RoleAdmin, Username: "admin"}
}

// IsAdmin reports whether this identity bypasses grant checks.
func (id *Identity) IsAdmin() bool {
	return id != nil && id.Role == RoleAdmin
}

// Can reports whether the identity may use the given feature: admins always,
// users only with the grant.
func (id *Identity) Can(g Grant) bool {
	if id == nil {
		return false
	}
	if id.Role == RoleAdmin {
		return true
	}
	return HasGrant(id.Grants, g)
}

type identityCtxKey struct{}

// WithIdentity returns a context carrying the resolved identity.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, identityCtxKey{}, id)
}

// IdentityFrom extracts the identity resolved by the auth middleware; nil when
// the request never passed through it.
func IdentityFrom(ctx context.Context) *Identity {
	id, _ := ctx.Value(identityCtxKey{}).(*Identity)
	return id
}
