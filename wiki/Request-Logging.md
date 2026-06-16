# 📜 Request Logging

Every request that passes through the proxy is logged with detailed telemetry. This helps you understand performance, identify bottlenecks, and debug issues.

## What Gets Logged

> [!IMPORTANT]
> The proxy records metadata only - **never the prompt content or response text**.

All fields are written to the `request_logs` PostgreSQL table.

### Request Log Schema

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID | Primary key (auto-generated via `uuid.New()`) |
| `request_hash` | TEXT | Random 16-character hex request identifier. Generated with `crypto/rand` (8 random bytes → 16 hex characters). Not content-derived. |
| `model_id` | TEXT | The requested model (e.g. `deepseek/deepseek-chat` or `hotel/gpt-4`) |
| `provider_id` | UUID | Which provider handled the request (NULL until resolved, NULL if provider deleted) |
| `virtual_key_id` | UUID | Foreign key to `virtual_keys` table (NULL for anonymous requests) |
| `virtual_key_name` | TEXT | Which virtual key was used. Retained even after key deletion for audit purposes. |
| `status_code` | INT | HTTP status persisted for the request. The column is nullable with no default: the initial insert omits it, so **in-progress and never-responded rows are `NULL`** (not `0`). A **mid-stream failure** is then explicitly written as `0` (its classification, when one is assigned, lives in `error_kind`). On a *non-streaming* failure the value is path-dependent: a **terminal upstream non-2xx** keeps the **upstream's own** status (e.g. `500`, `429`, `401`); only when **all candidates are exhausted** without a forwardable response does the proxy synthesise a *truthful* gateway code — **499** (client hangup), **504** (failover/retry timeout), or **502** (provider/transport failure). See [Error classification](#error-classification-and-status-codes). |
| `error_message` | TEXT | Error text on failure. Populated for upstream provider errors and proxy-internal errors (invalid model format, provider not found, etc.). Empty on success. |
| `error_kind` | TEXT | Machine-readable failure classification (added in migration `045`), set alongside `error_message`. One of `client_disconnect`, `provider_error`, `provider_timeout`, `failover_timeout`, `retry_timeout`, `internal`, `validation`, `auth`. NULL on success, for rows predating migration `045` (no backfill), and for a few edge paths that record only an `error_message` (e.g. a stream truncated before the `[DONE]` sentinel). The dashboard's "Interrupted" badge reads this, falling back to substring-matching the English `error_message` only when it is NULL. The Prometheus `error_kind` label carries the raw value verbatim and is **empty** (`""`) when unset — it does **not** fall back, so legacy/truncated rows with no kind appear unclassified in metrics. |
| `streaming` | BOOLEAN | Whether the request used SSE streaming |
| `failover_attempt` | INT | Which attempt this was - `0` for the first try, incrementing with each failover |
| `resolved_model_id` | TEXT | The actual upstream model ID used for the request (e.g. `openai/gpt-4o`). For hotel-routed requests, this differs from `model_id` which contains the requested `hotel/` name (e.g. `hotel/gpt-4o`). NULL for non-failover requests where the requested and resolved model are the same. |
| `state` | TEXT | Request lifecycle state: `pending` → `streaming` → `completed` or `failed` |
| `endpoint_type` | TEXT | Endpoint family the request came through: `chat` (default), `embeddings`, `image`, `tts`, or `stt`. Written at INSERT time; exposed in the Logs API response, filterable via `?endpoint_type=`, and shown as a badge in the dashboard Logs page for non-chat requests. |
| `duration_ms` | DOUBLE PRECISION | End-to-end wall time (request start to response end) |
| `latency_ms` | DOUBLE PRECISION | Provider response time only (`duration_ms - proxy_overhead_ms`) |
| `proxy_overhead_ms` | DOUBLE PRECISION | Total proxy overhead (sum of all seven overhead phases) |
| `parse_ms` | DOUBLE PRECISION | JSON parsing and request validation time |
| `failover_lookup_ms` | DOUBLE PRECISION | Time resolving the failover group for `hotel/` requests (0 for direct requests) |
| `model_lookup_ms` | DOUBLE PRECISION | Time resolving the model entity, checking enabled status, building candidate list |
| `provider_lookup_ms` | DOUBLE PRECISION | Time finding provider record in database (excludes key decryption) |
| `key_decrypt_ms` | DOUBLE PRECISION | Time decrypting provider API key (first call per 10-minute cache window; subsequent calls near-zero) |
| `settings_read_ms` | DOUBLE PRECISION | Time reading runtime settings from cache (circuit breaker state, rate limits, etc.) |
| `dial_ms` | DOUBLE PRECISION | Full DNS + TCP dial time to the upstream provider (renamed from `safe_dial_ms` in migration 032) |
| `cache_hits` | JSONB | Per-component cache hit/miss flags (`failover`, `model`, `provider`, `key`, `settings`); `true` = prewarmed cache hit, `false` = miss, absent = not applicable |
| `response_header_ms` | REAL | Time until upstream HTTP response headers arrived (renamed from the old `ttft_ms` in migration 035, which had actually measured headers, not tokens) |
| `ttft_ms` | REAL | True time to first token (streaming requests only). Measured by the TTFT probe that reads ahead to confirm the first data chunk before committing the stream. |
| `tokens_per_second` | DOUBLE PRECISION | Streaming throughput (`completion_tokens / total_duration × 1000`) |
| `tokens_prompt` | INT | Number of prompt tokens reported by the provider |
| `tokens_completion` | INT | Number of completion tokens reported by the provider |
| `tokens_completion_reasoning` | INT | Reasoning tokens (DeepSeek-R1, etc.). Written to DB and exposed in Logs API. |
| `tokens_prompt_cache_hit` | INT | Prompt cache hit tokens (DeepSeek). Written to DB and exposed in the Logs API response. |
| `tokens_prompt_cache_miss` | INT | Prompt cache miss tokens (DeepSeek). Written to DB and exposed in the Logs API response. |
| `created_at` | TIMESTAMPTZ | When the request was inserted (defaults to `now()`) |

