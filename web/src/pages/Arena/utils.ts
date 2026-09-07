import type { GenerationParams } from "../../api/types";
import { providerFromModelID } from "../../utils/model";
import { staggerByProvider } from "../../utils/stagger";
import type { ArenaResponse, BracketRound, Matchup } from "./types";

/** The bracket sizes a competition can run: a power of two up to eight. */
export const BRACKET_SIZES = [2, 4, 8];

/** Returns the smallest valid bracket size (2, 4, or 8) that fits `count` models. */
export function nextBracketSize(count: number): number {
	return BRACKET_SIZES.find((n) => count <= n) ?? 8;
}

/** The matchup response field a slot writes into. */
export const RESP_KEY = { A: "responseA", B: "responseB" } as const;
/** The matchup slot field a slot writes into. */
export const SLOT_KEY = { A: "slotA", B: "slotB" } as const;

/** A fresh, empty response for `model`, with `patch` applied over the defaults. */
export function newArenaResponse(
	model: string,
	now: number,
	patch?: Partial<ArenaResponse>,
): ArenaResponse {
	return {
		model,
		rawContent: "",
		content: "",
		thinkingContent: "",
		startTimeMs: now,
		done: false,
		error: null,
		metrics: null,
		...patch,
	};
}

/** Replaces a slot's response inside an immer draft, if the matchup exists. */
export function patchSlotResponse(
	draft: BracketRound[],
	roundIdx: number,
	matchupIdx: number,
	slotKey: "A" | "B",
	patch: Partial<ArenaResponse> | null,
): void {
	const mu = draft[roundIdx]?.matchups[matchupIdx];
	if (!mu) return;
	const key = RESP_KEY[slotKey];
	mu[key] = patch === null ? null : ({ ...mu[key], ...patch } as ArenaResponse);
}

/** Empties both the slot and its response, so the grid offers a swap picker. */
export function clearSlot(
	draft: BracketRound[],
	roundIdx: number,
	matchupIdx: number,
	slotKey: "A" | "B",
): void {
	const mu = draft[roundIdx]?.matchups[matchupIdx];
	if (!mu) return;
	mu[SLOT_KEY[slotKey]] = null;
	mu[RESP_KEY[slotKey]] = null;
}

/**
 * Every model id already on the board in this round, except the slot being
 * swapped, so the swap picker cannot offer a duplicate.
 */
export function usedModelIds(
	round: BracketRound,
	exceptMatchup: number,
	exceptSlot: "A" | "B",
): string[] {
	const ids: string[] = [];
	round.matchups.forEach((m, mi) => {
		for (const key of ["A", "B"] as const) {
			if (mi === exceptMatchup && key === exceptSlot) continue;
			const slot = m[SLOT_KEY[key]];
			if (slot) ids.push(slot.modelId);
		}
	});
	return ids;
}

/**
 * Initialize matchup response objects with empty ArenaResponse.
 * Returns a function suitable for use with Array.map().
 */
export function initMatchupResponses(now: number): (mu: Matchup) => Matchup {
	return (mu: Matchup) => ({
		...mu,
		responseA: mu.slotA ? newArenaResponse(mu.slotA.modelId, now) : null,
		responseB: mu.slotB ? newArenaResponse(mu.slotB.modelId, now) : null,
	});
}

/** One slot's streaming assignment: which model answers where. */
export interface SlotDispatch {
	modelId: string;
	personaPrompt: string;
	slotKey: "A" | "B";
	matchupIdx: number;
	params?: GenerationParams;
}

/**
 * Collect all slots from a round's matchups for streaming.
 */
export function collectSlots(round: BracketRound): SlotDispatch[] {
	const slots: SlotDispatch[] = [];
	round.matchups.forEach((mu, mi) => {
		for (const slotKey of ["A", "B"] as const) {
			const slot = mu[SLOT_KEY[slotKey]];
			if (!slot) continue;
			slots.push({
				modelId: slot.modelId,
				personaPrompt: slot.personaPrompt,
				slotKey,
				matchupIdx: mi,
				params: slot.params,
			});
		}
	});
	return slots;
}

/**
 * Stagger slots by provider and dispatch with optional delay. Returns the ids
 * of the timers still pending, so a stop or an unmount can cancel the slots
 * whose turn has not come yet instead of letting them start a stream into a
 * run that is already over.
 */
export function staggerAndDispatch(
	slots: SlotDispatch[],
	knownProviders: string[],
	dispatch: (slot: SlotDispatch) => void,
): ReturnType<typeof setTimeout>[] {
	const staggered = staggerByProvider(
		slots,
		(s) => providerFromModelID(s.modelId, knownProviders),
		300,
	);
	const timers: ReturnType<typeof setTimeout>[] = [];
	for (const { item, delayMs } of staggered) {
		if (delayMs > 0) {
			timers.push(setTimeout(() => dispatch(item), delayMs));
		} else {
			dispatch(item);
		}
	}
	return timers;
}
