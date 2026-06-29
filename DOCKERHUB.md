# Model Hotel

[github.com/hugalafutro/model-hotel](https://github.com/hugalafutro/model-hotel)

*"Because we have LiteLLM at home"*

**Multi-Provider AI Gateway**

![Github CI](https://github.com/hugalafutro/model-hotel/actions/workflows/ci.yml/badge.svg) ![Go Version](https://img.shields.io/github/go-mod/go-version/hugalafutro/model-hotel) ![Go Report](https://goreportcard.com/badge/github.com/hugalafutro/model-hotel) ![TypeScript](https://img.shields.io/badge/TypeScript-3178C6?logo=typescript&logoColor=white) ![React](https://img.shields.io/badge/React-61DAFB?logo=react&logoColor=black) ![PostgreSQL](https://img.shields.io/badge/PostgreSQL-4169E1?logo=postgresql&logoColor=white)  ![Coverage](https://codecov.io/github/hugalafutro/model-hotel/branch/master/graph/badge.svg) ![GitHub Repo stars](https://img.shields.io/github/stars/hugalafutro/model-hotel)


> **AI-Assisted Project Disclaimer:**
> Human judgment applied at every stage, particularly around architectural decisions, UX flows, and quality control.

A single OpenAI-compatible endpoint that sits in front of all your LLM providers. Models are auto-discovered the moment you add a provider and optionally on schedule; failover groups form automatically around shared model names and retry transparently when a provider goes down; no prompt data is ever stored.

> **Live demo:** poke around a real instance at [mh.site19.ddns.net](https://mh.site19.ddns.net) - rebuilds fresh every 30 minutes.

---

## One Endpoint, Many Providers

Add any OpenAI-compatible provider (Anthropic, DeepSeek, KoboldCPP, LMStudio, NanoGPT, OpenRouter, Z.AI, x.ai, Google AI Studio, Cohere, Ollama, Ollama Cloud, OpenCode Go, OpenCode Zen, OpenAI, or your own) and call them all through one API surface: `/v1/chat/completions` plus the multimodal endpoints below. The proxy handles model ID mapping and failover transparently. Provider keys are encrypted at rest with AES-256-GCM using your `MASTER_KEY`; only the proxy ever sees decrypted credentials. Keyless providers (OpenCode Zen free models, local Ollama) are also supported.

## Multimodal Endpoints

Beyond chat, the proxy serves `/v1/embeddings`, `/v1/images/generations`, `/v1/images/edits`, `/v1/images/variations`, `/v1/audio/speech` (TTS, binary or SSE), `/v1/audio/transcriptions`, and `/v1/audio/translations` (multipart STT) as transparent OpenAI-compatible pass-through. Only the `model` field is rewritten to the resolved upstream model; responses (JSON, SSE streams, binary audio) are forwarded verbatim. Every endpoint gets the same `hotel/` failover, virtual-key control, rate limiting, circuit breaker, and token metering as chat - and the same privacy guarantee: content (audio uploads, generated images, embedding inputs) is never inspected or logged.

## Transparent Failover

Requests that fail (server errors, rate limits, auth issues, request and TTFT-probe timeouts) are automatically retried on the next available provider. For streaming, a **TTFT probe** confirms the first token arrives before committing the stream to your client; if none arrives within the timeout (default 60s), it fails over. Once streaming begins, a **stall watchdog** terminates the connection and records a breaker failure if no data arrives within the window (default 30s); after 50 chunks the threshold triples to tolerate tool-call pauses and long reasoning. Both timeouts are configurable in **Settings → Proxy** (`0s` to disable), with retries paced by exponential backoff and jitter.

## Hotel Routing

Prefix a model with `hotel/` to use its failover group: `hotel/gpt-4o` resolves to every provider offering `gpt-4o`, tried in priority order. Groups form automatically when 2+ providers share a model name (auto groups show an "auto" badge and vanish below 2 providers); manual groups persist regardless. Entries toggle on/off, priorities survive syncs, and stale entries are pruned when a model is deleted or unlisted. The UI shows each entry's *effective* state - those whose model or provider is disabled are greyed out, since the router skips them regardless of the toggle. Sync from the dashboard or via `POST /api/failover-groups/sync`.

Provider health is tracked with a per-provider **circuit breaker**: after a configurable number of consecutive failures (default 5) the circuit moves to **Open** and requests skip that provider; after a cooldown (default 60s) a single **HalfOpen** probe is allowed, closing the circuit on success or resetting the cooldown on failure. State transitions broadcast as SSE events, and the breaker can be disabled in Settings. See the [Failover and Hotel Routing wiki](https://github.com/hugalafutro/model-hotel/wiki/Failover-and-Hotel-Routing) for the full breakdown.

## Per-Client Virtual Keys

Issue separate API keys for different users or services, each SHA-256 hashed before storage so raw keys are never persisted. Track token usage per key, set per-key rate limits (requests/sec and burst) plus an optional tokens-per-minute (TPM) cap, restrict which providers a key may reach, and delete a key to cut off access instantly - all without exposing your real provider credentials. Create and delete keys from the dashboard or the admin API.

## No Prompts Logged

**User prompts and request content are never captured, logged, or inspected.** The proxy forwards requests to the provider exactly as received, without reading or modifying message contents.

The only information recorded is what is strictly necessary to route and meter the request: timing (duration, latency, TTFT), token counts (with cache-hit/miss breakdown) and tokens/sec, HTTP status, upstream error messages (provider failures only, never user content), the proxy overhead breakdown, failover attempt count, resolved model ID, request state, endpoint family, virtual key, and target provider/model identifiers. This applies to the multimodal endpoints too: audio uploads, generated images/audio, and embedding vectors pass through byte-for-byte, never read or retained.

The optional **Arena History** feature (disabled by default, **Settings → Arena History**) can persist completed arena and compare results in your browser's local storage: model-generated responses (output, thinking blocks, metrics) plus preset prompts and personas saved by reference (built-in IDs only). Custom text you type yourself is never logged - only the fact that a custom prompt was used (shown as "Custom prompt"). History never leaves your browser and can be cleared anytime from Settings.

## Request Logging with Overhead Breakdown

Every request is logged with full latency decomposition:

- **TTFT** (time to first token, measured by the streaming probe)
- **Total duration** (end-to-end wall time)
- **Proxy overhead** split into request parsing, model/failover lookup, provider lookup, and key decryption
- **Tokens per second**, prompt / completion counts

Streaming requests are captured as they start and updated as they finish, so you can see in-flight requests in the Logs view. The overhead breakdown helps you determine whether latency is coming from your provider or from the proxy itself.

## Built-In Model Discovery

Add a provider and the service pulls its model list automatically via the provider's own API, kept in sync on a schedule you control (default every 6 hours). Models that disappear from a listing are disabled (never deleted) and return automatically if re-listed; manual disables are always respected. After a manual scan, a summary modal shows exactly what changed - models added, re-enabled, or disabled, plus any failover groups updated or deleted as a result - and discovery-disabled models carry a "not listed since…" tooltip so they're easy to tell apart from manual disables. The following providers get enriched metadata beyond the generic OpenAI-compatible endpoint:

| Provider | Context Length | Pricing | Reasoning Flags | Input/Output Modalities | Source |
|---|---|---|---|---|---|
| DeepSeek | ✅ | ✅ | ✅ | *(none)* | API (`/models`) + Catalog |
| NanoGPT | ✅ | ✅ | ✅ | ✅ | API (`/models?detailed=true`) |
| Z.AI | ✅ | *(none)* | ✅ | Derived | API (`/models`) + Catalog |
| OpenCode Go | ✅ | ✅ | ✅ | ✅ | API (`/models`) + Catalog |
| OpenCode Zen | ✅ | ✅ | ✅ | ✅ | API (`/models`) + Catalog |
| OpenAI | ✅ | ✅ | ✅ | ✅ | API (`/models`) + Catalog |
| OpenRouter | ✅ | ✅ | ✅ | ✅ | API (/models) |
| Anthropic | ✅ | ✅ | *(none)* | ✅ (partial) | API + Pricing catalog |
| xAI (Grok) | ✅ | ✅ | ✅ | ✅ | API (`/language-models`) + Catalog |
| Google AI Studio (Gemini) | ✅ | ✅ | ✅ | ✅ | API (`/v1beta/models`) + Pricing catalog |
| Cohere | ✅ | ✅ | ✅ | ✅ (vision) | API (`/v1/models`, paginated) + Pricing catalog |
| Ollama / Ollama Cloud | ✅ | *(none)* | ✅ | ✅ | API (`/api/show`) |

**Z.AI, xAI, OpenAI, DeepSeek, and OpenCode (Go & Zen) combine a live `/models` listing with a built-in catalog:** the API supplies the authoritative model list (plus live pricing and modalities for xAI) and the catalog backfills what the API omits (context window, max output, capability flags, pricing). For Z.AI, xAI, and OpenCode the catalog *also* surfaces working models the listing doesn't advertise - a freshly released GLM, or older Grok models xAI keeps callable. Live values always win; the catalog only fills gaps. xAI (403/429) and OpenCode Go (404) fall back to the pure catalog when the account or endpoint can't list; the others abort on error so a transient failure never disables existing models. Google AI Studio, Cohere, NanoGPT, and Anthropic use their native APIs (Cohere paginated; Google/Cohere/Anthropic adding a pricing catalog). Ollama and Ollama Cloud enrich via `/api/show`.

Models not covered by any built-in catalog are automatically enriched from [models.dev](https://models.dev/), an open-source catalogue of pricing, context limits, capabilities, and modality data for 40+ providers. Enrichment is non-destructive - it only fills empty fields, never overwriting populated data - making the full per-field precedence **live provider data → built-in catalog → models.dev → empty**, so a stale catalog can never mask fresh live data. If models.dev is unreachable, discovery proceeds using whatever the provider returned, so your existing catalogue is never at risk.

## Model Health at a Glance

Test any model from the Models page with a single click: it sends a minimal chat completion directly to the provider and reports total duration and the actual response, so you know the provider is alive. DeepSeek shows live account balance; NanoGPT and Z.AI show token quota and usage, all fetched from their APIs.

## Provider Quotas & Usage

For providers that expose it, click a provider's quota badge (card or sidebar panel) for a live usage breakdown without leaving the dashboard: OpenRouter (credit balance and per-key spend), Z.ai Coding Plan (5-hour, weekly, and MCP token quotas), NanoGPT (weekly token and daily image quotas with subscription details), NeuralWatt (energy-based quota with subscription and lifetime usage). Each modal toggles between used and remaining and refreshes on demand. DeepSeek (account balance) and Ollama Cloud (plan status) surface usage directly on their cards and sidebar badges.

## Themeable UI

Make the dashboard your own from Appearance settings: three UI styles - Clean SaaS (minimal, the default), Cyber Terminal (high-contrast), or Glassmorphism (translucent) - plus dark / light mode and an accent color (each style ships a tasteful default, or pick your own). Everything persists locally in the browser.

## Interactive Chat & Arena

The dashboard includes a built-in **Chat** interface for testing models interactively: system personas (preset or custom), generation parameters (temperature, top_p, max_tokens, min_p, top_k, frequency/presence penalties), and streaming with collapsible thinking-block rendering. Vision- and audio-capable models show upload buttons for image or audio input, sent as OpenAI-compatible multimodal content parts. Switch to **Conversation** mode to watch two models talk to each other: enter a starter prompt, set the rounds and optional delay between turns, and observe the back-and-forth with per-message metrics.

**Arena** mode offers two sub-modes: **Competition** runs bracket tournaments where models face off in pairwise matchups - vote for winners and the bracket auto-advances until a champion emerges. **Compare** places two or more models in a grid on the same prompt for parallel evaluation, with per-slot personas and voting. Both support per-model generation parameters, streaming with thinking-block rendering, and per-response metrics.

## Real-Time Events & System Status

A live SSE event bus pushes toast notifications for discovery outcomes, model disabling, token-counting errors, circuit-breaker transitions, and stale-request alerts to the dashboard. The sidebar polls system stats every 10 seconds - CPU, memory, disk I/O, and network throughput with color-coded warnings (orange at 75%, red at 90%), aggregated across containers under Docker Compose (otherwise cgroup metrics) - plus goroutine count, database health (size, connections, cache hit ratio), API uptime, and process count.

## Security & Privacy

Provider API keys are encrypted at rest with AES-256-GCM, the `MASTER_KEY` strengthened via **Argon2id** (per-provider random salts) before use as the AES key. Virtual keys and the admin token are SHA-256 hashed; the admin token's plaintext is shown once on first run and never stored (delete the `admin-token` file in `DATA_DIR` and restart to regenerate). Outbound provider connections are protected against SSRF and DNS rebinding: hostnames are resolved, private/loopback/link-local/cloud-metadata IPs blocked, then dialed by IP to close the rebinding TOCTOU gap; redirects are validated too. Use `KNOWN_PROXIES` to allow private CIDRs for internal LLM servers and `ALLOWED_PROVIDER_HOSTS` for specific hostnames. Standard security headers (X-Content-Type-Options, X-Frame-Options, Referrer-Policy, HSTS, CSP) are applied to all responses. Decrypted keys are cached in memory up to 10 minutes (`key_cache_ttl`). WebAuthn session tokens are SHA-256 hashed, never plaintext, 30-day TTL.

## Passkey Authentication

Log into the admin dashboard using a FIDO2/WebAuthn passkey (Touch ID, Windows Hello, YubiKey, etc.) instead of the admin token. Register passkeys from the Settings page and use them on the login screen alongside the traditional admin token.

Passkey login is disabled by default. Enable it with `WEBAUTHN_RP_ID` (your domain); `WEBAUTHN_RP_ORIGINS` (your origin URLs) falls back to `CORS_ORIGINS`, then `http://localhost:<port>`. Session tokens are SHA-256 hashed, never stored in plaintext, and expire after 30 days.

## Authenticator App (TOTP)

Add a time-based one-time code (TOTP, RFC 6238) from an authenticator app (Google Authenticator, Authy, 1Password, etc.) as a true second factor. Enable it from the Settings page: scan the QR code (or copy the secret), enter the 6-digit code, and save the one-time recovery codes.

When TOTP is on, the raw admin token no longer authenticates API requests by itself; it becomes a first factor that, combined with a valid code, is exchanged for a session token on the login screen (the same session infrastructure passkeys use). Recovery codes are single-use and stored as SHA-256 hashes; the TOTP secret is AES-256-GCM encrypted at rest with `MASTER_KEY`.

## Single Sign-On (OIDC)

Let admins sign in through an external OpenID Connect provider (Authentik, Authelia, Keycloak, Okta, Google, and so on). Configure the issuer URL, client ID, client secret, and an allowlist of verified emails from the Settings page; a "Sign in with SSO" button then appears on the login screen.

SSO is a third login path, not a replacement: it mints the same session token as passkey and TOTP login. The allowlist fails closed, only verified emails match, the client secret is AES-256-GCM encrypted at rest, and the flow uses PKCE plus single-use state and nonce. The minted token is delivered in the URL fragment (never sent back to the server); if your reverse proxy logs response headers, redact `Location` on `/api/auth/oidc/callback`. Each operator registers their own OIDC app and points it at `<public base URL>/api/auth/oidc/callback`. Local login (admin token, passkeys, TOTP) always keeps working, so a misconfigured provider cannot lock you out.

## High Availability

A single instance keeps its caches and rate limiters in memory, so to survive a host failure you run several instances behind one client endpoint. A **Front Desk** control plane holds the fleet roster and pushes configuration to every member, and **Traefik** load-balances them with health checks and automatic failover. Members share one `MASTER_KEY` (so encrypted provider keys port across the fleet) and each keeps its own admin token; a config change on any member syncs to the rest. See the [High Availability guide](https://github.com/hugalafutro/model-hotel/wiki/High-Availability).

## Quick Start

```bash
git clone https://github.com/hugalafutro/model-hotel.git
cd model-hotel

cp .env.example .env
nano .env          # set a strong MASTER_KEY and POSTGRES_PASSWORD

docker compose -f docker-compose.yml -f compose.dev.yml up --build -d
```

To use the prebuilt image instead of building from source, edit `docker-compose.yml`: comment out `build: .` and uncomment the `image:` line.

The admin token is shown once in the logs on first run and never again:

```bash
docker compose -f docker-compose.yml -f compose.dev.yml logs app | grep "ADMIN_TOKEN="
```

If you lose the token, delete `.data/admin-token` and restart to generate a new one.

You can also set a fixed admin token via the `ADMIN_TOKEN` environment variable.

Open `http://localhost:8081`, log in with that token, add your first provider, and start proxying.

> **Security:** The Docker socket is disabled by default; the `compose.dev.yml` override enables it for local development - only use it in trusted environments.

## Deploy without Git

No `git clone` needed. Create two files and go:

**1.** Create `.env` with your secrets:

```bash
# Generate strong secrets:
#   MASTER_KEY:       openssl rand -base64 32
#   POSTGRES_PASSWORD: openssl rand -hex 16
#   ADMIN_TOKEN:      openssl rand -hex 16   (optional; auto-generated if empty)

MASTER_KEY=<your-master-key>
POSTGRES_PASSWORD=<your-postgres-password>
ADMIN_TOKEN=

# Optional: WebAuthn/FIDO2 passkey login
# WEBAUTHN_RP_ID=your-domain.com
# WEBAUTHN_RP_ORIGINS=https://your-domain.com
```

**2.** Create a `docker-compose.yml`. Copy the ready-to-use file (app + PostgreSQL + all environment variables) from the [**Deploy without Git** quick start on GitHub »](https://github.com/hugalafutro/model-hotel#-deploy-without-git).

**3.** Deploy:

```bash
docker compose up -d
```

> **Note:** `WEBAUTHN_RP_ID` enables passkey login (empty to disable); `WEBAUTHN_RP_ORIGINS` falls back to `CORS_ORIGINS`. `TRUSTED_PROXIES` trusts inbound `X-Forwarded-For` headers from reverse proxies; `KNOWN_PROXIES` allows outbound connections to internal LLM servers on private networks (bypasses SSRF protection). See the [Configuration wiki](https://github.com/hugalafutro/model-hotel/wiki/Configuration) for details.

## API Example

```bash
# List available models
curl http://localhost:8081/v1/models \
  -H "Authorization: Bearer $VIRTUAL_KEY"

# Chat completion (with hotel routing for automatic failover)
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "hotel/gpt-4o", "messages": [{"role": "user", "content": "Hello!"}]}'

# Speech-to-text (multipart; multimodal endpoints share the same provider/model and hotel/ routing)
curl -X POST http://localhost:8081/v1/audio/transcriptions \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -F model="OpenAI/whisper-1" -F file=@speech.mp3
```

See the [API Reference](https://github.com/hugalafutro/model-hotel/wiki/API-Reference) for the full endpoint listing.

## Metrics & log shipping

A Prometheus endpoint is exposed at `/metrics` (request rates by provider/model/status, latency and TTFT histograms, token counters, failover attempts, per-provider circuit-breaker state, plus Go runtime metrics). It's authenticated - set a dedicated `METRICS_TOKEN` so your scrape config need not carry the admin token (which also works). No prompt content is ever exposed.

```yaml
# prometheus.yml
scrape_configs:
  - job_name: model-hotel
    authorization:
      credentials: "${METRICS_TOKEN}"
    static_configs:
      - targets: ["model-hotel:8080"]
```

For logs, set `LOG_FORMAT=json` to emit one structured JSON object per line on stdout for Fluent Bit / Vector / Promtail / Datadog - no extra endpoint, and (like everything here) never any prompt content. To **push** those logs to an OpenTelemetry collector, set `OTEL_EXPORTER_OTLP_ENDPOINT` (standard `OTEL_EXPORTER_OTLP_*` vars apply; http/protobuf by default, `OTEL_EXPORTER_OTLP_PROTOCOL=grpc` to switch) - logs only, no tracing. For verbose debug, `DEBUG_LOG=true` enables Debug everywhere; `DEBUG_LOG_SCOPES=failover,ratelimit` scopes it to those areas.

## Full Documentation

- [Configuration](https://github.com/hugalafutro/model-hotel/wiki/Configuration): Environment variables, runtime settings, Docker Compose
- [API Reference](https://github.com/hugalafutro/model-hotel/wiki/API-Reference): Proxy and admin endpoints
- [Security](https://github.com/hugalafutro/model-hotel/wiki/Security): AES-256-GCM encryption, Argon2id key derivation, hashing, URL validation
- [Privacy](https://github.com/hugalafutro/model-hotel/wiki/Privacy): What is and isn't captured, data retention, local deployment
- [Failover and Hotel Routing](https://github.com/hugalafutro/model-hotel/wiki/Failover-and-Hotel-Routing): Failover groups, circuit breaker, backoff
- [Model Discovery](https://github.com/hugalafutro/model-hotel/wiki/Model-Discovery): Automatic sync, provider-specific metadata, enrichment
- [Virtual Keys](https://github.com/hugalafutro/model-hotel/wiki/Virtual-Keys): Creating, using, and deleting client keys
- [Request Logging](https://github.com/hugalafutro/model-hotel/wiki/Request-Logging): Log fields, overhead breakdown, retention
- [Backup & Restore](#backup--restore): Creating backups, restoring, critical requirements
- [Development](https://github.com/hugalafutro/model-hotel/wiki/Development): Local setup, build commands, contributing
- [High Availability](https://github.com/hugalafutro/model-hotel/wiki/High-Availability): Front Desk control plane + Traefik for drop-in multi-instance HA

## Backup & Restore

Backups are created from the Settings page or the admin API (`POST /api/backups`) via `pg_dump --format=custom`. The `.dump` files contain all database tables: providers, models, virtual keys, failover groups, and settings.

### Restoring a backup

```bash
# Direct
pg_restore --clean --if-exists -d YOUR_DB backup_file.dump

# Via Docker
docker exec -i postgres-container pg_restore --clean --if-exists -U user -d dbname < backup_file.dump
```

### Critical requirements for a working restore

| Requirement | Details |
|---|---|
| **MASTER_KEY must match** | Provider keys are AES-256-GCM encrypted with a key derived from `MASTER_KEY` via Argon2id. Restoring with a different `MASTER_KEY` leaves all provider keys unrecoverable - the app starts, but key decryption fails. |
| **Admin token is not in the backup** | The admin token hash lives in `DATA_DIR/admin-token` on the filesystem, not the database. If lost, a new token is auto-generated on next boot - check startup logs. |
| **Virtual keys are irrecoverable** | Virtual keys are stored as SHA-256 hashes only; plaintext is never persisted. If you lose the plaintext keys, they cannot be recovered from the backup (by design). |

### What is and isn't in the backup

**Included** (in the database, captured by `pg_dump`): providers (encrypted keys, nonces, salts), models, virtual keys (hashes only), failover groups, settings.

**Not included** (filesystem only): `DATA_DIR/admin-token` (admin token hash), `DATA_DIR/backups/` (the backup files), `MASTER_KEY` (environment variable).

## Known Limitations

- **Single-instance only**: Caches and rate limiters are in-memory, not horizontally scalable within one instance. To run several instances behind one client endpoint with automatic failover, use the [Front Desk + Traefik HA stack](https://github.com/hugalafutro/model-hotel/wiki/High-Availability).

## License

[MIT](https://github.com/hugalafutro/model-hotel/blob/master/LICENSE). See [CONTRIBUTING.md](https://github.com/hugalafutro/model-hotel/blob/master/CONTRIBUTING.md) for the contributor license agreement.
