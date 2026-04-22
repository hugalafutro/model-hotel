# DeepSeek Provider Integration Plan

## Overview
Add DeepSeek as a new provider with full support for:
- Model discovery via `/models` + hardcoded catalog for specs not exposed by API
- Cache-aware cost tracking (`prompt_cache_hit_tokens` vs `prompt_cache_miss_tokens`)
- Balance display badge with USD/CNY toggle (no modal)

---

## Answers to Clarifying Questions
1. **Exchange rates**: Show whatever the API returns
2. **Logs cost display**: Initially hidden, with a toggle/expand
3. **Testing**: Create a test provider using the provided API key `sk-543abda8df844728bd680e4d2a768ef6`

---

## Phase 1: Database Migration (Backward Compatible)

**New migration file: `internal/db/migrations/004_deepseek_pricing.sql`**

```sql
-- Add cache hit/miss tracking columns to request_logs
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS tokens_prompt_cache_hit INT DEFAULT 0;
ALTER TABLE request_logs ADD COLUMN IF NOT EXISTS tokens_prompt_cache_miss INT DEFAULT 0;

-- Add pricing columns to models (for per-model price overrides)
ALTER TABLE models ADD COLUMN IF NOT EXISTS input_price_per_million_cache_hit REAL;
ALTER TABLE models ADD COLUMN IF NOT EXISTS input_price_per_million_cache_miss REAL;
```

**Key**: Default values of `0` ensure old logs still work - queries using `COALESCE(col, 0)` will handle missing data gracefully.

---

## Phase 2: Backend - Core Types & Catalog

### 2.1 New file: `internal/provider/deepseek_catalog.go`

```go
type DeepSeekModelSpec struct {
    ModelID                        string
    ContextLength                  int      // 128000 for DeepSeek-V3.2
    MaxOutputTokens                int      // 8192 default, 16384 for reasoner
    Reasoning                      bool
    InputPricePerMillionCacheHit   float64  // 0.028 USD
    InputPricePerMillionCacheMiss float64  // 0.28 USD
    OutputPricePerMillion          float64  // 0.42 USD
}

var deepseekCatalog = []DeepSeekModelSpec{
    {
        ModelID: "deepseek-chat",
        ContextLength: 128000,
        MaxOutputTokens: 8192,  // default 4K, max 8K
        Reasoning: false,
        ToolCalling: true,
        InputPricePerMillionCacheHit: 0.028,
        InputPricePerMillionCacheMiss: 0.28,
        OutputPricePerMillion: 0.42,
    },
    {
        ModelID: "deepseek-reasoner",
        ContextLength: 128000,
        MaxOutputTokens: 32768, // default 32K, max 64K
        Reasoning: true,
        ToolCalling: true,
        InputPricePerMillionCacheHit: 0.028,
        InputPricePerMillionCacheMiss: 0.28,
        OutputPricePerMillion: 0.42,
    },
}
```

---

## Phase 3: Backend - Model Discovery

### 3.1 `internal/provider/discovery.go` - Modify `DiscoverModels()`

Add check for `deepseek.com` in `DiscoverModels()` function.

### 3.2 New function: `discoverDeepSeek()`

1. Call `GET /models` to get model IDs
2. Match against hardcoded catalog for full specs
3. Return models with full specs from catalog

---

## Phase 4: Backend - Usage & Cost Tracking

### 4.1 Extend `Usage` struct in `internal/proxy/handler.go`

```go
type Usage struct {
    PromptTokens            int `json:"prompt_tokens"`
    CompletionTokens       int `json:"completion_tokens"`
    TotalTokens            int `json:"total_tokens"`
    PromptCacheHitTokens    int `json:"prompt_cache_hit_tokens,omitempty"`    // NEW
    PromptCacheMissTokens   int `json:"prompt_cache_miss_tokens,omitempty"`  // NEW
}
```

### 4.2 Parse cache tokens from DeepSeek response

Extract from response in `handleStreamingResponse()` and `handleNonStreamingResponse()`.

### 4.3 Extend `requestLogData`

```go
type requestLogData struct {
    // ... existing fields ...
    TokensPromptCacheHit   int
    TokensPromptCacheMiss  int
}
```

