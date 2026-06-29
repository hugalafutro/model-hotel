package frontdesk

import (
	"context"

	"github.com/hugalafutro/model-hotel/internal/adminauth"
)

// oidcSettings adapts Front Desk's typed, SQLite-backed Settings row to the
// key/value adminauth.OIDCSettings interface the shared OIDC handler expects.
// The main gateway passes its Postgres settings.Repository directly; Front Desk
// has no such key/value store, so this maps the handler's key lookups onto the
// single Settings row. Reads go through Store.GetSettings (one row read per
// lookup, on the rare login path), and a read error degrades to the caller's
// default so a transient DB hiccup cannot panic the login flow.
//
// OIDCClientSecretKey returns the stored *encrypted* value: the handler decrypts
// it with the master key, exactly as it does for the main gateway.
type oidcSettings struct {
	store *Store
}

func newOIDCSettings(store *Store) *oidcSettings { return &oidcSettings{store: store} }

// GetBool implements adminauth.OIDCSettings. Only OIDCEnabledKey is a bool.
func (a *oidcSettings) GetBool(ctx context.Context, key string, def bool) bool {
	set, err := a.store.GetSettings(ctx)
	if err != nil {
		return def
	}
	switch key {
	case adminauth.OIDCEnabledKey:
		return set.OidcEnabled
	default:
		return def
	}
}

// GetWithDefault implements adminauth.OIDCSettings for the string-valued keys.
func (a *oidcSettings) GetWithDefault(ctx context.Context, key, def string) string {
	set, err := a.store.GetSettings(ctx)
	if err != nil {
		return def
	}
	switch key {
	case adminauth.OIDCIssuerURLKey:
		return set.OidcIssuerURL
	case adminauth.OIDCClientIDKey:
		return set.OidcClientID
	case adminauth.OIDCClientSecretKey:
		return set.OidcClientSecret
	case adminauth.OIDCPublicBaseURLKey:
		return set.OidcPublicBaseURL
	case adminauth.OIDCAllowedEmailsKey:
		return set.OidcAllowedEmails
	default:
		return def
	}
}
