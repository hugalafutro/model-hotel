# 👁️ Privacy

Model Hotel is designed with privacy as a core principle: **no prompt or response content is ever persisted**. Nothing a client sends and nothing a provider answers is written to the database, to a log line, or to disk.

The gateway is not blind to the body, though. To route a request, size it against token budgets, translate it into a provider's own dialect and keep a provider's error text from quoting the prompt back into a log column, it has to read parts of the body in memory. All of that is transient: the parsed values live for the request and are dropped with it.

## What Is Never Captured

> [!IMPORTANT]
> **Prompts and request content are never stored, logged, or exposed.**

This means:
- **Chat messages** are not stored or logged
- **Images** uploaded via vision API or the image edit/variation endpoints are not inspected
- **System prompts** are not logged
- **Response text** is streamed straight to the client. A non-streaming answer is held in memory (32 MB ceiling) only long enough to read its `usage` counts and normalize the envelope, then dropped
- **Audio uploads** (`/v1/audio/transcriptions`, `/v1/audio/translations`) are forwarded byte-for-byte, never read
- **Generated media** (images, synthesized speech, transcripts) is streamed to the client, never retained
- **Embedding inputs and vectors** are passed through untouched
- **Request body content** is never written to disk

Everything Model Hotel keeps is routing and metering metadata: which key, which model, which provider, how long it took, how many tokens the provider reported.

### What the Gateway Reads in Memory

Four jobs need the body, and none of them outlives the request:

- **Routing**: the `model` and `stream` fields of a JSON body; the `model` form field of a multipart upload.
- **Token sizing**: the length of the message text, the tool definitions and the embeddings input is measured, so a request can still be charged against a virtual key's tokens-per-minute budget when the provider reports no usage of its own. Only lengths are kept; image and audio parts are skipped outright rather than sized, since a base64 blob costs a handful of tokens but would measure as millions. On a multipart request the text form fields (an image edit's `prompt`, for example) are measured the same way, never the upload.
- **Content fence**: the request's own text is indexed so that a stored upstream error fragment sharing a run of 16 or more characters with it can be dropped whole and replaced by the fixed phrase `provider error text withheld`. This exists so a provider that quotes the prompt back inside its error message cannot get prompt text into a log column. Only upstream text is checked; the gateway's own wording is never fenced. The index is built from the body, used, then released.
- **Dialect translation**: a provider that speaks Anthropic Messages, Gemini or the OpenAI Responses API gets the request rewritten into its own shape and its answer rewritten back into a chat completion. The same rewrite path drops parameters a provider has been observed to reject, and re-issues the request once when a 400 names one.

## What Is Logged