### 4.4 Update `insertRequestLog()` and `updateRequestLog()`

Include new columns in SQL.

---

## Phase 5: Backend - Balance API

### 5.1 New file: `internal/provider/deepseek_balance.go`

```go
type DeepSeekBalanceResponse struct {
    IsAvailable   bool `json:"is_available"`
    BalanceInfos  []DeepSeekBalanceInfo `json:"balance_infos"`
}

type DeepSeekBalanceInfo struct {
    Currency         string `json:"currency"`  // "CNY" or "USD"
    TotalBalance     string `json:"total_balance"`
    GrantedBalance   string `json:"granted_balance"`
    ToppedUpBalance  string `json:"topped_up_balance"`
}
```

### 5.2 Add `GetDeepSeekBalance()` function

### 5.3 New route in `internal/api/discovery.go`

```go
r.Route("/providers/{id}/balance", func(r chi.Router) {
    r.Get("/", h.GetProviderBalance)
})
```

---

## Phase 6: Frontend - Types & API Client

### 6.1 `web/src/api/types.ts` - Add new types

```typescript
export interface DeepSeekBalanceInfo {
  currency: 'CNY' | 'USD'
  total_balance: string
  granted_balance: string
  topped_up_balance: string
}

export interface DeepSeekBalance {
  is_available: boolean
  balance_infos: DeepSeekBalanceInfo[]
}
```

### 6.2 `web/src/api/client.ts` - Add balance fetch

```typescript
getBalance: async (id: string): Promise<DeepSeekBalance> => {
  const response = await fetch(`${API_BASE}/api/providers/${id}/balance`)
  return response.json()
}
```

---

## Phase 7: Frontend - Provider Page

### 7.1 `Providers.tsx` - Add DeepSeek to dropdown & base URLs

```typescript
const baseUrls: Record<string, string> = {
  nanogpt: 'https://api.nano-gpt.com/v1',
  'z-ai': 'https://api.z.ai/api/paas/v4',
  openai: 'https://api.openai.com/v1',
  deepseek: 'https://api.deepseek.com/v1',  // NEW
}

// In dropdown:
<option value="deepseek">DeepSeek</option>
```

### 7.2 Add DeepSeek balance badge

- No modal
- Click cycles USD → CNY → USD
- Shows formatted balance in current currency

---

## Phase 8: Frontend - Logs Page Enhancement

### 8.1 Display cost in log entries

Hidden by default, visible via toggle/expand:
- `Prompt Cost ($)` = `(tokens_prompt_cache_hit * cache_hit_price) + (tokens_prompt_cache_miss * cache_miss_price)`
- `Output Cost ($)` = `tokens_completion * output_price`
- `Total Cost ($)` = `Prompt Cost + Output Cost`

### 8.2 Model detail modal enhancement

Show prices and calculated cost for log entry.

---

## Summary of Files to Modify/Create

| File | Action | Purpose |
|------|--------|---------|
| `internal/db/migrations/004_*.sql` | **CREATE** | New columns for cache tracking |
| `internal/provider/deepseek_catalog.go` | **CREATE** | Hardcoded model specs |
| `internal/provider/discovery.go` | MODIFY | Add DeepSeek detection & discovery |
| `internal/proxy/handler.go` | MODIFY | Parse cache tokens, log them |
| `internal/provider/deepseek_balance.go` | **CREATE** | Balance fetching |
| `internal/api/discovery.go` | MODIFY | Add balance endpoint |
| `web/src/api/types.ts` | MODIFY | Add DeepSeek types |
| `web/src/api/client.ts` | MODIFY | Add getBalance() |
| `web/src/pages/Providers.tsx` | MODIFY | Dropdown, badge, currency toggle |
| `web/src/pages/Logs.tsx` | MODIFY | Display costs (hidden by default) |

---

## Implementation Order

1. DB Migration (`004_deepseek_pricing.sql`)
2. Backend - DeepSeek catalog (`deepseek_catalog.go`)
3. Backend - Discovery with DeepSeek support
4. Backend - Extend Usage struct and logging
5. Backend - Balance API
6. Frontend - Types and API client
7. Frontend - Provider page changes
8. Frontend - Logs page cost display
9. Test provider creation and end-to-end verification
