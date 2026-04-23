# OpenCode Go & OpenCode Zen Provider Integration Plan

## Overview

Add two new providers — **OpenCode Go** and **OpenCode Zen** — both operated by OpenCode (Anomaly) but distinct services with different models, pricing, and API characteristics.

- **OpenCode Go** — Low-cost subscription ($5–$10/mo) for open coding models. No `/models` API endpoint; everything must come from a hardcoded catalog.
- **OpenCode Zen** — Pay-as-you-go AI gateway for curated best-in-class models (GPT, Claude, Gemini, Qwen, etc.). Has a `/models` endpoint that returns model IDs but no detailed metadata (no pricing, context windows, or capabilities).

Both use the same API key. Both are OpenAI-compatible at `/chat/completions`, with some Zen models using Anthropic-style `/messages` or OpenAI `/responses` — but for our proxy purposes, the base URL always points to the OpenAI-compatible endpoint path.

---

## API Investigation Results

### OpenCode Zen — `https://opencode.ai/zen/v1`

| Endpoint | Auth Required | Response |
|---|---|---|
| `GET /models` | No | Returns `{object, data: [{id, object, created, owned_by}]}` — 41 models at time of writing. **No pricing, context length, or capabilities.** |
| `GET /models?detailed=true` | No | Identical to `/models` — `detailed` param is ignored. |
| `POST /chat/completions` | Yes | OpenAI-compatible chat completions. |
| `POST /messages` | Yes | Anthropic-compatible messages endpoint (for Claude models). |
| `POST /responses` | Yes | OpenAI responses endpoint (for GPT models). |

**Strategy**: Use `GET /models` to discover which model IDs are currently available, then cross-reference against a hardcoded catalog for full specs (pricing, context length, capabilities, etc.). This is the same hybrid approach used for DeepSeek.

### OpenCode Go — `https://opencode.ai/zen/go/v1`

| Endpoint | Auth Required | Response |
|---|---|---|
| `GET /models` | N/A | **404 — endpoint does not exist.** |
| `POST /chat/completions` | Yes | OpenAI-compatible chat completions. |
| `POST /messages` | Yes | Anthropic-compatible messages endpoint (for MiniMax M2.7/M2.5). |

**Strategy**: No API-based model discovery possible. Must rely entirely on a hardcoded catalog. We can optionally probe models with test requests (like ZAI does), but given the subscription model and rate limits, it's better to just use the full catalog and let discovery disable models that return errors when actually used.

---

## Existing Provider Patterns

The codebase follows a consistent pattern per provider:

| Provider | Discovery Method | Catalog File | API Data | Hardcoded Data |
|---|---|---|---|---|
| NanoGPT | `GET /models?detailed=true` | — | Everything (pricing, caps, context, etc.) | Nothing |
| ZAI | Test each model from catalog | `zai_catalog.go` | Availability only | Context, pricing, caps |
| DeepSeek | `GET /models` + catalog | `deepseek_catalog.go` | Model IDs | Context, pricing, caps |
| Ollama | `GET /api/tags` + `/api/show` per model | — | Everything via show API | Nothing |
| OpenAI | `GET /models` | — | Model IDs only | Minimal (streaming=true) |

**Our approach for OpenCode providers mirrors the DeepSeek pattern**: API returns model IDs → match against catalog for full specs. For Go, the API returns nothing → use catalog directly.

---

## Phase 1: Shared Catalog Types

Both providers share the same catalog spec structure. To avoid duplication, create a shared type in a new file.

### 1.1 New file: `internal/provider/opencode_catalog_types.go`

