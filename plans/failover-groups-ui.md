# Failover Groups UI — Implementation Plan

## 1. Problem Statement

The proxy already has backend failover logic (`internal/failover/failover.go`) and a `model_failover_groups` table, but it has two critical limitations:

1. **No UI** — There are zero admin API endpoints or frontend pages for managing failover groups.
2. **Only groups by exact `model_id` match** — `SyncForModel` groups models where 2+ providers share the exact same `model_id` string (e.g. `glm-5`). But in practice, NanoGPT returns `zai-org/glm-5` while Ollama returns `glm-5` — these are the same underlying model but different strings, so they're never grouped.

Additionally, the proxy's `ListModels` endpoint deduplicates by `model_id`, hiding the fact that multiple providers serve the same model. And there's no concept of a `hotel/` prefix that lets clients explicitly request failover-routed traffic.

## 2. Design Decisions

### 2.1 Naming Convention

| What | ID format | Example | Notes |
|------|-----------|---------|-------|
| Bare model (legacy) | `{model_id}` | `glm-5` | Backward compatible. If a failover group exists for this model_id, routes through it |
| Specific provider model | `{provider_name}/{model_id}` | `nanogpt/glm-5` | Targets one provider's model directly, no failover |
| Failover group | `hotel/{display_model}` | `hotel/glm-5` | Always uses failover group priority order |

**Rationale**: `hotel/` is short, memorable, and doesn't conflict with any real provider name prefix. Bare model IDs continue to work as before for backward compatibility.

### 2.2 Per-Entry Enable/Disable Storage

**Decision**: Store `enabled` state as a JSONB map `{model_uuid: bool}` in the failover group row, separate from the `priority_order` array.

**Rationale**: Priority order and enabled state are orthogonal concerns. A disabled entry stays in the priority list so the user can re-enable it without re-adding. The priority order determines sequence; the enabled map determines which entries are active.

### 2.3 Group Toggle

Each failover group has a `group_enabled` boolean (defaults to `true`). When disabled:
- The `hotel/{display_model}` endpoint is NOT exposed in `ListModels`
- Requests to `hotel/{display_model}` return 404
- The group configuration is preserved (not deleted)

### 2.4 Bare `model_id` Behavior (Unchanged)

When a request hits bare `glm-5`:
- If a failover group exists for `glm-5`, route through it (current behavior, unchanged)
- If no group exists, resolve to any single provider with that model_id (current fallback, unchanged)

### 2.5 New vs Auto-Discovered Groups

| Source | How Created | `display_model` | Can Edit? |
|--------|------------|-----------------|-----------|
| Auto-sync | `SyncForModel` runs on discovery | The shared `model_id` string (e.g. `glm-5`) | Yes — reorder, toggle entries, toggle group |
| Manual | User creates in UI | User-chosen alias (e.g. `glm-5` or `glm-5-unified`) | Yes — add/remove entries, reorder, toggle, etc. |

Auto-sync and manual groups share the same table. `SyncForModel` is updated to only auto-create groups, not auto-delete manually created ones. We add an `auto_created` boolean column to distinguish them.

### 2.6 Drag-and-Drop Library

**Decision**: Use `@dnd-kit/core` + `@dnd-kit/sortable`.

- Lightweight, accessible, well-maintained
- Excellent React/TypeScript support
- `react-beautiful-dnd` is deprecated
- No heavy UI framework dependency

## 3. Database Changes

### 3.1 Migration: `019_failover_groups_enhanced.sql`

```sql
-- Add group-level enable toggle
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS group_enabled BOOLEAN DEFAULT true;

-- Add per-entry enabled map: {model_uuid: true/false}
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS entry_enabled JSONB DEFAULT '{}';

-- Add display_name (human-readable, can differ from display_model)
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS display_name TEXT;

-- Add auto_created flag
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS auto_created BOOLEAN DEFAULT false;

-- Add description
ALTER TABLE model_failover_groups ADD COLUMN IF NOT EXISTS description TEXT DEFAULT '';
```

### 3.2 Updated Schema Shape

