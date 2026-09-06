# 🚀 Development

This guide covers the complete development workflow for the Model Hotel multi-provider LLM gateway.

## Prerequisites

Docker and Docker Compose are the only hard requirement: the dev stack builds the frontend and
the Go binary inside the image. The rest matter when you build or lint outside the container.

| Tool | Version | Purpose |
|------|---------|---------|
| **Docker & Docker Compose** | Latest | The dev stack, the test database, and production deployment |
| **Go** | 1.27+ | Backend runtime (required by `go.mod`) |
| **Node.js** | 24+ | Frontend build tooling (CI uses Node 24; the Dockerfile uses `node:26-alpine`) |
| **pnpm** | 10.33.0 | Frontend package manager (pinned by `web/package.json`) |
| **PostgreSQL** | 16 | Database (Compose uses `postgres:16-alpine`) |
| **golangci-lint** | v2.13+ | Go linting (CI pins v2.13) |
| **Python 3** | 3.x | The i18n gate, the coverage scripts, and the pre-push gate |

## Project Structure

One repository, three shipped deliverables: the Model Hotel gateway (`cmd/server/` + `web/`), the
Front Desk HA control plane (`cmd/frontdesk/` + `frontdesk/web/`), and the Bellhop Android
companion app (`android/`).

```
model-hotel/
├── cmd/
│   ├── server/                    # Gateway entry point
│   │   ├── main.go                # Server setup, middleware chain, graceful shutdown
│   │   ├── startup.go             # Boot sequence
│   │   ├── background.go          # Background loops (housekeeping, retention)
│   │   ├── discovery.go           # Scheduled provider/model discovery
│   │   ├── middleware.go          # Auth, CORS, security headers
│   │   ├── banner.go              # Startup banner
│   │   ├── spa.go                 # SPA static file serving
│   │   └── static/                # Embedded frontend build output
│   └── frontdesk/                 # Front Desk control plane entry point
├── internal/                      # Backend packages (private; full list under Backend Development)
├── web/                           # React + TypeScript dashboard
│   ├── src/
│   │   ├── api/                   # API client (client.ts, http.ts, endpoints/) + types
│   │   ├── assets/                # Images and provider logos
│   │   ├── components/            # Reusable UI components
│   │   ├── context/               # React contexts (Theme, Toast, Event, Identity, Storage, Sidebar, QuotaModal)
│   │   ├── data/                  # Static presets
│   │   ├── hooks/                 # Custom React hooks
│   │   ├── i18n/                  # i18next setup + 29 locale files (the repo is the source of truth)
│   │   ├── lib/                   # Icon registry and shared helpers
│   │   ├── pages/                 # Dashboard, Providers, Models, etc.
│   │   ├── test/                  # Vitest setup and shared test helpers
│   │   └── utils/                 # Formatting, SSE, model utils
│   ├── public/                    # Static assets (favicon, icons)
│   ├── index.html                 # HTML entry point
│   ├── vite.config.ts             # Vite configuration
│   ├── vitest.config.ts           # Test + coverage configuration
│   ├── biome.json                 # Biome formatter/linter config
│   ├── eslint.config.js           # ESLint config
│   ├── tailwind.config.js         # Tailwind CSS config
│   └── package.json               # Dependencies + scripts
├── web-shared/                    # Frontend modules both SPAs import via @web-shared/*
├── frontdesk/web/                 # Front Desk SPA (its own locales and tests)
├── android/                       # Bellhop Android companion (Kotlin/Compose, Gradle)
├── deploy/ha/                     # Front Desk + HA Compose stack
├── scripts/                       # Git hooks, size gate, coverage and sharded test runners
├── tools/                         # i18n translate/check, notices generator, dev tooling
├── wiki/                          # This wiki (published by sync-wiki.yml)
├── .github/workflows/             # 12 workflows; ci.yml is the main pipeline
├── docker-compose.yml             # Base stack (app + db)
├── compose.dev.yml                # Dev overlay (Docker socket, DEBUG_LOG, ALLOW_EMBED)
├── docker-compose.test.yml        # Ephemeral test database (port 5433)
├── Dockerfile                     # Multi-stage build (Node 26 -> Go 1.27 -> Alpine)
├── Dockerfile.frontdesk           # Front Desk image
├── Makefile                       # Build/test/lint/docker/i18n targets
└── go.mod                         # Go module definition
```

