<p align="center">
  <img src="docs/logo.svg" alt="Model Hotel">
</p>

<p align="center"><strong>Multi-Provider AI Gateway</strong></p>
<p align="center"><em>"Because we have LiteLLM at home"</em></p>
<br><br>

<p align="center">
  <img src="https://github.com/hugalafutro/model-hotel/actions/workflows/ci.yml/badge.svg" alt="CI">
  <img src="https://img.shields.io/github/license/hugalafutro/model-hotel" alt="License">
  <img src="https://img.shields.io/github/last-commit/hugalafutro/model-hotel" alt="Last Commit">
  <img src="https://img.shields.io/github/go-mod/go-version/hugalafutro/model-hotel" alt="Go Version">
  <br/>
  <img src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white" alt="Go">
  <img src="https://img.shields.io/badge/TypeScript-3178C6?logo=typescript&logoColor=white" alt="TypeScript">
  <img src="https://img.shields.io/badge/React-61DAFB?logo=react&logoColor=black" alt="React">
  <img src="https://img.shields.io/badge/PostgreSQL-4169E1?logo=postgresql&logoColor=white" alt="PostgreSQL">
  <img src="https://img.shields.io/badge/Docker-2496ED?logo=docker&logoColor=white" alt="Docker">
</p>

<div align="center">