```
model_failover_groups
├── id                UUID PRIMARY KEY
├── display_model     TEXT NOT NULL UNIQUE   -- e.g. "glm-5", used as hotel/{display_model}
├── display_name      TEXT                    -- optional human-readable name, e.g. "GLM-5 Failover"
├── description       TEXT DEFAULT ''
├── priority_order    JSONB                   -- [uuid, uuid, ...] ordered list of model UUIDs
├── entry_enabled     JSONB DEFAULT '{}'      -- {uuid: true/false} per-entry toggle
├── group_enabled     BOOLEAN DEFAULT true     -- group-level toggle
├── auto_created      BOOLEAN DEFAULT false    -- was this auto-created by SyncForModel?
├── created_at        TIMESTAMPTZ DEFAULT now()
└── updated_at        TIMESTAMPTZ DEFAULT now()
```

## 4. Backend Changes

### 4.1 `internal/failover/failover.go` — Update Types & Repository

Update `FailoverGroup` struct:

```go
type FailoverGroup struct {
    ID            uuid.UUID          `json:"id"`
    DisplayModel  string             `json:"display_model"`
    DisplayName   *string            `json:"display_name"`
    Description   string             `json:"description"`
    PriorityOrder []uuid.UUID        `json:"priority_order"`
    EntryEnabled  map[string]bool    `json:"entry_enabled"`
    GroupEnabled  bool               `json:"group_enabled"`
    AutoCreated   bool               `json:"auto_created"`
    CreatedAt     time.Time          `json:"created_at"`
    UpdatedAt     time.Time          `json:"updated_at"`
}
```

Add new repository methods:

- `GetByDisplayModel(ctx, displayModel) (*FailoverGroup, error)` — already exists as `GetByModel`
- `ListAll(ctx) ([]*FailoverGroup, error)` — already exists as `List`
- `Create(ctx, displayModel, priorityOrder, entryEnabled, groupEnabled, displayName, description, autoCreated) (*FailoverGroup, error)`
- `Update(ctx, id, priorityOrder, entryEnabled, groupEnabled, displayName, description) (*FailoverGroup, error)`
- `DeleteByDisplayModel(ctx, displayModel) error` — already exists as `Delete`
- `GetEnabled(ctx) ([]*FailoverGroup, error)` — list only group_enabled=true groups, for proxy route

Update `SyncForModel` to:
- Set `auto_created = true`
- Set `entry_enabled` to all-true map for discovered entries
- Only auto-create, never auto-delete manual groups

### 4.2 `internal/api/failover.go` — New Admin API Endpoints

