# Ollama Cloud Provider Integration Plan

## API Investigation Summary

### Ollama Cloud Endpoints Probed

| Endpoint | Method | Result | Notes |
|---|---|---|---|
| `/v1/models` | GET | ✅ Works | OpenAI-compatible list format. Returns `{object, data: [{id, object, created, owned_by}]}`. **No context_length, capabilities, or pricing.** |
| `/api/tags` | GET | ✅ Works | Native Ollama format. Returns `{models: [{name, model, modified_at, size, digest, details: {parent_model, format, family, families, parameter_size, quantization_level}}]}`. **`details` fields are mostly empty strings for cloud models** — no useful enrichment here. |
| `/api/show` | POST | ✅ Works | **Rich model details**. Takes `{"model": "<name>"}`. Returns `capabilities` array (`"completion"`, `"tools"`, `"thinking"`, `"vision"`) + `model_info` with `<arch>.context_length`, `<arch>.embedding_length`, `general.parameter_count`, etc. |
| `/v1/chat/completions` | POST | ✅ Works | OpenAI-compatible chat completions. Streaming with `stream_options.include_usage` supported. Usage object has `prompt_tokens`, `completion_tokens`, `total_tokens`. Streaming returns `reasoning` field for thinking models. |
| `/api/usage`, `/api/balance`, `/api/account`, `/api/quota` | GET | ❌ Not found | No usage/quota/balance endpoints exist. Ollama Cloud has no account-level API for balance or usage tracking. |

### Key Findings

1. **Discovery Strategy**: Call `/api/tags` to get model list, then call `/api/show` for each model to get `capabilities`, `context_length`, and `parameter_count`. This gives us everything we need from the API — **no hardcoded catalog required** for core model specs.

2. **No balance/quota API**: Unlike NanoGPT (usage endpoint) and DeepSeek (balance endpoint), Ollama Cloud exposes **no** account usage or billing endpoints. No badge UI needed.

3. **OpenAI-compatible proxy**: The `/v1/chat/completions` endpoint works as a drop-in. Our existing proxy handler already supports OpenAI-compatible providers. The base URL for Ollama Cloud is `https://ollama.com` (not `/v1` suffix — the proxy appends `/chat/completions`).

4. **`/api/tags` vs `/v1/models`**: Both work. `/v1/models` is simpler (OpenAI format) but gives only `id`, `created`, `owned_by`. `/api/tags` gives names that match `/api/show` model names plus `size` and `details` (mostly empty for cloud). We'll use `/api/tags` for the model list (to get the names that `/api/show` expects), then enrich each with `/api/show`.

5. **Capabilities mapping from `/api/show`**:
   - `"completion"` → always present, maps to our `streaming: true`
   - `"tools"` → maps to `tool_calling: true`
   - `"thinking"` → maps to `reasoning: true`
   - `"vision"` → maps to `vision: true`

6. **`model_info` context length**: The key pattern is `<architecture>.context_length` (e.g., `glm5.1.context_length: 202752`, `deepseek3.2.context_length: 163840`). We extract this dynamically.

7. **`/api/tags` has 37 cloud models** currently available, including GLM, DeepSeek, GPT-OSS, Qwen, Gemma, Mistral, Kimi, MiniMax, Nemotron, etc.

8. **Concurrency needed**: Calling `/api/show` for 37 models sequentially would be slow. We should use concurrent requests with a semaphore (similar to the ZAI test pattern, but for enrichment, not testing — these should be fast GETs).

---

## Phase 1: Backend — Ollama Discovery Types

### File: `internal/provider/discovery_types.go` — ADD types

Add Ollama-specific API response types after existing `DeepSeekBalanceResponse`:

