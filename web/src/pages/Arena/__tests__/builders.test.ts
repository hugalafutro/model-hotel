import { describe, expect, it } from "vitest";
import type { GenerationParams } from "../../../api/types";
import {
	advanceWinners,
	buildCompareRound,
	buildInitialRounds,
	getPreviewPairs,
	roundWinner,
} from "../builders";
import type { BracketRound, Matchup, MatchupSlot } from "../types";

const mkSlot = (modelId: string): MatchupSlot => ({
	modelId,
	personaId: null,
	personaPrompt: "",
});

const mkMatchup = (
	a: string | null,
	b: string | null,
	vote: Matchup["vote"] = null,
): Matchup => ({
	slotA: a ? mkSlot(a) : null,
	slotB: b ? mkSlot(b) : null,
	responseA: null,
	responseB: null,
	vote,
});

describe("buildCompareRound", () => {
	const modelParams: Record<string, GenerationParams> = {
		"model-1": { temperature: 0.7 },
		"model-2": { temperature: 0.5, max_tokens: 100 },
	};

	it("builds compare round with two models", () => {
		const modelIds = ["model-1", "model-2"];
		const rounds = buildCompareRound(
			modelIds,
			"persona-1",
			"Test prompt",
			modelParams,
		);

		expect(rounds).toHaveLength(1);
		expect(rounds[0].matchups).toHaveLength(2);

		expect(rounds[0].matchups[0].slotA).toEqual({
			modelId: "model-1",
			personaId: "persona-1",
			personaPrompt: "Test prompt",
			params: { temperature: 0.7 },
		});
		expect(rounds[0].matchups[0].slotB).toBeNull();
		expect(rounds[0].matchups[0].responseA).toBeNull();
		expect(rounds[0].matchups[0].responseB).toBeNull();
		expect(rounds[0].matchups[0].vote).toBeNull();
	});

	it("builds compare round with default persona values", () => {
		const modelIds = ["model-1"];
		const rounds = buildCompareRound(modelIds, null, "", modelParams);

		expect(rounds[0].matchups[0].slotA).toEqual({
			modelId: "model-1",
			personaId: null,
			personaPrompt: "",
			params: { temperature: 0.7 },
		});
	});

	it("builds compare round with single model", () => {
		const rounds = buildCompareRound(["solo-model"], null, "", modelParams);

		expect(rounds).toHaveLength(1);
		expect(rounds[0].matchups).toHaveLength(1);
		expect(rounds[0].matchups[0].slotA?.modelId).toBe("solo-model");
	});

	it("builds compare round with empty model list", () => {
		const rounds = buildCompareRound([], null, "", {});

		expect(rounds).toHaveLength(1);
		expect(rounds[0].matchups).toHaveLength(0);
	});

	it("applies different params per model", () => {
		const rounds = buildCompareRound(
			["model-1", "model-2"],
			null,
			"",
			modelParams,
		);

		expect(rounds[0].matchups[0].slotA?.params).toEqual({ temperature: 0.7 });
		expect(rounds[0].matchups[1].slotA?.params).toEqual({
			temperature: 0.5,
			max_tokens: 100,
		});
	});
});

describe("buildInitialRounds", () => {
	const modelParams: Record<string, GenerationParams> = {
		"model-1": { temperature: 0.7 },
		"model-2": { temperature: 0.5 },
		"model-3": { max_tokens: 200 },
		"model-4": { top_p: 0.9 },
	};

	it("builds single round for 2 models", () => {
		const rounds = buildInitialRounds(["model-1", "model-2"], modelParams);

		expect(rounds).toHaveLength(1);
		expect(rounds[0].matchups).toHaveLength(1);
		expect(rounds[0].matchups[0].slotA?.modelId).toBe("model-1");
		expect(rounds[0].matchups[0].slotB?.modelId).toBe("model-2");
		expect(rounds[0].matchups[0].slotA?.personaId).toBeNull();
		expect(rounds[0].matchups[0].slotA?.personaPrompt).toBe("");
	});

	it("builds two rounds for 4 models", () => {
		const rounds = buildInitialRounds(
			["model-1", "model-2", "model-3", "model-4"],
			modelParams,
		);

		expect(rounds).toHaveLength(2);
		expect(rounds[0].matchups).toHaveLength(2);
		expect(rounds[1].matchups).toHaveLength(1);

		expect(rounds[0].matchups[0].slotA?.modelId).toBe("model-1");
		expect(rounds[0].matchups[0].slotB?.modelId).toBe("model-2");
		expect(rounds[0].matchups[1].slotA?.modelId).toBe("model-3");
		expect(rounds[0].matchups[1].slotB?.modelId).toBe("model-4");

		expect(rounds[1].matchups[0].slotA).toBeNull();
		expect(rounds[1].matchups[0].slotB).toBeNull();
	});

	it("builds three rounds for 8 models", () => {
		const models = Array.from({ length: 8 }, (_, i) => `model-${i + 1}`);
		const params = Object.fromEntries(
			models.map((m) => [m, { temperature: 0.7 }]),
		);
		const rounds = buildInitialRounds(models, params);

		expect(rounds).toHaveLength(3);
		expect(rounds[0].matchups).toHaveLength(4);
		expect(rounds[1].matchups).toHaveLength(2);
		expect(rounds[2].matchups).toHaveLength(1);
	});

	it("applies correct params to each slot", () => {
		const rounds = buildInitialRounds(["model-1", "model-2"], modelParams);

		expect(rounds[0].matchups[0].slotA?.params).toEqual({ temperature: 0.7 });
		expect(rounds[0].matchups[0].slotB?.params).toEqual({ temperature: 0.5 });
	});

	it("handles empty model list", () => {
		const rounds = buildInitialRounds([], {});

		expect(rounds).toHaveLength(1);
		expect(rounds[0].matchups).toHaveLength(0);
	});
});

