import { describe, expect, it, vi } from "vitest";
import { providerFromModelID } from "../../../utils/model";
import { staggerByProvider } from "../../../utils/stagger";
import type { BracketRound, Matchup } from "../types";
import {
	collectSlots,
	initMatchupResponses,
	nextBracketSize,
	staggerAndDispatch,
} from "../utils";

vi.mock("../../../utils/stagger", () => ({
	staggerByProvider: vi.fn(),
}));

vi.mock("../../../utils/model", () => ({
	providerFromModelID: vi.fn(),
}));

describe("nextBracketSize", () => {
	it("returns 2 for count <= 2", () => {
		expect(nextBracketSize(0)).toBe(2);
		expect(nextBracketSize(1)).toBe(2);
		expect(nextBracketSize(2)).toBe(2);
	});

	it("returns 4 for count 3 or 4", () => {
		expect(nextBracketSize(3)).toBe(4);
		expect(nextBracketSize(4)).toBe(4);
	});

	it("returns 8 for count 5 to 8", () => {
		expect(nextBracketSize(5)).toBe(8);
		expect(nextBracketSize(6)).toBe(8);
		expect(nextBracketSize(7)).toBe(8);
		expect(nextBracketSize(8)).toBe(8);
	});
});

describe("initMatchupResponses", () => {
	it("initializes responseA for slotA", () => {
		const now = 1234567890;
		const matchup: Matchup = {
			slotA: {
				modelId: "model-1",
				personaId: null,
				personaPrompt: "Test",
				params: { temperature: 0.7 },
			},
			slotB: null,
			responseA: null,
			responseB: null,
			vote: null,
		};

		const initFn = initMatchupResponses(now);
		const result = initFn(matchup);

		expect(result.responseA).toEqual({
			model: "model-1",
			rawContent: "",
			content: "",
			thinkingContent: "",
			startTimeMs: now,
			done: false,
			error: null,
			metrics: null,
		});
		expect(result.responseB).toBeNull();
	});

	it("initializes responseB for slotB", () => {
		const now = 1234567890;
		const matchup: Matchup = {
			slotA: null,
			slotB: {
				modelId: "model-2",
				personaId: "p-1",
				personaPrompt: "Prompt",
			},
			responseA: null,
			responseB: null,
			vote: null,
		};

		const initFn = initMatchupResponses(now);
		const result = initFn(matchup);

		expect(result.responseA).toBeNull();
		expect(result.responseB).toEqual({
			model: "model-2",
			rawContent: "",
			content: "",
			thinkingContent: "",
			startTimeMs: now,
			done: false,
			error: null,
			metrics: null,
		});
	});

	it("initializes both responses when both slots exist", () => {
		const now = 1234567890;
		const matchup: Matchup = {
			slotA: {
				modelId: "model-a",
				personaId: null,
				personaPrompt: "",
				params: {},
			},
			slotB: {
				modelId: "model-b",
				personaId: null,
				personaPrompt: "",
				params: {},
			},
			responseA: null,
			responseB: null,
			vote: null,
		};

		const initFn = initMatchupResponses(now);
		const result = initFn(matchup);

		expect(result.responseA).not.toBeNull();
		expect(result.responseB).not.toBeNull();
		expect(result.responseA?.model).toBe("model-a");
		expect(result.responseB?.model).toBe("model-b");
		expect(result.responseA?.startTimeMs).toBe(now);
		expect(result.responseB?.startTimeMs).toBe(now);
	});

	it("preserves existing matchup properties", () => {
		const now = 1234567890;
		const matchup: Matchup = {
			slotA: {
				modelId: "model-1",
				personaId: null,
				personaPrompt: "",
				params: {},
			},
			slotB: null,
			responseA: null,
			responseB: null,
			vote: null,
		};

		const initFn = initMatchupResponses(now);
		const result = initFn(matchup);

		expect(result.slotA).toEqual(matchup.slotA);
		expect(result.slotB).toBeNull();
		expect(result.vote).toBeNull();
	});
});

