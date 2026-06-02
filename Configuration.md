# Configuration

Model Hotel is configured through **environment variables** (startup-only) and **runtime database settings** (changeable without restart).

## Environment Variables

These are read once at startup and cannot be changed at runtime.

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MASTER_KEY` | Yes | - | Master encryption key for provider API keys. Used as input to Argon2id key derivation before AES-256-GCM encryption. **Must be strong and kept secret.** |
| `DATABASE_URL` | Yes | - | PostgreSQL connection string (e.g. `postgres://user:pass@localhost:5432/modelhotel`) |
| `PORT` | No | `:8080` | Server listen address |
| `DATA_DIR` | No | `./data` | Directory for the admin token file |
| `ADMIN_TOKEN` | No | *(auto-generated)* | Fixed admin token. Auto-generated on first run if empty, displayed once in the logs, then stored as a SHA-256 hash in `<DATA_DIR>/admin-token`. Regenerate by deleting that file and restarting. |
| `ALLOW_HTTP_PROVIDERS` | No | `false` | Allow HTTP (non-HTTPS) provider base URLs. Useful for local Ollama instances or testing with mock servers. |
| `RATE_LIMIT_ENABLED` | No | `true` | **Hard kill-switch** for rate limiting. When set to `false`, the rate-limiting middleware becomes a complete no-op: no buckets are created, no headers are set, no 429 responses are ever sent. Cannot be overridden at runtime. |
| `MAX_REQUEST_SIZE` | No | `10485760` | Maximum request body size in bytes (default 10 MB) |
| `CORS_ORIGINS` | No | `http://localhost:5173,http://localhost:8081` | Comma-separated list of allowed CORS origins. Must include the scheme (e.g. `http://`). |
| `ALLOWED_PROVIDER_HOSTS` | No | *(empty)* | Comma-separated list of additional allowed provider hosts. Built-in provider hosts (`api.openai.com`, `api.nano-gpt.com`, `api.z.ai`, `api.deepseek.com`, `api.anthropic.com`, `ollama.com`, `opencode.ai`, `api.x.ai`, `generativelanguage.googleapis.com`, `api.cohere.com`, `api.cohere.ai`, `openrouter.ai`) are **always** allowed regardless of this setting. Hosts listed here bypass URL-validation checks (loopback-address blocking and DNS-resolved loopback detection) so `localhost` can be added for local Ollama or testing. They do **not** bypass SafeDialer private-IP blocking at the TCP level (use `KNOWN_PROXIES` for that). |
| `RATE_LIMIT_IP_RPS` | No | `30` | Per-IP requests per second (DoS safety net; always-on, not DB-configurable). |
| `RATE_LIMIT_IP_BURST` | No | `60` | Per-IP burst size for DoS protection token bucket. |
| `DATABASE_MAX_CONNS` | No | `25` | Maximum database connection pool size. |
| `DATABASE_MIN_CONNS` | No | `5` | Minimum database connection pool size. |
| `MODELSDEV_ENABLED` | No | `true` | Enable models.dev enrichment for auto-discovering model metadata (pricing, context window, capabilities). |
| `DEBUG_LOG` | No | `false` | Enable structured debug logging via `internal/debuglog`. |
| `TRUSTED_PROXIES` | No | *(empty)* | Comma-separated CIDR ranges for trusted reverse proxies (e.g. `10.0.0.0/8,172.16.0.0/12`). When set, `X-Forwarded-For` headers from these IPs are trusted for rate limiting and request logging. This controls inbound trust only; it is unrelated to outbound SSRF protection (see `KNOWN_PROXIES`). |
| `KNOWN_PROXIES` | No | *(empty)* | Comma-separated CIDR ranges for internal LLM servers on private networks (e.g. `10.0.0.0/8,192.168.1.0/24`). IPs within these CIDRs bypass the SSRF protection (SafeDialer private-IP blocking) so the proxy can reach self-hosted providers like Ollama or KoboldCPP running on private subnets, while still blocking all other private/loopback addresses. Unlike `ALLOWED_PROVIDER_HOSTS` (which allows by hostname and bypasses all SSRF checks), this operates at the network/CIDR level and only bypasses the private-IP block. |
| `WEBAUTHN_RP_ID` | No | *(empty)* | Relying Party ID for WebAuthn/FIDO2 passkey authentication (typically your domain, e.g. `example.com`). When empty, passkey login is disabled. When set, users can register and log in with passkeys (Touch ID, Windows Hello, YubiKey, etc.) alongside the admin token. |
| `WEBAUTHN_RP_DISPLAY_NAME` | No | `Model Hotel` | Display name for the WebAuthn relying party, shown in the browser's passkey dialog. |
| `WEBAUTHN_RP_ORIGINS` | No | *(falls back to `CORS_ORIGINS`)* | Comma-separated list of allowed origins for WebAuthn registration/authentication (e.g. `https://example.com`). Falls back to `CORS_ORIGINS` if empty, then to `http://localhost:<port>`. |
| `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_HOST`, `POSTGRES_DB` | No | *(empty)* | Fallback env vars for constructing `DATABASE_URL` when it is not set directly. If `DATABASE_URL` is empty, the connection string is built as `postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@$POSTGRES_HOST/$POSTGRES_DB`. |

