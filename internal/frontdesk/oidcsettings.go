package frontdesk

import (
	"context"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/adminauth"
)

// oidcSettingsCacheTTL bounds how long a loaded Settings row is reused across the
// per-key lookups the OIDC handler makes. A single OIDCHandler.runtime() call
// reads all six keys in sequence; without coalescing that is six identical
// `SELECT ... settings WHERE id = 1` queries per login attempt. The TTL is short
// so an operator who just saved new OIDC config sees it almost immediately, while
// the burst of reads within one request collapses to a single row read.
const oidcSettingsCacheTTL = time.Second

// oidcSettings adapts Front Desk's typed, SQLite-backed Settings row to the
// key/value adminauth.OIDCSettings interface the shared OIDC handler expects.
// The main gateway passes its Postgres settings.Repository directly; Front Desk
// has no such key/value store, so this maps the handler's key lookups onto the
// single Settings row. A read error degrades to the caller's default so a
// transient DB hiccup cannot panic the login flow.
//
// OIDCClientSecretKey returns the stored *encrypted* value: the handler decrypts
// it with the master key, exactly as it does for the main gateway.
type oidcSettings struct {
	store *Store

	mu       sync.Mutex
	cached   Settings
	cachedAt time.Time
	cachedOK bool
}

func newOIDCSettings(store *Store) *oidcSettings { return &oidcSettings{store: store} }

// load returns the Settings row, reusing a recent read within oidcSettingsCacheTTL
// so the handler's six per-key lookups in one runtime() pass coalesce to a single
// query. The DB read happens off-lock (the cache is only held briefly to read or
// store the snapshot), so concurrent logins never serialize on the row read; the
// worst case under contention is a redundant read, never a stale-then-clobber.
func (a *oidcSettings) load(ctx context.Context) (Settings, bool) {
	a.mu.Lock()
	if a.cachedOK && time.Since(a.cachedAt) < oidcSettingsCacheTTL {
		set := a.cached
		a.mu.Unlock()
		return set, true
	}
	a.mu.Unlock()

	set, err := a.store.GetSettings(ctx)
	if err != nil {
		return Settings{}, false
	}

	a.mu.Lock()
	a.cached = set
	a.cachedAt = time.Now()
	a.cachedOK = true
	a.mu.Unlock()
	return set, true
}

// GetBool implements adminauth.OIDCSettings. Only OIDCEnabledKey is a bool.
func (a *oidcSettings) GetBool(ctx context.Context, key string, def bool) bool {
	set, ok := a.load(ctx)
	if !ok {
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
	set, ok := a.load(ctx)
	if !ok {
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
