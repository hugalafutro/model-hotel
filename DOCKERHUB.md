# Model Hotel

[github.com/hugalafutro/model-hotel](https://github.com/hugalafutro/model-hotel)

*"Because we have LiteLLM at home"*

**Multi-Provider AI Gateway**

![Github CI](https://github.com/hugalafutro/model-hotel/actions/workflows/ci.yml/badge.svg) ![Go Version](https://img.shields.io/github/go-mod/go-version/hugalafutro/model-hotel) [![golangci-lint](https://github.com/hugalafutro/model-hotel/actions/workflows/lint.yml/badge.svg)](https://github.com/hugalafutro/model-hotel/actions/workflows/lint.yml) ![TypeScript](https://img.shields.io/badge/TypeScript-3178C6?logo=typescript&logoColor=white) ![React](https://img.shields.io/badge/React-61DAFB?logo=react&logoColor=black) ![PostgreSQL](https://img.shields.io/badge/PostgreSQL-4169E1?logo=postgresql&logoColor=white)  ![Coverage](https://codecov.io/github/hugalafutro/model-hotel/branch/master/graph/badge.svg) ![GitHub Repo stars](https://img.shields.io/github/stars/hugalafutro/model-hotel)


> **AI-Assisted Project Disclaimer:**
> Human judgment applied at every stage, particularly around architectural decisions, UX flows, and quality control.

A single OpenAI-compatible endpoint that sits in front of all your LLM providers. Models are auto-discovered the moment you add a provider and optionally on schedule; failover groups form automatically around shared model names and retry transparently when a provider goes down; no prompt data is ever stored. Full feature tour, screenshots, and the security/auth breakdown live on [GitHub](https://github.com/hugalafutro/model-hotel#readme).

> **Live demo:** poke around a real instance at [mh.site19.ddns.net](https://mh.site19.ddns.net) - rebuilds fresh every 30 minutes.

---

## Quick Start

```bash
git clone https://github.com/hugalafutro/model-hotel.git
cd model-hotel

cp .env.example .env
nano .env          # set a strong MASTER_KEY and POSTGRES_PASSWORD

docker compose -f docker-compose.yml -f compose.dev.yml up --build -d
```

To use the prebuilt image instead of building from source, edit `docker-compose.yml`: comment out `build: .` and uncomment the `image:` line.

The admin token is shown once in the logs on first run and never again:

```bash
docker compose -f docker-compose.yml -f compose.dev.yml logs app | grep "ADMIN_TOKEN="
```

If you lose the token, delete `.data/admin-token` and restart to generate a new one, or set a fixed token via the `ADMIN_TOKEN` environment variable.

Open `http://localhost:8081`, log in with that token, add your first provider, and start proxying.

> **Security:** The Docker socket is disabled by default; the `compose.dev.yml` override enables it for local development - only use it in trusted environments.

## Deploy without Git

No `git clone` needed. Create two files and go:

**1.** Create `.env` with your secrets:

```bash
# Generate strong secrets:
#   MASTER_KEY:       openssl rand -base64 32
#   POSTGRES_PASSWORD: openssl rand -hex 16
#   ADMIN_TOKEN:      openssl rand -hex 16   (optional; auto-generated if empty)

MASTER_KEY=<your-master-key>
POSTGRES_PASSWORD=<your-postgres-password>
ADMIN_TOKEN=

# Optional: WebAuthn/FIDO2 passkey login
# WEBAUTHN_RP_ID=your-domain.com
# WEBAUTHN_RP_ORIGINS=https://your-domain.com
```

**2.** Create `docker-compose.yml`:

<!-- AUTO-SYNC: docker-compose.yml start -->
<details>
<summary>docker-compose.yml (click to expand, then copy)</summary>

```yaml
    name: model-hotel
    services:
        app:
            # Build from source (default):
            build:
                context: .
                args:
                    VERSION: ${VERSION:-dev}
                    COMMIT: ${COMMIT:-unknown}
            # Prebuilt images (uncomment 1 image according to registry preference, comment out build above):
            # image: ghcr.io/hugalafutro/model-hotel:latest
            # image: hugalafutro/model-hotel:latest
            labels:
                app.group: model-hotel
            ports:
                - "${HOST_PORT:-8081}:8080"
            environment:
                - MASTER_KEY=${MASTER_KEY:?MASTER_KEY must be set in .env}
                - POSTGRES_USER=${POSTGRES_USER:-modelhotel}
                - POSTGRES_PASSWORD=${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set in .env}
                - POSTGRES_HOST=db
                - POSTGRES_DB=${POSTGRES_DB:-modelhotel}
                - ADMIN_TOKEN=${ADMIN_TOKEN:-}
                - ALLOW_HTTP_PROVIDERS=false
                - ALLOW_EMBED=false
                - DATA_DIR=/data
                - RATE_LIMIT_ENABLED=true
                - DEBUG_LOG=false
                - CORS_ORIGINS=http://localhost:5173,http://localhost:${HOST_PORT:-8081}
                - WEBAUTHN_RP_ID=${WEBAUTHN_RP_ID:-}
                - WEBAUTHN_RP_ORIGINS=${WEBAUTHN_RP_ORIGINS:-}
                - ALLOWED_PROVIDER_HOSTS=
                - TRUSTED_PROXIES=
                - KNOWN_PROXIES=
            volumes:
                - ./.data:/data
                # Docker socket (disabled by default for security).
                # Enable to show container-level stats in the sidebar (CPU, memory per container).
                # ⚠️  Granting Docker socket access allows the container to control the Docker daemon.
                #     Only enable if you trust the deployment environment.
                # - /var/run/docker.sock:/var/run/docker.sock:ro
            restart: unless-stopped
            depends_on:
                db:
                    condition: service_healthy
    
        db:
            image: postgres:16-alpine
            labels:
                app.group: model-hotel
            command: ["postgres", "-c", "log_min_error_statement=panic", "-c", "log_min_messages=error", "-c", "log_checkpoints=off"]
            environment:
                - POSTGRES_USER=${POSTGRES_USER:-modelhotel}
                - POSTGRES_PASSWORD=${POSTGRES_PASSWORD:?POSTGRES_PASSWORD must be set in .env}
                - POSTGRES_DB=${POSTGRES_DB:-modelhotel}
            volumes:
                - ./.data/pgdata:/var/lib/postgresql/data
            restart: unless-stopped
            healthcheck:
                test: ["CMD-SHELL", "pg_isready -U ${POSTGRES_USER:-modelhotel}"]
                interval: 5s
                timeout: 5s
                retries: 5
    
        # Optional: outbound alerting via Apprise. Uncomment to run a stateless
        # apprise-api container, then in Settings → Alerts set the Apprise API URL to
        # http://apprise:8000 and paste your notification target (e.g.
        # tgram://<bot_token>/<chat_id>). Model Hotel POSTs event summaries here and
        # Apprise fans them out to your service. No request content is ever sent.
        # apprise:
        #     image: caronc/apprise:latest
        #     labels:
        #         app.group: model-hotel
        #     restart: unless-stopped
        #     # Not exposed to the host: only Model Hotel needs to reach it.
        #     expose:
        #         - "8000"
```

</details>
<!-- AUTO-SYNC: docker-compose.yml end -->

**3.** Deploy:

```bash
docker compose up -d
```

> **Note:** For development, layer `compose.dev.yml`. For the prebuilt image, uncomment the `image:` line and comment out `build: .`. `WEBAUTHN_RP_ID` enables passkey login (empty to disable); `TRUSTED_PROXIES` trusts inbound `X-Forwarded-For` headers from reverse proxies; `KNOWN_PROXIES` allows outbound connections to internal LLM servers on private networks (bypasses SSRF protection). See the [Configuration wiki](https://github.com/hugalafutro/model-hotel/wiki/Configuration) for every variable.

## High Availability

A single instance keeps its caches and rate limiters in memory, so to survive a host failure you run several instances behind one client endpoint. A **Front Desk** control plane holds the fleet roster and replicates config to every member, and **Traefik** load-balances them with health checks and automatic failover; members share one `MASTER_KEY` so encrypted provider keys port across the fleet. Full runbook in the [High Availability guide](https://github.com/hugalafutro/model-hotel/wiki/High-Availability).

## API Example

```bash
# List available models
curl http://localhost:8081/v1/models \
  -H "Authorization: Bearer $VIRTUAL_KEY"

# Chat completion (hotel/ routing for automatic failover across providers)
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "hotel/gpt-4o", "messages": [{"role": "user", "content": "Hello!"}]}'

# Speech-to-text (multimodal endpoints share the same provider/model and hotel/ routing)
curl -X POST http://localhost:8081/v1/audio/transcriptions \
  -H "Authorization: Bearer $VIRTUAL_KEY" \
  -F model="OpenAI/whisper-1" -F file=@speech.mp3
```

The proxy also serves `/v1/embeddings`, `/v1/rerank`, `/v1/images/generations|edits|variations`, `/v1/audio/speech` (TTS), `/v1/audio/translations`, and a native Anthropic `POST /v1/messages` (for Claude Code / anthropic SDKs, with cross-provider failover) - all transparent OpenAI-compatible pass-through. See the [API Reference](https://github.com/hugalafutro/model-hotel/wiki/API-Reference).

## Security & Authentication

Provider keys: AES-256-GCM at rest (`MASTER_KEY`, Argon2id-derived). Virtual keys and the admin token: SHA-256 hashed. Outbound SSRF/DNS-rebinding protection. Optional login: WebAuthn passkey, TOTP, and OIDC/GitHub SSO. Details in the [Security guide](https://github.com/hugalafutro/model-hotel/wiki/Security). **No prompt or request content is ever logged** - see [Privacy](https://github.com/hugalafutro/model-hotel/wiki/Privacy).

## Metrics & log shipping

Prometheus at `/metrics` (set `METRICS_TOKEN` so the scrape config carries no admin token). `LOG_FORMAT=json` emits structured stdout logs for Fluent Bit / Vector / Promtail / Datadog; `OTEL_EXPORTER_OTLP_ENDPOINT` pushes them to an OTel collector. `DEBUG_LOG=true` for verbose, `DEBUG_LOG_SCOPES=failover,ratelimit` to scope it. See the [Configuration wiki](https://github.com/hugalafutro/model-hotel/wiki/Configuration).

## Backup & Restore

Backups from the Settings page or `POST /api/backups` (`pg_dump --format=custom`); the `.dump` holds providers, models, virtual keys, failover groups, and settings.

```bash
# Direct
pg_restore --clean --if-exists -d YOUR_DB backup_file.dump

# Via Docker
docker exec -i postgres-container pg_restore --clean --if-exists -U user -d dbname < backup_file.dump
```

**Critical requirements:** `MASTER_KEY` must match (provider keys decrypt with it; a mismatch leaves every provider dead). The admin token lives in `DATA_DIR/admin-token` on the filesystem, not the database (lost → auto-regenerated on next boot). Virtual keys are SHA-256 hashes only - plaintext is never persisted, so lost keys are irrecoverable by design.

## Full Documentation

- [Configuration](https://github.com/hugalafutro/model-hotel/wiki/Configuration) - environment variables, runtime settings, Docker Compose
- [API Reference](https://github.com/hugalafutro/model-hotel/wiki/API-Reference) - proxy and admin endpoints
- [Security](https://github.com/hugalafutro/model-hotel/wiki/Security) - encryption, hashing, URL validation
- [Privacy](https://github.com/hugalafutro/model-hotel/wiki/Privacy) - what is and isn't captured
- [Failover and Hotel Routing](https://github.com/hugalafutro/model-hotel/wiki/Failover-and-Hotel-Routing) - failover groups, circuit breaker, backoff
- [Model Discovery](https://github.com/hugalafutro/model-hotel/wiki/Model-Discovery) - automatic sync, provider enrichment
- [Virtual Keys](https://github.com/hugalafutro/model-hotel/wiki/Virtual-Keys) - client key management
- [Request Logging](https://github.com/hugalafutro/model-hotel/wiki/Request-Logging) - log fields, overhead breakdown
- [High Availability](https://github.com/hugalafutro/model-hotel/wiki/High-Availability) - Front Desk + Traefik multi-instance HA
- [Development](https://github.com/hugalafutro/model-hotel/wiki/Development) - local setup, build, contributing

## Known Limitations

- **Single-instance only**: caches and rate limiters are in-memory. For multi-instance automatic failover, use the [Front Desk + Traefik HA stack](https://github.com/hugalafutro/model-hotel/wiki/High-Availability).

## License

[MIT](https://github.com/hugalafutro/model-hotel/blob/master/LICENSE). See [CONTRIBUTING.md](https://github.com/hugalafutro/model-hotel/blob/master/CONTRIBUTING.md) for the contributor license agreement.