### Notes

- `MASTER_KEY` is **never used directly** as an AES key. It is fed through Argon2id key derivation (per-provider random salt in v2) to produce the 256-bit AES key. See [Security](Security) for details.
- `ADMIN_TOKEN` is stored as a SHA-256 hash. Legacy plaintext tokens are automatically migrated to hashed format on first validation. See [Security](Security).
- `RATE_LIMIT_ENABLED=false` completely removes the rate-limiting middleware from the request pipeline: it is not merely "disabled", it is a hard kill-switch.
- `ALLOWED_PROVIDER_HOSTS` is primarily for permitting non-standard hosts (loopback addresses for Ollama, custom provider endpoints). Built-in provider hosts never need to be listed here. Hosts listed here bypass URL-level validation (loopback blocking and DNS-resolved loopback detection) but do **not** bypass SafeDialer private-IP checks at the TCP level — use `KNOWN_PROXIES` for that.
- `KNOWN_PROXIES` permits entire CIDR ranges and only bypasses the private-IP block in the SafeDialer (provider URL validation still applies). If your internal LLM server has a stable hostname, use `ALLOWED_PROVIDER_HOSTS`. If it sits on a subnet with dynamic hostnames, use `KNOWN_PROXIES`.
- `TRUSTED_PROXIES` controls which reverse proxies are trusted for inbound request metadata (`X-Forwarded-For`). `KNOWN_PROXIES` controls which private CIDRs are allowed for outbound connections to self-hosted LLM providers. They serve opposite directions and should not be confused.
- `POSTGRES_*` env vars are a convenience for Docker Compose setups where `DATABASE_URL` is not set directly. If `DATABASE_URL` is provided, these vars are ignored.
- Self-hosted providers detected via port heuristics (KoboldCPP on 5001, LMStudio on 1234, local Ollama on 11434) are not in the built-in host allowlist; add them to `ALLOWED_PROVIDER_HOSTS` or `KNOWN_PROXIES` as needed.

## Database Settings

These settings are stored in the `settings` table and can be changed at runtime via the **Settings** UI or the `PUT /api/settings` endpoint; no restart required. Changes take effect immediately (within 30 seconds of cache TTL at most, or instantly via the subscription notification system).

| Setting | Default | Description |
|---------|---------|-------------|
| `discovery_interval` | `6h` | Model auto-discovery interval (e.g. `30m`, `1h`, `6h`, `24h`). Set to `0` to disable periodic discovery entirely. |
| `discovery_on_startup` | `true` | Whether to run model discovery automatically on server startup. If the last discovery was within 5 minutes, startup discovery is skipped to avoid duplicate work on rapid restarts. |
| `discovery_on_provider_create` | `true` | Whether to trigger model discovery when a new provider is created via the API. |
| `log_retention` | *(empty)* | How long to keep request logs. Accepts `1h`, `1d`/`24h`, `1w`/`168h`, `1m`/`720h`. Empty or unrecognised values = keep forever. Cleanup runs hourly. |
| `stale_request_timeout` | `30m` | Timeout for marking in-progress request logs as failed. Rows stuck in `pending` or `streaming` state for longer than this duration are automatically marked as `failed`. |
| `failover_on_rate_limit` | `true` | Whether to failover to the next provider when an upstream returns HTTP 429 (rate limited). 5xx errors always trigger failover regardless of this setting. |
| `rate_limit_enabled` | `true` | Runtime toggle for rate limiting. Overridden by the `RATE_LIMIT_ENABLED` env var: if the env var is `false`, this setting has no effect. |
| `rate_limit_rps` | `10` | Requests per second per virtual key. Set to `0` to disable rate limiting for all keys (makes every bucket unlimited). |
| `rate_limit_burst` | `20` | Maximum burst bucket size per virtual key. |
| `request_timeout` | `1m0s` | Timeout for upstream proxy requests (e.g. `30s`, `1m0s`, `2m0s`). |
| `circuit_breaker_enabled` | `true` | Enable circuit breaker for failover groups. When a provider fails repeatedly, the circuit opens and requests skip it until the cooldown expires. |
| `circuit_breaker_threshold` | `5` | Number of consecutive failures before the circuit breaker opens (1-100). |
| `circuit_breaker_cooldown` | `1m0s` | Duration the circuit breaker stays open before allowing a half-open retry (e.g. `30s`, `1m0s`, `5m0s`). |
| `rate_limit_ip_enabled` | `true` | Runtime toggle for per-IP rate limiting. Overridden by the `RATE_LIMIT_ENABLED` env var. |
| `rate_limit_ip_rps` | `30` | Per-IP requests per second. |
| `rate_limit_ip_burst` | `60` | Per-IP burst size for the token bucket. |
| `rate_limit_max_wait_ms` | `200` | Maximum time (ms) a rate-limited request waits for a token before returning 429 (0-10000). |
| `key_cache_ttl` | `10m0s` | How long decrypted provider API keys are cached in memory (e.g. `5m0s`, `10m0s`, `30m0s`). Shorter values improve key rotation responsiveness; longer values reduce Argon2id overhead. |
| `theme` | `dark` | UI theme: `dark` or `light`. |
| `ui_style` | *(empty)* | UI style preset: `cyber-terminal`, `glassmorphism-lite`, or empty for default. |
| `accent_color` | `#1dd1a1` | Primary accent color for the UI (hex color string). |
| `dashboard_refresh` | *(empty)* | Dashboard auto-refresh interval. Accepts predefined duration options. |
| `quota_refresh` | *(empty)* | Provider quota refresh interval. Accepts predefined duration options. |
| `history_limit` | *(empty)* | History display limit for log viewers. |
| `toast_duration` | *(empty)* | Toast notification duration in milliseconds (min: 1000, max: 15000). |
| `ttft_timeout` | `60s` | Time-to-first-token probe timeout for streaming requests (e.g. `30s`, `60s`). After the upstream responds 200, the proxy reads ahead to confirm the first token arrives before committing the stream to the client. If the provider fails to produce a token within this timeout, the request fails over to the next provider. Set to `0s` to disable the probe (immediate stream commit, backward-compatible behavior). |
| `stream_stall_timeout` | `30s` | Maximum silence during streaming before the connection is terminated and the circuit breaker records a failure (e.g. `10s`, `30s`, `1m0s`). After 50 chunks the effective timeout is multiplied by 3 to tolerate tool-call pauses and long reasoning chains. Set to `0s` to disable the stall watchdog. |