```go
type OllamaTagsModel struct {
    Name       string `json:"name"`
    Model      string `json:"model"`
    ModifiedAt string `json:"modified_at"`
    Size       int64  `json:"size"`
    Digest     string `json:"digest"`
    Details    struct {
        ParentModel      string   `json:"parent_model"`
        Format           string   `json:"format"`
        Family           string   `json:"family"`
        Families         []string `json:"families"`
        ParameterSize    string   `json:"parameter_size"`
        QuantizationLevel string  `json:"quantization_level"`
    } `json:"details"`
}

type OllamaTagsResponse struct {
    Models []OllamaTagsModel `json:"models"`
}

type OllamaShowResponse struct {
    Details struct {
        ParentModel       string   `json:"parent_model"`
        Format            string   `json:"format"`
        Family            string   `json:"family"`
        Families          []string `json:"families"`
        ParameterSize     string   `json:"parameter_size"`
        QuantizationLevel string   `json:"quantization_level"`
    } `json:"details"`
    ModelInfo    map[string]interface{} `json:"model_info"`
    Capabilities []string               `json:"capabilities"`
    ModifiedAt   string                 `json:"modified_at"`
}
```

**Design principle**: We use `map[string]interface{}` for `model_info` because the keys are dynamic (`<arch>.context_length`, `<arch>.embedding_length`, `general.architecture`, `general.parameter_count`). We extract what we need and ignore the rest — no hardcoding of architecture-specific prefixes.

---

## Phase 2: Backend — Discovery Dispatcher

### File: `internal/provider/discovery.go` — ADD Ollama branch

In `DiscoverModels()`, before the default `discoverOpenAI`, add:

```go
if strings.Contains(provider.BaseURL, "ollama.com") {
    return d.discoverOllama(ctx, provider, apiKey)
}
```

---

## Phase 3: Backend — Ollama Discovery Implementation

### File: `internal/provider/discovery.go` — ADD `discoverOllama()` method

This is the core of the integration. The strategy:

1. **GET `/api/tags`** — get list of all available cloud models (names + sizes)
2. **Concurrently POST `/api/show`** for each model to retrieve capabilities and context_length (semaphore-limited, e.g., 5 concurrent)
3. **Map capabilities** from the `capabilities` array to our `model.Capability` struct
4. **Extract context length** from `model_info.<arch>.context_length` by scanning for any key ending in `.context_length`
5. **Set modality** based on `"vision"` capability presence
6. **Set owned_by** from `details.family` or `model_info["general.architecture"]`

```go
func (d *DiscoveryService) discoverOllama(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
    baseURL := util.SanitizeBaseURL(provider.BaseURL)

    // Step 1: Get model list from /api/tags
    tagsURL := strings.TrimSuffix(baseURL, "/") + "/api/tags"
    req, err := http.NewRequestWithContext(ctx, "GET", tagsURL, nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }
    req.Header.Set("Authorization", "Bearer "+apiKey)

    resp, err := d.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("failed to fetch models: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
    }

    var tagsResp OllamaTagsResponse
    if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
        return nil, fmt.Errorf("failed to decode response: %w", err)
    }

    // Step 2: Concurrently fetch /api/show details for each model
    type showResult struct {
        index     int
        modelID   string
        showResp  *OllamaShowResponse
        err       error
    }

    results := make([]showResult, len(tagsResp.Models))
    sem := make(chan struct{}, 5)
    var wg sync.WaitGroup

    showCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
    defer cancel()

    for i, m := range tagsResp.Models {
        wg.Add(1)
        go func(idx int, modelName string) {
            defer wg.Done()
            sem <- struct{}{}
            defer func() { <-sem }()

            show, err := d.ollamaShowModel(showCtx, baseURL, apiKey, modelName)
            results[idx] = showResult{index: idx, modelID: modelName, showResp: show, err: err}
        }(i, m.Name)
    }
    wg.Wait()

    // Step 3: Build models from tags + show details
    models := make([]*model.Model, 0, len(tagsResp.Models))
    for _, r := range results {
        if r.err != nil {
            // Log warning but don't fail entirely — model might be temporarily unavailable
            continue
        }

        m := buildOllamaModel(provider, r.modelID, r.showResp)
        models = append(models, m)
    }

    return models, nil
}
```

### Helper: `ollamaShowModel()`

