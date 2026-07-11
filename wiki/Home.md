# Model Hotel

> **Multi-Provider AI Gateway**

A single OpenAI-compatible endpoint that sits in front of all your LLM providers. Route requests across models using your own priority ordering, fail over automatically when a provider goes down, and see exactly where your tokens are going.

![Dashboard](screenshots/dashboard.png)

## Quick Start

```bash
git clone https://github.com/hugalafutro/model-hotel.git
cd model-hotel
cp .env.example .env   # Set MASTER_KEY and DATABASE_URL
docker compose up --build
```

See [[Development]] for local setup details.

## Key Features

- **Unified API** - One OpenAI-compatible surface for all providers: `/v1/chat/completions` plus multimodal endpoints (embeddings, image generation/edits/variations, text-to-speech, speech-to-text)
- **Hotel Routing** - Prefix models with `hotel/` to route through failover groups (works on every endpoint)
- **Transparent Failover** - Automatic retry on 5xx, 429, 401/403, 404, and timeouts
- **Circuit Breaker** - Per-provider circuit breaker prevents wasted requests
- **Virtual Keys** - Per-client API keys with rate limiting and usage tracking
- **Model Discovery** - Auto-sync 300+ models across 30+ providers, with a post-scan summary of what changed (added / re-enabled / disabled models, failover group updates)
- **Request Logging** - Full latency decomposition (TTFT, overhead, per-stage timing)
- **Privacy by Design** - Prompts are never logged, read, or stored
- **Interactive Chat & Arena** - Built-in UI for testing and comparing models

## Documentation

### Getting Started

- [[Configuration]] - Environment variables, runtime settings, appearance
- [[Development]] - Local setup, project structure, contributing

### Using

- [[Virtual Keys]] - Per-client API key management, rate limiting, usage tracking
- [[API Reference]] - Proxy and admin API endpoints with examples
- [[Request Logging]] - Latency decomposition, log management, app logs

### Operating

- [[Model Discovery]] - Automatic model synchronization with per-provider metadata
- [[Failover and Hotel Routing]] - Transparent failover, hotel routing, circuit breaker
- [[High Availability]] - Front Desk control plane + Traefik for drop-in multi-instance HA
- [[Bellhop]] - Android companion app: pair a phone with Front Desk and monitor the fleet

### Reference

- [[Security]] - AES-256-GCM encryption, Argon2id, SHA-256 hashing, URL validation
- [[Privacy]] - Data handling, what is and isn't captured, local deployment

## Architecture

![Architecture](screenshots/architecture-tree.svg)

Core packages: `proxy/` (streaming, failover), `provider/` (discovery, encryption), `failover/` (circuit breaker, routing), `virtualkey/` (auth, rate limiting), `model/` (caching, CRUD). PostgreSQL backend with 43 migrations.

See the [full architecture diagram](https://github.com/hugalafutro/model-hotel#architecture) in the README.
