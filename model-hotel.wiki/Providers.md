# Providers

Provider management is the foundation of Model Hotel. A provider represents an LLM service (OpenAI, Anthropic, DeepSeek, etc.) with its own API credentials and base URL.

## Provider Types

Model Hotel auto-detects provider types from base URLs:

| Provider | Base URL Pattern | Type Detection |
|----------|------------------|----------------|
| OpenAI | `api.openai.com` | `openai` (fallback) |
| Anthropic | `api.anthropic.com` | `anthropic` |
| DeepSeek | `api.deepseek.com` | `deepseek` |
| NanoGPT | `api.nano-gpt.com` | `nanogpt` |
| Z.AI | `api.z.ai` | `zai` |
| Ollama | `ollama.com` or `localhost` | `ollama` |
| OpenCode Zen | `opencode.ai/zen` | `opencode-zen` |
| OpenCode Go | `opencode.ai/zen/go` | `opencode-go` |
| OpenRouter | `openrouter.ai` | `openrouter` |
| xAI (Grok) | `api.x.ai` | `xai` |
| Google AI Studio (Gemini) | `generativelanguage.googleapis.com` | `google` |
| Cohere | `api.cohere.ai` / `api.cohere.com` | `cohere` |
| Generic | Any other URL | `openai` |

Subdomains are also supported (e.g., `custom.nano-gpt.com` → `nanogpt`).

## Provider Properties

Each provider has the following properties:

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID | Unique provider identifier |
| `name` | string | Descriptive name (e.g., "OpenAI Production") |
| `base_url` | string | Provider API endpoint |
| `enabled` | bool | Whether the provider is active |
| `api_key` | string (write-only) | API key sent in requests; never stored in plaintext |
| `masked_key` | string (read-only) | Display-friendly key preview (e.g., `sk-op***ky`). Set automatically when a provider is created or updated with a new key. The full key is never exposed after initial submission. |
| `encrypted_key` | bytes | AES-256-GCM encrypted API key (never returned in API responses) |
| `last_discovered_at` | timestamp (nullable) | When discovery last ran for this provider |
| `last_used_at` | timestamp (nullable) | When the provider was last used for a proxy request. Updated fire-and-forget with a 5-second timeout. |
| `created_at` | timestamp | When the provider was created |
| `updated_at` | timestamp | When the provider was last updated |

## Adding a Provider

### Via Dashboard

1. Navigate to **Providers** page
2. Click **Add Provider** button
3. Fill in the form:
   - **Name**: Descriptive name (e.g., "OpenAI Production")
   - **Base URL**: Provider API endpoint (must be HTTPS unless `ALLOW_HTTP_PROVIDERS=true`). Pre-defined provider types have a preset base URL that cannot be edited; only the "Custom" type allows a free-form URL.
   - **API Key**: Provider API key (encrypted at rest)
4. Click **Create Provider**
5. Model discovery runs automatically (if `discovery_on_provider_create` is enabled)

> 📸 **Screenshot needed:** Provider creation dialog — showing the form with name, base URL, API key input (with masked display), and provider type auto-detection.

### Via Admin API

```bash
curl -X POST http://localhost:8081/api/providers \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "My OpenAI",
    "base_url": "https://api.openai.com",
    "api_key": "sk-..."
  }'
```

Response:
```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "name": "My OpenAI",
  "base_url": "https://api.openai.com",
  "enabled": true
}
```

## Managing Providers

### Enable/Disable

Disabling a provider:
- Removes its models from `/v1/models` listing
- Prevents routing requests to it
- Preserves configuration and model list

```bash
curl -X PUT http://localhost:8081/api/providers/{id} \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

### Delete Provider

Deleting a provider **permanently removes**:
- Provider configuration
- All associated models
- Failover group entries (if any)
- **Does NOT delete** request logs (for audit trail)

```bash
curl -X DELETE http://localhost:8081/api/providers/{id} \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Provider Quotas & Balance

Some providers expose usage data via their APIs:

### DeepSeek Balance

Fetches account balance in CNY:

```bash
curl http://localhost:8081/api/providers/{id}/balance \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Response:
```json
{
  "balance": 124.50,
  "currency": "CNY"
}
```

**Note**: Balance is fetched on-demand and cached briefly. Displayed in sidebar quota panel if available.

> 📸 **Screenshot needed:** Provider quota panel — showing usage/balance information for a provider that supports quota checking.

### Z.AI & NanoGPT Usage

Fetches token quota and usage:

```bash
curl http://localhost:8081/api/providers/{id}/usage \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

Response:
```json
{
  "quota": 1000000,
  "used": 234567,
  "remaining": 765433
}
```

### Refresh All Quotas

Refresh quotas for all providers that support it:

```bash
curl -X POST http://localhost:8081/api/providers/refresh-quotas \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

## Keyless Providers

Most providers require an API key, but **OpenCode Zen** (`opencode-zen`) is the only provider type that supports keyless access.

To create a keyless provider:

1. Leave **API Key** field empty when creating the provider
2. `encrypted_key` is stored as an empty byte array (`len(prov.EncryptedKey) == 0`)
3. During proxy resolution, keyless providers skip decryption entirely — the API key is sent as an empty string
4. Rate limits and quotas still apply

## OpenRouter

OpenRouter is a provider type that acts as a unified gateway to multiple LLM providers. It uses a standard OpenAI-compatible API.

### Auto-Detection

OpenRouter is auto-detected from base URLs containing `openrouter` (e.g., `https://openrouter.ai/api/v1`).

### Supported Features

- **Balance checking**: OpenRouter exposes balance information via `/api/v1/credits` (total credits and usage) and `/api/v1/key` (rate limits, usage limits, free tier status). Available through the `GET /api/providers/{id}/balance` endpoint and displayed in the sidebar quota panel.
- **Model discovery**: Uses `GET /api/v1/models` (see [Model Discovery](Model-Discovery.md) for details).
- **Quota display**: The frontend shows OpenRouter balance details including credits remaining, rate limit usage, and free tier status.

### Key Configuration Notes

- OpenRouter API keys use the `sk-or-v1-` prefix
- The base URL should point to `https://openrouter.ai/api/v1`
- OpenRouter supports most OpenAI-compatible parameters including streaming, tools, reasoning, and structured outputs

## Provider Host Validation

By default, loopback addresses (`localhost`, `127.0.0.1`) are blocked for security. To allow local providers (e.g., Ollama):

```bash
# In your .env file
ALLOWED_PROVIDER_HOSTS=localhost,127.0.0.1
```

Built-in provider hosts are always allowed and don't need to be listed:
- `api.openai.com`
- `api.nano-gpt.com`
- `api.z.ai`
- `api.deepseek.com`
- `api.anthropic.com`
- `ollama.com`
- `opencode.ai`
- `api.x.ai`
- `generativelanguage.googleapis.com`
- `api.cohere.com`
- `api.cohere.ai`

## Provider Health Monitoring

The **Providers** page shows:
- ✅ **Enabled** status
- 📊 **Last discovered** timestamp
- ⏰ **Last used** timestamp
- 💰 **Quota** or **Balance** (if available)
- ❌ **Error count** (from failed operations)

> 📸 **Screenshot needed:** Providers page — showing the provider list with name, type, enabled status, quota badges, and action buttons.

Quick actions:
- **Discover Models**: Trigger manual discovery
- **Edit**: Update name, base URL, API key
- **Disable/Enable**: Toggle availability
- **Delete**: Permanent removal

## Brand Colors

Each provider type has a brand color used for quota badges and sidebar pills. Colors are defined in `web/src/utils/providerBrands.ts`:

| Provider | Key | Color |
|----------|-----|-------|
| Anthropic/Claude | `anthropic` | `#D97757` |
| OpenAI | `openai` | `#000000` |
| Google/Gemini | `google` | `#4285F4` |
| DeepSeek | `deepseek` | `#4D6BFE` |
| xAI/Grok | `xai` | `#1A1A1A` |
| Ollama | `ollama` | `#3D3D3D` |
| OpenRouter | `openrouter` | `#6366F1` |
| Cohere | `cohere` | `#D4E7C5` |
| Z.AI | `zai-coding` | `#7C3AED` |
| NanoGPT | `nanogpt` | `#0EA5B0` |
| LM Studio | `lmstudio` | `#E879F9` |
| KoboldCpp | `koboldcpp` | `#DC2626` |
| OpenCode | `opencode` | `#2D2D2D` |

Dark brand colors (OpenAI, xAI, Ollama, OpenCode) use lighter text overrides in the sidebar for readability.

## Best Practices

1. **Use descriptive names**: "OpenAI Production" vs "OpenAI"
2. **Separate environments**: Different providers for prod/staging
3. **Enable discovery**: Let Model Hotel pull models automatically
4. **Monitor quotas**: Set up alerts before hitting limits
5. **Disable before delete**: Ensure no active dependencies
6. **Use allowed hosts**: Don't disable URL validation globally

## Troubleshooting

**Provider discovery fails**
- Check API key validity
- Verify base URL is reachable
- Review application logs for error details
- Ensure provider host is in allowed list (for non-standard hosts)

**Models not appearing**
- Verify `discovery_on_provider_create` is enabled
- Check if provider returned models in expected format
- Manually trigger discovery from provider card

**Quota not updating**
- Not all providers expose quota APIs
- Quota is fetched on-demand, not continuously
- Check provider documentation for quota endpoint availability