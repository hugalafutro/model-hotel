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
- [The Model Discrepancy Modal](#the-model-discrepancy-modal)
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
| `/api/providers/{id}/discover` | POST | Single provider | Discover and import models for one provider |
| `/api/providers/discover-all` | POST | All providers | Discover and import models for every enabled provider; skips disabled providers |

Both endpoints upsert discovered models and re-sync failover groups for what they saw. They do **not** disable missing models: disabling requires two consecutive confirmed-missing scans, so a single on-demand click can never reach that threshold, and running the in-scan confirmation probes (up to ~70s of backoff) on a request would overrun the route's 60s HTTP timeout - which also made the HA config-sync import look like it failed. Miss-recording and disabling are therefore owned exclusively by the scheduled/startup background sweep - see [Missing models: three layers of proof before a disable](#missing-models-three-layers-of-proof-before-a-disable). When the background sweep does disable a model, its failover group is re-synced in the same scan so the model is pruned instead of lingering as a stale entry.

Both endpoints also return a `diff` describing what the scan changed - models added or re-enabled (with machine-readable reason codes `new_model`, `reappeared`) plus any failover groups updated as a result. (The background sweep's diff can also carry `disabled` entries with reason `not_listed`; manual scans never disable, so theirs will not.) It also reports `updated` models whose pricing or context-length metadata moved since the previous scan: each entry carries per-field `changes` (codes `input_price`, `output_price`, `input_price_cache`, `context_length`, each with `old`/`new` numbers). The diff reports what actually persisted: price moves are reported from any source (live API, catalog, models.dev - prices follow their source on unpinned rows) but suppressed on operator-pinned rows, where the upsert keeps the stored values; context-length moves are reported only when the provider's own live API supplied the value (tracked via transient per-field live provenance), since a non-live context value is fill-only. OpenRouter's sub-tolerance price jitter is damped to the stored value before the upsert, so it neither persists nor reports. The dashboard renders this diff as a post-scan summary modal after manual Discover / Discover All runs; an all-empty diff still confirms "scanned, nothing changed".

Scheduled/startup background discovery does not pop the modal (SSE events cover it). Instead, any changes it records are persisted to a `discovery_changes` store (migration `047`) and surfaced as a badge on the **Models** sidebar item; clicking the badge opens the [Model Discrepancy Modal](#the-model-discrepancy-modal). A `discovery.changes_pending` SSE event fires when a background scan records changes. See the [API Reference](https://github.com/hugalafutro/model-hotel/wiki/API-Reference) for the `GET /api/discovery/changes` and `POST /api/discovery/changes/ack` endpoints.

Opening the modal does **not** clear the badge. Nothing is acked until the operator actually expands the Recent changes section, because a badge that clears on a glance is a badge that hides things.

```json
{
  "discovered": 14,
  "models": [...],
  "diff": {
    "added": [{"model_id": "gpt-4o-2024-11", "reason": "new_model"}],
    "disabled": [{"model_id": "gpt-4o-2024-05", "reason": "not_listed"}],
    "updated": [
      {"model_id": "gpt-4o", "changes": [{"field": "input_price", "old": 2.5, "new": 2.0}]}
    ],
    "failover_updated_groups": [
      {"display_model": "gpt-4o", "removed_model_ids": ["uuid-old"]}
    ]
  }
}
```

In `discover-all` responses the same `diff` object appears per provider inside each `results[]` entry (omitted for providers whose scan failed).

---

## The Model Discrepancy Modal

The badge on the **Models** sidebar item opens this. It answers one question: what does discovery currently believe is wrong, and what can you do about it.

<div align="center">
<img src="screenshots/discrepancy_modal.png" alt="Model discrepancy modal: one collapsed pill per provider with gone and stale counts" width="720"><br>
</div>

### Claims are derived, never stored

Every row is computed from live state (`models`, `model_failover_groups`) on each request. Nothing is read back from the `discovery_changes` journal, so a claim cannot drift from reality and a rescan always corrects it. The only persisted operator intent is two columns: `models.discovery_dismissed_at` (migration `061`) and `model_failover_groups.auto_disabled_at` (`062`).

### The three buckets

| Bucket | Meaning | Counted by the badge? |
| --- | --- | --- |
| **Gone** | The provider stopped listing it and discovery disabled it here. | **Yes** |
| **Suspect** | Still enabled, but absent from recent scans. One more miss and it goes. | No, early warning only |
| **Stale** | Missing over 30 days with no flapping, so almost certainly retired rather than broken. | No |

`ClaimWindow` (30 days) bounds three things that must agree: how far back flap counts are computed, how long journal rows are retained, and how long a quiet gone model waits before it stops counting. Auto-dismiss at that horizon is a *predicate*, not a write, so changing the constant re-derives every claim with no backfill.

### Navigating it

Providers render as collapsed pills carrying their actionable counts. Click a pill to reveal its bucket lines; click a line to reveal the models. **Only one provider and only one bucket line are ever open**, so opening a second collapses the first.

Rows are mounted only while their bucket is open. That is a performance decision, not a cosmetic one: a fleet with eight providers and 179 discrepancies would otherwise hold every row in the DOM at once, and animating a 52-row list open forces a full-subtree relayout on every frame. Scroll past the open provider's header and a return-to-top control floats in; it scrolls without collapsing what you are reading.

### Acting on it

| Control | Scope | Effect |
| --- | --- | --- |
| **Retest** | one provider | Re-runs discovery for it and re-checks its models. |
| **Retest all** | header | Walks every listed provider in turn, cancellable. |
| **Dismiss** | one model | Clears that row from the list. |
| **Dismiss all** | one provider | Clears every gone and stale model on it, in one request, after a confirm. |
| **Dismiss all** | header | Same, across every listed provider. This is the "I saw the badge, I do not need the detail" path: confirm once and the badge clears. |
| **Clean** (broom) | one provider | Appears only once nothing on that provider is actionable. Drops the pill from the view and writes nothing. |

**Dismissing is one-way, and that is deliberate.** There is no undo, and the endpoint has no un-dismiss direction: a dismissal reverses itself when discovery next sights the model, which is the only reversal the feature needs. Nothing here writes anything a scan cannot correct on its own.

**Dismissing never stops discovery.** It clears rows from this list. Discovery keeps sweeping, and `models.Upsert` clears `discovery_dismissed_at` on any sighting, so a dismissed model returns as a fresh claim if its provider lists it again and it later goes missing again. Suspect models cannot be dismissed at all: `setModelsDismissed` only touches `enabled = false` rows, because pre-dismissing a still-enabled model would silently hide the claim the next time it genuinely went missing.

Nothing vanishes when you act on it. A dismissed or resolved row stays struck through where it sat, and a cleared provider keeps its buckets as the log of what you did, until you hit Clean.

### Dismissed is not resolved

The modal distinguishes two reasons a row can clear, because they mean opposite things:

- **dismissed** - you acknowledged it. The model is still gone.
- **back** - the provider is listing it again. It fixed itself.

Both are absent from the next fetch, which is why the distinction has to be tracked client-side rather than inferred from absence.

### Retired rows are pruned

A model the provider stopped listing stays in the table as a disabled row so you can see what went away, retest it, or pin it. It does not stay forever. After each scheduled discovery pass, rows that discovery retired more than `model_prune_days` ago (default 7, maximum 180, `0` to keep everything) are deleted, and their failover groups resynced. The pass only touches providers it just scanned successfully, skips anything that flapped (was re-listed) in the last 30 days, and deletes at most 500 rows per pass. A row's own retirement does not count as a flap, so a horizon shorter than the 30-day flap window is honoured as written. Manual discovery from the dashboard never prunes; only the scheduled and startup passes do.

Never pruned: models you disabled or enabled by hand, models the proxy retired for refusing requests (the provider still lists those), and models of disabled providers (those wait, with their pins, prices and failover memberships, for the provider to come back).

Each fleet member prunes on its own schedule with the same horizon, so members converge without any deletion sync. If a pruned model is listed again later, discovery creates a fresh row.

### Failover group claims

Failover groups that discovery disabled appear in their own section, with live member and routable counts (`1 of 3 members routable` points at a specific broken member where a bare "group disabled" would not). They carry no Retest (a retest is provider-scoped, a group is not) and no dismiss: the claim clears itself once the group is routable again. Deleted groups are deliberately not represented, since both deletion reasons are downstream of gone-model claims that are already counted.

### Recent changes

The informational journal, newest first. It never holds the badge count open, only its dot, and collapsing or expanding it is what marks it seen. Price and context-length moves land here rather than as claims: a price change is news, not a fault.

---

## Provider-Specific Discovery

Each provider type has its own discovery implementation in `internal/provider/discovery_*.go`. Discovery reads the type stored on the provider row, which is the one the operator picked when adding it. A row without a type (created before the column existed) falls back to the legacy URL derivation. Types with no dedicated implementation, including `custom`, use OpenAI-compatible discovery.

### Live + Catalog Merge

Providers that expose a live model list **and** ship a built-in catalog are combined through a shared helper, `mergeLiveAndCatalog` (`internal/provider/catalog_merge.go`), instead of picking one source or the other. The merge has three rules:

1. **Union of models.** The result is every model the live API returned **plus** every catalog model the API did not list. This surfaces models the provider keeps callable without advertising them in its listing endpoint (a freshly released GLM the listing hasn't caught up to, or older Grok models xAI keeps serving), and it means new models the provider adds are picked up automatically with no catalog edit.
2. **Live wins per field.** For a model present in both sources the live value is authoritative. The catalog only fills fields the live result left empty, nil, or a known placeholder (a `display_name` equal to the `model_id`, or `"[]"` modalities). A stale catalog can therefore never mask fresh live data - at worst it supplies slightly outdated gap-fill.
3. **Capabilities are OR-merged.** A capability flag is enabled in the result if either source reports it.

models.dev enrichment runs *after* the merge and fills anything still empty, so the final precedence per field is **live → catalog → models.dev → zero value**. If the live fetch fails entirely (network, auth, 403/429 quota), the discoverer falls back to the pure catalog so discovery never goes dark.

Providers on the merge (union): **Z.AI**, **xAI**, **DeepSeek**, **OpenCode Go**, **OpenCode Zen**. **OpenAI** uses the same live-first model but **backfill-only** (no union) via `backfillLiveFromCatalog`, because discoverOpenAI is the fallback for unknown/custom hosts and must not attach catalog-only gpt-5.x models to them. Providers with a *pricing-only* catalog - **Anthropic**, **Google AI Studio**, **Cohere** - keep their own discoverers: the live API is already the rich model-list source and the catalog only backfills pricing, so there is nothing to union. Pure-live providers (NanoGPT, OpenRouter, Ollama, LM Studio, KoboldCPP, NeuralWatt, Kimi Code, AWS Bedrock, Azure AI Foundry) have no catalog. **MiniMax** is a third, narrower case - call it *live-stub + models.dev*: `discoverMiniMax` has no catalog either, but unlike Kimi Code its live listing is metadata-bare (id and owner only), so every other field (context, pricing, capabilities) comes from models.dev enrichment rather than a rich live payload. **Vertex AI express** is the inverse case: Google exposes no listing route for express keys, so discovery starts from a shipped candidate catalog and validates each entry live (see its section below).

### Provider Type

A provider's type is chosen by the operator in the add dialog and stored on the
row (`providers.provider_type`); nothing re-derives it from the URL afterwards.

For the three self-hosted server families (Ollama, LM Studio, KoboldCPP) the
address says nothing about what is listening on it, so the choice is
**verified** before the provider is saved: Model Hotel probes the identifying
endpoint of the chosen family and refuses the save if another server answers,
naming what it found. The same check runs when an existing provider's base URL
is changed. That means the server has to be running when it is added.

A create request that omits `provider_type` (an API client rather than the
dashboard) falls back to the vendor hostname, using the table below; a host that
matches nothing is a generic OpenAI-compatible endpoint. Rows created before the
column existed are backfilled once at startup, using those same hostname rules
plus the default-port rules that were in force when they were written
(`LegacyTypeFromURL` in `internal/provider/discovery.go`).

Hostname rules (`detectByHost`, exact and suffix matching):

| Hostname Pattern | Path Pattern | Provider Type |
|------------------|--------------|---------------|
| `api.openai.com`, `*.openai.com` | - | `openai` (fallback) |
| `api.anthropic.com`, `*.anthropic.com` | - | `anthropic` |
| `api.deepseek.com`, `*.deepseek.com` | - | `deepseek` |
| `api.nano-gpt.com`, `nano-gpt.com` | - | `nanogpt` |
| `api.z.ai`, `z.ai`, `*.z.ai` | - | `zai-coding` |
| `api.kimi.com`, `kimi.com`, `*.kimi.com` | - | `kimi-code` |
| `api.minimax.io`, `minimax.io`, `*.minimax.io` | - | `minimax` |
| `ollama.com`, `*.ollama.com` | - | `ollama-cloud` |
| `opencode.ai`, `*.opencode.ai` | `/zen/go/` | `opencode-go` |
| `opencode.ai`, `*.opencode.ai` | `/zen/` | `opencode-zen` |
| `openrouter.ai`, `*.openrouter.ai` | - | `openrouter` |
| `api.x.ai`, `x.ai`, `*.x.ai` | - | `xai` |
| `generativelanguage.googleapis.com` (and `*generativelanguage*.googleapis.com`) | - | `google` |
| `aiplatform.googleapis.com` (incl. regional `{region}-aiplatform...`) | - | `vertex-express` |
| `bedrock-mantle.{region}.api.aws` | - | `bedrock` |
| `*.services.ai.azure.com`, `*.openai.azure.com` | - | `azure` |
| `api.cohere.com`, `api.cohere.ai`, `*.cohere.com`, `*.cohere.ai` | - | `cohere` |
| `api.neuralwatt.com`, `neuralwatt.com` | - | `neuralwatt` |
| Any other host | - | `openai` (fallback) |

Self-hosted servers have no entry here: `ollama`, `lmstudio` and `koboldcpp`
are chosen, not matched, and run on whatever address and port the operator gave.
`anthropic-messages` has none either, and for the same reason: it names a wire
format rather than a vendor, so no host implies it. `api.anthropic.com` resolves
to `anthropic` as it always has.

**Identifying endpoints used to verify a self-hosted choice:**

| Type | Endpoint | Match |
|------|----------|-------|
| `koboldcpp` | `GET {origin}/api/extra/version` | `result` equals `KoboldCpp` (the reply also carries the version and the modality flags) |
| `lmstudio` | `GET {origin}/api/v0/models` | a `data` array with LM Studio's native model fields |
| `ollama` | `GET {origin}/api/tags` | a `models` array |

The match is on the body, never on the status: LM Studio answers routes it does
not serve with HTTP 200 and an `{"error": ...}` body, so a status-only check
would identify it as whichever family was probed first.

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

### Anthropic (Messages API) - `anthropic-messages`

**Source files:** `discovery_anthropic.go` (shared with `anthropic`)

**Method:** Identical to Anthropic above, and deliberately so: the models listing is part of the Messages API surface, so any endpoint serving that API answers `GET /v1/models` in the same shape. The type exists for an endpoint that speaks Anthropic's Messages API but is not Anthropic's own: an operator types the base URL, nothing is inferred from the host, and no host rule ever resolves to this type.

Model metadata, `owned_by` included, is produced exactly as for `anthropic`: the listing carries no ownership field, and reporting a different owner per provider type would make the same model look like two different things. (Leaving it empty for this type was tried and reverted, because models.dev enrichment then fills `owned_by` from the model *family*: `claude-fable-5` came back owned by `anthropic` under one provider and `claude-fable` under the other.) The Anthropic pricing catalog still applies to any `claude-*` ID that matches, and models.dev enrichment covers the rest.

The one real difference from `anthropic`: **every chat request is translated**, not just the ones carrying a document. An `anthropic` provider defaults to Anthropic's OpenAI-compat `/v1/chat/completions` and only re-routes through `internal/anthropicegress` for content that endpoint cannot express; an `anthropic-messages` provider has no compat endpoint at all, so all of its chat traffic goes to `/v1/messages` through the same adapter. A client that speaks Anthropic natively (`/v1/messages` in, see `anthropic_native.go`) is forwarded verbatim in both directions, so `cache_control` and thinking blocks survive.

Because everything is translated, `reasoning_effort` is honoured on this type (and only this type: `anthropic` strips it, since its default route has no thinking control and honouring it on document requests alone would make one request reason and the next not). See **Extended thinking** below.

Discovery fails loudly if the endpoint does not serve the listing, rather than adding a provider whose models were never confirmed to exist.

#### Extended thinking

An OpenAI client asks for reasoning with `reasoning_effort`, and Anthropic takes that request in one of two mutually exclusive shapes:

| Shape | Request | Models |
|-------|---------|--------|
| adaptive | `thinking: {type: "adaptive"}` + `output_config: {effort}` | the newer ones; the model chooses how much to think under the ceiling |
| budget | `thinking: {type: "enabled", budget_tokens}` | the older ones; a fixed token allowance |

Nothing in a model id says which it takes, and the split is not generational. Measured live on 2026-08-20: `claude-opus-5` and `claude-sonnet-5` accept adaptive **only**, `claude-opus-4-5` and `claude-haiku-4-5` accept budget **only**, and `claude-sonnet-4-6` accepts **both**. A third-party Messages endpoint may serve model ids that follow no Anthropic naming convention at all.

So the dialect is learned rather than guessed. MH asks in the adaptive shape (what current models want), and if the upstream refuses with the 400 that names the other shape, it records the dialect for that provider+model and re-issues the request once. The caller sees the answer, not the 400, and no later request to that model pays the extra round-trip. The cache is in memory and per instance, like the learned param caches: relearning after a restart costs one 400, and the alternative is a stored fact that goes stale when Anthropic moves a model between dialects. See `internal/proxy/anthropic_thinking_retry.go`.

The same self-heal covers the other per-model fact no id reveals: **a param the model has retired.** `claude-sonnet-5` and `claude-opus-5` answer `` `temperature` is deprecated for this model `` while every 4.x model accepts it, and OpenAI clients send `temperature` as a matter of course, so this is the more common of the two. A Messages 400 naming a rejected param is learned into the same `deprecationCache` the compat path uses and the request re-issued without it. Learning is deliberately restricted to `anthropic-messages` providers, whose only route is Messages: the cache is keyed by provider+model, so learning a strip from a Messages 400 on an `anthropic` provider could remove a param from that model's compat traffic, which accepts it.

One retry covers both, because they cannot co-occur: a thinking request has already had its sampling params dropped, and a request without thinking cannot earn a dialect complaint.

Two consequences worth knowing:

- **Sampling params are dropped from a thinking request.** Anthropic rejects any `temperature` but 1 when thinking is on, in either shape, and treats `top_p`/`top_k` the same way. A caller who asks to think is asking about reasoning depth, so the reasoning request wins and the sampling knobs go.
- **A small `max_tokens` is raised.** Thinking tokens come out of the same allowance as the answer, so a tight budget can be spent entirely on thinking: a live `claude-sonnet-5` given `max_tokens: 30` returned 29 thinking tokens, no text, and `finish_reason: "length"`. An adaptive request with a smaller allowance is raised to the default, and a budget request is raised above its budget.

Effort maps across directly: `low`/`medium`/`high` are common to both scales, OpenAI's `minimal` floors to Anthropic's `low`, and Anthropic's two extra levels (`xhigh`, `max`) are honoured if a client names them. Anything else (`none`, absent) leaves thinking off and the sampling params intact.

### AWS Bedrock

**Source files:** `discovery_bedrock.go`

**Method:** Calls `GET /models` on Bedrock's OpenAI-optimized **bedrock-mantle** endpoint (`https://bedrock-mantle.{region}.api.aws/v1`, e.g. `us-east-1`), authenticated with a [Bedrock API key](https://docs.aws.amazon.com/bedrock/latest/userguide/api-keys.html) as a bearer token. The listing is OpenAI-shaped; entries become clean stubs enriched by models.dev (the enrichment lookup strips Bedrock's `vendor.` ID prefix, so `openai.gpt-oss-120b` matches the models.dev `gpt-oss-120b` entry).

Only mantle is supported: the classic `bedrock-runtime` endpoint serves chat solely under `/openai/v1` and exposes **no models listing at all**, so discovery cannot work against it. Point the provider at a mantle URL.

**Anthropic models are skipped at discovery.** On Bedrock, `anthropic.*` models reject `/v1/chat/completions` (they are served only through the Anthropic Messages dialect at `{base}/anthropic/v1/messages`, which the proxy does not forward to). Exposing them would list models that fail on every chat request, so the discoverer drops them (logged at debug level, with an aggregate `skipped_messages_dialect` count in the completion log line).

**Account prerequisites for Bedrock itself** (not MH-specific): most non-Anthropic models (GPT-OSS, GPT-5.x, Qwen, Kimi, GLM, DeepSeek, Mistral, Gemma, ...) work as soon as you generate an API key in the Bedrock console. Anthropic models additionally require a valid payment method, the Anthropic first-time-use form, a per-model Marketplace agreement (`aws bedrock create-foundation-model-agreement`), and on new or low-usage accounts may still be gated behind an AWS support request.

### Azure AI Foundry

**Source files:** `discovery_azure.go`

**Method:** Azure is deployment-based: `GET /openai/v1/models` returns the full Azure model *catalog* (300+ entries), but only **deployments** the user created are invokable — and requests must name the deployment, not the base model. Discovery therefore enumerates deployments, via one of two routes depending on the base URL:

- **Foundry project endpoint** (`https://{resource}.services.ai.azure.com/api/projects/{project}` — exactly the string the Foundry portal hands you; recommended): lists via the project data-plane (`{root}/api/projects/{project}/deployments?api-version=v1`), which also carries the underlying model name, version, and publisher.
- **Anything else on an Azure AI host** (a bare resource root or an `/openai/v1` base, including classic `{resource}.openai.azure.com` resources): lists via the classic data-plane route (`{root}/openai/deployments?api-version=2023-03-15-preview` — the only api-version that still serves the listing; GA versions dropped it). Non-`succeeded` deployments are skipped.

Both routes accept the resource API key as a **bearer token** (the legacy `api-key` header also works but MH doesn't need it). Whatever base URL shape is configured, the proxy sends chat traffic to the one real inference surface, `https://{host}/openai/v1/chat/completions` (no `api-version` parameter needed on the v1 surface).

**Enrichment for aliased deployments:** the deployment name becomes the model ID (it is the invokable identifier) and the underlying base-model name is kept as the model's internal name. models.dev enrichment matches the deployment name first and falls back to the base-model name, so a deployment called `my-fast-gpt` backing `gpt-4.1-mini` still gets context/pricing metadata.

**Account prerequisites for Azure itself** (not MH-specific): create an Azure AI Foundry resource (or classic Azure OpenAI resource) in the Azure portal, then **deploy at least one model** (Foundry portal → Deployments → Deploy model). A resource with zero deployments discovers zero models (MH logs a warning telling you to deploy first). Azure-sold models (OpenAI family) need no extra agreement; partner/community models (Meta, Mistral, xAI, Anthropic, ...) may require an Azure Marketplace subscription accepted during deployment.

### Vertex AI (express keys)

**Source files:** `discovery_vertex.go`, `vertex_catalog.go`, `catalogs/vertex_express.json`

**Method:** Vertex AI **express-mode** API keys (free-tier keys from [express mode](https://cloud.google.com/vertex-ai/generative-ai/docs/start/express-mode/overview), no billing account needed) only work on Google's *native* publisher routes — every OpenAI-compatible Google surface rejects them, and **no model-listing route accepts them** (the publishers listing wants OAuth). Discovery therefore starts from a shipped candidate list (`catalogs/vertex_express.json`) and validates each entry with a free `POST .../models/{id}:countTokens` probe (parallel, bounded concurrency):

- **200** → the key can invoke the model; it is kept as a clean stub for models.dev enrichment (context, pricing, modalities).
- **404** → not express-eligible (or retired); dropped silently. Not-yet-eligible candidates stay in the catalog so they light up automatically once Google enables them for express mode.
- **401/403** → discovery fails loudly, so a bad key reads as an error instead of "zero models".

**Chat traffic is translated, not proxied.** Gemini's native `generateContent` dialect is not OpenAI-shaped, so requests to a vertex-express provider go through MH's Gemini egress adapter (`internal/gemini`): the chat-completions body is rewritten to `generateContent` on the way out (system → `systemInstruction`, tools → `functionDeclarations` with full JSON Schema, images → `inlineData`, `reasoning_effort` → thinking budget, JSON response formats → `responseJsonSchema`, penalties/seed → `generationConfig`) and the response — including SSE streams, tool calls, and thinking-token usage — is translated back to the chat-completions shape before the rest of the pipeline sees it. Failover groups can therefore mix vertex-express with OpenAI-compatible providers transparently. Auth uses the `x-goog-api-key` header.

**Account prerequisites for Vertex itself** (not MH-specific): sign up for Vertex AI express mode with a Google account and copy the express API key (`AQ.`-prefixed). Free-tier keys expire after 90 days and cover a subset of Gemini models under pre-GA terms; a paid Vertex key on the same routes works identically.

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

**Method:** Fetches the live OpenAI-compatible model list from `GET /models` on the coding-plan base URL, then merges it with the built-in `zaiCatalog` via [`mergeLiveAndCatalog`](#live--catalog-merge). The live listing supplies the authoritative model set and `owned_by`; the catalog backfills context length, max output, capability flags, and modality, and unions in catalog models the listing omits (a freshly released GLM, or the vision/turbo variants the coding plan serves but does not advertise). If the `/models` fetch fails, the scan **aborts** rather than falling back to the pure catalog: the catalog is a subset of the live listing, so a catalog-only result would let `RecordMissingModels` disable every live-only model on a transient outage.

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
| Pricing | Catalog **overrides only** (see below); otherwise models.dev (canonical `zai` entry) |

**Pricing:** most Z.AI prices come from models.dev enrichment via its canonical `zai` provider entry, which tracks the [official pricing page](https://docs.z.ai/guides/overview/pricing). The catalog carries per-model price *overrides* only for models that entry lacks - currently `glm-4.5-x` and `glm-4.5-airx` (official prices restated from the pricing page). Do not duplicate a models.dev-covered price into the catalog, and do not guess a price for a model whose official price is not yet published (`glm-5.3` at its release): the catalog wins over models.dev, so a duplicate or a guess keeps enforcing itself after the real price lands. An unpriced model meters at zero and is named in the discovery warning log until models.dev lists it, at which point [price-follows-source](#stored-metadata-on-re-scan-context-is-stable-prices-follow-source) propagates the real price to existing rows on the next scan.

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

### Kimi Code

**Source files:** `discovery_kimi.go`

**Method:** Moonshot's coding-subscription endpoint (base URL `https://api.kimi.com/coding/v1`, API keys `sk-kimi-...` from the Kimi Code console). `discoverKimiCode` fetches `GET {base}/models`, an OpenAI-shaped listing with rich extras that are mapped directly onto the model rather than routed through a catalog - there is no embedded catalog and no models.dev fallback for this provider; everything comes from the live API.

Subscription keys only work against `api.kimi.com/coding` - they 401 on Moonshot's pay-per-token platform (`api.moonshot.ai`) and vice versa, since the two are isolated key namespaces. A platform key pointed at `api.moonshot.ai` is handled by the generic OpenAI-compatible discoverer instead.

**Live API provides:**

| Field | Source |
|-------|--------|
| Model list | API (`GET /models`) |
| Display name | API (`display_name`) |
| Context length | API (`context_length`, e.g. `262144`) |
| Reasoning | API (`supports_reasoning`) |
| Vision / image input | API (`supports_image_in`) |
| Video input | API (`supports_video_in`) |
| Owned by | API (recorded as `moonshotai`) |

Known models: `k3` (K3), `kimi-for-coding` (K2.7 Coding), `kimi-for-coding-highspeed` (K2.7 Coding Highspeed). All three are thinking-only - reasoning cannot be disabled, and responses carry DeepSeek-style `reasoning_content`.

### MiniMax

**Source files:** `discovery_minimax.go`

**Method:** MiniMax's international platform (base URL `https://api.minimax.io/v1`). Token Plan subscription keys (`sk-cp-...`) and pay-as-you-go keys (`sk-api-...`) share the same OpenAI-compatible endpoint, but only Token Plan keys have quota data to report (see below). `discoverMiniMax` fetches `GET {base}/models`, a metadata-bare OpenAI-shaped listing (id and owner only) - there is no embedded catalog for this provider, so every model becomes a live stub and models.dev fills in context length, pricing, capabilities, and reasoning support.

The CN twin (`api.minimaxi.com`) is a separate key namespace and is left to the generic OpenAI-compatible discoverer.

**Live API provides:**

| Field | Source |
|-------|--------|
| Model list | API (`GET /models`) |
| Owned by | API (recorded as `minimax`) |

Everything else - context length, pricing, capabilities, reasoning - is backfilled by models.dev enrichment rather than the live listing.

Known models: `MiniMax-M3`, `MiniMax-M2.7` (+ `MiniMax-M2.7-highspeed`), `MiniMax-M2.5` (+ `MiniMax-M2.5-highspeed`), `MiniMax-M2.1` (+ `MiniMax-M2.1-highspeed`), and `MiniMax-M2`.

**Chat-completion HTTP-200 business errors:** MiniMax reports chat-completion failures (rate limit, exhausted Token Plan balance, auth rejection) inside a real HTTP `200` whose JSON body carries `base_resp.status_code != 0` (e.g. `1008` "insufficient balance"). The proxy's failover, circuit-breaker, and error-forwarding paths are all keyed on `resp.StatusCode`, so an unmodified `200` would be treated as success - the client gets an empty completion and no failover fires. For minimax-typed providers, the proxy inspects each non-streaming `200` and remaps the business code to the HTTP status it stands for (`1002`/`1039`/`1008` rate/token/balance to `429`, `1004` auth to `401`, anything else to `502`), restoring the original body so the error message still forwards. Genuine successes (`base_resp.status_code == 0`), streaming SSE responses, and unparseable bodies are passed through untouched. This is a proxy concern only; the quota endpoint (`GetMiniMaxQuota`, below) still passes `base_resp` through to the dashboard as-is.

### OpenCode Go

**Source files:** `discovery_opencode_go.go`, `opencode_go_catalog.go`, `opencode_catalog_types.go`, `catalog_merge.go`

**Method:** Calls `GET /models` (OpenAI-compatible list endpoint), converts the listing to clean stubs, and merges them with the built-in catalog via [`mergeLiveAndCatalog`](#live--catalog-merge). The catalog is an **override channel that is normally empty**: every live model's metadata and per-token prices come from models.dev's `opencode-go` entry (with the cross-provider index as gap coverage). Those prices are not what a Go subscriber pays per request - they are the shadow cost that Go's dollar-based quotas ($/5h, $/week, $/month) burn, the same convention used for the Z.AI and Kimi coding plans. A `404` (endpoint gone) falls back to the catalog - normally empty, so the scan yields no models and nothing gets disabled; other non-200s abort the scan so a transient outage can't disable live-only models. (Quota overrun does not gate the listing - it still returns `200`.)

**Catalog rows, when present, are overrides:** a row wins over models.dev for every field it sets, and `OpenCodeCatalogToModel` always materializes the price fields - so an override row **must state real prices**, because omitting them pins the model's price at $0 and it meters free.

### OpenCode Zen

**Source files:** `discovery_opencode_zen.go`, `opencode_zen_catalog.go`, `opencode_catalog_types.go`, `catalog_merge.go`

**Method:** For **keyed** providers, same as OpenCode Go - `GET /models` merged with the catalog via [`mergeLiveAndCatalog`](#live--catalog-merge). For **keyless** providers (no API key), the merge is bypassed: only free (zero-priced) catalog models the live listing includes are returned, with no union, since a keyless caller must not be shown models it cannot reach.

The catalog and model conversion logic is shared with OpenCode Go via `OpenCodeModelSpec` and `OpenCodeCatalogToModel`. The Zen catalog carries **only the zero-priced free-model rows** - they are load-bearing for the keyless path above, which can only surface free models the catalog identifies. Paid models take their metadata and pricing from live + models.dev (`opencode` entry). (OpenCode Zen rotates free models aggressively; stale delisted free/preview entries are pruned from the catalog rather than unioned in as dead models.)

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

**Method:** LMStudio is a self-hosted server exposing an OpenAI-compatible API on whatever address it was started on. Discovery reads its native listing (`GET /api/v0/models`), which reports context length and capabilities, and falls back to `GET /v1/models` when that endpoint is unavailable. No built-in catalog is used.

**Detection:** Chosen by the operator, confirmed by probing `/api/v0/models` when the provider is added or its URL changed.

**Hardcoded / missing:**

| Field | Value |
|-------|-------|
| Context length | From the native listing (`max_context_length`); not set on the `/v1/models` fallback |
| Max output tokens | Not set |
| Pricing | None (self-hosted) |
| Capabilities | From the native listing (e.g. `tool_use`); not set on the fallback |
| Modality | From the native listing's model `type`; not set on the fallback |

### KoboldCPP

**Source files:** `discovery_koboldcpp.go`

**Method:** KoboldCPP is a self-hosted server exposing an OpenAI-compatible API on whatever address it was started on. Discovery confirms the server via `GET /api/extra/version`, reads the loaded model from `GET /v1/models`, and takes the context size from `GET /api/extra/true_max_context_length`. Image and audio input come from the version endpoint's `vision` and `audio` flags, which describe the adapters the loaded chat model was given. No built-in catalog is used.

**Detection:** Chosen by the operator, confirmed by probing `/api/extra/version` when the provider is added or its URL changed.

**Hardcoded / missing:**

| Field | Value |
|-------|-------|
| Context length | From `/api/extra/true_max_context_length`; left unset when that endpoint is unavailable |
| Max output tokens | Not set |
| Pricing | None (self-hosted) |
| Capabilities | Hardcoded: streaming on, tool calling off (KoboldCPP uses its own tool format) |
| Modality | Input modalities from the version endpoint's `vision` and `audio` flags. `transcribe`, `tts`, `txt2img` and `embeddings` are separate endpoints and are deliberately ignored |

---

## Models.dev Enrichment

**Source file:** `internal/provider/modelsdev.go`

In addition to provider-specific discovery and built-in catalogs, Model Hotel can enrich models using the [models.dev](https://models.dev/) open-source model catalogue. This community-maintained database covers 40+ providers and provides pricing, context limits, capabilities, and modality data for thousands of models.

### How It Works

1. On server startup, a blocking call in `main.go` fetches `https://models.dev/api.json` with the default HTTP client (no explicit timeout).
2. The response is parsed into two in-memory indexes: a per-provider index (models.dev provider ID → model ID → spec) and a cross-provider index keyed by bare model ID.
3. During **every** discovery run (after the provider-specific discovery function returns its model list), each model is passed through the enrichment layer along with the provider's detected type.
4. `EnrichModel` fills **only empty or zero-value fields** - it never overwrites data already populated by the provider API or built-in catalog.
5. If the models.dev fetch fails (network error, timeout, invalid JSON), enrichment is silently disabled. Existing catalogue data is never at risk.

### Canonical Provider Preference

The same bare model ID appears under dozens of models.dev providers - the official vendor plus resellers, each publishing its own prices (`glm-5.2` is listed by 26 providers). Enrichment therefore resolves specs in two steps:

1. **Canonical provider first.** `modelsDevProviderForType` maps each Model Hotel provider type to the models.dev provider entry that carries the vendor's own official metadata (`zai-coding` → `zai`, `minimax` → `minimax`, `kimi-code` → `moonshotai`, ...). That entry's models are consulted first, so a reseller's price can never shadow the official one. Coding-plan types deliberately map to the pay-per-token vendor entry, not the `-coding-plan` entry (which prices everything at $0): Model Hotel meters the shadow cost a request would have had at list price.
2. **Cross-provider fallback.** On a miss (or for unmapped/custom provider types) the lookup falls back to the cross-provider index. That index is built deterministically - canonical vendor entries are ranked ahead of all other providers, each group sorted by provider ID - so a colliding bare ID always resolves to the same spec across restarts. (Previously the winner was whichever provider a Go map iteration happened to visit first, which let random reseller prices land on official models.)

### Matching Logic

Within each index, models are matched by their `model_id` using progressive fallback:

1. **Exact match** - `gpt-4o` → `gpt-4o`
2. **Strip date suffix** - `gpt-4o-2024-08-06` → `gpt-4o`
3. **Strip version suffix** - `claude-sonnet-4-5-20250514` → `claude-sonnet-4-5`
4. **Longest prefix match** - finds the models.dev entry with the longest matching prefix

The `lookupFuzzyIn` helper implements this logic (the canonical-provider and cross-provider passes run it against their respective maps), handling date patterns like `YYYY-MM-DD`, `YYYYMMDD`, and version suffixes.

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
    missing_scans INTEGER NOT NULL DEFAULT 0,
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
- `054_model_missing_scans.sql` - Added `missing_scans` consecutive-miss counter

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
| xAI | `grok-4.3`, `grok-4.5` |
| Google | `gemini-2.5-flash`, `gemini-3.6-flash` (stripped from `models/gemini-3.6-flash`) |
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
  - If `disabled_manually = false`, the model is **re-enabled** (it may have been previously disabled by `RecordMissingModels` but is now back).
  - If `disabled_manually = true`, the model **stays disabled** - the user's manual override is respected even if the model reappears in the provider API.
- `last_seen_at` is always updated to `now()`.

```sql
enabled = CASE WHEN models.disabled_manually = false THEN true ELSE models.enabled END
```

### Stored metadata on re-scan: context is stable, prices follow source

The same `ON CONFLICT` update merges pricing/context per field rather than blindly overwriting:

- **Context length / max output tokens** are *fill-only* unless the value came from the provider's own live API this scan (tracked via transient per-field live provenance): a live value overwrites, a catalog/models.dev value only fills a gap. This keeps stored metadata stable when sources disagree or a probe is flaky.
- **Prices follow their source** (`price_customized = false`, the default): the scan's price - live API, embedded catalog, or models.dev enrichment, already merged in that precedence - **overwrites** the stored one; only a scan that carries no price at all keeps the stored value. Vendor price changes and corrected enrichment data therefore propagate to existing rows on the next scan. Installs that upgraded past the random-reseller-price bug (see [Canonical Provider Preference](#canonical-provider-preference)) heal automatically on their first scan, with no migration or manual reset.
- **Operator-pinned prices** (`price_customized = true`) are untouchable: no source, live included, overwrites them. Editing any price via `PATCH /api/models/{id}` sets the pin implicitly; sending `"price_customized": false` clears it AND nulls the price columns so the next scan re-derives them (the dashboard's model detail modal surfaces this as "Reset to source" on the pin banner).

### Missing models: three layers of proof before a disable

A model absent from one listing is **never** disabled immediately. Discovery
requires three independent layers of evidence, so one DNS flap, transient 5xx,
or partial upstream listing cannot disable models (which, in an HA fleet,
would then propagate to every member). Only the scheduled/startup background
sweep applies these layers and disables; manual `Discover` scans just import
what they see (see [Manual Discovery](#manual-discovery)).

1. **Transport retries.** Every discovery HTTP call (listings and per-model
   detail probes) retries transient network errors and 429/5xx responses up to
   3 attempts with linear backoff and jitter. Ollama-family per-model
   `/api/show` failures no longer drop the model from the results either: the
   model was listed by `/api/tags`, so it is kept with default metadata.
2. **In-scan confirmation probes (`ConfirmMissingModels`).** If the listing is
   missing any previously enabled model, discovery re-runs the full listing up
   to 2 more times (after ~15s and ~45s backoff plus jitter) and takes the
   union of all probes' model IDs as the scan's membership. A model only
   counts as *missing this scan* if every probe missed it. If a confirmation
   probe itself fails, or if the confirmed-missing set is implausibly large
   (more than 5 models AND more than half the provider's enabled models - the
   *mass-vanish guard*, which emits a `discovery.suspect_scan` warning event),
   the whole scan is treated as suspect and records no misses at all. A provider
   that genuinely retires that many models would otherwise trip the guard on
   every scan and stay stale forever, so a per-provider `suspect_scans` counter
   tracks consecutive mass-vanish scans; once it reaches 3 (and every 3 after),
   discovery raises a distinct high-severity `discovery.bulk_removal_suspected`
   event asking an operator to review and disable the retired models by hand. It
   never auto-disables on this signal - disabling half a provider's catalog is
   too destructive to do unattended when it fans out across an HA fleet. A
   healthy scan (the listing recovered) resets the counter to 0.
3. **Cross-scan miss streak (`RecordMissingModels`).** A confirmed miss
   increments the model's `missing_scans` counter. Only when the streak
   reaches 2 consecutive scans is the model disabled (`enabled = false`). Any
   sighting - a scheduled scan, a manual re-test, even a confirmation probe -
   resets the streak to 0.

`RecordMissingModels` returns the newly disabled models, which the discovery
handlers use to (a) re-sync the failover groups those models belonged to,
pruning their stale entries, and (b) report them in the scan's `diff` with
reason `not_listed`. Models missing but below the threshold are only logged
("pending"); they appear nowhere in the diff and stay fully routable.

Note: `RecordMissingModels` sets `enabled = false` but does **not** set `disabled_manually = true`. This means the model will be automatically re-enabled on the next discovery run if it reappears. On the Models page, discovery-disabled models (`enabled = false`, `disabled_manually = false`) show a tooltip on the status badge: "Not listed by the provider since {date}" (based on `last_seen_at`).

Miss recording can only run after a **successful** listing, and it is skipped entirely when the discovered list is empty, when the pre-scan snapshot could not be read, or when any model upsert failed - a provider outage, auth error, or DB hiccup aborts the scan before anything is counted missing, so "disabled by discovery" always means *"the provider repeatedly responded and did not list this model"*.

In summary:
- **Auto-disabled** models (removed from the provider API) have `disabled_manually = false` and are re-enabled if they reappear.
- **Manually disabled** models have `disabled_manually = true` and stay disabled even if the model reappears in the provider API.

### Traffic-driven retirement: verified before it is written

Discovery can only act when a model leaves a provider's listing. Some providers
keep serving a listing entry for a model they have already shut down (Google
kept `gemini-2.0-flash` listed for two months after retirement, OpenCode Zen
lists `claude-sonnet-4` and refuses it), so the only thing that knows such a
model is dead is a real request to it. The proxy therefore also retires models
from traffic, and every one of those retirements is verified with an upstream
request before anything is written.

How a retirement is reached:

1. **Nomination.** A request the provider refuses as a retirement counts one
   strike against that model. Three strikes nominate it, with no successful
   request in between and no more than 30 minutes between one strike and the
   next. That is a gap, not a deadline: refusals at 0, 29 and 58 minutes are one
   streak of three, while a model refused once an hour never accumulates one.
   Strikes are in-memory and per gateway instance: they are not persisted, and
   each HA member reaches its own conclusion from its own traffic.

   A refusal is read two ways. In prose, the model's own id has to sit beside the
   phrase that retires it, with no clause break between them, and a phrase that
   only refuses one capability ("not supported for this endpoint") does not
   count. In fields, a model-scoped error code (`model_not_found`,
   `model_not_supported`) is a retirement on its own because it names its own
   subject, and a generic `not_found_error` counts only when the error's message
   also names the model. That name may be a dated snapshot, since providers
   resolve an alias like `claude-sonnet-4` and answer about
   `claude-sonnet-4-20250514`. Only dash-separated digit runs are accepted as
   that kind of suffix, so `gpt-4.1` still cannot retire `gpt-4`.

   Strikes are counted per surface, and a refusal only counts on a surface the
   model is known to serve. Chat and embeddings keep separate counts, because the
   probe asks on the surface the strikes came from and the two are different
   questions. Sending a chat model to `/v1/embeddings` draws a capability error
   that names the model, which reads exactly like a retirement, and a
   misconfigured client must not be able to disable a model that serves chat
   perfectly.

   The two surfaces treat a silent catalog differently, on purpose. A refusal on
   `/v1/embeddings` only counts when the model's `output_modalities` say it
   produces embeddings, so an embeddings model whose catalog entry declares
   nothing is never auto-retired. A refusal on chat counts unless the entry
   positively describes something a chat completion cannot be about: an image,
   video, audio, embedding or rerank output, or an input that admits no text
   (a speech-to-text model produces text like any chat model and gives itself
   away on the input side). Chat is what most models are and where most refusals
   arrive, so requiring a declared modality there would switch traffic-driven
   retirement off for every uncatalogued model at once, while guessing wrong on
   embeddings retires a working chat model everywhere.

   A success clears the surface it arrived on and only that one, for the same
   reason the counts are separate: a provider can retire a model's chat surface
   while still serving its embeddings, and a global clear would let the healthy
   surface hold the dead one open forever. Traffic on a surface that is never
   auto-retired (images, speech, rerank) clears nothing at all.

   A model the catalog says serves BOTH chat and embeddings is never
   auto-retired. Disabling turns off the model row, so it cannot express "gone on
   chat, still serving embeddings", and no probe can catch that: the probe would
   be right about the surface it asked, and the disable simply broader than what
   was found. Such a model stays enabled until discovery drops it or you disable
   it by hand. It needs a provider serving one model id on both surfaces, which
   is rare.
2. **Adjudication.** At the threshold the gateway sends a real, minimal request
   to the model itself (a 64-token chat completion, or a one-input embedding),
   off the request path. Content coming back means the model works: the strike
   count is cleared, nothing is disabled, and a warning is logged because a model
   that refuses real traffic and answers a probe is worth a look. Such a model
   needs three fresh strikes AND the probe cooldown below before it is asked
   again, which matters because a provider whose prose disagrees with its own
   behaviour keeps producing this outcome. The provider
   refusing the model by name is what writes the disable. Anything else (a 429,
   a 5xx, an entitlement failure, a timeout, an unreadable answer) establishes
   nothing and postpones.
3. **The write.** A confirmed retirement sets `enabled = false` and stamps
   `auto_retired_at`, revalidates the custom failover groups the model belonged
   to, and publishes a `model.auto_disabled_gone` event.

Two consequences worth knowing about before you upgrade:

- **Every retirement decision costs one upstream request.** It is a call you did
  not make, so it is logged as one: the `proxy: auto-disabled retired model`
  line and the `model.auto_disabled_gone` event both carry `probe_verdict` and
  the endpoint family. A retirement without `probe_verdict: refused` did not
  come from this path. The rate is bounded on two axes: a model is probed at
  most once every 5 minutes however hard it is being retried, and at most 4
  probes are in flight against any one provider at a time. Probes deliberately
  skip rate limiting and circuit-breaker accounting (a verification must not be
  able to sideline a healthy provider), but they do respect an already-open
  circuit and postpone instead of calling a provider the gateway has sidelined.

  A model whose probe can never answer keeps its strikes and keeps paying that
  cost. After three postponements in a row the line escalates from info to
  warning (`proxy: auto-disable postponed repeatedly`, carrying
  `inconclusive_probes`), so a provider that rate limits the gateway, or a model
  that cannot be reached on the surface the probe asks, shows up as itself rather
  than as ordinary noise. Nothing is retired on the strength of it; the run ends
  as soon as the model answers.

  Every finished probe is also counted, so the cost and the classifier's accuracy
  can be watched without reading logs:

  ```
  modelhotel_retirement_probes_total{provider,model,verdict}
  ```

  `verdict` is `refused`, `served` or `inconclusive`. The interesting figure is
  the ratio rather than any single series: a rising `served` count means the
  classifier keeps nominating models that are alive, and a rising `inconclusive`
  count means those nominations are not being settled, so the model keeps its
  strikes and comes back every cooldown. Most inconclusive verdicts cost an
  upstream request, but not all of them do (a request that cannot be built for
  that provider, for instance, is counted without anything being sent), so read
  the series as unanswered questions rather than as a spend figure.

  Two things it does not say. `refused` counts verdicts, not completed
  retirements: the write that follows can still be called off by a late success
  or refused by the database, so count retirements with the
  `model.auto_disabled_gone` event instead. And `model` here is the provider-side
  model id, whereas `modelhotel_requests_total` carries the name the client asked
  for. For ordinary `provider/model` traffic those are the same string and the two
  metrics join cleanly. They part company on requests routed through a failover
  group, which `modelhotel_requests_total` labels `hotel/<group>` while this
  counter uses the real model id, so a join covers direct traffic and silently
  drops the group-routed kind. (Validation failures are labelled `unresolved`
  there and are the other place the two label spaces differ, but they never
  reached a provider, so there is no probe for them to have joined to.)
- **Some endpoint families are never auto-retired from traffic.** Only chat,
  messages and embeddings models can be verified cheaply and safely. Image, TTS,
  STT and rerank models are never auto-retired at all, because a chat probe
  against one fails for reasons that have nothing to do with retirement and that
  failure would read as confirmation. Retiring them without verification is the
  guessing this design exists to remove, so a retired image or TTS model stays
  enabled until discovery drops it from the listing or you disable it by hand.
  Refusals on those families are logged at debug level and no strikes are kept.

A model retired this way is distinct from both a manual disable and a discovery
disable (see migration `063`): re-appearing in a listing does not revive it,
because the provider was refusing it while still listing it. Enabling it by hand
clears the stamp, and if the model really is gone it is retired again with a
fresh alert, always behind a fresh probe. What differs is how it gets there.
Strikes are kept in memory (see above), so if the count was cleared (the gateway
restarted, 30 minutes passed with no further refusal, or a request to the model
succeeded) the model re-earns its three strikes before the next probe. If the
count is still parked at the threshold in the same running process, with no
successful request in between, the first refusal past the probe cooldown claims
a probe directly instead of re-earning three strikes; that is also how a disable
that failed to write is retried. Either way, nothing is retired without a probe
confirming it first, and the probe cooldown applies to every one of those
routes. A success resets what the model is accused of, not the rate at which the
gateway may ask the provider about it.

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
- `RecordMissingModels()` - Miss-streak recording / threshold disable

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
| Kimi Code | API | - | API | API | Live API (no catalog, no models.dev) |
| MiniMax | models.dev | models.dev | models.dev | models.dev | Live API (metadata-bare `GET /models`; no catalog, enriched via models.dev) |

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
| Kimi Code | `GET /usages` | `GetKimiCodeQuota` | 5-hour and weekly quota (limit/remaining/reset time), parallel-request limit, and membership tier |
| MiniMax | `GET /token_plan/remains` | `GetMiniMaxQuota` | 5-hour and weekly Token Plan quota per model class (status and remaining percent), passed through as-is including `base_resp` |

These are exposed via:
- `GET /api/providers/{id}/usage` - for NanoGPT, Z.AI, OpenRouter, NeuralWatt, Kimi Code, and MiniMax
- `GET /api/providers/{id}/balance` - for DeepSeek
- `POST /api/providers/refresh-quotas` - refreshes usage/balance for all supported providers

Quota/balance fetches use a circuit breaker with 5 consecutive failure threshold and 5-minute cooldown.

---

## Related Documentation

- [[Failover & Hotel Routing]] - How discovered models are grouped for automatic failover
- [[Security]] - Provider key encryption and virtual key hashing
- [[Home]] - Architecture overview and feature summary
