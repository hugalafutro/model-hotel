# Architecture

## Overview

Model Hotel is a multi-provider AI gateway built with:
- **Backend**: Go (Golang) with Chi router, PostgreSQL database
- **Frontend**: React + TypeScript + Vite
- **Database**: PostgreSQL 16 with pgx driver
- **Deployment**: Docker Compose

## System Components

### Backend Structure

```
cmd/server/           # Main application entry point
  ├── main.go         # HTTP server setup, middleware, routing
  └── spa.go          # SPA static file handler

internal/
  ├── api/            # Admin API handlers
  │   ├── admin.go    # Provider CRUD
  │   ├── models.go   # Model management
  │   ├── virtualkeys.go
  │   ├── logs.go     # Request logs
  │   ├── applogs.go  # Application logs
  │   ├── logscache.go # Logs cache
  │   ├── settings.go
  │   ├── discovery.go # Model discovery triggers
  │   ├── events.go   # SSE events
  │   ├── failover.go # Failover group management
  │   ├── stats.go    # Statistics
  │   ├── system.go   # System stats
  │   ├── helpers.go  # Test helpers
  │   └── validate.go # Validation helpers
  ├── proxy/          # OpenAI-compatible proxy endpoints
  │   ├── handler.go  # /v1 routes, rate limiting, auth
  │   ├── proxy.go    # Chat completions with failover
  │   ├── models.go   # Model listing with hotel/ prefix
  │   ├── resolve.go  # Hotel routing resolution
  │   ├── logging.go  # Request logging
  │   ├── helpers.go  # Proxy helpers
  │   └── types.go    # Chat completion types
  ├── provider/       # Provider management
  │   ├── provider.go # Provider repository (CRUD)
  │   ├── discovery.go # Auto-discovery logic + type detection
  │   ├── modelsdev.go # Models.dev enrichment (pricing, capabilities from open-source catalogue)
  │   ├── cache.go    # Provider caching
  │   └── discovery_*.go # Per-provider discovery (openai, anthropic, deepseek, nanogpt, ollama, zai, opencode_*)
  ├── model/          # Model repository
  │   ├── model.go    # Model CRUD + metadata
  │   └── cache.go    # Model caching
  ├── virtualkey/     # Virtual key repository
  │   ├── virtualkey.go # Key CRUD + generation
  │   └── auth.go     # Key authentication middleware
  ├── failover/       # Failover group repository
  │   ├── failover.go # Failover group management
  │   └── cache.go    # Failover caching
  ├── ratelimit/      # Rate limiting implementation
  │   ├── limiter.go  # Per-key token bucket limiter
  │   └── ip_limiter.go # Per-IP DoS protection
  ├── auth/           # Encryption/decryption (AES-256-GCM)
  ├── db/             # Database migrations
  ├── settings/       # Runtime settings
  ├── events/         # SSE event bus
  └── util/           # Utilities

web/                  # Frontend React app
  ├── src/
  │   ├── pages/      # Page components
  │   ├── components/ # Reusable UI components
  │   ├── api/        # API client and TypeScript types
  │   ├── context/    # React contexts (Theme, Events, etc.)
  │   └── utils/      # Frontend utilities
  └── dist/           # Built static files (served by Go)
```

## Request Flow

### Proxy Request (Client → Provider)

```
1. Client sends request to /v1/chat/completions with virtual key
2. Chi middleware chain:
   - RequestID, RealIP, Logger, Recoverer, Compress
   - streamingAwareTimeout (5min for streaming)
   - IPLimiter.Middleware (per-IP DoS protection)
   - ProxyKeyMiddleware (SHA-256 hash lookup)
   - RateLimiter.Middleware (per-key token bucket)
3. ChatCompletions handler:
   - Parse request body
   - Resolve model (failover group or provider/model)
   - Decrypt provider API key (AES-256-GCM, cached 5min)
   - INSERT request_logs (pending state)
   - For each failover candidate:
     - Forward to upstream provider
     - On 5xx/429/401/403: exponential backoff, retry next
     - On 200: stream/return response
   - UPDATE request_logs (completed/failed state)
   - UPDATE virtual_keys (increment tokens_used)
   - Fire-and-forget TouchLastUsed
4. Return response to client
```

