import type { GenerationParams } from "../../api/types";
import { providerFromModelID } from "../../utils/model";
import { staggerByProvider } from "../../utils/stagger";
import type { BracketRound, Matchup } from "./types";

/** Returns the smallest valid bracket size (2, 4, or 8) that fits `count` models. */
export function nextBracketSize(count: number): number {
	return count <= 2 ? 2 : count <= 4 ? 4 : 8;
}

/**
 * Initialize matchup response objects with empty ArenaResponse.
 * Returns a function suitable for use with Array.map().
 */
export function initMatchupResponses(now: number): (mu: Matchup) => Matchup {
	return (mu: Matchup) => ({
		...mu,
		responseA: mu.slotA
			? {
					model: mu.slotA.modelId,
					rawContent: "",
					content: "",
					thinkingContent: "",
					startTimeMs: now,
					done: false,
					error: null,
					metrics: null,
				}
			: null,
		responseB: mu.slotB
			? {
					model: mu.slotB.modelId,
					rawContent: "",
					content: "",
					thinkingContent: "",
					startTimeMs: now,
					done: false,
					error: null,
					metrics: null,
				}
			: null,
	});
}

/**
 * Collect all slots from a round's matchups for streaming.
 */
export function collectSlots(round: BracketRound): Array<{
	modelId: string;
	personaPrompt: string;
	slotKey: "A" | "B";
	matchupIdx: number;
	params?: GenerationParams;
}> {
	const slots: Array<{
		modelId: string;
		personaPrompt: string;
		slotKey: "A" | "B";
		matchupIdx: number;
		params?: GenerationParams;
	}> = [];
	for (let mi = 0; mi < round.matchups.length; mi++) {
		const mu = round.matchups[mi];
		if (mu.slotA) {
			slots.push({
				modelId: mu.slotA.modelId,
				personaPrompt: mu.slotA.personaPrompt,
				slotKey: "A",
				matchupIdx: mi,
				params: mu.slotA.params,
			});
		}
		if (mu.slotB) {
			slots.push({
				modelId: mu.slotB.modelId,
				personaPrompt: mu.slotB.personaPrompt,
				slotKey: "B",
				matchupIdx: mi,
				params: mu.slotB.params,
			});
		}
	}
	return slots;
}

/**
 * Stagger slots by provider and dispatch with optional delay.
 */
export function staggerAndDispatch(
	slots: Array<{
		modelId: string;
		personaPrompt: string;
		slotKey: "A" | "B";
		matchupIdx: number;
		params?: GenerationParams;
	}>,
	knownProviders: string[],
	dispatch: (slot: (typeof slots)[number]) => void,
) {
	const staggered = staggerByProvider(
		slots,
		(s) => providerFromModelID(s.modelId, knownProviders),
		300,
	);
	for (const { item, delayMs } of staggered) {
		if (delayMs > 0) {
			setTimeout(() => dispatch(item), delayMs);
		} else {
			dispatch(item);
		}
	}
}
