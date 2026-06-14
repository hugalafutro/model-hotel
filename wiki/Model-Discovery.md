# 🔍 Model Discovery

Model discovery is the process by which Model Hotel learns about available models from configured providers. Discovery fetches model lists from provider APIs, enriches them with metadata from built-in catalogs and the models.dev community database, and upserts the results into the PostgreSQL database.

<p align="center">
<img src="screenshots/models.png" alt="Models Page" width="700"><br>
<em>Model catalog with provider, pricing, context length, and enabled toggle columns</em>
</p>

<p align="center">
<img src="screenshots/modeldetailsmodal.png" alt="Model Detail Panel" width="500"><br>
<em>Model detail panel with configuration, pricing, capabilities, and test button</em>
</p>

---

## Table of Contents

- [Discovery Triggers](#discovery-triggers)
- [Provider-Specific Discovery](#provider-specific-discovery)
- [Models.dev Enrichment](#modelsdev-enrichment)
- [Model Metadata Fields](#model-metadata-fields)
- [Database Schema](#database-schema)
- [Model ID Construction](#model-id-construction)
- [Enabling/Disabling Models](#enablingdisabling-models)
- [Model CRUD API](#model-crud-api)
- [Model Caching](#model-caching)

---

## Discovery Triggers

Discovery runs are triggered by four mechanisms:

### 1. On Startup

When `discovery_on_startup` is `true` (the default), the server runs discovery for all enabled providers immediately after boot. A 5-minute deduplication guard prevents redundant runs - if any provider's `last_discovered_at` is within 5 minutes of the current time, startup discovery is skipped entirely. This avoids double-discovery when the server is restarted quickly (e.g., during a rolling deploy).

```go
if settingsRepo.GetBool(ctx, "discovery_on_startup", true) {
    // check if any provider was discovered within 5 minutes
    // if not, run discovery in a background goroutine
}
```

### 2. On Provider Create

When `discovery_on_provider_create` is `true` (the default), discovery is triggered immediately after a new provider is created. This trigger is **client-side**: after the `POST /api/providers` call succeeds, the frontend checks the setting and calls `POST /api/providers/{id}/discover`. For keyless providers (e.g., OpenCode Zen free models), this still works - the discovery service handles empty API keys.

```tsx
// Frontend (Providers.tsx):
const shouldDiscover = settings?.discovery_on_provider_create !== "false";
if (shouldDiscover) {
    const result = await api.providers.discover(newProvider.id);
}
```

### 3. Periodic (Scheduled)

A background goroutine runs discovery on a configurable interval (`discovery_interval`, default 6h). The timer **reacts immediately** to setting changes via a subscription channel - there is no need to wait for the current timer to expire when the interval is changed at runtime. Setting the interval to `0` (or `"0s"`) **disables** periodic discovery entirely; the goroutine blocks on the settings subscription channel until a non-zero value arrives.

```go
select {
case <-timerC:
    result := runDiscovery()
    publishDiscoveryEvent("Scheduled", result)
    interval = readInterval()
    applyInterval(interval)
case <-settingsSub.Events():
    newInterval := readInterval()
    if newInterval != interval { applyInterval(newInterval) }
case <-ctx.Done():
    return
}
```

### 4. Manual (API)

Two manual discovery endpoints are available:

| Endpoint | Method | Scope | Description |
|----------|--------|-------|-------------|
| `/api/providers/{id}/discover` | POST | Single provider | Discover models for one provider; disables models no longer present |
| `/api/providers/discover-all` | POST | All providers | Discover models for every enabled provider; skips disabled providers |

Both endpoints upsert discovered models and call `DisableMissingModels` to disable any models that were previously known but are no longer returned by the provider API. The failover groups of newly disabled models are re-synced in the same scan, so a model that leaves a provider's listing is also pruned from its group instead of lingering as a stale entry (the same cleanup a manual failover sync performs).

Both endpoints also return a `diff` describing what the scan changed - models added, re-enabled, or disabled (with machine-readable reason codes `new_model`, `reappeared`, `not_listed`) plus any failover groups updated or deleted as a result. The dashboard renders this diff as a post-scan summary modal after manual Discover / Discover All runs; an all-empty diff still confirms "scanned, nothing changed". Scheduled background discovery does not pop the modal (SSE events cover it).

```json
{
  "discovered": 14,
  "models": [...],
  "diff": {
    "added": [{"model_id": "gpt-4o-2024-11", "reason": "new_model"}],
    "disabled": [{"model_id": "gpt-4o-2024-05", "reason": "not_listed"}],
    "failover_updated_groups": [
      {"display_model": "gpt-4o", "removed_model_ids": ["uuid-old"]}
    ]
  }
}
```

In `discover-all` responses the same `diff` object appears per provider inside each `results[]` entry (omitted for providers whose scan failed).

---

## Provider-Specific Discovery

Each provider type has its own discovery implementation in `internal/provider/discovery_*.go`. Provider type is auto-detected from the base URL hostname (and path, for OpenCode Go/Zen) via `DetectProviderType`. Unknown hosts default to OpenAI-compatible discovery.

### Live + Catalog Merge

Providers that expose a live model list **and** ship a built-in catalog are combined through a shared helper, `mergeLiveAndCatalog` (`internal/provider/catalog_merge.go`), instead of picking one source or the other. The merge has three rules:

1. **Union of models.** The result is every model the live API returned **plus** every catalog model the API did not list. This surfaces models the provider keeps callable without advertising them in its listing endpoint (a freshly released GLM the listing hasn't caught up to, or older Grok models xAI keeps serving), and it means new models the provider adds are picked up automatically with no catalog edit.
2. **Live wins per field.** For a model present in both sources the live value is authoritative. The catalog only fills fields the live result left empty, nil, or a known placeholder (a `display_name` equal to the `model_id`, or `"[]"` modalities). A stale catalog can therefore never mask fresh live data - at worst it supplies slightly outdated gap-fill.
3. **Capabilities are OR-merged.** A capability flag is enabled in the result if either source reports it.

models.dev enrichment runs *after* the merge and fills anything still empty, so the final precedence per field is **live → catalog → models.dev → zero value**. If the live fetch fails entirely (network, auth, 403/429 quota), the discoverer falls back to the pure catalog so discovery never goes dark.

Providers on the merge (union): **Z.AI**, **xAI**, **DeepSeek**, **OpenCode Go**, **OpenCode Zen**. **OpenAI** uses the same live-first model but **backfill-only** (no union) via `backfillLiveFromCatalog`, because discoverOpenAI is the fallback for unknown/custom hosts and must not attach catalog-only gpt-5.x models to them. Providers with a *pricing-only* catalog - **Anthropic**, **Google AI Studio**, **Cohere** - keep their own discoverers: the live API is already the rich model-list source and the catalog only backfills pricing, so there is nothing to union. Pure-live providers (NanoGPT, OpenRouter, Ollama, LM Studio, KoboldCPP, NeuralWatt) have no catalog.

### Provider Type Detection

The `DetectProviderType` function in `internal/provider/discovery.go` uses exact host matching and suffix matching:

| Hostname Pattern | Path Pattern | Provider Type |
|------------------|--------------|---------------|
| `api.openai.com`, `*.openai.com` | - | `openai` (fallback) |
| `api.anthropic.com`, `*.anthropic.com` | - | `anthropic` |
| `api.deepseek.com`, `*.deepseek.com` | - | `deepseek` |
| `api.nano-gpt.com`, `nano-gpt.com` | - | `nanogpt` |
| `api.z.ai`, `z.ai`, `*.z.ai` | - | `zai-coding` |
| `ollama.com`, `*.ollama.com` | - | `ollama-cloud` |
| `opencode.ai`, `*.opencode.ai` | `/zen/go/` | `opencode-go` |
| `opencode.ai`, `*.opencode.ai` | `/zen/` | `opencode-zen` |
| `openrouter.ai`, `*.openrouter.ai` | - | `openrouter` |
| `api.x.ai`, `x.ai`, `*.x.ai` | - | `xai` |
| `generativelanguage.googleapis.com`, `*.googleapis.com` | - | `google` |
| `api.cohere.com`, `api.cohere.ai`, `*.cohere.com`, `*.cohere.ai` | - | `cohere` |
| `api.neuralwatt.com`, `neuralwatt.com` | - | `neuralwatt` |
| `localhost`, `127.0.0.1`, `::1` (port 11434) | - | `ollama` |
| `localhost`, `127.0.0.1`, `::1` (port 5001) | - | `koboldcpp` |
| `localhost`, `127.0.0.1`, `::1` (port 1234) | - | `lmstudio` |
| Any other host | - | `openai` (fallback) |

### OpenAI

**Source files:** `discovery_openai.go`, `openai_catalog.go`, `catalog_merge.go`

**Method:** Calls `GET /v1/models`, converts the listing to clean stubs (id + owner), and **backfills** matching models from the built-in `openaiCatalog` (the gpt-5.x family) via `backfillLiveFromCatalog` - *not* a union. discoverOpenAI is also the fallback for unknown/custom hosts, so the catalog must never add catalog-only models (that would attach phantom gpt-5.x models to a custom OpenAI-compatible provider); for real OpenAI the catalog is a subset of the live listing anyway. The ~110 uncatalogued models (gpt-4o, the o-series, etc.) are enriched by models.dev instead of the old fabricated empty entry.

- Models covered by the catalog receive full metadata: display name, description, context length, max output tokens, modality, input/output modalities, streaming/reasoning/tool-calling/structured-output/vision flags, pricing (including cache-hit pricing).
- Models **not** in the catalog pass through as clean stubs (`Streaming: true`, empty modalities) for models.dev to fill.

**Catalog fields provided:**

| Field | Source |
|-------|--------|
| Display name | Catalog |
| Description | Catalog |
| Context length | Catalog |
| Max output tokens | Catalog |
| Modality | Catalog |
| Input/Output modalities | Catalog |
| Streaming | Catalog |
| Reasoning | Catalog |
| Tool calling | Catalog |
| Structured output | Catalog |
| Vision | Catalog |
| Input price / cache-hit price / output price | Catalog |
| Owned by | API (`/v1/models`) |

### Anthropic

**Source files:** `discovery_anthropic.go`, `anthropic_catalog.go`

**Method:** Calls `GET /v1/models?limit=100` with pagination (using `after_id` cursor) to list all models. The Anthropic API returns rich capability metadata per model. Pricing is then looked up from the built-in `anthropicPricing` catalog. Date-suffixed model IDs (e.g., `claude-sonnet-4-5-20250514`) are stripped to their base ID for catalog lookup.

**API-provided fields:**

| Field | Source |
|-------|--------|
| Display name | API (`display_name`) |
| Max input tokens (→ context length) | API (`max_input_tokens`) |
| Max output tokens | API (`max_tokens`) |
| Vision | API (`capabilities.image_input.supported`) |
| PDF upload | API (`capabilities.pdf_input.supported`) |
| Structured output | API (`capabilities.structured_outputs.supported`) |
| Modality | Derived from API capabilities (vision → `"vision"`, else `"text"`) |
| Input modalities | Derived from API capabilities (vision → `["text","image"]`, else `["text"]`) |
| Streaming | Hardcoded `true` |
| Tool calling | Hardcoded `true` |
| Output modalities | Hardcoded `[]` |

**Catalog-provided fields:**

| Field | Source |
|-------|--------|
| Input price per million | Pricing catalog |
| Input price cache-hit per million | Pricing catalog |
| Output price per million | Pricing catalog |

### NanoGPT

**Source files:** `discovery_nanogpt.go`

**Method:** Calls `GET /models?detailed=true` - a single request returns complete model metadata. **No dedicated catalog is used.** The API provides all information directly.

**All fields from API:**

| Field | Source |
|-------|--------|
| Display name / name | API (`name`) |
| Description | API (`description`) |
| Context length | API (`context_length`) |
| Max output tokens | API (`max_output_tokens`) |
| Modality | API (`architecture.modality`) |
| Input modalities | API (`architecture.input_modalities`) |
| Output modalities | API (`architecture.output_modalities`) |
| Vision | API (`capabilities.vision`) |
| Video input | API (`capabilities.video_input`) |
| Audio input | API (`capabilities.audio_input`) |
| Reasoning | API (`capabilities.reasoning`) |
| Tool calling | API (`capabilities.tool_calling`) |
| Parallel tool calls | API (`capabilities.parallel_tool_calls`) |
| Structured output | API (`capabilities.structured_output`) |
| PDF upload | API (`capabilities.pdf_upload`) |
| Streaming | Hardcoded `true` |
| Input price / output price | API (`pricing.prompt`, `pricing.completion`) |
| Subscription info | API (`subscription.included`, `subscription.note`) → stored in `params` |
| Owned by | API (`owned_by`) |

### DeepSeek

**Source files:** `discovery_deepseek.go`, `deepseek_catalog.go`, `catalog_merge.go`

**Method:** Calls `GET /models` (OpenAI-compatible list endpoint), converts the listing to clean stubs, and merges them with the built-in `deepseekCatalog` via [`mergeLiveAndCatalog`](#live--catalog-merge). The catalog backfills context length, max output, reasoning flag, and pricing (cache-miss maps to the standard input price; cache-hit is carried separately). The former hardcoded 128k/8k default for uncatalogued models was dropped - an unknown model is now a clean stub filled by models.dev (DeepSeek models are 1M/384K, so the old default was stale).

**Catalog provides:**

| Field | Source |
|-------|--------|
| Context length | Catalog |
| Max output tokens | Catalog |
| Reasoning | Catalog |
| Input price (cache miss) | Catalog |
| Input price (cache hit) | Catalog |
| Output price | Catalog |

**Hardcoded / missing:**

| Field | Value |
|-------|-------|
| Modality | Hardcoded `"text"` |
| Input modalities | Hardcoded `"[]"` |
| Output modalities | Hardcoded `"[]"` |
| Streaming | Hardcoded `true` |
| Tool calling | Hardcoded `true` |
| Vision | Not set |

### Ollama

**Source files:** `discovery_ollama.go`

**Method:** Two-step discovery. First calls `GET /api/tags` to list all locally available models. Then, for each model, calls `POST /api/show` (with the model name) to retrieve detailed metadata. The `/api/show` calls run concurrently (max 5 parallel) with a 120-second overall timeout.

**Fields from `/api/show`:**

| Field | Source |
|-------|--------|
| Capabilities (tools, thinking, vision) | API (`capabilities` array) |
| Context length | API (`model_info` → `*.context_length`) |
| Model family (→ owned_by) | API (`details.family`) |
| Format | API (`details.format`) - not stored in model |
| Parameter size | API (`details.parameter_size`) - not stored in model |
| Quantization level | API (`details.quantization_level`) - not stored in model |

**Derived from capabilities:**

| Field | Logic |
|-------|-------|
| Tool calling | `"tools"` in capabilities array |
| Reasoning | `"thinking"` in capabilities array |
| Vision | `"vision"` in capabilities array |
| Modality | Vision → `"vision"`, else `"text"` |
| Input modalities | Vision → `["text","image"]`, else `["text"]` |

**Hardcoded / missing:**

| Field | Value |
|-------|-------|
| Streaming | Hardcoded `true` |
| Output modalities | Hardcoded `"[]"` |
| Pricing | None (Ollama is local, no pricing) |
| Max output tokens | None |
| Structured output | Not set |

### Z.AI (Zhipu)

**Source files:** `discovery_zai.go`, `zai_catalog.go`, `catalog_merge.go`

**Method:** Fetches the live OpenAI-compatible model list from `GET /models` on the coding-plan base URL, then merges it with the built-in `zaiCatalog` via [`mergeLiveAndCatalog`](#live--catalog-merge). The live listing supplies the authoritative model set and `owned_by`; the catalog backfills context length, max output, capability flags, and modality, and unions in catalog models the listing omits (a freshly released GLM, or the vision/turbo variants the coding plan serves but does not advertise). If the `/models` fetch fails, discovery falls back to the pure catalog.

**Live API provides:**

| Field | Source |
|-------|--------|
| Model list | API (`GET /models`) |
| Owned by | API (`owned_by`; `"z-ai"` normalized to `"zhipu"`) |

**Catalog backfills (live wins where present):**

| Field | Source |
|-------|--------|
| Context length | Catalog |
| Max output tokens | Catalog |
| Reasoning | Catalog |
| Tool calling | Catalog |
| Structured output | Catalog |
| Modality | Catalog |

**Derived from catalog modality:**

| Field | Logic |
|-------|-------|
| Vision | `modality == "vision"` |
| Video input | `modality == "vision"` |
| Input modalities | Vision → `["text","image","video","file"]`, else `["text"]` |

**Hardcoded / missing:**

| Field | Value |
|-------|-------|
| Streaming | Hardcoded `true` (catalog entries) |
| Output modalities | Hardcoded `"[]"` |
| Pricing | None |

### OpenCode Go

**Source files:** `discovery_opencode_go.go`, `opencode_go_catalog.go`, `opencode_catalog_types.go`, `catalog_merge.go`

**Method:** Calls `GET /models` (OpenAI-compatible list endpoint), converts the listing to clean stubs, and merges them with the built-in catalog via [`mergeLiveAndCatalog`](#live--catalog-merge) - catalog backfills the metadata below, and newer models the catalog doesn't cover yet surface from live + models.dev. A `404` (endpoint gone / over-quota historically) falls back to the full catalog; other non-200s abort the scan so a transient outage can't disable live-only models. (Quota overrun does not gate the listing - it still returns `200`.)

**Catalog provides (full `OpenCodeModelSpec`):**

| Field | Source |
|-------|--------|
| Display name | Catalog |
| Description | Catalog |
| Context length | Catalog |
| Max output tokens | Catalog |
| Modality | Catalog |
| Input modalities | Catalog |
| Output modalities | Catalog |
| Streaming | Catalog |
| Reasoning | Catalog |
| Tool calling | Catalog |
| Structured output | Catalog |
| Vision | Catalog |
| Input price / cache-hit price / output price | Catalog (all zero - subscription-based) |

### OpenCode Zen

**Source files:** `discovery_opencode_zen.go`, `opencode_zen_catalog.go`, `opencode_catalog_types.go`, `catalog_merge.go`

**Method:** For **keyed** providers, same as OpenCode Go - `GET /models` merged with the catalog via [`mergeLiveAndCatalog`](#live--catalog-merge). For **keyless** providers (no API key), the merge is bypassed: only free (zero-priced) catalog models the live listing includes are returned, with no union, since a keyless caller must not be shown models it cannot reach.

The catalog and model conversion logic is shared with OpenCode Go via `OpenCodeModelSpec` and `OpenCodeCatalogToModel`. (OpenCode Zen rotates free models aggressively; stale delisted free/preview entries are pruned from the catalog rather than unioned in as dead models.)

### xAI (Grok)

**Source files:** `discovery_xai.go`, `xai_catalog.go`, `xai_types.go`, `catalog_merge.go`

**Method:** Live-plus-catalog merge via [`mergeLiveAndCatalog`](#live--catalog-merge). The live model list is obtained with a tiered strategy, then merged with the catalog:

1. **Funded accounts**: Calls `GET /language-models` - a proprietary endpoint that returns rich data including pricing (cents per 100M tokens, converted to USD/1M) and input/output modalities. These live fields are kept as-is.
2. **No-access accounts (403/429)**: xAI returns 403 for unauthorized keys and 429 for accounts that have exhausted credits or reached spending limits. Discovery falls back to the pure static catalog in both cases.
3. **Other failures / empty list**: Falls back to `GET /v1/models` (minimal OpenAI-compatible: id + owner).

The live result is then merged with the catalog. The catalog **backfills** the fields xAI's API does not report (context window, max output, reasoning flag, friendly display name) and **unions in** catalog grok models the listing endpoints don't advertise but that remain callable (verified: all catalog grok ids return 200). Live values always win - unlike the previous implementation, the catalog no longer overrides live data, and no placeholder description (`"xAI language model (vX)"`) or hardcoded `"text"` modality is fabricated, so a real catalog description/modality is never masked.

**Live API provides (from `/language-models`):**

| Field | Source |
|-------|--------|
| Input modalities | API (`input_modalities`) |
| Output modalities | API (`output_modalities`) |
| Input price | API (`prompt_text_token_price`) - converted from cents/100M to USD/1M, set only when > 0 |
| Cache-hit price | API (`cached_prompt_text_token_price`) - converted |
| Output price | API (`completion_text_token_price`) - converted, set only when > 0 |
| Owned by | API (`owned_by`) |
| Streaming / Tool calling / Structured output | Hardcoded `true` |
| Vision | Derived from API input modalities (image present) |

**Catalog backfills (live wins where present):**

| Field | Source |
|-------|--------|
| Display name | Catalog (live emits the raw id as a placeholder) |
| Description | Catalog |
| Context length | Catalog (API does not report it) |
| Max output tokens | Catalog (API does not report it) |
| Reasoning | Catalog (OR-merged into live capabilities) |

**Pricing conversion:** xAI reports prices in cents per 100 million tokens. Conversion: `$per_1M = cents_per_100M / 100`.

### OpenRouter

**Source file:** `discovery_openrouter.go`

**Method:** Calls `GET /models` to list available models from OpenRouter's unified API. Responses are parsed into `OpenRouterModelsResponse` which provides rich metadata per model.

**API-provided fields:**

| Field | Source |
|-------|--------|
| Display name | API (`name`) |
| Description | API (`description`) |
| Context length | API (`context_length`), falls back to `top_provider.context_length` |
| Max output tokens | API (`top_provider.max_completion_tokens`) |
| Modality | API (`architecture.modality`) |
| Input modalities | API (`architecture.input_modalities`) |
| Output modalities | API (`architecture.output_modalities`) |
| Input price (per 1M tokens) | API (`pricing.prompt`) - converted from per-token |
| Cache-hit price (per 1M tokens) | API (`pricing.input_cache_read`) - converted from per-token |
| Output price (per 1M tokens) | API (`pricing.completion`) - converted from per-token |
| Owned by | Derived from model ID prefix (e.g., `openai` from `openai/gpt-4.1`) |

**Capability mapping:** OpenRouter models report `supported_parameters` which are mapped to capabilities:

| Parameter | Capability |
|-----------|------------|
| `tools` | Tool calling |
| `reasoning` | Reasoning |
| `structured_outputs` | Structured output |
| *(all models)* | Streaming (hardcoded `true`) |

**Model filtering:** Models with IDs starting with `~` (auto-routing aliases) are skipped. Models whose output modalities exclude `text` or `code` (image-only, embedding-only) are also skipped.

**Pricing conversion:** OpenRouter reports prices as per-token strings (e.g., `"0.000002"`). These are converted to $/1M tokens by multiplying by 1,000,000.

### Google AI Studio (Gemini)

**Source files:** `discovery_google.go`, `google_catalog.go`, `google_types.go`

**Method:** Uses Google's native Gemini API (`GET /v1beta/models?key=KEY`) for discovery, which provides rich metadata including context windows, max output tokens, supported generation methods, and thinking support. The base URL is configured for the OpenAI-compatible proxy endpoint (`/v1beta/openai`), but discovery internally converts to the native API URL.

Model IDs from the native API have a `models/` prefix (e.g., `models/gemini-2.5-flash`) which is stripped for internal use.

**API-provided fields (from `/v1beta/models`):**

| Field | Source |
|-------|--------|
| Display name | API (`displayName`) |
| Description | API (`description`) |
| Context length | API (`inputTokenLimit`) |
| Max output tokens | API (`outputTokenLimit`) |
| Reasoning (thinking) | API (`thinking`) |
| Generation methods | API (`supportedGenerationMethods`) |
| Streaming | Derived (has `generateContent` method) |

**Pricing catalog-provided fields:**

| Field | Source |
|-------|--------|
| Input price per million | Pricing catalog |
| Input price cache-hit per million | Pricing catalog |
| Output price per million | Pricing catalog |

**Derived from model name:**

| Field | Logic |
|-------|-------|
| Vision | Name contains `gemini-2`, `gemini-3`, or `gemma` (excluding embedding/tts/live) |
| Tool calling | Not embedding/imagen/veo/lyria/aqa/tts/live |
| Structured output | Same as tool calling |
| Modality | Default `text`; image gen models get text+image output |
| Input modalities | Vision → `["text","image"]`, audio → `["text","image","audio","video"]` |
| Output modalities | Default `["text"]`; image gen → `["text","image"]`, embedding → `["embedding"]` |

**Model filtering:** Only models supporting `generateContent` or `embedContent` are included. AQA-only models are excluded.

**Auth:** Discovery uses `?key=API_KEY` query parameter (native API). Proxy uses `Authorization: Bearer API_KEY` (OpenAI-compatible endpoint). Google API keys are simple alphanumeric strings starting with `AIzaSy...`.

### Cohere

**Source files:** `discovery_cohere.go`, `cohere_catalog.go`

**Method:** Calls `GET /v1/models` with pagination support to list all available models. The API returns model metadata including context length, pricing, and capabilities. Discovery filters out deprecated models (those marked with `deprecated: true` in the API response). Models are enriched with the built-in `cohere_catalog` which contains 10 models with detailed pricing information.

**API-provided fields:**

| Field | Source |
|-------|--------|
| Model ID | API |
| Display name | API (`name`) |
| Context length | API (`context_length`) |
| Max output tokens | API (`max_output_tokens`) |
| Pricing | API (`pricing`) |
| Tool calling capability | API (`capabilities.tool_calling`) |
| Structured output | API (`capabilities.structured_output`) |
| Vision | API (`capabilities.vision`) |
| Streaming | Hardcoded `true` |
| Input modalities | Derived from capabilities |
| Output modalities | Hardcoded `[]` |

**Capability mapping:** Cohere API `features` array is mapped to capabilities:
- `tools` → tool calling
- `json_mode` → structured output

**Catalog provided fields:**

| Field | Source |
|-------|--------|
| Input price per million | Catalog |
| Output price per million | Catalog |
| Cache-hit price | Catalog |

**Host detection:** `api.cohere.com`, `api.cohere.ai`, and all subdomains of `cohere.com`

### LMStudio

**Source files:** `discovery_lmstudio.go`

**Method:** LMStudio is a local provider that exposes an OpenAI-compatible API at a predictable port (`localhost:1234`). Discovery detects this by port-based detection and fetches models via `GET /v1/models`. No built-in catalog is used.

**Detection:** Port-based detection on `localhost:1234`

**Hardcoded / missing:**

| Field | Value |
|-------|-------|
| Context length | Not set |
| Max output tokens | Not set |
| Pricing | None (local provider) |
| Capabilities | Not set |
| Modality | Not set |

### KoboldCPP

**Source files:** `discovery_koboldcpp.go`

**Method:** KoboldCPP is a local provider that exposes an OpenAI-compatible API at `localhost:5001`. Discovery detects this by port-based detection and fetches models via `GET /v1/models`. No built-in catalog is used.

**Detection:** Port-based detection on `localhost:5001`

**Hardcoded / missing:**

| Field | Value |
|-------|-------|
| Context length | Not set |
| Max output tokens | Not set |
| Pricing | None (local provider) |
| Capabilities | Not set |
| Modality | Not set |

---

## Models.dev Enrichment

**Source file:** `internal/provider/modelsdev.go`

In addition to provider-specific discovery and built-in catalogs, Model Hotel can enrich models using the [models.dev](https://models.dev/) open-source model catalogue. This community-maintained database covers 40+ providers and provides pricing, context limits, capabilities, and modality data for thousands of models.

### How It Works

1. On server startup, a blocking call in `main.go` fetches `https://models.dev/api.json` with the default HTTP client (no explicit timeout).
2. The response is parsed into an in-memory index keyed by model ID.
3. During **every** discovery run (after the provider-specific discovery function returns its model list), each model is passed through the enrichment layer.
4. `EnrichModel` fills **only empty or zero-value fields** - it never overwrites data already populated by the provider API or built-in catalog.
5. If the models.dev fetch fails (network error, timeout, invalid JSON), enrichment is silently disabled. Existing catalogue data is never at risk.

### Matching Logic

Models are matched by their `model_id` using progressive fallback:

1. **Exact match** - `gpt-4o` → `gpt-4o`
2. **Strip date suffix** - `gpt-4o-2024-08-06` → `gpt-4o`
3. **Strip version suffix** - `claude-sonnet-4-5-20250514` → `claude-sonnet-4-5`
4. **Longest prefix match** - finds the models.dev entry with the longest matching prefix

The `LookupFuzzy` function implements this logic, handling date patterns like `YYYY-MM-DD`, `YYYYMMDD`, and version suffixes.

### Fields Enriched

| Field | Condition |
|-------|-----------|
| Display name | Only if empty or same as `model_id` |
| Context length | Only if nil/zero |
| Max output tokens | Only if nil/zero |
| Input price per million | Only if nil/zero |
| Output price per million | Only if nil/zero |
| Input price per million (cache hit) | Only if nil/zero |
| Reasoning capability | Only if false |
| Tool calling capability | Only if false |
| Structured output capability | Only if false |
| Vision capability | Only if false (mapped from `attachment` field) |
| Modality | Only if empty or default `"text"` |
| Input modalities | Only if empty or default `"[]"` |
| Output modalities | Only if empty or default `"[]"` |
| Owned by / family | Only if empty |

**Note:** The `modalityFromModelsDev` function produces `"audio"`, `"multimodal"`, and `"video"` modalities from models.dev data, not just `"text"` and `"vision"`.

### Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `MODELSDEV_ENABLED` | `true` | Enable or disable models.dev enrichment. When `false`, the fetch is skipped entirely. |

### Gap Coverage

Models.dev is particularly valuable for providers that lack built-in catalogs or have incomplete ones:

| Provider | Built-in Catalog | What models.dev adds |
|----------|-----------------|---------------------|
| **OpenAI** (generic) | GPT-5.x family only | Pricing and specs for older GPT-4.x, o-series, and any new models |
| **Anthropic** | Pricing only | Capabilities, modalities, context limits for Claude models |
| **DeepSeek** | 2 models (v4 only) | Specs for older DeepSeek models and any not yet in the catalog |
| **Ollama** | None | Pricing, capabilities for well-known models available through Ollama |
| **OpenRouter** | None (API-driven) | Pricing and specs for any OpenRouter-hosted model not covered by the API |
| **Any unknown provider** | None | Full metadata for any model that exists in the models.dev database |

---

## Model Metadata Fields

Each discovered model is stored in the `models` database table with the following fields:

| Field | Type | Description |
|-------|------|-------------|
| `id` | UUID | Primary key (generated on discovery) |
| `provider_id` | UUID | Foreign key to the owning provider |
| `model_id` | string | Provider-unique model identifier (e.g., `gpt-5.5`, `claude-sonnet-4-5`) |
| `name` | string | Model name (often same as `model_id`) |
| `description` | string | Human-readable description |
| `display_name` | string | Friendly display name |
| `capabilities` | JSONB | `Capability` struct serialized as JSON |
| `params` | JSONB | Provider-specific parameters (e.g., NanoGPT subscription info) |
| `modality` | string | Primary modality: `"text"`, `"vision"`, `"audio"`, `"video"`, or `"multimodal"` |
| `input_modalities` | JSONB array | Input modality list (e.g., `["text","image"]`) |
| `output_modalities` | JSONB array | Output modality list (e.g., `["text"]` or `[]`) |
| `context_length` | int (nullable) | Maximum context window in tokens |
| `max_output_tokens` | int (nullable) | Maximum output tokens |
| `input_price_per_million` | float (nullable) | Input price per million tokens (USD) |
| `input_price_per_million_cache_hit` | float (nullable) | Per-million-token price for cache hits (e.g., DeepSeek) |
| `output_price_per_million` | float (nullable) | Output price per million tokens (USD) |
| `owned_by` | string | Model creator/owner |
| `enabled` | bool | Whether the model is active for routing |
| `disabled_manually` | bool | Whether the model was disabled by a user (not discovery) |
| `created_at` | timestamptz | When the model was first discovered |
| `last_seen_at` | timestamptz | When the model was last seen during discovery |
| `provider_name` | string | Denormalized provider name (from JOIN) |
| `provider_enabled` | bool | Denormalized provider enabled state (from JOIN) |

### Capability Struct

The `capabilities` JSONB field stores a `Capability` struct with boolean flags:

```json
{
  "streaming": true,
  "vision": false,
  "video_input": false,
  "audio_input": false,
  "reasoning": false,
  "tool_calling": false,
  "parallel_tool_calls": false,
  "structured_output": false,
  "pdf_upload": false
}
```

Not all providers populate every field. Unsupported flags are simply left as `false`.

---

## Database Schema

### Models Table

```sql
CREATE TABLE IF NOT EXISTS models (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id   UUID REFERENCES providers(id) ON DELETE CASCADE,
    model_id      TEXT NOT NULL,
    display_name  TEXT,
    name          TEXT,
    description   TEXT,
    capabilities  JSONB,
    params        JSONB,
    modality      TEXT,
    input_modalities  JSONB,
    output_modalities JSONB,
    context_length    INTEGER,
    max_output_tokens INTEGER,
    input_price_per_million      REAL,
    input_price_per_million_cache_hit REAL,
    output_price_per_million     REAL,
    owned_by    TEXT,
    enabled     BOOLEAN DEFAULT true,
    disabled_manually BOOLEAN DEFAULT false,
    created_at  TIMESTAMPTZ DEFAULT now(),
    last_seen_at TIMESTAMPTZ DEFAULT now(),
    UNIQUE(provider_id, model_id)
);
```

**Indexes:**
- Unique index on `(provider_id, model_id)` - created implicitly by the UNIQUE constraint
- No separate index on `provider_id` alone is needed as the unique index covers it

**Migrations:**
- `001_init.sql` - Initial table creation
- `002_model_seen_and_settings.sql` - Added `last_seen_at`, `owned_by`, `context_length`, `input_price_per_million`, `output_price_per_million`
- `003_model_details.sql` - Added `name`, `description`, `max_output_tokens`, `modality`, `input_modalities`, `output_modalities`
- `021_model_disabled_manually.sql` - Added `disabled_manually` column

### Settings Table (Discovery Configuration)

```sql
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT now()
);
```

Default settings:
- `discovery_interval` = `"6h"`
- `discovery_on_startup` = `"true"`
- `discovery_on_provider_create` = `"true"`

---

## Model ID Construction

Model IDs are **provider-specific identifiers** returned by the provider's API. They are used to uniquely identify a model within a provider's catalog.

### Format

Model IDs follow the format provided by each provider:

| Provider | Example Model IDs |
|----------|-------------------|
| OpenAI | `gpt-4o`, `gpt-4o-2024-08-06`, `o1-preview` |
| Anthropic | `claude-sonnet-4-5-20250514`, `claude-3-5-haiku-20241022` |
| DeepSeek | `deepseek-chat`, `deepseek-reasoner` |
| xAI | `grok-3`, `grok-3-mini` |
| Google | `gemini-2.5-flash`, `gemini-2.0-flash` (stripped from `models/gemini-2.5-flash`) |
| Ollama | `llama3.2:3b`, `gemma3:4b` |
| OpenRouter | `openai/gpt-4.1`, `anthropic/claude-3-5-sonnet` |

### Usage in Routing

Model IDs are used in:
1. **Database lookups** - `GetByModelID()` finds all enabled models with a given model ID across providers
2. **Failover groups** - Models with the same `model_id` from different providers can be grouped for failover
3. **Proxy requests** - The proxy endpoint accepts model IDs in the format `{provider_name}/{model_id}` for admin chat, or just `{model_id}` for virtual key auth

### Composite Key

The unique constraint `(provider_id, model_id)` ensures that each provider can only have one entry per model ID. This composite key is also used for:
- Cache lookups (composite key cache in `internal/model/cache.go`)
- Upsert operations during discovery

---

## Enabling/Disabling Models

### Automatic Enable/Disable During Discovery

When discovery upserts a model, it uses an `ON CONFLICT` strategy:

- **New models** are inserted with `enabled = true`.
- **Existing models** (matched by `provider_id + model_id` unique constraint) are updated with all new metadata. The `enabled` flag is set based on the `disabled_manually` flag:
  - If `disabled_manually = false`, the model is **re-enabled** (it may have been previously disabled by `DisableMissingModels` but is now back).
  - If `disabled_manually = true`, the model **stays disabled** - the user's manual override is respected even if the model reappears in the provider API.
- `last_seen_at` is always updated to `now()`.

```sql
enabled = CASE WHEN models.disabled_manually = false THEN true ELSE models.enabled END
```

### DisableMissingModels

After each discovery run, `DisableMissingModels` is called. This sets `enabled = false` for any model belonging to the provider that was **not** in the discovered set (and was still enabled - already-disabled rows are not touched again on repeat scans). It returns the list of newly disabled models, which the discovery handlers use to (a) re-sync the failover groups those models belonged to, pruning their stale entries, and (b) report them in the scan's `diff` with reason `not_listed`.

Note: `DisableMissingModels` sets `enabled = false` but does **not** set `disabled_manually = true`. This means the model will be automatically re-enabled on the next discovery run if it reappears. On the Models page, discovery-disabled models (`enabled = false`, `disabled_manually = false`) show a tooltip on the status badge: "Not listed by the provider since {date}" (based on `last_seen_at`).

`DisableMissingModels` can only run after a **successful** listing, and it early-returns when the discovered list is empty - a provider outage or auth error aborts the scan before anything is disabled, so "disabled by discovery" always means *"the provider responded and did not list this model"*.

In summary:
- **Auto-disabled** models (removed from the provider API) have `disabled_manually = false` and are re-enabled if they reappear.
- **Manually disabled** models have `disabled_manually = true` and stay disabled even if the model reappears in the provider API.

### Manual Enable/Disable (API)

Users can manually enable or disable a model via:

```http
PATCH /api/models/{id}
Content-Type: application/json

{"enabled": false}
```

This sets both `enabled` and `disabled_manually`:
- `enabled = false`, `disabled_manually = true` - model is disabled and stays disabled across discovery runs.
- `enabled = true`, `disabled_manually = false` - model is re-enabled and will stay enabled.

The `Update` endpoint also supports editing: `display_name`, `context_length`, `max_output_tokens`, `input_price_per_million`, and `output_price_per_million`.

### Summary of Enable/Disable States

| Scenario | `enabled` | `disabled_manually` | Behavior on next discovery |
|----------|-----------|-------------------|---------------------------|
| New model discovered | `true` | `false` | Normal |
| Model disappears from API | `false` | `false` | Will be re-enabled if it reappears |
| User manually disables | `false` | `true` | Stays disabled even if it reappears |
| User manually re-enables | `true` | `false` | Normal |

---

## Model CRUD API

### List Models

```http
GET /api/models?provider_id={uuid}
```

Returns all models, optionally filtered by provider ID.

**Response:**
```json
[
  {
    "id": "uuid",
    "model_id": "gpt-4o",
    "name": "gpt-4o",
    "display_name": "GPT-4o",
    "provider_id": "uuid",
    "provider_name": "OpenAI",
    "capabilities": "{\"streaming\":true,\"vision\":true}",
    "modality": "vision",
    "input_modalities": "[\"text\",\"image\"]",
    "output_modalities": "[\"text\"]",
    "context_length": 128000,
    "enabled": true,
    "created_at": "2024-01-01T00:00:00Z",
    "last_seen_at": "2024-01-01T00:00:00Z"
  }
]
```

### Update Model

```http
PATCH /api/models/{id}
Content-Type: application/json

{
  "display_name": "Custom Name",
  "context_length": 64000,
  "max_output_tokens": 4096,
  "input_price_per_million": 2.5,
  "output_price_per_million": 10.0,
  "enabled": true
}
```

All fields are optional. Updates the model and returns the updated record.

**Validation:**
- `display_name`: 1-128 characters
- `context_length`: 256-2,000,000
- `max_output_tokens`: 1-128,000
- `input_price_per_million`: 0-1000
- `output_price_per_million`: 0-1000

### Delete Model

```http
DELETE /api/models/{id}
```

Removes the model record entirely from the database. Deleted models are not tracked - if discovery runs again and the model is still available at the provider, it will be re-discovered as a new entry with a new UUID.

**Response:** `204 No Content`

### Test Model

```http
POST /api/models/{id}/test
```

Tests a model by making a minimal chat completion request (`"Respond only with 'Hi'"`) and returns latency metrics.

**Request:** No body required.

**Response:**
```json
{
  "success": true,
  "ttft_ms": 150,
  "duration_ms": 450,
  "response": "Hi"
}
```

Or on error:
```json
{
  "success": false,
  "duration_ms": 5000,
  "error": "connection timeout"
}
```

The test request is logged to `request_logs` table with full timing breakdown.

---

## Model Caching

The `internal/model/cache.go` module provides in-memory caching for model lookups with a 5-minute TTL.

### Cache Types

1. **UUID cache** - `GetCachedByUUID(id)` - Returns a single model by its UUID
2. **Model ID cache** - `GetCachedByModelID(modelID)` - Returns all models with a given model ID (across providers)
3. **Composite key cache** - `GetCachedByCompositeKey(providerID, modelID)` - Returns a model by provider + model ID

### Cache Operations

| Function | Description |
|----------|-------------|
| `cacheModelByUUID(model)` | Cache a single model by UUID |
| `cacheModelsByModelID(modelID, models)` | Cache multiple models by model ID string |
| `cacheModelByCompositeKey(providerID, modelID, model)` | Cache by composite key |
| `GetCachedByUUID(id)` | Lookup by UUID |
| `GetCachedByModelID(modelID)` | Lookup by model ID |
| `GetCachedByCompositeKey(providerID, modelID)` | Lookup by composite key |
| `InvalidateModelCache()` | Clear all cache entries (called on every write) |
| `WarmModelCache(models)` | Populate cache with a slice of models |

### Cache Invalidation

The cache is **invalidated on every write operation**:
- `Upsert()` - Called during discovery
- `SetEnabled()` - Manual enable/disable
- `Update()` - Partial updates
- `DeleteByID()` - Model deletion
- `DisableMissingModels()` - Bulk disable

This ensures cache consistency at the cost of cache hit rate during active discovery runs.

---

## Provider Metadata Comparison

The table below summarizes what each provider type supplies during model discovery:

| Provider | Context Length | Pricing | Capabilities | Modalities | Source |
|----------|---------------|---------|-------------|------------|--------|
| OpenAI | Catalog | Catalog | Catalog | Catalog | Live API + catalog (merge) |
| Anthropic | API | Catalog | API | API | Live API + catalog |
| DeepSeek | Catalog | Catalog | Catalog | Catalog | Live API + catalog (merge) |
| Google AI Studio | API | Catalog | API | API | Live API + catalog |
| xAI | Catalog | API | Catalog | API | Live API + catalog (merge) |
| Cohere | API | Catalog | API | API | Live API + catalog |
| NanoGPT | API | API | API | API | Live API |
| Z.AI | Catalog | - | Catalog | Catalog | Live API + catalog (merge) |
| OpenCode Go | Catalog | Catalog | Catalog | Catalog | Live API + catalog (merge) |
| OpenCode Zen | Catalog | Catalog | Catalog | Catalog | Live API + catalog (merge) |
| Ollama | API | - | API | API | Live API |
| Ollama Cloud | API | - | API | API | Live API |
| LMStudio | API | - | API | API | Live API |
| KoboldCPP | API | - | - | - | Live API |
| NeuralWatt | models.dev | models.dev | models.dev | models.dev | OpenAI-compatible `GET /v1/models` (no dedicated discovery; enriched via models.dev) |

---

## Additional Provider APIs

Some providers offer supplementary APIs that are accessible outside of model discovery:

| Provider | Endpoint | API | Description |
|----------|----------|-----|-------------|
| NanoGPT | `GET /usage` | `GetNanoGPTUsage` | Account usage: daily/weekly token counts, image limits, subscription status |
| Z.AI | `GET /api/monitor/usage/quota/limit` | `GetZAIQuota` | Quota limits and usage per model |
| DeepSeek | `GET /user/balance` | `GetDeepSeekBalance` | Account balance (total, granted, topped-up) |
| OpenRouter | `GET /api/v1/credits`, `GET /api/v1/key` | `GetOpenRouterBalance` | Account credits, rate limits, usage limits, free tier status |
| Ollama Cloud | `POST /api/me` | `GetOllamaCloudAccount` | Account information |
| NeuralWatt | `GET /quota` | `GetNeuralWattQuota` | Quota/balance (a 404 means a free-tier key with no quota endpoint - treated as "no data", not an error) |

These are exposed via:
- `GET /api/providers/{id}/usage` - for NanoGPT, Z.AI, OpenRouter, and NeuralWatt
- `GET /api/providers/{id}/balance` - for DeepSeek
- `POST /api/providers/refresh-quotas` - refreshes usage/balance for all supported providers

Quota/balance fetches use a circuit breaker with 5 consecutive failure threshold and 5-minute cooldown.

---

## Related Documentation

- [[Failover & Hotel Routing]] - How discovered models are grouped for automatic failover
- [[Security]] - Provider key encryption and virtual key hashing
- [[Home]] - Architecture overview and feature summary
