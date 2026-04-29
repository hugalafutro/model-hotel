import type { GenerationParams } from "../api/types";

// Storage keys
const ARENA_HISTORY_KEY = "arenaMatchHistory";
const ARENA_HISTORY_ENABLED_KEY = "arenaHistoryEnabled";
const ARENA_HISTORY_LIMIT_KEY = "arenaHistoryLimit";

// ---------------------------------------------------------------------------
// Serializable history entry types (simplified from Arena.tsx internal types)
// These are designed for privacy: no custom user prompts are stored
// ---------------------------------------------------------------------------

export interface HistoryResponse {
	modelId: string;
	content: string;
	thinkingContent: string;
	error: string | null;
	metrics: {
		charsPerSecond: number | null;
		durationMs: number;
		promptTokens: number;
		completionTokens: number;
	} | null;
	params?: Record<string, unknown>;
}

export interface HistoryMatchupSlot {
	modelId: string;
	// Only store preset persona references, never the actual custom text
	personaId: string | null; // preset ID like "merlin", "sarge" etc. — null means custom/none
	// personaPrompt is deliberately OMITTED for privacy — it could contain user-written text
	params?: Record<string, unknown>;
}

export interface HistoryMatchup {
	slotA: HistoryMatchupSlot | null;
	slotB: HistoryMatchupSlot | null;
	responseA: HistoryResponse | null;
	responseB: HistoryResponse | null;
	vote: "A" | "B" | null;
}

export interface HistoryBracketRound {
	matchups: HistoryMatchup[];
}

export type HistoryMode = "competition" | "compare";

export interface ArenaHistoryEntry {
	id: string;
	timestamp: number;
	mode: HistoryMode;
	// For preset prompts: store the preset ID only, never the user's custom prompt text
	promptPresetId: string | null; // e.g. "dilemma", "lore", "hook" — null means custom prompt (not stored)
	// personaId for compare mode global persona (preset only)
	comparePersonaId: string | null;
	// Competition bracket results
	rounds?: HistoryBracketRound[];
	winner?: string;
	// Compare mode flat results
	compareModels?: string[];
	compareResponses?: HistoryResponse[];
	completed: boolean;
}

// ---------------------------------------------------------------------------
// Internal Arena types (mirrored from Arena.tsx to avoid circular imports)
// These are only used as input types for the save*ToHistory helpers.
// ---------------------------------------------------------------------------

/** Mirrors ArenaResponse from Arena.tsx */
interface ArenaResponseInput {
	model: string;
	content: string;
	thinkingContent: string;
	error: string | null;
	metrics: {
		charsPerSecond: number | null;
		durationMs: number;
		promptTokens: number;
		completionTokens: number;
	} | null;
	params?: GenerationParams;
}

/** Mirrors MatchupSlot from Arena.tsx */
interface MatchupSlotInput {
	modelId: string;
	personaId: string | null;
	personaPrompt: string; // deliberately ignored during mapping
	params?: GenerationParams;
}

/** Mirrors Matchup from Arena.tsx */
interface MatchupInput {
	slotA: MatchupSlotInput | null;
	slotB: MatchupSlotInput | null;
	responseA: ArenaResponseInput | null;
	responseB: ArenaResponseInput | null;
	vote: "A" | "B" | null;
}

/** Mirrors BracketRound from Arena.tsx */
interface BracketRoundInput {
	matchups: MatchupInput[];
}

// ---------------------------------------------------------------------------
// Known preset IDs — used to determine whether a personaId is a preset or custom
// ---------------------------------------------------------------------------

const KNOWN_PERSONA_IDS = new Set([
	"merlin",
	"madame-vex",
	"sarge",
	"auntie-wei",
	"grimm",
	"kairos",
	"phreak",
	"roux",
	"unit-734",
	"bramble",
]);

const KNOWN_PROMPT_IDS = new Set([
	"dilemma",
	"lore",
	"hook",
	"blueprint",
	"spiral",
	"trolley",
	"algorithm",
	"paradox",
	"integral",
	"contract",
]);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function isPresetPersonaId(id: string | null): boolean {
	if (id === null) return false;
	return KNOWN_PERSONA_IDS.has(id);
}

function isPresetPromptId(id: string | null): boolean {
	if (id === null) return false;
	return KNOWN_PROMPT_IDS.has(id);
}

/** Map an Arena.tsx MatchupSlot to a history-safe HistoryMatchupSlot, stripping user text */
function toHistorySlot(
	slot: MatchupSlotInput | null,
): HistoryMatchupSlot | null {
	if (!slot) return null;
	return {
		modelId: slot.modelId,
		personaId: isPresetPersonaId(slot.personaId) ? slot.personaId : null,
		...(slot.params && Object.keys(slot.params).length > 0
			? { params: { ...slot.params } }
			: undefined),
	};
}

/** Map an Arena.tsx ArenaResponse to a history-safe HistoryResponse */
function toHistoryResponse(
	resp: ArenaResponseInput | null,
): HistoryResponse | null {
	if (!resp) return null;
	return {
		modelId: resp.model,
		content: resp.content,
		thinkingContent: resp.thinkingContent,
		error: resp.error,
		metrics: resp.metrics
			? {
					charsPerSecond: resp.metrics.charsPerSecond,
					durationMs: resp.metrics.durationMs,
					promptTokens: resp.metrics.promptTokens,
					completionTokens: resp.metrics.completionTokens,
				}
			: null,
		...(resp.params && Object.keys(resp.params).length > 0
			? { params: { ...resp.params } }
			: undefined),
	};
}

