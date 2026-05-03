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
| xAI (Grok) | `api.x.ai` | `xai` |
| Google AI Studio (Gemini) | `generativelanguage.googleapis.com` | `google` |
| Cohere | `api.cohere.ai` / `api.cohere.com` | `cohere` |
| Generic | Any other URL | `openai` |

Subdomains are also supported (e.g., `custom.nano-gpt.com` → `nanogpt`).

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

Some providers (e.g., OpenCode Zen free tier) don't require API keys:

1. Leave **API Key** field empty when creating provider
2. `encrypted_key` is stored as empty byte array
3. Proxy skips decryption and uses empty string for API key
4. Rate limits and quotas still apply

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

Quick actions:
- **Discover Models**: Trigger manual discovery
- **Edit**: Update name, base URL, API key
- **Disable/Enable**: Toggle availability
- **Delete**: Permanent removal

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