# Model Hotel

> **Multi-Provider AI Gateway**
>
> *"Because we have LiteLLM at home"*

Model Hotel is a single OpenAI-compatible endpoint that sits in front of all your LLM providers. Route requests to the cheapest or fastest model, fail over automatically when a provider goes down, and see exactly where your tokens are going.

## Quick Navigation

- [[Architecture]] — How the system is structured
- [[Providers]] — Adding and managing LLM providers
- [[Failover & Hotel Routing]] — Transparent failover and hotel-prefixed routing
- [[Virtual Keys]] — Per-client API key management
- [[Request Logging]] — Latency decomposition and overhead breakdown
- [[Model Discovery]] — Built-in provider model synchronization
- [[Provider Health]] — Testing and health monitoring
- [[Chat & Arena]] — Interactive testing and model comparison
- [[Real-Time Events & System Status]] — Live notifications and system monitoring
- [[Configuration]] — Environment variables and runtime settings
- [[API Reference]] — Proxy and admin API endpoints
- [[Security]] — Encryption, hashing, and security headers
- [[Privacy]] — Data handling and privacy guarantees
- [[Development]] — Local development setup and contributing

## What It Does

- **One Endpoint, Many Providers** — Add any OpenAI-compatible provider (OpenAI, Anthropic, Groq, DeepSeek, NanoGPT, Z.AI, Ollama, OpenCode, or your own)
- **Transparent Failover** — Automatic retries when a provider returns 5xx or times out
- **Hotel Routing** — Prefix models with `hotel/` to route through a curated provider pool
- **Per-Client Virtual Keys** — Issue separate API keys for different users or services
- **Request Logging with Overhead Breakdown** — Full latency decomposition (TTFT, parsing, lookup, decryption)
- **Built-In Model Discovery** — Automatic model list syncing with rich metadata
- **Interactive Chat & Arena** — Test models interactively and run side-by-side comparisons
- **Real-Time Events** — Live toast notifications and system status monitoring
- **Keyless Providers** — Support for providers that don't require API keys (e.g. OpenCode Zen)

## Tech Stack

| Layer | Technology |
|-------|------------|
| Backend | Go (Chi router, pgx PostgreSQL driver) |
| Frontend | React + TypeScript + Tailwind CSS + Vite |
| Database | PostgreSQL 16 |
| Containerization | Docker Compose |
| Real-Time | Server-Sent Events (SSE) |
| Encryption | AES-256-GCM (provider keys), SHA-256 (virtual keys, admin token) |

## License

