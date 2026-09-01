# Logging & error-message conventions

Canonical, in-repo conventions for Model Hotel's logs and user-facing error
messages. (AGENTS.md is local-only, so the rules live here.) Background and the
full rollout are in `plans/logging-and-errors-overhaul.md`.

Two audiences, two channels:

- **Machine-readable** classification (`request_logs.error_kind`, the slog level
  and attrs) - for the dashboard, log collectors, and metrics. Never inferred
  from prose.
- **Human-readable** sentences (`error_message`, the slog message) - rendered
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
| `provider_saturated` | provider alive but at capacity (concurrency/RPM/TPM 429); retry in seconds | **429** + `Retry-After` (502 when `failover_exhaustion_status_429` is off) |
| `provider_quota_exhausted` | a usage window is spent (session/daily/weekly cap); retry after it resets. Fixed by time, unlike `provider_not_entitled` which a person fixes | **429** + `Retry-After` on the all-pinned up-front skip; 502 otherwise |
| `provider_timeout` | TTFT probe / stall watchdog fired | 502 |
| `failover_timeout` | overall failover deadline expired | **504** |
| `retry_timeout` | param-strip retry deadline expired | **504** |
| `hedge_superseded` | gateway abandoned this attempt; a faster hedged candidate won. Appears on the attempt's log line only — the winner serves the client, so it is never a request's terminal kind | 502 if it ever became terminal |
| `internal` | gateway-internal failure (e.g. request build) | 502 |
| `validation` | bad client request (malformed body, missing/unknown model) | 400/404 |
| `auth` | virtual key lacks access | 403 |

`failRequest` takes `kind` as a required argument, so no failure path can record
a request without classifying it (compile-time guardrail).

Rules:

- A client hangup is **never** a provider failure: 499, and it must not record a
  circuit-breaker failure or count against provider stats.
- The real provider/transport error is preserved (`reqError.Underlying`) even
  when a higher-level cause (disconnect, timeout) is the terminal one - wrap,
  don't replace.
- Attempt numbers are **1-based** in every human-facing string.

### The per-attempt trail (`request_logs.attempts`)

The flat columns describe the TERMINAL attempt only. `attempts` (JSONB, nullable,
migration 078) keeps one element per failover attempt, hedged probes, in-flight
busy skips and the saturation retry included, written once at terminal time by
`updateRequestLog` from the records the attempt paths open and close
(`internal/proxy/attempt_trail.go`). Each element carries `attempt` (the loop
index; `-1` for a candidate the breaker refused before any request),
`provider_id`, `provider`, `model`, `status` (upstream, after the MiniMax remap;
omitted when none), `error_kind`, `detail`, `phrase`, `duration_ms`, `ttft_ms`,
`hedged` and `breaker` (`charge`, `noop`, `success`, `alive`, `skipped`,
`disabled`: what the attempt did to the circuit).

`detail` is the one field that carries provider text, and it is fenced twice: at
most 160 runes of the already-sanitized body (`util.SanitizeLogBody`), passed
through the attempt's credential masker. A provider quoting the prompt back
cannot fit; a key cannot survive. The no-content rule of section 6 applies to it
unchanged.

Provider discovery and quota polling scrub the same way. The shared HTTP helpers in
`internal/provider/discovery.go` never receive the key as a value, so they read it back off the
request they are sending (the credential headers, and the credential-bearing query parameters one
family authenticates with) and mask it exactly, then by shape, then bound the text, in that order so a
key straddling the cut is still redacted whole. This covers upstream error bodies, retryable-status
bodies and transport errors that quote the URL, in what is logged, returned to the dashboard, and
stored as a provider's quota failure. The vendor-specific paths that talk to an upstream directly run
`util.MaskCredential` over its answer for the same reason.

`phrase` is what the daily phrase-staleness report reads
(`internal/proxy/phrase_staleness.go`): a rate-limit phrase-table entry that has
matched no attempt in 90 days, and was added more than 90 days ago, is named in a
`rate-limit phrases: entries unmatched inside the horizon` Warn line so a
provider that rewrote its error text is noticed inside a season rather than at
the next incident.

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
   error bodies may contain prompt echoes - extract only the provider error
   `message` field and truncate (`reqError.Underlying` caps at 500 chars).
