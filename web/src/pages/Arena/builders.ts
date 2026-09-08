import type { GenerationParams } from "../../api/types";
import type { BracketRound, Matchup, MatchupSlot } from "./types";
import { nextBracketSize } from "./utils";

export function buildCompareRound(
	modelIds: string[],
	personaId: string | null = null,
	personaPrompt: string = "",
	modelParams: Record<string, GenerationParams>,
): BracketRound[] {
	return [
		{
			matchups: modelIds.map((id) => ({
				slotA: {
					modelId: id,
					personaId,
					personaPrompt,
					params: modelParams[id],
				} as MatchupSlot,
				slotB: null,
				responseA: null,
				responseB: null,
				vote: null,
			})),
		},
	];
}

export function buildInitialRounds(
	models: string[],
	modelParams: Record<string, GenerationParams>,
): BracketRound[] {
	const makeSlot = (id: string): MatchupSlot => ({
		modelId: id,
		personaId: null,
		personaPrompt: "",
		params: modelParams[id],
	});

	const emptyMatchup = (): Matchup => ({
		slotA: null,
		slotB: null,
		responseA: null,
		responseB: null,
		vote: null,
	});

	const numRounds = Math.log2(models.length);
	const firstRoundMatchups: Matchup[] = [];
	for (let i = 0; i < models.length; i += 2) {
		firstRoundMatchups.push({
			slotA: makeSlot(models[i]),
			slotB: makeSlot(models[i + 1]),
			responseA: null,
			responseB: null,
			vote: null,
		});
	}

	const bracketRounds: BracketRound[] = [{ matchups: firstRoundMatchups }];

	for (let r = 1; r < numRounds; r++) {
		const matchupCount = models.length / 2 ** (r + 1);
		bracketRounds.push({
			matchups: Array.from({ length: matchupCount }, () => emptyMatchup()),
		});
	}

	return bracketRounds;
}

/**
 * Writes the winners of `roundIdx` into the next round's slots, pairing them
 * up in board order. A round with no successor is left alone.
 */
export function advanceWinners(draft: BracketRound[], roundIdx: number): void {
	const next = draft[roundIdx + 1];
	if (!next) return;
	const winners = draft[roundIdx].matchups.map((m: Matchup) =>
		m.vote === "A" ? m.slotA : m.slotB,
	);
	for (let i = 0; i < winners.length; i += 2) {
		next.matchups[i / 2] = {
			slotA: winners[i] ? { ...(winners[i] as MatchupSlot) } : null,
			slotB: winners[i + 1] ? { ...(winners[i + 1] as MatchupSlot) } : null,
			responseA: null,
			responseB: null,
			vote: null,
		};
	}
}

/** The model the final round's single matchup was voted for, if it was voted. */
export function roundWinner(round: BracketRound): string | undefined {
	const mu = round.matchups[0];
	return mu?.vote === "A" ? mu.slotA?.modelId : mu?.slotB?.modelId;
}

export function getPreviewPairs(
	bracketModels: string[],
): { a: string; b: string }[] {
	const target = nextBracketSize(bracketModels.length);
	const items = [...bracketModels];
	while (items.length < target) items.push("");
	const pairs: { a: string; b: string }[] = [];
	for (let i = 0; i < items.length; i += 2) {
		pairs.push({ a: items[i], b: items[i + 1] ?? "" });
	}
	return pairs;
}