describe("getPreviewPairs", () => {
	it("returns pairs for 2 models", () => {
		const pairs = getPreviewPairs(["model-1", "model-2"]);
		expect(pairs).toEqual([{ a: "model-1", b: "model-2" }]);
	});

	it("returns pairs for 4 models", () => {
		const pairs = getPreviewPairs(["model-1", "model-2", "model-3", "model-4"]);
		expect(pairs).toEqual([
			{ a: "model-1", b: "model-2" },
			{ a: "model-3", b: "model-4" },
		]);
	});

	it("pads to next bracket size for 3 models", () => {
		const pairs = getPreviewPairs(["model-1", "model-2", "model-3"]);
		expect(pairs).toEqual([
			{ a: "model-1", b: "model-2" },
			{ a: "model-3", b: "" },
		]);
	});

	it("pads to next bracket size for 5 models (to 8)", () => {
		const pairs = getPreviewPairs([
			"model-1",
			"model-2",
			"model-3",
			"model-4",
			"model-5",
		]);
		expect(pairs).not.toBeNull();
		// biome-ignore lint/style/noNonNullAssertion: test assertion
		expect(pairs!).toHaveLength(4);
		expect(pairs?.[0]).toEqual({ a: "model-1", b: "model-2" });
		expect(pairs?.[1]).toEqual({ a: "model-3", b: "model-4" });
		expect(pairs?.[2]).toEqual({ a: "model-5", b: "" });
		expect(pairs?.[3]).toEqual({ a: "", b: "" });
	});

	it("handles single model", () => {
		const pairs = getPreviewPairs(["solo"]);
		expect(pairs).toEqual([{ a: "solo", b: "" }]);
	});

	it("handles empty array", () => {
		const pairs = getPreviewPairs([]);
		expect(pairs).toEqual([{ a: "", b: "" }]);
	});
});

describe("advanceWinners", () => {
	it("pairs the voted winners into the next round", () => {
		const rounds: BracketRound[] = [
			{
				matchups: [mkMatchup("m1", "m2", "A"), mkMatchup("m3", "m4", "B")],
			},
			{ matchups: [mkMatchup(null, null)] },
		];

		advanceWinners(rounds, 0);

		expect(rounds[1].matchups[0].slotA?.modelId).toBe("m1");
		expect(rounds[1].matchups[0].slotB?.modelId).toBe("m4");
		expect(rounds[1].matchups[0].responseA).toBeNull();
		expect(rounds[1].matchups[0].vote).toBeNull();
	});

	it("leaves the board alone when there is no next round", () => {
		const rounds: BracketRound[] = [{ matchups: [mkMatchup("m1", "m2", "A")] }];
		expect(() => advanceWinners(rounds, 0)).not.toThrow();
		expect(rounds).toHaveLength(1);
	});
});

describe("roundWinner", () => {
	it("returns the model the final matchup was voted for", () => {
		expect(roundWinner({ matchups: [mkMatchup("m1", "m2", "A")] })).toBe("m1");
		expect(roundWinner({ matchups: [mkMatchup("m1", "m2", "B")] })).toBe("m2");
	});

	it("returns undefined for an empty round", () => {
		expect(roundWinner({ matchups: [] })).toBeUndefined();
	});
});