> **AI-Assisted Project Disclaimer:**<br>Human judgment applied at every stage, particularly around architectural decisions, UX flows, and quality control.<br>
>
> Made in [CodeNomad](https://github.com/NeuralNomadsAI/CodeNomad) with [OpenCode](https://opencode.ai).<br>
>
> Meet the [oh-my-opencode-slim](https://github.com/alvinunreal/oh-my-opencode-slim) team:<br><br><img src="https://img.shields.io/badge/GLM_5.1-orchestrator,%20council,%20commit%20review-8B5CF6?style=flat" alt="GLM 5.1"> <img src="https://img.shields.io/badge/Kimi_K2.6-designer-06B6D4?style=flat" alt="Kimi K2.6"> <img src="https://img.shields.io/badge/DeepSeek_V4_Pro-oracle,%20council-E53E3E?style=flat" alt="DeepSeek V4 Pro"> <img src="https://img.shields.io/badge/Qwen3_Coder_480B-council-F59E0B?style=flat" alt="Qwen3 Coder"><br><img src="https://img.shields.io/badge/Devstral_2_123B-fixer-E8A842?style=flat" alt="Devstral 2"> <img src="https://img.shields.io/badge/MiniMax_M2.7-librarian,%20explorer-10B981?style=flat" alt="MiniMax M2.7"> <img src="https://img.shields.io/badge/Qwen3_VL_235B-observer-F59E0B?style=flat" alt="Qwen3 VL"><br><br><img src="https://img.shields.io/badge/Claude_Opus_4.7-code%20review-D97706?style=flat" alt="Claude Opus 4.7"> <img src="https://img.shields.io/badge/Grok_4-code_review-FF4500?style=flat" alt="Grok 4"><br>
>
> <i>Thanks <a href="https://ollama.com">Ollama Cloud</a> for generous limits. I have nothing nice to say about <a href="https://z.ai">Z.AI</a> or <a href="https://opencode.ai">OpenCode Go</a> in that regard.</i><br><br>

Powered by <a href="https://github.com/aovestdipaperino/tokensave">tokensave<br>
![Tokens Saved](https://img.shields.io/endpoint?url=https://tokens.o5.ddns.net/&link=https://github.com/aovestdipaperino/tokensave&cacheSeconds=1800)</a>
</div>
<br>
<details>
<summary>📊 OpenCode Statistics</summary>

```
┌────────────────────────────────────────────────────────┐
│                       OVERVIEW                         │
├────────────────────────────────────────────────────────┤
│Sessions                                            866 │
│Messages                                         24,579 │
│Days                                                 22 │
└────────────────────────────────────────────────────────┘

┌────────────────────────────────────────────────────────┐
│                    COST & TOKENS                       │
├────────────────────────────────────────────────────────┤
│Total Cost                                       $79.85 │
│Avg Cost/Day                                      $3.63 │
│Avg Tokens/Session                                 1.7M │
│Median Tokens/Session                            358.4K │
│Input                                            873.3M │
│Output                                             6.3M │
│Cache Read                                       589.0M │
│Cache Write                                        2.4M │
└────────────────────────────────────────────────────────┘


┌────────────────────────────────────────────────────────┐
│                      TOOL USAGE                        │
├────────────────────────────────────────────────────────┤
│ read               ████████████████████ 9863 (36.7%)   │
│ bash               ███████████          5867 (21.8%)   │
│ edit               ██████               3340 (12.4%)   │
│ grep               █████                2682 (10.0%)   │
│ glob               █                    877 ( 3.3%)    │
│ todowrite          █                    811 ( 3.0%)    │
│ tokensave_tokens.. █                    695 ( 2.6%)    │
│ task               █                    565 ( 2.1%)    │
│ tokensave_tokens.. █                    361 ( 1.3%)    │
│ write              █                    286 ( 1.1%)    │
│ tokensave_tokens.. █                    224 ( 0.8%)    │
│ sequential-think.. █                    164 ( 0.6%)    │
│ compress           █                    158 ( 0.6%)    │
│ tokensave_tokens.. █                    140 ( 0.5%)    │
│ webfetch           █                    101 ( 0.4%)    │
│ apply_patch        █                     62 ( 0.2%)    │
│ tokensave_tokens.. █                     55 ( 0.2%)    │
│ git_git_status     █                     48 ( 0.2%)    │
│ git_git_diff_uns.. █                     41 ( 0.2%)    │
│ warpgrep_codebas.. █                     39 ( 0.1%)    │
│ git_git_diff_sta.. █                     28 ( 0.1%)    │
│ grep_app_searchG.. █                     28 ( 0.1%)    │
│ git_git_add        █                     26 ( 0.1%)    │
│ git_git_commit     █                     25 ( 0.1%)    │
│ morph_edit         █                     24 ( 0.1%)    │
│ git_git_log        █                     23 ( 0.1%)    │
│ chrome-devtools_.. █                     18 ( 0.1%)    │
│ question           █                     18 ( 0.1%)    │
│ context7_query-d.. █                     17 ( 0.1%)    │
│ searxng_searxng_.. █                     17 ( 0.1%)    │
│ invalid            █                     16 ( 0.1%)    │
│ tokensave_tokens.. █                     16 ( 0.1%)    │
│ searxng_web_url_.. █                     15 ( 0.1%)    │
│ tokensave_tokens.. █                     14 ( 0.1%)    │
│ chrome-devtools_.. █                     14 ( 0.1%)    │
│ websearch_web_se.. █                     13 ( 0.0%)    │
│ auto_continue      █                     12 ( 0.0%)    │
│ websearch          █                     10 ( 0.0%)    │
│ chrome-devtools_.. █                     10 ( 0.0%)    │
│ chrome-devtools_.. █                     10 ( 0.0%)    │
│ ast_grep_replace   █                      9 ( 0.0%)    │
│ memory_remember    █                      9 ( 0.0%)    │
│ tokensave_tokens.. █                      8 ( 0.0%)    │
│ warpgrep_github_.. █                      7 ( 0.0%)    │
│ memory_recall      █                      6 ( 0.0%)    │
│ tokensave_tokens.. █                      5 ( 0.0%)    │
│ chrome-devtools_.. █                      5 ( 0.0%)    │
│ arch-linux_searc.. █                      5 ( 0.0%)    │
│ tokensave_tokens.. █                      4 ( 0.0%)    │
│ tokensave_tokens.. █                      4 ( 0.0%)    │
│ submit_plan        █                      4 ( 0.0%)    │
│ chrome-devtools_.. █                      4 ( 0.0%)    │
│ chrome-devtools_.. █                      4 ( 0.0%)    │
│ context7_resolve.. █                      3 ( 0.0%)    │
│ tokensave_tokens.. █                      3 ( 0.0%)    │
│ tokensave_tokens.. █                      3 ( 0.0%)    │
│ tokensave_tokens.. █                      3 ( 0.0%)    │
│ tokensave_tokens.. █                      3 ( 0.0%)    │
│ chrome-devtools_.. █                      3 ( 0.0%)    │
│ github_pull_requ.. █                      3 ( 0.0%)    │
│ tokensave_tokens.. █                      3 ( 0.0%)    │
│ chrome-devtools_.. █                      2 ( 0.0%)    │
│ tokensave_tokens.. █                      2 ( 0.0%)    │
│ tokensave_tokens.. █                      2 ( 0.0%)    │
│ skill              █                      2 ( 0.0%)    │
│ chrome-devtools_.. █                      2 ( 0.0%)    │
│ chrome-devtools_.. █                      2 ( 0.0%)    │
│ arch-linux_get_o.. █                      2 ( 0.0%)    │
│ memory_forget      █                      2 ( 0.0%)    │
│ github_get_me      █                      2 ( 0.0%)    │
│ tokensave_tokens.. █                      2 ( 0.0%)    │
│ tokensave_tokens.. █                      2 ( 0.0%)    │
│ ast_grep_search    █                      2 ( 0.0%)    │
│ chrome-devtools_.. █                      1 ( 0.0%)    │
│ chrome-devtools_.. █                      1 ( 0.0%)    │
│ github_delete_file █                      1 ( 0.0%)    │
│ tokenscope         █                      1 ( 0.0%)    │
│ git_git_show       █                      1 ( 0.0%)    │
│ github_get_file_.. █                      1 ( 0.0%)    │
│ chrome-devtools_.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ github_issue_write █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
│ envsitter_keys     █                      1 ( 0.0%)    │
│ memory_update      █                      1 ( 0.0%)    │
│ github_list_pull.. █                      1 ( 0.0%)    │
│ github_search_is.. █                      1 ( 0.0%)    │
│ tokensave_tokens.. █                      1 ( 0.0%)    │
└────────────────────────────────────────────────────────┘
```
</details>

---

A single OpenAI-compatible endpoint that sits in front of all your LLM providers. Route requests to the cheapest or fastest model, fail over automatically when a provider goes down, and see exactly where your tokens are going.

## [<img src="docs/icons/screenshots.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Screenshots](#-screenshots)

| | | | | |
|:---:|:---:|:---:|:---:|:---:|
| ![Dashboard](docs/screenshots/placeholder-1.png) | ![Providers](docs/screenshots/placeholder-2.png) | ![Models](docs/screenshots/placeholder-3.png) | ![Logs](docs/screenshots/placeholder-4.png) | ![Failover](docs/screenshots/placeholder-5.png) |
| ![Settings](docs/screenshots/placeholder-6.png) | ![Virtual Keys](docs/screenshots/placeholder-7.png) | ![Stats](docs/screenshots/placeholder-8.png) | ![Discovery](docs/screenshots/placeholder-9.png) | ![Proxy Usage](docs/screenshots/placeholder-10.png) |

## What It Does

### [<img src="docs/icons/providers.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> One Endpoint, Many Providers](#-one-endpoint-many-providers)
Add any OpenAI-compatible provider ([Anthropic](https://claude.ai/), [DeepSeek](https://deepseek.com/), [NanoGPT](https://docs.nano-gpt.com/), [Z.AI Coding Plan](https://z.ai/), [x.ai](https://x.ai/), [Google AI Studio](https://aistudio.google.com/), [Cohere](https://cohere.com/), [Ollama](https://github.com/ollama/ollama), [OpenCode](https://opencode.ai), [Groq](https://groq.com/), [OpenAI](https://openai.com/), or your own), and call them all through the same `/v1/chat/completions` endpoint. The proxy handles model ID mapping and failover transparently. Provider API keys are encrypted with AES-256-GCM at rest using your `MASTER_KEY`; only the proxy ever sees the decrypted credentials. Keyless providers (e.g. OpenCode Zen free models) are also supported (no API key required).

### [<img src="docs/icons/failover.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Transparent Failover](#-transparent-failover)
When a provider returns a 5xx, a 429 (rate limit, configurable via `failover_on_rate_limit`), an auth error (401/403), or times out, the request is automatically retried with the next available provider for that model. Failover decisions happen at the response-header layer, so the client never receives a partial stream from a provider that returned a non-2xx status. An exponential backoff (100ms base, capped at 2s) is applied between attempts to avoid hammering slow providers; client disconnects during backoff are detected immediately. The final request record logs the attempt number that succeeded (or the last one that failed), along with the error code and total duration. Per-attempt failover events (attempt number, provider, status code) are also written to the application log for real-time debugging.

### [<img src="docs/icons/hotel.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Hotel Routing](#-hotel-routing)
Prefix any model with `hotel/` to route through a failover group: an ordered list of providers that expose the same base model. Example: `hotel/gpt-4o` resolves to all providers whose model ID matches `gpt-4o` exactly (after stripping the org prefix, e.g. `openai/gpt-4o` → `gpt-4o`). Models with different base names like `gpt-4o-mini` are separate groups.

Requests are sent to each provider in priority order. If a provider responds with a server error (5xx), an auth error (401/403), or a rate-limit error (429, configurable), the next provider in the list is tried. Failover does **not** trigger on slow responses or client errors (4xx other than 401/403/429).

A per-provider **circuit breaker** prevents wasted requests to consistently failing providers. After 5 consecutive failures (connection errors, 5xx, 429, 401/403), the provider's circuit opens and it is skipped during candidate resolution. After a 60-second cooldown, the circuit transitions to half-open and allows a single probe request; if the probe succeeds, the circuit closes and normal traffic resumes. State transitions (open/closed) are published as SSE events for real-time dashboard visibility. The circuit breaker can be disabled entirely via the `circuit_breaker_enabled` setting.

Failover groups are auto-generated when models are discovered, but only when **2 or more providers** expose the same base model. Groups with a single provider are automatically disabled. You can manually edit priorities, disable individual entries, or toggle entire groups on or off.

### [<img src="docs/icons/virtualkeys.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Per-Client Virtual Keys](#-per-client-virtual-keys)
Issue separate API keys for different users or services. Each key is SHA-256 hashed before storage, so raw keys are never persisted. Track token usage per key, delete a key to immediately cut off access, and never expose your real provider credentials. Keys can be created and deleted from the dashboard or the admin API.

### [<img src="docs/icons/privacy.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> No Prompts Logged: Privacy by Design](#-no-prompts-logged)
> **Prompts and request content are never captured, logged, or inspected.**
> The proxy forwards requests to the provider exactly as received, without reading or modifying message contents.
>
> The only information recorded is what is strictly necessary to route and meter the request: timestamp, duration, latency, time-to-first-token (TTFT), token counts (including cache-hit/miss breakdown), tokens per second, HTTP status code, error messages (upstream provider failures only, never user content), proxy overhead breakdown (parse, model lookup, provider lookup, key decryption), streaming flag, failover attempt count, request state, virtual key identifier, and target provider/model identifiers.

The optional **Arena History** feature (disabled by default, configurable in **Settings → Arena History**) can persist completed arena and compare session results in your browser's local storage. When enabled:

- **Model-generated responses** (output text, thinking blocks, metrics) are stored locally so you can review past results.
- **Preset prompts and personas** are saved by reference (e.g. "Dilemma preset", "Merlin persona"), storing only their built-in IDs, never the text content you didn't write yourself.
- **Custom user-entered text is never logged.** If you type your own prompt or persona system prompt, it is intentionally excluded from history records. Only the fact that a custom prompt was used is recorded (shown as "Custom prompt" in the history UI), with no content retained.

History data never leaves your browser. It can be cleared at any time from the Settings page.

### [<img src="docs/icons/logging.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Request Logging with Overhead Breakdown](#-request-logging-with-overhead-breakdown)
Every request is logged with full latency decomposition:
- **TTFT** (time to first token)
- **Total duration** (end-to-end wall time)
- **Proxy overhead** split into request parsing, model/failover lookup, provider lookup, and key decryption
- **Tokens per second**, prompt / completion counts

Streaming requests are captured as they start and updated as they finish, so you can see in-flight requests in the Logs view. The overhead breakdown helps you determine whether latency is coming from your provider or from the proxy itself.

### [<img src="docs/icons/discovery.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Built-In Model Discovery](#-built-in-model-discovery)
Add a provider and the service pulls the model list automatically via the provider's own API. Models are kept in sync on a schedule you control (default every 6 hours, configurable). Nine providers get enriched metadata beyond what the generic OpenAI-compatible endpoint returns:

| Provider | Context Length | Pricing | Reasoning Flags | Input/Output Modalities | Source |
|---|---|---|---|---|---|
| DeepSeek | ✅ | ✅ | ✅ | *(none)* | Catalog |
| NanoGPT | ✅ | ✅ | ✅ | ✅ | API (`/models?detailed=true`) |
| Z.AI | ✅ | *(none)* | ✅ | Derived | Catalog |
| OpenCode Go | ✅ | ✅ | ✅ | ✅ | Catalog |
| OpenCode Zen | ✅ | ✅ | ✅ | ✅ | Catalog |
| OpenAI | ✅ | ✅ | ✅ | ✅ | Catalog |
| Anthropic | ✅ | ✅ | *(none)* | ✅ (partial) | API + Pricing catalog |
| xAI (Grok) | ✅ | ✅ | ✅ | ✅ | API (`/language-models`) + Catalog |
| Google AI Studio (Gemini) | ✅ | ✅ | ✅ | ✅ | API (`/v1beta/models`) + Pricing catalog |
| Cohere | ✅ | ✅ | ✅ | ✅ (vision) | API (`/v1/models`, paginated) + Pricing catalog |

DeepSeek, Z.AI, OpenCode (Go & Zen), and OpenAI use **dedicated static catalogs** that supply context length, pricing, capability flags, and modalities not available from the provider's `/models` endpoint. xAI uses a catalog for context windows and capabilities, enriched with live pricing from its `/language-models` endpoint (or falls back to pure catalog when the account has no API access: 403 or 429). Google AI Studio provides rich metadata (context, thinking support) from its native API, supplemented with a pricing catalog. Cohere uses its native API with full pagination for model discovery, enriched with a pricing catalog for cost data, capability detection (tool calling, vision, structured output, reasoning), and modality mapping. NanoGPT and Anthropic expose richer model metadata through their own APIs; Anthropic additionally uses a pricing catalog for per-model cost data. Ollama enriches models via its `/api/show` endpoint.

Models that aren't covered by any built-in catalog are automatically enriched from [models.dev](https://models.dev/), an open-source model catalogue that provides pricing, context limits, capabilities, and modality data for 40+ providers. The enrichment is non-destructive: it only fills fields that are empty or missing from the provider's own API response, never overwriting data that was already populated. If models.dev is unreachable, discovery proceeds normally using whatever data the provider returned, so your existing catalogue is never at risk.

### [<img src="docs/icons/health.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Model Health at a Glance](#-model-health-at-a-glance)
Test any model from the Models page with a single click. The test sends a minimal chat completion directly to the provider and reports total duration and the actual model response, so you know the provider is alive and responsive. DeepSeek providers show live account balance; NanoGPT and Z.AI providers show token quota and usage data, all fetched from their respective APIs and displayed on both the provider cards and the sidebar quota panel.

### [<img src="docs/icons/api.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Interactive Chat & Arena](#-interactive-chat--arena)
The dashboard includes a built-in **Chat** interface for testing models interactively, with support for system personas (presets or custom prompts), generation parameters (temperature, top_p, max_tokens, min_p, top_k, frequency/presence penalties), and streaming responses with collapsible thinking-block rendering. Vision-capable models show an image upload button: attach a photo for the model to describe or analyze. Audio-capable models show an audio upload button for sending audio input. Attachments are sent as OpenAI-compatible multimodal content parts (`image_url`, `input_audio`). Switch to **Conversation** mode to watch two models talk to each other: enter a starter prompt, set the number of rounds and optional delay between turns, and observe the back-and-forth with per-message metrics (duration, tokens, chars/sec).

**Arena** mode offers two sub-modes: **Competition** runs bracket tournaments where models face off in pairwise matchups. Vote for winners, and the bracket auto-advances to the next round until a champion emerges. **Compare** places two or more models in a grid with the same prompt for parallel evaluation, with per-slot personas and voting. Both modes support per-model generation parameters, streaming with thinking-block rendering, and per-response metrics. Past sessions are saved to an arena history modal for review and restoration.

### [<img src="docs/icons/settings.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Real-Time Events & System Status](#-real-time-events--system-status)
A live SSE event bus delivers toast notifications for discovery outcomes, model disabling events, token counting errors, circuit breaker state transitions, and stale-request alerts straight to the dashboard. Failover retries during proxying are logged but **not** pushed as SSE events. The sidebar polls system stats every 10 seconds, showing CPU, memory, disk I/O, and network throughput with color-coded warnings (orange at 75%, red at 90%). When running under Docker Compose, stats are aggregated across containers; otherwise, cgroup metrics are used. Goroutine count, database health (size, connections, cache hit ratio), API uptime, and process count are also displayed.

## [<img src="docs/icons/security.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Security & Privacy](#-security--privacy)

Provider API keys are encrypted at rest with AES-256-GCM. The `MASTER_KEY` is strengthened via **Argon2id** key derivation (with per-provider random salts) before use as the AES key. Virtual keys are SHA-256 hashed. The admin token is SHA-256 hashed before storage: the plaintext token is displayed once on first run and never stored on disk. To regenerate a lost token, delete the `admin-token` file in your configured `DATA_DIR` and restart. Standard security headers (X-Content-Type-Options, X-Frame-Options, X-XSS-Protection) are applied to all responses. Decrypted provider keys are cached in memory for up to 5 minutes to avoid repeated key derivation overhead.

## [<img src="docs/icons/quickstart.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Quick Start (Docker Compose)](#-quick-start-docker-compose)

```bash
git clone <repository-url>
cd model-hotel

cp .env.example .env
nano .env          # set a strong MASTER_KEY and DATABASE_URL

docker compose up --build
```

The admin token is displayed once in the logs on first run and will never be shown again:

```bash
docker compose logs app | grep "ADMIN_TOKEN="
```

If you lose the token, delete `.data/admin-token` and restart to generate a new one.

You can also set a fixed admin token via the `ADMIN_TOKEN` environment variable.

Open `http://localhost:8081`, log in with that token, add your first provider, and start proxying.

## [<img src="docs/icons/settings.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> Configuration](#-configuration)

### Environment Variables

> **Single-Instance Deployment:** Rate limits, circuit breakers, and caches are in-process. Not horizontally scalable without Redis/equivalent shared state.
>
| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MASTER_KEY` | Yes | - | Master encryption key for provider API keys |
| `DATABASE_URL` | Yes | - | PostgreSQL connection string |
| `PORT` | No | `:8080` | Server listen address |
| `DATA_DIR` | No | `./data` | Directory for admin token file |
| `ADMIN_TOKEN` | No | *(auto)* | Fixed admin token (auto-generated if empty) |
| `ALLOW_HTTP_PROVIDERS` | No | `false` | Allow HTTP provider URLs |
| `RATE_LIMIT_ENABLED` | No | `true` | Hard kill-switch for per-key rate limiting (env var only; when `false`, rate-limit middleware is a complete no-op) |
| `RATE_LIMIT_IP_RPS` | No | `30` | Per-IP requests per second (DoS safety net; always-on, not DB-configurable) |
| `RATE_LIMIT_IP_BURST` | No | `60` | Per-IP burst size for DoS protection token bucket |
| `MAX_REQUEST_SIZE` | No | `10485760` | Max request body in bytes (10MB) |
| `CORS_ORIGINS` | No | `http://localhost:5173,http://localhost:8081` | Allowed CORS origins (comma-separated) |
| `ALLOWED_PROVIDER_HOSTS` | No | *(empty)* | Additional allowed provider hosts (comma-separated; built-in provider hosts are always allowed) |
| `TRUSTED_PROXIES` | No | *(empty)* | Trusted proxy CIDRs (comma-separated). When set, `X-Forwarded-For` and `X-Real-IP` headers are only honored from these IPs. When empty, forwarded headers are ignored and only `RemoteAddr` is used. |
| `DATABASE_MAX_CONNS` | No | `25` | Maximum database connection pool size |
| `DATABASE_MIN_CONNS` | No | `5` | Minimum database connection pool size |
| `MODELSDEV_ENABLED` | No | `true` | Enable enrichment from [models.dev](https://models.dev/) catalogue (pricing, capabilities, context limits) |

### DB-Backed Settings

| Setting | Default | Description |
|---|---|---|
| `rate_limit_enabled` | `true` | Runtime toggle for rate limiting (overridden by `RATE_LIMIT_ENABLED` env kill-switch) |
| `rate_limit_rps` | `10` | Requests per second per virtual key (`0` for unlimited) |
| `rate_limit_burst` | `20` | Burst size for rate limiter token bucket |
| `discovery_interval` | `6h` | Interval between automatic model discovery runs |
| `discovery_on_startup` | `true` | Run model discovery on server startup |
| `discovery_on_provider_create` | `true` | Run discovery when a new provider is created |
| `log_retention` | *(none)* | Log retention period |
| `stale_request_timeout` | *(none)* | Timeout for stale/in-flight requests |
| `failover_on_rate_limit` | `true` | Enable failover to another provider on 429 rate-limit errors |
| `circuit_breaker_enabled` | `true` | Enable per-provider circuit breaker for hotel/ failover routes |
| `circuit_breaker_threshold` | `5` | Consecutive failures before a provider's circuit opens |
| `circuit_breaker_cooldown` | `60s` | Duration before an open circuit transitions to half-open |
| `theme` | *(none)* | UI theme preference |
| `ui_style` | *(none)* | UI style preference |
| `accent_color` | *(none)* | UI accent color |
| `dashboard_refresh` | *(none)* | Dashboard auto-refresh interval |
| `quota_refresh` | *(none)* | Provider quota refresh interval |
| `history_limit` | *(none)* | History display limit |
| `toast_duration` | *(none)* | Toast notification duration (ms, 1000–15000) |

> **Rate Limiting:** Two layers of protection run on every request:
>
> 1. **Per-IP limiter** (always-on): Env-var-only ceiling (`RATE_LIMIT_IP_RPS` / `RATE_LIMIT_IP_BURST`) that blocks floods before they reach auth. Mounts first in the middleware chain so it catches unauthenticated brute-force attempts. Not exposed in the UI; this is a safety rail, not a tuning knob.
> 2. **Per-key limiter** (runtime-configurable): When `RATE_LIMIT_ENABLED=true` (the default), rate limiting can be toggled on/off at runtime via the **Settings** UI and the DB-backed settings above. Each virtual key gets its own independent token bucket. Setting `RATE_LIMIT_ENABLED=false` in the environment completely disables per-key limiting regardless of DB settings.
>
> 429 responses from both layers include `Retry-After` and `X-RateLimit-*` headers. The `X-RateLimit-Scope` header (`ip` or absent) distinguishes which layer triggered the rejection.

## [<img src="docs/icons/api.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> API Endpoints](#-api-endpoints)

### Proxy API (`/v1/*`): OpenAI-compatible, requires a virtual key

| Endpoint | Method | Description |
|---|---|---|
| `/v1/models` | GET | List available models (OpenAI format) |
| `/v1/chat/completions` | POST | Chat completions (supports `"stream": true` for SSE streaming) |

```bash
export PROXY_KEY="your-proxy-key"

curl http://localhost:8081/v1/models \
  -H "Authorization: Bearer $PROXY_KEY"

curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "hotel/gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'

# Streaming example
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "hotel/gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

### Admin API (`/api/*`): requires the admin token

**Providers**

| Endpoint | Method | Description |
|---|---|---|
| `/api/providers` | POST | Create provider |
| `/api/providers` | GET | List providers |
| `/api/providers/{id}` | GET | Get provider |
| `/api/providers/{id}` | PUT | Update provider |
| `/api/providers/{id}` | DELETE | Delete provider |
| `/api/providers/discover-all` | POST | Discover models for all providers |
| `/api/providers/refresh-quotas` | POST | Refresh quotas for all providers |
| `/api/providers/{id}/discover` | POST | Discover models for a provider |
| `/api/providers/{id}/usage` | GET | Get provider usage (Z.AI, NanoGPT) |
| `/api/providers/{id}/balance` | GET | Get provider balance (DeepSeek) |

**Models**

| Endpoint | Method | Description |
|---|---|---|
| `/api/models` | GET | List models (optional `?provider_id=` filter) |
| `/api/models/{id}` | PATCH | Update model |
| `/api/models/{id}` | DELETE | Delete model |
| `/api/models/{id}/test` | POST | Test model connectivity |

**Virtual Keys**

| Endpoint | Method | Description |
|---|---|---|
| `/api/virtual-keys` | POST | Create virtual key |
| `/api/virtual-keys` | GET | List virtual keys |
| `/api/virtual-keys/{id}` | GET | Get virtual key |
| `/api/virtual-keys/{id}` | DELETE | Delete virtual key |

**Request Logs**

| Endpoint | Method | Description |
|---|---|---|
| `/api/logs` | GET | List request logs (paginated, filterable) |
| `/api/logs/purge` | DELETE | Purge logs (`1h`, `1d`, `1w`, `1m`, `all`) |

**App Logs**

| Endpoint | Method | Description |
|---|---|---|
| `/api/logs/app` | GET | Get app logs (ring buffer or `?history=true` DB query) |
| `/api/logs/app` | DELETE | Clear app logs |

**Settings**

| Endpoint | Method | Description |
|---|---|---|
| `/api/settings` | GET | Get all settings |
| `/api/settings` | PUT | Update settings |

**System & Stats**

| Endpoint | Method | Description |
|---|---|---|
| `/api/system` | GET | System stats (memory, DB, Docker) |
| `/api/stats` | GET | Aggregate request stats |
| `/api/stats/timeseries` | GET | Time series stats |
| `/api/stats/provider-distribution` | GET | Provider distribution stats |

**Failover Groups**

| Endpoint | Method | Description |
|---|---|---|
| `/api/failover-groups` | GET | List failover groups |
| `/api/failover-groups` | POST | Create failover group |
| `/api/failover-groups/sync` | POST | Sync all failover groups |
| `/api/failover-groups/candidates` | GET | List candidate models for failover |
| `/api/failover-groups/by-model/{model_uuid}` | GET | Get failover group by model UUID |
| `/api/failover-groups/{id}` | GET | Get failover group |
| `/api/failover-groups/{id}` | PUT | Update failover group |
| `/api/failover-groups/{id}` | DELETE | Delete failover group |

**Events (SSE)**

| Endpoint | Method | Description |
|---|---|---|
| `/api/events` | GET | Server-sent event stream |

**Admin Chat Proxy**

| Endpoint | Method | Description |
|---|---|---|
| `/api/chat/chat` | POST | Chat completions (admin-authenticated) |
| `/api/chat/arena` | POST | Arena completions (admin-authenticated) |
| `/api/chat/completions` | POST | Completions (admin-authenticated) |

### Health Check

| Endpoint | Method | Description |
|---|---|---|
| `/health` | GET | Returns `OK` (no auth required) |

## [<img src="docs/icons/license.svg" width="20" height="20" style="vertical-align:middle;margin-right:6px;" alt=""> License](#-license)

[MIT](LICENSE). See [CONTRIBUTING.md](CONTRIBUTING.md) for the contributor license agreement.