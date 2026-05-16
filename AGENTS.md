# Agent Rules

## Project
Multi-provider LLM gateway. Go backend (`cmd/server/`, `internal/`), TypeScript/React frontend (`web/`), PostgreSQL, Docker Compose. Module: `github.com/hugalafutro/model-hotel`, Go 1.25.

## Commands

### Backend
```bash
make build                    # go build -o bin/server ./cmd/server/
make test                     # go test -v ./...
make run                      # build + ./server
go test ./internal/proxy/...  # single package
go test -timeout 5m ./...     # CI-equivalent (requires DB)
golangci-lint run ./...       # must pass; includes gci, govet
```
Requires PostgreSQL (`make docker-up`). CI uses PostgreSQL 16.

### Frontend (MUST run from `web/`)
```bash
pnpm dev          # Vite dev server :5173
pnpm build        # TypeScript + Vite build
pnpm lint         # ESLint
pnpm biome check --write src/components/Foo.tsx  # format + lint fix (paths relative to web/)
pnpm biome check src/utils/bar.ts                # check only
```

**COVERAGE:** exact command, do not modify: `cd web && pnpm vitest run --coverage 2>&1 | tail -200`
- `pnpm test --coverage` silently strips the flag. Use the command above.
- Do not modify the pipeline. Do not grep/glob/read source to estimate coverage.
- Do not run partial suites expecting coverage. Full suite required.
- `2>&1 | tail -200` captures the table past Node's ExperimentalWarning spam.
- ~85s, CPU-intensive. Run ONCE per request. Report the summary block.

**BACKEND COVERAGE:** `go test -timeout 5m -coverprofile=coverage.out -covermode=atomic ./...`
- CI uploads `coverage.out` to Codecov with `flags: backend`.
- `codecov.yml` excludes `cmd/` and `tools/` from coverage (entry points, not business logic).
- Frontend lcov also uploaded to Codecov with `flags: frontend`.
- Badge reflects combined coverage after exclusions.

**BIOME:** always run from `web/` with paths relative to `web/`. `src/components/Foo.tsx` correct, `web/src/components/Foo.tsx` wrong. "configuration resulted in errors" = ran from wrong dir.

## Pre-commit
1. `go test ./...` passes
2. `golangci-lint run ./...` passes
3. `pnpm lint` (from `web/`) passes
4. `pnpm biome check` (from `web/`) passes
5. Do NOT run Vite build for pre-commit.
6. Both ESLint and Biome must pass.

## Editing
- **Tool priority:** tokensave tools then native `edit`. Do NOT use native `edit` for `.tsx`/`.ts` (mangles tabs).
  - `str_replace` for single replacement, `multi_str_replace` for multiple, `insert_at` for before/after anchor.
  - If tokensave returns `unsupported file type`, fall back to native `edit`.
- **`insert_at` drift:** each insertion shifts subsequent line numbers. Prefer `str_replace`/`multi_str_replace` (string anchors are drift-proof). If using `insert_at`, use string anchors not line numbers. Never call `insert_at` twice on the same file without re-reading. Do not duplicate the anchor line in `content`.
- **Search priority:** TokenSave `search`/`context`/`body` then `grep` (non-symbol text only).
- Targeted edits only. Never rewrite an entire file to fix one thing.
- Retry failed tool calls before switching approaches. 3 retries minimum.
- Do not use scripts (Python, sed, awk) when edit tools work.
- Do not change unrelated code.
- After editing `web/src/` files: `pnpm lint` AND `pnpm biome check --write <file>` (from `web/`, relative path).
- `tokensave_*` and `edit` use project-relative paths (e.g. `web/src/Foo.tsx`).

## Code Style
- Tab indentation everywhere (Biome enforces). Native `edit` silently converts tabs to spaces; fix with `pnpm biome check --write <file>`.
- Follow existing patterns in the file being edited.

## Logging & Events (MANDATORY)
- Use `internal/debuglog` for structured logging. `debuglog.Info(...)` normal, `debuglog.Error(...)` failures. Never `fmt.Println`/`log.Println`.
- Publish SSE events via `events.Publish(events.Event{Type, Severity, Message, Metadata})` for user-facing ops. Domain-prefixed types (`"backup.created"`, `"provider.deleted"`). Severity: `"success"`/`"info"`/`"warning"`/`"error"`. Non-`"request."` types auto-become frontend toasts.

## Testing Proxy Endpoints

```bash
# Get admin token (NEVER extract from DB or decrypt provider keys)
TOKEN=$(docker compose exec -T app env | grep ADMIN_TOKEN | cut -d= -f2-)

# List providers
curl -s http://localhost:8081/api/providers -H "Authorization: Bearer $TOKEN" | python3 -c "
import json,sys
for p in json.load(sys.stdin): print(f\"{p['name']:40s} models={p['model_count']}\")"

# Test model via admin chat proxy (no virtual key needed)
# Model format: "{provider_name}/{model_id}" (spaces allowed)
curl -s http://localhost:8081/api/chat/chat \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"model":"Ollama Cloud/gemma3:4b","messages":[{"role":"user","content":"Hello"}],"stream":false}'
```
- Admin chat: `POST /api/chat/chat`, `/api/chat/arena`, `/api/chat/completions` (admin token auth)
- `/v1/chat/completions` requires virtual key (external clients)
- `.data/admin-token` = SHA-256 hash only; plaintext from Docker env