Register on `chi.Router` under `/api/failover-groups`.

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/failover-groups` | List all failover groups with resolved provider/model info |
| `GET` | `/api/failover-groups/{id}` | Get a single group with full detail |
| `POST` | `/api/failover-groups` | Create a new manual group |
| `PUT` | `/api/failover-groups/{id}` | Update group (reorder, toggle entries, toggle group, rename) |
| `DELETE` | `/api/failover-groups/{id}` | Delete a group |
| `POST` | `/api/failover-groups/sync` | Trigger re-sync of all auto groups |
| `GET` | `/api/failover-groups/candidates` | List available model entries that can be added to groups (all enabled models grouped by model_id, with provider info) |

**Response shape for list/detail**:

```json
{
  "id": "uuid",
  "display_model": "glm-5",
  "display_name": "GLM-5 Failover",
  "description": "Primary GLM-5 with fallbacks",
  "group_enabled": true,
  "auto_created": false,
  "entries": [
    {
      "model_uuid": "uuid",
      "model_id": "glm-5",
      "provider_id": "uuid",
      "provider_name": "ollama-cloud",
      "display_name": "GLM-5",
      "enabled": true,
      "context_length": 200000,
      "owned_by": "zhipu"
    },
    {
      "model_uuid": "uuid",
      "model_id": "zai-org/glm-5",
      "provider_id": "uuid",
      "provider_name": "nanogpt",
      "display_name": "GLM-5",
      "enabled": true,
      "context_length": 200000,
      "owned_by": "zhipu"
    }
  ],
  "created_at": "...",
  "updated_at": "..."
}
```

Note: entries can have **different** `model_id` strings. This is the key difference from the current system — a group can span `glm-5` from Ollama and `zai-org/glm-5` from NanoGPT.

### 4.3 `internal/proxy/handler.go` — Update Proxy Routes

#### `ListModels` changes:

1. Continue showing bare `model_id` entries as before (deduplicated)
2. For each **group_enabled** failover group, add a `hotel/{display_model}` entry:

```go
// After existing deduplicated models...
groups, _ := h.failoverRepo.GetEnabled(r.Context())
for _, g := range groups {
    // Resolve first enabled entry for metadata
    for _, modelUUID := range g.PriorityOrder {
        if g.EntryEnabled[modelUUID.String()] {
            m, _ := h.modelRepo.Get(r.Context(), modelUUID)
            if m != nil && m.Enabled && m.ProviderEnabled {
                hotelEntry := map[string]interface{}{
                    "id":       "hotel/" + g.DisplayModel,
                    "object":   "model",
                    "created":  m.CreatedAt.Unix(),
                    "owned_by": m.OwnedBy,
                    // ... copy metadata from first enabled entry
                }
                openAIModels = append(openAIModels, hotelEntry)
                break
            }
        }
    }
}
```

#### `ChatCompletions` changes:

When `req.Model` starts with `hotel/`:
1. Strip the `hotel/` prefix to get `displayModel`
2. Look up the failover group by `display_model`
3. Resolve candidates from the group's `priority_order`, skipping `entry_enabled=false` entries
4. Proceed with existing failover logic

When `req.Model` contains a `/` but doesn't start with `hotel/` (e.g. `nanogpt/glm-5`):
1. Split on first `/` to get `providerName` and `modelId`
2. Look up provider by name, then model by `provider_id + model_id`
3. Route directly to that provider (no failover)

When `req.Model` has no `/` (bare model ID, e.g. `glm-5`):
- Existing behavior: use failover group if exists, otherwise single provider

#### `resolveCandidates` changes:

Update to respect `entry_enabled` map from failover group:

```go
if fgErr == nil && len(fg.PriorityOrder) > 0 {
    candidates := make([]modelCandidate, 0, len(fg.PriorityOrder))
    for _, modelUUID := range fg.PriorityOrder {
        // Check entry_enabled (default to true if not in map)
        if enabled, ok := fg.EntryEnabled[modelUUID.String()]; ok && !enabled {
            continue
        }
        m, err := h.modelRepo.Get(ctx, modelUUID)
        // ... rest same as before
    }
    return candidates, modelLookupMs, nil
}
```

Also add a new `resolveSpecificProvider` method for `provider/model` routes:

```go
func (h *Handler) resolveSpecificProvider(ctx context.Context, providerName, modelID string) ([]modelCandidate, float64, error) {
    // Find provider by name, then get the specific model
    // Returns single-element candidate list
}
```

#### `shouldFailover` — no changes needed

Already handles 5XX and 429 correctly. We keep the current behavior: fail over on 5XX and optionally on 429 (controlled by `failover_on_rate_limit` setting).

**Streaming success commitment**: Once the first enabled provider starts streaming (status 200), we disengage failover and treat the request as successfully started. This is already the current behavior in `ChatCompletions`.

### 4.4 `internal/api/admin.go` — Register New Routes

Add to the `Register` method:

```go
failoverHandler := NewFailoverHandler(h.dbPool, h.cfg)
failoverHandler.Register(r)
```

### 4.5 `cmd/server/main.go` — Minor Update

The `runDiscovery` function already calls `failoverRepo.SyncForModel`. After our changes, `SyncForModel` will also set `auto_created=true` and populate `entry_enabled`. No structural changes needed, just ensure the updated `SyncForModel` is called.

## 5. Frontend Changes

### 5.1 New NPM Dependencies

```
@dnd-kit/core
@dnd-kit/sortable
@dnd-kit/utilities
```

### 5.2 New Types — `web/src/api/types.ts`

```typescript
export interface FailoverEntry {
  model_uuid: string;
  model_id: string;
  provider_id: string;
  provider_name: string;
  display_name: string;
  enabled: boolean;
  context_length: number | null;
  owned_by: string;
}

export interface FailoverGroup {
  id: string;
  display_model: string;
  display_name: string | null;
  description: string;
  group_enabled: boolean;
  auto_created: boolean;
  entries: FailoverEntry[];
  created_at: string;
  updated_at: string;
}

export interface CreateFailoverGroupRequest {
  display_model: string;
  display_name?: string;
  description?: string;
  entry_ids: string[];  // model UUIDs in priority order
}

