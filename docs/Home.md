# Model Hotel

> **Multi-Provider AI Gateway**
>
> *"Because we have LiteLLM at home"*

A single OpenAI-compatible endpoint that sits in front of all your LLM providers. Route requests to the cheapest or fastest model, fail over automatically when a provider goes down, and see exactly where your tokens are going.

Built for when the popular option didn't autodiscover ~300 models across 4 providers. Then it spiraled a bit.

---

## Pages

| Page | Description |
|------|-------------|
| [Architecture](Architecture.md) | System structure, request flow, data model, frontend state management |
| [Providers](Providers.md) | Adding and managing LLM providers, type detection, keyless providers |
| [Failover & Hotel Routing](Failover-and-Hotel-Routing.md) | Transparent failover, `hotel/`-prefixed multi-provider routing, failover groups |
| [Virtual Keys](Virtual-Keys.md) | Per-client API key management, rate limiting, usage tracking |
| [Request Logging](Request-Logging.md) | Latency decomposition, overhead breakdown, log management |
| [Model Discovery](Model-Discovery.md) | Automatic model synchronization with rich per-provider metadata |
| [Provider Health](Provider-Health.md) | Model testing, quota/balance monitoring, system status sidebar |
| [Chat & Arena](Chat-and-Arena.md) | Interactive chat, conversation mode, arena tournaments and comparisons |
| [Real-Time Events & System Status](Real-Time-Events-and-System-Status.md) | SSE event bus, system stats polling, threshold warnings |
| [Configuration](Configuration.md) | Environment variables, database settings, runtime configuration |
| [API Reference](API-Reference.md) | Proxy and admin API endpoints with examples |
| [Security](Security.md) | AES-256-GCM encryption, Argon2id key derivation, SHA-256 hashing, URL validation |
| [Privacy](Privacy.md) | Data handling, what is and isn't captured, local deployment |
| [Development](Development.md) | Local setup, project structure, contributing |