## Testing
- After editing `.go`: `go test ./path/to/package/...`
- After editing API endpoints: stress test suite (`tools/stress-test/`) if available

### Frontend Testing
- `pnpm vitest run` (from `web/`), single file: `pnpm vitest run src/components/__tests__/Foo.test.tsx`
- Coverage: see COVERAGE block above.
- Test infra: `web/src/test/utils.tsx` (renderWithProviders), `test/helpers.ts` (MSW factories, `getByDialogName`), `test/mocks/` (handlers, data, server), `test/setup.ts` (MockEventSource, mocks, MSW lifecycle)
- Prefer `getByRole`, `getByLabelText`, `getByPlaceholderText`, `getByDialogName`. Avoid `getByTestId`, `querySelector`, `getByText`.
- MSW factories: `server.use(...mockProviders())`, override with `{ status: 500 }` or `{ body: [] }`. `mockAllDefaults()` for all endpoints.
- Accessible components required: `aria-label` on icon buttons, `aria-labelledby` on modals, `<label htmlFor>` on inputs, `aria-label` on hidden file inputs.

## Architecture

### Backend (`internal/`)
| Package | Purpose |
|---------|---------|
| `admin` | Admin token management (SHA-256 hashed) |
| `api` | HTTP handlers/routes: CRUD (providers, models, virtual keys, failover groups, settings), backup, app log ingestion (ring buffer + slog handler), SSE endpoint |
| `auth` | API key encryption (AES-256-GCM + Argon2id), LRU cache for decrypted keys |
| `config` | Env/config loading, trusted proxy CIDR parsing |
| `ctxkeys` | Shared context key constants (cycle avoidance) |
| `db` | PostgreSQL pool, embedded SQL migrations, `WaitForReady` |
| `debuglog` | Structured slog wrapper (DEBUG_LOG env control) |
| `events` | SSE event bus (in-memory pub/sub) |
| `failover` | Failover group repo + routing, circuit breaker, cache |
| `model` | Model entity, repository (CRUD + upsert/disable), in-memory cache |
| `provider` | Provider CRUD, model discovery (OpenAI, Anthropic, DeepSeek, Google, Cohere, Ollama, NanoGPT, KoboldCPP, LMStudio, OpenCode Go/Zen, OpenRouter, xAI, zAI), models.dev enrichment |
| `proxy` | OpenAI-compatible proxy (`/v1/chat/completions`), admin chat (`/api/chat/*`), virtual key auth, rate limiting, failover routing, streaming, request logging |
| `ratelimit` | Token-bucket: per-virtual-key (`Limiter`) and per-IP (`IPLimiter`) |
| `settings` | DB-backed settings with 30s cache TTL, SSE subscriptions, typed getters |
| `util` | Docker stats/compose, HTTP helpers, system stats, URL sanitization |
| `virtualkey` | Virtual key CRUD + SHA-256 hashing, auth middleware |

### Frontend (`web/src/`)
| Directory | Contents |
|-----------|----------|
| `api/` | `client.ts` (fetch wrapper), `types.ts` (all API types incl. multimodal) |
| `components/` | Reusable UI: modals, tables, badges, pickers, markdown, thinking block, sliders |
| `pages/` | Dashboard, Chat, Arena, Providers, Models, FailoverGroups, VirtualKeys, Logs, AppLogs, Settings. Each has sub-dir for local components/hooks/utils |
| `context/` | Theme, Event (SSE), Toast, Storage, SidebarMode, QuotaModal |
| `hooks/` | `useDisableModel`, `useLocalStorage`, `useModels`, `useQuotaData`, `useRecommendedSettings` |
| `utils/` | format, model, params, providerBrands, recommendedSettings, sse, stagger, thinking, arenaHistory, paramCompat |
| `data/` | `presets.ts` (prompt/model presets) |

Frontend embedded in Go binary (`cmd/server/static/`). Chat supports vision + audio input via OpenAI-compatible content parts. Proxy is transparent pass-through.

### Routing
- `/v1/chat/completions` = proxy (virtual key required)
- `/api/chat/chat`, `/api/chat/arena`, `/api/chat/completions` = admin chat (admin token)
- `hotel/` prefix in model names = failover group routing
- Virtual API keys: SHA-256 hashed, never stored plaintext

