# 🔗 API Reference

Model Hotel exposes three API surfaces: the **Proxy API** (OpenAI-compatible, for client applications), the **Admin API** (for management and configuration), and a **Health** endpoint without authentication.

## Proxy API (`/v1/*`)

OpenAI-compatible endpoints that require a virtual key. The proxy covers chat completions plus the multimodal OpenAI API surface (embeddings, images, audio); everything else (fine-tuning, files, assistants, batches) is intentionally out of scope: Model Hotel is a proxy, not a full API gateway.

### Authentication

```
Authorization: Bearer <virtual-key>
```

Virtual keys use the `sk-` prefix (e.g. `sk-a1b2c3d4e5f6a7b8`). Keys are created via the Admin API and are shown only once at creation time.

### Endpoints

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/v1/models` | GET | Virtual Key | List available models (OpenAI-compatible format) |
| `/v1/chat/completions` | POST | Virtual Key | Chat completion (streaming and non-streaming) |
| `/v1/embeddings` | POST | Virtual Key | Embeddings (JSON pass-through) |
| `/v1/images/generations` | POST | Virtual Key | Image generation (JSON; SSE streaming via `partial_images`) |
| `/v1/images/edits` | POST | Virtual Key | Image edits (multipart upload) |
| `/v1/images/variations` | POST | Virtual Key | Image variations (multipart upload) |
| `/v1/audio/speech` | POST | Virtual Key | Text-to-speech (binary audio response; SSE via `stream_format`) |
| `/v1/audio/transcriptions` | POST | Virtual Key | Speech-to-text (multipart upload) |
| `/v1/audio/translations` | POST | Virtual Key | Speech translation to English (multipart upload) |

All endpoints share the same model routing (`hotel/<model>` failover or `<provider>/<model>` direct), virtual-key authentication, `allowed_providers` access control, rate limiting, circuit breaker, and request logging. See [Multimodal Endpoints](#multimodal-endpoints) below.

### GET `/v1/models`

Returns the model list in OpenAI-compatible format.

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
    "model": "hotel/gpt-4o",
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

**Model Routing:**

- `hotel/<model>` - Failover routing (tries all providers that offer the model, with automatic failover on 5xx and optionally on 429)
- `<provider>/<model>` - Direct routing to a specific named provider (no failover)

**Streaming Response Format:**
```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":1234567890,"model":"gpt-4o","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

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

