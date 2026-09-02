// Package ctxkeys defines shared context key constants used across multiple
// packages (e.g. proxy and ratelimit). Centralising them here avoids import
// cycles: packages that read or write the same context value both depend on
// ctxkeys rather than on each other.
//
// context.Value requires an exact type match on the key, so the keys are
// exported constants of an unexported type (contextKey), which prevents
// collisions with context keys defined elsewhere.
package ctxkeys

import (
	"context"
	"time"
)

type contextKey string

// VirtualKeyHashKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the SHA-256 hash of the virtual key used for the
// current request. The ratelimit middleware reads it to enforce per-key
// throttling.
const VirtualKeyHashKey contextKey = "virtual_key_hash"

// VirtualKeyStripReasoningKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the per-key strip_reasoning flag (bool). When true,
// reasoning and reasoning_content fields are stripped from streaming output.
const VirtualKeyStripReasoningKey contextKey = "virtual_key_strip_reasoning"

// RequestBodyKey is the context key under which the streaming-aware timeout
// middleware stores the already-read request body bytes. Downstream handlers
// (proxy.ChatCompletions) read it instead of re-reading r.Body, avoiding a
// second allocation.
const RequestBodyKey contextKey = "request_body"

// SettingsReadMsKey is the context key under which the rate limiter middleware
// stores a *float64 accumulating settings read time across the request pipeline
// (in ms). The ratelimiter creates the pointer; downstream code (resolve, proxy)
// adds to it via AddSettingsReadMs.
const SettingsReadMsKey contextKey = "settings_read_ms"

// DialMsKey is the context key under which the proxy handler stores its
// per-request dial-timing slot (an atomic; the proxy package owns the type) for
// upstream dial time (DNS resolution plus TCP connect). The SafeDialer's
// DialContext stores the total dial duration into it and the handler swaps it
// out after the upstream request completes. Atomic because the transport's dial
// goroutine can outlive the request that started it.
const DialMsKey contextKey = "dial_ms"

// VirtualKeyRateLimitRPSKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the per-key RPS override (float64 pointer, nil when
// unset). The ratelimit middleware reads it to apply per-key rate limits, which
// take precedence over global settings.
const VirtualKeyRateLimitRPSKey contextKey = "virtual_key_rate_limit_rps"

// VirtualKeyRateLimitBurstKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the per-key burst override (int pointer, nil when
// unset). The ratelimit middleware reads it alongside VirtualKeyRateLimitRPSKey.
const VirtualKeyRateLimitBurstKey contextKey = "virtual_key_rate_limit_burst"

// VirtualKeyRateLimitTPMKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the per-key tokens-per-minute cap (int pointer, nil
// when unset). The TPM admission middleware reads it to enforce the per-key
// minute token budget; nil falls back to the global default.
const VirtualKeyRateLimitTPMKey contextKey = "virtual_key_rate_limit_tpm"

// VirtualKeyAllowedProvidersKey is the context key under which the proxy's
// ProxyKeyMiddleware stores the per-key allowed provider list (*[]string, nil
// when all providers are allowed). The proxy handler reads it to filter resolved
// candidates, so a restricted key only reaches its own providers.
const VirtualKeyAllowedProvidersKey contextKey = "virtual_key_allowed_providers"

// VirtualKeyOwnerIDKey is the context key under which the account behind the
// request is published as a UUID string (absent when there is none). The
// rate-limit middlewares derive the shared "user:<uuid>" bucket key from it so
// one user's traffic aggregates across every surface and key they use, and the
// proxy stamps it on request lifecycle SSE events so a non-admin's live log feed
// can be scoped to their own activity. On surfaces with no virtual key it is
// also persisted to request_logs.owner_user_id, the only owner a keyless row can
// have.
//
// Written by the same two middlewares as UserAllowedProvidersKey below: the
// proxy's ProxyKeyMiddleware (from the virtual key's OWNER, absent when the key
// is unowned) and internal/api's ChatUserContextMiddleware (from the CALLER's
// own row).
const VirtualKeyOwnerIDKey contextKey = "virtual_key_owner_id"