[MIT License](https://github.com/user/llm-proxy/blob/master/LICENSE) — see [CONTRIBUTING.md](https://github.com/user/llm-proxy/blob/master/CONTRIBUTING.md) for the contributor license agreement.
```

```markdown
# Architecture

Model Hotel is a Go backend with a React frontend, communicating via REST APIs and Server-Sent Events (SSE).

## High-Level Architecture

```
┌─────────────────┐      HTTP/WebSocket      ┌──────────────────┐
│   React SPA     │ ◄──────────────────────► │   Go Backend     │
│  (Dashboard)    │   /api/* (admin)         │   (Chi Router)   │
│                 │   /v1/* (proxy)          │                  │
└─────────────────┘                          └────────┬─────────┘
                                                      │
                              ┌───────────────────────┼──────────┐
                              │                       │          │
                              ▼                       ▼          ▼
                        ┌──────────┐           ┌──────────┐  ┌─────────┐
                        │PostgreSQL│           │ LLM APIs │  │ Event   │
                        │  (Data)  │           │(Providers)│ │  Bus    │
                        └──────────┘           └──────────┘  └─────────┘
```

## Backend Structure

The Go backend is organized into internal packages:

| Package | Responsibility |
|---------|---------------|
| `cmd/server` | Entry point, HTTP server setup |
| `internal/admin` | Admin token generation and validation |
| `internal/api` | HTTP handlers (REST + SSE) |
| `internal/auth` | AES-256-GCM encryption, SHA-256 hashing, key cache |
| `internal/config` | Environment variable loading and validation |
| `internal/db` | PostgreSQL connection pool and migrations |
| `internal/events` | In-memory pub/sub event bus |
| `internal/failover` | Failover group logic and provider ordering |
| `internal/model` | Model repository |
| `internal/provider` | Provider repository and discovery |
| `internal/proxy` | OpenAI-compatible proxy endpoint |
| `internal/ratelimit` | Per-virtual-key token bucket rate limiting |
| `internal/settings` | DB-backed runtime settings with cache |
| `internal/util` | Utilities (Docker stats, network, formatting) |
| `internal/virtualkey` | Virtual key repository |

## Frontend Structure

The React frontend uses a component-based architecture:

| Directory | Contents |
|-----------|----------|
| `web/src/pages` | Top-level pages (Dashboard, Chat, Arena, Providers, Models, etc.) |
| `web/src/components` | Reusable UI components (ModelPicker, ModelReplyCard, DataTable, etc.) |
| `web/src/context` | React contexts (Theme, Toast, Event, Storage, SidebarMode) |
| `web/src/api` | API client and TypeScript types |
| `web/src/data` | Persona presets and arena prompts |
| `web/src/utils` | Formatting, thinking block extraction, model helpers |

## Database Schema

### Core Tables

- **`providers`** — Provider configuration (name, base URL, encrypted API key)
- **`models`** — Discovered models with capabilities, pricing, modalities
- **`model_failover_groups`** — Hotel routing groups with priority ordering
- **`request_logs`** — Request telemetry (latency, tokens, overhead, cache hits)
- **`virtual_keys`** — Per-client proxy keys with usage tracking
- **`proxy_keys`** — Deprecated (replaced by virtual_keys)
- **`settings`** — Key-value runtime configuration
- **`app_logs`** — Persisted application log output

### Key Relationships

- A **provider** has many **models**
- A **model** belongs to one **provider**
- A **failover group** references multiple **providers** via `priority_order` JSONB
- A **request log** references a **provider** and optionally a **virtual key**
- **Virtual keys** are independent; token usage is tracked per key

## Request Flow

1. Client sends request to `/v1/chat/completions` with a **virtual key**
2. Proxy extracts the model ID from the request body
3. If model starts with `hotel/`, resolve the failover group and pick the highest-priority enabled provider
4. Otherwise, look up the model directly to find its provider
5. Decrypt the provider's API key (cached for 5 minutes)
6. Forward the request to the provider with the real API key
7. Stream the response back to the client
8. If the provider fails (5xx or timeout), retry with the next provider in the failover group
9. Log the request with full timing decomposition

## Data Flow

```
Client Request
    │
    ▼
┌─────────────┐
│  Rate Limit │ ──► 429 if exceeded
│   Check     │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Model Look  │ ──► 404 if not found
│    Up       │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Hotel?    │
│   Routing   │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Decrypt   │
│  Provider   │
│    Key      │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Forward   │
│  to Provider│
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Stream    │
│   Response  │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│    Log      │
│   Request   │
└─────────────┘
```

## Frontend State Management

The frontend uses a combination of:

- **React Query** — Server state caching and synchronization
- **Local Storage** — Persisted user preferences (theme, accent color, sidebar sub-modes)
- **React Context** — Global UI state (theme, toasts, events, sidebar modes)
- **URL State** — Some filters and pagination

### Sidebar Sub-Modes

The sidebar supports sub-mode toggling for pages with multiple views:

| Page | Sub-Modes |
|------|-----------|
| Chat | Chat, Conversation |
| Arena | Arena (competition), Compare |
| Logs | Requests, App Logs |

Clicking a sidebar link navigates; clicking it again when already on that page toggles the sub-mode.
```

```markdown
# Providers

Providers are the backbone of Model Hotel. Each provider represents an external LLM API that the proxy can route requests to.

## Supported Provider Types

Model Hotel natively supports the following provider types with automatic detection:

| Provider | Detection | Discovery | Keyless |
|----------|-----------|-----------|---------|
| OpenAI | `api.openai.com` | ✅ Generic OpenAI-compatible | ❌ |
| Anthropic | Custom base URL | ✅ Generic OpenAI-compatible | ❌ |
| Groq | Custom base URL | ✅ Generic OpenAI-compatible | ❌ |
| DeepSeek | `api.deepseek.com` | ✅ Dedicated catalog with pricing | ❌ |
| NanoGPT | `api.nano-gpt.com` | ✅ Dedicated catalog with pricing | ❌ |
| Z.AI | `api.z.ai` | ✅ Dedicated catalog with pricing | ❌ |
| Ollama | `ollama.com` or localhost | ✅ Ollama API | ❌ |
| OpenCode Zen | `*.opencode.ai` with `/zen` path | ✅ Dedicated catalog | ✅ |
| OpenCode Go | `*.opencode.ai` with `/zen/go` path | ✅ Dedicated catalog | ❌ |
| Custom | Any OpenAI-compatible URL | ✅ Generic OpenAI-compatible | Varies |

## Adding a Provider

### Via Dashboard

1. Navigate to **Providers** in the sidebar
2. Click **Add Provider**
3. Enter:
   - **Name** — A unique display name (max 100 characters)
   - **Base URL** — The provider's API base URL (max 500 characters, HTTPS required unless `ALLOW_HTTP_PROVIDERS=true`)
   - **API Key** — The provider's API key (optional for keyless providers like OpenCode Zen)
4. The provider is created and discovery runs automatically (if `discovery_on_provider_create` is enabled)

### Via Admin API

```bash
curl -X POST http://localhost:8081/api/providers \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-openai",
    "base_url": "https://api.openai.com/v1",
    "api_key": "sk-..."
  }'
```

## Provider Validation

When creating or updating a provider, the backend performs several validations:

1. **URL Scheme Check** — HTTPS is required unless `ALLOW_HTTP_PROVIDERS=true`
2. **Loopback Block** — `localhost`, `127.0.0.1`, and `::1` are always blocked
3. **Host Resolution** — All resolved IPs are checked for loopback
4. **Allowed Hosts** — If `ALLOWED_PROVIDER_HOSTS` is set, the host must be in the allowlist (built-in providers are always allowed)
5. **Unique Name** — Provider names must be unique

## Provider Type Detection

The `DetectProviderType` function in `internal/provider/discovery.go` uses a cascading approach:

1. **Exact host match** — `api.openai.com`, `api.deepseek.com`, etc.
2. **Subdomain suffix match** — `*.nano-gpt.com`, `*.deepseek.com`, etc.
3. **OpenCode path-based detection** — `/zen/go` for Go, `/zen` for Zen
4. **Localhost Ollama** — Any localhost with "ollama" in the host

## Keyless Providers

Some providers support keyless access for free models:

- **OpenCode Zen** — Free models available without an API key
- When creating a keyless provider, leave the API key field empty
- The backend stores `NULL` for `encrypted_key`, `key_nonce`, and `key_salt` columns
- Discovery and proxy routing work normally, but no API key is sent to the provider

## Provider Discovery

When a provider is added, the discovery process:

1. **Detects the provider type** from the base URL
2. **Calls the provider's model list API** using the detected type's endpoint
3. **Enriches metadata** using dedicated catalogs (DeepSeek, NanoGPT, Z.AI, OpenCode)
4. **Creates or updates** model records in the database
5. **Syncs failover groups** — Auto-generates or updates hotel routing groups

Discovery runs:
- On startup (if `discovery_on_startup=true`)
- When a provider is created (if `discovery_on_provider_create=true`)
- Periodically via background job (interval controlled by `discovery_interval`, default 6h)

## Provider Health & Quotas

The dashboard shows live data for supported providers:

- **DeepSeek** — Account balance fetched from DeepSeek API
- **NanoGPT** — Usage data from NanoGPT API
- **Z.AI** — Quota usage from Z.AI API

These are fetched in real-time when viewing the provider list and cached briefly.

## Provider State

Providers can be:
- **Enabled** — Normal operation, available for routing
- **Disabled** — Not considered for new requests but remains in failover groups
- **Deleted** — Removes the provider and sets `provider_id=NULL` on historical logs

Disabling a provider does not remove it from failover groups; you must also disable the specific failover entry if you want to stop routing through it.
```

```markdown
# Failover & Hotel Routing

Model Hotel has two related but distinct routing concepts: **transparent failover** and **hotel routing**.

## Transparent Failover

Transparent failover happens automatically when a provider request fails. It is the proxy's last-resort mechanism to ensure requests succeed even when individual providers are flaky.

### How It Works

1. Client requests `model-x` from `provider-a`
2. `provider-a` returns a 5xx error or times out
3. The proxy checks if `model-x` is available from any other provider
4. If found, the request is retried with the alternative provider
5. This continues until the request succeeds or all alternatives are exhausted
6. Each attempt is logged with the attempt number and error details

### Failover Decisions

Failover triggers on:
- HTTP 5xx status codes
- Request timeouts
- Network errors

Failover does **not** trigger on:
- 4xx client errors (these are forwarded to the client)
- Rate limit responses (unless configured otherwise)

### Failover Attempt Logging

Each failed attempt creates a log entry with:
- `failover_attempt` — The attempt number (1, 2, 3...)
- `error_message` — The error from the failed provider
- `duration_ms` — Time spent before the failure
- `status_code` — The HTTP status if available

## Hotel Routing

Hotel routing is explicit multi-provider routing via the `hotel/` prefix. It gives you fine-grained control over which providers are used and in what order.

### Using Hotel Routing

Prefix any model ID with `hotel/`:

```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_KEY" \
  -d '{
    "model": "hotel/gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

This resolves to the failover group named `gpt-4o` and tries providers in priority order.

### Failover Groups

A failover group represents one logical model with multiple backing providers:

| Field | Description |
|-------|-------------|
| `display_model` | The model name used in `hotel/` requests |
| `priority_order` | JSON array of provider IDs in priority order |
| `created_at` / `updated_at` | Timestamps |

Groups are auto-generated during discovery when multiple providers expose the same model (or a similar model ID). The system normalizes model IDs to match equivalents across providers.

### Managing Failover Groups

In the dashboard **Failover** page:

- **Drag to reorder** providers within a group
- **Disable individual entries** — Strikethrough styling, reduced opacity
- **Manual editing** — Add or remove providers from the group

Disabled entries are skipped during routing but remain in the group for easy re-enablement.

### How Hotel Routing Works

```
Client requests "hotel/gpt-4o"
         │
         ▼
┌─────────────────┐
│ Look up failover│
│ group "gpt-4o" │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Get priority    │
│ order list      │
└────────┬────────┘
         │
         ▼
┌─────────────────┐     ┌──────────────┐
│ Try provider #1 │──►  │ Success?     │
│ (highest prio)  │     │ Yes → Stream │
└────────┬────────┘     │ No → Retry   │
         │               └──────┬───────┘
         │                      │
         │               ┌──────┘
         │               ▼
         │      ┌─────────────────┐
         │      │ Try provider #2 │
         │      │ (next in list)  │
         │      └─────────────────┘
         │
         ▼
    (Continue until success
     or all providers exhausted)
```

### Combined Behavior

Transparent failover and hotel routing work together:

1. Client requests `hotel/gpt-4o`
2. Hotel routing selects `provider-a` (highest priority)
3. `provider-a` times out → transparent failover kicks in
4. Proxy checks if any other provider in the hotel group can serve `gpt-4o`
5. Retries with `provider-b`, `provider-c`, etc.
6. If all hotel providers fail, returns the last error to the client
```

```markdown
# Virtual Keys

Virtual keys are client-facing API keys that proxy requests to your configured providers. They are the authentication mechanism for the `/v1/*` proxy endpoints.

## How Virtual Keys Work

```
┌──────────┐      Virtual Key      ┌──────────┐      Provider API Key
│  Client  │ ─────────────────────► │  Proxy   │ ─────────────────────► │ Provider │
│          │                        │          │                        │          │
└──────────┘                        └──────────┘                        └──────────┘
        (Bearer: proxy-key-abc)          (Decrypts provider key
                                          using MASTER_KEY)
```

1. Client includes a virtual key in the `Authorization: Bearer <virtual-key>` header
2. Proxy validates the virtual key (SHA-256 hash lookup)
3. Proxy looks up the requested model and provider
4. Proxy decrypts the provider's real API key using `MASTER_KEY`
5. Request is forwarded to the provider with the real API key

## Security Properties

| Property | Implementation |
|----------|---------------|
| Storage | SHA-256 hash only — raw key never persisted |
| Key preview | First 4 + last 4 characters shown in UI |
| Revocation | Instant — delete the key, it stops working immediately |
| Per-key tracking | Token usage (prompt + completion) logged per virtual key |
| Rate limiting | Independent token bucket per virtual key |

## Creating Virtual Keys

### Via Dashboard

1. Navigate to **Virtual Keys** in the sidebar
2. Click **Create Key**
3. Enter a name (e.g., "production-app", "personal-use")
4. Copy the generated key immediately — it is shown only once
5. The key hash is stored, the raw key is discarded

### Via Admin API

```bash
curl -X POST http://localhost:8081/api/virtual-keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-app"}'
```

Response:
```json
{
  "id": "...",
  "name": "my-app",
  "key": "proxy-key-abc123...",
  "key_preview": "ab****23",
  "tokens_used": 0
}
```

## Using Virtual Keys

```bash
export PROXY_KEY="proxy-key-abc123..."

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

## Rate Limiting

Each virtual key has its own independent rate limit token bucket. The global settings control the defaults:

| Setting | Default | Description |
|---------|---------|-------------|
| `rate_limit_enabled` | `true` | Master toggle for rate limiting |
| `rate_limit_rps` | `10` | Requests per second (set to `0` for unlimited) |
| `rate_limit_burst` | `20` | Maximum burst size |

These can be changed at runtime in **Settings** or via the admin API. Setting `RATE_LIMIT_ENABLED=false` in the environment completely disables rate limiting.

When a key is rate limited, the proxy returns:
- HTTP `429 Too Many Requests`
- `Retry-After` header with seconds until retry
- `X-RateLimit-Limit` and `X-RateLimit-Remaining` headers

## Tracking Usage

The dashboard shows per-key statistics:
- Total tokens used (prompt + completion)
- Last used timestamp
- Request count (when metric toggle is set to "requests")

The **Logs** page can filter by virtual key name to see all requests from a specific key.

## Revoking Keys

Delete a virtual key to immediately revoke access. Historical logs retain the `virtual_key_name` for auditing, but the key itself can no longer be used.
```

```markdown
# Request Logging

Every request that passes through the proxy is logged with detailed telemetry. This helps you understand performance, identify bottlenecks, and debug issues.

## What Gets Logged

The proxy records metadata only — **never the prompt content or response text**.

| Field | Description |
|-------|-------------|
| `timestamp` | When the request started |
| `provider_id` | Which provider handled the request |
| `model_id` | The requested model |
| `request_id` | Unique request identifier |
| `status_code` | HTTP status returned to the client |
| `latency_ms` | Total time from request start to response end |
| `duration_ms` | End-to-end wall time |
| `ttft_ms` | Time to first token (for streaming) |
| `tokens_prompt` | Number of prompt tokens |
| `tokens_completion` | Number of completion tokens |
| `tokens_prompt_cache_hit` | DeepSeek cache hit tokens |
| `tokens_prompt_cache_miss` | DeepSeek cache miss tokens |
| `tokens_per_second` | Streaming throughput |
| `streaming` | Whether the request used streaming |
| `error_message` | Error text if the request failed |
| `failover_attempt` | Which attempt this was (0 for first try) |
| `virtual_key_name` | Which virtual key was used |
| `virtual_key_id` | Foreign key to virtual_keys table |

## Proxy Overhead Breakdown

The latency is decomposed into proxy-internal phases:

| Phase | Description |
|-------|-------------|
| `parse_ms` | JSON parsing and request validation |
| `model_lookup_ms` | Resolving model ID to provider |
| `provider_lookup_ms` | Finding the provider record |
| `key_decrypt_ms` | Decrypting the provider API key |

These are all measured in parallel to the actual provider request, so they represent pure proxy overhead.

## Viewing Logs

The dashboard has two log views:

### Requests Log (`/logs`)

Shows all proxy requests with:
- Sortable columns (timestamp, model, provider, status, latency, etc.)
- Filters (date range, model, provider, status code, virtual key)
- Pagination
- In-flight request highlighting (rows pulse while streaming)
- Click-through to provider/model details

### App Logs (`/logs` → toggle to "Logs")

Shows the server's own application logs:
- Persisted to the `app_logs` database table
- Async batch writer for performance
- Severity levels (info, warning, error)
- Source attribution
- Pagination and purge capability
- Live updates via ring buffer polling

## Log Retention

The `log_retention` setting (default not set) controls how long request logs are kept. When set, a background job purges logs older than the retention period on startup and periodically.

## Filtering and Search

The requests log supports:
- **Date range** — Start and end dates
- **Model** — Filter by model ID (with multi-select)
- **Provider** — Filter by provider name
- **Status code** — Filter by HTTP status
- **Virtual key** — Filter by key name
- **Text search** — Full-text search across log fields

## Streaming Requests

Streaming requests are logged twice:
1. **On start** — A log entry is created with `status_code=0` (in-progress)
2. **On completion** — The same entry is updated with final status, token counts, and timing

This means you can see in-flight requests in the log table. They appear with a pulsing row animation.
```

```markdown
# Model Discovery

Model discovery automatically synchronizes the model list from each provider. This means you don't have to manually enter model IDs — they're pulled directly from the provider's API.

## Discovery Triggers

Discovery runs in three scenarios:

1. **On startup** — If `discovery_on_startup=true` (default)
2. **On provider create** — If `discovery_on_provider_create=true` (default)
3. **Periodic background sync** — Controlled by `discovery_interval` (default `6h`)

## Provider-Specific Discovery

Different provider types have different discovery implementations:

### Generic OpenAI-Compatible

Uses the standard `/v1/models` endpoint. Returns basic model IDs only.

### DeepSeek

Uses `/v1/models` plus a dedicated catalog for metadata:
- Context length
- Input/output pricing
- Reasoning flag
- Cache pricing
- Model description

### NanoGPT

Uses the NanoGPT models API plus a dedicated catalog:
- Context length
- Pricing per million tokens
- Reasoning flag
- Model description

### Z.AI

Uses the Z.AI API plus dedicated catalog:
- Context length
- Pricing
- Reasoning flag
- Input/output modalities (text, image, video, file)
- Model description

### Ollama

Uses the Ollama `/api/tags` endpoint:
- Lists all local models
- Detects vision support from model tags
- Sets input modalities (text, or text+image for vision models)

### OpenCode (Zen & Go)

Uses a built-in static catalog (`opencode_zen_catalog.go`, `opencode_go_catalog.go`):
- Context length
- Pricing
- Reasoning flag
- Input/output modalities
- Model description

## Model Metadata

Discovered models store rich metadata:

| Field | Description |
|-------|-------------|
| `model_id` | Provider-specific model identifier |
| `display_name` | Human-readable name |
| `name` | Normalized short name |
| `description` | Model description |
| `context_length` | Maximum context window |
| `max_output_tokens` | Maximum output length |
| `modality` | Primary modality (text, vision, etc.) |
| `input_modalities` | Supported input types (JSON array) |
| `output_modalities` | Supported output types (JSON array) |
| `capabilities` | JSONB with capability flags |
| `params` | Default generation parameters |
| `input_price_per_million` | Input token pricing |
| `output_price_per_million` | Output token pricing |
| `reasoning` | Whether the model supports reasoning |
| `enabled` | Whether the model is available for routing |

## Model Enabling/Disabling

Models can be individually enabled or disabled in the dashboard:
- **Enabled** — Available for proxy requests and discovery
- **Disabled** — Not considered for routing but remains in the database
- **Multi-select** — Bulk enable/disable from the Models page

## Model Deletion

Models can be permanently deleted. This is useful for cleaning up old models that are no longer offered by the provider.

## Discovery Configuration

All discovery settings are DB-backed and can be changed at runtime:

| Setting | Default | Description |
|---------|---------|-------------|
| `discovery_interval` | `6h` | How often to re-sync all providers |
| `discovery_on_startup` | `true` | Run discovery when the server starts |
| `discovery_on_provider_create` | `true` | Run discovery when a new provider is added |
```

```markdown
# Provider Health

The dashboard provides tools to monitor provider health and verify that models are responding correctly.

## Model Testing

From the **Models** page, click the **Test** button on any model to send a minimal chat completion request through the proxy. The test reports:

- **TTFT** (Time to First Token) — How quickly the model starts responding
- **Total duration** — End-to-end request time
- **Actual response** — The model's generated text
- **Error details** — If the request fails

This uses the admin API test endpoint, which sends a simple prompt and measures the full response cycle.

## Provider Quotas & Balances

For supported providers, the dashboard displays live account information:

### DeepSeek

- Fetches balance from DeepSeek API
- Shows remaining credit

### NanoGPT

- Fetches usage data from NanoGPT API
- Shows account status

### Z.AI

- Fetches quota usage from Z.AI API
- Shows remaining quota

These are fetched when viewing the provider list and cached briefly to avoid excessive API calls.

## System Status Sidebar

The left sidebar shows real-time system statistics:

| Metric | Source | Description |
|--------|--------|-------------|
| API Status | Health check | Whether the proxy API is responding |
| Uptime | Process | How long the server has been running |
| CPU | cgroup / Docker | CPU usage percentage |
| Processes | cgroup / Docker | Number of running processes |
| Memory | cgroup / Docker | Memory usage (with limit if available) |
| Network RX/TX | cgroup / Docker | Network throughput |
| Disk Read/Write | cgroup / Docker | Disk I/O |

When running under Docker Compose, Docker container stats are shown (aggregated across compose services). Otherwise, cgroup stats are used.

### Color Warnings

System stats use threshold-based color coding:

| Color | Condition |
|-------|-----------|
| Green | Normal |
| Orange | Warning (CPU ≥ 75%, memory ≥ 80%) |
| Red | Critical (CPU ≥ 90%, memory ≥ 95%) |

## In-Flight Request Monitoring

The **Requests** log shows streaming requests as they happen:
- Rows pulse with an animation while the request is active
- Status shows as in-progress
- Updated in real-time as the stream completes
```

```markdown
# Chat & Arena

The dashboard includes interactive tools for testing and comparing models: **Chat**, **Conversation**, and **Arena**.

## Chat Mode

The standard chat interface for interactive testing.

### Features

- **Model picker** — Select any discovered model with search and filtering
- **System personas** — Choose from preset characters or enter a custom system prompt:
  - Merlin (mythic allegory)
  - Madame Vex (aggressively positive life coach)
  - Sarge (hard-boiled detective)
  - Auntie Wei (gossiping neighbor)
  - Grimm (museum docent)
  - Kairos (sports commentator)
- **Generation parameters** — Adjust temperature, top_p, max_tokens, min_p, top_k, frequency_penalty, presence_penalty
- **Streaming responses** — Real-time token streaming with thinking block rendering
- **Message controls** — Copy, delete, regenerate, stop
- **Model detail pill** — Inline model info with parameter display
- **Auto-resize textarea** — Expands as you type

### Chat API

Chat uses the admin API at `/api/chat/chat` (admin-authenticated proxy to the provider).

## Conversation Mode

Watch two models talk to each other. This is a unique feature for observing model behavior, testing consistency, or just entertainment.

### How It Works

1. Select **Model A** and **Model B** (can be the same or different)
2. Optionally set different system prompts for each model
3. Configure generation parameters for each model independently
4. Enter a **starter prompt** (what Model A responds to first)
5. Set **rounds** (1-50, each round = both models respond)
6. Set **delay** (0-5000ms pause between turns)
7. Click **Start**

### Conversation Flow

```
Round 1:
  User Prompt → Model A
  Model A Response → Model B
  Model B Response → Model A

Round 2:
  Model A Response → Model B
  Model B Response → Model A

... continues for N rounds
```

### Controls

- **Start** — Begin the conversation
- **Continue** — Resume after pausing
- **Pause** — Stop after the current turn
- **Reset** — Clear the conversation and start over
- **Collapsible config** — Hide/show the configuration panel

### Persistence

Conversation state can be persisted to localStorage (toggle in Settings). This saves:
- Selected models
- System prompts
- Generation parameters
- Message history
- Round configuration

## Arena Mode

Compare models side-by-side with structured evaluation.

### Competition Mode (Bracket Tournament)

Run a bracket-style tournament between multiple models:

1. Select **Group 1** and **Group 2** models
2. Choose an **arena prompt** (preset or custom)
3. Set generation parameters
4. Click **Run Arena**

The system runs all matchups between Group 1 and Group 2 models. After each matchup, you vote for the better response. The tournament auto-advances through rounds.

**Arena Prompts:**
- Dilemma (locked room narrative)
- Lore (reluctant deity religion)
- Hook (impossible-to-stop novel opening)
- Blueprint (pointless indispensable app)
- Spiral (define "almost" without synonyms)

### Compare Mode

Side-by-side comparison without voting:

1. Select one or more models
2. Enter any prompt
3. See all responses in a grid layout
4. Compare metrics (duration, tokens, chars/second)

### Features

- **Model detail panel** — Click model pills to see full model info
- **Thinking blocks** — Rendered separately from main response
- **Markdown rendering** — Full markdown support in responses
- **Copy / retry** — Per-response actions
- **Auto-advance** — Optional automatic progression
- **Persist state** — Save arena configuration to localStorage

### Arena API

Arena uses `/api/chat/arena` (admin-authenticated proxy with model duplication).
```

```markdown
# Real-Time Events & System Status

Model Hotel provides real-time visibility into system state through two mechanisms: an SSE event bus and live system statistics.

## Event Bus (SSE)

The backend publishes events to an in-memory pub/sub bus. The frontend subscribes via Server-Sent Events (SSE) at `/api/events`.

### Event Types

| Type | Severity | When Published |
|------|----------|---------------|
| `provider.created` | success | New provider added |
| `provider.updated` | info | Provider configuration changed |
| `provider.deleted` | warning | Provider removed |
| `model.discovered` | success | New models found during discovery |
| `failover.triggered` | warning | Failover retry initiated |
| `rate_limit.hit` | warning | Virtual key rate limited |
| `virtual_key.created` | success | New virtual key issued |
| `virtual_key.deleted` | warning | Virtual key revoked |
| `settings.changed` | info | Runtime setting updated |
| `error` | error | Unhandled proxy error |

### Toast Notifications

Events are displayed as toast notifications in the dashboard:
- Configurable position (top-left, top-center, top-right, bottom-left, bottom-center, bottom-right)
- Configurable timeout (1-30 seconds, default 4s)
- Severity-based colors (success=green, info=blue, warning=amber, error=red)
- Duplicate suppression (same message won't stack)

### SSE Reconnection

The frontend handles SSE connection drops with exponential backoff:
- Starts at 1 second
- Doubles each failure (2s, 4s, 8s...)
- Caps at 30 seconds
- Resets to 1s on successful connection

## System Status

The sidebar displays live system statistics refreshed every 10 seconds.

### Metrics

| Metric | Source | Docker Compose |
|--------|--------|---------------|
| API Status | HTTP health check | Same |
| Uptime | Process start time | Same |
| CPU % | cgroup cpuacct | Docker aggregate |
| Processes | cgroup pids | Docker container count |
| Memory | cgroup memory | Docker memory limits |
| Network RX/TX | cgroup network | Docker network stats |
| Disk Read/Write | cgroup blkio | Docker I/O stats |

### Docker Container Stats

When the app detects it's running under Docker Compose (via `docker.sock` mount), it shows aggregated stats across all compose services. This provides a more complete picture than the app container alone.

### Threshold Warnings

The sidebar uses color coding to highlight issues:

| Metric | Warning | Critical |
|--------|---------|----------|
| CPU | ≥ 75% | ≥ 90% |
| Memory | ≥ 80% | ≥ 95% |

Warnings show in orange, critical in red.
```

```markdown
# Configuration

Model Hotel is configured through environment variables and runtime database settings.

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MASTER_KEY` | Yes | — | Master encryption key for provider API keys |
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `PORT` | No | `:8080` | Server listen address |
| `DATA_DIR` | No | `./data` | Directory for admin token file |
| `ADMIN_TOKEN` | No | *(auto)* | Fixed admin token (auto-generated if empty) |
| `ALLOW_HTTP_PROVIDERS` | No | `false` | Allow HTTP provider URLs |
| `RATE_LIMIT_ENABLED` | No | `true` | Hard kill-switch for rate limiting |
| `MAX_REQUEST_SIZE` | No | `10485760` | Max request body in bytes (10MB) |
| `CORS_ORIGINS` | No | `localhost` | Allowed CORS origins (comma-separated) |
| `ALLOWED_PROVIDER_HOSTS` | No | — | Additional allowed provider hosts (comma-separated) |

### Notes

- `MASTER_KEY` must be strong and kept secret — it encrypts all provider API keys
- `ADMIN_TOKEN` is auto-generated on first run if not provided. It is displayed once in the logs and never stored in plaintext.
- `ALLOW_HTTP_PROVIDERS` is useful for local Ollama instances
- `ALLOWED_PROVIDER_HOSTS` restricts which hosts can be used as provider base URLs (built-in providers are always allowed)

## Database Settings

These settings are stored in the `settings` table and can be changed at runtime via the **Settings** UI or admin API:

| Setting | Default | Description |
|---------|---------|-------------|
| `discovery_interval` | `6h` | Model auto-discovery interval |
| `discovery_on_startup` | `true` | Run discovery on server start |
| `discovery_on_provider_create` | `true` | Run discovery when adding a provider |
| `log_retention` | — | How long to keep request logs |
| `stale_request_timeout` | — | Timeout for stale request cleanup |
| `failover_on_rate_limit` | — | Whether to failover on 429 responses |
| `rate_limit_enabled` | `true` | Runtime toggle for rate limiting |
| `rate_limit_rps` | `10` | Requests per second (0 = unlimited) |
| `rate_limit_burst` | `20` | Burst bucket size |
| `theme` | `dark` | UI theme (dark/light) |
| `ui_style` | — | UI style preset (cyber-terminal, glassmorphism-lite) |
| `accent_color` | `#1dd1a1` | Primary accent color |

### Rate Limiting

The rate limiting system uses a token bucket per virtual key:

- Each key gets its own independent bucket
- `rate_limit_rps` controls the refill rate (requests per second)
- `rate_limit_burst` controls the maximum bucket size
- Setting `rate_limit_rps=0` disables rate limiting for all keys
- The `RATE_LIMIT_ENABLED` environment variable is a hard kill-switch

When rate limited, responses include:
- `Retry-After: <seconds>`
- `X-RateLimit-Limit: <burst>`
- `X-RateLimit-Remaining: <tokens left>`

## Frontend Settings

User preferences are stored in `localStorage`:

| Key | Description |
|-----|-------------|
| `adminToken` | Admin authentication token |
| `theme` | dark/light |
| `accentColor` | Hex color |
| `uiStyle` | cyber-terminal, glassmorphism-lite, or default |
| `toastPosition` | Toast notification position |
| `toastTimeout` | Toast display duration (ms) |
| `persistChat` | Whether to persist chat state |
| `persistConversation` | Whether to persist conversation state |
| `persistArena` | Whether to persist arena state |
| `sidebarChatSubMode` | chat/conversation |
| `sidebarArenaSubMode` | competition/compare |
| `sidebarLogsSubMode` | request/app |

## Docker Compose

The included `docker-compose.yml` sets up:

- **app** service — The Model Hotel server
  - Port `8081` mapped to container `8080`
  - Volume mount for `.data` (persistent storage)
  - Optional Docker socket mount for container stats
- **db** service — PostgreSQL 16
  - Port `5432` exposed
  - Health check for readiness

### Quick Start

```bash
git clone <repository-url>
cd llm-proxy

cp .env.example .env
nano .env          # set MASTER_KEY and DATABASE_URL

docker compose up --build
```

Get the admin token:
```bash
docker compose logs app | grep "ADMIN_TOKEN="
```

If you lose the token, delete `.data/admin-token` and restart.
```

```markdown
# API Reference

Model Hotel exposes two API surfaces: the **Proxy API** (OpenAI-compatible, for clients) and the **Admin API** (for management).

## Proxy API (`/v1/*`)

OpenAI-compatible endpoints that require a virtual key.

### Authentication

```
Authorization: Bearer <virtual-key>
```

### Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/v1/models` | GET | List available models |
| `/v1/chat/completions` | POST | Chat completion (streaming supported) |

### Chat Completions

```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "hotel/gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

Supports all standard OpenAI parameters plus model hotel routing via `hotel/` prefix.

## Admin API (`/api/*`)

Requires the admin token.

### Authentication

```
Authorization: Bearer <admin-token>
```

### Providers

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/providers` | GET | List all providers |
| `/api/providers` | POST | Create a provider |
| `/api/providers/{id}` | GET | Get provider details |
| `/api/providers/{id}` | PUT | Update provider |
| `/api/providers/{id}` | DELETE | Delete provider |
| `/api/providers/{id}/discover` | POST | Trigger manual discovery |
| `/api/providers/{id}/test` | POST | Test a specific model |
| `/api/providers/discover-all` | POST | Discover all providers |
| `/api/providers/refresh-quotas` | POST | Refresh quota data |

### Models

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/models` | GET | List all models |
| `/api/models/{id}` | GET | Get model details |
| `/api/models/{id}` | PUT | Update model (enable/disable) |
| `/api/models/{id}` | DELETE | Delete model |

### Failover Groups

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/failover-groups` | GET | List failover groups |
| `/api/failover-groups/{id}` | GET | Get group details |
| `/api/failover-groups/{id}` | PUT | Update priority order |
| `/api/failover-groups/{id}/sync` | POST | Sync group with current models |

### Virtual Keys

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/virtual-keys` | GET | List virtual keys |
| `/api/virtual-keys` | POST | Create a virtual key |
| `/api/virtual-keys/{id}` | DELETE | Revoke a virtual key |

### Request Logs

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/logs` | GET | Query request logs (with filters) |
| `/api/logs/stats` | GET | Log statistics |

### App Logs

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/app-logs` | GET | Query application logs |
| `/api/app-logs/purge` | POST | Purge old app logs |

### Settings

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/settings` | GET | List all settings |
| `/api/settings` | PUT | Update settings |

### Stats

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/stats` | GET | Dashboard statistics |
| `/api/stats/timeseries` | GET | Time-series data |
| `/api/stats/provider-distribution` | GET | Provider usage breakdown |

### System

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/system` | GET | System status (CPU, memory, network, Docker) |

### Events (SSE)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/events` | GET | Server-Sent Events stream (requires admin token) |

### Chat & Arena (Admin)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/chat/completions` | POST | Admin-authenticated chat completion |
| `/api/chat/chat` | POST | Interactive chat (admin) |
| `/api/chat/arena` | POST | Arena mode (admin) |
```

```markdown
# Security

Model Hotel implements multiple layers of security to protect sensitive data.

## Encryption at Rest

### Provider API Keys

Provider API keys are encrypted using **AES-256-GCM**:

- Each key gets a unique random nonce (12 bytes) and salt (16 bytes)
- The master key derives the encryption key via PBKDF2
- Ciphertext, nonce, and salt are stored in the database
- Decryption is cached for 5 minutes to reduce overhead

### Key Cache

Decrypted keys are held in an in-memory cache with a 5-minute TTL:
- Cache key derived from ciphertext + nonce + salt
- Thread-safe with RWMutex
- Eviction goroutine cleans expired entries

## Hashing

### Virtual Keys

Virtual keys are stored as **SHA-256 hashes** only:
- Raw key is shown once on creation, then discarded
- Only the hash is persisted
- Lookup compares SHA-256 of provided key against stored hash

### Admin Token

The admin token is **SHA-256 hashed** before storage:
- Plaintext token displayed once on first run
- Hash stored in `<DATA_DIR>/admin-token`
- Regenerate by deleting the file and restarting

## Provider URL Validation

The `ValidateProviderURL` function enforces:

1. **HTTPS by default** — HTTP only if `ALLOW_HTTP_PROVIDERS=true`
2. **Loopback block** — `localhost`, `127.0.0.1`, `::1` always rejected
3. **IP resolution check** — All resolved IPs checked for loopback
4. **Allowed hosts** — Optional allowlist via `ALLOWED_PROVIDER_HOSTS`

## Security Headers

All HTTP responses include:
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`
- `X-XSS-Protection: 1; mode=block`

## Admin Token Authentication

The admin API requires a Bearer token:
- Token parsed from `Authorization: Bearer <token>` header
- Compared against SHA-256 hash using constant-time comparison
- Rejected with 401 if missing or invalid

## Rate Limiting

Per-virtual-key token bucket rate limiting prevents abuse:
- Independent buckets per key
- Configurable RPS and burst
- Returns standard `Retry-After` and `X-RateLimit-*` headers
```

```markdown
# Privacy

Model Hotel is designed with privacy as a core principle.

## What Is Never Captured

> **Prompts and request content are never captured, logged, or inspected.**
>
> The proxy forwards requests to the provider exactly as received, without reading or modifying message contents.

This means:
- Your chat messages are not stored
- Images uploaded via vision API are not inspected
- System prompts are not logged
- Response text is not retained

## What Is Logged

The only information recorded is strictly necessary for routing and metering:

| Data | Purpose |
|------|---------|
| Timestamp | Request timing and analytics |
| Time-to-first-token (TTFT) | Performance monitoring |
| Total duration | End-to-end latency |
| Token counts | Usage tracking and billing |
| Proxy overhead breakdown | Performance optimization |
| Virtual key identifier | Usage attribution |
| Target provider | Routing analysis |
| Model ID | Usage analytics |

## Data Retention

- Request logs can be purged via the `log_retention` setting
- App logs (server output) can be purged via the admin API
- Historical data is not automatically deleted unless configured

## Provider Trust

While Model Hotel does not read your prompts, the underlying providers (OpenAI, Anthropic, etc.) still receive them. Choose providers whose privacy policies align with your requirements.

## Local Deployment

For maximum privacy, run Model Hotel locally with Ollama or another local provider. This keeps all data on your own infrastructure.
```

```markdown
# Development

## Prerequisites

- Go 1.22+
- Node.js 20+
- PostgreSQL 16+
- Docker & Docker Compose (optional)

## Local Setup

1. **Clone the repository**
   ```bash
   git clone <repository-url>
   cd llm-proxy
   ```

2. **Install Go dependencies**
   ```bash
   go mod tidy
   ```

3. **Install frontend dependencies**
   ```bash
   cd web
   npm install
   cd ..
   ```

4. **Set up PostgreSQL**
   ```bash
   # Option 1: Local PostgreSQL
   createdb llmproxy
   
   # Option 2: Docker
   docker compose up -d db
   ```

5. **Create `.env`**
   ```bash
   cp .env.example .env
   # Edit .env:
   #   DATABASE_URL=postgres://user:pass@localhost/llmproxy
   #   MASTER_KEY=your-32-byte-master-key
   ```

6. **Run migrations**
   Migrations run automatically on server startup.

7. **Start the backend**
   ```bash
   make run
   # Or: go run ./cmd/server/
   ```

8. **Start the frontend (dev mode)**
   ```bash
   cd web
   npm run dev
   ```

## Makefile Commands

| Command | Description |
|---------|-------------|
| `make build` | Build the server binary |
| `make run` | Build and run the server |
| `make test` | Run all Go tests |
| `make clean` | Remove build artifacts |
| `make deps` | Tidy Go modules |
| `make docker-up` | Start PostgreSQL in Docker |
| `make docker-down` | Stop Docker services |
| `make docker-logs` | Tail Docker logs |

## Project Structure

```
llm-proxy/
├── cmd/server/          # Entry point
│   ├── main.go          # Server setup, graceful shutdown
│   └── spa.go           # SPA static file serving
├── internal/
│   ├── admin/           # Admin token management
│   ├── api/             # HTTP handlers
│   ├── auth/            # Encryption & hashing
│   ├── config/          # Environment config
│   ├── ctxkeys/         # Context key types
│   ├── db/              # Database pool & migrations
│   ├── events/          # Event bus
│   ├── failover/        # Failover logic
│   ├── model/           # Model repository
│   ├── provider/        # Provider repository & discovery
│   ├── proxy/           # OpenAI-compatible proxy
│   ├── ratelimit/       # Rate limiting
│   ├── settings/        # Runtime settings
│   ├── util/            # Utilities
│   └── virtualkey/      # Virtual key repository
├── web/                 # React frontend
│   ├── src/
│   │   ├── api/         # API client
│   │   ├── components/  # Reusable components
│   │   ├── context/     # React contexts
│   │   ├── data/         # Presets
│   │   ├── pages/        # Top-level pages
│   │   └── utils/        # Helpers
│   ├── public/
│   └── index.html
├── docs/                # Documentation assets
├── docker-compose.yml
├── Makefile
└── README.md
```

## Contributing

See [CONTRIBUTING.md](https://github.com/user/llm-proxy/blob/master/CONTRIBUTING.md) for the contributor license agreement and guidelines.

- Open an issue to discuss large changes before investing time in a PR
- Keep commits focused and write clear commit messages
- Run `make test` before submitting
- Be excellent to each other
```

```markdown
# Sidebar

This is the wiki for Model Hotel, a multi-provider AI gateway.

## Pages

- [[Architecture]]
- [[Providers]]
- [[Failover & Hotel Routing]]
- [[Virtual Keys]]
- [[Request Logging]]
- [[Model Discovery]]
- [[Provider Health]]
- [[Chat & Arena]]
- [[Real-Time Events & System Status]]
- [[Configuration]]
- [[API Reference]]
- [[Security]]
- [[Privacy]]
- [[Development]]

## Quick Links

- [GitHub Repository](https://github.com/user/llm-proxy)
- [LICENSE](https://github.com/user/llm-proxy/blob/master/LICENSE)
- [CONTRIBUTING.md](https://github.com/user/llm-proxy/blob/master/CONTRIBUTING.md)
