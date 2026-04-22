# LLM-Proxy: Initial Implementation Plan

## Overview

Aggregate multiple OpenAI-compatible LLM providers behind a single OpenAI-compatible endpoint. Includes auto-discovery, failover, smart parameter filtering, encrypted key storage, and a modern React dashboard.

## Tech Stack

| Layer | Technology | Why |
|-------|-----------|-----|
| Backend | Go 1.22+ | High concurrency, low memory, fast |
| Router | chi v5 | Idiomatic, simple, well-documented, LLMs know it |
| Database | PostgreSQL 16 | Structured storage for providers, models, logs |
| DB Driver | pgx v5 | Best Go Postgres driver, excellent docs |
| Migrations | golang-migrate | Industry standard, SQL-based |
| Encryption | AES-256-GCM (crypto/aes) | Standard library, no external deps |
| Key Derivation | Argon2id (golang.org/x/crypto) | Industry standard KDF |
| Frontend | React + Vite + TypeScript | Most LLM-friendly framework combo |
| UI Components | shadcn/ui | Modern, accessible, well-known to LLMs |
| Data Fetching | TanStack Query v5 | Reactive, cached, well-documented |
| Charts | Recharts | Simple, React-native, LLMs know it |
| Container | Docker Compose | Single-command deploy |

## Project Structure

```
llm-proxy/
├── cmd/
│   └── server/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── auth/
│   │   ├── encryption.go
│   │   └── proxy_auth.go
│   ├── admin/
│   │   └── token.go
│   ├── provider/
│   │   ├── provider.go
│   │   └── discovery.go
│   ├── model/
│   │   ├── model.go
│   │   └── params.go
│   ├── proxy/
│   │   ├── handler.go
│   │   ├── stream.go
│   │   ├── filter.go
│   │   ├── vision.go
│   │   └── failover.go
│   ├── logging/
│   │   └── logger.go
│   └── db/
│       ├── db.go
│       └── migrations/
│           └── 001_init.sql
├── web/
│   ├── src/
│   │   ├── App.tsx
│   │   ├── main.tsx
│   │   ├── pages/
│   │   │   ├── Dashboard.tsx
│   │   │   ├── Providers.tsx
│   │   │   ├── Models.tsx
│   │   │   └── Logs.tsx
│   │   ├── components/
│   │   │   ├── ui/
│   │   │   ├── ProviderCard.tsx
│   │   │   ├── ModelTable.tsx
│   │   │   ├── UsageChart.tsx
│   │   │   └── LatencyGraph.tsx
│   │   ├── api/
│   │   │   └── client.ts
│   │   └── hooks/
│   │       └── useProviders.ts
│   ├── package.json
│   ├── vite.config.ts
│   ├── tsconfig.json
│   └── index.html
├── plans/
│   └── initial-implementation.md
├── docker-compose.yml
├── Dockerfile
├── go.mod
├── go.sum
├── .env.example
├── .gitignore
└── Makefile
```

## Implementation Steps

---

### Step 1: Project Scaffold & Core Config

**Goal**: Go module, config loading, basic main.go that starts and reads env vars.

**Files to create**:
- `go.mod` — module `github.com/user/llm-proxy`
- `cmd/server/main.go` — entry point, loads config, starts HTTP server
- `internal/config/config.go` — struct + env loading for all config vars

**Config struct**:
```go
type Config struct {
    Port                string // default :8080
    DatabaseURL         string // required
    MasterKey           string // required, used to encrypt provider API keys
    DiscoveryInterval   string // default "30m", parsed as time.Duration
    DataDir             string // default "./data"
    AllowHTTPProviders  bool   // default false, allow http:// provider URLs
}
```

**How to test**:
1. `go build ./cmd/server/`
2. Run with env vars: `DATABASE_URL=x MASTER_KEY=testkey ./server`
3. Verify it starts and logs the config values (with MasterKey masked)
4. Verify it fails with a clear error if required env vars are missing

---

### Step 2: Database Setup & Migrations

**Goal**: Connect to PostgreSQL, run migrations, confirm schema exists.

**Files to create**:
- `internal/db/db.go` — connection pool setup, migration runner
- `internal/db/migrations/001_init.sql` — full schema

**Schema**:

```sql
-- Providers
CREATE TABLE providers (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    base_url      TEXT NOT NULL,
    encrypted_key BYTEA NOT NULL,
    key_nonce     BYTEA NOT NULL,
    enabled       BOOLEAN DEFAULT true,
    last_discovered_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ DEFAULT now(),
    updated_at    TIMESTAMPTZ DEFAULT now()
);

-- Discovered Models
CREATE TABLE models (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id   UUID REFERENCES providers(id) ON DELETE CASCADE,
    model_id      TEXT NOT NULL,
    display_name  TEXT,
    capabilities  JSONB,
    params        JSONB,
    enabled       BOOLEAN DEFAULT true,
    created_at    TIMESTAMPTZ DEFAULT now(),
    UNIQUE(provider_id, model_id)
);

-- Failover Groups: same model from multiple providers
CREATE TABLE model_failover_groups (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    display_model TEXT NOT NULL UNIQUE,
    priority_order JSONB,
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- Usage Logs (NO prompts, NO responses)
CREATE TABLE request_logs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id   UUID REFERENCES providers(id),
    model_id      TEXT,
    request_id    TEXT,
    status_code   INT,
    latency_ms    INT,
    tokens_prompt INT,
    tokens_completion INT,
    streaming     BOOLEAN,
    error_message TEXT,
    created_at    TIMESTAMPTZ DEFAULT now()
);

-- Indexes for common queries
CREATE INDEX idx_request_logs_created_at ON request_logs(created_at DESC);
CREATE INDEX idx_request_logs_model_id ON request_logs(model_id);
CREATE INDEX idx_request_logs_provider_id ON request_logs(provider_id);

-- Proxy API Keys (for clients connecting to this proxy)
CREATE TABLE proxy_keys (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_hash      TEXT NOT NULL UNIQUE,
    name          TEXT,
    created_at    TIMESTAMPTZ DEFAULT now()
);
```

**Dependencies**: `github.com/jackc/pgx/v5`, `github.com/golang-migrate/migrate/v4`

**How to test**:
1. Start PostgreSQL (via `docker compose up db`)
2. Run the server, verify it connects and applies migrations
3. Connect to DB with `psql`, verify all 5 tables exist with correct columns
4. Stop and restart server, verify it doesn't re-apply migrations (idempotent)

---

### Step 3: Encrypted Key Storage & Admin Token Generation

**Goal**: AES-256-GCM encryption/decryption for provider API keys, and admin token generation on first run.

**Files to create**:
- `internal/auth/encryption.go` — `Encrypt(plaintext, masterKey) -> (ciphertext, nonce)`, `Decrypt(ciphertext, nonce, masterKey) -> plaintext`
- `internal/admin/token.go` — generate admin token on first run, save to `{DataDir}/admin-token`, print to stdout

**Key derivation**: Use Argon2id to derive a 32-byte AES key from the `MASTER_KEY` env var with a fixed salt. The salt being static is fine — the MASTER_KEY itself is the secret.

**Admin token**:
- On first run: generate 32-byte hex token, write to `./data/admin-token`
- On subsequent runs: read existing token from file
- Print token to stdout on every start
- Dashboard and management API require `Authorization: Bearer <admin-token>`

**How to test**:
1. Write a Go test: encrypt a string, decrypt it, verify roundtrip
2. Write a Go test: verify different master keys produce different ciphertexts
3. Start server, verify `./data/admin-token` file is created
4. Restart server, verify same token is loaded (not regenerated)
5. Delete `./data/admin-token`, restart, verify a new token is generated

---

### Step 4: Provider CRUD API

**Goal**: REST API to add, list, update, and delete providers. API keys are encrypted before storage.

**Files to create**:
- `internal/provider/provider.go` — struct, DB operations (Create, List, Get, Update, Delete)
- `internal/api/admin.go` — chi router with admin routes

**Routes**:
- `POST /api/providers` — create provider
- `GET /api/providers` — list all providers
- `GET /api/providers/{id}` — get single provider
- `PUT /api/providers/{id}` — update provider
- `DELETE /api/providers/{id}` — delete provider

**Request/Response**:
```
POST /api/providers
  Body: { "name": "OpenAI", "base_url": "https://api.openai.com", "api_key": "sk-..." }
  Response: { "id": "uuid", "name": "OpenAI", "base_url": "...", "masked_key": "sk-...***", "enabled": true }

GET /api/providers
  Response: [ { ... }, ... ]

DELETE /api/providers/{id}
  Response: 204 No Content
```

**Key rule**: API key is encrypted before DB insert. In responses, API key is masked (show last 4 chars only). The full key is NEVER returned after creation.