### Model Resolution

**Hotel Routing** (`hotel/gpt-4o`):
```
1. Trim "hotel/" prefix → displayModel = "gpt-4o"
2. Lookup failover group for displayModel
3. Filter enabled entries with enabled providers
4. Return prioritized candidate list
5. Try each candidate in order with exponential backoff
```

**Direct Provider** (`openai/gpt-4o`):
```
1. Split on "/" → provider = "openai", model = "gpt-4o"
2. Lookup provider by type
3. Return single candidate
```

## Database Schema

### Core Tables

**providers**: LLM provider configuration
- `id`, `name`, `base_url`, `encrypted_key`, `key_nonce`, `key_salt`
- `enabled`, `last_discovered_at`, `last_used_at`

**models**: Discovered models from providers
- `id`, `provider_id`, `model_id`, `display_name`
- `capabilities` (JSON), `params` (JSON)
- `enabled`, `provider_enabled`, `disabled_manually`

**model_failover_groups**: Hotel routing groups
- `id`, `display_model` (unique base model name)
- `priority_order` (array of model UUIDs)
- `group_enabled`, `entry_enabled` (per-entry toggle)

**request_logs**: Usage and performance metrics
- **NO prompt content stored**
- `id`, `model_id`, `virtual_key_id`, `status_code`
- `latency_ms`, `ttft_ms`, `duration_ms`, `tokens_per_second`
- `tokens_prompt`, `tokens_completion`, `tokens_prompt_cache_hit/miss`
- `proxy_overhead_breakdown` (parse, lookup, decrypt)
- `streaming`, `failover_attempt`, `state`
- `error_message` (provider errors only)

**virtual_keys**: Per-client API keys
- `id`, `key_hash` (SHA-256), `name`, `tokens_used`

**settings**: Runtime configuration
- `key`, `value`, `updated_at`
- See `settings.AllowedSettings` for valid keys

**app_logs**: Application events
- `id`, `level`, `message`, `metadata` (JSON)
- `created_at` (ring buffer + optional DB persistence)

## Frontend Architecture

### State Management

- **URL-based routing**: React Router v6
- **API data**: TanStack Query (react-query) with caching
- **UI state**: React Context
  - `ThemeContext`: Theme, accent color, UI style
  - `SidebarModeContext`: Chat/arena/logs sub-modes
  - `StorageContext`: localStorage persistence
  - `EventContext`: SSE event handling
  - `ToastContext`: Toast notifications
  - `QuotaModalContext`: Quota display modal

### Key Pages

- **Dashboard**: Provider/model overview, quick stats
- **Providers**: Add/edit/delete providers, manual discovery, quota/balance display
- **Models**: Model list, enable/disable, health testing
- **Failover**: Configure hotel routing groups, priorities
- **Virtual Keys**: Create/revoke client API keys
- **Logs**: Request logs with filtering, app logs
- **Chat**: Interactive chat with personas, streaming
- **Arena**: Competition mode (brackets), Compare mode (grid)
- **Settings**: Runtime configuration UI

### SSE Events

Real-time events pushed via Server-Sent Events:
- `discovery.complete` — Model discovery finished for a provider
- `discovery.models_disabled` — Models were disabled after discovery
- `failover.sync_error` — Error during failover group sync
- `logs.stale_startup` — Stale request detected at startup
- `logs.stale_cleanup` — Stale request cleaned up
- `request.started` — Proxy request began
- `request.completed` — Proxy request finished
- `tokens.error` — Error counting tokens

Event bus decouples backend operations from frontend UI updates.

## Security Model

### Encryption

- **Provider API keys**: AES-256-GCM encryption
  - `MASTER_KEY` → Argon2id key derivation → per-provider AES key
  - Per-provider random salt (stored in `providers.key_salt`)
  - Nonce required for decryption (stored in `providers.key_nonce`)
  - Decrypted keys cached in-memory for 5 minutes

- **Virtual keys**: SHA-256 hash only (irreversible)
  - Raw key displayed once on creation
  - Only hash stored in database

- **Admin token**: SHA-256 hash only
  - Auto-generated on first run, displayed once
  - Stored as hash in `<DATA_DIR>/admin-token`