The response is the provider's raw binary audio (Content-Type passed through, e.g. `audio/mpeg`), streamed with no buffering. `stream_format: "sse"` responses stream through as SSE.

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
    "message": "Rate limit exceeded",
    "type": "rate_limit_error"
  }
}
```

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
| `/api/providers/{id}/usage` | GET | Get usage/quota info (Z.AI, Nano-GPT, OpenRouter) |
| `/api/providers/{id}/balance` | GET | Get balance info (DeepSeek) |
| `/api/providers/{id}/account` | GET | Get account info (Ollama Cloud) |
| `/api/providers/discover-all` | POST | Trigger discovery for all enabled providers |
| `/api/providers/refresh-quotas` | POST | Refresh quota/balance data for all supported providers |
| `/api/discovery/changes` | GET | List unseen model changes recorded by background (scheduled/startup) discovery |
| `/api/discovery/changes/ack` | POST | Mark recorded background changes as seen (clears the Models nav badge); returns the acked entries |

#### GET `/api/providers`

**Response:**
```json
[
  {
    "id": "uuid",
    "name": "OpenAI",
    "base_url": "https://api.openai.com/v1",
    "enabled": true,
    "model_count": 15,
    "total_tokens": 1234567,
    "last_discovered_at": "2024-01-01T00:00:00Z"
  }
]
```

![Providers Page](screenshots/providers.png)

#### POST `/api/providers`

**Request Body:**
```json
{
  "name": "OpenAI",
  "base_url": "https://api.openai.com/v1",
  "api_key": "sk-..."
}
```

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `name` | string | Yes | 1-100 characters, unique |
| `base_url` | string | Yes | 1-500 characters, must use HTTPS unless `ALLOW_HTTP_PROVIDERS=true` |
| `api_key` | string | No | 1-500 characters (required for most providers, optional for Ollama, KoboldCPP, LMStudio, OpenCode Zen, custom) |

**Response:** `201 Created` with provider object

#### PUT `/api/providers/{id}`

**Request Body:** All fields optional for partial update. Accepts `name`, `base_url`, `api_key`, and `enabled` - unlike POST which does not accept `enabled`.

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
      {"display_model": "gpt-4o", "removed_model_ids": ["uuid-old"], "added_model_ids": ["uuid-new"]}
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

**Response (Z.AI example):**
```json
{
  "total_quota": 1000000,
  "used_quota": 50000,
  "remaining_quota": 950000
}
```

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
| `/api/models` | GET | List all models (optional `?provider_id=` filter) |
| `/api/models/cursor` | GET | Cursor-paginated model listing (keyset pagination for large catalogs) |
| `/api/models/{id}` | PATCH | Update model (enable/disable, edit metadata) |
| `/api/models/{id}` | DELETE | Delete model permanently |
| `/api/models/{id}/test` | POST | Test a model by sending a minimal prompt |

#### GET `/api/models`

**Query Parameters:**

| Parameter | Type | Description |
|-----------|------|-------------|
| `provider_id` | UUID | Filter by provider UUID |

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

**Request Body:** (all fields optional)
```json
{
  "display_name": "Custom Name",
  "context_length": 128000,
  "max_output_tokens": 16384,
  "input_price_per_million": 5.0,
  "output_price_per_million": 15.0,
  "enabled": true
}
```

**Validation:**
- `display_name`: 1-128 characters
- `context_length`: 256-2000000
- `max_output_tokens`: 1-128000
- `input_price_per_million`: 0-1000
- `output_price_per_million`: 0-1000

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
      "display_model": "gpt-4o",
      "display_name": "GPT-4o Failover",
      "description": "Primary failover group",
      "group_enabled": true,
      "auto_created": false,
      "entries": [
        {
          "model_uuid": "uuid",
          "model_id": "gpt-4o",
          "provider_name": "OpenAI",
          "display_name": "GPT-4o",
          "enabled": true,
          "model_enabled": true,
          "provider_enabled": true,
          "context_length": 128000
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
  "display_model": "gpt-4o",
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
| `description` | string | 0-500 characters |
| `group_enabled` | boolean | Enable/disable entire group |
| `priority_order` | array | New priority order of model UUIDs |
| `entry_enabled` | object | Map of model UUID to enabled state |

**Validation:** At least one entry must be enabled for an active failover group.

#### GET `/api/failover-groups/by-model/{model_uuid}`

Find which failover group contains a given model UUID.

**Response:**
```json
{
  "id": "uuid",
  "display_model": "gpt-4o",
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
    "key_preview": "sk-...ab",
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
  "key_preview": "sk-...d6",
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
| `/api/logs/cursor` | GET | Cursor-paginated request log listing (keyset pagination) |
| `/api/logs/{id}` | GET | Get a single request log entry by ID |
| `/api/logs/purge` | DELETE | Purge logs older than a specified period |

> **Caching:** Responses are cached using a `globalLogsCache` keyed by the raw query string. The response includes an `X-Cache: HIT` or `X-Cache: MISS` header.

#### GET `/api/logs`

**Query Parameters:**

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `page` | integer | 1 | Page number |
| `per_page` | integer | 20 | Page size (max 200) |
| `model_id` | string | - | Filter by model ID (partial match) |
| `provider_id` | UUID | - | Filter by provider UUID |
| `status_code` | string | - | Filter by status code (`4xx`, `5xx`, or exact integer; `0` = no response) |
| `endpoint_type` | string | - | Filter by endpoint family: `chat`, `embeddings`, `image`, `tts`, `stt` (unknown values are ignored) |
| `from` | RFC3339 | - | Start timestamp |
| `to` | RFC3339 | - | End timestamp |
| `sort_by` | string | `time` | Sort column: `time`, `model`, `provider`, `status`, `tokens`, `tps`, `ttft`, `duration`, `overhead`, `key` |
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
      "failover_attempt": 0,
      "state": "completed",
      "endpoint_type": "chat",
      "created_at": "2024-01-01T00:00:00Z"
    }
  ],
  "total": 1000,
  "page": 1,
  "per_page": 20
}
```

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
| `/api/logs/app/cursor` | GET | Cursor-paginated app log history (keyset pagination) |
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

### Backups

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/backups` | GET | List available backups |
| `/api/backups` | POST | Create a new backup |
| `/api/backups/restore` | POST | Restore the database from an uploaded backup file |
| `/api/backups/{filename}` | GET | Download a backup file |
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

