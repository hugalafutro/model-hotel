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

export function getRoundLabel(
	roundIdx: number,
	totalRounds: number,
	arenaMode: string,
): string {
	if (arenaMode === "compare") return "Generation";
	if (totalRounds === 1) return "Match";
	if (roundIdx === totalRounds - 1) return "Final";
	if (roundIdx === totalRounds - 2) return "Semifinals";
	if (roundIdx === totalRounds - 3) return "Quarterfinals";
	return `Round ${roundIdx + 1}`;
}

export function getPreviewPairs(
	bracketModels: string[],
): { a: string; b: string }[] | null {
	const target = nextBracketSize(bracketModels.length);
	const items = [...bracketModels];
	while (items.length < target) items.push("");
	const pairs: { a: string; b: string }[] = [];
	for (let i = 0; i < items.length; i += 2) {
		pairs.push({ a: items[i], b: items[i + 1] ?? "" });
	}
	return pairs;
}