**How to test**:
1. Start server with admin token
2. `curl -X POST -H "Authorization: Bearer <token>" -H "Content-Type: application/json" -d '{"name":"Test","base_url":"https://api.openai.com","api_key":"sk-test123"}' http://localhost:8080/api/providers`
3. Verify response has masked key (`***123`)
4. `GET /api/providers` — verify key is masked
5. Check DB directly — verify `encrypted_key` is binary, not plaintext
6. `DELETE /api/providers/{id}` — verify 204 and provider is gone

---

### Step 5: Proxy API Key Management

**Goal**: Issue and revoke API keys that clients use to access the proxy's `/v1/*` endpoints.

**Files to create**:
- `internal/auth/proxy_auth.go` — generate key, hash with SHA-256, validate incoming Bearer tokens
- `internal/api/keys.go` — `POST /api/keys` (create), `GET /api/keys` (list), `DELETE /api/keys/{id}` (revoke)

**Flow**:
1. Admin creates a proxy key: `POST /api/keys {"name": "my-laptop"}`
2. Response includes the full key ONE TIME: `{ "id": "uuid", "name": "my-laptop", "key": "llmp_abc123..." }`
3. Only the SHA-256 hash is stored in DB (like a password)
4. On `/v1/*` requests, hash the provided Bearer token and look up in DB
5. Key is never stored in plaintext, never returned again after creation

**How to test**:
1. Create a key via `POST /api/keys` — note the full key from response
2. Make a `GET /v1/models` with `Authorization: Bearer <key>` — verify 200
3. Make the same request with a wrong key — verify 401
4. `GET /api/keys` — verify keys are listed but full keys are NOT shown
5. `DELETE /api/keys/{id}` — verify key no longer authenticates

---

### Step 6: Model Auto-Discovery

**Goal**: Periodically poll each enabled provider's `/v1/models` endpoint, discover models, and store them in the DB.

**Files to create**:
- `internal/provider/discovery.go` — `DiscoverProviderModels(ctx, provider) -> []Model`
- `internal/model/model.go` — DB operations for models (Upsert, List, Get, Delete)
- `internal/model/params.go` — best-effort parameter discovery from `/v1/models/{id}` if supported

