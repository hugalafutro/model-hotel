# Model Hotel

> **Multi-Provider AI Gateway**

A single OpenAI-compatible endpoint that sits in front of all your LLM providers. Route requests across models using your own priority ordering, fail over automatically when a provider goes down, and see exactly where your tokens are going.

![Dashboard](screenshots/dashboard.png)

## Quick Start

```bash
git clone https://github.com/hugalafutro/model-hotel.git
cd model-hotel
cp .env.example .env
nano .env              # set MASTER_KEY and POSTGRES_PASSWORD; change HOST_PORT if 8081 is taken
docker compose up --build -d
```

To run a prebuilt image instead of building from source, edit `docker-compose.yml`: comment out the `build:` block and uncomment one of the `image:` lines. To skip the clone altogether, follow [[Deploy without Git]]: two files, no clone, no build.

On first start the server prints the admin token once, in a boxed `ADMIN TOKEN (save now ...)` panel. Read it with `docker compose logs app`, and save it: it is never shown again. If you lose it, delete `.data/admin-token` and restart to generate a new one. The `ADMIN_TOKEN` environment variable seeds the token on first boot only: once `.data/admin-token` exists the file wins and the variable is ignored.

Open `http://localhost:8081` (or the `HOST_PORT` you set), log in with that token, add your first provider, and start proxying. To stop, update or remove the stack, see [Stop, Update, Remove](#stop-update-remove).

For local development, use the dev stack with the `compose.dev.yml` overlay instead (Docker socket, `DEBUG_LOG`, embedding allowed; trusted environments only): see [Development](Development#quick-start).

### Stop, Update, Remove

`docker compose down` stops the stack and keeps your data. Both services use bind mounts under `./.data` (PostgreSQL in `./.data/pgdata`). There are no named volumes, so `down -v` removes nothing more.

To update a two-file deployment, run `docker compose pull && docker compose up -d`. Compose changes do not reach you on their own: diff the block in [[Deploy without Git]] against your file now and then, merge what changed by hand, keeping every local edit (the step 3 image switch, added `environment:` entries, an uncommented socket mount or apprise service), then run the same two commands. A clone that builds from source runs `git pull && docker compose pull --ignore-buildable && docker compose up --build -d` (the extra pull refreshes the PostgreSQL image, which `up --build` leaves alone). A clone switched to a prebuilt image has a local edit in `docker-compose.yml`, so run `git stash && git pull && git stash pop`; if the pop reports a conflict, remove the conflict markers in `docker-compose.yml`, keeping your `image:` line and upstream's other changes, then run `git restore --staged docker-compose.yml && git stash drop`. Finish with `docker compose pull && docker compose up -d`.

To remove everything, run `docker compose down --rmi all` (containers, network and the images the services use) and delete `./.data`. `./.data/pgdata` belongs to PostgreSQL (uid 70), so this needs `sudo rm -rf .data`; the rest of `.data` belongs to uid 1000, which usually matches your host user. `.env` and `docker-compose.yml` are yours to delete.

## Key Features

- **Unified API** - One OpenAI-compatible surface for all providers: `/v1/chat/completions` plus multimodal endpoints (embeddings, image generation/edits/variations, text-to-speech, speech-to-text, music generation on KoboldCpp)
- **Hotel Routing** - Prefix models with `hotel/` to route through failover groups (works on every endpoint)
- **Transparent Failover** - Automatic retry on 5xx, 401/403, 402, 404, 499, and timeouts, plus 429 while the `failover_on_rate_limit` setting is on (the default)
- **Circuit Breaker** - Health is tracked per provider and model: a repeatedly failing model is skipped, and a provider whose failures span enough of its models is taken out of rotation too
- **Virtual Keys** - Per-client API keys with rate limiting and usage tracking
- **Model Discovery** - Auto-sync models across 22 provider families, with a post-scan summary of what changed (added / re-enabled / disabled models, failover group updates)
- **Request Logging** - Full latency decomposition (TTFT, overhead, per-stage timing)
- **Privacy by Design** - Prompts are never logged, read, or stored
- **Interactive Chat & Arena** - Built-in UI for testing and comparing models

## Documentation

### Getting Started

- [[Deploy without Git]] - Run the published image from two files, no clone, no build
- [[Configuration]] - Environment variables, runtime settings, appearance
- [[Development]] - Local setup, project structure, contributing

### Using

- [[Virtual Keys]] - Per-client API key management, rate limiting, usage tracking
- [[Multi-User]] - Named dashboard accounts with per-user roles and scoped access
- [[API Reference]] - Proxy and admin API endpoints with examples
- [[Request Logging]] - Latency decomposition, log management, app logs

### Operating

- [[Model Discovery]] - Automatic model synchronization with per-provider metadata
- [[Failover and Hotel Routing]] - Transparent failover, hotel routing, circuit breaker
- [[Observability]] - Prometheus metrics, JSON logs, OTLP log export, and a provisioned Grafana dashboard
- [[Alerting]] - Outbound notifications for operational events, via Telegram, email, Discord, Slack, Matrix, webhooks, and more
- [[High Availability]] - Front Desk control plane + Traefik for drop-in multi-instance HA
- [[Bellhop]] - Android companion app: pair a phone with Front Desk and monitor the fleet
- [[CrowdSec]] - Parsers and scenarios that turn repeated auth failures into an edge ban

### Reference

- [[Security]] - AES-256-GCM encryption, Argon2id, SHA-256 hashing, URL validation
- [[Privacy]] - Data handling, what is and isn't captured, local deployment

## Architecture

![Architecture](screenshots/architecture-tree.svg)

Core packages: `proxy/` (routing, failover loop, streaming), `provider/` (discovery), `failover/` (groups, circuit breaker), `auth/` (provider key encryption), `virtualkey/` (client key auth), `ratelimit/` (rate limiting), `budget/` (dollar budgets), `model/` (caching, CRUD). PostgreSQL backend (schema managed by versioned migrations).
