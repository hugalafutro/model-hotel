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
| [[Architecture]] | System structure, request flow, data model, frontend state management |
| [[Providers]] | Adding and managing LLM providers, type detection, keyless providers |
| [[Failover & Hotel Routing]] | Transparent failover, `hotel/`-prefixed multi-provider routing, failover groups |
| [[Virtual Keys]] | Per-client API key management, rate limiting, usage tracking |
| [[Request Logging]] | Latency decomposition, overhead breakdown, log management |
| [[Model Discovery]] | Automatic model synchronization with rich per-provider metadata |
| [[Provider Health]] | Model testing, quota/balance monitoring, system status sidebar |
| [[Chat & Arena]] | Interactive chat, conversation mode, arena tournaments and comparisons |
| [[Real-Time Events & System Status]] | SSE event bus, system stats polling, threshold warnings |
| [[Configuration]] | Environment variables, database settings, runtime configuration |
| [[API Reference]] | Proxy and admin API endpoints with examples |
| [[Security]] | AES-256-GCM encryption, Argon2id key derivation, SHA-256 hashing, URL validation |
| [[Privacy]] | Data handling, what is and isn't captured, local deployment |
| [[Development]] | Local setup, project structure, contributing |