### Privacy Note

The proxy **never** logs prompt text, completion text, or any user content. The `request_hash` is a random identifier generated with `crypto/rand` (8 random bytes → 16 hex characters) - it is not derived from request content in any way.

### Dead Columns

The `request_id TEXT` column (from the initial schema) was never populated by the proxy and was always empty in the Logs API response. It was dropped in migration `030_drop_request_id.sql`.

> ⚠️ **The `prompt` column was removed in migration 027.** It was added in migration 006 but no application code ever wrote to it. The column has been dropped entirely - it no longer exists in the database schema.

## Proxy Overhead Breakdown

The `proxy_overhead_ms` field is decomposed into seven phases, measured in parallel with the actual provider request:

| Phase | DB Column | Description |
|-------|-----------|-------------|
| Parse | `parse_ms` | JSON parsing and request validation |
| Failover lookup | `failover_lookup_ms` | Resolving the failover group for `hotel/` requests (0 for direct provider requests) |
| Model lookup | `model_lookup_ms` | Resolving the model entity, checking model and provider enabled status, and building the ordered candidate list |
| Provider lookup | `provider_lookup_ms` | Finding the provider record in the database (excludes key decryption time) |
| Key decryption | `key_decrypt_ms` | Decrypting the provider API key (first call per 10-minute cache window; subsequent calls within the window are near-zero) |
| Settings read | `settings_read_ms` | Time spent reading runtime settings from the cached settings store (circuit breaker state, rate limits, etc.) |
| Dial | `dial_ms` | Full DNS + TCP dial time to the upstream provider |

The companion `cache_hits` JSONB column records, per component (`failover`, `model`, `provider`, `key`, `settings`), whether the lookup hit a prewarmed cache - useful for explaining why the same phase is sometimes fast and sometimes slow.

These represent pure proxy overhead - the time spent **inside** Model Hotel before and after the upstream call. You can use this to determine whether latency is coming from the provider or from the proxy itself.

The relationship between fields is:

- `proxy_overhead_ms = parse_ms + failover_lookup_ms + model_lookup_ms + provider_lookup_ms + key_decrypt_ms + settings_read_ms + dial_ms`
- `latency_ms = duration_ms - proxy_overhead_ms`

## Log Lifecycle and State Machine

### Request States

