// Package ctxkeys defines shared context key constants used across multiple
// packages (e.g. proxy and ratelimit). Centralising them here avoids import
// cycles — packages that need to read or write the same context value can
// both depend on ctxkeys without depending on each other.
//
// Go's context.Value requires an exact type match on the key, so we use a
// dedicated unexported type (contextKey) with exported constants of that
// type. This prevents accidental collisions with context keys defined
// elsewhere.
package ctxkeys

type contextKey string

// VirtualKeyHashKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the SHA-256 hash of the virtual key used
// for the current request. The ratelimit middleware reads this same
// key to enforce per-key throttling.
const VirtualKeyHashKey contextKey = "virtual_key_hash"
