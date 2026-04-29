# Chat & Arena

The dashboard includes interactive tools for testing and comparing models: **Chat**, **Conversation**, and **Arena**.

## Chat Mode

The standard chat interface for interactive model testing with a single model.

### Features

- **Model picker** — Select any discovered model with search and filtering. Collapsible sections for provider grouping.
- **System personas** — Choose from preset characters or enter a custom system prompt via `PersonaPicker`:
  - **Merlin** — Mythic allegory (wizards, quests, and ancient wisdom)
  - **Madame Vex** — Aggressively positive life coach
  - **Sarge** — Hard-boiled detective noir
  - **Auntie Wei** — Gossiping neighbor with strong opinions
  - **Grimm** — Museum docent (creepy, pedantic, exacting)
  - **Kairos** — Sports commentator (every moment is the defining moment)
- **Generation parameters** — All 7 parameters are adjustable:
  1. `temperature` — Randomness (0–2, step 0.01)
  2. `top_p` — Nucleus sampling (0–1, step 0.01)
  3. `max_tokens` — Maximum output length (1–32768, step 1)
  4. `min_p` — Minimum probability threshold (0–1, step 0.01)
  5. `top_k` — Top-K sampling (1–100, step 1)
  6. `frequency_penalty` — Frequency penalty (−2 to 2, step 0.01)
  7. `presence_penalty` — Presence penalty (−2 to 2, step 0.01)
- **Streaming responses** — Real-time token streaming with thinking block rendering (separate collapsible section for reasoning models)
- **Message controls** — Copy, delete, regenerate, stop generating
- **Model detail pill** — Inline model info with parameter display
- **Auto-resize textarea** — Expands as you type, shift+enter for newline
- **Conversation context** — Full multi-turn conversation with the model

### Chat API

Chat uses the admin API at `/api/chat/chat` (admin-authenticated proxy to the provider). This bypasses virtual key requirements — the admin token is used for authentication.

## Conversation Mode

Watch two models talk to each other. A unique feature for observing model behavior, testing consistency, or just entertainment.

### How It Works

1. Select **Model A** and **Model B** (can be the same or different models)
2. Optionally set different system prompts/personas for each model
3. Configure generation parameters for each model independently
4. Enter a **starter prompt** using a free-text `<textarea>` (not a `PromptPicker`)
5. Set **rounds** (1–50, each round = both models respond once)
6. Set **delay** (0–5000ms pause between turns)
7. Click **Start**

### Conversation Flow

```
Round 1:
  User Prompt → Model A (generates response)
  Model A Response → Model B (as input)
  Model B Response → Model A (as input for round 2)

Round 2:
  Model A Response → Model B
  Model B Response → Model A

... continues for N rounds
```

### Controls

- **Start** — Begin the conversation
- **Continue** — Resume after pausing (adds more rounds)
- **Pause** — Stop after the current turn completes
- **Reset** — Clear the conversation and start over
- **Collapsible config** — Hide/show the configuration panel to maximize chat space

### Persistence

Conversation state can be persisted to `localStorage` (toggle in Settings → Chat). This saves:

- Selected models (A and B)
- System prompts / personas (per model)
- Generation parameters (per model)
- Message history
- Round configuration

## Arena Mode

Compare models with structured evaluation. Arena has **two sub-modes**: **Competition** and **Compare**.

### Competition Mode (Bracket Tournament)

Run a bracket-style tournament between two groups of models:

1. Select models for the bracket (up to 16 models)
2. Choose an **arena prompt** (preset or custom) via `PromptPicker`
3. Set generation parameters (global for all matchups, or per-slot via `ParamEditorModal`)
4. Click **Run Arena**

The system generates bracket matchups between pairs. After each matchup, you **vote** for the better response. The tournament auto-advances through rounds — winners proceed to the next bracket round.

**Built-in Arena Prompts:**
- **Dilemma** — A locked room with a single impossible choice
- **Lore** — A reluctant deity faces a new religion
- **Hook** — An impossible-to-stop novel opening paragraph
- **Blueprint** — Design a pointless but indispensable app
- **Spiral** — Define "almost" without using synonyms

### Compare Mode (Grid Comparison)

Multi-model comparison without voting:

1. Select one or more models (up to 3)
2. Enter any prompt (preset or custom)
3. See all responses in a **grid layout** (not side-by-side) — responsive columns: 1 on mobile, 2 on medium, 3 on wide screens
4. Compare metrics (duration, tokens, chars/second)

> **Note:** Compare mode uses a `grid` CSS layout (`grid-cols-1 md:grid-cols-2 xl:grid-cols-3`), not a simple side-by-side split.

### Arena Features

- **Model detail panel** — Click model pills to see full model info (context, pricing, capabilities)
- **Thinking blocks** — Rendered separately from main response in a collapsible section
- **Markdown rendering** — Full markdown support in responses
- **Copy / retry** — Per-response actions
- **Per-model generation parameters** — Each slot in a matchup can have individual `GenerationParams` (temperature, top_p, etc.) via the `ParamEditorModal`
- **Personas in Arena** — Both Competition and Compare modes support persona selection via `PersonaPicker`, allowing you to give each model a different system prompt
- **Auto-advance** — Automatic progression through bracket tournament rounds (built-in behavior, not a user-facing toggle)
- **Persist state** — Save arena configuration and history to `localStorage`

### Arena History Modal

When Arena History is enabled (Settings → Arena History), past session results are stored in `localStorage` and reviewable via the **Arena History Modal** (`ArenaHistoryModal`). This provides:

- Filterable history view (Competition, Compare, or All)
- Per-entry detail expansion with model info and responses
- **Restore** — Reload a past session configuration
- **Delete** — Remove individual entries
- **Clear All** — Purge all arena history

### Arena History Privacy

When Arena History is enabled (Settings → Arena History), session results are stored in your browser's `localStorage`:

- **Model-generated responses** are stored locally for review
- **Preset prompts and personas** are saved by reference only (e.g., "Dilemma preset")
- **Custom user-entered text is never logged** — only the fact that a custom prompt was used is recorded
- History data never leaves your browser and can be cleared from Settings

### Arena API

Arena uses `/api/chat/arena` (admin-authenticated proxy with model duplication for multi-model comparison).