| State | Description | Transitions |
|-------|-------------|-------------|
| `pending` | Initial state when request is received | → `streaming` (on headers) or → `failed` (on error) |
| `streaming` | SSE stream in progress (headers sent, tokens flowing) | → `completed` (on success) or → `failed` (on error/interrupt) |
| `completed` | Request finished successfully (2xx status) | Terminal state |
| `failed` | Request failed (4xx/5xx status, timeout, or stale cleanup) | Terminal state |

### Logging Phases

#### 1. Initial INSERT (Async)

When a request arrives, `insertRequestLogAsync()` is called:

```go
func (h *Handler) insertRequestLogAsync(logEntry *requestLogData) {
    logEntry.id = uuid.New().String()
    logEntry.requestHash = generateRequestHash()
    
    // Async INSERT with minimal fields
    INSERT INTO request_logs (id, model_id, request_hash, streaming, virtual_key_name, virtual_key_id, failover_attempt, state)
    VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
}
```

Fields written at this stage:
- `id` - UUID generated synchronously
- `request_hash` - Random 16-char hex ID
- `model_id` - From request body
- `streaming` - Boolean from request
- `virtual_key_name` - From context (if authenticated)
- `virtual_key_id` - From context (if authenticated)
- `failover_attempt` - `0` initially
- `state` - `"pending"`

All other fields are NULL at this point. The INSERT runs in a goroutine with a 5-second timeout.

#### 2. UPDATE on Headers (Streaming) or Complete (Non-Streaming)

When upstream headers arrive (streaming) or the full response is received (non-streaming), `updateRequestLog()` is called:

```go
func (h *Handler) updateRequestLog(ctx context.Context, logEntry *requestLogData) {
    h.WaitForInsert(logEntry) // Block until async INSERT completes
    
    logEntry.latencyMs = logEntry.durationMs - logEntry.proxyOverheadMs
    
    UPDATE request_logs SET
        provider_id = $2,
        status_code = $3,
        duration_ms = $4,
        latency_ms = $19,
        proxy_overhead_ms = $5,
        parse_ms = $6,
        model_lookup_ms = $7,
        provider_lookup_ms = $8,
        key_decrypt_ms = $9,
        dial_ms = $20,
        settings_read_ms = $21,
        ttft_ms = $10,
        tokens_per_second = $11,
        tokens_prompt = $12,
        tokens_completion = $13,
        tokens_prompt_cache_hit = $14,
        tokens_prompt_cache_miss = $15,
        error_message = $16,
        failover_attempt = $17,
        state = $18
    WHERE id = $1
}
```

For streaming requests:
- State transitions: `pending` → `streaming` (headers received) → `completed`/`failed` (stream ends)
- The same `updateRequestLog()` is called twice: once when headers arrive, once when the stream completes

For non-streaming requests:
- State transitions: `pending` → `completed`/`failed` (single UPDATE)

#### 3. Error Handling

The `failRequest()` helper populates error details:

```go
func (h *Handler) failRequest(ctx context.Context, logData *requestLogData, statusCode int, errMsg string, ...) {
    logData.statusCode = statusCode
    logData.errorMessage = errMsg
    logData.durationMs = float64(time.Since(startTime).Microseconds()) / 1000.0
    logData.state = "failed"
    h.updateRequestLog(ctx, logData)
}
```

Error messages are truncated to 200 characters (with `…` suffix) for SSE events.

### SSE Events

Request lifecycle events are published to the event bus:

| Event Type | Severity | When | Metadata |
|------------|----------|------|----------|
| `request.started` | `info` | On initial INSERT | `request_id`, `model_id`, `streaming`, `state` |
| `request.completed` | `success` | On successful completion | `request_id`, `model_id`, `state`, `status_code` |
| `request.completed` | `warning` | On failure | `request_id`, `model_id`, `state`, `status_code`, `error_message` (truncated) |

## Log Retention and Purge

### Automatic Retention

The `log_retention` setting controls how long request logs are kept. When set, a background goroutine purges logs older than the retention period every hour.

**Accepted values:** `1h`, `1d` (or `24h`), `1w` (or `168h`), `1m` (or `720h`). Set to `0` or leave empty to disable automatic cleanup.

This setting can be changed at runtime via the Settings API (`PUT /api/settings`).

### Stale Request Cleanup

The `stale_request_timeout` setting (default: `30m`) controls how long a request can remain in `pending` or `streaming` state before being marked as `failed` with the error `"request interrupted (stale)"`.