### Request Isolation

- Per-virtual-key rate limiting (independent token buckets)
- Per-IP DoS protection (always-on, non-configurable)
- No prompt/response content logging or inspection
- Request logs contain only metadata (timing, tokens, errors)

## Caching Strategy

### Backend Caches

- **Provider key cache**: In-memory, 5min TTL, per-provider
  - Prevents repeated Argon2id key derivation
  - Cleared periodically to limit memory usage

- **Settings cache**: 30sec TTL in settings.Repository
  - Reduces DB queries for frequently-accessed settings
  - Subscribers notified immediately on change

- **Discovery cache**: `last_discovered_at` field
  - Skips startup discovery if within 5 minutes
  - Prevents duplicate work on rapid restarts

### Frontend Caches

- **React Query**: Stale-while-revalidate pattern
  - Dashboard stats: 10s refetch
  - System stats: 10s refetch
  - Logs: Real-time polling

- **localStorage**: User preferences
  - Theme, accent color, UI style
  - Chat/conversation/arena state (optional)
  - Arena history (optional, client-side only)

## Deployment

### Docker Compose (Recommended)

```yaml
services:
  app:  # Go server
    port: 8081:8080
    env: MASTER_KEY, DATABASE_URL, etc.
    volumes: ./.data:/data
    mounts: /var/run/docker.sock:ro (for stats)
  
  db:   # PostgreSQL 16
    port: 5432:5432
    env: POSTGRES_USER/PASSWORD/DB
    volumes: ./.data/pgdata:/var/lib/postgresql/data
```

### Standalone

```bash
# Prerequisites: PostgreSQL 16+
export MASTER_KEY="..."
export DATABASE_URL="postgres://..."
go run cmd/server/main.go
```

## Monitoring & Observability

### Metrics Captured

- **Per-request**: latency, TTFT, tokens/s, token counts, cache hit/miss
- **Per-provider**: quota usage, account balance, last used
- **Per-virtual-key**: total tokens used
- **System**: CPU, memory, disk I/O, network, goroutines

### Log Retention

- **request_logs**: Configurable retention (1h to 1mo) via `log_retention` setting
- **app_logs**: Same retention as request_logs
- **Arena history**: Client-side only, never sent to server

### Health Checks

- **/health**: Returns "OK" (no auth required)
- **/api/system**: Detailed system stats (admin auth required)
- **Provider health**: Manual testing from Models page

## Performance Characteristics

### Database Load

- **Per request**: 4-5 DB writes (INSERT log, UPDATE log, UPDATE key usage, TouchLastUsed)
- **Connection pool**: pgxpool default (MaxConns=25)
- **Indices**: `created_at`, `model_id`, `provider_id`, `request_hash`

### Memory Usage

- **Key cache**: O(providers) × 5min TTL
- **Settings cache**: O(settings) × 30sec TTL
- **Request logs**: Not cached, direct DB queries
- **Goroutines**: One per streaming request, cleaned up on disconnect

### Network

- **Shared Transport**: `http.Transport` reused across requests
- **Keep-alive**: Enabled for provider connections
- **Timeout**: 5min for streaming, 60s for non-streaming

## Development Notes

### Adding New Providers

1. Add type to `DetectProviderType()` in `internal/provider/discovery.go`
2. Implement discovery in `DiscoveryService.DiscoverModels()`
3. Add quota/balance fetching if provider supports it
4. Add to `defaultKnownProviderHosts` in `internal/config/config.go`
5. Update frontend model enrichment if needed

For providers that don't have a dedicated catalog, the [models.dev](https://models.dev/) enrichment layer (see [Model Discovery](Model-Discovery#modelsdev-enrichment)) will automatically fill in pricing, capabilities, and context limits for known models. You only need a hardcoded catalog if the provider's models are not in models.dev or if you need provider-specific overrides.

### Database Migrations

- Sequential SQL files in `internal/db/migrations/`
- Applied automatically on startup
- Use `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` for idempotency
- Backfill data in separate migrations

### Testing

- **Unit tests**: `*_test.go` files alongside implementation
- **Stress test**: `tools/stress-test/` with mock upstream
- **Manual testing**: Use dashboard UI or `curl` with admin endpoints
