# 👁️ Privacy

Model Hotel is designed with privacy as a core principle. The system operates as a **dumb pipe** - it routes requests and measures timing, but never inspects, logs, or stores user content.

## What Is Never Captured

> [!IMPORTANT]
> **Prompts and request content are never captured, logged, or inspected.**

The proxy forwards requests to the provider exactly as received, without reading or modifying message contents. Only the `model` and `stream` fields are parsed from JSON request bodies for routing purposes; for multipart uploads (audio transcription/translation, image edits/variations) only the `model` form field is read, and on responses only the `usage` token counts are decoded for metering.

This means:
- **Chat messages** are not stored or logged
- **Images** uploaded via vision API or the image edit/variation endpoints are not inspected
- **System prompts** are not logged
- **Response text** is not retained or buffered
- **Audio uploads** (`/v1/audio/transcriptions`, `/v1/audio/translations`) are forwarded byte-for-byte, never read
- **Generated media** (images, synthesized speech, transcripts) is streamed to the client, never retained
- **Embedding inputs and vectors** are passed through untouched
- **Request body content** is never written to disk

Model Hotel is a **transparent pass-through** - it measures latency and token counts (as reported by providers), but never looks at the actual content flowing through it.

## What Is Logged

The only information recorded is strictly necessary for routing, metering, and diagnostics. All logged data is stored in the `request_logs` PostgreSQL table.

| Data | Column | Purpose |
|------|--------|---------|
| Timestamp | `created_at` | Request timing and analytics |
| Model ID | `model_id` | Usage analytics and cost estimation (e.g. `openai/gpt-4o`, `hotel/gpt-4o`) |
| Provider ID | `provider_id` | Routing analysis and failover tracking (set to `NULL` when a provider is deleted) |
| Virtual key name | `virtual_key_name` | Usage attribution per client |
| Virtual key ID | `virtual_key_id` | Stable key reference (persists even if key is revoked) |
| Token counts | `tokens_prompt`, `tokens_completion` | Usage tracking and billing attribution (provider-reported) |
| Token cache metrics | `tokens_prompt_cache_hit`, `tokens_prompt_cache_miss` | Cache efficiency tracking (provider-reported) |
| Tokens per second | `tokens_per_second` | Performance metric (completion tokens / total duration) |
| Time-to-first-token | `ttft_ms` | Performance monitoring |
| Time-to-response-headers | `response_header_ms` | Performance monitoring (upstream HTTP headers received) |
| Total duration | `duration_ms` | End-to-end latency tracking |
| Provider latency | `latency_ms` | Upstream provider response time (total duration minus proxy overhead) |
| Proxy overhead breakdown | `proxy_overhead_ms`, `parse_ms`, `failover_lookup_ms`, `model_lookup_ms`, `provider_lookup_ms`, `key_decrypt_ms`, `dial_ms`, `settings_read_ms` | Performance optimization and bottleneck identification |
| Cache hit flags | `cache_hits` | Whether each resolution step hit a prewarmed cache (booleans only, no content) |
| Status code | `status_code` | Error tracking and success rate |
| Error message | `error_message` | Provider diagnostic info from failed upstream requests **only** (truncated, see below) |
| Streaming flag | `streaming` | Whether the request used SSE streaming |
| Failover attempt | `failover_attempt` | Which provider candidate was used (0-indexed; for retry analysis) |
| Request state | `state` | Lifecycle status: `pending` → `streaming` → `completed` / `failed` |
| Endpoint family | `endpoint_type` | Which endpoint the request came through: `chat`, `embeddings`, `image`, `tts`, `stt` |
| Request hash | `request_hash` | Random 16-character hex request identifier (see below) |

### About `error_message`

The `error_message` field is populated **only when a request fails** and contains **provider diagnostic information, never user content**. Specifically:

- **Upstream error responses**: When a provider returns a non-200 status code and no failover candidate is available, the raw upstream response body is captured (truncated to 2000 characters for failover responses, 200 characters for SSE events; full error stored in database). This is the provider's error JSON (e.g. `{"error": {"message": "Rate limit exceeded"}}`), not the user's prompt.
- **Connection failures**: Network-level errors (timeouts, DNS failures, connection refused).
- **Client disconnect**: `"client disconnected"` - recorded when a streaming client closes the connection mid-stream.
- **Server restart**: `"request interrupted (server restart)"` - applied to in-flight requests when the server restarts.
- **Stale cleanup**: `"request interrupted (stale)"` - applied to orphaned requests that exceeded the `stale_request_timeout`.

### About `request_hash`

Despite the name, `request_hash` is **not a hash of the request content**. It is a random 16-character hex string generated from 8 random bytes via `crypto/rand` at request creation time:

```go
func generateRequestHash() string {
    b := make([]byte, 8)
    rand.Read(b)
    return hex.EncodeToString(b)
}
```

No part of the user's prompt or request body is used in its generation. The naming is historical - treat it as a **request ID**, not a content fingerprint.

## Dead `prompt` Column

> ⚠️ **The `prompt` column has been removed.**

Migration 006 originally added a `prompt TEXT` column to `request_logs`, but no application code ever wrote to it. Migration 027 (`internal/db/migrations/027_drop_unused_prompt_column.sql`) dropped the column entirely - it no longer exists in the database schema.

This is explicitly documented to avoid confusion when inspecting older migration files. The `prompt` column was abandoned for privacy reasons and has been completely removed.

## App Logs

The `app_logs` table (added in migration 025) records server-side application logs with these fields:

| Column | Purpose |
|--------|---------|
| `level` | Severity (`info`, `warning`, `error`) |
| `source` | Which package/module emitted the log (e.g., `proxy`, `auth`, `discovery`) |
| `message` | Log message text (may contain request paths, error details, provider names) |
| `timestamp` | When the log was emitted |
| `created_at` | Record creation time (for retention) |

App logs may contain internal diagnostic information like provider error messages and request paths, but **never** contain:
- User prompts or response content
- API keys (provider or virtual)
- Request body content

App logs can be purged via the admin API: `DELETE /api/logs/app` - this deletes **ALL** entries, not time-based.

## What Is NOT Logged

To be explicit about the boundaries:

| Data | Logged? | Notes |
|------|---------|-------|
| User messages / prompts | ❌ Never | Not read, not stored, not inspected |
| System prompts | ❌ Never | Passed through unchanged |
| Assistant responses | ❌ Never | Streamed directly to client, not buffered |
| Images / attachments | ❌ Never | Not inspected, forwarded as-is |
| Audio input | ❌ Never | Passed through to provider unchanged |
| API keys (provider or virtual) | ❌ Never | Decrypted in memory only, never written to logs or DB |
| Request body content | ❌ Never | JSON bodies are parsed only for `model` and `stream`; multipart forms only for `model` |
| IP addresses | ❌ Never | Used only for in-memory rate limiting, not stored in request logs |
| User-agent strings | ❌ Never | Not captured |
| X-Forwarded-For headers | ❌ Never | Used only for IP rate limiting when behind trusted proxies |

## Cryptographic Security

### Virtual Key Hashing

Virtual API keys (client authentication) are **SHA-256 hashed** before storage:

```go
// internal/virtualkey/auth.go
func Hash(key string) string {
    hash := sha256.Sum256([]byte(key))
    return hex.EncodeToString(hash[:])
}
```

- Keys are **never stored in plaintext** in the database
- The `virtual_keys.key_hash` column contains only the hash
- When a client presents a key, it is hashed and compared against stored hashes
- Even if the database is compromised, virtual keys cannot be recovered from hashes

### Provider Key Encryption

Provider API keys are encrypted using **AES-256-GCM** with **Argon2id** key derivation:

| Parameter | Value |
|-----------|-------|
| Salt | Random 32-byte per-provider |
| Memory | 8 MB |
| Time | 1 |
| Threads | 4 |
| Output | 32 bytes (256 bits) |

```go
// internal/auth/encryption.go
func Encrypt(plaintext, masterKey string) (*KeyPair, error) {
    salt := make([]byte, 32)
    io.ReadFull(cryptoRand.Reader, salt) // Random per-provider salt
    
    key := argon2.IDKey([]byte(masterKey), salt, 1, 8*1024, 4, 32)
    // ... AES-256-GCM encryption
}
```

**Key points:**
- `MASTER_KEY` environment variable is required for encryption/decryption
- Provider keys are **decrypted in memory only** at request time
- Decrypted keys are **never written to logs**, the database, or sent to the frontend
- The `providers.encrypted_key` column contains only ciphertext
- The `providers.key_nonce` and `providers.key_salt` columns store encryption parameters

### Why Lower Argon2 Parameters?

The Argon2id parameters (t=1, m=8MB, p=4) are intentionally below the RFC 9106 minimum (t=3, m=64MB). This is deliberate:

- `MASTER_KEY` is a high-entropy random value (32+ bytes), **not a user-chosen password**
- Argon2id's primary defense is against low-entropy brute-force, which does not apply here
- Increasing parameters would add latency to every provider key decrypt (including per-request operations) for no meaningful security gain

## IP Address Handling

IP addresses are used **only for in-memory rate limiting** and are **never stored**:

```go
// internal/ratelimit/ip_limiter.go
type ipEntry struct {
    limiter  *rate.Limiter
    rps      float64
    burst    int
    lastUsed time.Time  // Only for cleanup, not logged
}
```

