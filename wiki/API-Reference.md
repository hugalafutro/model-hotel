# 🔗 API Reference

Model Hotel exposes three API surfaces: the **Proxy API** (OpenAI-compatible, for client applications), the **Admin API** (for management and configuration), and a **Health** endpoint without authentication.

## Proxy API (`/v1/*`)

OpenAI-compatible endpoints that require a virtual key. The proxy covers chat completions plus the multimodal OpenAI API surface (embeddings, images, audio); everything else (fine-tuning, files, assistants, batches) is intentionally out of scope: Model Hotel is a proxy, not a full API gateway.

### Authentication

```
Authorization: Bearer <virtual-key>
```

Virtual keys use the `sk-` prefix (e.g. `sk-a1b2c3d4e5f6a7b8`). Keys are created via the Admin API and are shown only once at creation time.

The `x-api-key` header (what the Anthropic SDKs send) carries the virtual key just as well, on every `/v1` route rather than only `/v1/messages`. `Authorization: Bearer` is preferred when both are present.

### Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/v1/models` | GET | Virtual Key | List available models (OpenAI-compatible format) |
| `/v1/chat/completions` | POST | Virtual Key | Chat completion (streaming and non-streaming) |
| `/v1/messages` | POST | Virtual Key | Anthropic Messages API (translation + native passthrough) |
| `/v1/embeddings` | POST | Virtual Key | Embeddings (JSON pass-through) |
| `/v1/rerank` | POST | Virtual Key | Document rerank (JSON pass-through, Cohere-style body) |
| `/v1/images/generations` | POST | Virtual Key | Image generation (JSON; SSE streaming via `partial_images`) |
| `/v1/images/edits` | POST | Virtual Key | Image edits (multipart upload) |
| `/v1/images/variations` | POST | Virtual Key | Image variations (multipart upload) |
| `/v1/audio/speech` | POST | Virtual Key | Text-to-speech (binary audio response; SSE via `stream_format`) |
| `/v1/audio/transcriptions` | POST | Virtual Key | Speech-to-text (multipart upload) |
| `/v1/audio/translations` | POST | Virtual Key | Speech translation to English (multipart upload) |

