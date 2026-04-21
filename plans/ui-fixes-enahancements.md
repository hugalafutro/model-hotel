Phase 1 — Quick Fixes (no new APIs, no new state)

**1.1 Remove `sk-` hint from provider modal API key field**
- File: `web/src/pages/Providers.tsx`
- Change the API key input `placeholder` from `"sk-..."` to `"API key"`

**1.2 Remove "Key decryption" row from overhead modal**
- File: `web/src/pages/Logs.tsx`
- Remove the "Key decryption" `<div>` row from `OverheadModal`
- Also remove `key_decrypt_ms` from the `OverheadBreakdown` interface

**1.3 Rename "Model lookup" to "Model/failover lookup" in overhead modal**
- File: `web/src/pages/Logs.tsx`
- Change label from "Model lookup" to "Model/failover lookup"

**1.4 Remove "Purge Logs" label, keep just the button**
- File: `web/src/pages/Settings.tsx`
- Find the Logging section card. Currently has a heading "Logging" with a sub-section "Purge Logs" label above the delete button. Remove the text label — the button itself says "Delete Logs" which is self-explanatory.

**1.5 Remove horizontal lines from Logging and Provider Discovery Status**
- File: `web/src/pages/Settings.tsx`
- Remove `border-t border-gray-700 pt-4` separator between log retention and purge
- Remove `border-b border-gray-700 last:border-0` from provider discovery list items

**1.6 Add descriptor to sidebar DB cache hit ratio**
- File: `web/src/components/Layout.tsx`
- Change `{stats.db.cache_hit_ratio}%` to `Hit {stats.db.cache_hit_ratio}%`

**1.7 Focus first field in modals on open**
- Files: `web/src/pages/Providers.tsx`, `web/src/pages/Models.tsx`, `web/src/pages/VirtualKeys.tsx`
- Add `autoFocus` to the first `<input>` in each create/edit modal
- For detail modals (Models, VirtualKeys), no `autoFocus` since they're view-only until editing

---

### Phase 2 — Data & Display Enhancements

**2.1 Provider pill — show discovered model count**
- Backend: `internal/api/admin.go` — Add `model_count` to `ProviderResponse` struct. In `ListProviders`, after fetching providers, query `SELECT provider_id, COUNT(*) FROM models GROUP BY provider_id` and join the counts.
- Frontend: `web/src/api/types.ts` — Add `model_count: number` to `Provider` interface
- Frontend: `web/src/pages/Providers.tsx` — Show model count badge on each provider card, e.g. `<span className="bg-indigo-500/20 text-indigo-300 text-xs px-2 py-0.5 rounded-full">{provider.model_count}</span>`

**2.2 Model list — provider filter pills**
- File: `web/src/pages/Models.tsx`
- Add a row of pills above the table showing each provider name with count. Clicking a pill sets `selectedProvider` filter (already exists). Style like capability filter pills but with provider colors.

**2.3 Provider type dropdown on create**
- Frontend: `web/src/pages/Providers.tsx`
- Add a `<select>` above the name field in the create modal with options: "Custom" (default), "NanoGPT", "Z.ai", "OpenAI Compatible"
- When selecting a preset, auto-fill the `base_url` field: NanoGPT → `https://api.nano-gpt.com/v1`, Z.ai → `https://api.z.ai/api/paas/v4`, OpenAI → `https://api.openai.com/v1`
- The name and API key fields still need manual input
- No backend changes needed

**2.4 Overhead modal — relabel and restructure**
- Already covered in 1.2 and 1.3 above
- Additionally: restructure rows to show:
  - Request parsing
  - Model/failover lookup  
  - Provider lookup
  - Total overhead

---

### Phase 3 — Edit Modes

**3.1 Provider modal — edit mode**
- Currently providers only support create and delete. Need to enable editing name, base_url, and enabled state.
- Backend: Already has `PUT /api/providers/{id}` endpoint. Need to add `api.providers.update()` to frontend client.
- Frontend: `web/src/pages/Providers.tsx`
  - Add `EditProviderModal` component (or extend existing modal with edit state)
  - Clicking a provider card opens the edit modal with pre-filled fields
  - Add an "Edit" button to each provider card
  - On save, call `api.providers.update(id, { name, base_url, api_key?, enabled })`
- Confirmation: If user has edited fields and clicks close/cancel, show "Discard changes to name, base_url?"

**3.2 Model details modal — inline edit mode**
- Currently models only support toggling enabled/disabled.
- Backend: `PATCH /api/models/{id}` currently only accepts `{ enabled }`. Need to extend to accept: `display_name`, `context_length`, `max_output_tokens`, `input_price_per_million`, `output_price_per_million`, `enabled`.
- Frontend: `web/src/pages/Models.tsx`
  - Add an "Edit" button to the modal header
  - Clicking it turns fields into editable inputs (inline editing)
  - Fields that were set by discovery show the discovered value as a "default" — the revert button resets to that default
  - On save, call `api.models.update(id, { changed fields })`
  - Confirmation if closing with unsaved changes: show which fields differ from current saved values

**3.3 Update confirmation with field diff**
- Create a reusable `ConfirmDialog` component
- When closing a modal with unsaved changes, list the fields that will be discarded: "Discard changes to: name, base_url?"
- Usage in both provider edit and model edit modals

---

### Phase 4 — New Features

**4.1 Theme accent colors**
- Create a new context/hook `useAccentColor` that manages the accent color in localStorage
- Define CSS custom properties for the accent color and derive hover/active/disabled/opacity variants
- Presets for dark theme (muted pastels): `#818cf8` (indigo), `#a78bfa` (violet), `#7dd3fc` (sky), `#86efac` (green), `#fbbf24` (amber), `#f472b6` (pink), `#fb923c` (orange)
- Presets for light theme (deeper but not 255): same hues at higher saturation
- Final swatch opens native `<input type="color">`
- Apply by setting CSS vars on `<html>` element: `--accent`, `--accent-hover`, `--accent-light`, `--accent-lighter`
- Replace all hardcoded `indigo-400/500/600` references with CSS custom properties
- Two-phase approach: 
  1. Create the accent context + preset picker UI
  2. Gradually replace Tailwind indigo classes with `var(--accent-*)` based classes

**4.2 Live log fetching (5s polling with toggle)**
- File: `web/src/pages/Logs.tsx`
- Add state: `liveEnabled` (boolean, default true)
- Set `refetchInterval` on the `useQuery` to `5000` when `liveEnabled` is true, `false` otherwise
- Add a toggle button in the Logs header: a small pill/icon button with "Live" label showing green dot when active, gray when paused
- Clicking toggles `liveEnabled`

---

### Implementation Order

1. **1.1** Remove `sk-` hint (1 min)
2. **1.2** Remove key decryption row (1 min)
3. **1.3** Relabel model lookup (1 min)
4. **1.4** Remove Purge Logs label (1 min)
5. **1.5** Remove horizontal lines (1 min)
6. **1.6** Add cache hit descriptor (1 min)
7. **1.7** AutoFocus modals (5 min)
8. **2.3** Provider type dropdown (10 min)
9. **2.4** Overhead modal restructure (already done in 1.2/1.3)
10. **4.2** Live log fetching (5 min) ← quick win, nice to have early
11. **2.1** Provider model count (15 min) ← needs backend + frontend
12. **2.2** Model provider filter pills (10 min)
13. **3.1** Provider edit modal (30 min)
14. **3.2** Model inline edit (45 min) ← most complex
15. **3.3** Unsaved changes confirmation (20 min)
16. **4.1** Theme accent colors (45 min) ← large scope
