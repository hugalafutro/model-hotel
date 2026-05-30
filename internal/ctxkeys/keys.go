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

import (
	"context"
	"time"
)

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
// middleware stores a *float64 pointer for accumulating settings read
// time across the entire request pipeline (in ms). The ratelimiter
// creates the pointer; downstream code (resolve, proxy) adds to it.
// Use AddSettingsReadMs to safely add to the accumulated total.
const SettingsReadMsKey contextKey = "settings_read_ms"

// DialMsKey is the context key under which the proxy handler stores a
// *float64 pointer for capturing per-request upstream dial timing
// (DNS resolution + TCP connect). The SafeDialer's DialContext writes
// the total dial duration into this pointer so the handler can read it
// after the upstream request completes, avoiding cross-request race
// conditions from a shared atomic.
const DialMsKey contextKey = "dial_ms"

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

// VirtualKeyAllowedProvidersKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the per-key allowed provider list (*[]string,
// nil when all providers are allowed). The proxy handler reads this to
// filter resolved candidates so restricted keys can only reach specific
// providers.
const VirtualKeyAllowedProvidersKey contextKey = "virtual_key_allowed_providers"

// CancelOriginKey is the context key under which the proxy handler stores a
// string describing why a derived context (failover, retry) was created.
// When a context cancellation error is caught, this value identifies whether
// the cancellation came from the client disconnecting, the failover timeout
// expiring, or the retry timeout expiring. This makes "context canceled" errors
// actionable instead of opaque.
//
// Values: "client_disconnect", "failover_timeout", "retry_timeout"
const CancelOriginKey contextKey = "cancel_origin"

// RequestBodyParseMsKey is the context key under which the
// streamingAwareTimeout middleware stores the time spent reading and
// parsing the request body (float64, in ms). This covers both the
// io.ReadAll of the body and the json.Unmarshal to extract model and
// stream fields. The proxy handler reads this for accurate overhead
// timing instead of measuring only its own re-unmarshal of cached bytes.
const RequestBodyParseMsKey contextKey = "request_body_parse_ms"

// RequestModelKey is the context key under which the
// streamingAwareTimeout middleware stores the model name extracted
// from the request body (string). This avoids a redundant
// json.Unmarshal in ChatCompletions when the body bytes are already
// cached via RequestBodyKey.
const RequestModelKey contextKey = "request_model"

// IsStreamingKey is the context key under which the
// streamingAwareTimeout middleware stores the stream flag (bool)
// extracted from the request body. This avoids a redundant
// json.Unmarshal in ChatCompletions when the body bytes are already
// cached via RequestBodyKey.
const IsStreamingKey contextKey = "is_streaming"

// AddSettingsReadMs adds duration (in ms) to the accumulated settings
// read time stored under SettingsReadMsKey. Safe to call when the
// pointer is nil (no-op). Each call site that reads settings should
// call this to ensure all settings reads are captured in overhead.
func AddSettingsReadMs(ctx context.Context, start time.Time) {
	if v := ctx.Value(SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			*p += float64(time.Since(start).Microseconds()) / 1000.0
		}
	}
}