## Quick Start

The dev stack is the supported path. It builds the frontend and the Go binary inside the image,
so nothing but Docker has to be installed to get a running gateway.

### 1. Clone and Install Hooks

```bash
git clone https://github.com/hugalafutro/model-hotel.git
cd model-hotel
make setup            # points core.hooksPath at scripts/ (pre-commit + pre-push)
```

### 2. Configure Environment

```bash
cp .env.example .env
```

Generate the required secrets and paste them into `.env`:

```bash
openssl rand -base64 32     # MASTER_KEY (AES-256-GCM key for provider API keys)
openssl rand -hex 16        # POSTGRES_PASSWORD
openssl rand -hex 16        # ADMIN_TOKEN (optional; one is generated on first boot if empty)
```

`MASTER_KEY` and `POSTGRES_PASSWORD` are mandatory: Compose refuses to start without them.
**Never commit `.env`**, it is gitignored.

### 3. Start the Stack

```bash
make docker-up
```

That runs `docker compose -f docker-compose.yml -f compose.dev.yml up -d`, which starts the app
and PostgreSQL 16 together. The dev overlay `compose.dev.yml` adds the pieces you want locally and
not in production: the Docker socket mounted read-only (container stats in the sidebar),
`DEBUG_LOG=true`, `ALLOW_EMBED=true` (so the dashboard loads in a workspace browser), and
proxy/observability variables read from `.env`. Every `make docker-*` target passes both files, so
the overlay is never accidentally dropped.

Open **http://localhost:8081** (change with `HOST_PORT` in `.env`). If you left `ADMIN_TOKEN`
empty, the generated token is printed once to stdout on first boot: `make docker-logs`.

### 4. Rebuild After Editing

The frontend is embedded in the Go binary, so both Go and `web/src/` changes need a rebuild:

```bash
make docker-build     # down, then up -d --build
make docker-logs      # follow logs from all services
make docker-down      # stop the stack
```

`docker compose restart app` does **not** pick up frontend changes.

### 5. Frontend Dev Server (optional)

For fast UI iteration with hot module replacement:

```bash
cd web
pnpm install
pnpm dev              # http://localhost:5173
```

> **Note:** The dashboard calls the API on its own origin (`API_BASE` is empty in
> `web/src/api/http.ts`) and `vite.config.ts` configures no API proxy, so the dev server serves the
> UI but cannot reach the backend. Use the Docker stack on `:8081` for anything that talks to the
> API.

### 6. Local Binary (optional)

`make run` builds `bin/server` and runs it on the host. The Compose database publishes no host
port, so the binary needs a PostgreSQL you can reach from the host: your own local install, or any
other instance. Point `DATABASE_URL` in `.env` at it, then:

```bash
go mod tidy
make run              # make build + ./bin/server
```

The binary listens on `PORT`, which defaults to `:8080`, not on `HOST_PORT`. `HOST_PORT` only maps
the container port in Compose.

## Backend Development

### Module Structure

The backend uses Go 1.27 with the module `github.com/hugalafutro/model-hotel`. All internal packages live under `internal/` and are not importable by external modules.

**Entry points:** `cmd/server/` (the gateway: middleware chain, discovery loops, graceful shutdown)
and `cmd/frontdesk/` (the Front Desk control plane).

**The 38 packages under `internal/`:**