```go
package provider

// OpenCodeModelSpec describes a model's capabilities and pricing.
// Used by both OpenCode Go and OpenCode Zen catalogs.
type OpenCodeModelSpec struct {
    ModelID                       string  `json:"model_id"`
    DisplayName                   string  `json:"display_name"`
    Description                   string  `json:"description,omitempty"`
    ContextLength                 int     `json:"context_length"`
    MaxOutputTokens               int     `json:"max_output_tokens"`
    Modality                      string  `json:"modality"` // "text" or "vision"
    InputModalities               string  `json:"input_modalities"`  // JSON array string e.g. `["text","image"]`
    OutputModalities              string  `json:"output_modalities"` // JSON array string e.g. `["text"]`
    Streaming                     bool    `json:"streaming"`
    Reasoning                     bool    `json:"reasoning"`
    ToolCalling                   bool    `json:"tool_calling"`
    StructuredOutput              bool    `json:"structured_output"`
    Vision                        bool    `json:"vision"`
    InputPricePerMillion          float64 `json:"input_price_per_million"`
    InputPricePerMillionCacheHit  float64 `json:"input_price_per_million_cache_hit,omitempty"`
    OutputPricePerMillion         float64 `json:"output_price_per_million"`
}

// LookupOpenCodeCatalog finds a spec by model ID in a catalog slice.
// Returns nil if not found.
func LookupOpenCodeCatalog(catalog []OpenCodeModelSpec, modelID string) *OpenCodeModelSpec {
    for i := range catalog {
        if catalog[i].ModelID == modelID {
            return &catalog[i]
        }
    }
    return nil
}

// OpenCodeCatalogToModel converts an OpenCodeModelSpec into a model.Model
// suitable for upsert into the database.
func OpenCodeCatalogToModel(spec *OpenCodeModelSpec, providerID uuid.UUID) *model.Model {
    caps := model.Capability{
        Streaming:        spec.Streaming,
        Reasoning:        spec.Reasoning,
        ToolCalling:      spec.ToolCalling,
        StructuredOutput: spec.StructuredOutput,
        Vision:           spec.Vision,
    }
    capJSON, _ := json.Marshal(caps)

    contextLen := spec.ContextLength
    maxOutput := spec.MaxOutputTokens
    inPrice := spec.InputPricePerMillion
    outPrice := spec.OutputPricePerMillion

    m := &model.Model{
        ID:                    uuid.New(),
        ProviderID:            providerID,
        ModelID:               spec.ModelID,
        Name:                  spec.ModelID,
        DisplayName:           spec.DisplayName,
        Description:           spec.Description,
        Capabilities:          string(capJSON),
        Params:                "{}",
        Modality:              spec.Modality,
        InputModalities:       spec.InputModalities,
        OutputModalities:      spec.OutputModalities,
        ContextLength:         &contextLen,
        MaxOutputTokens:       &maxOutput,
        InputPricePerMillion:  &inPrice,
        OutputPricePerMillion: &outPrice,
        OwnedBy:               "opencode",
        Enabled:               true,
    }

    if spec.InputPricePerMillionCacheHit > 0 {
        cacheHitPrice := spec.InputPricePerMillionCacheHit
        m.InputPricePerMillionCacheHit = &cacheHitPrice
    }

    return m
}
```

**Why a shared function?** Both providers need to convert catalog specs to `model.Model` structs. Without this, the same ~30 lines of struct-building code would be duplicated in both discovery files. The `LookupOpenCodeCatalog` helper also avoids duplicating the linear search pattern.

---

## Phase 2: OpenCode Zen Catalog

### 2.1 New file: `internal/provider/opencode_zen_catalog.go`

Hardcoded specs for all 41 models currently listed in Zen docs. Data sourced from the pricing table and endpoint documentation at `https://opencode.ai/docs/zen/#endpoints`.

