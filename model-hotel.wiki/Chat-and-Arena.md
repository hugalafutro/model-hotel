# Chat & Arena

The dashboard includes interactive tools for testing and comparing models: **Chat**, **Conversation**, and **Arena**.

## Chat Mode

The standard chat interface for interactive model testing with a single model.

### Features

- **Model picker** — Select any discovered model with search and filtering. Collapsible sections for provider grouping.
- **System personas** — Choose from preset characters or enter a custom system prompt via `PersonaPicker`:
  - **Configurable** — Custom system prompt, all parameters adjustable
  - **Code Helper** — Optimized for programming tasks
  - **Concise** — Brief, to-the-point responses
  - **Savant** — Detailed, thorough explanations
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

Chat also supports a **Conversation mode** where two models (Model A and Model B) take turns responding to each other, creating a dialogue.

### How It Works

1. You provide an initial prompt
2. Model A responds to the prompt
3. Model B responds to Model A's output
4. They alternate, with each model seeing the other's responses relabeled as `user` role messages
5. The conversation continues for `maxTurns` iterations (configurable, each turn = one model response)

### Configuration

- **Max Turns** — Number of back-and-forth exchanges (each model's response counts as one turn). The total messages = `maxTurns × 2`
- **Turn Delay** — Configurable delay (ms) between model turns
- **Model A / Model B** — Each can be any enabled model from the picker

### State Persistence

- Conversation messages are saved to localStorage on every update
- If an error occurs, the state becomes `error` and the original prompt is restored to the input field
- Users can click "Resume" to continue from the last successful turn

### Message Construction

Messages from the opposite model are re-labeled with `user` role so each model only sees a single alternating dialogue. If a system persona is set, it's prepended as the first message.

> 📸 **Screenshot needed:** Chat page — showing the model picker, persona selector, parameter panel, and a streaming response with thinking block expanded.

> 📸 **Screenshot needed:** Chat page — Conversation mode active, showing two models alternating with Model A and Model B labels.

---

## Thinking Blocks

When models return reasoning/thinking content, the UI detects and renders it as collapsible blocks:

### Detectable Formats

1. **Fence format:** `<<\n...thinking content...\n>>` — content between `<<` and `>>` delimiters
2. **XML tag format:** `<thinking>...</thinking>` (also matches `<thought>`, `<start_thought>`, ` thinking`)

### Rendering

- Thinking blocks render as a collapsible section with a 🧠 brain icon
- Default state: collapsed
- When open: shows thinking content in a scrollable container (max-height: 60vh)
- During streaming: text pulses with the accent color animation

> 📸 **Screenshot needed:** Thinking block — collapsed state showing the brain icon, and expanded state showing reasoning content.

---

## Arena Mode

Compare models with structured evaluation. Arena has **two sub-modes**: **Competition** and **Compare**.

### Competition Mode (Bracket Tournament)

Run a bracket-style tournament between two groups of models:

1. Select models for the bracket (up to 16 models)
2. Choose an **arena prompt** (preset or custom) via `PromptPicker`
3. Set generation parameters (global for all matchups, or per-slot via `ParamEditorModal`)
4. Click **Run Arena**

The system generates bracket matchups between pairs. After each matchup, you **vote** for the better response. The tournament auto-advances through rounds — winners proceed to the next bracket round. Eventually crowns a champion.

Competition mode has its own prompt, active prompt ID, and persona settings stored in localStorage (separate from Compare mode).

**Built-in Arena Prompts:**
- **Dilemma** — A locked room with a single impossible choice
- **Lore** — A reluctant deity faces a new religion
- **Hook** — An impossible-to-stop novel opening paragraph
- **Blueprint** — Design a pointless but indispensable app
- **Spiral** — Define "almost" without using synonyms

> 📸 **Screenshot needed:** Arena page — Competition mode showing a tournament bracket with voting buttons.

### Compare Mode (Grid Comparison)

Multi-model comparison without voting:

1. Select one or more models (up to 3)
2. Enter any prompt (preset or custom)
3. See all responses in a **grid layout** (not side-by-side) — responsive columns: 1 on mobile, 2 on medium, 3 on wide screens
4. Compare metrics (duration, tokens, chars/second)

No tournament or elimination — just side-by-side comparison. Has separate localStorage keys for prompt and persona (independent from Competition mode).

> **Note:** Compare mode uses a `grid` CSS layout (`grid-cols-1 md:grid-cols-2 xl:grid-cols-3`), not a simple side-by-side split.

> 📸 **Screenshot needed:** Arena page — Compare mode showing a grid of multiple model responses side by side.

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