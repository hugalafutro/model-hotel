import type { GenerationParams } from "../../api/types";
import type { ArenaSubMode } from "../../context/SidebarModeContext";
import { providerFromModelID } from "../../utils/model";
import { staggerByProvider } from "../../utils/stagger";
import type {
	ArenaResponse,
	BracketPhase,
	BracketRound,
	Matchup,
} from "./types";

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
 * The key a slot's in-flight stream is tracked by (abort controller and the
 * running set). Keyed by position, not model id: a blind swap may put the
 * opponent's model into the other slot, and two streams of one model must
 * stop and settle independently.
 */
export function streamKey(
	roundIdx: number,
	matchupIdx: number,
	slotKey: "A" | "B",
): string {
	return `${roundIdx}:${matchupIdx}:${slotKey}`;
}

/**
 * Every model id already on the board in this round, except the slot being
 * swapped, so the swap picker cannot offer a duplicate. In a blind round
 * (competition mode) only voted, revealed matchups are excluded, whatever the
 * picker's own matchup: an excluded model from an unvoted one would name its
 * unseen side (the opponent in a one-matchup round, another matchup's
 * survivor otherwise), so the picker may offer those models instead.
 */
export function usedModelIds(
	round: BracketRound,
	exceptMatchup: number,
	exceptSlot: "A" | "B",
	blind = false,
): string[] {
	const ids: string[] = [];
	round.matchups.forEach((m, mi) => {
		if (blind && m.vote === null) return;
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
 * Stagger slots by provider and dispatch with optional delay. Returns the
 * timers still pending with the slot each one will start, so a stop, an
 * unmount, or a cancel of that one slot can drop it instead of letting it
 * start a stream into a run that is over or a slot that was swapped.
 */
export function staggerAndDispatch(
	slots: SlotDispatch[],
	knownProviders: string[],
	dispatch: (slot: SlotDispatch) => void,
): { slot: SlotDispatch; timer: ReturnType<typeof setTimeout> }[] {
	const staggered = staggerByProvider(
		slots,
		(s) => providerFromModelID(s.modelId, knownProviders),
		300,
	);
	const timers: { slot: SlotDispatch; timer: ReturnType<typeof setTimeout> }[] =
		[];
	for (const { item, delayMs } of staggered) {
		if (delayMs > 0) {
			timers.push({
				slot: item,
				timer: setTimeout(() => dispatch(item), delayMs),
			});
		} else {
			dispatch(item);
		}
	}
	return timers;
}

/** The slots in display order: a flipped matchup shows B on the left. */
export function sideOrder(mu: Pick<Matchup, "flipped">): ("A" | "B")[] {
	return mu.flipped ? ["B", "A"] : ["A", "B"];
}

/**
 * The label a blind card shows in place of its model, by display position
 * ("A" left, "B" right), never by slot: the setup preview and the winner
 * advance both put a known model in slot A, so a slot-keyed label would give
 * the flip away. Undefined when the model may be named: compare mode, setup
 * (personas are assigned by name), a voted matchup, or an errored reply, which
 * the user has to see to swap.
 */
export function blindLabel(
	mu: Matchup,
	slotKey: "A" | "B",
	position: number,
	mode: ArenaSubMode,
	phase: BracketPhase,
): "A" | "B" | undefined {
	if (mode !== "competition" || phase === "setup" || mu.vote !== null)
		return undefined;
	const response = slotKey === "A" ? mu.responseA : mu.responseB;
	if (response?.error) return undefined;
	return position === 0 ? "A" : "B";
}