All endpoints share the same model routing (`hotel/<model>` failover or `<provider>/<model>` direct), virtual-key authentication, `allowed_providers` access control, rate limiting, circuit breaker, and request logging. See [Multimodal Endpoints](#multimodal-endpoints) below.

### GET `/v1/models`

Returns the model list in OpenAI-compatible format.

The list is scoped to what the calling key may actually call: the key's own `allowed_providers` intersected with its owner account's provider cap, the same pair a chat request is routed through. A key restricted on neither side sees the whole catalogue, so this is a no-op unless an operator has deliberately restricted access. A `hotel/` failover group stays listed while any entry in its priority order sits on a provider the caller may reach, and is described by the entry that would actually serve the request.

**Response:**
```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o",
      "object": "model",
      "created": 1234567890,
      "owned_by": "openai"
    }
  ]
}
```

### POST `/v1/chat/completions`

Chat completion endpoint supporting both streaming and non-streaming modes.

**Request:**
```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "hotel/glm-4.6",
    "messages": [{"role": "user", "content": "Hello!"}],
    "stream": true
  }'
```

**Request Body Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `model` | string | Yes | Model identifier in format `hotel/<model>` (failover) or `<provider>/<model>` (direct) |
| `messages` | array | Yes | Array of message objects with `role` and `content` |
| `stream` | boolean | No | Enable streaming responses (default: `false`) |
| `temperature` | number | No | Sampling temperature |
| `max_tokens` | integer | No | Maximum completion tokens |
| `top_p` | number | No | Nucleus sampling parameter |
| `frequency_penalty` | number | No | Frequency penalty |
| `presence_penalty` | number | No | Presence penalty |
| `stop` | array/string | No | Stop sequences |
| `stream_options` | object | No | Streaming options (e.g. `include_usage: true`). **Note:** The proxy automatically injects `stream_options: {include_usage: true}` for all streaming requests to ensure token usage is reported. |

**Message normalization:** if a message in `messages` carries `tool_calls: []` (an empty array), the proxy removes the field before forwarding. Some clients serialize aborted or filtered tool-call turns this way, and strict providers reject the whole request with a 400 (`Invalid 'messages[N].tool_calls': empty array`), which permanently breaks any conversation carrying such a turn in its history. Non-empty `tool_calls` and all other message content pass through untouched.

**OpenAI Responses API re-route:** OpenAI's newest models refuse function tools combined with reasoning on `/v1/chat/completions`, and the pro tier (`o1-pro`, `o3-pro`, `gpt-5.x-pro`) is not served there at all. When a direct OpenAI candidate refuses for either reason, the proxy retries the same request against that provider's `/v1/responses`, translated in both directions, and remembers the requirement per model so later requests go straight there.

This is invisible to the client: ordinary Chat Completions in and out, streaming included, with the model's reasoning summary surfaced as `reasoning_content` and token usage metered as usual. Every `/v1/responses` call sends `store: false`, so OpenAI retains no conversation state. See [Failover & Hotel Routing](Failover-and-Hotel-Routing).

**Model Routing:**

- `hotel/<model>` - Failover routing (tries all providers that offer the model, with automatic failover on 5xx and optionally on 429)
- `<provider>/<model>` - Direct routing to a specific named provider (no failover)

**Streaming Response Format:**
```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

### POST `/v1/messages`

The native [Anthropic Messages API](https://docs.anthropic.com/en/api/messages) surface, so Claude Code and the anthropic SDKs can drive the gateway directly and fail over across every provider in a `hotel/` group, not just Claude. Model routing is identical to the rest of the proxy: send `hotel/<group>` or `<provider>/<model>` in the `model` field. Authenticate with `x-api-key` (what Anthropic clients send) or `Authorization: Bearer`.

```bash
curl -X POST http://localhost:8081/v1/messages \
  -H "x-api-key: $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "hotel/claude-sonnet-4-6",
    "max_tokens": 1024,
    "stream": true,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'
```

Two serving modes are chosen automatically, per failover attempt:

- **Native passthrough** when the resolved candidate is an Anthropic-family provider: the original Messages body is forwarded to the provider's own `/v1/messages` and the response is streamed back verbatim, so extended-thinking blocks (`thinking` / `signature_delta`), `cache_control`, and fine-grained tool streaming survive end to end.
- **Translation** for every other provider: the request is converted to the OpenAI Chat Completions shape (system prompt, text, vision, `tools`, `tool_choice`, and multi-turn `tool_use`/`tool_result`), run through the same pipeline, and the OpenAI response, SSE stream, or error is converted back to the Anthropic wire format on the way out. A Gemini 3 candidate signs each tool call and refuses the next turn without the signature; the `tool_use` block has no member for it, so the translation carries it inside the block's `id` as an opaque suffix in the id alphabet (around a kilobyte), the client echoes the id back on the `tool_use` block and the `tool_result` as it does any id, and the translation recovers the signature on the way in. A later attempt served natively by an Anthropic provider gets the ids with the suffix stripped. Treat `tool_use` ids as opaque strings of any length.

Because the choice is per attempt, a single `hotel/claude-*` request served natively by Anthropic transparently fails over to a translated provider (e.g. another vendor offering the same model) if Anthropic is unavailable. The translated path drops `thinking` output (v1); use a provider that serves the model natively to preserve it. Streaming, tool use, multi-turn tool results, vision, and Anthropic-shaped errors are all supported. Token usage is metered the same way as chat and **request/response content is never logged**.

### Multimodal Endpoints

The multimodal endpoints are **transparent pass-through**: the request is forwarded to the resolved provider with only the `model` field rewritten to the upstream model ID, and the provider's response is returned verbatim (JSON, SSE stream, or binary audio). The proxy extracts token counts from the `usage` object for metering; request and response **content is never inspected or logged** (see [Privacy](Privacy)).

Failover applies the same way as chat: with `hotel/<model>` routing, an upstream 5xx/429/401/403/404 moves to the next provider in the group. Failover happens only before any response byte has been forwarded; once a binary or SSE stream starts, the proxy is committed to that provider.

#### POST `/v1/embeddings`

```bash
curl -X POST http://localhost:8081/v1/embeddings \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "OpenAI/text-embedding-3-small", "input": ["Hello!", "World"]}'
```

Standard OpenAI body (`input`, `dimensions`, `encoding_format`); response is the provider's embeddings list with `usage.prompt_tokens` metered.

#### POST `/v1/rerank`

```bash
curl -X POST http://localhost:8081/v1/rerank \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "Cohere/rerank-v3.5", "query": "what is a capybara", "documents": ["The capybara is a giant rodent.", "Paris is the capital of France."], "top_n": 2}'
```

Cohere-style rerank body (`query`, `documents`, `top_n`), the de-facto standard shape also served by Jina, Voyage, and local TEI servers; the response (`results` with `index` and `relevance_score`) is returned verbatim. Cohere providers are routed to the native `https://api.cohere.com/v2/rerank` endpoint automatically (rerank is not part of Cohere's OpenAI-compatibility surface); any other provider receives the request at `<base URL>/rerank`. Cohere rerank models are auto-discovered alongside chat models. Token metering is best-effort: providers reporting `usage.total_tokens` (Jina, Voyage) are metered, Cohere's search-unit billing meters as zero tokens.

#### POST `/v1/images/generations`

```bash
curl -X POST http://localhost:8081/v1/images/generations \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "OpenAI/gpt-image-1", "prompt": "a hotel for AI models", "size": "1024x1024"}'
```

All provider parameters (`n`, `size`, `quality`, `response_format`, `style`, `output_format`, `partial_images`, `stream`) pass through untouched. Streaming partial images (SSE) are forwarded verbatim.

#### POST `/v1/images/edits` and `/v1/images/variations`

`multipart/form-data` with `image` file(s), `model`, and the provider's other form fields. The proxy parses the form once to read `model`, then rebuilds it per failover candidate with the resolved model ID; file bytes are forwarded unmodified.

#### POST `/v1/audio/speech`

```bash
curl -X POST http://localhost:8081/v1/audio/speech \
  -H "Authorization: Bearer $PROXY_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model": "OpenAI/gpt-4o-mini-tts", "input": "Welcome to Model Hotel", "voice": "alloy"}' \
  --output speech.mp3
```

**Gemini TTS models** (`gemini-2.5-flash-preview-tts` and kin on Google AI Studio or Vertex AI express) have no speech route on Google's OpenAI-compatibility layer, so the proxy serves them through `generateContent` instead of passing the request through: the text becomes a request for an `AUDIO` response with a `speechConfig` naming the voice, and the PCM the model answers with is delivered as the audio the client asked for. What that means for the request:

- `response_format` may be `wav` (the default when absent, in place of OpenAI's mp3) or `pcm` (raw 16-bit mono at the model's 24 kHz). The compressed formats (`mp3`, `opus`, `aac`, `flac`) need an encoder the gateway does not carry and are refused with a 400 naming the two it takes; in a `hotel/` group holding a model that does produce them, the request fails over to that model instead.
- `voice` takes OpenAI's names (`alloy`, `echo`, `fable`, `onyx`, `nova`, `shimmer`, `ash`, `ballad`, `coral`, `sage`, `verse`, `cedar`, `marin`), each mapped to a Gemini prebuilt voice, or any Gemini voice name (`Kore`, `Puck`, `Zephyr`, ...) as it is. Absent means `Kore`.
- `speed` and `instructions` have no counterpart and are ignored; Gemini takes delivery style from the text itself ("Say cheerfully: ...").
- `stream_format` is not honoured; the whole clip is returned at once.

Usage is metered from the token counts the answer reports.

Except for the Gemini TTS models described above, the response is the provider's raw binary audio (Content-Type passed through, e.g. `audio/mpeg`), streamed with no buffering, and `stream_format: "sse"` responses stream through as SSE.

#### POST `/v1/audio/transcriptions` and `/v1/audio/translations`

```bash
curl -X POST http://localhost:8081/v1/audio/transcriptions \
  -H "Authorization: Bearer $PROXY_KEY" \
  -F model="OpenAI/whisper-1" -F file=@speech.mp3
```

`multipart/form-data` with `file` and `model` (plus optional `language`, `response_format`, `temperature`, etc.). Returns the provider's JSON (`{"text": ...}`) or alternate `response_format` output verbatim.

> **Upload size:** request bodies are capped by the `MAX_REQUEST_SIZE` environment variable (default 50MB, which accommodates OpenAI's 25MB audio limit plus multipart overhead). See [Configuration](Configuration).

### Rate Limiting

Per-key rate limiting applies based on virtual key configuration. Returns `429 Too Many Requests` when exceeded.

```json
{
  "error": {
    "message": "rate limit exceeded",
    "type": "rate_limit_error",
    "code": 429
  }
}
```

The message names the limiter that refused: `rate limit exceeded` for a per-key or per-IP request cap, `user rate limit exceeded` for the caller's account-wide request cap, `token rate limit exceeded` and `user token rate limit exceeded` for the tokens-per-minute equivalents.

---

## Admin API (`/api/*`)

Requires the admin token for all management operations.

### Authentication

```
Authorization: Bearer <admin-token>
```

The admin token is generated on first startup and saved to `.data/admin-token`. It is shown only once in the startup logs.

### Providers

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/providers` | GET | List all providers (with model counts and token totals) |
| `/api/providers` | POST | Create a provider |
| `/api/providers/{id}` | GET | Get provider details |
| `/api/providers/{id}` | PUT | Update provider |
| `/api/providers/{id}` | DELETE | Delete provider |
| `/api/providers/{id}/discover` | POST | Trigger manual model discovery |
| `/api/providers/{id}/usage` | GET | Get usage/quota info (Z.AI, Nano-GPT, OpenRouter, NeuralWatt, Kimi Code, MiniMax) |
| `/api/providers/{id}/balance` | GET | Get balance info (DeepSeek) |
| `/api/providers/{id}/account` | GET | Get account info (Ollama Cloud) |
| `/api/providers/discover-all` | POST | Trigger discovery for all enabled providers |
| `/api/providers/refresh-quotas` | POST | Refresh quota/balance data for all supported providers |
| `/api/discovery/changes/ack` | POST | Mark model changes recorded by background (scheduled/startup) discovery as seen, clearing the Models nav badge; returns the acked entries |
| `/api/discovery/status` | GET | Outstanding discovery claims and their flap counts (`?review=1` adds the since-last-review numbers the modal shows) |
| `/api/discovery/{provider_id}/dismiss` | POST | Stop reporting a discrepancy for the given `model_ids` on one provider. Only already auto-disabled rows can be dismissed; `404` if none match |
| `/api/discovery/{provider_id}/unpin` | POST | Drop the operator pin from the given `model_ids`, handing them back to discovery's listing-based auto-disable; `404` if none carry a pin |

#### GET `/api/providers`

**Response:**
```json
[
  {
    "id": "uuid",
    "name": "OpenAI",
    "base_url": "https://api.openai.com/v1",
    "provider_type": "openai",
    "masked_key": "sk-p...c5d6",
    "enabled": true,
    "autodiscovery_enabled": true,
    "scheduled_disable_on": null,
    "max_in_flight": null,
    "last_discovered_at": "2024-01-01T00:00:00Z",
    "last_used_at": "2024-01-01T00:00:00Z",
    "created_at": "2024-01-01T00:00:00Z",
    "updated_at": "2024-01-01T00:00:00Z",
    "model_count": 15,
    "total_tokens": 1234567,
    "tokens_since": "2024-01-01T00:00:00Z"
  }
]
```

The plaintext API key is never returned; `masked_key` is a display-only preview. `tokens_since` (the timestamp of the oldest request log behind `total_tokens`) is omitted when the provider has no logged traffic, and a `last_cap` object is added only when the provider has answered an exhausted `429` since the process started. Non-admin callers see only their own traffic in `total_tokens`.

![Providers Page](screenshots/providers.png)

#### POST `/api/providers`

**Request Body:**
```json
{
  "name": "OpenAI",
  "base_url": "https://api.openai.com/v1",
  "provider_type": "openai",
  "api_key": "sk-..."
}
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `name` | string | Yes | 1-100 characters, unique |
| `base_url` | string | Yes | 1-500 characters, must use HTTPS unless `ALLOW_HTTP_PROVIDERS=true` |
| `provider_type` | string | No | One of the known types (see [Model Discovery](Model-Discovery#provider-type)). Omitted, it is derived from the vendor hostname |
| `api_key` | string | No | 1-500 characters (required for most providers, optional for Ollama, KoboldCPP, LMStudio, OpenCode Zen, custom) |

**Self-hosted providers must name their type.** Omitting `provider_type` derives it
from the hostname only, so `http://box:11434` becomes a generic OpenAI-compatible
provider rather than an Ollama one: no native discovery, no keyless waiver.
Scripts that used to rely on the old port detection (11434 / 5001 / 1234) need
`"provider_type": "ollama" | "koboldcpp" | "lmstudio"` added. When a self-hosted
type is named, the address is probed before the provider is saved and the
request fails with `provider_type_mismatch`, `provider_type_unconfirmed` or
`provider_unreachable` if the server does not answer as that type, so the server
must be running.

**An Anthropic Messages endpoint must name its type too**, for the same reason:
`anthropic-messages` describes a wire format, not a vendor, so no hostname
implies it. Named, the provider's chat traffic is translated to Anthropic's
`/v1/messages` instead of being sent to `/v1/chat/completions`, and models are
discovered from `<base_url>/v1/models` in Anthropic's shape. Omitted, the same
base URL becomes a generic OpenAI-compatible provider whose every request would
go to an endpoint that is not there. `api.anthropic.com` still derives
`anthropic`, which serves the OpenAI-compatible route by default. As with any
host that is not a known vendor's, a restricted install has to list the endpoint
in `ALLOWED_PROVIDER_HOSTS` before the provider can be created.

**Response:** `201 Created` with provider object

#### PUT `/api/providers/{id}`

**Request Body:** all fields optional for partial update. Accepts `name`, `base_url`, `provider_type`, `api_key`, `enabled`, `autodiscovery_enabled`, `scheduled_disable_on` and `max_in_flight`; POST accepts only the first four of those.

`provider_type` can be corrected here, which matters for a row the legacy hostname rules filed under the wrong type: re-adding the provider instead would cascade its models away. A new self-hosted type is probed exactly as on create, as is a changed `base_url`. `scheduled_disable_on` is an ISO date (`YYYY-MM-DD`) that must not be in the past, or `null` to clear it. `max_in_flight` caps the provider's concurrent upstream requests: `null` means no ceiling, and any number outside 1-10000 is a `400`.

#### DELETE `/api/providers/{id}`

**Response:** `204 No Content`

Cascades to delete associated models and updates failover groups.

#### POST `/api/providers/{id}/discover`

Triggers manual model discovery for a specific provider.

**Response:**
```json
{
  "discovered": 15,
  "models": [...],
  "diff": {
    "added": [{"model_id": "gpt-4o-2024-11", "reason": "new_model"}],
    "reenabled": [{"model_id": "o3-mini", "reason": "reappeared"}],
    "disabled": [{"model_id": "gpt-4o-2024-05", "reason": "not_listed"}],
    "failover_deleted_groups": [
      {"display_model": "o1-preview", "reason": "only 1 enabled provider (need 2+ for failover)", "provider_count": 1, "provider_names": []}
    ],
    "failover_updated_groups": [
      {"display_model": "glm-4.6", "removed_model_ids": ["uuid-old"], "added_model_ids": ["uuid-new"]}
    ]
  }
}
```

The `diff` summarizes what the scan changed (all sections omitted when empty). Reasons are machine-readable codes: `new_model`, `reappeared`, `not_listed`. Failover groups of newly disabled models are re-synced as part of the scan; the resulting group changes appear in the two `failover_*` sections. The dashboard shows this diff as a post-scan summary modal.

#### GET `/api/providers/{id}/usage`

Returns usage/quota information for supported providers.

**Supported providers:**
- `zai-coding` (Z.AI)
- `nanogpt` (Nano-GPT)
- `openrouter` (OpenRouter - returns key balance)
- `neuralwatt` (NeuralWatt - returns quota; 404 from the upstream quota endpoint means a free-tier key and yields no data)
- `kimi-code` (Kimi Code - returns 5-hour/weekly quota, parallel-request limit, and membership tier)
- `minimax` (MiniMax - returns 5-hour/weekly Token Plan quota per model class)

**Response (Z.AI example):**
```json
{
  "total_quota": 1000000,
  "used_quota": 50000,
  "remaining_quota": 950000
}
```

**Response (Kimi Code example, abbreviated):**
```json
{
  "user": {"userId": "u_123", "region": "global", "membership": {"level": "standard"}},
  "limits": [
    {"window": {"duration": 300, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "100", "remaining": "62", "resetTime": "2026-07-19T18:00:00Z"}},
    {"window": {"duration": 10080, "timeUnit": "TIME_UNIT_MINUTE"}, "detail": {"limit": "700", "remaining": "410", "resetTime": "2026-07-24T00:00:00Z"}}
  ],
  "parallel": {"limit": "4"},
  "subType": "coding"
}
```
The service passes the Kimi Code `/usages` payload through as-is; the dashboard derives 5-hour and weekly percentages from the `limits` array by matching each window's `duration` (300 minutes = 5h, 10080 minutes = weekly).

**Response (MiniMax example):**
```json
{
  "model_remains": [
    {
      "model_name": "general",
      "current_interval_status": 1,
      "current_interval_remaining_percent": 100,
      "current_weekly_status": 1,
      "current_weekly_remaining_percent": 100,
      "start_time": 1784473200000,
      "end_time": 1784491200000,
      "remains_time": 16420081,
      "weekly_remains_time": 30820081
    }
  ],
  "base_resp": {"status_code": 0, "status_msg": "success"}
}
```
The service passes the MiniMax `/token_plan/remains` payload through as-is, including `base_resp`. MiniMax reports business errors (such as `2062` for "no active token plan subscription") inside an HTTP 200 response, so the dashboard checks `base_resp.status_code` rather than the HTTP status to decide whether quota data is available.

#### GET `/api/providers/{id}/balance`

Returns balance information for supported providers.

**Supported providers:**
- `deepseek`

**Response:**
```json
{
  "balance": 100.50,
  "currency": "CNY"
}
```

#### GET `/api/providers/{id}/account`

Returns account information for supported providers.

**Supported providers:**
- `ollama-cloud`

**Response:**
```json
{
  "account_id": "...",
  "email": "...",
  "credits_remaining": 1000000
}
```

#### POST `/api/providers/discover-all`

Triggers discovery for all enabled providers.

**Response:**
```json
{
  "results": [
    {"provider_name": "OpenAI", "discovered": 15, "diff": {"added": [{"model_id": "gpt-4o-2024-11", "reason": "new_model"}]}},
    {"provider_name": "Anthropic", "discovered": 5, "diff": {}},
    {"provider_name": "Broken", "discovered": 0, "error": "connection refused"}
  ],
  "succeeded": 2,
  "failed": 1,
  "discovered": 20
}
```

Each successful result carries the same per-provider `diff` as the single-provider endpoint (omitted when the provider's scan failed).

#### POST `/api/providers/refresh-quotas`

Refreshes quota/balance information for all providers that support it.

**Response:**
```json
{
  "results": [
    {"provider_name": "Z.AI", "provider_type": "zai-coding", "refreshed": true},
    {"provider_name": "DeepSeek", "provider_type": "deepseek", "refreshed": true}
  ],
  "refreshed": 2,
  "failed": 0,
  "skipped": 5
}
```

---

### Models

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/models` | GET | List all models (optional `?provider_id=` and `?provider_enabled=true\|false` filters; the latter scopes to rows whose provider is enabled, i.e. what `/v1/models` advertises) |
| `/api/models/cursor` | GET | Cursor-paginated model listing for large catalogues; accepts the same `provider_enabled` filter and reports filter-wide `total`, `enabled_total` and `parked_total` |
| `/api/models/bulk-delete` | POST | Delete several models in one call (admin only) |
| `/api/models/{id}` | PATCH | Update model (enable/disable, edit metadata) |
| `/api/models/{id}` | DELETE | Delete model permanently |
| `/api/models/{id}/test` | POST | Test a model by sending a minimal prompt |

Cursor (keyset) pagination walks the list by passing the previous response's `next_cursor` back instead of an offset, so a page stays stable while rows are inserted ahead of it.

#### GET `/api/models`

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `provider_id` | UUID | Filter by provider UUID |
| `provider_enabled` | `true` / `false` | Filter on the owning provider's enabled flag. `true` returns the rows the proxy can list on `/v1/models` (subject to the model's own `enabled`); `false` returns rows parked under a disabled provider. Any other value is a `400`. Omit for both. |

**Response:**
```json
[
  {
    "id": "uuid",
    "model_id": "gpt-4o",
    "name": "GPT-4o",
    "display_name": "GPT-4o",
    "provider_id": "uuid",
    "provider_name": "OpenAI",
    "provider_enabled": true,
    "capabilities": "{\"streaming\":true,\"vision\":true,\"reasoning\":false,\"audio_input\":false}",
    "context_length": 128000,
    "max_output_tokens": 16384,
    "input_price_per_million": 5.0,
    "output_price_per_million": 15.0,
    "input_price_per_million_cache_hit": 2.5,
    "owned_by": "openai",
    "description": "Most capable model",
    "params": {"temperature": 0.7},
    "modality": "text",
    "input_modalities": ["text", "image"],
    "output_modalities": ["text"],
    "enabled": true,
    "created_at": "2024-01-01T00:00:00Z",
    "last_seen_at": "2024-01-01T00:00:00Z"
  }
]
```

![Models Page](screenshots/models.png)

#### PATCH `/api/models/{id}`

**Request Body:** (all fields optional, but at least one is required: an empty body is a `400`)
```json
{
  "display_name": "Custom Name",
  "context_length": 128000,
  "max_output_tokens": 16384,
  "input_price_per_million": 5.0,
  "input_price_per_million_cache_hit": 2.5,
  "output_price_per_million": 15.0,
  "price_customized": true,
  "enabled": true
}
```

**Validation:**
- `display_name`: 1-128 characters (empty clears it back to the discovered name)
- `context_length`: 256-2000000
- `max_output_tokens`: 1-128000
- `input_price_per_million`: 0-1000
- `input_price_per_million_cache_hit`: 0-1000
- `output_price_per_million`: 0-1000
- `price_customized`: boolean; marks the prices as operator-set so discovery enrichment leaves them alone

#### DELETE `/api/models/{id}`

**Response:** `204 No Content`

#### POST `/api/models/{id}/test`

Tests a model by sending a minimal prompt and measuring response.

**Response:**
```json
{
  "success": true,
  "duration_ms": 234,
  "ttft_ms": 123,
  "response": "Hi"
}
```

On error:
```json
{
  "success": false,
  "duration_ms": 5000,
  "error": "HTTP 401: Invalid API key"
}
```

---

### Failover Groups

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/failover-groups` | GET | List all failover groups (with token counts) |
| `/api/failover-groups` | POST | Create a failover group |
| `/api/failover-groups/sync` | POST | Re-sync all groups with current discovery data |
| `/api/failover-groups/candidates` | GET | List candidate models available for failover groups |
| `/api/failover-groups/circuit-breaker-status` | GET | Current circuit breaker state per provider (cached briefly; `?detail=1` adds per-circuit detail) |
| `/api/failover-groups/circuit-breaker/{provider_id}/reset` | POST | Force one provider's circuits back into rotation; `?model=<upstream id>` scopes it to one circuit |
| `/api/failover-groups/{id}/circuit-breaker/reset` | POST | Force every circuit behind one failover group's entries back into rotation, on this member |
| `/api/failover-groups/circuit-breaker/reset` | POST | Force every tracked circuit back into rotation (API only, no UI control) |
| `/api/failover-groups/by-model/{model_uuid}` | GET | Find which failover group a model belongs to |
| `/api/failover-groups/{id}` | GET | Get group details with priority order |
| `/api/failover-groups/{id}` | PUT | Update priority order, enable/disable entries, rename |
| `/api/failover-groups/{id}` | DELETE | Delete a failover group |

#### GET `/api/failover-groups`

**Response:**
```json
{
  "groups": [
    {
      "id": "uuid",
      "display_model": "glm-4.6",
      "display_name": "GLM 4.6 Failover",
      "description": "Primary failover group",
      "group_enabled": true,
      "auto_created": false,
      "entries": [
        {
          "model_uuid": "uuid",
          "model_id": "z-ai/glm-4.6",
          "provider_name": "OpenRouter",
          "display_name": "GLM 4.6",
          "enabled": true,
          "model_enabled": true,
          "provider_enabled": true,
          "context_length": 200000
        }
      ],
      "total_tokens": 123456,
      "created_at": "2024-01-01T00:00:00Z",
      "updated_at": "2024-01-01T00:00:00Z"
    }
  ],
  "last_synced_at": "2024-01-01T00:00:00Z"
}
```

![Failover Groups Page](screenshots/failover.png)

#### POST `/api/failover-groups`

**Request Body:**
```json
{
  "display_model": "glm-4.6",
  "display_name": "GPT-4o Failover",
  "description": "Optional description",
  "entry_ids": ["uuid-1", "uuid-2", "uuid-3"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `display_model` | string | Yes | 1-128 characters, must be unique |
| `display_name` | string | No | 1-128 characters |
| `description` | string | No | 0-500 characters |
| `entry_ids` | array | Yes | Array of model UUIDs in priority order (minimum 2) |

**Response:** `201 Created` with failover group object

#### PUT `/api/failover-groups/{id}`

**Request Body:** (all fields optional)
```json
{
  "display_name": "Updated Name",
  "display_model": "glm-4.6",
  "description": "Updated description",
  "group_enabled": true,
  "priority_order": ["uuid-2", "uuid-1", "uuid-3"],
  "entry_enabled": {
    "uuid-1": true,
    "uuid-2": false,
    "uuid-3": true
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `display_name` | string | 1-128 characters |
| `display_model` | string | 1-128 characters, must stay unique across groups: the `hotel/` name clients route on |
| `description` | string | 0-500 characters |
| `group_enabled` | boolean | Enable/disable entire group |
| `priority_order` | array | New priority order of model UUIDs |
| `entry_enabled` | object | Map of model UUID to enabled state |

**Validation:** an active group must keep at least one enabled entry. Turning a group on (the off to on transition only) additionally requires at least 2 routable members, meaning entries whose model and whose provider are both enabled; short of that the request is a `400`.

#### GET `/api/failover-groups/by-model/{model_uuid}`

Find which failover group contains a given model UUID.

**Response:**
```json
{
  "id": "uuid",
  "display_model": "glm-4.6",
  "position": 1,
  "total_entries": 3
}
```

Returns `404` if the model is not found in any failover group.

#### POST `/api/failover-groups/sync`

Re-synchronizes all failover groups with current model database state.

**Response:**
```json
{
  "deleted_groups": [],
  "purged_entries": [],
  "sync_errors": []
}
```

#### GET `/api/failover-groups/candidates`

Returns available models that can be added to failover groups.

**Response:**
```json
[
  {
    "model_uuid": "uuid",
    "model_id": "gpt-4o",
    "provider_id": "uuid",
    "provider_name": "OpenAI",
    "display_name": "GPT-4o",
    "context_length": 128000,
    "owned_by": "openai"
  }
]
```

#### POST `/api/failover-groups/circuit-breaker/{provider_id}/reset`

Clears one provider's circuit, returning it to rotation immediately instead of waiting out the cooldown. Use it when you have fixed the provider yourself (rotated a dead key, restarted a local runtime, topped up a spent plan) and do not want to wait for the breaker to find out.

**Response:**
```json
{
  "provider_id": "uuid",
  "previous_state": "open",
  "reset": true
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `provider_id` | string (UUID) | The provider whose circuit was cleared |
| `previous_state` | string | `closed`, `open` or `half-open`: what the breaker reported a moment before the reset |
| `reset` | bool | `false` when there was nothing to clear, so the response can say "no change" instead of implying a recovery |

Resetting an untracked or already-closed provider is a successful no-op (`previous_state: "closed"`, `reset: false`), not an error: the breaker only tracks providers it has routed, so "no circuit" and "closed circuit" are the same healthy state. The reset clears state, failure count and any quota pin together; if the provider is still broken the circuit reopens after `circuit_breaker_threshold` failures.

`?model=<resolved upstream model id>` scopes the reset to that one circuit and leaves the provider's other circuits, and the charges they have legitimately accrued, alone; the response then carries `"model"`. Without it every circuit of the provider is cleared, as before. Either way the breaker logs one `circuit-breaker: manual reset` line per circuit cleared, with `cause=manual reset`, `previous_state` and the model, so a fleet-wide clean-up reads in the app log as N circuits with N causes.

This endpoint backs the circular-arrow button ("Reset circuit breaker") beside each open or half-open member on the Failover page.

**Response (400):** invalid provider UUID. **Response (503):** the circuit breaker is not available.

#### POST `/api/failover-groups/{id}/circuit-breaker/reset`

Clears every circuit behind one failover group's entries on this member, in one call instead of one per provider. Only the group's (provider, resolved model) pairs are touched: a provider's circuits for models outside the group keep their state.

**Response:**
```json
{
  "group_id": "uuid",
  "display_model": "glm53",
  "entries": 3,
  "cleared": 2,
  "recovered": 1
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `entries` | int | The group's entries that resolved to a model |
| `cleared` | int | Circuits that existed and were discarded, healthy ones included |
| `recovered` | int | Those that were sidelining their entry (open or half-open) |

Like the other resets it is outside the managed-write guard: a circuit is local runtime health, not synced config. Front Desk fans this out to every member (`POST /api/fleet/circuit-breaker/reset`, see [High Availability](High-Availability)).

**Response (404):** unknown group. **Response (503):** the circuit breaker is not available.

#### POST `/api/failover-groups/circuit-breaker/reset`

Clears every tracked circuit at once, for recovering from a fleet-wide upstream incident without resetting providers one by one.

**Response:**
```json
{
  "cleared": 7,
  "recovered": 2
}
```

| Field | Type | Meaning |
|-------|------|---------|
| `cleared` | int | Every circuit discarded, including healthy closed ones |
| `recovered` | int | Only those that were actually sidelining their provider (open or half-open), so a bulk reset does not imply every tracked provider was broken |

This endpoint is **deliberately API only: there is no UI control for it, by decision rather than omission.** Per-provider reset is the right operation almost every time and it names the provider being recovered, whereas a fleet-wide button placed beside it is too easy to reach for when one circuit is the problem, and it throws away the breaker's evidence about every other provider in the process. Bulk reset is left to scripts and runbooks, where its blast radius is written down.

**Response (503):** the circuit breaker is not available.

#### DELETE `/api/failover-groups/{id}`

**Response:** `204 No Content`

---

### Virtual Keys

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/virtual-keys` | GET | List virtual keys (key values are not returned) |
| `/api/virtual-keys` | POST | Create a virtual key (returns full key once) |
| `/api/virtual-keys/{id}` | GET | Get virtual key details (without key value) |
| `/api/virtual-keys/{id}` | PUT | Update virtual key (name, rate limits) |
| `/api/virtual-keys/{id}` | DELETE | Revoke a virtual key |

#### GET `/api/virtual-keys`

**Response:**
```json
[
  {
    "id": "uuid",
    "name": "Production Key",
    "key_preview": "sk-...c5d6",
    "tokens_used": 123456,
    "last_used_at": "2024-01-01T00:00:00Z",
    "created_at": "2024-01-01T00:00:00Z",
    "rate_limit_rps": null,
    "rate_limit_burst": null,
    "rate_limit_tpm": null,
    "allowed_providers": null,
    "strip_reasoning": false
  }
]
```

![Virtual Keys Page](screenshots/virtual_keys.png)

#### POST `/api/virtual-keys`

**Request Body:**
```json
{
  "name": "Production Key",
  "rate_limit_rps": 10.0,
  "rate_limit_burst": 20,
  "rate_limit_tpm": 50000,
  "allowed_providers": ["provider-uuid-1", "provider-uuid-2"],
  "strip_reasoning": false
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | 1-100 characters, cannot be reserved names (`chat`, `arena`, `completions`, `admin`) |
| `rate_limit_rps` | number | No | Requests per second (null = use global default) |
| `rate_limit_burst` | integer | No | Burst capacity (null = use global default, must be >= 1 if set) |
| `rate_limit_tpm` | integer | No | Tokens-per-minute cap (null = no cap / global default, must be >= 1 if set). Counts prompt + completion + reasoning; over-budget keys get `429 token rate limit exceeded` with `Retry-After` |
| `allowed_providers` | array of UUID strings | No | Restrict this key to the listed provider IDs (null = all providers accessible; an empty array is rejected) |
| `strip_reasoning` | boolean | No | Strip `reasoning`/`reasoning_content` fields from streaming output for this key |

**Response:** `201 Created`
```json
{
  "id": "uuid",
  "name": "Production Key",
  "key": "sk-a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6",
  "key_preview": "sk-...c5d6",
  "tokens_used": 0,
  "last_used_at": null,
  "created_at": "2024-01-01T00:00:00Z",
  "rate_limit_rps": 10.0,
  "rate_limit_burst": 20,
  "rate_limit_tpm": 50000
}
```

> ⚠️ **Important:** The full key is shown only once at creation time and cannot be retrieved later.

#### PUT `/api/virtual-keys/{id}`

**Request Body:** Same as POST, `name` is required, other fields optional

#### DELETE `/api/virtual-keys/{id}`

**Response:** `204 No Content`

---

### Request Logs

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/logs` | GET | Query request logs (with pagination, filtering, sorting) |
| `/api/logs/cursor` | GET | Cursor-paginated request log listing |
| `/api/logs/{id}` | GET | Get a single request log entry by ID |
| `/api/logs/purge` | DELETE | Purge logs older than a specified period |

> **Caching:** responses are cached in-process, keyed by the raw query string. Each response carries an `X-Cache: HIT` or `X-Cache: MISS` header.

#### GET `/api/logs`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | integer | 1 | Page number |
| `per_page` | integer | 20 | Page size (max 200) |
| `model_id` | string | - | Filter by model ID (partial match) |
| `provider_id` | UUID | - | Filter by provider UUID |
| `virtual_key_id` | UUID | - | Filter by the virtual key that made the request |
| `client_ip` | string | - | Filter by the resolved client address |
| `owner_user_id` | UUID | - | Filter by the account that owns the key (admins only; a non-admin caller is scoped to their own rows regardless) |
| `status_code` | string | - | Filter by status code (`4xx`, `5xx`, or exact integer; `0` = no response) |
| `endpoint_type` | string | - | Filter by endpoint family: `chat`, `messages`, `embeddings`, `rerank`, `image`, `tts`, `stt` (unknown values are ignored) |
| `from` | RFC3339 | - | Start timestamp |
| `to` | RFC3339 | - | End timestamp |
| `attempt_provider_id` | UUID | - | Select requests whose per-attempt trail names this provider on ANY attempt, whoever served the request in the end ("every request in which Neuralwatt answered") |
| `attempt_status` | positive integer | - | Select requests with an attempt that reached this upstream status (`0`, "no response seen", cannot be selected: such attempts carry no status). Combined with `attempt_provider_id`, both must hold on the same attempt ("every request in which Neuralwatt returned 429") |
| `sort_by` | string | `time` | Sort column: `time`, `model`, `provider`, `status`, `tokens`, `tps`, `ttft`, `response_header_ms`, `duration`, `overhead`, `key`, `ip`. Anything else falls back to `time` |
| `sort_dir` | string | `desc` | Sort direction: `asc` or `desc` |

**Response:**
```json
{
  "entries": [
    {
      "id": "uuid",
      "provider_id": "uuid",
      "provider_name": "OpenAI",
      "model_id": "gpt-4o",
      "request_hash": "...",
      "status_code": 200,
      "latency_ms": 234.5,
      "duration_ms": 456.7,
      "ttft_ms": 123.4,
      "response_header_ms": 98.7,
      "proxy_overhead_ms": 1.2,
      "parse_ms": 0.5,
      "failover_lookup_ms": 0.1,
      "model_lookup_ms": 0.3,
      "provider_lookup_ms": 0.2,
      "key_decrypt_ms": 0.8,
      "dial_ms": 0.1,
      "settings_read_ms": 0.1,
      "cache_hits": {"failover": true, "model": true, "provider": true, "key": true, "settings": true},
      "tokens_per_second": 45.6,
      "tokens_prompt": 100,
      "tokens_completion": 200,
      "tokens_completion_reasoning": 0,
      "tokens_prompt_cache_hit": 0,
      "tokens_prompt_cache_miss": 0,
      "streaming": true,
      "virtual_key_name": "Production Key",
      "virtual_key_deleted": false,
      "virtual_key_id": "uuid",
      "error_message": "",
      "error_kind": "",
      "failover_attempt": 1,
      "state": "completed",
      "endpoint_type": "chat",
      "created_at": "2024-01-01T00:00:00Z",
      "attempts": [
        {"attempt": 0, "provider_id": "uuid", "provider": "Neuralwatt", "model": "glm-5.3", "status": 429, "error_kind": "provider_saturated", "detail": "concurrent_budget_exceeded", "phrase": "concurrent_budget_exceeded", "duration_ms": 412, "breaker": "noop"},
        {"attempt": 1, "provider_id": "uuid", "provider": "OpenAI", "model": "gpt-4o", "status": 200, "duration_ms": 8299, "ttft_ms": 123.4, "breaker": "success"}
      ]
    }
  ],
  "total": 1000,
  "page": 1,
  "per_page": 20
}
```

`attempts` is the per-attempt trail: one element per failover attempt, in order, hedged probes (a hedged attempt abandoned in flight appears with no `status` and the exit as its `error_kind`: `hedge_superseded` when another candidate won, `failover_timeout`, `client_disconnect`) and in-flight busy skips included; sorted by `attempt`, skips first. `attempt` is the loop's index (the same numbering as `failover_attempt`); `-1` marks a candidate the circuit breaker refused before any request was made (`breaker: "skipped"`). `status` is the upstream status the attempt reached (omitted when no response was seen), `detail` at most 160 characters of the sanitized, credential-masked upstream error (never request content), `phrase` the rate-limit phrase-table entry a 429 matched, and `breaker` what the attempt did to the circuit: `charge`, `noop`, `success`, `alive`, `skipped` or `disabled`. The field is omitted for rows without a trail (rows from before it existed, rows an older member wrote, requests that never reached a candidate). The terminal attempt's values also stay in the flat columns, so nothing that reads them changes. See [Failover: the per-attempt trail](Failover-and-Hotel-Routing#the-per-attempt-trail).

![Request Logs Page](screenshots/logs.png)

#### DELETE `/api/logs/purge`

**Request Body:**
```json
{ "older_than": "1h" }
```

**Accepted values:** `1h`, `1d`, `1w`, `1m`, `all`

**Response:** `204 No Content`

---

### App Logs

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/logs/app` | GET | Query application logs |
| `/api/logs/app/cursor` | GET | Cursor-paginated app log history |
| `/api/logs/app` | DELETE | Clear all app logs (ring buffer + DB) |

#### GET `/api/logs/app`

**Ring buffer mode (default):**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | integer | 500 | Max entries (max 1000) |
| `after` | RFC3339 | - | Only return entries after this timestamp |

**Response:** Array of log entries
```json
[
  {
    "timestamp": "2024-01-01T00:00:00.000000000Z",
    "level": "info",
    "source": "proxy",
    "message": "Request completed successfully"
  }
]
```

**History mode (`?history=true`):**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `level` | string | - | Filter: `info`, `warning`, `error` |
| `source` | string | - | Filter: `proxy`, `auth`, `discovery`, etc. |
| `search` | string | - | Text search in message (case-insensitive) |
| `from` | RFC3339 | - | Start timestamp |
| `to` | RFC3339 | - | End timestamp |
| `page` | integer | 1 | Page number |
| `per_page` | integer | 20 | Page size (max 100) |
| `sort_by` | string | `time` | Sort: `time`, `level`, `source`, `message` |
| `sort_dir` | string | `desc` | Sort direction |

**Response:**
```json
{
  "entries": [...],
  "total": 500,
  "page": 1,
  "per_page": 20,
  "level_counts": {
    "info": 450,
    "warning": 30,
    "error": 20
  },
  "source_counts": {
    "proxy": 300,
    "auth": 100,
    "discovery": 100
  }
}
```

#### DELETE `/api/logs/app`

**Response:**
```json
{ "deleted": 1234 }
```

---

### Audit Trail

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/audit` | GET | Query the audit trail (cursor-paginated, newest first) |
| `/api/audit/purge` | DELETE | Purge audit entries older than a specified period |

> **Admin-only.** Both endpoints require an admin session. The trail itself is written by middleware that records every mutating (POST/PUT/PATCH/DELETE) request on the authenticated `/api/*` surface from any signed-in account, admin or user. Request and response bodies are never stored.

#### GET `/api/audit`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `limit` | integer | 50 | Page size (max 200) |
| `cursor` | string | - | Keyset cursor from a previous response's `next_cursor` |
| `actor` | string | - | Filter by actor (exact match: a username, or `admin` for legacy admin-token/passkey/SSO logins) |
| `method` | string | - | Filter by HTTP method (`POST`, `PUT`, `PATCH`, `DELETE`) |
| `from` | RFC3339 | - | Start timestamp |
| `to` | RFC3339 | - | End timestamp |

**Response:**
```json
{
  "entries": [
    {
      "id": "uuid",
      "created_at": "2026-01-01T00:00:00Z",
      "actor": "maya",
      "actor_role": "user",
      "method": "PUT",
      "route": "/api/virtual-keys/{id}",
      "path": "/api/virtual-keys/0a1b2c3d-...",
      "entity_id": "0a1b2c3d-...",
      "entity_name": "maya-cli",
      "status_code": 200,
      "remote_addr": "10.0.0.17:41822"
    }
  ],
  "total": 132,
  "has_more": true,
  "next_cursor": "..."
}
```

`entity_name` is resolved at read time from the entity's current display name (models, providers, virtual keys, failover groups, users) and is omitted when the entity has been deleted or the route has no mapped entity; the UUID in `entity_id` then remains the only trace. Names are never stored, so a rename shows the current name.

[![Audit Page](screenshots/audit.png)](screenshots/audit.png)

#### DELETE `/api/audit/purge`

**Request Body:**
```json
{ "older_than": "1w" }
```

**Accepted values:** `1h`, `1d`, `1w`, `1m`, `all`

**Response:** `204 No Content`

The purge is itself a mutating request and is recorded by the audit middleware, so a wiped trail always shows who wiped it.

---

### Backups

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/backups` | GET | List available backups |
| `/api/backups` | POST | Create a new backup |
| `/api/backups/restore` | POST | Restore the database from an uploaded backup file |
| `/api/backups/{filename}` | GET | Download a backup file |
| `/api/backups/{filename}/signature` | GET | Fetch a backup's signature sidecar (for the restore form) |
| `/api/backups/{filename}` | DELETE | Delete a backup |
| `/api/backups/prune-preview` | POST | Preview which backups would be pruned (dry run) |
| `/api/backups/prune` | POST | Execute backup rotation and prune old backups |

#### GET `/api/backups`

**Response:**
```json
[
  {
    "filename": "backup_20240101_120000_123456.dump",
    "size_bytes": 10485760,
    "created_at": "2024-01-01T12:00:00Z"
  }
]
```

![Backups Section](screenshots/settings_backup.png)

#### POST `/api/backups`

Creates a PostgreSQL backup using `pg_dump` (custom format, `--compress=zstd:19`; readable by `pg_restore` 16 or later built with zstd, which the `postgres:16-alpine` image is).

**Response:** `201 Created`
```json
{
  "filename": "backup_20240101_120000_123456.dump",
  "size_bytes": 10485760,
  "created_at": "2024-01-01T12:00:00Z"
}
```

**Error Responses:**
- `409 Conflict` - Backup already in progress
- `412 Precondition Failed` - `pg_dump` not found (install `postgresql-client`)
- `500 Internal Server Error` - Backup failed

#### GET `/api/backups/{filename}`

Downloads the backup file.

**Response:** File download with `Content-Disposition: attachment`

#### GET `/api/backups/{filename}/signature`

Returns the backup's HMAC signature sidecar, the value the restore endpoint takes in its `signature` form field. The download serves the dump alone, so this is how an operator without shell access to the backup directory carries the signature to a restore. Not verified here: the restore checks it against the uploaded bytes.

**Response:**
```json
{ "signature": "3f1a…64 hex characters…" }
```

**Error Responses:**
- `400 Bad Request` - Invalid filename
- `404 Not Found` - Backup does not exist, or has no signature (backups predating signing, dumps copied in from another instance, or a backup whose signing failed at creation; see the `backup.unsigned` event)
- `500 Internal Server Error` - The sidecar exists but cannot be read, or is not a valid signature

#### DELETE `/api/backups/{filename}`

**Response:** `204 No Content`

#### POST `/api/backups/prune-preview`

Preview which backups would be pruned under the son/father/grandfather rotation scheme (three retention tiers: daily "son" backups, weekly "father", monthly "grandfather", each with its own count). Non-destructive (dry run).

**Response:**
```json
{
  "son": [
    {"filename": "backup_20260608_120000_0000.dump", "size_bytes": 1048576, "created_at": "2026-06-08T12:00:00Z"}
  ],
  "father": [],
  "grandfather": [],
  "prune": [
    {"filename": "backup_20260301_120000_0000.dump", "size_bytes": 2097152, "created_at": "2026-03-01T12:00:00Z"}
  ]
}
```

#### POST `/api/backups/prune`

Execute the son/father/grandfather rotation, deleting backups that fall outside the retention tiers. Returns the same classification as `prune-preview`.

**Response:** Same structure as `prune-preview`, reflecting the state after pruning.

---

### Settings

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/settings` | GET | List all settings (key-value map) |
| `/api/settings` | PUT | Update settings (partial update) |
| `/api/settings` | DELETE | Reset settings to Go-side defaults. Body: `{"keys": ["key1", ...]}`; empty `keys` array resets all. Returns the full updated settings map. |

#### GET `/api/settings`

Every value is a string, whatever its logical type. The map also carries a few read-only keys that `PUT` rejects: `app_version`, `app_commit`, `log_export_json`, `log_export_metrics`, `log_export_otel` and `discovery_claim_window_days`. Secret values (`alert_apprise_targets`, `oidc_client_secret`, `github_client_secret`) come back as a fixed placeholder, never as stored ciphertext or plaintext.

**Response:**
```json
{
  "rate_limit_enabled": "true",
  "rate_limit_ip_rps": "30",
  "rate_limit_ip_burst": "60",
  "discovery_interval": "6h",
  "discovery_on_startup": "true",
  "circuit_breaker_enabled": "true",
  "app_version": "1.2.3"
}
```

![Settings Page](screenshots/settings.png)

#### PUT `/api/settings`

**Request Body:**
```json
{
  "rate_limit_ip_rps": "50",
  "discovery_interval": "12h"
}
```

A key outside the allowlist below is a `400` (`unknown setting: <key>`), as is a value longer than 500 characters or outside its range. A managed fleet member additionally answers `403` for any synced key: the primary owns those and replaces them on the next sync.

**Allowed Settings:**

| Key | Type | Constraints and meaning |
|-----|------|-------------------------|
| `rate_limit_enabled` | string | `"true"` or `"false"` |
| `rate_limit_ip_enabled` | string | `"true"` or `"false"` |
| `rate_limit_ip_rps` | float | 0-10000 |
| `rate_limit_ip_burst` | int | 1-10000 |
| `rate_limit_max_wait_ms` | int | 0-10000 |
| `rate_limit_rps` | float | 0-10000 |
| `rate_limit_burst` | int | 1-10000 |
| `rate_limit_tpm` | int | 0-100000000; global per-key tokens-per-minute default, `0` = no cap |
| `request_timeout` | string | Duration (e.g. `"1m0s"`) |
| `failover_on_rate_limit` | string | `"true"` or `"false"` |
| `failover_exhaustion_status_429` | string | `"true"` or `"false"`; all-busy/all-pinned exhaustion answers `429` plus `Retry-After` instead of `502` |
| `server_error_retry_enabled` | string | `"true"` or `"false"`; the last candidate's one same-provider retry of a transient 5xx |
| `rate_limit_classify_enabled` | string | `"true"` or `"false"`; master switch for saturation-vs-exhaustion classification of a `429` |
| `rate_limit_saturation_max_wait` | string | Duration; a `Retry-After` at or below this reads as saturation, and it caps the saturation wait |
| `rate_limit_recent_success_window` | string | Duration; an unclassifiable `429` following a `2xx` this recent is treated as saturation |
| `inflight_limiter_enabled` | string | `"true"` or `"false"`; adaptive per-provider concurrency learner |
| `inflight_grow_after` | int | 1-1000; clean completions per `+1` of a capped in-flight window |
| `inflight_forget_after` | string | Duration; a capped window returns to uncapped after this long without a cut |
| `circuit_breaker_enabled` | string | `"true"` or `"false"` |
| `circuit_breaker_open_on_exhaustion` | string | `"true"` or `"false"`; one exhausted `429` opens the model circuit outright |
| `circuit_breaker_threshold` | int | 1-100 |
| `circuit_breaker_span_models` | int | 1-100 (default `2`); open model circuits it takes to skip the provider itself |
| `circuit_breaker_cooldown` | string | Duration |
| `circuit_breaker_quota_pin_max` | string | Duration ceiling for pinning an open circuit's cooldown to the provider's quota reset (default `"24h0m0s"`); `"0s"` switches pinning off |
| `circuit_breaker_pin_probe_interval` | string | Duration between probes of a circuit pinned on a response's own claim (default `"1h0m0s"`); `"0s"` disables the probe |
| `circuit_breaker_backoff_max` | string | Duration ceiling for doubling an open circuit's cooldown per failed half-open probe (default `"15m0s"`); `"0s"` switches backoff off |
| `discovery_interval` | string | Duration (e.g. `"6h"`, `"0"` = disabled) |
| `discovery_on_startup` | string | `"true"` or `"false"` |
| `discovery_on_provider_create` | string | `"true"` or `"false"` |
| `discovery_claim_alert_days` | int | 1-29; age at which an unaddressed discovery claim raises an alert. The ceiling is one day below the 30-day claim window |
| `model_prune_days` | int | 0-180; days an unlisted model stays before its row is deleted, `0` = never |
| `log_retention` | string | Any Go duration (`"24h"`, `"48h"`, `"168h0m0s"`); legacy `"1d"`/`"1w"`/`"1m"` (30 days) still accepted; `"0"` or empty = keep forever |
| `stale_request_timeout` | string | Duration |
| `key_cache_ttl` | string | Duration (e.g. `"10m0s"`) |
| `ttft_timeout` | string | Duration; time-to-first-token probe timeout for streaming (`"0s"` disables) |
| `stream_stall_timeout` | string | Duration; max silence during streaming before termination (`"0s"` disables) |
| `hedging_enabled` | string | `"true"` or `"false"` |
| `hedge_delay` | string | Duration before a backup provider is raced (default `"4s"`) |
| `backup_enabled` | string | `"true"` or `"false"` (periodic backup with rotation) |
| `backup_interval` | string | Duration between automatic backups (default `"24h"`); anything under 5 minutes is clamped up to 5 minutes |
| `backup_son_retention` | int | 1-365 (daily tier) |
| `backup_father_retention` | int | 0-52 (weekly tier) |
| `backup_grandfather_retention` | int | 0-120 (monthly tier) |
| `alert_enabled` | string | `"true"` or `"false"`; outbound alerting through apprise-api |
| `alert_apprise_api_url` | string | Base URL of the apprise-api container, validated against SSRF targets |
| `alert_apprise_targets` | string | Secret: encrypted at rest, masked on read |
| `alert_events` | string | Comma-separated list of the event types to notify on |
| `session_idle_timeout_minutes` | int | 0-240; dashboard auto-logout window, `0` = disabled |
| `pwned_password_check_enabled` | string | `"true"` or `"false"`; breached-password screening |
| `oidc_enabled` | string | `"true"` or `"false"` |
| `oidc_issuer_url` | string | OIDC discovery base URL, validated against SSRF targets |
| `oidc_client_id` | string | OAuth client id |
| `oidc_client_secret` | string | Secret: encrypted at rest, masked on read |
| `oidc_allowed_emails` | string | Comma- or newline-separated allowlist |
| `oidc_public_base_url` | string | This app's external origin, used to build the redirect URI |
| `github_sso_enabled` | string | `"true"` or `"false"` |
| `github_client_id` | string | GitHub OAuth App client id |
| `github_client_secret` | string | Secret: encrypted at rest, masked on read |
| `github_allowed_emails` | string | Comma- or newline-separated allowlist of verified emails |
| `github_public_base_url` | string | This app's external origin, used to build the callback URI |
| `quota_refresh_interval_min` | int | 0-30; provider quota poll interval in minutes, `0` = disabled |

**Response:** `200 OK` with full settings map

---

### Stats

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/stats` | GET | Dashboard statistics |
| `/api/stats/timeseries` | GET | Time-series data for charts |
| `/api/stats/provider-distribution` | GET | Top 5 provider distribution |

#### GET `/api/stats`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `period` | string | `24h` | Time period: `1h`, `24h`, `7d` (anything else is read as `24h`) |
| `exclude_deleted` | boolean | `false` | Set `true` to exclude rows whose virtual key has been deleted |
| `metric` | string | `requests` | Metric for aggregation: `requests` or `tokens` |
| `include_latency` | boolean | `false` | Set `true` to add the latency breakdown to the response |

**Response:**
```json
{
  "total_requests_last_24h": 12345,
  "total_requests_last_7d": 54321,
  "by_model": {
    "OpenAI/gpt-4o": 5000,
    "Anthropic/claude-3-opus": 2000,
    "hotel/fast-chat": 1000
  },
  "by_provider": {
    "OpenAI": 8000,
    "Anthropic": 4000
  },
  "by_virtual_key": {
    "Production Key": 10000,
    "Development": 2000
  },
  "avg_latency_ms": 234.5,
  "error_rate": 0.02,
  "avg_overhead_ms": 1.2,
  "total_tokens_prompt": 500000,
  "total_tokens_completion": 750000,
  "avg_tokens_per_request": 101.2,
  "rate_limit_hits": 15,
  "avg_ttft_ms": 123.4,
  "requests_last_1h": 500
}
```

#### GET `/api/stats/timeseries`

**Query Parameters:** Same as `/api/stats`

**Response:**
```json
{
  "points": [
    {
      "bucket": "2024-01-01T00:00:00Z",
      "count": 100,
      "tokens": 50000,
      "errors": 2,
      "latency_ms": 234.5,
      "overhead_ms": 1.2,
      "provider_latency_ms": 230.0,
      "rate_limit_hits": 1,
      "avg_ttft_ms": 123.4
    }
  ]
}
```

Returns hourly buckets for `1h` and `24h` periods, daily buckets for `7d`. Empty buckets are filled with zeros.

#### GET `/api/stats/provider-distribution`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `period` | string | `24h` | Time period: `1h`, `24h`, `7d` (anything else is read as `24h`) |
| `exclude_deleted` | boolean | `false` | Set `true` to exclude rows whose virtual key has been deleted |
| `metric` | string | `requests` | Distribution metric: `requests` or `tokens` |

**Response:**
```json
{
  "items": [
    {
      "name": "OpenAI",
      "count": 8000,
      "tokens": 0,
      "share": 66.7
    },
    {
      "name": "Anthropic",
      "count": 4000,
      "tokens": 0,
      "share": 33.3
    }
  ]
}
```

---

### System

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/system` | GET | System status (CPU, memory, Go runtime, DB, Docker) |

#### GET `/api/system`

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `since` | RFC3339 | Start of day for `requests_today` calculation (defaults to UTC midnight) |

**Response:**
```json
{
  "app": {
    "heap_alloc_mb": 45.6,
    "sys_memory_mb": 128.0,
    "goroutines": 50,
    "gc_cycles": 123,
    "memory_current_bytes": 134217728,
    "memory_limit_bytes": 1073741824,
    "in_container": true,
    "uptime_seconds": 86400,
    "cpu_percent": 2.5,
    "requests_today": 12345,
    "net_rx_bytes_sec": 1024.0,
    "net_tx_bytes_sec": 2048.0,
    "disk_read_bytes_sec": 512.0,
    "disk_write_bytes_sec": 256.0,
    "procs": 4
  },
  "db": {
    "size_mb": 256.0,
    "connections": 10,
    "cache_hit_ratio": 99.5,
    "tx_per_sec": 15.3,
    "dead_tuples": 1000,
    "lock_waits": 0
  },
  "docker": {
    "cpu_percent": 5.0,
    "memory_usage_bytes": 536870912,
    "memory_limit_bytes": 2147483648,
    "net_rx_bytes": 1048576,
    "net_tx_bytes": 2097152,
    "block_read_bytes": 524288,
    "block_write_bytes": 262144
  }
}
```

---

### Events (SSE)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/events` | GET | Server-Sent Events stream (requires admin token) |

#### GET `/api/events`

Long-lived SSE stream for real-time dashboard updates. Requires admin token in `Authorization` header.

**Response Headers:**
```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
X-Accel-Buffering: no
```

**Event Format:**
```
: connected

data: {"type":"discovery.complete","severity":"success","message":"Discovery complete: 15 models across 2 providers","metadata":{"source":"Startup","models_pruned":0}}

: heartbeat
```

**Event Types:**

| Event | Severity | Description |
|-------|----------|-------------|
| `discovery.complete` | `success`/`warning`/`error` | A discovery run finished |
| `discovery.provider_fetched` | `success` | Fetched models from a provider |
| `discovery.provider_failed` | `error` | Discovery failed for a provider |
| `discovery.enriched` | `info` | Models enriched from the models.dev catalogue |
| `discovery.models_disabled` | `warning` | Models were disabled during discovery |
| `discovery.changes_pending` | `info` | Background discovery recorded model changes (badged on the Models nav) |
| `discovery.claims_outstanding` | `warning` | Model discrepancies have gone unaddressed past `discovery_claim_alert_days` |
| `discovery.suspect_scan` | `warning` | A scan produced models that look wrong and were held back from auto-disable |
| `discovery.bulk_removal_suspected` | `error` | A provider dropped so many models at once that the listing itself is suspect |
| `model.auto_disabled_gone` | `warning` | A model was disabled because the provider kept refusing it as retired |
| `provider.scheduled_disable` | `warning` | A provider reached its `scheduled_disable_on` date and was switched off |
| `failover.sync_error` | `warning` | Error during failover group synchronization |
| `circuit_breaker.open` | `warning` | A circuit opened |
| `circuit_breaker.closed` | `success` | A circuit closed (recovered) |
| `circuit_breaker.unstable` | `warning` | One model opened its circuit 3 times in 24h, so it keeps returning to service still broken |
| `quota.schema_drift` | `warning` | A provider changed the shape of its quota response |
| `tokens.error` | `error` | Error counting tokens |
| `request.started` | `info` | A proxied request started |
| `request.streaming` | `info` | A proxied request began streaming |
| `request.completed` | varies | A proxied request finished (severity follows the outcome) |
| `request.discovery.provider_starting` | `info` | Starting discovery for a provider |
| `request.discovery.provider_completed` | `info` | Discovery finished for a provider |
| `backup.created` | `success` | Database backup created (manual or scheduled) |
| `backup.deleted` | `info` | Backup deleted |
| `backup.pruned` | `info` | Backup pruned by rotation |
| `backup.restored` | `success` | Database restored from a backup |
| `backup.integrity_failed` | `error` | A backup failed its integrity check |
| `backup.restore_unverified` | `warning` | A restore ran from a backup whose signature could not be verified |
| `backup.unsigned` | `warning` | A backup was written without a signature |
| `configsync.malformed_password_hash` | `error` | A config export or import carried an unusable password hash |
| `fleet.conflict` | `warning` | Two fleet members claim the same role |
| `auth.sessions_revoked` | `info` | Sessions were signed out |
| `auth.sso_identity_bound` | `warning` | An SSO identity was bound to an existing account |
| `webauthn.credential_registered` | `success` | A passkey was registered |
| `webauthn.credential_deleted` | `info` | A passkey was deleted |
| `logs.stale_startup` | `warning` | Requests left in flight by a previous process were closed out at startup |
| `logs.stale_cleanup` | `warning` | The background sweep closed out requests that never finished |

Front Desk publishes its own event set (`member.*`, `fleet.*`, `config.*`, `health.*`, `settings.changed`, and more) on its own stream; those never appear here.

Heartbeat comments (`: heartbeat`) are sent every 30 seconds.

**Circuit breaker event metadata:**

`circuit_breaker.open` and `circuit_breaker.closed` carry the same metadata block:

| Field | Type | Present | Meaning |
|-------|------|---------|---------|
| `provider_id` | string (UUID) | always | The provider whose circuit changed state |
| `provider` | string | always | Provider name, so the event reads without a lookup |
| `model` | string | always | The resolved upstream model id whose circuit changed state (never a `hotel/` alias) |
| `model_id` | string | on `closed` always; on `open` unless `provider_open` is `true` | The same value as `model`, carried separately because outbound alerts debounce on it |
| `state` | string | always | `open` or `closed` |
| `cause` | string | always | A fixed phrase chosen by the gateway for the transition (`upstream status 503`, `success`, ...), never provider text; see [Why a circuit is open](Failover-and-Hotel-Routing#why-a-circuit-is-open) |
| `status` | int | always | The upstream HTTP status behind `cause`; `0` when no response was seen |
| `provider_open` | bool | always | Whether the provider as a whole is now being skipped |
| `consecutive_fails` | int | always | Consecutive failures recorded against that model's circuit |
| `quota_pinned` | bool | always | Whether a quota reset deadline is in force on this circuit |
| `pin_source` | string | always | Where a pin came from: `advisor` (measured by the quota poller), `response` (inferred from the exhausted reply), `account` (the reply refused the whole account); empty when no pin governs |
| `backed_off` | bool | always | Whether the probe backoff is in force on this circuit |
| `failed_probes` | int | always | Half-open probes that failed since the circuit last closed |
| `cooldown_ms` | int | on `open` | The cooldown actually enforced |
| `next_retry_at` | string (RFC3339) | on `open`, when `quota_pinned` or `backed_off` is `true` | When the circuit is next eligible to probe |

`next_retry_at` is the retry deadline, not the provider's quota reset time: it is the open moment plus the cooldown actually in force, and the longer of a pin and a backoff governs. A pin can also be lifted early, without any event, when a quota refresh shows the provider back in credit. See [Quota-pinned cooldowns](Failover-and-Hotel-Routing#quota-pinned-cooldowns).

`circuit_breaker.unstable` is not a state transition and carries a smaller block: `provider_id`, `provider`, `model`, `model_id`, `opens` (always 3) and `window` (the string `24h`). It fires at most once per model per window and never disables anything: a model the provider has stopped serving is retired by the model-gone path instead.

**Quota schema drift metadata:**

| Field | Type | Meaning |
|-------|------|---------|
| `provider_id` | string (UUID) | The provider that reshaped its quota response |
| `provider` | string | Provider name |
| `provider_type` | string | Provider family (`openai`, `anthropic`, …) |
| `kind` | string | Which quota document changed |
| `added` | array of string | Key paths present now but not in the stored baseline |
| `removed` | array of string | Key paths that were in the baseline and have gone |

`quota.schema_drift` is alert-only: it never affects routing, failover, or the circuit breaker. It exists because the failure it guards against is silent: a normalizer written against the old shape keeps answering, wrongly, and nothing else would ever say so.

---

### WebAuthn / Passkeys

Available only when `WEBAUTHN_RP_ID` is configured (see [Security](Security) for the full authentication flow).

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/api/webauthn/available` | GET | None (public) | Check if WebAuthn is enabled (`{"enabled": true/false}`) |
| `/api/webauthn/login/start` | POST | IP rate-limited | Begin passkey login |
| `/api/webauthn/login/finish` | POST | IP rate-limited | Complete passkey login, receive session token |
| `/api/webauthn/register/start` | POST | Admin/session token | Begin credential registration |
| `/api/webauthn/register/finish` | POST | Admin/session token | Complete credential registration |
| `/api/webauthn/credentials` | GET | Admin/session token | List registered credentials |
| `/api/webauthn/credentials/{id}` | PATCH | Admin/session token | Rename a credential |
| `/api/webauthn/credentials/{id}` | DELETE | Admin/session token | Delete a credential |
| `/api/webauthn/logout` | POST | Admin/session token | Revoke the current session token |

### TOTP / Authenticator-App 2FA

Time-based one-time passwords (RFC 6238) as an admin-login second factor, independent of passkeys. Opt-in at runtime from Settings; no environment variable required (see [Security](Security) for the full authentication flow and enforcement model).

| Route | Method | Auth | Description |
|-------|--------|------|-------------|
| `/api/totp/status` | GET | None (public) | Report whether TOTP is enabled (`{"enabled": true/false}`) |
| `/api/totp/login` | POST | IP rate-limited | Exchange admin token + 6-digit code (or a recovery code) for a session token |
| `/api/totp/info` | GET | Admin/session token | Enrollment state and remaining recovery-code count |
| `/api/totp/enroll/start` | POST | Admin/session token | Begin enrollment; returns the otpauth URI + base32 secret |
| `/api/totp/enroll/verify` | POST | Admin/session token | Verify the first code, enable TOTP, return recovery codes + a session token |
| `/api/totp/disable` | POST | Admin/session token | Disable TOTP (gated on a current code or recovery code) |

When TOTP is enabled, the raw admin token alone no longer authorizes `/api/*`: it is a first factor that must be exchanged via `/api/totp/login` for a session token.

---

### Version

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/version/latest` | GET | Latest released version tag (fetched from GitHub, cached), used by the dashboard update notice |

---

### Chat & Arena (Admin)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/chat/chat` | POST | Interactive chat session (admin-authenticated, single model) |
| `/api/chat/arena` | POST | Arena mode (admin-authenticated, multi-model comparison) |
| `/api/chat/completions` | POST | Admin-authenticated chat completion (single model) |

These endpoints proxy through the same completion handler as `/v1/chat/completions`, but authenticate a dashboard session instead of a virtual key: any signed-in identity holding the chat grant may use them, not only an admin. Streaming and non-streaming both work as on `/v1`.

Three limiters apply in order: the per-IP limiter every `/api` route carries, the per-key RPS limiter (bucketed on the route name, `chat`/`arena`/`completions`, since there is no virtual key here), and the caller's own per-user tokens-per-minute cap. A user with a TPM cap is metered here exactly as on `/v1`.

**Request/Response:** Same format as `/v1/chat/completions`

---

### Other endpoints

Routes that exist but have no section of their own. Everything under `/api` carries the same admin/session authentication unless the row says otherwise.

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/metrics` | GET | `METRICS_TOKEN` or admin token | Prometheus metrics; never anonymous, and outside the per-IP limiter so scrapers are not throttled |
| `/api/public-config` | GET | None (public) | Feature flags the login screen needs (e.g. read-only demo mode) |
| `/api/demo-login` | GET | None (public) | Demo-mode login helper |
| `/api/auth/status` | GET | None (public) | Whether password login is available |
| `/api/auth/login` | POST | None (IP rate-limited) | Password login; mints a session |
| `/api/auth/admin-exchange` | POST | None (IP rate-limited) | Trade a raw admin token for an HttpOnly session cookie |
| `/api/auth/logout` | POST | Session | End the current session |
| `/api/auth/me` | GET | Any signed-in identity | The caller's identity, role and grants |
| `/api/auth/password` | POST | Any signed-in identity | Rotate the caller's own password |
| `/api/auth/sessions` | GET | Any signed-in identity | The caller's own active sessions |
| `/api/auth/sessions/{id}` | DELETE | Any signed-in identity | Revoke one of the caller's own sessions |
| `/api/auth/sessions/revoke-others` | POST | Any signed-in identity | Sign the caller's other sessions out |
| `/api/auth/totp/status`, `/enroll/start`, `/enroll/verify`, `/disable` | GET/POST | Any signed-in identity | Per-user TOTP self-service (the `/api/totp/*` routes above are the admin-level equivalents) |
| `/api/auth/oidc/status`, `/start`, `/callback` | GET | None (the ceremony is the login) | OIDC single sign-on |
| `/api/auth/github/status`, `/start`, `/callback` | GET | None (the ceremony is the login) | GitHub single sign-on |
| `/api/users` | GET/POST | Admin | List and create accounts |
| `/api/users/grants` | GET | Admin | The catalogue of assignable grants |
| `/api/users/{id}` | PUT/DELETE | Admin | Update or delete an account |
| `/api/users/{id}/password` | POST | Admin | Set another account's password |
| `/api/users/{id}/totp/reset` | POST | Admin | Clear an account's TOTP enrollment |
| `/api/alert/events` | GET | Admin | The catalogue of alertable event types |
| `/api/alert/status` | GET | Admin | Whether outbound alerting is configured and reachable |
| `/api/alert/targets` | GET | Admin | The configured apprise targets |
| `/api/alert/probe` | POST | Admin | Check that apprise-api answers |
| `/api/alert/test` | POST | Admin | Send a test notification |
| `/api/config/export` | GET | Admin | Fleet config export (the primary's side of config sync) |
| `/api/config/version` | GET | Admin | The exporting member's config version, for the skew check |
| `/api/config/import` | POST | Admin | Apply an exported config on this member |
| `/api/config/quota-snapshots` | GET/POST | Admin | Export or receive fleet quota snapshots (no key material, so no `MASTER_KEY` canary) |
| `/api/fleet/announce` | POST | Admin | Front Desk's membership heartbeat |

---

## Health Endpoint

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/health` | GET | None | Database reachability: `200 OK` or `503 DEGRADED` |

### GET `/health`

Returns `200` with the body `OK` while the database answers, and `503` with the body `DEGRADED` when it does not, so a load balancer stops routing to an instance whose Postgres is down. The probe result is cached briefly, so a burst of health checks costs one database round-trip. Content type is `text/plain`; no authentication is required.

---

## Error Responses

### Common Error Format

```json
{
  "error": {
    "message": "error description",
    "type": "invalid_request_error",
    "code": 400
  }
}
```

`code` repeats the HTTP status as a number. `type` is derived from that status and is one of exactly six values; messages are lowercase.

| Status | `type` |
|--------|--------|
| `401` | `authentication_error` |
| `403` | `permission_error` |
| `404` | `not_found_error` |
| `429` | `rate_limit_error` |
| `500` and above | `server_error` |
| anything else | `invalid_request_error` |

### HTTP Status Codes

| Code | Description | Common Causes |
|------|-------------|---------------|
| `200` | OK | Successful request |
| `201` | Created | Resource created successfully |
| `204` | No Content | Successful deletion |
| `400` | Bad Request | Invalid request body, validation errors |
| `401` | Unauthorized | Missing or invalid authentication |
| `403` | Forbidden | Authenticated but not permitted (missing grant, read-only demo, managed fleet member) |
| `404` | Not Found | Resource not found |
| `409` | Conflict | Duplicate resource, operation in progress |
| `412` | Precondition Failed | Missing dependency (e.g. `pg_dump`) |
| `429` | Too Many Requests | Rate limit exceeded |
| `500` | Internal Server Error | Server error |
| `502` | Bad Gateway | Upstream provider error |

### Proxy-Specific Errors

**Invalid Virtual Key:**
```json
{
  "error": {
    "message": "invalid virtual key",
    "type": "authentication_error",
    "code": 401
  }
}
```

**Rate Limit Exceeded:**
```json
{
  "error": {
    "message": "rate limit exceeded",
    "type": "rate_limit_error",
    "code": 429
  }
}
```

**Model Not Found:**
```json
{
  "error": {
    "message": "model not found: hotel/gpt-5",
    "type": "not_found_error",
    "code": 404
  }
}
```

**No Provider Left:**
```json
{
  "error": {
    "message": "virtual key does not have access to any provider for this model",
    "type": "permission_error",
    "code": 403
  }
}
```

---

## Authentication Summary

| Route Group | Auth Method | Token Format |
|-------------|-------------|--------------|
| `/v1/*` | Virtual Key | `Bearer sk-...` |
| `/api/*` | Admin Token (or WebAuthn/TOTP session) | `Bearer <admin-token>` |
| `/api/events` | Admin Token (or WebAuthn/TOTP session) | `Bearer <admin-token>` |
| `/api/chat/*` | Any signed-in identity holding the chat grant | `Bearer <admin-token>` or session cookie |
| `/api/webauthn/available`, `/api/webauthn/login/*` | None (IP rate-limited) | - |
| `/api/totp/status` | None (public) | - |
| `/api/totp/login` | None (IP rate-limited) | - |
| `/health` | None | - |

---

## Rate Limiting

### Per-Key Rate Limits

Virtual keys can have custom rate limits configured. If not set, global defaults apply.

**Headers (when rate limited):**
```
Retry-After: 60
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 0
X-RateLimit-Burst: 20
```

`Retry-After` is only set when a wait was computed. The per-IP limiter adds `X-RateLimit-Scope: ip`, so a client can tell an address-wide refusal from a key-wide one.

### Per-IP Rate Limits

Applied to all routes, configurable via settings:

| Setting | Default | Description |
|---------|---------|-------------|
| `rate_limit_ip_rps` | 30 | Requests per second per IP |
| `rate_limit_ip_burst` | 60 | Burst capacity |

Trusted proxies (via `TRUSTED_PROXIES` env var) use the `X-Forwarded-For` header for IP identification.

---

## CORS

CORS is configurable via the `CORS_ORIGINS` environment variable (comma-separated list).

**Response Headers (when origin matches):**
```
Access-Control-Allow-Origin: <origin>
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
Access-Control-Allow-Credentials: true
Access-Control-Max-Age: 86400
```

**Preflight:** `OPTIONS` requests return `204 No Content` when the origin is allowed.

---

## Security Headers

All responses include:

```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: strict-origin-when-cross-origin
Content-Security-Policy: default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'
```

With `ALLOW_EMBED=true` the two framing controls are dropped, `X-Frame-Options` entirely and `frame-ancestors 'none'` out of the CSP, so any origin can put the dashboard in an iframe (workspace browsers, Home Assistant). Nothing else about the policy changes.

HSTS (`Strict-Transport-Security: max-age=63072000; includeSubDomains; preload`) is set only when this process terminated the TLS connection itself. Behind a reverse proxy that terminates TLS and forwards plain HTTP, the gateway does not set it, because a cached HSTS pin would point browsers at an HTTPS listener that does not exist; set the header on the proxy instead.