```go
func (d *DiscoveryService) ollamaShowModel(ctx context.Context, baseURL, apiKey, modelName string) (*OllamaShowResponse, error) {
    showURL := strings.TrimSuffix(baseURL, "/") + "/api/show"
    body := fmt.Sprintf(`{"model":"%s"}`, modelName)
    req, err := http.NewRequestWithContext(ctx, "POST", showURL, strings.NewReader(body))
    if err != nil {
        return nil, err
    }
    req.Header.Set("Authorization", "Bearer "+apiKey)
    req.Header.Set("Content-Type", "application/json")

    resp, err := d.httpClient.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    if resp.StatusCode != http.StatusOK {
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("show failed for %s: status %d: %s", modelName, resp.StatusCode, string(body))
    }

    var showResp OllamaShowResponse
    if err := json.NewDecoder(resp.Body).Decode(&showResp); err != nil {
        return nil, err
    }
    return &showResp, nil
}
```

### Helper: `buildOllamaModel()`

```go
func buildOllamaModel(provider *Provider, modelID string, show *OllamaShowResponse) *model.Model {
    caps := model.Capability{Streaming: true}
    var modality string = "text"
    inputMods := `["text"]`

    for _, c := range show.Capabilities {
        switch c {
        case "tools":
            caps.ToolCalling = true
        case "thinking":
            caps.Reasoning = true
        case "vision":
            caps.Vision = true
            modality = "vision"
            inputMods = `["text","image"]`
        }
    }
    capJSON, _ := json.Marshal(caps)

    // Extract context_length from model_info dynamically
    var contextLength *int
    for k, v := range show.ModelInfo {
        if strings.HasSuffix(k, ".context_length") {
            if f, ok := v.(float64); ok {
                cl := int(f)
                contextLength = &cl
                break
            }
        }
    }

    ownedBy := show.Details.Family
    if ownedBy == "" {
        ownedBy = "ollama"
    }

    return &model.Model{
        ID:               uuid.New(),
        ProviderID:       provider.ID,
        ModelID:          modelID,
        Name:             modelID,
        DisplayName:      modelID,
        Capabilities:     string(capJSON),
        Params:           "{}",
        Modality:         modality,
        InputModalities:  inputMods,
        OutputModalities: "[]",
        ContextLength:    contextLength,
        OwnedBy:          ownedBy,
        Enabled:          true,
    }
}
```

**Why no hardcoded catalog?** Unlike DeepSeek (where pricing is not in the API and needs hardcoding) or Z.ai (where capabilities aren't available), Ollama's `/api/show` endpoint provides everything we need:
- `capabilities` → tool_calling, reasoning, vision
- `model_info.<arch>.context_length` → context window
- `details.family` → owned_by / display name
- `model_info.general.parameter_count` → could store in params if desired

**Pricing note**: Ollama Cloud doesn't expose pricing via API. Since the app already has `input_price_per_million` and `output_price_per_million` fields on models that can be edited manually, operators can set pricing after discovery. The plan stores `null` for pricing (as NanoGPT does when it doesn't return pricing). No catalog needed for this.

---

## Phase 4: Frontend — Provider Type Dropdown

### File: `web/src/pages/Providers.tsx`

1. **Add `ollama` option** to the provider type `<select>`:

```tsx
<option value="ollama">Ollama Cloud</option>
```

2. **Add base URL mapping** in `handleProviderTypeChange`:

```tsx
ollama: 'https://ollama.com',
```

Note: `https://ollama.com` not `https://ollama.com/v1` because our proxy constructs URLs as `<base_url>/v1/chat/completions`. The Ollama Cloud OpenAI-compatible endpoint is at `https://ollama.com/v1/chat/completions`. However, we also need discovery to hit `https://ollama.com/api/tags` and `https://ollama.com/api/show`. The `util.SanitizeBaseURL` strips trailing slashes; our discovery code will construct paths accordingly.