**Cleanup runs every 5 minutes** and applies two strategies:

1. **Server-start-time check:** Any in-progress row that predates the current server process is definitively orphaned (the previous process is dead). This has zero false-positive risk.

2. **Age-based check:** Rows older than `stale_request_timeout` are marked failed. This catches in-process orphans (e.g., a panic skips the final `updateRequestLog`). The timeout is generous to avoid killing legitimate long-running streaming requests.

**Accepted values:** `5m`, `10m`, `15m`, `30m` (default), `1h`, `2h`, `0s` (disabled).

Stale cleanup also runs on server startup, marking any in-progress rows that predate the current process as failed.

### Manual Purge

**API endpoint:** `DELETE /api/logs/purge`

Request body:
```json
{
  "older_than": "1h"
}
```

**Accepted values:** `1h`, `1d`, `1w`, `1m`, `all`

Response: `204 No Content` on success.

## Viewing Logs

The dashboard has two log views, accessible from the **Logs** sidebar entry with sub-mode toggling.

### Requests Log

**API endpoint:** `GET /api/logs`

Shows all proxy requests with:
- Sortable columns (timestamp, model, provider, status, latency, TTFT, tokens, etc.)
- Filters (date range, model, provider, status code, virtual key)
- Pagination (default 20 per page, max 200)
- In-flight request highlighting (rows pulse with animation while streaming)
- Click-through to provider/model details
- Status code color coding (green for 2xx, red for 4xx/5xx)

![Request Logs](screenshots/logs.png)

### App Logs

**API endpoint:** `GET /api/logs/app`

Shows the server's own application logs:
- Persisted to the `app_logs` database table
- Async batch writer for performance (buffers writes, flushes every 500ms or every 50 entries)
- Severity levels (`info`, `warning`, `error`) - inferred from log content heuristics
- Source attribution (which package/module produced the log, parsed from `[prefix]` tags)
- Two access modes:
  - **Ring buffer mode** (default) - returns the last N entries from an in-memory buffer of 500, with optional `?after=` polling for live updates
  - **History mode** (`?history=true`) - queries the database with full filtering, pagination, and sorting

![App Logs](screenshots/applogs.png)

#### Debug verbosity (`DEBUG_LOG` / `DEBUG_LOG_SCOPES`)

By default the server logs at Info and above. To get the per-request Debug
mechanics:

- **`DEBUG_LOG=true`** turns Debug on for **every** scope. Thorough, but it floods
  both stdout and this App Logs view at any real request rate.
- **`DEBUG_LOG_SCOPES=failover,ratelimit`** turns Debug on for **only** the named
  scopes - far more usable in production. It is ignored when `DEBUG_LOG=true`.

A scope is the source prefix before the first `:` in a log line (matched
case-insensitively). The canonical scopes are:

`proxy`, `resolve`, `discovery`, `failover`, `provider`, `settings`, `backup`,
`webauthn`, `stats`, `system`, `db`, `admin`, `applogs`, `events`, `ratelimit`,
`keycache`, `docker`, `auth`, `model`, `virtual-keys`, `version`, `api`.

Both variables are startup-only (env vars, set via `.env` / compose). See
[[Configuration]] for the full table.

#### App Logs Schema

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID PK | Auto-generated |
| `timestamp` | TIMESTAMPTZ NOT NULL | When the log was emitted |
| `level` | TEXT NOT NULL | Severity: `info`, `warning`, `error` |
| `source` | TEXT NOT NULL | Package/module that emitted the log |
| `message` | TEXT NOT NULL | Log message content |
| `created_at` | TIMESTAMPTZ NOT NULL | DB insertion timestamp (default `now()`) |

## Filtering and Search

### Request Logs (`GET /api/logs`)

| Parameter | Description |
|-----------|-------------|
| `page` | Page number (default 1) |
| `per_page` | Results per page (1–200, default 20) |
| `model_id` | Filter by model ID (ILIKE match) |
| `provider_id` | Filter by provider UUID |
| `status_code` | Filter by HTTP status. Accepts exact codes, `4xx`, `5xx`, or `0` for in-progress/never-responded |
| `endpoint_type` | Filter by endpoint family: `chat`, `embeddings`, `image`, `tts`, `stt` (unknown values are ignored) |
| `from` | Start timestamp (RFC3339) |
| `to` | End timestamp (RFC3339) |
| `sort_by` | Sort column: `time`, `model`, `provider`, `status`, `tokens`, `tps`, `ttft`, `duration`, `overhead`, `key` |
| `sort_dir` | `asc` or `desc` (default `desc`) |