5. No internal jargon, no raw Go error prefixes (`context canceled`), no 0-based
   indices reaching users. ("param-strip retry" → "retry without unsupported
   parameters".)
6. One message per failure mode - no near-duplicates.

## 3. Debug logging (`internal/debuglog`)

`debuglog.{Debug,Info,Warn,Error}(msg, k, v, …)` wraps `log/slog`.

### Source prefix

Every message starts with a source prefix, `"source: message"`, e.g.
`debuglog.Info("proxy: routing to provider", …)`. The App Logs pipeline parses
this prefix (`extractSource`) to tag the entry's source, and the App Logs source
filter is built from what the running binary actually emitted, so the set is open:
a new package adds its own source by prefixing its messages. Common ones are
`proxy`, `resolve`, `discovery`, `failover`, `provider`, `settings`, `db`,
`admin`, `api`, `ratelimit`, `alert`, `oidc`, `netguard` and `frontdesk`.

Only the prefixes in [Scoped debug](#scoped-debug-debug_log_scopes) can be named
in `DEBUG_LOG_SCOPES`; the rest emit nothing at Debug level.

### Levels

- **Debug** - per-request mechanics; only emitted when `DEBUG_LOG` is set (all
  scopes) or when the message's scope is listed in `DEBUG_LOG_SCOPES`.
- **Info** - lifecycle events and *normal* client behavior. **Client
  disconnects are Info**, not Warn - they are not our failure.
- **Warn** - degraded but self-healing: transient retry, breaker opening,
  stripped params, slow provider.
- **Error** - action needed or data lost: all candidates exhausted, DB write
  failed, decryption failed.

### Scoped debug (`DEBUG_LOG_SCOPES`)

`DEBUG_LOG` turns Debug on for *everything*, which floods stdout at any real RPS.
`DEBUG_LOG_SCOPES` instead enables Debug for **only** the listed source prefixes,
e.g. `DEBUG_LOG_SCOPES=failover,resolve`. These are the sources that emit Debug
records, and the whole of what the variable can act on:

`access`, `admin`, `admin-chat`, `adminauth`, `anthropic`, `api`, `audit`,
`configsync`, `db`, `discovery`, `failover`, `frontdesk`, `models.dev`,
`paramrewrite`, `proxy`, `quota`, `resolve`.

`proxy` is by far the most voluminous, followed by `frontdesk`, `resolve` and
`discovery`. Every other source (`ratelimit`, `settings`, `provider`, `netguard`
and the rest) logs at Info and above only, so naming one does nothing.
It is comma-separated, trimmed, and matched case-insensitively against the prefix
before the first `:` in each message. It is ignored when `DEBUG_LOG` is on (Debug
is already global). The parsed scope set is logged once at startup
(`debuglog: per-scope debug enabled`) so an operator can confirm it took effect.

Mechanism (`internal/debuglog`): the handler's level gate is lowered to Debug
whenever *any* Debug output is possible (global or scoped), and a
`scopeFilterHandler` wrapper then drops Debug records whose scope isn't enabled.
Filtering lives in the handler, not in `Debug()`, so `debuglog.Debug` always
reaches whatever handler is installed - callers that install their own slog
handler (e.g. tests, the app-log buffer) keep working unchanged. Non-Debug
records always pass through regardless of scope.

### Field names

Use the canonical key, never a synonym: `model`, `provider`, `provider_id`,
`attempt`, `error`, `status`, `duration_ms`, `kind`. (Don't introduce
`provider_name` where `provider` is meant.)

### Pairing rule

Any failure that records a request-log error should also emit one debuglog line
at the matching level carrying the full structured detail - including the
underlying provider error that the user-facing message may truncate.

## 4. Output format: `LOG_FORMAT`

`LOG_FORMAT` controls the **docker-logs (stdout/stderr)** surface; the App Logs
page (ring buffer + DB + SSE) is unaffected.

- unset / `text` (default): human-readable `TIME level=LEVEL source: message k=v …`.
- `json`: one JSON object per line - `time`, `level`, `source`, `msg`, plus each
  slog attr as its own field. For Fluent Bit / Vector / Promtail / Datadog and
  friends; no extra endpoint or dependency. Safe to ship off-box because the
  no-content rule guarantees no prompt data in any log line.

The switch lives in `debuglog.JSONFormat()` (read by `debuglog.Init` and
`api.NewAppSlogHandler`), so every binary that calls `Init` inherits it. The
stderr filter's level gate and source suppression are JSON-aware
(`parseJSONLogLine`), so behavior is identical in both formats.

One shape, everywhere: every JSON line is rendered by `debuglog.JSONLine`
(levels via `debuglog.LevelName`: `debug`/`info`/`warning`/`error`; source split
out of the `scope: message` prefix by `debuglog.SplitSource`; attrs collected by
`debuglog.AddJSONField`). The plain stdout handler (`debuglog.StdoutHandler`,
used by the dashboard until it installs the app-log handler and by Front Desk
throughout) and the dashboard's app-log handler both go through it, so a
collector never sees two record shapes from one process. The dashboard loads
`.env` and calls `debuglog.Init` before `config.Load` for the same reason:
config warnings come out in the configured format too.

Field values keep their JSON type where one exists - numbers and bools stay
numbers and bools, JSON-marshalable values are embedded as-is - so
`latency_ms` or `consecutive_failures` can be indexed numerically; durations
(`"1.5s"`), times (RFC 3339) and errors render as strings, and anything that
cannot marshal falls back to its textual form so no value is dropped. slog
groups expand into dotted keys (`http.client.status`).

Before this was unified, Front Desk's JSON lines had upper-case levels and the
`scope: ` prefix inside `msg` (slog's default handler), and the dashboard's
app-log handler stringified every value; a pipeline that matched on those
older shapes needs adjusting.

## 5. OTLP log export (`OTEL_EXPORTER_OTLP_*`)

In addition to the stdout surface (§4), the same structured records can be
**pushed** to an OpenTelemetry collector over OTLP. This is **logs only** — no
request tracing (spans) and no OTLP metrics; Prometheus (`/metrics`) remains the
metrics path.

- Enabled purely by environment, like `LOG_FORMAT`: set
  `OTEL_EXPORTER_OTLP_ENDPOINT` (or the logs-specific
  `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT`). When unset, nothing is wired up and there
  is zero overhead. See `otelexport.LogsEnabled()`.
- All standard `OTEL_EXPORTER_OTLP_*` variables apply (endpoint, headers, TLS,
  timeout). Transport defaults to **http/protobuf**; set
  `OTEL_EXPORTER_OTLP_PROTOCOL=grpc` (or the `_LOGS_` variant) to switch.
  `OTEL_SERVICE_NAME` defaults to `model-hotel` when not provided.
- Wiring (`cmd/server/main.go`): `otelexport.NewSlogHandler` builds an SDK
  `LoggerProvider` + batch processor + OTLP exporter and returns an `otelslog`
  bridge handler, which is fanned out alongside the app-log handler via
  `debuglog.NewFanout`. The batch processor is flushed on graceful shutdown.
- Level/scope parity: the bridge is wrapped in a level gate set to the app's log
  level, and `DEBUG_LOG_SCOPES` filtering is applied by `debuglog.SetHandler`
  around the whole fan-out — so OTLP receives exactly the same records as stdout.
  (The level gate is required: the OTel log SDK reports every level as enabled,
  so without it the fan-out would export DEBUG records even with `DEBUG_LOG` off.)
- Failure behavior: export errors (e.g. an unreachable collector) are reported by
  the OTel SDK's default error handler to **stderr**; the batch processor's queue
  is bounded and **drops** records on overflow rather than blocking the caller, so
  a down collector never stalls the log hot-path.
- Dependency note: the OTLP **log** SDK/exporters (`otel/sdk/log`, `otlplog*`,
  `otelslog`) are pre-1.0 (`v0.x`) — the newest OTel signal — so an OTel SDK
  upgrade may need code adjustment. The feature is opt-in, so this only matters
  when the env vars are set.
- Safe to export off-box for the same reason as §4: the no-content rule (§6)
  guarantees no prompt data in any record.

### Front Desk

Front Desk shares the pipeline (`debuglog.Init`, `LOG_FORMAT`, the OTLP bridge)
and, on top of its own diagnostics, mirrors every persisted control-plane event
(member up/down, syncs, holds, alerts, settings changes) into the log at the
level its severity implies (`frontdesk.logEvent`): `info`/`success` → INFO,
`warning` → WARN, `error`/`critical` → ERROR, with the Events-tab message as `msg` and
`event`, `event_id`, `member_id` plus the event metadata as fields. Without
this a healthy Front Desk logs almost nothing above DEBUG, which made the
export look dead.

## 6. No content, ever

Absolute: no prompt, request, or response content in any log line or error
message - only routing/metering/diagnostic metadata. This is what makes logs
safe to export to a collector.

## 7. Audit status

The `debuglog.*` call sites were audited against §3 (2026-06-13). The codebase
was already largely consistent; the only field-key fixes needed were
`providerID`→`provider_id` and `provider_name`→`provider`. The structural pieces
(kinds, message renderer, `LOG_FORMAT=json`, client-disconnect level fixes) are
in place. New code must follow the conventions above; keep them as the spec for
any future logging.