// UserRateLimitRPSKey is the context key under which the account's aggregate
// RPS cap is published (*float64, nil when unset). Unlike the per-key override
// there is no global-settings fallback: a nil cap simply skips the user-level
// stage. Same two writers as VirtualKeyOwnerIDKey.
const UserRateLimitRPSKey contextKey = "user_rate_limit_rps"

// UserRateLimitBurstKey is the context key under which the account's burst cap
// is published (*int, nil when unset; global burst default applies when only
// RPS is set). Same two writers as VirtualKeyOwnerIDKey.
const UserRateLimitBurstKey contextKey = "user_rate_limit_burst"

// UserRateLimitTPMKey is the context key under which the account's aggregate
// tokens-per-minute cap is published (*int, nil when unset). No global
// fallback, same as the RPS cap. Same two writers as VirtualKeyOwnerIDKey.
const UserRateLimitTPMKey contextKey = "user_rate_limit_tpm"

// UserAllowedProvidersKey is the context key under which an account's provider
// cap is published (*[]string, nil when there is no cap). Intersected with the
// key's own list at candidate resolution. Two middlewares write it, for the two
// surfaces that can reach a provider:
//
//   - the proxy's ProxyKeyMiddleware, from the virtual key's OWNER (absent when
//     the key is unowned);
//   - internal/api's ChatUserContextMiddleware on /api/chat/*, from the CALLER's
//     own users row, since that surface has a session rather than a key.
const UserAllowedProvidersKey contextKey = "user_allowed_providers"

// CancelOriginKey is the context key under which the proxy handler stores a
// string describing why a derived context (failover, retry) was created. When a
// context cancellation error is caught, this value says whether the cancellation
// came from the client disconnecting, the failover timeout expiring, or the
// retry timeout expiring, so "context canceled" errors are actionable.
//
// Values: "client_disconnect", "failover_timeout", "retry_timeout"
const CancelOriginKey contextKey = "cancel_origin"

// HedgeSupersededKey is the context key under which the hedging orchestrator
// stores a *atomic.Bool for each in-flight attempt. It is set to true just
// before the orchestrator cancels that attempt because another candidate won
// the race.
//
// Cancelling an attempt produces context.Canceled, which is indistinguishable
// from the client hanging up, so without this flag a hedge loser is reported as
// a client disconnect even though the client is still connected and gets a 200
// from the winner. The origin is read only for context.Canceled; a deadline on
// the same context resolves via CancelOriginKey.
const HedgeSupersededKey contextKey = "hedge_superseded"

// RequestBodyParseMsKey is the context key under which the
// streamingAwareTimeout middleware stores the time spent reading and parsing the
// request body (float64, in ms). It covers both the io.ReadAll of the body and
// the json.Unmarshal that extracts the model and stream fields, so the proxy
// handler's overhead timing does not count only its own re-unmarshal.
const RequestBodyParseMsKey contextKey = "request_body_parse_ms"

// RequestModelKey is the context key under which the streamingAwareTimeout
// middleware stores the model name extracted from the request body (string). It
// saves a redundant json.Unmarshal in ChatCompletions when the body bytes are
// already cached via RequestBodyKey.
const RequestModelKey contextKey = "request_model"

// IsStreamingKey is the context key under which the streamingAwareTimeout
// middleware stores the stream flag (bool) extracted from the request body. It
// saves a redundant json.Unmarshal in ChatCompletions when the body bytes are
// already cached via RequestBodyKey.
const IsStreamingKey contextKey = "is_streaming"

// AddSettingsReadMs adds the elapsed time since start (in ms) to the accumulated
// settings read time stored under SettingsReadMsKey. A nil pointer is a no-op.
// Every call site that reads settings calls this, so all reads land in the
// overhead total.
func AddSettingsReadMs(ctx context.Context, start time.Time) {
	if v := ctx.Value(SettingsReadMsKey); v != nil {
		if p, ok := v.(*float64); ok {
			*p += float64(time.Since(start).Microseconds()) / 1000.0
		}
	}
}
