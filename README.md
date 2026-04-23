<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 48 48" fill="none" style="vertical-align:middle;margin-right:8px;"><rect width="48" height="48" rx="10" fill="#0b0c0f"/><path d="M24 6L8 16v4h32v-4L24 6z" fill="#4f8cff" opacity="0.9"/><rect x="10" y="22" width="6" height="10" rx="1" fill="#4f8cff" opacity="0.7"/><rect x="21" y="22" width="6" height="10" rx="1" fill="#4f8cff" opacity="0.7"/><rect x="32" y="22" width="6" height="10" rx="1" fill="#4f8cff" opacity="0.7"/><rect x="8" y="34" width="32" height="4" rx="1" fill="#4f8cff" opacity="0.5"/><circle cx="24" cy="12" r="2" fill="#0b0c0f"/></svg> Model Hotel

> **AI-Assisted Project Disclaimer**
>
> This project was created with assistance from multiple AI models:
> - **GLM-5.1** (mostly design and planning / implementation)
> - **Kimi-K2.6** (mostly UX / theming / implementation)
> - **Minimax-M2.7** (implementation)
>
> Development was done in Zed editor and/or Opencode, with extensive human testing and iterative refinement.
>
> Human judgment applied at every stage, particularly around architectural decisions, UX flows, and quality control.

---

A single OpenAI-compatible endpoint that sits in front of all your LLM providers. Route requests to the cheapest or fastest model, fail over automatically when a provider goes down, and see exactly where your tokens are going.

## <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/></svg> Screenshots

| | | | | |
|:---:|:---:|:---:|:---:|:---:|
| ![Dashboard](docs/screenshots/placeholder-1.png) | ![Providers](docs/screenshots/placeholder-2.png) | ![Models](docs/screenshots/placeholder-3.png) | ![Logs](docs/screenshots/placeholder-4.png) | ![Failover](docs/screenshots/placeholder-5.png) |
| ![Settings](docs/screenshots/placeholder-6.png) | ![Virtual Keys](docs/screenshots/placeholder-7.png) | ![Stats](docs/screenshots/placeholder-8.png) | ![Discovery](docs/screenshots/placeholder-9.png) | ![Proxy Usage](docs/screenshots/placeholder-10.png) |

## What It Does

### <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M5 9.8a8.99 8.99 0 0 1 14 0"/><path d="M12 18v3"/><path d="M9 21h6"/></svg> One Endpoint, Many Providers
Add any OpenAI-compatible provider (OpenAI, Anthropic, Groq, DeepSeek, NanoGPT, Z.AI, Ollama, or your own), and call them all through the same `/v1/chat/completions` endpoint. The proxy handles model ID mapping, parameter filtering, and vision payload normalization transparently. Provider API keys are encrypted with AES-256-GCM at rest using your `MASTER_KEY`; only the proxy ever sees the decrypted credentials.

### <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M2 12h10"/><path d="M9 4v16"/><path d="m3 9 3 3-3 3"/><path d="M14 8V5.87c0-.47.12-.93.34-1.34l2.04-3.65a.98.98 0 0 1 1.72 0l2.04 3.65c.22.41.34.87.34 1.34V8"/><path d="M18 12v5.87c0 .47-.12.93-.34 1.34l-2.04 3.65a.98.98 0 0 1-1.72 0l-2.04-3.65A2.49 2.49 0 0 1 10 17.87V12"/><rect width="8" height="4" x="14" y="8" rx="1"/></svg> Transparent Failover
When a provider returns a 5xx or times out, the request is automatically retried with the next available provider for that model. Failover decisions happen at the response-header layer, so the client never receives a partial stream from a dead provider. Failed attempts are logged with full context (attempt number, error code, duration up to the failure point), making it easy to identify flaky providers in the Logs view.

### <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M2 22h20"/><path d="M4 22V9a3 3 0 0 1 3-3h10a3 3 0 0 1 3 3v13"/><path d="M9 22v-4h6v4"/><path d="M8 6h.01"/><path d="M16 6h.01"/><path d="M12 6h.01"/><path d="M12 10h.01"/><path d="M12 14h.01"/><path d="M16 10h.01"/><path d="M16 14h.01"/><path d="M8 10h.01"/><path d="M8 14h.01"/></svg> Hotel Routing
Prefix any model with `hotel/` to route through a curated pool of providers for the same base model, sorted by your preference. Example: `hotel/llama-3.3-70b` resolves to all providers that expose `meta-llama/llama-3.3-70b` or similar, then tries them in the order you configured. If the first is down or slow, the next takes over instantly. The failover group is auto-generated when models are discovered, but you can manually edit priorities and disable individual entries.