The only information recorded per proxied request is strictly necessary for routing, metering, and diagnostics, and all of it lives in the `request_logs` PostgreSQL table. Server-side application logs are a separate surface, covered under [App Logs](#app-logs).

| Data | Column | Purpose |
|------|--------|---------|
| Timestamp | `created_at` | Request timing and analytics |
| Model ID | `model_id` | Usage analytics and cost estimation (e.g. `z-ai/glm-4.6`, `hotel/glm-4.6`) |
| Resolved model ID | `resolved_model_id` | Which provider model actually served a `hotel/` group request |
| Provider ID | `provider_id` | Routing analysis and failover tracking (set to `NULL` when a provider is deleted) |
| Owner | `owner_user_id` | Which account owns a keyless (dashboard chat, model test) request; `NULL` for virtual-key rows and for rows predating migration 067 |
| Virtual key name | `virtual_key_name` | Usage attribution per client |
| Virtual key ID | `virtual_key_id` | Stable key reference (persists even if key is revoked) |
| Token counts | `tokens_prompt`, `tokens_completion` | Usage tracking and billing attribution (provider-reported) |
| Reasoning tokens | `tokens_completion_reasoning` | Thinking tokens, which reasoning models report separately from visible output |
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
| Error kind | `error_kind` | Machine-readable failure classification (`provider_error`, `client_disconnect`, `provider_quota_exhausted` and so on), so the dashboard does not have to substring-match English |
| Streaming flag | `streaming` | Whether the request used SSE streaming |
| Failover attempt | `failover_attempt` | Which provider candidate was used (0-indexed; for retry analysis) |
| Attempt trail | `attempts` | One JSON entry per failover attempt: provider, model, status, error kind, timings, circuit-breaker verdict, and at most 160 characters of that attempt's error text (credential-masked and content-fenced like `error_message`). `NULL` on rows predating migration 078 |
| Request state | `state` | Lifecycle status: `pending` → `streaming` → `completed` / `failed` |
| Endpoint family | `endpoint_type` | Which endpoint the request came through: `chat`, `messages`, `embeddings`, `rerank`, `image`, `tts`, `stt` |
| Request hash | `request_hash` | Random 16-character hex request identifier (see below) |
| Client IP | `client_ip` | Source-address attribution of key usage (trusted-proxy resolved, see [IP Address Handling](#ip-address-handling); `NULL` on rows predating migration 073) |

### About `error_message`

The `error_message` field is populated **only when a request fails** and contains **provider diagnostic information, never user content**. Specifically:

- **Upstream error responses**: When a provider returns a non-200 status code and no failover candidate is available, the upstream response body is captured. It passes through a sanitizer that masks credential-shaped tokens and UUIDs and caps the text at 10,000 bytes (500 bytes for an error frame that arrives mid-stream), and the column itself stores at most 10,000 characters. The 200-character version is the dashboard's live notification text, not the stored row. What lands there is the provider's error JSON (e.g. `{"error": {"message": "Rate limit exceeded"}}`), not the user's prompt. A provider that quotes the prompt back inside its error message does not get it into the row: every fragment of upstream text is checked against the request's own text as it is captured, and one sharing a run of 16 or more characters with it is dropped whole (`provider error text withheld`; a per-attempt detail is simply left empty) rather than stored with the run blanked, so what is kept is either all of the provider's own words or none of them.
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

Where a discovery or quota poll logs a provider's error body, that body is masked for credentials and capped at 2000 characters. It is a provider-API response (a model list, a credit balance), never a proxied request.

App logs can be purged via the admin API: `DELETE /api/logs/app`. Sent with no body it clears everything; an `older_than` of `1h`, `1d`, `1w`, `1m` or `all` clears only that far back. They are also swept hourly against the same `log_retention` setting the request logs use, which defaults to keep-forever (see [Data Retention](#data-retention)).

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
| Request body content | ❌ Never | Read in memory for routing, token sizing, the content fence and dialect translation; never written anywhere |
| IP addresses | ⚠️ Metadata | Recorded in app-log lines (access/auth), the audit trail, the active-sessions list, and per-request in `request_logs.client_ip`; each follows its surface's retention (request logs: the `log_retention` setting, which defaults to keep-forever) |
| User-agent strings | ⚠️ Sessions only | Up to 256 bytes stored per dashboard login for the active-sessions list; deleted with the session |
| X-Forwarded-For headers | ⚠️ Resolved | Honored only from `TRUSTED_PROXIES`; the resolved client IP is what gets recorded, never the raw header |

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

### Stored Key Fragments

One fragment of each key is stored in the clear so the dashboard can tell two keys apart: `virtual_keys.key_preview` holds a virtual key's first three and last four characters, and `providers.masked_key` a provider key's first two and last four. A provider key shorter than 13 characters is masked entirely instead. Neither fragment is enough to reconstruct a key, but both travel with backups and config exports, so treat them as identifying rather than secret.

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
    io.ReadFull(randReader, salt) // Random per-provider salt

    key := deriveKey(masterKey, salt) // argon2.IDKey(masterKey, salt, 1, 8*1024, 4, 32)
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

The client address is resolved once per request with trusted-proxy awareness: `X-Forwarded-For` / `X-Real-IP` are honored only when the TCP peer is inside the `TRUSTED_PROXIES` CIDRs, so a direct client can never spoof its own address. The resolved IP is operational metadata, used in a few bounded places:

- **In-memory rate limiting**: per-IP token buckets (`map[string]*ipEntry`), cleaned up after **10 minutes of inactivity**, never persisted. Can be disabled via the `rate_limit_ip_enabled` setting.
- **Application logs**: access lines and auth warnings (failed logins, invalid keys) record the client IP alongside routing metadata, subject to the same retention as other app logs. Never request or prompt content.
- **Audit trail**: each recorded admin action stores the caller address, pruned per the `audit_retention_days` setting (90 days by default). That setting is not exposed in the dashboard, so changing it means writing the setting row directly.
- **Active sessions**: each dashboard login stores the IP it was minted from, shown in Settings so the operator can spot a session that isn't theirs; it is deleted with the session.
- **Request logs**: each proxied call's `request_logs` row stores the resolved client address (`client_ip`, since migration 073), so virtual-key usage stays attributable to a source address for as long as request logs are kept. Rows are purged together with the rest of the request log per the `log_retention` setting.

Note that `log_retention` defaults to keep-forever: if storing client addresses indefinitely is a concern for your deployment, set a bounded `log_retention` (see [Data Retention](#data-retention)) so request-log rows, `client_ip` included, are purged on schedule.

## Data Retention

### Request Logs

Request logs can be purged automatically via the `log_retention` setting:

| Value | Retention |
|-------|-----------|
| `""` or `"0"` | Keep forever (default) |
| Any Go duration (`"24h"`, `"168h0m0s"`) | That window. The dashboard's day slider writes this form |
| `"1d"`, `"1w"`, `"1m"` | Legacy dropdown tokens: 1 day, 1 week, 30 days. Note `1m` is the 30-day token, not one minute; for a minute-scale window write `"60m"` |
| A duration that parses to zero or less (`"0s"`) | Keep forever |

Retention cleanup runs **hourly** in the background and deletes from `request_logs` and `app_logs` alike. A value that is neither a duration nor a legacy token is skipped, with one warning rather than one per hour.

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

### What a Backup Contains

A backup is a `pg_dump` of the whole database in PostgreSQL's custom format, written **unencrypted** to the backup directory with an HMAC signature in a `.sig` file beside it. The signature proves the dump has not been altered; it does not conceal what is in it.

So a backup carries everything the database carries: request logs (client IPs, provider error text, token counts), app logs, the audit trail, sessions, virtual-key hashes and previews, and provider keys as ciphertext with their salts and nonces. It carries no prompt or response content, because none is stored in the first place. Restoring those provider keys needs the same `MASTER_KEY`: a dump taken from an instance with a different master key restores the rows but cannot decrypt them.

Treat the backup directory as sensitive, and keep any copy you move off the machine encrypted.

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

## What Leaves the Instance

Every outbound connection Model Hotel makes:

- **The configured providers**, on every proxied request. They receive the prompt in full, carrying that provider's own API key. This is the point of the gateway, and the only place content leaves.
- **Provider discovery and quota polls**, to each provider's model-list and credit endpoints, again carrying that provider's key. No request content.
- **`https://models.dev/api.json`**, during discovery, to fill in context windows and pricing for models a provider does not describe itself. Anonymous: no key, no content.
- **The Have I Been Pwned range API** (`https://api.pwnedpasswords.com` by default), when a dashboard password is set or changed and breached-password screening is on. Only the first five characters of the password's SHA-1 hash are sent; see [Local Deployment](#local-deployment) for how to turn it off or point it at a mirror.
- **An OpenTelemetry collector**, only when `OTEL_EXPORTER_OTLP_ENDPOINT` or `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` is set. It receives the same structured application-log records the instance already keeps, client IP addresses included. Logs only: no traces, no metrics, and no request content, since none is logged in the first place.

Nothing else dials out. Front Desk polls this instance, not the other way round.

## Provider Trust

While Model Hotel never stores your prompts, the underlying providers (OpenAI, Anthropic, DeepSeek, Ollama Cloud, etc.) still receive them in full. Choose providers whose privacy policies align with your requirements.

For sensitive workloads, consider:
- **Local providers** like [Ollama](https://github.com/ollama/ollama) - nothing leaves your infrastructure
- **Self-hosted models** via compatible APIs
- **Private cloud deployments** with data residency guarantees

## Local Deployment

For maximum privacy, run Model Hotel locally with [Ollama](https://github.com/ollama/ollama) or another local provider. This keeps all data on your own infrastructure - nothing leaves your machine.

To use Ollama as a provider:
1. Set `ALLOW_HTTP_PROVIDERS=true` (Ollama typically runs on HTTP, not HTTPS)
2. Add the address Model Hotel will actually dial to `ALLOWED_PROVIDER_HOSTS`: `localhost` when
   Model Hotel runs directly on the same machine, otherwise the machine's network address, since
   a containerized Model Hotel cannot reach your own machine through `localhost` at all
3. Add the provider, picking the **Ollama** type and giving it that same address (the server has
   to be running: Model Hotel confirms it is really an Ollama before saving)

One optional outbound call remains even then: with breached-password screening on (the default), setting or changing a dashboard password sends the first five characters of the password's SHA-1 hash to the Have I Been Pwned range API and matches the returned suffixes locally, so neither the password nor its full hash leaves the instance. Switch the check off in **Settings > Authentication > Password policy** (or set `PWNED_PASSWORD_CHECK_ENABLED=false`) to keep account changes fully offline, or point `PWNED_PASSWORD_API_URL` at a self-hosted mirror; see [Configuration](Configuration#breached-password-screening).

See [Configuration](Configuration) for details.

## Security Summary

| Feature | Implementation |
|---------|----------------|
| Virtual key storage | SHA-256 hash (one-way) |
| Provider key storage | AES-256-GCM + Argon2id (per-provider random salt, 8MB) |
| Request content | Read in memory for routing, sizing, fencing and translation; never logged, never stored |
| IP addresses | Rate-limit buckets in-memory (10-minute cleanup); logged addresses follow app-log, audit, and request-log retention |
| Error messages | Provider diagnostics only, credential-masked and content-fenced, at most 10,000 characters |
| Request identifiers | Random 8-byte hex (not content-based) |
| Data retention | Configurable: any Go duration, or keep forever (the default) |
| Backups | Unencrypted `pg_dump`, HMAC-signed; carries key ciphertext and client IPs, no content |
| Master key requirement | Required for provider key encryption/decryption |

## Compliance Considerations

Model Hotel's architecture supports compliance with data protection regulations:

- **GDPR**: Prompts and responses are never stored. Operational metadata does include client IP addresses (personal data under the GDPR) in app logs, the audit trail, the active-sessions list, and request logs (`client_ip`). The audit trail is retention-bound by default (90 days) and sessions expire on their own, but request logs and app logs share the `log_retention` setting, which defaults to keep-forever: set it when IP addresses must not be kept indefinitely. Backups inherit whatever was in the database when they were taken.
- **Data minimization**: Only essential operational data is collected.
- **Purpose limitation**: Logged data is used only for routing, metering, and diagnostics.
- **Storage limitation**: Once a retention window is set, logs are purged on that schedule automatically. Note that no window is set by default.
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