## Security (NON-NEGOTIABLE)
- NEVER expose/log/commit `*.zmrd.uk` domains (exception: `model-hotel-dev.zmrd.uk` in this file only).
- NEVER log/expose API keys, MASTER_KEY, admin tokens, or secrets.
- Provider keys: AES-256-GCM + Argon2id. Decrypted material stays in `internal/provider/`.
- Virtual keys: SHA-256 hashed. Never store/log plaintext.
- Do not weaken/bypass auth middleware.

## Dev Container
Dev environment: `model-hotel-dev.zmrd.uk`. Only permitted `.zmrd.uk` reference. `model-hotel.zmrd.uk` must NOT appear.

## Environment
Required: `MASTER_KEY`, `DATABASE_URL`.

Optional: `PORT` (:8080), `DATA_DIR` (./data), `ADMIN_TOKEN` (auto-generated), `ALLOW_HTTP_PROVIDERS`, `RATE_LIMIT_ENABLED` (false = not mounted), `RATE_LIMIT_IP_RPS` (30), `RATE_LIMIT_IP_BURST` (60), `MAX_REQUEST_SIZE` (10MB), `CORS_ORIGINS`, `ALLOWED_PROVIDER_HOSTS`, `DATABASE_MAX_CONNS` (25), `DATABASE_MIN_CONNS` (5), `MODELSDEV_ENABLED` (true), `DEBUG_LOG` (false), `TRUSTED_PROXIES` (CIDRs).

## Documentation
- README for user-facing changes only. Code is source of truth.
- NEVER dispatch @fixer for prose/docs. Use @oracle.

## Commits
- Do not commit without passing tests.
- Do not commit commented-out code or debug prints.
- Do not push before confirming CI. Ask user first.

## Deployment
Frontend embedded in Go binary. `web/src/` edits require: `docker compose build app && docker compose up -d app`. `docker compose restart app` does NOT pick up changes. MUST ASK before rebuilding (other agents may be working).

## Memory Tools
- "remember"/"note" = `memory_remember` with `type`/`scope`. Do not just claim you will remember.
- Before feature/refactor, check `memory_recall`.

## TokenSave (PRIORITY: USE FIRST)

Code graph index of this repo. Always prefer over read/grep/glob for exploration.

### Path Scoping
| Scope | `path` |
|-------|--------|
| Backend | `internal/` |
| Server entry | `cmd/` |
| Frontend | `web/src/` |

Omit `path` for project-wide. Graph excludes `node_modules`/`dist`.

### Tool Selection
- **Symbol lookup:** `search` (not grep/glob+read)
- **Pre-implementation context:** `context` with `task=`, `include_code=true` (replaces multiple reads)
- **Function source:** `body` with `symbol=`, `limit=3`
- **Callers/callees:** `callers`/`callees` with `node_id`; `callers_for` for batch
- **Qualified name lookup:** `by_qualified_name`
- **Blast radius:** `impact` with `node_id`
- **File public API:** `module_api` with `path`
- **Test mapping:** `test_map`; high-risk untested: `test_risk`
- **Change impact:** `diff_context` with `files=`
- **Commit/PR drafting:** `commit_context`, `pr_context`
- **Code quality:** `todos`, `circular`, `complexity`, `god_class`, `health`, `coupling`, `simplify_scan`

### Coverage Caveat
`test_risk`/`test_map` measure static test presence, not actual coverage. For real numbers: `go test -cover ./...` (backend), `cd web && pnpm vitest run --coverage 2>&1 | tail -200` (frontend).

### Workflow
1. Before reading files: `context` or `search`. Only `read` for editing.
2. Before grepping symbols: `search`. `grep` for non-symbol text only.
3. Before implementing: `context` with `task=` description.
4. After editing: `diff_context` on changed files.
5. Before committing: `commit_context`.
6. For review: `simplify_scan` on changed files.

### Node IDs
Get from `search`, `context`, or `module_api` results.

### Budget
`context` has **7 calls per session**. Use `search`/`body` for simple lookups, reserve `context` for architecture questions.

### Session Health
`session_start` before work, `session_end` after. `health` with `details=true` for breakdown.

## Comment Maintenance
- Stale comments are worse than none. Update/remove affected comments with every edit.
- Fix stale comments on sight, even in unrelated work.
- Remove resolved TODOs/FIXMEs.

## Safety Net (NON-NEGOTIABLE)
- NEVER work around the safety net.
- Blocked operation = use safe alternative or ask user. Do not split commands, find alternate flags, or retry with variant syntax to dodge.
- Safe alternatives: `-d` not `-D`, `--force-with-lease` not `--force`.

## Git History (NON-NEGOTIABLE)
- NEVER use `git reset` or `git commit --amend` without explicit user permission.
- These commands rewrite history and can destroy other agents' commits.
- Always create a new commit for your changes. Let the user decide if consolidation is needed.
- If your commit is not at HEAD, do not try to move it. A new commit on top is always safe.

## Prohibitions
- Do not add dependencies without asking.
- Do not add features while fixing a bug.
- Do not change padding.
- Do not use em dashes in documentation. Use colons, parentheses, periods, or commas.