export interface UpdateFailoverGroupRequest {
  display_name?: string;
  description?: string;
  group_enabled?: boolean;
  priority_order?: string[];      // model UUIDs in new priority order
  entry_enabled?: Record<string, boolean>;  // {uuid: bool}
}
```

### 5.3 API Client — `web/src/api/client.ts`

Add to the `api` object:

```typescript
failoverGroups: {
  list: async (): Promise<FailoverGroup[]> => { ... },
  get: async (id: string): Promise<FailoverGroup> => { ... },
  create: async (data: CreateFailoverGroupRequest): Promise<FailoverGroup> => { ... },
  update: async (id: string, data: UpdateFailoverGroupRequest): Promise<FailoverGroup> => { ... },
  delete: async (id: string): Promise<void> => ... },
  sync: async (): Promise<void> => { ... },
  candidates: async (): Promise<Model[]> => { ... },  // all enabled models for building groups
},
```

### 5.4 New Page — `web/src/pages/FailoverGroups.tsx`

#### Layout

```
┌──────────────────────────────────────────────────────────────────────┐
│  🏨 Failover Groups                              [+ New Group] [↻ Sync]│
├──────────────────────────────────────────────────────────────────────│
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │  hotel/glm-5                                    [🟢 ON] [⚙] │  │
│  │                                                                │  │
│  │  ┌─────────────────────────────────────────────────────────┐  │  │
│  │  │ ⠿  ollama-cloud / glm-5                       [✓ ON]   │  │  │
│  │  │ ⠿  nanogpt / zai-org/glm-5                   [✓ ON]   │  │  │
│  │  │ ⠿  zai / glm-5                               [✓ ON]   │  │  │
│  │  └─────────────────────────────────────────────────────────┘  │  │
│  │                                                                │  │
│  │  3/3 providers active • Try in order ↓       Auto-discovered │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
│  ┌────────────────────────────────────────────────────────────────┐  │
│  │  hotel/deepseek-r1                              [🔴 OFF]  │  │
│  │                                                                │  │
│  │  ┌─────────────────────────────────────────────────────────┐  │  │
│  │  │ ⠿  nanogpt / deepseek-r1                      [✓ ON]   │  │  │
│  │  │ ⠿  deepseek / deepseek-reasoner              [✓ ON]   │  │  │
│  │  └─────────────────────────────────────────────────────────┘  │  │
│  │                                                                │  │
│  │  2/2 providers active • Group disabled       Auto-discovered │  │
│  └────────────────────────────────────────────────────────────────┘  │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

#### Component Structure

```
FailoverGroups (page)
├── FailoverGroupCard (one per group)
│   ├── Group header: display_model, group toggle, settings gear
│   ├── Entry list (dnd-kit sortable)
│   │   └── FailoverEntryRow
│   │       ├── Drag handle (⠿)
│   │       ├── Provider name / model_id
│   │       └── Toggle switch (enabled/disabled)
│   └── Status bar: "X/Y providers active • Try in order ↓ • Auto/Manual"
├── CreateGroupModal
│   ├── Display model name input
│   ├── Optional display name
│   ├── Model entry picker (multi-select from candidates)
│   └── Priority drag ordering
└── GroupSettingsModal (gear icon)
    ├── Display name
    ├── Description
    ├── Delete group button
    └── Cancel/Save
```

#### Key Interactions

1. **Drag to reorder**: Use `@dnd-kit/sortable` on the entry list. On drop, call `PUT /api/failover-groups/{id}` with new `priority_order`.

2. **Toggle entry**: Click the toggle switch on an entry row. If this would be the last enabled entry, show a toast error "At least one provider must remain active." Call `PUT /api/failover-groups/{id}` with updated `entry_enabled`.

3. **Toggle group**: Click the green/red pill in the group header. Calls `PUT /api/failover-groups/{id}` with `group_enabled: false/true`. When disabled, the card gets dimmed (opacity-50, grayscale).

4. **Create group**: Opens modal. User picks a `display_model` name (e.g. `glm-5`), optional display name, and selects model entries from a searchable dropdown. Entries can have different `model_id` strings (this is the key feature).

5. **Sync**: Button in header triggers `POST /api/failover-groups/sync`. Re-runs `SyncAllModels` to discover new groups from models that now share the same `model_id`. Toast on success.

6. **Delete group**: Available in settings modal. Confirmation dialog.

#### Visual Design Specs

- **Active group card**: `bg-gray-800 border-indigo-500/30` with left accent border
- **Disabled group card**: `bg-gray-800/50 border-gray-700 opacity-60` 
- **Entry row**: `bg-gray-750 hover:bg-gray-700 rounded-lg px-3 py-2`
- **Drag handle**: `text-gray-500 cursor-grab active:cursor-grabbing` — only visible on hover of the row
- **Toggle**: Use the same toggle component pattern as the provider enabled/disabled toggle already in the codebase
- **Group toggle**: Pill-shaped, `bg-indigo-500` when on, `bg-gray-600` when off
- **Status summary**: `text-xs text-gray-500 mt-2`

