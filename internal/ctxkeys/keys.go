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

// RequestBodyKey is the context key under which the streaming-aware
// timeout middleware stores the already-read request body bytes.
// Downstream handlers (proxy.ChatCompletions) can read from this
// instead of re-reading r.Body, avoiding a full second allocation.
const RequestBodyKey contextKey = "request_body"

// SettingsReadMsKey is the context key under which the rate limiter
// middleware stores the time spent reading settings (float64, in ms).
// The proxy handler reads this for observability logging.
const SettingsReadMsKey contextKey = "settings_read_ms"

// SafeDialMsKey is the context key under which the proxy handler stores a
// *float64 pointer for capturing per-request DNS resolution timing.
// The SafeDialer's DialContext writes dial duration into this pointer
// so the handler can read it after the upstream request completes,
// avoiding cross-request race conditions from a shared atomic.
const SafeDialMsKey contextKey = "safe_dial_ms"

// VirtualKeyRateLimitRPSKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the per-key RPS override (float64 pointer,
// nil when unset). The ratelimit middleware reads this to apply
// per-key rate limits that take precedence over global settings.
const VirtualKeyRateLimitRPSKey contextKey = "virtual_key_rate_limit_rps"

// VirtualKeyRateLimitBurstKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the per-key burst override (int pointer,
// nil when unset). The ratelimit middleware reads this alongside
// VirtualKeyRateLimitRPSKey.
const VirtualKeyRateLimitBurstKey contextKey = "virtual_key_rate_limit_burst"