- IP limiters are stored in memory (`map[string]*ipEntry`)
- Entries are cleaned up after **10 minutes of inactivity**
- IP addresses are **not written to `request_logs`** or any persistent storage
- When behind a trusted proxy (configured via `TRUSTED_PROXIES` CIDRs), `X-Forwarded-For` and `X-Real-IP` headers are honored
- IP rate limiting can be disabled via the `rate_limit_ip_enabled` setting

## Data Retention

### Request Logs

Request logs can be purged automatically via the `log_retention` setting:

| Value | Retention |
|-------|-----------|
| `""` (empty) | Keep forever (default) |
| `"24h"` or `"1d"` | 1 day |
| `"168h"` or `"1w"` | 1 week |
| `"720h"` or `"1m"` | 30 days |

Retention cleanup runs **hourly** in the background.

Manual purge is available via the admin API:

```bash
curl -X DELETE http://localhost:8081/api/logs/purge \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"older_than": "1w"}'
```

Supported `older_than` values: `1h`, `1d`, `1w`, `1m`, `all`

### App Logs

App logs (server output) can be purged via:

```bash
curl -X DELETE http://localhost:8081/api/logs/app \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### Provider Deletion

When a provider is deleted:
- `provider_id` is set to `NULL` on historical request logs (via `ON DELETE SET NULL` cascade)
- The audit trail is preserved without retaining the provider's identity
- Provider name in logs shows as `"Deleted"`

### Virtual Key Revocation

When a virtual key is revoked:
- `virtual_key_id` and `virtual_key_name` remain referenced in request logs
- Historical attribution is preserved for analytics
- The key itself (hash) cannot be used for new requests

## Arena History Privacy

The optional **Arena History** feature (disabled by default, configurable in Settings) can persist arena results in your browser's `localStorage`:

- **Model-generated responses** are stored locally for review
- **Preset prompts** are saved by reference only (e.g. "Dilemma preset")
- **Custom user-entered text is never logged** - only the fact that a custom prompt was used is recorded
- History data **never leaves your browser** and can be cleared from Settings
- This is purely a client-side convenience feature; no arena data is sent to the server

## Provider Trust

While Model Hotel does not read your prompts, the underlying providers (OpenAI, Anthropic, DeepSeek, Ollama Cloud, etc.) still receive them in full. Choose providers whose privacy policies align with your requirements.

For sensitive workloads, consider:
- **Local providers** like [Ollama](https://github.com/ollama/ollama) - nothing leaves your infrastructure
- **Self-hosted models** via compatible APIs
- **Private cloud deployments** with data residency guarantees

## Local Deployment

For maximum privacy, run Model Hotel locally with [Ollama](https://github.com/ollama/ollama) or another local provider. This keeps all data on your own infrastructure - nothing leaves your machine.

To use Ollama as a provider:
1. Set `ALLOW_HTTP_PROVIDERS=true` (Ollama typically runs on HTTP, not HTTPS)
2. Add `localhost` to `ALLOWED_PROVIDER_HOSTS`
3. Configure the provider with base URL `http://localhost:11434`

See [Configuration](Configuration) for details.

## Security Summary

| Feature | Implementation |
|---------|----------------|
| Virtual key storage | SHA-256 hash (one-way) |
| Provider key storage | AES-256-GCM + Argon2id (per-provider random salt, 8MB) |
| Request content | Never logged, never stored |
| IP addresses | In-memory only, 10-minute cleanup |
| Error messages | Provider diagnostics only (200-char SSE, 2000-char failover, full in DB) |
| Request identifiers | Random 8-byte hex (not content-based) |
| Data retention | Configurable (1h to 30d, or forever) |
| Master key requirement | Required for provider key encryption/decryption |

## Compliance Considerations

Model Hotel's architecture supports compliance with data protection regulations:

- **GDPR**: No personal data (prompts, responses) is stored. Request logs contain only metadata.
- **Data minimization**: Only essential operational data is collected.
- **Purpose limitation**: Logged data is used only for routing, metering, and diagnostics.
- **Storage limitation**: Automatic retention policies ensure logs are purged after configurable periods.
- **Integrity and confidentiality**: Encryption at rest (provider keys) and hashing (virtual keys) protect sensitive credentials.

For deployments handling sensitive data, consider:
1. Enabling log retention (`log_retention` setting)
2. Using local providers (Ollama, LM Studio)
3. Restricting `ALLOWED_PROVIDER_HOSTS` to trusted endpoints
4. Running behind a reverse proxy with TLS termination
5. Regular purging of app logs via scheduled API calls

---

## Related Documentation

- [[Security]] - Encryption schemes, security headers, and authentication
- [[Virtual Keys]] - Virtual key creation, hashing, and management
- [[Request Logging]] - Request log structure, retention, and truncation