| Package | Responsibility |
|---------|----------------|
| `admin` | Admin token management (SHA-256 hashed) |
| `adminauth` | Admin authentication HTTP surface shared by both control planes |
| `alert` | Outbound notifications for noteworthy gateway events (Apprise) |
| `anthropic` | Anthropic Messages wire format, inbound |
| `anthropicegress` | OpenAI to Anthropic translation, outbound |
| `api` | HTTP handlers and routing for the admin API (REST + SSE) |
| `audit` | Audit trail: one row per mutating admin action |
| `auth` | AES-256-GCM encryption of provider API keys, key caching |
| `authcookie` | Session auth over hardened cookies |
| `clientip` | Client address resolution behind proxies |
| `config` | Environment configuration loading and defaults |
| `ctxkeys` | Type-safe context keys |
| `db` | PostgreSQL connection pool, migrations, test-database helpers |
| `debuglog` | `log/slog` wrapper honouring `DEBUG_LOG` |
| `egress` | Pieces the vendor dialect translators share |
| `events` | SSE event bus (pub/sub) |
| `failover` | Failover group management, caching, circuit breaker |
| `frontdesk` | The HA Front Desk control plane |
| `gemini` | Google Gemini wire-format translation |
| `httpx` | HTTP response helpers shared by the admin surfaces |
| `jsonfault` | JSON decode failures rendered as safe diagnostics |
| `metrics` | Prometheus metrics on a private registry |
| `model` | Model entity, repository, discovery cache |
| `netguard` | SSRF defenses for outbound HTTP |
| `openairesponses` | OpenAI Responses API translation |
| `otelexport` | Wires the slog pipeline to an OpenTelemetry OTLP exporter |
| `paramrewrite` | Shared parameter rewriting for provider dialects |
| `provider` | Provider CRUD, API clients, model discovery |
| `proxy` | OpenAI-compatible `/v1/chat/completions` proxy |
| `pwned` | Have I Been Pwned range check for new passwords |
| `quota` | Cached provider quota/usage snapshots |
| `ratelimit` | Token-bucket rate limiting middleware |
| `settings` | Database-backed settings with cache and change subscriptions |
| `totp` | TOTP (RFC 6238) second factor |
| `user` | Dashboard user accounts for multi-user support |
| `util` | Helpers (Docker stats, system utilities) |
| `virtualkey` | Virtual key CRUD (SHA-256 hashed) |
| `webauthn` | WebAuthn/FIDO2 passkey credentials and sessions |

### Running the Backend

```bash
# Development run (builds then runs)
make run

# Build only
make build
./bin/server
```

Both go through the Makefile so the version and commit are stamped into the binary. The binary
listens on `PORT` (default `:8080`) and needs a PostgreSQL reachable from the host, which the
Compose database is not.

### Backend Testing

```bash
# All tests (needs the test database, below)
make test

# Same packages, sharded across concurrent processes
make test-parallel

# Single package
go test ./internal/proxy/...

# CI-equivalent
go test -timeout 10m ./...
```

`-timeout` is per package, not for the whole run, and `internal/api` is by far the largest:
roughly 1,450 tests against a real PostgreSQL. CI allows 10 minutes for it, and 20 for the
separate race-detector job.

#### Test Database Setup

Integration tests need PostgreSQL. Use the ephemeral test database:

```bash
make test-db-up       # starts docker-compose.test.yml on port 5433
make test
make test-db-down     # stop and remove it
```

`docker-compose.test.yml` runs a throwaway `testdb` with no volume and durability turned off
(`fsync=off`), which is why it is fast and why that config must never be copied into
`docker-compose.yml`. Port 5433 keeps it clear of the app's own database, so both can run at once.

**Test patterns:**

- Unit tests: no database, plain Go tests
- Integration tests: `db.SetupTestDB("<pkg>")` in `TestMain` gives the package its own database
  (derived from `TEST_DATABASE_URL`, dropped and recreated on every run); defer
  `db.CleanupTestDB("<pkg>")`. Per-package databases are what let packages run concurrently
  without deadlocking each other. See `internal/db/testdb.go`.
- Handler tests: `newTestHandler(t)` from `internal/api/handler_test_helpers_test.go`, which
  truncates the tables and flushes every process-global cache fed by them.

`TEST_DATABASE_URL` defaults to the local test database; CI sets it to
`postgres://modelhotel:...@localhost:5433/testdb?sslmode=disable`.

> **⚠️ No skipped tests:** Tests must pass or fail: never `t.Skip`/`it.skip`/`describe.skip`/`.only`/`it.todo`, and no environment-gated skips that silently no-op in CI. If a test needs an external dependency (PostgreSQL, `pg_dump`, docker), CI must provide it rather than skip it. The `forbidigo` linter enforces the Go half by banning `t.Skip`/`t.Skipf` outright.

### Backend Linting

```bash
make lint             # golangci-lint run ./...
make fmt              # gci import ordering + go fmt
make size-check       # file-size ratchet
```

`make fmt` runs `gci` (import grouping: standard, default, then this module) over `internal/`
and `cmd/`, then `go fmt ./...`.