**Special sorting behavior:**
- `provider`: NULL providers sort last, deleted providers (name NULL but ID present) sort second-to-last
- `status`: Errors with cancel/disconnect/context-canceled messages sort after other errors
- `key`: Deleted virtual keys (ID present but no matching row) sort last

### App Logs (history mode: `GET /api/logs/app?history=true`)

| Parameter | Description |
|-----------|-------------|
| `level` | Filter by severity: `info`, `warning`, `error` |
| `source` | Filter by log source (e.g. `proxy`, `auth`, `discovery`) |
| `search` | Text search in message (ILIKE match) |
| `from` | Start timestamp (RFC3339) |
| `to` | End timestamp (RFC3339) |
| `page` | Page number (default 1) |
| `per_page` | Results per page (1–100, default 20) |
| `sort_by` | Sort column: `time`, `level`, `source`, `message` |
| `sort_dir` | `asc` or `desc` (default `desc`) |

History mode returns a structured response including `entries`, `total`, `page`, `per_page`, plus `level_counts` and `source_counts` aggregates (cached for 5 seconds).

## Streaming Request Handling

Streaming requests are logged in three phases:

### Phase 1: On Start

A log entry is created with `state=pending`. The initial INSERT (async) writes:
- `model_id`
- `request_hash`
- `streaming=true`
- `virtual_key_name`
- `virtual_key_id`
- `failover_attempt=0`
- `state=pending`

All other fields are NULL at this point.

### Phase 2: When Upstream Headers Arrive

The entry is updated with:
- `state=streaming`
- `provider_id` (resolved provider)
- `status_code` (from upstream)
- `proxy_overhead_ms` and all breakdown fields (`parse_ms`, `model_lookup_ms`, etc.)
- `ttft_ms` (time to first token)
- `resolved_model_id` (actual upstream model ID, differs from `model_id` for hotel-routed requests)

### Phase 3: On Completion

The same entry is updated with final status:
- `state=completed` or `state=failed`
- `duration_ms` (total wall time)
- `latency_ms` (provider time only)
- `tokens_prompt`, `tokens_completion`
- `tokens_per_second` (throughput)
- `error_message` (if failed)

This means you can see in-flight requests in the log table in real-time. They appear with a pulsing row animation and update as the stream completes. A request that is interrupted (server crash, client disconnect) will remain in `pending` or `streaming` state until the stale cleanup goroutine marks it as `failed`.

## How Model Name, Provider, and Token Counts Are Captured

### Model Name

The `model_id` field is extracted directly from the request body's `model` field:

```go
var req ChatCompletionRequest
json.Unmarshal(bodyBytes, &req) // req.Model → logData.modelID
```

For failover group requests (`hotel/xxx`), the display model name is logged, not the resolved provider-specific model.

### Provider Resolution

The `provider_id` is populated when the upstream connection is established:

```go
logData.providerID = candidate.provider.ID // After resolveHotelModel() succeeds
```

If the provider is later deleted, the `provider_id` remains (foreign key with no CASCADE), but the JOIN in the Logs API returns `'Deleted'` as the provider name.

### Token Counts

Token counts are extracted from the upstream response:

**Non-streaming:**
```go
var upstreamResp ChatCompletionResponse
json.Unmarshal(body, &upstreamResp)
logData.tokensPrompt = upstreamResp.Usage.PromptTokens
logData.tokensCompletion = upstreamResp.Usage.CompletionTokens
```

**Streaming:**
Tokens are accumulated as SSE chunks arrive:
```go
for scanner.Scan() {
    // Parse data: {"usage": {"prompt_tokens": 10, "completion_tokens": 20}}
    if chunk.Usage.PromptTokens > 0 {
        promptTokens = chunk.Usage.PromptTokens
    }
    if chunk.Usage.CompletionTokens > 0 {
        completionTokens = chunk.Usage.CompletionTokens
    }
}
logData.tokensPrompt = promptTokens
logData.tokensCompletion = completionTokens
```