Creates a PostgreSQL backup using `pg_dump`.

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

#### DELETE `/api/backups/{filename}`

**Response:** `204 No Content`

#### POST `/api/backups/prune-preview`

Preview which backups would be pruned under the son/father/grandfather rotation scheme. Non-destructive (dry run).

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

**Response:**
```json
{
  "rate_limit_enabled": "true",
  "rate_limit_ip_rps": "30",
  "rate_limit_ip_burst": "60",
  "discovery_interval": "6h",
  "discovery_on_startup": "true",
  "circuit_breaker_enabled": "true",
  "theme": "dark",
  "accent_color": "#1dd1a1"
}
```

![Settings Page](screenshots/settings.png)

#### PUT `/api/settings`

**Request Body:**
```json
{
  "rate_limit_ip_rps": "50",
  "discovery_interval": "12h",
  "theme": "light"
}
```

**Allowed Settings:**

| Key | Type | Description |
|-----|------|-------------|
| `rate_limit_enabled` | string | `"true"` or `"false"` |
| `rate_limit_ip_enabled` | string | `"true"` or `"false"` |
| `rate_limit_ip_rps` | float | 0-10000 |
| `rate_limit_ip_burst` | int | 1-10000 |
| `rate_limit_max_wait_ms` | int | 0-10000 |
| `rate_limit_rps` | float | 0-10000 |
| `rate_limit_burst` | int | 1-10000 |
| `rate_limit_tpm` | int | 0-100000000 |
| `request_timeout` | string | Duration (e.g. `"1m0s"`) |
| `failover_on_rate_limit` | string | `"true"` or `"false"` |
| `circuit_breaker_enabled` | string | `"true"` or `"false"` |
| `circuit_breaker_threshold` | int | 1-100 |
| `circuit_breaker_cooldown` | string | Duration |
| `discovery_interval` | string | Duration (e.g. `"6h"`, `"0"` = disabled) |
| `discovery_on_startup` | string | `"true"` or `"false"` |
| `discovery_on_provider_create` | string | `"true"` or `"false"` |
| `log_retention` | string | `"1h"`, `"1d"`/`"24h"`, `"1w"`/`"168h"`, `"1m"`/`"720h"`, `"0"` (disable), or empty (keep forever) |
| `stale_request_timeout` | string | Duration |
| `key_cache_ttl` | string | Duration (e.g. `"10m0s"`) |
| `ttft_timeout` | string | Duration; time-to-first-token probe timeout for streaming (`"0s"` disables) |
| `stream_stall_timeout` | string | Duration; max silence during streaming before termination (`"0s"` disables) |
| `backup_enabled` | string | `"true"` or `"false"` (periodic backup with rotation) |
| `backup_interval` | string | Duration between automatic backups (minimum 300s) |
| `backup_son_retention` | int | 0-365 (daily tier) |
| `backup_father_retention` | int | 0-52 (weekly tier) |
| `backup_grandfather_retention` | int | 0-120 (monthly tier) |

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
| `period` | string | `24h` | Time period: `1h`, `24h`, `7d` |
| `exclude_deleted` | boolean | `true` | Exclude deleted providers/keys |
| `metric` | string | `requests` | Metric for aggregation: `requests` or `tokens` |

**Response:**
```json
{
  "total_requests_last_24h": 12345,
  "total_requests_last_7d": 54321,
  "by_model": {
    "gpt-4o": 5000,
    "claude-3-opus": 3000
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
| `period` | string | `24h` | Time period: `1h`, `24h`, `7d` |
| `exclude_deleted` | boolean | `true` | Exclude deleted providers/keys |
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

data: {"type":"discovery.complete","severity":"success","message":"Discovery complete: 15 models across 2 providers","metadata":{"source":"Startup"}}

: heartbeat
```

