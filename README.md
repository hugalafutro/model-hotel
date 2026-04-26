<p align="center">
  <img src="docs/logo.png" width="280" height="64" alt="ModelHotel">
</p>

<p align="center"><strong>Multi-Provider AI Gateway</strong></p>

<p align="center"><em>"Because we have LiteLLM at home"</em></p>

> **AI-Assisted Project Disclaimer**
>
> This project was created with assistance from multiple AI models:
> - **GLM-5.1** (mostly design and planning / backend)
> - **Kimi-K2.6** (mostly UX / theming / frontend)
> - **Minimax-M2.7** (small code / style adjustments)
>
> Development was done in [Zed](https://zed.dev) editor and/or [Opencode](https://opencode.ai), with extensive human testing and iterative refinement.
>
> Human judgment applied at every stage, particularly around architectural decisions, UX flows, and quality control.

---

A single OpenAI-compatible endpoint that sits in front of all your LLM providers. Route requests to the cheapest or fastest model, fail over automatically when a provider goes down, and see exactly where your tokens are going.

## [<img src="docs/icons/screenshots.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Screenshots](#-screenshots)

| | | | | |
|:---:|:---:|:---:|:---:|:---:|
| ![Dashboard](docs/screenshots/placeholder-1.png) | ![Providers](docs/screenshots/placeholder-2.png) | ![Models](docs/screenshots/placeholder-3.png) | ![Logs](docs/screenshots/placeholder-4.png) | ![Failover](docs/screenshots/placeholder-5.png) |
| ![Settings](docs/screenshots/placeholder-6.png) | ![Virtual Keys](docs/screenshots/placeholder-7.png) | ![Stats](docs/screenshots/placeholder-8.png) | ![Discovery](docs/screenshots/placeholder-9.png) | ![Proxy Usage](docs/screenshots/placeholder-10.png) |

## What It Does

### [<img src="docs/icons/providers.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> One Endpoint, Many Providers](#-one-endpoint-many-providers)
Add any OpenAI-compatible provider ([OpenAI](https://openai.com/), [Anthropic](https://claude.ai/), [Groq](https://groq.com/), [DeepSeek](https://deepseek.com/), [NanoGPT](https://docs.nano-gpt.com/), [Z.AI](https://z.ai/), [Ollama](https://github.com/ollama/ollama), or your own), and call them all through the same `/v1/chat/completions` endpoint. The proxy handles model ID mapping, parameter filtering, and vision payload normalization transparently. Provider API keys are encrypted with AES-256-GCM at rest using your `MASTER_KEY`; only the proxy ever sees the decrypted credentials.

### [<img src="docs/icons/failover.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Transparent Failover](#-transparent-failover)
When a provider returns a 5xx or times out, the request is automatically retried with the next available provider for that model. Failover decisions happen at the response-header layer, so the client never receives a partial stream from a dead provider. Failed attempts are logged with full context (attempt number, error code, duration up to the failure point), making it easy to identify flaky providers in the Logs view.

### [<img src="docs/icons/hotel.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Hotel Routing](#-hotel-routing)
Prefix any model with `hotel/` to route through a curated pool of providers for the same base model, sorted by your preference. Example: `hotel/[llama-3.3-70b](https://build.nvidia.com/meta/llama-3_3-70b-instruct)` resolves to all providers that expose `meta-llama/llama-3.3-70b` or similar, then tries them in the order you configured. If the first is down or slow, the next takes over instantly. The failover group is auto-generated when models are discovered, but you can manually edit priorities and disable individual entries.

### [<img src="docs/icons/virtualkeys.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Per-Client Virtual Keys](#-per-client-virtual-keys)
Issue separate API keys for different users or services. Each key is SHA-256 hashed before storage, so raw keys are never persisted. Track token usage per key, revoke access instantly, and never expose your real provider credentials. Keys can be created and revoked from the dashboard or the admin API.

### [<img src="docs/icons/logging.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Request Logging with Overhead Breakdown](#-request-logging-with-overhead-breakdown)
Every request is logged with full latency decomposition:
- **TTFT** (time to first token)
- **Total duration** (end-to-end wall time)
- **Proxy overhead** split into parsing, model lookup, provider lookup, and key decryption
- **Tokens per second**, prompt / completion counts, and cache hit/miss stats

Streaming requests are captured as they start and updated as they finish, so you can see in-flight requests in the Logs view. The overhead breakdown helps you determine whether latency is coming from your provider or from the proxy itself.

### [<img src="docs/icons/discovery.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Built-In Model Discovery](#-built-in-model-discovery)
Add a provider and the service pulls the model list automatically via the provider's own API. Models are kept in sync on a schedule you control (default every 6 hours, configurable). DeepSeek and NanoGPT get rich metadata (context length, pricing, reasoning flags) pulled from dedicated catalogs rather than generic discovery.

### [<img src="docs/icons/health.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Provider Health at a Glance](#-provider-health-at-a-glance)
Test any model directly from the dashboard with a single click. The test sends a minimal chat completion through the proxy and reports TTFT, total duration, and the actual model response, so you know the provider is alive and responsive. DeepSeek and NanoGPT providers also show live account balance / usage data fetched from their respective APIs.

## [<img src="docs/icons/quickstart.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Quick Start (Docker Compose)](#-quick-start-docker-compose)

```bash
git clone <repository-url>
cd llm-proxy

cp .env.example .env
nano .env          # set a strong MASTER_KEY

docker compose up --build
```

The admin token is displayed once in the logs on first run and will never be shown again:

```bash
docker compose logs app | grep "ADMIN_TOKEN="
```

If you lose the token, delete `data/admin-token` and restart to generate a new one.

Open `http://localhost:8081`, log in with that token, add your first provider, and start proxying.

## [<img src="docs/icons/settings.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Configuration](#-configuration)

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MASTER_KEY` | Yes | - | Master encryption key for provider API keys |
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |
| `PORT` | No | `:8080` | Server listen address |
| `DISCOVERY_INTERVAL` | No | `30m` | Model auto-discovery interval |
| `DATA_DIR` | No | `./data` | Directory for admin token file |
| `ALLOW_HTTP_PROVIDERS` | No | `false` | Allow HTTP provider URLs |
| `RATE_LIMIT_ENABLED` | No | `true` | Hard kill-switch for rate limiting (env var only) |
| `MAX_REQUEST_SIZE` | No | `10485760` | Max request body in bytes (10MB) |
| `CORS_ORIGINS` | No | `localhost` | Allowed CORS origins |
| `ALLOWED_PROVIDER_HOSTS` | No | - | Additional allowed provider hosts |

> **Rate Limiting** — When `RATE_LIMIT_ENABLED=true` (the default), rate limiting can be toggled on/off at runtime via the **Settings** UI and the following DB-backed settings: `rate_limit_enabled` (bool, default `true`), `rate_limit_rps` (float, default `10` — set to `0` for unlimited), and `rate_limit_burst` (int, default `20`). Setting `RATE_LIMIT_ENABLED=false` in the environment completely disables rate limiting regardless of DB settings. Each virtual key gets its own independent token bucket; 429 responses include `Retry-After` and `X-RateLimit-*` headers.


## [<img src="docs/icons/api.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> API Endpoints](#-api-endpoints)

**Proxy API** (`/v1/*`) - OpenAI-compatible, requires a virtual key:

```bash
export PROXY_KEY="your-proxy-key"

curl http://localhost:8081/v1/models \
  -H "Authorization: Bearer $PROXY_KEY"

curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "hotel/gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

**Admin API** (`/api/*`) - requires the admin token for management operations.

## [<img src="docs/icons/security.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Security](#-security)

Provider API keys are encrypted at rest with AES-256-GCM using your `MASTER_KEY`. Virtual keys are SHA-256 hashed. The admin token is SHA-256 hashed before storage — the plaintext token is displayed once on first run and never stored on disk. To regenerate a lost token, delete the `admin-token` file in your configured `DATA_DIR` and restart. Standard security headers (X-Content-Type-Options, X-Frame-Options, X-XSS-Protection) are applied to all responses.

## [<img src="docs/icons/privacy.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Privacy & Data Handling](#-privacy--data-handling)

> **Prompts and request content are never captured, logged, or inspected.**
> The proxy forwards requests to the provider exactly as received, without reading or modifying message contents.
>
> The only information recorded is what is strictly necessary to route and meter the request:
> timestamp, time-to-first-token (TTFT), token counts, proxy overhead breakdown, virtual key identifier, and target provider.


## [<img src="docs/icons/license.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> License](#-license)

[MIT](LICENSE)