```go
package provider

var opencodeZenCatalog = []OpenCodeModelSpec{
    // === Free Models ===
    {
        ModelID: "big-pickle", DisplayName: "Big Pickle",
        Description: "Stealth free model (limited time)",
        ContextLength: 131072, MaxOutputTokens: 16384,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, ToolCalling: true, StructuredOutput: true,
        InputPricePerMillion: 0, OutputPricePerMillion: 0,
    },
    {
        ModelID: "minimax-m2.5-free", DisplayName: "MiniMax M2.5 Free",
        Description: "Free tier (limited time)",
        ContextLength: 1048576, MaxOutputTokens: 16384,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, ToolCalling: true, StructuredOutput: true,
        InputPricePerMillion: 0, OutputPricePerMillion: 0,
    },
    {
        ModelID: "ling-2.6-flash-free", DisplayName: "Ling 2.6 Flash Free",
        Description: "Free tier (limited time)",
        ContextLength: 131072, MaxOutputTokens: 16384,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, ToolCalling: true,
        InputPricePerMillion: 0, OutputPricePerMillion: 0,
    },
    {
        ModelID: "hy3-preview-free", DisplayName: "Hy3 Preview Free",
        Description: "Free tier (limited time)",
        ContextLength: 131072, MaxOutputTokens: 16384,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, ToolCalling: true,
        InputPricePerMillion: 0, OutputPricePerMillion: 0,
    },
    {
        ModelID: "nemotron-3-super-free", DisplayName: "Nemotron 3 Super Free",
        Description: "Free tier (NVIDIA trial, limited time)",
        ContextLength: 131072, MaxOutputTokens: 16384,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, ToolCalling: true,
        InputPricePerMillion: 0, OutputPricePerMillion: 0,
    },

    // === GPT Models (OpenAI-compatible via /responses) ===
    {
        ModelID: "gpt-5.4", DisplayName: "GPT 5.4",
        ContextLength: 200000, MaxOutputTokens: 32768,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, ToolCalling: true, StructuredOutput: true,
        InputPricePerMillion: 2.50, InputPricePerMillionCacheHit: 0.25, OutputPricePerMillion: 15.00,
    },
    // ... (all remaining GPT, Claude, Gemini, Qwen, etc. models)
    // Full catalog derived from docs pricing table
}

func GetOpenCodeZenCatalog() []OpenCodeModelSpec {
    return opencodeZenCatalog
}
```

The actual file will contain all 41 models with their full specs. Context lengths for models not documented will use reasonable defaults (e.g., 200000 for GPT-5.x, 200000 for Claude Opus/Sonnet, 128000 for Haiku, 1M for Gemini, etc.).

**Note on context lengths**: The docs don't explicitly list context windows. We'll use well-known values from the underlying model providers. These can be updated when OpenCode exposes them via API.

---

## Phase 3: OpenCode Go Catalog

### 3.1 New file: `internal/provider/opencode_go_catalog.go`

Hardcoded specs for all 12 Go models. Data sourced from the docs at `https://opencode.ai/docs/go/#endpoints`.

```go
package provider

var opencodeGoCatalog = []OpenCodeModelSpec{
    {
        ModelID: "glm-5.1", DisplayName: "GLM 5.1",
        ContextLength: 200000, MaxOutputTokens: 131072,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
        InputPricePerMillion: 0, OutputPricePerMillion: 0, // subscription-based, no per-token cost
    },
    {
        ModelID: "glm-5", DisplayName: "GLM 5",
        ContextLength: 200000, MaxOutputTokens: 131072,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
        InputPricePerMillion: 0, OutputPricePerMillion: 0,
    },
    {
        ModelID: "kimi-k2.5", DisplayName: "Kimi K2.5",
        ContextLength: 131072, MaxOutputTokens: 16384,
        Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
        Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
        InputPricePerMillion: 0, OutputPricePerMillion: 0,
    },
    // ... all 12 models
}

func GetOpenCodeGoCatalog() []OpenCodeModelSpec {
    return opencodeGoCatalog
}
```

**Note**: Go is subscription-based, so per-token prices are $0. The cost is the flat subscription fee. We may want to add a note in `Description` or `Params` about this.

---

## Phase 4: Discovery Functions

### 4.1 New file: `internal/provider/discovery_opencode_zen.go`

```go
package provider

// discoverOpenCodeZen fetches model IDs from the /models endpoint
// and enriches them with catalog data (pricing, context, capabilities).
func (d *DiscoveryService) discoverOpenCodeZen(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
    // 1. Fetch model IDs from GET /models (no auth needed, but send it anyway)
    baseURL := util.SanitizeBaseURL(provider.BaseURL)
    req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
    // ... standard request setup ...

    var openAIResp OpenAIModelsResponse
    // ... decode response ...

    // 2. Build lookup from catalog
    catalog := GetOpenCodeZenCatalog()

    // 3. For each model ID from API, find matching catalog entry
    models := make([]*model.Model, 0, len(openAIResp.Data))
    for _, m := range openAIResp.Data {
        spec := LookupOpenCodeCatalog(catalog, m.ID)
        if spec == nil {
            // Model exists in API but not in our catalog — create minimal entry
            // (preserves forward compatibility when new models are added)
            capJSON, _ := json.Marshal(model.Capability{Streaming: true})
            models = append(models, &model.Model{
                ID:           uuid.New(),
                ProviderID:   provider.ID,
                ModelID:      m.ID,
                Name:         m.ID,
                DisplayName:  m.ID,
                Capabilities: string(capJSON),
                Params:       "{}",
                Modality:     "text",
                InputModalities:  "[]",
                OutputModalities: "[]",
                OwnedBy:      m.OwnedBy,
                Enabled:      true,
            })
            continue
        }
        models = append(models, OpenCodeCatalogToModel(spec, provider.ID))
    }

    return models, nil
}
```

