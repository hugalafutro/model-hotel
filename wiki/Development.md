# 🚀 Development

This guide covers the complete development workflow for the Model Hotel multi-provider LLM gateway.

## Prerequisites

| Tool | Version | Purpose |
|------|---------|---------|
| **Go** | 1.26+ | Backend runtime (required by `go.mod`) |
| **Node.js** | 24+ | Frontend build tooling (CI uses Node 24; Dockerfile uses `node:26-alpine`) |
| **pnpm** | 10.33.0 | Frontend package manager (specified in `package.json`) |
| **PostgreSQL** | 16+ | Database (Docker Compose uses `postgres:16-alpine`) |
| **Docker & Docker Compose** | Latest | Containerized database and full-stack deployment |
| **golangci-lint** | v2.11+ | Go linting (CI requirement) |

## Project Structure

```
model-hotel/
├── cmd/server/                    # Go server entry point
│   ├── main.go                    # Server setup, middleware, graceful shutdown
│   ├── static.go                  # SPA static file serving
│   └── static/                    # Embedded frontend build output
├── internal/                      # Backend packages (private)
│   ├── admin/                     # Admin token management (SHA-256)
│   ├── api/                       # HTTP handlers (REST + SSE)
│   ├── auth/                      # AES-256-GCM encryption, key caching
│   ├── config/                    # Environment configuration
│   ├── ctxkeys/                   # Type-safe context keys
│   ├── db/                        # PostgreSQL connection + migrations
│   ├── debuglog/                  # Structured logging wrapper
│   ├── events/                    # SSE event bus (pub/sub)
│   ├── failover/                  # Failover group routing
│   ├── model/                     # Model entity + repository
│   ├── provider/                  # Provider CRUD + model discovery
│   ├── proxy/                     # OpenAI-compatible proxy
│   ├── ratelimit/                 # Token-bucket rate limiting
│   ├── settings/                  # DB-backed settings with cache
│   ├── util/                      # Helpers (Docker stats, HTTP utils)
│   ├── virtualkey/                # Virtual key CRUD (SHA-256 hashed)
│   └── webauthn/                  # WebAuthn/FIDO2 passkey sessions + credentials
├── web/                           # React + TypeScript frontend
│   ├── src/
│   │   ├── api/                   # API client + TypeScript types
│   │   ├── components/            # Reusable UI components
│   │   ├── context/               # React contexts (Theme, Toast, Event)
│   │   ├── hooks/                 # Custom React hooks
│   │   ├── i18n/                  # i18next setup + locale files (repo is source of truth; translated by hand)
│   │   ├── pages/                 # Dashboard, Providers, Models, etc.
│   │   └── utils/                 # Formatting, SSE, model utils
│   ├── public/                    # Static assets (favicon, icons)
│   ├── index.html                 # HTML entry point
│   ├── vite.config.ts             # Vite configuration
│   ├── tailwind.config.js         # Tailwind CSS config
│   ├── tsconfig.json              # TypeScript config
│   └── package.json               # Dependencies + scripts
├── .github/workflows/
│   └── ci.yml                     # CI pipeline (test, lint, build)
├── docker-compose.yml             # Production stack (app + db)
├── docker-compose.test.yml        # Ephemeral test database (port 5433)
├── Dockerfile                     # Multi-stage build (Node → Go → Alpine)
├── Makefile                       # Build/test/lint commands
├── go.mod                         # Go module definition
```

## Quick Start

### 1. Clone and Initialize

```bash
git clone https://github.com/hugalafutro/model-hotel.git
cd model-hotel
```

### 2. Install Dependencies

```bash
# Backend
go mod tidy

# Frontend
cd web
pnpm install
cd ..
```

### 3. Configure Environment

```bash
cp .env.example .env
```

Generate required secrets:

```bash
# Master encryption key (AES-256-GCM)
MASTER_KEY=$(openssl rand -base64 32)

# Admin authentication token
ADMIN_TOKEN=$(openssl rand -hex 16)

# Database password
POSTGRES_PASSWORD=$(openssl rand -hex 16)
```

Edit `.env` with your generated values. **Never commit `.env`** - it is gitignored.

### 4. Start PostgreSQL

```bash
# Option A: Docker (recommended)
make docker-up

# Option B: Local PostgreSQL
createdb modelhotel
# Set DATABASE_URL in .env to point to localhost
```

### 5. Run the Server

```bash
# Build and run
make run

# Or build only
make build
./bin/server
```

