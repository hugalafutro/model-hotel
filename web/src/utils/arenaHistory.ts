import type { GenerationParams } from "../api/types";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../data/presets";
import { hasAnyParam } from "./params";

// The three localStorage keys the arena history lives under. Exported because
// StorageContext owns the writes for the enabled flag and the limit, and both
// sides must name the same keys.
export const ARENA_HISTORY_KEY = "arenaMatchHistory";
export const ARENA_HISTORY_ENABLED_KEY = "arenaHistoryEnabled";
export const ARENA_HISTORY_LIMIT_KEY = "arenaHistoryLimit";
// Every localStorage key the arena's persisted setup lives under, cleared as
// one set by both the arena's own reset and StorageContext's "stop persisting"
// branch. The union is deliberate: a reset that leaves the mirrored board in
// `arenaState` brings the cleared setup back on the next mount, and switching
// persistence off has to take the compare persona with it like every other
// stored field.
export const ARENA_STORAGE_KEYS = [
	"arenaState",
	"arenaCompetitionPrompt",
	"arenaComparePrompt",
	"arenaCompetitionActivePromptId",
	"arenaCompareActivePromptId",
	"arenaComparePersonaId",
	"arenaComparePersonaPrompt",
];

/** The stored history cap; an absent, unparsable or non-positive value reads as 25. */
export const DEFAULT_ARENA_HISTORY_LIMIT = 25;
export function parseArenaHistoryLimit(raw: string | null): number {
	const parsed = raw === null ? Number.NaN : Number.parseInt(raw, 10);
	return !Number.isNaN(parsed) && parsed > 0
		? parsed
		: DEFAULT_ARENA_HISTORY_LIMIT;
}

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
		tokensPerSecond: number | null;
		durationMs: number;
		promptTokens: number;
		completionTokens: number;
	} | null;
	params?: Record<string, unknown>;
}

export interface HistoryMatchupSlot {
	modelId: string;
	// Only store preset persona references, never the actual custom text
	personaId: string | null; // preset ID like "merlin", "sarge" etc. - null means custom/none
	// personaPrompt is deliberately OMITTED for privacy - it could contain user-written text
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
	promptPresetId: string | null; // e.g. "dilemma", "lore", "hook" - null means custom prompt (not stored)
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
		tokensPerSecond: number | null;
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
// Known preset IDs - used to determine whether a personaId is a preset or custom
// ---------------------------------------------------------------------------

const KNOWN_PERSONA_IDS = new Set(CHAT_PERSONAS.map((p) => p.id));

const KNOWN_PROMPT_IDS = new Set(ARENA_PROMPTS.map((p) => p.id));

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
		...(slot.params && hasAnyParam(slot.params)
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
		metrics: resp.metrics ? { ...resp.metrics } : null,
		...(resp.params && hasAnyParam(resp.params)
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

export function getArenaHistoryLimit(): number {
	try {
		return parseArenaHistoryLimit(
			localStorage.getItem(ARENA_HISTORY_LIMIT_KEY),
		);
	} catch {
		return DEFAULT_ARENA_HISTORY_LIMIT;
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
		// Silently ignore - history is non-critical
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
		// Only store the preset prompt ID - never the custom text
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
		// Only store the preset prompt ID - never the custom text
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