**Wait — proxy routing check**: Our proxy does `SanitizeBaseURL(baseURL) + "/chat/completions"`. For Ollama Cloud, this would produce `https://ollama.com/chat/completions` which is **wrong** — the OpenAI-compatible endpoint is at `/v1/chat/completions`. We need the stored base_url to be `https://ollama.com/v1` OR we need to handle this in the proxy/discovery layer.

**Decision**: Store base URL as `https://ollama.com/v1` (following the same pattern as `https://api.openai.com/v1`, `https://api.deepseek.com/v1`). Then:
- Proxy: `SanitizeBaseURL("https://ollama.com/v1") + "/chat/completions"` = `https://ollama.com/v1/chat/completions` ✅
- Discovery `/api/tags`: Needs to hit `https://ollama.com/api/tags` — we derive this from the base URL by replacing `/v1` with empty and appending `/api/tags`. Alternatively, we just derive the host and build the native API URL.

**Actually**: Looking at how `SanitizeBaseURL` works and how `discoverNanoGPT` uses `baseURL + "/models?detailed=true"`, the pattern is that discovery code already constructs provider-specific URL paths from the base URL. For Ollama, discovery just needs to strip `/v1` from the base URL before constructing native API paths. This is simple string manipulation.

---

## Phase 5: Proxy Compatibility — No Changes Needed

The proxy handler at `internal/proxy/handler.go` routes all providers through the same OpenAI-compatible `/v1/chat/completions` endpoint. Since Ollama Cloud's `/v1/chat/completions` is fully OpenAI-compatible (confirmed via testing), no proxy changes are needed.

The streaming response format matches OpenAI's SSE format. The usage reporting (`prompt_tokens`, `completion_tokens`, `total_tokens`) works with our existing `Usage` struct. The `reasoning` field in streaming chunks is extra and safely ignored by our parser.

---

## Phase 6: No Balance/Quota Endpoint

Ollama Cloud has no account-level API for balance or usage. Confirmed by probing `/api/usage`, `/api/balance`, `/api/account`, `/api/quota` — all return 404.

**No frontend badge needed**. Unlike DeepSeek (balance modal) or NanoGPT (quota modal), Ollama gets no account-level UI.

---

## Summary of Changes

| File | Action | What |
|---|---|---|
| `internal/provider/discovery.go` | MODIFY | Add `ollama.com` branch in `DiscoverModels()` + `discoverOllama()` + `ollamaShowModel()` + `buildOllamaModel()` |
| `internal/provider/discovery_types.go` | MODIFY | Add `OllamaTagsModel`, `OllamaTagsResponse`, `OllamaShowResponse` types |
| `web/src/pages/Providers.tsx` | MODIFY | Add "Ollama Cloud" dropdown option + base URL `https://ollama.com/v1` |

**Files NOT touched** (important to note):
- No new catalog file (Ollama's `/api/show` gives us everything)
- No database migration (no new columns needed)
- No proxy handler changes (OpenAI-compatible)
- No balance/usage API changes
- No API client changes (no new endpoints)
- No types changes (no new response shapes for frontend)

---

## Implementation Order

1. Add Ollama types to `internal/provider/discovery_types.go`
2. Add `discoverOllama()`, `ollamaShowModel()`, and `buildOllamaModel()` to `internal/provider/discovery.go`
3. Add Ollama URL match in `DiscoverModels()` dispatcher
4. Add "Ollama Cloud" to frontend provider dropdown + base URL
5. Test with the provisioned API key: `49071b69aefc4bbe9d78a3419b6e3d23.os5ggixiHouArVW8RzqUkFWm`
6. Verify discovery, model listing, and chat completions end-to-end

---

## Testing Plan

1. Create an Ollama Cloud provider via the UI with base URL `https://ollama.com/v1` and the API key
2. Trigger model discovery — should discover all 37 models with capabilities and context lengths
3. Verify model capabilities: vision models get `vision:true`, thinking models get `reasoning:true`, models with `"tools"` capability get `tool_calling:true`
4. Send a test chat completion through the proxy to verify routing works
5. Test streaming and non-streaming responses
6. Verify that models not found in `/api/show` (if any fail) are gracefully skipped without breaking the whole discovery