CI pins `golangci-lint` v2.13. `.golangci.yml` enables `errcheck`, `govet`, `ineffassign`,
`staticcheck`, `unused`, `gosec`, `gocritic`, `revive`, `gocyclo`, `funlen`, `errorlint`,
`nilerr`, `bodyclose`, `forbidigo` (the `t.Skip` ban) and `nolintlint`. Because `govet` runs as
part of it, there is no separate `go vet` pass in the pre-push hook.

#### Size ceilings

Four ratchets that only ever tighten. Anything over the line gets split rather than exempted:

| Unit | Ceiling | Enforced by |
|------|---------|-------------|
| Production file (`internal/`, `cmd/`, `web/src/`, `frontdesk/web/src/`, `web-shared/`) | 800 lines | `make size-check` |
| Test file (Go `_test.go`; TS under `__tests__/` or an app's `src/test/`) | 2000 lines | `make size-check` |
| Go function (production only) | 200 lines | `funlen` |
| TS/TSX function (production only) | 500 lines | `max-lines-per-function` in both ESLint configs |

Files already over a ceiling carry their current count in `scripts/ci/size-allowlist.txt` and may
only shrink. The list takes no new entries, and the gate compares it against the base commit's
copy, so loosening it fails the same way in CI as it does locally.

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
- Serves the UI only: `API_BASE` is empty, so API calls go to the dev server's own origin and
  `vite.config.ts` sets up no proxy to the backend. Use the Docker stack on `:8081` to exercise
  the API.

### Building for Production

```bash
pnpm build            # tsc -b, then vite build
pnpm exec tsc -b      # typecheck alone (what the pre-push hook runs)
```

Output is written to `web/dist/` and embedded into the Go binary at `cmd/server/static/`.

### Linting and Formatting

The frontend uses **both** ESLint and Biome. Both must pass in CI, and the pre-commit hook runs
Biome over the staged files.

```bash
# ESLint (web/ plus web-shared/)
pnpm lint

# Biome format + lint, writing fixes
pnpm format:fix src/components/Foo.tsx

# Biome check only, no writes (this is what CI runs)
pnpm format
```

> **⚠️ Never `pnpm biome` or `pnpm exec biome`.** Under `pnpm exec` the native lint worker is
> killed and Biome misreports it as `Linter process terminated abnormally (possibly out of
> memory)`. It is not an out-of-memory condition: Biome checks a file in about 10ms and needs
> negligible RAM. Run it through the `format` / `format:fix` package scripts, or call the binary
> directly as `./node_modules/.bin/biome`.

Run both from `web/`, with paths relative to `web/`:
> - ✅ `pnpm format:fix src/components/Foo.tsx`
> - ❌ `pnpm format:fix web/src/components/Foo.tsx`

### Testing

```bash
# Run tests once
pnpm test

# Watch mode
pnpm test:watch

# Full suite with coverage (about 85 seconds; run it once)
pnpm vitest run --coverage
```

Tests use Vitest with jsdom for DOM simulation. Test files live in a `__tests__/` directory beside
the code they cover (`web/src/components/__tests__/Foo.test.tsx`), not alongside the source file.

Coverage output lands in `web/coverage/` and by default contains only `lcov.info`. Add
`--coverage.reporter=json-summary` (or `json`) if you need a machine-readable total. CI shards the
suite three ways and merges the shard reports before checking the 90% threshold.

### Frontend Architecture

| Directory | Purpose |
|-----------|---------|
| `src/api/` | Fetch plumbing (`http.ts`), per-area endpoints (`endpoints/`), the assembled `api` object (`client.ts`), types (`types.ts`) |
| `src/components/` | Reusable UI components |
| `src/pages/` | Top-level pages (Dashboard, Providers, Models, etc.) |
| `src/context/` | React contexts (Theme, Toast, Event, Identity, Storage, SidebarMode, QuotaModal) |
| `src/hooks/` | Custom hooks |
| `src/i18n/` | i18next setup and the 29 locale catalogs |
| `src/lib/` | Icon registry and shared helpers |
| `src/data/` | Static presets |
| `src/utils/` | Helpers (formatting, SSE, model utils) |

Modules shared with the Front Desk SPA live in `web-shared/` and are imported through the
`@web-shared/*` alias. They are linted and coverage-gated as part of `web/`.

Key patterns:
- **API client:** `api/client.ts`, a typed facade over the `http.ts` fetch wrapper
- **Type safety:** all API responses typed via `api/types.ts`
- **Server state:** TanStack Query for fetching and caching; React Context (plus `useReducer`
  where the state is complex) for cross-cutting UI state
- **Routing:** React Router
- **Styling:** Tailwind CSS v4 with a custom theme, themed through semantic `ui-*` classes (below)

### Semantic `ui-*` classes

Themed UI styling goes through semantic classes defined in `web/src/index.css`, so a component
declares *what* an element is and `index.css` decides how it looks per UI style (`clean-saas`,
`cyber-terminal`, `glassmorphism-lite`) and per light/dark mode:

| Class | Variants |
|-------|----------|
| `ui-btn` | `-primary`, `-secondary`, `-danger` |
| `ui-badge` | `-neutral`, `-success`, `-error`, `-warning`, `-info`, `-accent`, `-orange`, `-purple`, `-cyan` |
| `ui-callout` | `-warning` (something the operator must not miss), `-info` (what a control does or sends where); inline notice boxes inside cards |
| `ui-card`, `ui-table`, `ui-tab`, `ui-toggle` | (single look each) |

Rules of thumb: never write CSS that targets raw Tailwind utility classes to restyle one component
(such selectors break silently when the component's classes change; add or extend a semantic
variant instead), never put raw palette stacks on `ui-btn`/`ui-badge` elements, and have tests
assert the semantic class (`toHaveClass("ui-badge-error")`), never a color utility. `index.css`
rules are unlayered and beat Tailwind utilities regardless of specificity, so size and padding set
on a `ui-*` class cannot be overridden with `px-*`/`text-*`.

## Docker Workflow

### Docker Compose Services

`docker-compose.yml` defines two services, plus an optional commented-out `apprise` container for
outbound alerting:

| Service | Description | Ports | Volumes |
|---------|-------------|-------|---------|
| `app` | Full Model Hotel application | `${HOST_PORT:-8081}:8080` | `./.data:/data`, plus the Docker socket read-only (commented out in the base file, mounted by `compose.dev.yml`) |
| `db` | PostgreSQL 16 | not published to the host | `./.data/pgdata:/var/lib/postgresql/data` |

Because `db` publishes no host port, a process outside Docker cannot reach it. That is why the
local-binary path in the Quick Start needs its own PostgreSQL.

### Running the Full Stack

Always go through the Makefile: every target passes both `-f docker-compose.yml -f
compose.dev.yml`, and a raw `docker compose` command silently drops the dev overlay.

```bash
make docker-up        # start
make docker-logs      # follow logs from all services
make docker-down      # stop
make docker-build     # down, then up -d --build
```

### Rebuild Process

The frontend is embedded in the Go binary, so **any change to `web/src/` (or to Go code) needs a
full rebuild:**

```bash
make docker-build
```

> **⚠️ Important:** `docker compose restart app` does **NOT** pick up frontend changes. You must rebuild.

The dev stack is a test bed, not a deployment, so rebuild it whenever a change needs verifying. Do
say so in a shared checkout: someone else may be looking at the running instance. Front Desk
changes are a separate build (`make ha-up`, or `make frontdesk-build` for a local binary); the app
rebuild does not touch it.

### Environment Variables (Docker)

Compose reads `.env` at the project root, but only the variables it interpolates. `MASTER_KEY`,
`POSTGRES_PASSWORD`, `ADMIN_TOKEN`, `HOST_PORT`, `POSTGRES_USER`, `POSTGRES_DB`, `WEBAUTHN_RP_ID`
and `WEBAUTHN_RP_ORIGINS` come from `.env`:

| Variable | Description | Default |
|----------|-------------|---------|
| `MASTER_KEY` | AES-256-GCM key for provider API keys | *required* |
| `POSTGRES_PASSWORD` | Database password | *required* |
| `ADMIN_TOKEN` | Admin auth token | Generated on first boot, printed once |
| `HOST_PORT` | Port published on the host | `8081` |
| `POSTGRES_USER` | Database user | `modelhotel` |
| `POSTGRES_DB` | Database name | `modelhotel` |

Others are pinned literally in `docker-compose.yml` (`ALLOW_HTTP_PROVIDERS=false`,
`RATE_LIMIT_ENABLED=true`, `DEBUG_LOG=false`, `DATA_DIR=/data`, `CORS_ORIGINS`), so setting them in
`.env` has no effect on the container: edit the compose file, or override them in the dev overlay.
`compose.dev.yml` already flips `DEBUG_LOG` to `true` and `ALLOW_EMBED` to `true`, and passes
`KNOWN_PROXIES`, `TRUSTED_PROXIES`, `LOG_FORMAT`, `METRICS_TOKEN` and the OTLP variables through
from `.env`.

`.env.example` lists every variable the binary itself reads. `POSTGRES_HOST` and `PORT` matter only
when running the binary outside Docker, since Compose sets both for the container.

## CI/CD Pipeline

The CI pipeline (`.github/workflows/ci.yml`) runs on pushes to `master` and on pull requests
targeting it:

| Job | Description |
|-----|-------------|
| `Changed Surfaces` | Classifies what the push or PR touched; every job below is gated on its output |
| `Go Test` | `go test -timeout 10m ./...` against PostgreSQL 16, then a 90% coverage threshold (`cmd/` and `tools/` excluded) |
| `Go Race` | `go test -race -count=1 -timeout 20m ./...` |
| `Go Lint` | `golangci-lint` v2.13 |
| `Go Vet` | `go vet ./...` |
| `Go Vulncheck` | `govulncheck`, which fails only on vulnerable functions the code actually calls |
| `i18n Check` | `make i18n-check`: locale parity, `{{placeholder}}` parity, plural forms, no non-allowlisted English |
| `Size Check` | `make size-check`, including the check that the allowlist only shrank since the base commit |
| `Workflow Lint` | `actionlint` over the workflow files |
| `Frontend Lint & Build` | `pnpm run lint`, `pnpm run format` (Biome check), `pnpm run build` |
| `Frontend Test (shard n/3)` | The dashboard vitest suite in three parallel shards, each writing a blob report |
| `Frontend Coverage Gate` | Merges the shard reports and enforces the 90% line threshold |
| `Front Desk Web Test` | Lint, format check, build, and the Front Desk suite at a 90% threshold |
| `Coverage Diff Gate` | PR-only: fails if under 90% of the PR's changed lines are covered (backend and both frontends) |
| `Coverage Aggregate` | master-only: overall coverage, fails below 90%, publishes the shields.io badge JSON to the `badges` branch |
| `Third-Party Notices` | Reruns `make notices` and fails if `THIRD-PARTY-NOTICES.md` is stale |
| `Docker Build` | Builds the image, then scans it with Trivy for fixable HIGH/CRITICAL vulnerabilities |
| `Publish :dev image` | master-only: pushes the `:dev` images to GHCR and Docker Hub once every gate above has passed |

CI is surface-scoped: gating is per job (`if:`), never `on.paths`, so a docs-only change runs no
suites while every required check still reports. The gate fails open, so a bug there wastes a run
rather than waving an untested merge through. Adding a job means adding its gate.

Other workflows cover CodeQL, image scanning, the Android app, Docker publishing and pruning, the
README/Docker Hub sync, and publishing this wiki.

### Git Hooks

`make setup` points `core.hooksPath` at `scripts/`, which installs two hooks.

**`scripts/pre-commit`** is formatting only, and aims to finish in under ten seconds:

1. Refuses the commit if the branch is behind `origin/master`
2. `gci` import ordering on staged `.go` files, re-staging anything it rewrites
3. `go vet` on the packages those files belong to
4. `biome check --write` on staged `web/**` TS/TSX/CSS files, re-staging anything it rewrites

**`scripts/pre-push`** is a scoped smoke test. Each step runs only if the surface it protects
changed: the README/`DOCKERHUB.md` sync guard and Docker Hub length limit, `golangci-lint` on
`./cmd/... ./internal/...`, `pnpm lint` plus `tsc -b` for `web/`, and a diff-coverage gate that
fails the push if under 90% of the changed lines are covered. The coverage gate runs whole test
suites and can take minutes; skip just that part with `NO_COVERAGE_PREPUSH=1`.

Never bypass either hook with `--no-verify`.

### Translations

Locale files in `web/src/i18n/locales/` are the single source of truth; translations are maintained in-repo. The workflow when adding user-facing strings:

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
   pnpm format:fix src/components/Changed.tsx
   pnpm lint
   ```
4. **Integration test:** Run the full suite if changes affect multiple packages
   ```bash
   make test-db-up
   go test -timeout 10m ./...
   ```
5. **Docker test:** Rebuild and check in the container
   ```bash
   make docker-build
   # Test via http://localhost:8081
   ```

### Debugging Techniques

#### Backend Debugging

1. **Debug logging** is already on in the dev stack (`compose.dev.yml` sets `DEBUG_LOG=true`).
   Outside Docker: `DEBUG_LOG=true ./bin/server`. `DEBUG_LOG_SCOPES=failover,resolve` turns on
   Debug records for named scopes only.

2. **Check app logs via API** (admin token required):
   ```bash
   curl -H "Authorization: Bearer $TOKEN" "http://localhost:8081/api/logs/app?limit=50"
   ```

3. **Database inspection:**
   ```bash
   docker compose -f docker-compose.yml -f compose.dev.yml exec db psql -U modelhotel -d modelhotel
   ```

4. **Test proxy endpoints:**
   See "Dev Proxy Endpoints" in `AGENTS.md` for curl examples, including how to get `$TOKEN`.

#### Frontend Debugging

1. **React DevTools:** Install browser extension for component inspection
2. **Network tab:** Monitor API calls in browser dev tools
3. **Console logs:** Check for JavaScript errors
4. **SSE events:** Monitor `/api/events` for real-time updates

#### Common Issues

| Issue | Solution |
|-------|----------|
| Biome reports "Linter process terminated abnormally" | You ran `pnpm biome` or `pnpm exec biome`. Use `pnpm format` / `pnpm format:fix` |
| Biome fails with config errors | Run from `web/`, with paths relative to `web/` |
| Tests fail with "connection refused" | Start the test database: `make test-db-up` |
| Frontend changes not appearing | Rebuild: `make docker-build` (a restart does not rebuild) |
| Admin token lost | `.data/admin-token` holds a SHA-256 hash only, so it cannot be read back. Delete the file and restart: a fresh token is generated and printed once to `make docker-logs`. To choose the token yourself, set `ADMIN_TOKEN` in `.env` before that restart, since the env var only seeds a first boot and is ignored while the file exists |
| Provider discovery fails | Check the logs, verify the provider key decrypts (a changed `MASTER_KEY` invalidates every stored key) |
| Commit refused as "behind master" | `git fetch origin && git merge origin/master`, then retry |

## Contributing

1. **Open an issue** to discuss large changes before implementation
2. **Create a feature branch** from `master` (pull requests only, never push to `master`)
3. **Install the hooks** with `make setup` and let them run: never `--no-verify`
4. **Write tests** for new functionality; the diff-coverage gate wants 90% of your changed lines
5. **Update documentation and locales** for user-facing changes
6. **Submit a pull request**

All contributions are licensed under the MIT License. See `CONTRIBUTING.md` for the full terms.

## Deployment Notes

### Production Build

```bash
# Build Docker image
docker build -t model-hotel:latest .

# Run with production environment
docker compose up -d
```

### Environment-Specific Configuration

- **Development:** `DEBUG_LOG=true` and `ALLOW_EMBED=true`, both set by `compose.dev.yml`. A plain
  HTTP provider (a local Ollama, say) also needs `ALLOW_HTTP_PROVIDERS=true`, which the base
  compose file pins to `false`, so add it to the overlay.
- **Production:** the base `docker-compose.yml` defaults are the production ones:
  `DEBUG_LOG=false`, `ALLOW_HTTP_PROVIDERS=false`, `ALLOW_EMBED=false`, rate limiting on.

### Backup and Restore

Backups the application writes itself land in `./.data/backups/` (`DATA_DIR` plus `backups`).
`./.data/pgdata/` is PostgreSQL's own data directory, not a backup location.

For a manual dump, use the PostgreSQL tools inside the `db` container:

```bash
# Backup
docker compose exec db pg_dump -U modelhotel modelhotel > backup.sql

# Restore
docker compose exec -T db psql -U modelhotel modelhotel < backup.sql
```

The application also supports periodic backup with son/father/grandfather rotation via the Settings UI (Database Backup section). When enabled, backups are created automatically at a configurable interval and old backups are pruned according to daily/weekly/monthly retention tiers. See [Configuration](Configuration) for the full list of backup settings.
