# Model Hotel dashboard (`web/`)

React + TypeScript single-page app for the Model Hotel gateway: providers, models, failover groups,
virtual keys, users, security, audit, logs, settings, plus the chat and arena testing surfaces. It
builds to `web/dist/`, which is copied into `cmd/server/static/` and embedded in the Go binary, so
in production the backend serves it from its own origin.

Setup, the Docker workflow, CI, git hooks, translations and the semantic `ui-*` design system are
documented in the
[Development wiki](https://github.com/hugalafutro/model-hotel/wiki/Development). This page is the
short orientation for working inside `web/`.

## Stack

- React 19 with TypeScript, Vite 8, React Router 8
- TanStack Query for server state; React Context for cross-cutting UI state (theme, toasts, events,
  identity, storage, sidebar mode, quota modal)
- Tailwind CSS 4, themed through semantic `ui-*` classes defined in `src/index.css`
- Phosphor icons; i18next with 29 locale catalogs (every user-facing string goes through `t()`)
- Vitest, Testing Library and MSW for tests; ESLint and Biome for lint and formatting

## Layout

| Path | What lives there |
|------|------------------|
| `src/api/` | `http.ts` (fetch wrapper, `ApiError`, cookie auth helpers), `endpoints/` per area, `client.ts` (the assembled typed `api` facade), `types.ts` / `types/` |
| `src/components/` | Reusable UI, including the virtualized log tables |
| `src/pages/` | Top-level screens, most a `Page.tsx` plus a `Page/` directory of its parts, a few a directory only: Dashboard, Providers, Models, FailoverGroups, VirtualKeys, Logs (request and app logs), Users, Security, Audit, Settings, Chat, Arena |
| `src/context/` | Theme, Toast, Event (SSE), Identity, Storage, SidebarMode, QuotaModal |
| `src/hooks/` | Custom hooks (`useLocalStorage`, `useModels`, `useQuotaData`, `useIdleLogout`, ...) |
| `src/i18n/` | i18next setup and `locales/*.json` |
| `src/lib/`, `src/utils/`, `src/data/` | Icon registry, helpers (formatting, SSE parsing, model utils), static presets |
| `src/test/` | Vitest setup and shared test helpers |

Modules shared with the Front Desk SPA live in `../web-shared/` (cookies, quota, alerts, i18n,
device, clipboard, ...) and are imported through the `@web-shared/*` alias, which is configured in
both `vite.config.ts` and `vitest.config.ts`. They are linted and coverage-gated as part of `web/`.

## Commands

Node 24 or newer (the floor CI runs; the Docker builder image is `node:26-alpine`) and pnpm
10.33.0, pinned by the `packageManager` field in `package.json`. Run everything from `web/`, with
paths relative to `web/`.

```bash
pnpm install
pnpm dev                                  # UI on http://localhost:5173, no API proxy (see below)
pnpm build                                # tsc -b, then vite build -> web/dist/
pnpm exec tsc -b                          # typecheck alone (what the pre-push hook runs)
pnpm lint                                 # ESLint over web/ and web-shared/
pnpm format                               # Biome check, no writes (what CI runs)
pnpm format:fix src/components/Foo.tsx    # Biome format + lint, writing fixes
pnpm test                                 # Vitest, single run
pnpm test:watch
pnpm vitest run --coverage                # full suite, about 85 seconds; run it once
```

Never run `pnpm biome` or `pnpm exec biome`. Under `pnpm exec` the native lint worker is killed and
Biome misreports it as `Linter process terminated abnormally (possibly out of memory)`. Use the
`format` / `format:fix` scripts, or call `./node_modules/.bin/biome` directly.

## Talking to the backend

`API_BASE` is the empty string in `src/api/http.ts`, and `vite.config.ts` configures no proxy, so
requests always resolve against the serving origin. Nothing in `src/` reads `import.meta.env`;
there are no `VITE_*` variables to set. In practice that means `pnpm dev` serves the UI with hot
reload but cannot reach the API: use the Docker stack on `:8081` for anything that talks to the
backend. Because the built assets are embedded in the Go binary, `web/src/` changes need
`make docker-build` from the repo root; `docker compose restart` does not pick them up.

## Auth and events

Auth is a cookie session, not a bearer token. The server sets an httpOnly `mh_session` cookie that
the browser attaches automatically on same-origin requests, plus a readable `mh_csrf` cookie that
acts as the client-visible "logged in" signal. `getAuthHeaders()` echoes that token in an
`X-CSRF-Token` header on mutating requests; `fetchOK` strips it from GET and HEAD, and calls
`clearAuth()` on a 401 so `isAuthenticated()` flips false. No token is ever stored in
`localStorage`.

Server-sent events go through `src/context/EventContext.tsx`, which opens
`fetch("/api/events", { credentials: "same-origin" })` and parses the stream with `readSSEStream`
(`src/utils/sse.ts`) rather than using `EventSource`, so the cookie session, the 401 path and
reconnect backoff are all handled explicitly. Incoming events invalidate the relevant TanStack
Query caches and raise toasts.

## localStorage

Only UI preferences, never credentials. `StorageContext` owns the persistence toggles
(`persistChat`, `persistArena`, `persistConversation`, `arenaHistoryEnabled`, `arenaHistoryLimit`).
`ThemeContext` owns `theme` (`dark`, `light` or `system`), `uiStyle` (`clean-saas` by default, plus
`cyber-terminal` and `glassmorphism-lite`) and `accentColor`. The remaining keys are per-page view
modes, chart ranges, toast settings, sidebar state and unsent chat or arena drafts, plus
`i18nextLng` from the language detector.

## Testing

Test files live in a `__tests__/` directory beside the code they cover, for example
`src/components/__tests__/Foo.test.tsx` (a few older `src/utils/` tests still sit next to their
source). The environment is jsdom with `TZ` pinned to UTC, `globals: true`, `retry: 2` and a 15s
timeout; MSW handles network stubbing. Coverage settings, including the `web-shared/` include
pattern, live in `vitest.config.ts`.

## Resources

- [Main project README](../README.md)
- [Development](https://github.com/hugalafutro/model-hotel/wiki/Development)
- [API Reference](https://github.com/hugalafutro/model-hotel/wiki/API-Reference)
- [Configuration](https://github.com/hugalafutro/model-hotel/wiki/Configuration)