### Rate Limiting Details

The rate limiting system uses a token bucket per virtual key (backed by `golang.org/x/time/rate`):

- Each key gets its own independent bucket
- `rate_limit_rps` controls the refill rate (tokens per second)
- `rate_limit_burst` controls the maximum bucket size
- Setting `rate_limit_rps=0` makes every bucket unlimited (no rate limiting at all)
- The `RATE_LIMIT_ENABLED` environment variable is a **hard kill-switch**: when `false`, the middleware is a complete no-op (no buckets, no headers, no 429s)
- When rate limiting is re-enabled after being disabled, all existing buckets are reset to ensure fresh state
- Unused buckets are cleaned up after 10 minutes of inactivity

When a request is rate-limited, the response includes:
- `Retry-After: <seconds>`: When the client can retry
- `X-RateLimit-Limit: <rate>`: The refill rate
- `X-RateLimit-Remaining: <tokens>`: Remaining tokens in the bucket
- `X-RateLimit-Burst: <burst>`: The burst capacity

## Frontend Settings

User preferences are stored in `localStorage` (client-side only, never sent to the server):

| Key | Description |
|-----|-------------|
| `adminToken` | Admin authentication token (used for API calls) |
| `theme` | dark/light |
| `accentColor` | Hex color string |
| `uiStyle` | cyber-terminal, glassmorphism-lite, or default |
| `toastPosition` | Toast notification position |
| `toastTimeout` | Toast display duration (ms) |
| `persistChat` | Whether to persist chat state across sessions |
| `persistConversation` | Whether to persist conversation state |
| `persistArena` | Whether to persist arena state and history |
| `sidebarChatSubMode` | chat/conversation |
| `sidebarArenaSubMode` | competition/compare |
| `arenaHistoryEnabled` | Whether to persist arena battle history across sessions |
| `arenaHistoryLimit` | Maximum number of arena history entries to keep (default: 25) |
| `sidebarLogsSubMode` | request/app |

## Docker Compose

The included `docker-compose.yml` (production) sets up:

- **app** service: The Model Hotel server
  - Port `8081` mapped to container `8080`
  - Volume mount for `.data` (persistent admin token storage)
  - Optional Docker socket mount for container stats (`/var/run/docker.sock`)
- **db** service: PostgreSQL 16
  - Port `5432` exposed for local development
  - Health check for readiness (`pg_isready`)
  - Named volume for persistent data

For local development, use the `compose.dev.yml` override which enables the Docker socket and debug logging:

```bash
docker compose -f docker-compose.yml -f compose.dev.yml up -d
```

### Quick Start

```bash
git clone https://github.com/hugalafutro/model-hotel.git
cd model-hotel

cp .env.example .env
nano .env          # set a strong MASTER_KEY and DATABASE_URL

docker compose -f docker-compose.yml -f compose.dev.yml up --build
```

Get the admin token:
```bash
docker compose -f docker-compose.yml -f compose.dev.yml logs app | grep "ADMIN_TOKEN="
```

If you lose the token, delete `.data/admin-token` and restart to generate a new one.

You can also set a fixed admin token via the `ADMIN_TOKEN` environment variable in your `.env` file.