### 5.5 Navigation Update — `web/src/components/Layout.tsx`

Add to navigation array:

```typescript
{ name: 'Failover', href: '/failover', icon: '🏨' }
```

### 5.6 Route Update — `web/src/App.tsx`

Add route:

```typescript
<Route path="/failover" element={<FailoverGroups />} />
```

### 5.7 Models Page Integration — `web/src/pages/Models.tsx`

In the model detail modal, add a small section showing failover group membership:

```
Failover Group: hotel/glm-5 (2nd of 3 providers) [View Group →]
```

This requires a lightweight lookup endpoint: `GET /api/failover-groups/by-model/{model_uuid}` — returns the group containing that model UUID, if any.

## 6. Implementation Order

### Phase 1: Database & Backend Core
1. Create migration `019_failover_groups_enhanced.sql`
2. Update `internal/failover/failover.go` — new struct fields, repository methods, updated `SyncForModel`
3. Create `internal/api/failover.go` — all admin API endpoints
4. Wire up in `internal/api/admin.go`

### Phase 2: Proxy Route Updates
5. Update `internal/proxy/handler.go` — `ListModels` to expose `hotel/` entries
6. Update `internal/proxy/handler.go` — `ChatCompletions` to handle `hotel/` and `provider/model` prefixes
7. Update `internal/proxy/handler.go` — `resolveCandidates` to respect `entry_enabled`
8. Add `resolveSpecificProvider` method

### Phase 3: Frontend Foundation
9. Install `@dnd-kit/core`, `@dnd-kit/sortable`, `@dnd-kit/utilities`
10. Add types to `web/src/api/types.ts`
11. Add API methods to `web/src/api/client.ts`
12. Add navigation + routes

### Phase 4: Frontend UI
13. Build `FailoverGroupCard` component
14. Build `FailoverEntryRow` component (with drag handle)
15. Build `CreateGroupModal` component
16. Build `GroupSettingsModal` component
17. Build `FailoverGroups` page
18. Add failover group link in Models detail modal

### Phase 5: Polish
19. Add `GET /api/failover-groups/by-model/{model_uuid}` for model detail integration
20. Handle edge cases (group with 0 enabled entries, deleted provider in group, etc.)
21. Add keyboard accessibility for drag reorder
22. Test end-to-end: create group, reorder, toggle, verify proxy routes correctly

## 7. Edge Cases & Error Handling

| Scenario | Behavior |
|----------|----------|
| All entries in a group are disabled | Prevent in UI. Backend: if no enabled entries, return 502 "no available provider for hotel/{model}" |
| Group is disabled, user requests `hotel/{model}` | Return 404 with clear message |
| A provider/model in a group is deleted | Auto-prune on next `SyncAllModels`. UI shows warning badge until resolved |
| A provider/model in a group is disabled (model-level) | Skip that entry during resolution, same as entry_enabled=false |
| User requests `provider/model` where provider doesn't exist | Return 404 "provider not found" |
| User requests `provider/model` where model doesn't exist | Return 404 "model not found" |
| `hotel/` prefix on a model with no failover group | Return 404 "no failover group for hotel/{model}" |
| Race condition: model disabled while request is in-flight | Failover to next candidate (existing behavior) |
| Auto-sync creates a group that user then manually edited | Manual edits are preserved. Auto-sync only creates new groups, never overwrites manual ones |

## 8. File Change Summary

### New Files
- `internal/db/migrations/019_failover_groups_enhanced.sql`
- `internal/api/failover.go`
- `web/src/pages/FailoverGroups.tsx`

### Modified Files
- `internal/failover/failover.go` — struct updates, new repository methods, updated SyncForModel
- `internal/api/admin.go` — register failover routes
- `internal/proxy/handler.go` — ListModels, ChatCompletions, resolveCandidates, resolveSpecificProvider
- `internal/proxy/types.go` — add route type constants
- `cmd/server/main.go` — wire up failover handler (if needed)
- `web/src/api/types.ts` — FailoverGroup, FailoverEntry types
- `web/src/api/client.ts` — failoverGroups API methods
- `web/src/App.tsx` — add route
- `web/src/components/Layout.tsx` — add nav item
- `web/src/pages/Models.tsx` — add group membership link in detail modal
- `web/package.json` — add @dnd-kit dependencies

### Total: ~12 modified files, ~3 new files