**Key design choice**: Models returned by the API but not in our catalog get a minimal entry rather than being skipped. This means when OpenCode adds new models, they'll appear in discovery (just without pricing/capability details) rather than being invisible until we update the catalog.

### 4.2 New file: `internal/provider/discovery_opencode_go.go`

```go
package provider

// discoverOpenCodeGo uses the full hardcoded catalog since the Go API
// has no /models endpoint.
func (d *DiscoveryService) discoverOpenCodeGo(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
    catalog := GetOpenCodeGoCatalog()

    models := make([]*model.Model, 0, len(catalog))
    for i := range catalog {
        models = append(models, OpenCodeCatalogToModel(&catalog[i], provider.ID))
    }

    return models, nil
}
```

**Why not probe models like ZAI does?** ZAI tests each model with a minimal chat request because it needs to verify which models from a large catalog are actually available on the account. Go is a flat subscription — all models are available (within usage limits). Probing 12 models adds ~2 minutes to discovery for no benefit. If a model becomes unavailable, it will fail at request time and failover will handle it.

---

## Phase 5: Discovery Router Update

### 5.1 Modify: `internal/provider/discovery.go`

Add URL-based routing for both OpenCode providers. The key distinguishing factor is the URL path:

- `https://opencode.ai/zen/v1` → OpenCode Zen
- `https://opencode.ai/zen/go/v1` → OpenCode Go

```go
func (d *DiscoveryService) DiscoverModels(ctx context.Context, provider *Provider, masterKey string) ([]*model.Model, error) {
    apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
    if err != nil {
        return nil, fmt.Errorf("failed to decrypt API key: %w", err)
    }

    if strings.Contains(provider.BaseURL, "nano-gpt.com") {
        return d.discoverNanoGPT(ctx, provider, apiKey)
    }
    if strings.Contains(provider.BaseURL, "z.ai") {
        return d.discoverZAI(ctx, provider, apiKey)
    }
    if strings.Contains(provider.BaseURL, "deepseek.com") {
        return d.discoverDeepSeek(ctx, provider, apiKey)
    }
    if strings.Contains(provider.BaseURL, "ollama.com") {
        return d.discoverOllama(ctx, provider, apiKey)
    }

    // OpenCode providers — must check Go before Zen since Go URL contains /zen/go/
    if strings.Contains(provider.BaseURL, "opencode.ai/zen/go") {
        return d.discoverOpenCodeGo(ctx, provider, apiKey)
    }
    if strings.Contains(provider.BaseURL, "opencode.ai/zen") {
        return d.discoverOpenCodeZen(ctx, provider, apiKey)
    }

    return d.discoverOpenAI(ctx, provider, apiKey)
}
```

**Important**: The Go check (`opencode.ai/zen/go`) must come before the Zen check (`opencode.ai/zen`) because the Go URL is a substring of the Zen URL pattern. Without this ordering, `opencode.ai/zen/go/v1` would match the Zen check first.

---

## Phase 6: Config — Allowed Provider Hosts

### 6.1 Modify: `internal/config/config.go`

Add `opencode.ai` to `defaultKnownProviderHosts` so users don't need to manually add it to `ALLOWED_PROVIDER_HOSTS`.

```go
var defaultKnownProviderHosts = []string{
    "api.openai.com",
    "api.nano-gpt.com",
    "api.z.ai",
    "api.deepseek.com",
    "ollama.com",
    "opencode.ai",  // NEW — covers both /zen/v1 and /zen/go/v1
}
```

---

## Phase 7: Frontend — Provider Presets

### 7.1 Modify: `web/src/pages/Providers.tsx`

Add both providers to the type dropdown with pre-populated base URLs:

```typescript
const handleProviderTypeChange = (type: string) => {
    const baseUrls: Record<string, string> = {
        nanogpt: "https://nano-gpt.com/api/subscription/v1",
        "z-ai": "https://api.z.ai/api/paas/v4",
        openai: "https://api.openai.com/v1",
        deepseek: "https://api.deepseek.com/v1",
        ollama: "https://ollama.com/v1",
        "opencode-zen": "https://opencode.ai/zen/v1",        // NEW
        "opencode-go": "https://opencode.ai/zen/go/v1",      // NEW
    };
    // ...
};
```

And in the dropdown JSX:
```html
<option value="opencode-zen">OpenCode Zen</option>
<option value="opencode-go">OpenCode Go</option>
```

No special badge/usage UI needed — neither provider exposes a usage/balance API endpoint that we can query programmatically. If they add one in the future, we can extend the discovery API endpoints.

---

## Phase 8: Proxy Compatibility

**No changes needed to the proxy layer.** Both providers are fully OpenAI-compatible:

- The proxy routes requests to `{base_url}/chat/completions`
- Both `https://opencode.ai/zen/v1/chat/completions` and `https://opencode.ai/zen/go/v1/chat/completions` accept standard OpenAI-format requests
- The model ID in the request body is passed through as-is (e.g., `glm-5.1`, `gpt-5.4`)
- Streaming works the same way via SSE

The only edge case: Zen routes some models to `/messages` (Anthropic) or `/responses` (OpenAI native) rather than `/chat/completions`. However, from our testing, the `/chat/completions` endpoint appears to handle all models regardless (it's a gateway). If specific models fail through `/chat/completions`, we can add endpoint routing logic later — but this is unlikely to be needed.

---

## File Summary

| File | Action | Purpose |
|---|---|---|
| `internal/provider/opencode_catalog_types.go` | **CREATE** | Shared `OpenCodeModelSpec` type, `LookupOpenCodeCatalog()`, `OpenCodeCatalogToModel()` |
| `internal/provider/opencode_zen_catalog.go` | **CREATE** | Hardcoded specs for all ~41 Zen models |
| `internal/provider/opencode_go_catalog.go` | **CREATE** | Hardcoded specs for all 12 Go models |
| `internal/provider/discovery_opencode_zen.go` | **CREATE** | Zen discovery: fetch IDs from `/models` → enrich from catalog |
| `internal/provider/discovery_opencode_go.go` | **CREATE** | Go discovery: use full catalog (no `/models` endpoint) |
| `internal/provider/discovery.go` | **MODIFY** | Add URL routing for both OpenCode providers |
| `internal/config/config.go` | **MODIFY** | Add `opencode.ai` to `defaultKnownProviderHosts` |
| `web/src/pages/Providers.tsx` | **MODIFY** | Add dropdown presets for both providers |

---

## Implementation Order

1. **`opencode_catalog_types.go`** — shared types and helper functions (dependency for everything else)
2. **`opencode_zen_catalog.go`** — Zen model catalog
3. **`opencode_go_catalog.go`** — Go model catalog
4. **`discovery_opencode_zen.go`** — Zen discovery function
5. **`discovery_opencode_go.go`** — Go discovery function
6. **`discovery.go`** — routing update (3 lines)
7. **`config.go`** — allowed hosts update (1 line)
8. **`Providers.tsx`** — frontend dropdown presets
9. **Test** — create both providers in the dashboard, discover models, send test requests

---

## Open Questions / Future Considerations

1. **Context lengths are estimates**: OpenCode docs don't list context windows. We're using well-known values from the underlying model providers. When OpenCode exposes these via API, we can switch to dynamic discovery.

2. **Zen usage/balance API**: Zen has a console for tracking usage, but no documented programmatic API. If one is added, we can add a usage endpoint like NanoGPT/DeepSeek have.

3. **Go model availability probing**: Currently we trust the full catalog. If needed, we could add optional probing (like ZAI does) to detect which models are currently active on the subscription.

4. **Endpoint routing for non-chat models**: Some Zen models use `/messages` or `/responses` endpoints natively. Currently we route everything through `/chat/completions`. If specific models have issues, we can add endpoint-type metadata to the catalog and route accordingly in the proxy layer.

5. **Free model data privacy flags**: Some free models (Big Pickle, Nemotron) have data collection policies. We could add a `Params` field with privacy metadata if the frontend wants to show warnings.