describe("collectSlots", () => {
	it("collects slots from single matchup with both slots", () => {
		const round: BracketRound = {
			matchups: [
				{
					slotA: {
						modelId: "model-a",
						personaId: "p1",
						personaPrompt: "Prompt A",
						params: { temperature: 0.7 },
					},
					slotB: {
						modelId: "model-b",
						personaId: "p2",
						personaPrompt: "Prompt B",
						params: { max_tokens: 100 },
					},
					responseA: null,
					responseB: null,
					vote: null,
				},
			],
		};

		const slots = collectSlots(round);

		expect(slots).toHaveLength(2);
		expect(slots[0]).toEqual({
			modelId: "model-a",
			personaPrompt: "Prompt A",
			slotKey: "A",
			matchupIdx: 0,
			params: { temperature: 0.7 },
		});
		expect(slots[1]).toEqual({
			modelId: "model-b",
			personaPrompt: "Prompt B",
			slotKey: "B",
			matchupIdx: 0,
			params: { max_tokens: 100 },
		});
	});

	it("collects slots from multiple matchups", () => {
		const round: BracketRound = {
			matchups: [
				{
					slotA: {
						modelId: "m1",
						personaId: null,
						personaPrompt: "",
						params: {},
					},
					slotB: {
						modelId: "m2",
						personaId: null,
						personaPrompt: "",
						params: {},
					},
					responseA: null,
					responseB: null,
					vote: null,
				},
				{
					slotA: {
						modelId: "m3",
						personaId: null,
						personaPrompt: "",
						params: {},
					},
					slotB: null,
					responseA: null,
					responseB: null,
					vote: null,
				},
			],
		};

		const slots = collectSlots(round);

		expect(slots).toHaveLength(3);
		expect(slots[0].modelId).toBe("m1");
		expect(slots[0].slotKey).toBe("A");
		expect(slots[0].matchupIdx).toBe(0);
		expect(slots[1].modelId).toBe("m2");
		expect(slots[1].slotKey).toBe("B");
		expect(slots[1].matchupIdx).toBe(0);
		expect(slots[2].modelId).toBe("m3");
		expect(slots[2].slotKey).toBe("A");
		expect(slots[2].matchupIdx).toBe(1);
	});

	it("handles round with only slotA", () => {
		const round: BracketRound = {
			matchups: [
				{
					slotA: {
						modelId: "solo",
						personaId: null,
						personaPrompt: "",
						params: {},
					},
					slotB: null,
					responseA: null,
					responseB: null,
					vote: null,
				},
			],
		};

		const slots = collectSlots(round);

		expect(slots).toHaveLength(1);
		expect(slots[0].slotKey).toBe("A");
	});

	it("handles empty round", () => {
		const round: BracketRound = { matchups: [] };

		const slots = collectSlots(round);

		expect(slots).toHaveLength(0);
	});
});

describe("staggerAndDispatch", () => {
	it("staggeres slots by provider and dispatches with delay", () => {
		const slots = [
			{
				modelId: "OpenAI/gpt-4",
				personaPrompt: "",
				slotKey: "A" as const,
				matchupIdx: 0,
			},
			{
				modelId: "Anthropic/claude-3",
				personaPrompt: "",
				slotKey: "B" as const,
				matchupIdx: 0,
			},
			{
				modelId: "OpenAI/gpt-3.5",
				personaPrompt: "",
				slotKey: "A" as const,
				matchupIdx: 1,
			},
		];

		const knownProviders = ["OpenAI", "Anthropic"];
		const dispatch = vi.fn();

		const staggeredSlots = [
			{ item: slots[0], delayMs: 0 },
			{ item: slots[1], delayMs: 300 },
			{ item: slots[2], delayMs: 0 },
		];

		vi.mocked(staggerByProvider).mockReturnValue(staggeredSlots);
		vi.mocked(providerFromModelID).mockImplementation((modelId) => {
			if (modelId.startsWith("OpenAI")) return "OpenAI";
			if (modelId.startsWith("Anthropic")) return "Anthropic";
			return "Unknown";
		});

		vi.useFakeTimers();

		staggerAndDispatch(slots, knownProviders, dispatch);

		expect(dispatch).toHaveBeenCalledTimes(2);
		expect(dispatch).toHaveBeenCalledWith(slots[0]);
		expect(dispatch).toHaveBeenCalledWith(slots[2]);

		vi.advanceTimersByTime(300);
		expect(dispatch).toHaveBeenCalledTimes(3);
		expect(dispatch).toHaveBeenCalledWith(slots[1]);

		vi.useRealTimers();
	});

	it("dispatches immediately when delay is 0", () => {
		const slots = [
			{
				modelId: "model-1",
				personaPrompt: "",
				slotKey: "A" as const,
				matchupIdx: 0,
			},
		];
		const dispatch = vi.fn();

		vi.mocked(staggerByProvider).mockReturnValue([
			{ item: { ...slots[0] }, delayMs: 0 },
		]);

		staggerAndDispatch(slots, [], dispatch);

		expect(dispatch).toHaveBeenCalledTimes(1);
		expect(dispatch).toHaveBeenCalledWith(slots[0]);
	});

	it("handles empty slots array", () => {
		const dispatch = vi.fn();

		vi.mocked(staggerByProvider).mockReturnValue([]);

		staggerAndDispatch([], [], dispatch);

		expect(dispatch).not.toHaveBeenCalled();
	});
});