Some providers send token counts only in the final chunk; others send them in every chunk. The proxy uses the last non-zero value.

### DeepSeek Cache Tokens

DeepSeek providers may report `prompt_cache_hit_tokens` and `prompt_cache_miss_tokens` in the `usage` object. These are captured, written to the `tokens_prompt_cache_hit` and `tokens_prompt_cache_miss` columns, and exposed in the Logs API response.

## Error Logging for Failed Requests

### Error classification and status codes

Most failed requests are tagged with a machine-readable [`error_kind`](#request-log-schema), defined in `internal/proxy/reqerror.go`. The persisted `status_code` is **not** a fixed function of `error_kind` — it depends on *where* the request failed (see the [`status_code`](#request-log-schema) column and the notes below the table):

| `error_kind` | Status code | When | Example `error_message` |
|--------------|-------------|------|--------------------------|
| `validation` | 400 / 404 | Malformed body, missing model, invalid model format (400); unknown model / no enabled providers (404) | `"invalid request body"`, `"model 'xxx' not found"` |
| `auth` | 403 | Virtual key lacks access to any provider for the model | `"virtual key does not have access to any provider for this model"` |
| `provider_error` | 502 | Upstream returned a non-2xx or the transport failed (all candidates exhausted) | `"all 3 providers failed; last error: provider \"X\" returned HTTP 500 on attempt 2"` |
| `provider_timeout` | 502 | TTFT probe or stall watchdog fired — provider connected but produced no output in time | `"provider \"X\" did not return a response in time on attempt 1"` |
| `failover_timeout` | 504 | The overall failover deadline expired | `"request timed out while waiting on provider \"X\""` |
| `retry_timeout` | 504 | The param-strip retry's deadline expired | `"retry without unsupported parameters timed out on provider \"X\""` |
| `client_disconnect` | 499 | The calling client hung up before we responded | `"client disconnected during attempt 1 to provider \"X\""` |
| `internal` | 502 | Gateway-internal failure (e.g. could not build the upstream request) | `"internal error on attempt 1: …"` |

> **The `Status code` column is the code written for non-streaming failures.** `validation` and `auth` rows always carry the code shown (they are rejected before any upstream attempt), as does the all-candidates-exhausted path. Two cases diverge:
> - A **terminal upstream non-2xx** (non-streaming) is logged with the **upstream's own** status (e.g. `500`, `429`, `401`), not `502` — the gateway only synthesises `502`/`504`/`499` once *every* candidate is exhausted without a forwardable response.
> - **Any mid-stream failure** is logged with `status_code = 0` regardless of `error_kind`. Find these by `state = 'failed'` (and `error_kind`), not by status.

Two background states are written by the stale-log sweep in `cmd/server/main.go` (not a live request). The sweep updates only `state`, `error_kind`, and `error_message` — it **does not touch `status_code`**, so a still-pending row keeps its `NULL`/`0`, while a row already in the `streaming` state retains its interim status (typically `200`):

| Category | `status_code` | `error_kind` | `error_message` |
|----------|---------------|--------------|------------------|
| Stale timeout | unchanged (NULL/0, or 200 if streaming) | `internal` | `"request interrupted (stale)"` |
| Server restart | unchanged (NULL/0, or 200 if streaming) | `internal` | `"request interrupted (server restart)"` |

> Historical rows predating migration `045` have a NULL `error_kind`; the dashboard falls back to substring-matching their `error_message` to render the "Interrupted" badge.

### Error Message Truncation

For SSE events, error messages are truncated to 200 characters with a `…` suffix:

For **failover non-200 responses**, the upstream error body is truncated to **2000 characters** (with `…` suffix) before logging and forwarding to the client. This prevents excessively large error responses from consuming memory or log space.

```go
msg = fmt.Sprintf("Request failed: %s - %s", logEntry.modelID, logEntry.errorMessage)
if len(msg) > 200 {
    msg = msg[:200] + "…"
}
```

The full error message is stored in the database without truncation.

### Client Disconnect Handling

Client disconnects are detected via context cancellation:

```go
select {
case <-r.Context().Done():
    clientDisconnected = true
    goto logUpdate
}
```

The log entry is updated with `state=failed` and an appropriate error message.

## Database Migrations History

The `request_logs` table has evolved through these migrations:

| Migration | Changes |
|-----------|---------|
| `001_init.sql` | Initial schema: `id`, `provider_id`, `model_id`, `status_code`, `latency_ms`, `tokens_prompt`, `tokens_completion`, `streaming`, `error_message`, `created_at` |
| `006_enhanced_logs.sql` | Added: `request_hash`, `ttft_ms`, `proxy_overhead_ms`, `duration_ms`, `tokens_per_second`, `virtual_key_name`, `prompt` (never used) |
| `007_overhead_breakdown.sql` | Added: `parse_ms`, `model_lookup_ms`, `provider_lookup_ms`, `key_decrypt_ms` |
| `008_timing_precision.sql` | Converted timing columns from INT to REAL for sub-ms precision |
| `011_failover_attempt.sql` | Added: `failover_attempt` |
| `012_add_virtual_key_id_to_request_logs.sql` | Added: `virtual_key_id` |
| `013_backfill_virtual_key_id.sql` | Backfilled `virtual_key_id` for existing logs with matching `virtual_key_name` |
| `020_log_state_column.sql` | Added: `state` with default `'pending'`, backfilled existing rows |
| `024_cleanup_stale_logs.sql` | One-time cleanup of rows stuck in `pending`/`streaming` state |
| `027_drop_unused_prompt_column.sql` | Dropped `prompt` column (never written to) |
| `028_add_timing_columns.sql` | Added: `safe_dial_ms`, `settings_read_ms` (DOUBLE PRECISION) |
| `030_drop_request_id.sql` | Dropped `request_id` column (never populated) |
| `031_reasoning_tokens.sql` | Added: `tokens_completion_reasoning` (reasoning/thinking models report these separately) |
| `032_rename_dial_add_failover_lookup.sql` | Renamed `safe_dial_ms` → `dial_ms` (now full DNS+TCP dial); added `failover_lookup_ms` |
| `035_rename_ttft_to_response_header_ms.sql` | Renamed old `ttft_ms` → `response_header_ms` (it measured time-to-headers); added new true `ttft_ms` |
| `036_resolved_model_id.sql` | Added: `resolved_model_id` (actual model that served a `hotel/` request) |
| `042_cache_hits.sql` | Added: `cache_hits` JSONB (per-component cache hit/miss flags) |
| `043_endpoint_type.sql` | Added: `endpoint_type` with default `'chat'` (multimodal proxy endpoints: embeddings, image, tts, stt) |
| `045_error_kind.sql` | Added: `error_kind` (nullable machine-readable failure classification; no backfill) |

## Implementation Details

### Async INSERT with Synchronous ID

The log ID is generated synchronously, but the INSERT runs asynchronously:

```go
func (h *Handler) insertRequestLogAsync(logEntry *requestLogData) {
    logEntry.id = uuid.New().String()         // Synchronous
    logEntry.requestHash = generateRequestHash() // Synchronous
    logEntry.insertWg.Add(1)
    
    go func() {
        defer logEntry.insertWg.Done()
        // Async INSERT with 5-second timeout
    }()
}
```

This ensures the ID is available for the subsequent UPDATE without blocking the request.

### WaitForInsert Safety

Before any UPDATE, `WaitForInsert()` blocks until the async INSERT completes:

```go
func (h *Handler) WaitForInsert(logEntry *requestLogData) {
    done := make(chan struct{})
    go func() {
        defer close(done)
        logEntry.insertWg.Wait()
    }()
    select {
    case <-done:
    case <-time.After(5 * time.Second):
        debuglog.Warn("proxy: timed out waiting for request log INSERT")
    }
}
```

This prevents race conditions where the UPDATE runs before the INSERT.

### Log Caching

The Logs API caches query results with a short TTL to reduce database load during live polling:

```go
var globalLogsCache = &logsCache{
    data: make(map[string]*LogsResponse),
    ttl:  2 * time.Second,
}
```

Cache hits include `X-Cache: HIT` header; misses include `X-Cache: MISS`.

## Related

- [App Logs](#app-logs) - Application log system (ring buffer + DB, documented below)
- [[Configuration]] - Runtime configuration including `log_retention` and `stale_request_timeout`
- [[API Reference]] - SSE event system for real-time updates