The server starts on `http://localhost:8081` (configured via `HOST_PORT` in `.env`).

### 6. Run the Frontend (Development Mode)

```bash
cd web
pnpm dev
```

The Vite dev server runs on `http://localhost:5173` with hot module replacement.

> **Note:** When running the frontend dev server, you still need the backend running for API calls. The dev server proxies API requests to the backend via the `api/client.ts` configuration.

## Backend Development

### Module Structure

The backend uses Go 1.26 with the module `github.com/hugalafutro/model-hotel`. All internal packages live under `internal/` and are not importable by external modules.

**Key packages:**

| Package | Responsibility |
|---------|----------------|
| `cmd/server/` | Entry point, middleware chain, graceful shutdown |
| `internal/api/` | HTTP handlers for REST endpoints |
| `internal/proxy/` | OpenAI-compatible `/v1/chat/completions` proxy |
| `internal/provider/` | Provider API clients, model discovery |
| `internal/db/` | PostgreSQL connection pool, migrations |
| `internal/events/` | SSE event bus for real-time UI updates |

### Running the Backend

```bash
# Development run (builds then runs)
make run

# Build only
make build
./bin/server

# Direct Go run (no binary)
go run ./cmd/server/
```

### Backend Testing

```bash
# All tests
make test

# Single package
go test ./internal/proxy/...

# Verbose output
go test -v ./...

# CI-equivalent (requires test database)
go test -timeout 5m ./...
```

#### Test Database Setup

Integration tests require a PostgreSQL database. Use the ephemeral test database:

```bash
# Start test database (port 5433)
make test-db-up

# Run tests
make test

# Stop and remove test database
make test-db-down
```

The test database uses `docker-compose.test.yml` which creates an isolated `testdb` database with no persistent volumes.

**Test patterns:**

- Unit tests: No database required, use mocks
- Integration tests: Use `internal/db/testdb.go` helpers
- Handler tests: Use `newTestHandler()` from `internal/api/handler_integration_test.go`