**Discovery logic**:
1. For each enabled provider, HTTP GET `{base_url}/v1/models` with `Authorization: Bearer {decrypted_key}`
2. Parse response into Model structs
3. Upsert into `models` table (match on provider_id + model_id)
4. Mark models that disappeared as `enabled = false` (don't delete — they might come back)
5. Try `/v1/models/{model_id}` for parameter details (tolerate 404s — not all providers support this)
6. Schedule next discovery based on `DISCOVERY_INTERVAL`
7. Also expose manual trigger: `POST /api/providers/{id}/discover`

**How to test**:
1. Add a real provider (e.g., OpenAI or a local Ollama with `ALLOW_HTTP_PROVIDERS=true`)
2. `POST /api/providers/{id}/discover` — trigger manual discovery
3. `GET /api/models` — verify models appear with correct data
4. Wait for auto-discovery cycle, verify new/changed models update
5. Disable a model on the provider, run discovery again, verify it's marked `enabled = false`
6. Test with an invalid provider URL — verify graceful error handling

---

### Step 7: Proxy Endpoint — Chat Completions

**Goal**: Implement `/v1/chat/completions` that forwards requests to the correct provider.

**Files to create**:
- `internal/proxy/handler.go` — main proxy handler
- `internal/proxy/stream.go` — SSE streaming passthrough

**Flow**:
1. Receive `POST /v1/chat/completions` with model ID (e.g., `gpt-4o`)
2. Look up model in DB — find provider and base_url
3. Decrypt provider's API key
4. Filter request parameters (strip params the target provider is known to reject)
5. Forward request to `{provider.base_url}/v1/chat/completions`
6. If streaming: pipe SSE response through verbatim
7. If non-streaming: read full response and return
8. Log the request (NO prompts, NO responses — only metadata)

**How to test**:
1. Set up a provider (Ollama locally or OpenAI)
2. Create a proxy key
3. `curl -X POST -H "Authorization: Bearer <proxy-key>" -H "Content-Type: application/json" -d '{"model":"<model-id>","messages":[{"role":"user","content":"Hello"}]}' http://localhost:8080/v1/chat/completions`
4. Verify response is valid OpenAI format
5. Test streaming: add `"stream": true`, verify SSE events flow through
6. Test with invalid model — verify 404
7. Test with invalid proxy key — verify 401

---

### Step 8: Smart Parameter Filtering

**Goal**: Strip or adapt request parameters based on known provider incompatibilities.

**Files to create**:
- `internal/proxy/filter.go` — provider-specific filter rules

**Known incompatibilities to handle**:

| Parameter | OpenAI | Groq | Ollama | Azure OpenAI |
|-----------|--------|------|--------|-------------|
| `logprobs` | Yes | No | No | Yes |
| `response_format` | Yes | Partial | No | Yes |
| `seed` | Yes | Yes | Yes | No |
| `stop` (as array) | Yes | Yes | Partial | Yes |
| `stream_options` | Yes | No | No | Partial |

**Approach**: Maintain a map of `provider_type -> set of unsupported params`. Before forwarding, strip unsupported params. Log which params were stripped at debug level.

**How to test**:
1. Send request with `logprobs` to a Groq provider — verify it's stripped and request succeeds
2. Send same request to OpenAI — verify `logprobs` is preserved
3. Add a test for each known incompatibility
4. Verify no params are stripped when sending to an unknown provider type

---

### Step 9: Vision Payload Adaptation

**Goal**: Allow sending images to vision-capable models through the proxy.

**Files to create**:
- `internal/proxy/vision.go` — detect vision messages, adapt format if needed

**Vision message format** (OpenAI standard):
```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "What is in this image?"},
    {"type": "image_url", "image_url": {"url": "data:image/png;base64,..."}}
  ]
}
```

**Approach**:
1. Detect if request contains `image_url` content parts
2. Check if target model/provider supports vision (from `models.capabilities.vision`)
3. If model doesn't support vision, return clear error: `"Model X does not support vision input"`
4. If supported, pass through as-is (OpenAI-compatible providers all use this format)

**How to test**:
1. Send a vision request (with base64 image) to a vision model — verify it works
2. Send a vision request to a non-vision model — verify clear error message
3. Send a vision request with a URL-based image (not base64) — verify passthrough
4. Test with multiple images in one message

---

### Step 10: Failover Logic

**Goal**: Route requests for the same model across multiple providers with automatic failover.

**Files to create**:
- `internal/proxy/failover.go` — failover selection and retry logic

**Flow**:
1. Receive request for model `gpt-4o`
2. Look up `model_failover_groups` for `display_model = "gpt-4o"`
3. If no failover group exists, use the single provider from `models` table
4. If failover group exists, try providers in priority order
5. On success (2xx), return response and log which provider served it
6. On failure (5xx, timeout > 10s, connection error), try next provider
7. If all providers fail, return 502 with error details

**Timeout**: 10 seconds per provider attempt, configurable.

**Management API**:
- `POST /api/failover-groups` — create group
- `GET /api/failover-groups` — list groups
- `PUT /api/failover-groups/{id}` — update priority order
- `DELETE /api/failover-groups/{id}` — delete group

**How to test**:
1. Add same model from two providers (e.g., same model on OpenAI and Azure OpenAI)
2. Create a failover group: `POST /api/failover-groups {"display_model":"gpt-4o","priority_order":["provider-a-uuid","provider-b-uuid"]}`
3. Send request — verify it hits primary provider
4. Temporarily make primary provider unavailable — verify failover to secondary
5. Make both unavailable — verify 502 with details
6. Verify request log shows which provider actually served

---

### Step 11: Request Logging

**Goal**: Log request metadata to PostgreSQL. NEVER log prompts or responses.

**Files to create**:
- `internal/logging/logger.go` — async request logger with buffered writing

**What gets logged**:

| Field | Example |
|-------|---------|
| provider_id | UUID |
| model_id | "gpt-4o" |
| request_id | "req_abc123" |
| status_code | 200 |
| latency_ms | 1234 |
| tokens_prompt | 150 |
| tokens_completion | 500 |
| streaming | true |
| error_message | (empty or error text) |

**What NEVER gets logged**:
- Message content / prompts
- Response content / completions
- API keys
- User IP addresses

**Implementation**:
- Use a buffered channel + goroutine for async writing (don't block proxy requests)
- Batch inserts every 5 seconds or 100 rows, whichever comes first
- Include request_id in proxied response headers for traceability

**How to test**:
1. Make several proxy requests (streaming and non-streaming)
2. Query `SELECT * FROM request_logs ORDER BY created_at DESC LIMIT 10`
3. Verify all expected fields are populated
4. Verify NO message content exists anywhere in the logs
5. Verify logging doesn't add noticeable latency to proxy requests (<5ms overhead)

---

### Step 12: Management API — Models, Stats, Logs

**Goal**: API endpoints for the dashboard to fetch model info, failover groups, and aggregated statistics.

**Files to create**:
- `internal/api/models.go` — `GET /api/models`, `PUT /api/models/{id}`, model detail endpoint
- `internal/api/stats.go` — `GET /api/stats` aggregated stats for dashboard
- `internal/api/logs.go` — `GET /api/logs` with pagination and filters

**Stats endpoint response**:
```json
{
  "total_requests_last_24h": 1500,
  "total_requests_last_7d": 12000,
  "by_model": { "gpt-4o": 800, "claude-3": 700 },
  "by_provider": { "OpenAI": 1000, "Anthropic": 500 },
  "avg_latency_ms": 1200,
  "error_rate": 0.02,
  "total_tokens_prompt": 150000,
  "total_tokens_completion": 50000
}
```

**Logs endpoint**: Paginated, filterable by model, provider, status, date range. Sorted by created_at DESC.

**How to test**:
1. Make proxy requests to populate logs
2. `GET /api/stats` — verify aggregated numbers are correct
3. `GET /api/logs?model=gpt-4o&limit=10` — verify filtered results
4. `GET /api/logs?from=2025-01-01&to=2025-01-02` — verify date filtering
5. Verify pagination works (`?page=2&limit=20`)

---

### Step 13: Dashboard — Setup & Layout

**Goal**: React SPA with routing, layout shell, and navigation.

**Files to create**:
- `web/` — entire Vite + React + TypeScript project (use `npm create vite@latest`)
- Install: `react-router-dom`, `@tanstack/react-query`, `shadcn/ui` (via npx), `recharts`
- `web/src/App.tsx` — router with 4 pages
- `web/src/main.tsx` — query client provider
- Layout with sidebar navigation: Dashboard, Providers, Models, Logs

**How to test**:
1. `cd web && npm run dev` — verify dev server starts
2. Navigate to each route — verify pages render (even if empty)
3. Verify responsive layout works at mobile width
4. Verify navigation highlights current page

---

### Step 14: Dashboard — Providers Page

**Goal**: Full CRUD for providers with real-time status.

**Components**:
- `ProviderCard.tsx` — shows provider name, base_url, status indicator, model count
- Add provider modal/form (name, base_url, api_key — key input is password-masked)
- Edit/Delete actions
- Manual "Discover Models" button per provider
- Last discovery timestamp

**How to test**:
1. Add a provider via the UI — verify it appears in the list with masked key
2. Edit the provider name — verify update persists
3. Delete a provider — verify it's removed and its models are cascaded
4. Click "Discover Models" — verify spinner and then models appear in Models page
5. Verify real-time status indicator (green = healthy, red = error)

---

### Step 15: Dashboard — Models Page

**Goal**: View all discovered models across all providers with search and filtering.

**Components**:
- `ModelTable.tsx` — sortable/filterable table of all models
- Columns: Display Name, Model ID, Provider, Capabilities (vision/streaming badges), Status
- Search bar for model names
- Filter by provider, capability, status
- Click model for detail view (parameters, failover config)

**How to test**:
1. Verify all discovered models appear
2. Search for "gpt" — verify filtered results
3. Filter by "vision" capability — verify only vision models show
4. Click a model — verify detail view with parameters
5. Verify data refreshes when discoveries run

---

### Step 16: Dashboard — Usage & Stats

**Goal**: Visual dashboard showing usage patterns, latency, and error rates.

**Components**:
- `UsageChart.tsx` — line/bar chart of requests over time (Recharts)
- `LatencyGraph.tsx` — average and P95 latency over time per model
- Summary cards: total requests (24h, 7d), total tokens, error rate
- Model usage breakdown (pie chart or bar chart)

**How to test**:
1. Generate some traffic via proxy (curl requests)
2. Verify dashboard shows correct request counts
3. Verify latency graph updates after requests
4. Verify time range selector works (1h, 24h, 7d, 30d)
5. Verify error rate calculation is correct after introducing errors

---

### Step 17: Dashboard — Logs Page

**Goal**: Paginated, filterable request log viewer (NO prompts visible, ever).

**Components**:
- Table with columns: Timestamp, Model, Provider, Status, Latency, Tokens (prompt/completion), Streaming
- Filters: model, provider, status code, date range
- Pagination controls
- Request ID column for traceability
- Verify: absolutely no message content anywhere on this page

**How to test**:
1. Make proxy requests, verify they appear in logs
2. Filter by model — verify only that model's logs show
3. Filter by error status — verify only failed requests show
4. Paginate through results — verify correct page boundaries
5. Search entire page HTML for any accidental prompt content — must find none

---

### Step 18: Docker & Deployment

**Goal**: Production-ready Docker Compose setup with bind mounts.

**Files to create**:
- `Dockerfile` — multi-stage build (Go build + React build + final image)
- `docker-compose.yml` — app + PostgreSQL services
- `.env.example` — template for required env vars

**docker-compose.yml**:
```yaml
services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      - MASTER_KEY=${MASTER_KEY}
      - DATABASE_URL=postgres://llmproxy:changeme@db:5432/llmproxy
      - DISCOVERY_INTERVAL=30m
    volumes:
      - ./.data:/data
    depends_on:
      db:
        condition: service_healthy

  db:
    image: postgres:16-alpine
    environment:
      - POSTGRES_USER=llmproxy
      - POSTGRES_PASSWORD=changeme
      - POSTGRES_DB=llmproxy
    volumes:
      - ./.data/pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U llmproxy"]
      interval: 5s
      timeout: 5s
      retries: 5
```

**How to test**:
1. `docker compose up --build` — verify everything starts
2. Verify admin token is printed to app container logs
3. Verify PostgreSQL is healthy and migrations ran
4. Add a provider via API, verify it persists across `docker compose restart`
5. Make proxy requests, verify logs in DB survive restart
6. `docker compose down && docker compose up` — verify all data persists (bind mounts)

---

### Step 19: Security Hardening

**Goal**: Enforce security best practices across the application.

**Measures**:
1. **TLS enforcement**: Validate all provider `base_url`s start with `https://` (reject `http://` unless `ALLOW_HTTP_PROVIDERS=true`)
2. **Security headers**: Add CSP, X-Frame-Options, X-Content-Type-Options, Strict-Transport-Security headers
3. **Rate limiting**: Simple per-key rate limiting on proxy endpoint (e.g., 100 req/min default)
4. **Input validation**: Validate all API inputs with strict schemas
5. **Admin token file permissions**: Set `./data/admin-token` to mode 0600 (owner read-only)
6. **CORS**: Restrict to dashboard origin only
7. **Request size limit**: 10MB max request body on proxy endpoint

**How to test**:
1. Try adding provider with `http://` base URL (without ALLOW_HTTP_PROVIDERS) — verify rejection
2. Verify security headers are present on all responses
3. Send >100 requests in 1 minute — verify 429 rate limit response
4. Send malformed JSON — verify 400, not 500
5. Verify CORS headers only allow dashboard origin
6. Send 15MB request body — verify 413 rejection

---

### Step 20: End-to-End Integration Test

**Goal**: Full workflow test from provider setup to proxy request to dashboard visibility.

**Test scenario**:
1. Start with clean Docker environment
2. `docker compose up --build`
3. Get admin token from logs
4. Add Ollama as provider: `POST /api/providers {"name":"Ollama","base_url":"http://host.docker.internal:11434","api_key":"ollama"}` (with `ALLOW_HTTP_PROVIDERS=true`)
5. Trigger discovery: `POST /api/providers/{id}/discover`
6. Verify models appear: `GET /api/models`
7. Create proxy key: `POST /api/keys {"name":"test-key"}`
8. Make chat request: `POST /v1/chat/completions` with proxy key
9. Verify response is valid
10. Check dashboard — verify provider, model, and log entry all visible
11. Make a streaming request — verify SSE works
12. Check logs page — verify entry appears with correct metadata

---

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MASTER_KEY` | Yes | — | Master key for encrypting provider API keys |
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `PORT` | No | `:8080` | Server listen address |
| `DISCOVERY_INTERVAL` | No | `30m` | How often to auto-discover models |
| `DATA_DIR` | No | `./data` | Directory for admin token file |
| `ALLOW_HTTP_PROVIDERS` | No | `false` | Allow http:// provider URLs (for local dev) |

## Future Improvements (Not in v1)

- Budget/cost tracking per provider/model
- Embeddings endpoint (`/v1/embeddings`)
- Audio endpoint (`/v1/audio/transcriptions`)
- Multi-user / RBAC
- Provider health monitoring (periodic ping)
- WebSocket support
- OpenTelemetry tracing
- Automatic failover group creation (detect same model across providers)
- Model aliasing (e.g., map "gpt4" -> "gpt-4o")
- Rate limiting per proxy key (configurable)