**Event Types:**

| Event | Severity | Description |
|-------|----------|-------------|
| `discovery.complete` | `success`/`warning`/`error` | Model discovery finished for a provider |
| `discovery.provider_fetched` | `success` | Fetched models from a provider |
| `discovery.provider_failed` | `error` | Discovery failed for a provider |
| `discovery.enriched` | `info` | Models enriched from models.dev catalogue |
| `discovery.models_disabled` | `warning` | Models were disabled during discovery |
| `discovery.changes_pending` | `info` | Background discovery recorded model changes (badged on the Models nav) |
| `failover.sync_error` | `warning` | Error during failover group synchronization |
| `circuit_breaker.open` | `warning` | Provider circuit breaker opened |
| `circuit_breaker.half-open` | `info` | Circuit breaker probing |
| `circuit_breaker.closed` | `success` | Circuit breaker closed (recovered) |
| `tokens.error` | `error` | Error counting tokens |
| `backup.created` | `success` | Database backup created (manual or scheduled) |
| `backup.deleted` | `info` | Backup deleted |
| `backup.pruned` | `info` | Backup pruned by rotation |
| `request.discovery.provider_starting` | `info` | Starting discovery for a provider |

Heartbeat comments (`: heartbeat`) are sent every 30 seconds.

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
| `/api/totp/enroll/start` | POST | Admin/session token | Begin enrollment; returns the otpauth URI + base32 secret |
| `/api/totp/enroll/verify` | POST | Admin/session token | Verify the first code, enable TOTP, return recovery codes + a session token |
| `/api/totp/disable` | POST | Admin/session token | Disable TOTP (gated on a current code or recovery code) |

When TOTP is enabled, the raw admin token alone no longer authorizes `/api/*`: it is a first factor that must be exchanged via `/api/totp/login` for a session token.

---

### Version

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/version/latest` | GET | Latest released version tag (fetched from GitHub, cached) - used by the dashboard update notice |

---

### Chat & Arena (Admin)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/chat/chat` | POST | Interactive chat session (admin-authenticated, single model) |
| `/api/chat/arena` | POST | Arena mode (admin-authenticated, multi-model comparison) |
| `/api/chat/completions` | POST | Admin-authenticated chat completion (single model) |

These endpoints proxy through the same completion handler as `/v1/chat/completions` but use admin token authentication instead of a virtual key. They support the same streaming and non-streaming modes. Rate limiting applies per-IP on these routes.

**Request/Response:** Same format as `/v1/chat/completions`

---

## Health Endpoint

| Endpoint | Method | Auth | Description |
|----------|--------|------|-------------|
| `/health` | GET | None | Returns `OK` with HTTP 200 |

#### GET `/health`

**Response:**
```
OK
```

This endpoint is intended for load balancer health checks and monitoring. No authentication is required.

---

## Error Responses

### Common Error Format

```json
{
  "error": {
    "message": "Error description",
    "type": "error_type"
  }
}
```

### HTTP Status Codes

| Code | Description | Common Causes |
|------|-------------|---------------|
| `200` | OK | Successful request |
| `201` | Created | Resource created successfully |
| `204` | No Content | Successful deletion |
| `400` | Bad Request | Invalid request body, validation errors |
| `401` | Unauthorized | Missing or invalid authentication |
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
    "message": "Invalid virtual key",
    "type": "authentication_error"
  }
}
```

**Rate Limit Exceeded:**
```json
{
  "error": {
    "message": "Rate limit exceeded",
    "type": "rate_limit_error"
  }
}
```

**Model Not Found:**
```json
{
  "error": {
    "message": "Model not found: hotel/gpt-5",
    "type": "model_not_found"
  }
}
```

**Upstream Provider Error:**
```json
{
  "error": {
    "message": "Upstream provider returned error: Invalid API key",
    "type": "upstream_error",
    "provider": "openai"
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
| `/api/chat/*` | Admin Token (or WebAuthn/TOTP session) | `Bearer <admin-token>` |
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
```

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

HSTS (`Strict-Transport-Security`) is set only over HTTPS connections.