> **⚠️ No skipped tests:** Tests must pass or fail — never `t.Skip`/`it.skip`/`describe.skip`/`.only`/`it.todo`, and no environment-gated skips that silently no-op in CI. If a test needs an external dependency (PostgreSQL, `pg_dump`, docker), CI must provide it rather than skip it. (Policy: PR #340.)

Example test structure:

```go
func TestSomething(t *testing.T) {
    // Arrange
    db := testdb.New(t)
    repo := NewRepository(db.Pool())
    
    // Act
    result, err := repo.Create(ctx, entity)
    
    // Assert
    assert.NoError(t, err)
    assert.NotNil(t, result.ID)
}
```

### Backend Linting

```bash
# Run all linters (CI requirement)
golangci-lint run ./...

# Format imports and run go fmt
make fmt

# Go vet
go vet ./...
```

The `make fmt` command runs both `gci` (import formatting) and `go fmt` on all Go source files.

The CI pipeline runs `golangci-lint` v2.11 with the following linters enabled:
- `gci` - import ordering
- `govet` - standard Go vet checks
- Additional linters configured in `.golangci.yml`

### Debug Logging

The backend uses `internal/debuglog` for structured logging. Control logging via the `DEBUG_LOG` environment variable:

```bash
# Enable debug logging
DEBUG_LOG=true ./bin/server

# Disable (production default)
DEBUG_LOG=false ./bin/server
```

Log levels: `Info`, `Warn`, `Error`. Never use `fmt.Println` in production code.

## Frontend Development

### Setup

```bash
cd web
pnpm install
```

### Development Server

```bash
pnpm dev
```

- Runs on `http://localhost:5173`
- Hot module replacement enabled
- Proxies API calls to backend (configured in `vite.config.ts`)

### Building for Production

```bash
pnpm build
```

Output is written to `web/dist/` and embedded into the Go binary at `cmd/server/static/`.

### Linting and Formatting

The frontend uses **both** ESLint and Biome. Both must pass for pre-commit checks.

```bash
# ESLint
pnpm lint

# Biome (format + lint)
pnpm biome check --write src/components/Foo.tsx

# Biome (check only, no writes)
pnpm biome check src/components/Foo.tsx

# Run all checks
pnpm lint && pnpm biome check
```

> **⚠️ Critical:** Biome **MUST** run from the `web/` directory with paths relative to `web/`.
> - ✅ `pnpm biome check --write src/components/Foo.tsx`
> - ❌ `pnpm biome check --write web/src/components/Foo.tsx`

Running from the project root causes Biome to fail with "configuration resulted in errors" due to the nested `biome.json` structure.

### Testing

```bash
# Run tests once
pnpm test

# Watch mode
pnpm test:watch

# Run with coverage (full suite, ~85 seconds)
cd web && pnpm vitest run --coverage 2>&1 | tail -200
```

Tests use Vitest with jsdom for DOM simulation. Test files are co-located with source files (`*.test.ts`).

> **⚠️ Coverage Command:** Use the exact command above for coverage. Do NOT modify it (no grep pipes, no `2>/dev/null`). The `2>&1 | tail -200` is essential to capture the coverage table after Node's experimental warnings. Takes ~85 seconds with full CPU utilization.

### Frontend Architecture

| Directory | Purpose |
|-----------|---------|
| `src/api/` | Fetch wrapper (`client.ts`), TypeScript types (`types.ts`) |
| `src/components/` | Reusable UI components |
| `src/pages/` | Top-level pages (Dashboard, Providers, Models, etc.) |
| `src/context/` | React contexts (Theme, Toast, Event, Storage) |
| `src/hooks/` | Custom hooks (`useLocalStorage`, `useModels`, etc.) |
| `src/utils/` | Helpers (formatting, SSE, model utils) |

Key patterns:
- **API client:** `api/client.ts` - fetch wrapper with error handling
- **Type safety:** All API responses typed via `api/types.ts`
- **State management:** React Context + `useReducer` for complex state
- **Styling:** Tailwind CSS v4 with custom theme

## Docker Workflow

### Docker Compose Services

The `docker-compose.yml` defines two services:

| Service | Description | Ports | Volumes |
|---------|-------------|-------|---------|
| `app` | Full Model Hotel application | `8081:8080` | `./.data:/data`, Docker socket (ro, commented out by default) |
| `db` | PostgreSQL 16 | (not exposed to host) | `./.data/pgdata:/var/lib/postgresql/data` |

### Running the Full Stack

```bash
# Stop all services
docker compose down

# Stop and remove volumes (destructive)
docker compose down -v

# View logs (follow mode)
make docker-logs
```

The `make docker-logs` command is a convenience wrapper around `docker compose logs -f` to stream logs from all services.

### Rebuild Process

The frontend is embedded in the Go binary. **Any change to `web/src/` requires a full rebuild:**

```bash
# Rebuild and restart
docker compose build app
docker compose up -d app
```

> **⚠️ Important:** `docker compose restart app` does **NOT** pick up frontend changes. You must rebuild.

**Ask before rebuilding** - other developers may be working on the stack.

### Environment Variables (Docker)

Docker Compose reads from `.env` at the project root. Key variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `MASTER_KEY` | AES-256-GCM encryption key | *required* |
| `POSTGRES_PASSWORD` | Database password | *required* |
| `ADMIN_TOKEN` | Admin auth token | Auto-generated |
| `HOST_PORT` | External port | `8081` |
| `POSTGRES_USER` | Database user | `modelhotel` |
| `POSTGRES_DB` | Database name | `modelhotel` |
| `POSTGRES_HOST` | Database host (internal) | `db` |
| `DEBUG_LOG` | Enable debug logging | `false` |
| `RATE_LIMIT_ENABLED` | Enable rate limiting | `true` |
| `CORS_ORIGINS` | Allowed CORS origins | `http://localhost:5173,http://localhost:8081` |

See `.env.example` for the full list.

## CI/CD Pipeline

The CI pipeline (`.github/workflows/ci.yml`) runs on every push and pull request:

| Job | Description |
|-----|-------------|
| `go-test` | Runs `go test -timeout 5m ./...` against PostgreSQL 16, enforces an 80% coverage threshold, uploads to Codecov |
| `go-lint` | Runs `golangci-lint` v2.11 |
| `go-vet` | Runs `go vet ./...` |
| `frontend` | Runs `pnpm lint`, `pnpm vitest run --coverage` (80% threshold, Codecov upload), `pnpm build` |
| `docker-build` | Verifies Docker image builds successfully |
| `i18n-check` | Verifies all 28 locales are in sync with `en.json`: no missing keys, `{{placeholder}}` parity, no non-allowlisted English values |

### Pre-commit Checklist

Before committing changes:

1. ✅ `go test ./...` - All Go tests pass
2. ✅ `golangci-lint run ./...` - Go linting passes
3. ✅ `cd web && pnpm lint` - ESLint passes
4. ✅ `cd web && pnpm biome check` - Biome passes

> **Note:** Do NOT run the full Vite build for pre-commit checks. `lint` + `go test` is sufficient.

### Translations

Locale files in `web/src/i18n/locales/` are the single source of truth (the project previously synced with Crowdin; that integration was removed). The workflow when adding user-facing strings:

1. Add the key to `en.json` AND to all 28 other locales.
2. **Translate the new keys by hand** into each locale, keeping `{{placeholders}}`, `<tags>`, acronyms, and brand names verbatim. The quickest correct way is a one-off script that reuses `tools/i18n-translate/translate.py`'s `load_locale`/`set_path`/`save_locale` helpers (preserves nesting + formatting).
3. Intentionally-English values (brand names, loanwords like "Failover", or a word genuinely identical in some language) belong in `tools/i18n-translate/allow-english.json`.

`make i18n-check` is the CI gate: it runs **offline** (no network) and fails on missing keys, broken `{{placeholder}}` parity, or non-allowlisted English values. Translation corrections are welcome as plain PRs against the locale files.

## Development Workflow

### Typical Edit-Test Cycle

1. **Make changes** to backend or frontend code
2. **Backend:** Run targeted tests
   ```bash
   go test ./internal/proxy/...
   ```
3. **Frontend:** Run Biome + ESLint
   ```bash
   cd web
   pnpm biome check --write src/components/Changed.tsx
   pnpm lint
   ```
4. **Integration test:** Run full test suite if changes affect multiple packages
   ```bash
   make test-db-up
   go test -timeout 5m ./...
   make test-db-down
   ```
5. **Docker test:** Rebuild and test in container
   ```bash
   docker compose build app
   docker compose up -d app
   # Test via http://localhost:8081
   ```

### Debugging Techniques

#### Backend Debugging

1. **Enable debug logging:**
   ```bash
   DEBUG_LOG=true ./bin/server
   ```

2. **Check app logs via API:**
   ```bash
   curl http://localhost:8081/api/logs/app?limit=50
   ```

3. **Database inspection:**
   ```bash
   docker compose exec db psql -U modelhotel -d modelhotel
   ```

4. **Test proxy endpoints:**
   See "Testing Proxy Endpoints" in AGENTS.md for curl examples.

#### Frontend Debugging

1. **React DevTools:** Install browser extension for component inspection
2. **Network tab:** Monitor API calls in browser dev tools
3. **Console logs:** Check for JavaScript errors
4. **SSE events:** Monitor `/api/events` for real-time updates

#### Common Issues

| Issue | Solution |
|-------|----------|
| Biome fails with config errors | Ensure running from `web/` directory with relative paths |
| Tests fail with "connection refused" | Start test database: `make test-db-up` |
| Frontend changes not appearing | Rebuild Docker: `docker compose build app` |
| Admin token lost | Check `.data/admin-token` file or regenerate by deleting it |
| Provider discovery fails | Check `DEBUG_LOG=true` logs, verify API key encryption |

## Contributing

1. **Open an issue** to discuss large changes before implementation
2. **Create a feature branch** from `master`
3. **Follow the pre-commit checklist** above
4. **Write tests** for new functionality
5. **Update documentation** for user-facing changes
6. **Submit a pull request**

All contributions are licensed under the MIT License.

## Deployment Notes

### Production Build

```bash
# Build Docker image
docker build -t model-hotel:latest .

# Run with production environment
docker compose up -d
```

### Environment-Specific Configuration

- **Development:** `DEBUG_LOG=true`, `ALLOW_HTTP_PROVIDERS=true` (for local Ollama)
- **Production:** `DEBUG_LOG=false`, `ALLOW_HTTP_PROVIDERS=false`, enable rate limiting

### Backup and Restore

Database backups are stored in `./.data/pgdata/`. Use PostgreSQL tools:

```bash
# Backup
docker compose exec db pg_dump -U modelhotel modelhotel > backup.sql

# Restore
docker compose exec -T db psql -U modelhotel modelhotel < backup.sql
```

The application also supports periodic backup with son/father/grandfather rotation via the Settings UI (Database Backup section). When enabled, backups are created automatically at a configurable interval and old backups are pruned according to daily/weekly/monthly retention tiers. See [Configuration](Configuration) for the full list of backup settings.

---

**Last updated:** June 2026 (v0.9.49)  
**Go version:** 1.26  
**Node version:** 24 (CI) / 26 (Docker image)  
**pnpm version:** 10.33.0