### <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><circle cx="15" cy="8" r="3"/><path d="M10 21h8a2 2 0 0 0 2-2c0-3.9-3.1-7-7-7h-2a7 7 0 0 0-7 7 2 2 0 0 0 2 2Z"/><path d="M7 8a5 5 0 0 0 5 5h0a5 5 0 0 0 5-5 5 5 0 0 0-10 0Z"/></svg> Per-Client Virtual Keys
Issue separate API keys for different users or services. Each key is SHA-256 hashed before storage, so raw keys are never persisted. Track token usage per key, revoke access instantly, and never expose your real provider credentials. Keys can be created and revoked from the dashboard or the admin API.

### <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M16 13H8"/><path d="M16 17H8"/><path d="M10 9H8"/></svg> Request Logging with Overhead Breakdown
Every request is logged with full latency decomposition:
- **TTFT** (time to first token)
- **Total duration** (end-to-end wall time)
- **Proxy overhead** split into parsing, model lookup, provider lookup, and key decryption
- **Tokens per second**, prompt / completion counts, and cache hit/miss stats

Streaming requests are captured as they start and updated as they finish, so you can see in-flight requests in the Logs view. The overhead breakdown helps you determine whether latency is coming from your provider or from the proxy itself.

### <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg> Built-In Model Discovery
Add a provider and the service pulls the model list automatically via the provider's own API. Models are kept in sync on a schedule you control (default every 6 hours, configurable). DeepSeek and NanoGPT get rich metadata (context length, pricing, reasoning flags) pulled from dedicated catalogs rather than generic discovery.

### <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M6 9H4.5a2.5 2.5 0 0 1 0-5H6"/><path d="M18 9h1.5a2.5 2.5 0 0 0 0-5H18"/><path d="M4 22h16"/><path d="M10 14.66V17c0 .55-.47.98-.97 1.21C7.85 18.75 7 20.24 7 22"/><path d="M14 14.66V17c0 .55.47.98.97 1.21C16.15 18.75 17 20.24 17 22"/><path d="M18 2H6v7a6 6 0 0 0 12 0V2Z"/></svg> Provider Health at a Glance
Test any model directly from the dashboard with a single click. The test sends a minimal chat completion through the proxy and reports TTFT, total duration, and the actual model response, so you know the provider is alive and responsive. DeepSeek and NanoGPT providers also show live account balance / usage data fetched from their respective APIs.

## <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" x2="12" y1="22.08" y2="12"/></svg> Quick Start (Docker Compose)

```bash
git clone <repository-url>
cd llm-proxy

cp .env.example .env
nano .env          # set a strong MASTER_KEY

docker compose up --build
```

The admin token is printed in the logs:

```bash
docker compose logs app | grep "Admin token"
```

Open `http://localhost:8081`, log in with that token, add your first provider, and start proxying.

## <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg> Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MASTER_KEY` | Yes | — | Master encryption key for provider API keys |
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `PORT` | No | `:8080` | Server listen address |
| `DISCOVERY_INTERVAL` | No | `30m` | Model auto-discovery interval |
| `DATA_DIR` | No | `./data` | Directory for admin token file |
| `ALLOW_HTTP_PROVIDERS` | No | `false` | Allow HTTP provider URLs |
| `RATE_LIMIT_ENABLED` | No | `true` | Enable rate limiting |
| `MAX_REQUEST_SIZE` | No | `10485760` | Max request body in bytes (10MB) |
| `CORS_ORIGINS` | No | `localhost` | Allowed CORS origins |
| `ALLOWED_PROVIDER_HOSTS` | No | — | Additional allowed provider hosts |

## <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg> API Endpoints

**Proxy API** (`/v1/*`) — OpenAI-compatible, requires a virtual key:

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

**Admin API** (`/api/*`) — requires the admin token for management operations.

## <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/></svg> Security

Provider API keys are encrypted at rest with AES-256-GCM using your `MASTER_KEY`. Virtual keys are SHA-256 hashed. The admin token is stored in your configured `DATA_DIR` and printed once on startup. Standard security headers (X-Content-Type-Options, X-Frame-Options, X-XSS-Protection) are applied to all responses.

## <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align:middle;margin-right:6px;"><path d="m16 16 3-8 3 8c-.87.65-1.92 1-3 1s-2.13-.35-3-1Z"/><path d="m2 16 3-8 3 8c-.87.65-1.92 1-3 1s-2.13-.35-3-1Z"/><path d="M7 21h10"/><path d="M12 3v18"/><path d="M3 7h2c2 0 5-1 7-2 2 1 5 2 7 2h2"/></svg> License

MIT