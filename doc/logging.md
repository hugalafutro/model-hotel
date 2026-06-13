# Logging & error-message conventions

Canonical, in-repo conventions for Model Hotel's logs and user-facing error
messages. (AGENTS.md is local-only, so the rules live here.) Background and the
full rollout are in `plans/logging-and-errors-overhaul.md`.

Two audiences, two channels:

- **Machine-readable** classification (`request_logs.error_kind`, the slog level
  and attrs) — for the dashboard, log collectors, and metrics. Never inferred
  from prose.
- **Human-readable** sentences (`error_message`, the slog message) — rendered
  *from* the classification, worded for people.

## 1. Error kinds (`internal/proxy/reqerror.go`)

Every proxied-request failure carries an `ErrorKind`. It is stored in
`request_logs.error_kind` (nullable; legacy rows are NULL) and exposed on the
API `LogEntry`. The frontend keys behavior off the kind, with substring matching
of the message kept only as a fallback for legacy NULL rows.

| Kind | Meaning | Terminal HTTP status |
|---|---|---|
| `client_disconnect` | caller hung up before we responded | **499** (client closed request) |
| `provider_error` | upstream non-2xx or transport failure | 502 |
| `provider_timeout` | TTFT probe / stall watchdog fired | 502 |
| `failover_timeout` | overall failover deadline expired | **504** |
| `retry_timeout` | param-strip retry deadline expired | **504** |
| `internal` | gateway-internal failure (e.g. request build) | 502 |

Rules:

- A client hangup is **never** a provider failure: 499, and it must not record a
  circuit-breaker failure or count against provider stats.
- The real provider/transport error is preserved (`reqError.Underlying`) even
  when a higher-level cause (disconnect, timeout) is the terminal one — wrap,
  don't replace.
- Attempt numbers are **1-based** in every human-facing string.

## 2. User-facing error messages

Applies to every `writeOpenAIError` (client response) and `failRequest`
(request-log `error_message`) site. The exhaustion path derives both from the
same `reqError` renderer so the client and the dashboard tell the same story.

Style:

1. Lowercase sentence fragments, no trailing period (OpenAI-API convention).
2. Order by causality: what failed → why → (optionally) what to try.
   e.g. `invalid model format: expected "provider/model" or "hotel/group"`.
3. Name the model/provider when known and safe.
4. **Never** echo prompt/request/response content or key material. Provider
   error bodies may contain prompt echoes — extract only the provider error
   `message` field and truncate (`reqError.Underlying` caps at 500 chars).
5. No internal jargon, no raw Go error prefixes (`context canceled`), no 0-based
   indices reaching users. ("param-strip retry" → "retry without unsupported
   parameters".)
6. One message per failure mode — no near-duplicates.

## 3. Debug logging (`internal/debuglog`)

`debuglog.{Debug,Info,Warn,Error}(msg, k, v, …)` wraps `log/slog`.

### Source prefix

Every message starts with a canonical source prefix, `"source: message"`, e.g.
`debuglog.Info("proxy: routing to provider", …)`. The App Logs pipeline parses
this prefix (`extractSource`) to tag the entry's source. Canonical sources:

`proxy`, `resolve`, `discovery`, `failover`, `provider`, `settings`, `backup`,
`webauthn`, `stats`, `system`, `db`, `admin`, `applogs`, `events`, `ratelimit`,
`keycache`, `docker`, `auth`, `model`, `virtual-keys`, `version`, `api`.

(The list is extensible — e.g. a future `frontdesk` binary adds its own source.)

### Levels

- **Debug** — per-request mechanics; only emitted when `DEBUG_LOG` is set.
- **Info** — lifecycle events and *normal* client behavior. **Client
  disconnects are Info**, not Warn — they are not our failure.
- **Warn** — degraded but self-healing: transient retry, breaker opening,
  stripped params, slow provider.
- **Error** — action needed or data lost: all candidates exhausted, DB write
  failed, decryption failed.

### Field names

Use the canonical key, never a synonym: `model`, `provider`, `provider_id`,
`attempt`, `error`, `status`, `duration_ms`, `kind`. (Don't introduce
`provider_name` where `provider` is meant.)

### Pairing rule

Any failure that records a request-log error should also emit one debuglog line
at the matching level carrying the full structured detail — including the
underlying provider error that the user-facing message may truncate.

## 4. Output format: `LOG_FORMAT`

`LOG_FORMAT` controls the **docker-logs (stdout/stderr)** surface; the App Logs
page (ring buffer + DB + SSE) is unaffected.

- unset / `text` (default): human-readable `TIME level=LEVEL source: message k=v …`.
- `json`: one JSON object per line — `time`, `level`, `source`, `msg`, plus each
  slog attr as its own field. For Fluent Bit / Vector / Promtail / Datadog and
  friends; no extra endpoint or dependency. Safe to ship off-box because the
  no-content rule guarantees no prompt data in any log line.

The switch lives in `debuglog.JSONFormat()` (read by `debuglog.Init` and
`api.NewAppSlogHandler`), so every binary that calls `Init` inherits it. The
stderr filter's level gate and source suppression are JSON-aware
(`parseJSONLogLine`), so behavior is identical in both formats.

## 5. No content, ever

Absolute: no prompt, request, or response content in any log line or error
message — only routing/metering/diagnostic metadata. This is what makes logs
safe to export to a collector.

## 6. Pending mechanical follow-up

The full audit/normalization of all ~440 existing `debuglog.*` call sites
against §3 (field-name unification, any remaining level adjustments) is tracked
as ongoing mechanical cleanup — the conventions above are the spec. The
structural pieces (kinds, message renderer, `LOG_FORMAT=json`, level fixes for
client-disconnect paths) are in place.