/** Map a full Arena.tsx Matchup to a history-safe HistoryMatchup */
function toHistoryMatchup(mu: MatchupInput): HistoryMatchup {
	return {
		slotA: toHistorySlot(mu.slotA),
		slotB: toHistorySlot(mu.slotB),
		responseA: toHistoryResponse(mu.responseA),
		responseB: toHistoryResponse(mu.responseB),
		vote: mu.vote,
	};
}

// ---------------------------------------------------------------------------
// localStorage CRUD
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Exported API
// ---------------------------------------------------------------------------

export function getArenaHistoryEnabled(): boolean {
	try {
		return localStorage.getItem(ARENA_HISTORY_ENABLED_KEY) === "true";
	} catch {
		return false;
	}
}

export function setArenaHistoryEnabled(v: boolean): void {
	try {
		localStorage.setItem(ARENA_HISTORY_ENABLED_KEY, String(v));
		if (!v) {
			// When disabled, clear stored history data
			localStorage.removeItem(ARENA_HISTORY_KEY);
		}
	} catch {
		// Silently ignore
	}
}

export function getArenaHistoryLimit(): number {
	try {
		const raw = localStorage.getItem(ARENA_HISTORY_LIMIT_KEY);
		if (raw !== null) {
			const parsed = parseInt(raw, 10);
			if (!Number.isNaN(parsed) && parsed > 0) return parsed;
		}
	} catch {
		// Fall through to default
	}
	return 25;
}

export function setArenaHistoryLimit(n: number): void {
	try {
		localStorage.setItem(ARENA_HISTORY_LIMIT_KEY, String(n));
	} catch {
		// Silently ignore
	}
}

export function getArenaHistory(): ArenaHistoryEntry[] {
	try {
		const raw = localStorage.getItem(ARENA_HISTORY_KEY);
		if (!raw) return [];
		const parsed = JSON.parse(raw);
		if (!Array.isArray(parsed)) return [];
		return parsed as ArenaHistoryEntry[];
	} catch {
		return [];
	}
}

export function saveArenaHistory(entry: ArenaHistoryEntry): void {
	try {
		const history = getArenaHistory();
		// Prepend new entry (most recent first)
		history.unshift(entry);
		// Enforce FIFO limit
		const limit = getArenaHistoryLimit();
		if (history.length > limit) {
			history.length = limit;
		}
		localStorage.setItem(ARENA_HISTORY_KEY, JSON.stringify(history));
	} catch {
		// Silently ignore — history is non-critical
	}
}

export function deleteArenaHistoryEntry(id: string): void {
	try {
		const history = getArenaHistory();
		const filtered = history.filter((entry) => entry.id !== id);
		localStorage.setItem(ARENA_HISTORY_KEY, JSON.stringify(filtered));
	} catch {
		// Silently ignore
	}
}

export function clearArenaHistory(): void {
	try {
		localStorage.removeItem(ARENA_HISTORY_KEY);
	} catch {
		// Silently ignore
	}
}

export function getArenaHistoryCount(): number {
	return getArenaHistory().length;
}

export function generateHistoryId(): string {
	return `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
}

// ---------------------------------------------------------------------------
// High-level save helpers
// ---------------------------------------------------------------------------

export interface SaveCompetitionToHistoryArgs {
	rounds: BracketRoundInput[];
	winner: string | null;
	promptPresetId: string | null;
	comparePersonaId: string | null;
}

/**
 * Save a completed competition bracket to arena history.
 * Maps Arena.tsx internal types to serializable history types,
 * stripping out any user-entered text (custom prompts, custom persona prompts).
 */
export function saveCompetitionToHistory(
	args: SaveCompetitionToHistoryArgs,
): void {
	if (!getArenaHistoryEnabled()) return;

	const { rounds, winner, promptPresetId, comparePersonaId } = args;

	const entry: ArenaHistoryEntry = {
		id: generateHistoryId(),
		timestamp: Date.now(),
		mode: "competition",
		// Only store the preset prompt ID — never the custom text
		promptPresetId: isPresetPromptId(promptPresetId) ? promptPresetId : null,
		// Only store the preset persona ID
		comparePersonaId: isPresetPersonaId(comparePersonaId)
			? comparePersonaId
			: null,
		rounds: rounds.map((round) => ({
			matchups: round.matchups.map((mu) => toHistoryMatchup(mu)),
		})),
		winner: winner ?? undefined,
		completed: true,
	};

	saveArenaHistory(entry);
}

export interface SaveCompareToHistoryArgs {
	models: string[];
	responses: ArenaResponseInput[];
	promptPresetId: string | null;
	comparePersonaId: string | null;
}

/**
 * Save a completed compare-mode run to arena history.
 * Creates a flat list of HistoryResponse entries, one per model.
 */
export function saveCompareToHistory(args: SaveCompareToHistoryArgs): void {
	if (!getArenaHistoryEnabled()) return;

	const { models, responses, promptPresetId, comparePersonaId } = args;

	const entry: ArenaHistoryEntry = {
		id: generateHistoryId(),
		timestamp: Date.now(),
		mode: "compare",
		// Only store the preset prompt ID — never the custom text
		promptPresetId: isPresetPromptId(promptPresetId) ? promptPresetId : null,
		// Only store the preset persona ID
		comparePersonaId: isPresetPersonaId(comparePersonaId)
			? comparePersonaId
			: null,
		compareModels: models,
		compareResponses: responses
			.map((r) => toHistoryResponse(r))
			.filter((r): r is HistoryResponse => r !== null),
		completed: true,
	};

	saveArenaHistory(entry);
}
