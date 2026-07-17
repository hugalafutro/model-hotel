package adminauth

import (
	"context"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// SSOUserResolver binds an OIDC/GitHub login to a user account by verified
// email, enforcing one external identity per account so a second provider
// cannot take over an account by asserting its email. The bool it returns is
// true only when this call recorded a first-ever binding. Implemented by
// *user.Repository.
type SSOUserResolver interface {
	ResolveSSOIdentity(ctx context.Context, provider, subject, email string) (*user.User, bool, error)
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
	u, bound, err := users.ResolveSSOIdentity(ctx, provider, subject, email)
	if err != nil || u == nil || !u.Enabled {
		return nil
	}
	if bound {
		// First-ever SSO login for this account. A legitimate onboarding produces
		// exactly one of these; an *unexpected* one is the fingerprint of the
		// cross-provider takeover this binding defends against, so make it loud in
		// the server log and on the events bus (dashboard stream + opt-in Apprise
		// alert). The subject is deliberately omitted; it is an opaque provider id
		// and the (provider, user) pair is enough to investigate.
		debuglog.Warn("sso: account bound to a provider identity for the first time",
			"provider", provider, "user_id", u.ID.String(), "email_masked", maskEmail(email))
		events.Publish(events.Event{
			Type:     "auth.sso_identity_bound",
			Severity: "warning",
			Source:   provider,
			Message:  "A user account was linked to an SSO identity for the first time",
			Metadata: map[string]any{
				"provider": provider,
				"user_id":  u.ID.String(),
				"username": u.Username,
			},
		})
	}
	return u
}
