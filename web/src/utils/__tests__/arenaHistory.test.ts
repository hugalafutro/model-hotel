import { beforeEach, describe, expect, it, vi } from "vitest";
import {
	type ArenaHistoryEntry,
	clearArenaHistory,
	deleteArenaHistoryEntry,
	generateHistoryId,
	getArenaHistory,
	getArenaHistoryCount,
	getArenaHistoryEnabled,
	getArenaHistoryLimit,
	type HistoryMatchup,
	type HistoryMatchupSlot,
	type HistoryResponse,
	saveArenaHistory,
	saveCompareToHistory,
	saveCompetitionToHistory,
	setArenaHistoryEnabled,
	setArenaHistoryLimit,
} from "../arenaHistory";

describe("arenaHistory", () => {
	beforeEach(() => {
		localStorage.clear();
	});

	describe("generateHistoryId", () => {
		it("generates unique IDs", () => {
			const id1 = generateHistoryId();
			const id2 = generateHistoryId();
			expect(id1).not.toBe(id2);
		});

		it("includes timestamp prefix", () => {
			const id = generateHistoryId();
			// ID format: timestamp-alphanumeric
			expect(id).toMatch(/^\d+-[a-z0-9]+$/);
		});

		it("generates IDs with correct format", () => {
			const id = generateHistoryId();
			// Format: timestamp-randomstring
			const parts = id.split("-");
			expect(parts).toHaveLength(2);
			expect(parts[0]).toMatch(/^\d+$/);
			expect(parts[1]).toMatch(/^[a-z0-9]+$/);
		});
	});

	describe("getArenaHistoryEnabled / setArenaHistoryEnabled", () => {
		it("returns false by default when not set", () => {
			expect(getArenaHistoryEnabled()).toBe(false);
		});

		it("returns true after enabling", () => {
			setArenaHistoryEnabled(true);
			expect(getArenaHistoryEnabled()).toBe(true);
		});

		it("returns false after disabling", () => {
			setArenaHistoryEnabled(true);
			setArenaHistoryEnabled(false);
			expect(getArenaHistoryEnabled()).toBe(false);
		});

		it("clears history when disabled", () => {
			const entry: ArenaHistoryEntry = {
				id: "test-1",
				timestamp: Date.now(),
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: ["model-a", "model-b"],
				compareResponses: [],
				completed: true,
			};
			saveArenaHistory(entry);
			expect(getArenaHistoryCount()).toBe(1);

			setArenaHistoryEnabled(false);
			expect(getArenaHistoryCount()).toBe(0);
		});

		it("handles localStorage errors gracefully", () => {
			// Replace localStorage with a mock that throws on setItem.
			// Object.defineProperty and vi.spyOn don't work on jsdom's
			// non-configurable localStorage in CI.
			const realStorage = globalThis.localStorage;
			const store: Record<string, string> = {};
			vi.stubGlobal("localStorage", {
				getItem: (key: string) => store[key] ?? null,
				setItem: () => {
					throw new Error("Storage error");
				},
				removeItem: (key: string) => {
					delete store[key];
				},
				clear: () => {
					for (const k of Object.keys(store)) delete store[k];
				},
				get length() {
					return Object.keys(store).length;
				},
				key: (i: number) => Object.keys(store)[i] ?? null,
			});

			expect(() => setArenaHistoryEnabled(true)).not.toThrow();
			// When setItem fails, the value is not persisted
			expect(getArenaHistoryEnabled()).toBe(false);

			vi.stubGlobal("localStorage", realStorage);
		});
	});

	describe("getArenaHistoryLimit / setArenaHistoryLimit", () => {
		it("returns default limit of 25 when not set", () => {
			expect(getArenaHistoryLimit()).toBe(25);
		});

		it("returns custom limit after setting", () => {
			setArenaHistoryLimit(50);
			expect(getArenaHistoryLimit()).toBe(50);
		});

		it("returns default for invalid values (negative)", () => {
			setArenaHistoryLimit(-10);
			expect(getArenaHistoryLimit()).toBe(25);
		});

		it("returns default for invalid values (zero)", () => {
			setArenaHistoryLimit(0);
			expect(getArenaHistoryLimit()).toBe(25);
		});

		it("returns default for NaN values", () => {
			localStorage.setItem("arenaHistoryLimit", "not-a-number");
			expect(getArenaHistoryLimit()).toBe(25);
		});

		it("handles localStorage errors gracefully", () => {
			const spy = vi
				.spyOn(Storage.prototype, "setItem")
				.mockImplementation(() => {
					throw new Error("Storage error");
				});

			expect(() => setArenaHistoryLimit(100)).not.toThrow();

			spy.mockRestore();
		});
	});

	describe("getArenaHistory / saveArenaHistory", () => {
		it("returns empty array when no history exists", () => {
			expect(getArenaHistory()).toEqual([]);
		});

		it("returns empty array for invalid JSON", () => {
			localStorage.setItem("arenaMatchHistory", "invalid-json");
			expect(getArenaHistory()).toEqual([]);
		});

		it("returns empty array for non-array data", () => {
			localStorage.setItem("arenaMatchHistory", JSON.stringify({ foo: "bar" }));
			expect(getArenaHistory()).toEqual([]);
		});

		it("saves and retrieves a single entry", () => {
			const entry: ArenaHistoryEntry = {
				id: "test-1",
				timestamp: Date.now(),
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: ["model-a", "model-b"],
				compareResponses: [],
				completed: true,
			};

			saveArenaHistory(entry);

			const history = getArenaHistory();
			expect(history).toHaveLength(1);
			expect(history[0].id).toBe("test-1");
			expect(history[0].mode).toBe("compare");
		});

		it("prepends new entries (most recent first)", () => {
			const entry1: ArenaHistoryEntry = {
				id: "test-1",
				timestamp: 1000,
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			};
			const entry2: ArenaHistoryEntry = {
				id: "test-2",
				timestamp: 2000,
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			};

			saveArenaHistory(entry1);
			saveArenaHistory(entry2);

			const history = getArenaHistory();
			expect(history).toHaveLength(2);
			expect(history[0].id).toBe("test-2"); // Most recent first
			expect(history[1].id).toBe("test-1");
		});

		it("enforces the history limit", () => {
			setArenaHistoryLimit(3);

			for (let i = 1; i <= 5; i++) {
				saveArenaHistory({
					id: `test-${i}`,
					timestamp: i * 1000,
					mode: "compare",
					promptPresetId: null,
					comparePersonaId: null,
					compareModels: [],
					compareResponses: [],
					completed: true,
				});
			}

			const history = getArenaHistory();
			expect(history).toHaveLength(3);
			expect(history[0].id).toBe("test-5");
			expect(history[1].id).toBe("test-4");
			expect(history[2].id).toBe("test-3");
		});

		it("handles localStorage errors gracefully", () => {
			const spy = vi
				.spyOn(Storage.prototype, "setItem")
				.mockImplementation(() => {
					throw new Error("Storage error");
				});

			expect(() =>
				saveArenaHistory({
					id: "test-1",
					timestamp: Date.now(),
					mode: "compare",
					promptPresetId: null,
					comparePersonaId: null,
					compareModels: [],
					compareResponses: [],
					completed: true,
				}),
			).not.toThrow();

			spy.mockRestore();
		});
	});

	describe("getArenaHistoryCount", () => {
		it("returns 0 when no history exists", () => {
			expect(getArenaHistoryCount()).toBe(0);
		});

		it("returns correct count after saving entries", () => {
			saveArenaHistory({
				id: "test-1",
				timestamp: Date.now(),
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			});
			expect(getArenaHistoryCount()).toBe(1);

			saveArenaHistory({
				id: "test-2",
				timestamp: Date.now(),
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			});
			expect(getArenaHistoryCount()).toBe(2);
		});
	});

	describe("deleteArenaHistoryEntry", () => {
		it("removes entry by ID", () => {
			saveArenaHistory({
				id: "test-1",
				timestamp: 1000,
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			});
			saveArenaHistory({
				id: "test-2",
				timestamp: 2000,
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			});

			deleteArenaHistoryEntry("test-1");

			const history = getArenaHistory();
			expect(history).toHaveLength(1);
			expect(history[0].id).toBe("test-2");
		});

		it("does nothing if ID not found", () => {
			saveArenaHistory({
				id: "test-1",
				timestamp: Date.now(),
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			});

			deleteArenaHistoryEntry("non-existent");

			expect(getArenaHistoryCount()).toBe(1);
		});

		it("handles localStorage errors gracefully", () => {
			const spy = vi
				.spyOn(Storage.prototype, "setItem")
				.mockImplementation(() => {
					throw new Error("Storage error");
				});

			expect(() => deleteArenaHistoryEntry("test-1")).not.toThrow();

			spy.mockRestore();
		});
	});

	describe("clearArenaHistory", () => {
		it("removes all history entries", () => {
			saveArenaHistory({
				id: "test-1",
				timestamp: Date.now(),
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			});
			saveArenaHistory({
				id: "test-2",
				timestamp: Date.now(),
				mode: "compare",
				promptPresetId: null,
				comparePersonaId: null,
				compareModels: [],
				compareResponses: [],
				completed: true,
			});

			clearArenaHistory();

			expect(getArenaHistoryCount()).toBe(0);
		});

		it("handles localStorage errors gracefully", () => {
			const spy = vi
				.spyOn(Storage.prototype, "removeItem")
				.mockImplementation(() => {
					throw new Error("Storage error");
				});

			expect(() => clearArenaHistory()).not.toThrow();

			spy.mockRestore();
		});
	});

	describe("saveCompetitionToHistory", () => {
		it("does not save when history is disabled", () => {
			setArenaHistoryEnabled(false);

			saveCompetitionToHistory({
				rounds: [],
				winner: "model-a",
				promptPresetId: null,
				comparePersonaId: null,
			});

			expect(getArenaHistoryCount()).toBe(0);
		});

		it("saves competition bracket with winner", () => {
			setArenaHistoryEnabled(true);

			const rounds = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: "merlin",
								personaPrompt: "You are Merlin",
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
							},
							responseA: {
								model: "model-a",
								content: "Response A",
								thinkingContent: "",
								error: null,
								metrics: {
									tokensPerSecond: 10,
									durationMs: 1000,
									promptTokens: 50,
									completionTokens: 100,
								},
							},
							responseB: {
								model: "model-b",
								content: "Response B",
								thinkingContent: "",
								error: null,
								metrics: null,
							},
							vote: "A" as const,
						},
					],
				},
			];

			saveCompetitionToHistory({
				rounds,
				winner: "model-a",
				promptPresetId: "dilemma",
				comparePersonaId: "merlin",
			});

			const history = getArenaHistory();
			expect(history).toHaveLength(1);

			const entry = history[0];
			expect(entry.mode).toBe("competition");
			expect(entry.winner).toBe("model-a");
			expect(entry.promptPresetId).toBe("dilemma");
			expect(entry.comparePersonaId).toBe("merlin");
			expect(entry.rounds).toHaveLength(1);
			expect(entry.rounds?.[0].matchups).toHaveLength(1);
			expect(entry.completed).toBe(true);

			// Verify privacy: custom persona prompts are stripped
			const round = entry.rounds?.[0];
			expect(round).toBeDefined();
			const matchup = round.matchups[0];
			expect(matchup.slotA?.personaId).toBe("merlin"); // preset preserved
			expect(matchup.slotB?.personaId).toBe(null); // null preserved
		});

		it("strips non-preset persona IDs", () => {
			setArenaHistoryEnabled(true);

			saveCompetitionToHistory({
				rounds: [
					{
						matchups: [
							{
								slotA: {
									modelId: "model-a",
									personaId: "custom-persona",
									personaPrompt: "Custom prompt text",
								},
								slotB: null,
								responseA: null,
								responseB: null,
								vote: null,
							},
						],
					},
				],
				winner: null,
				promptPresetId: null,
				comparePersonaId: null,
			});

			const entry = getArenaHistory()[0];
			const round2 = entry.rounds?.[0];
			expect(round2).toBeDefined();
			expect(round2.matchups[0].slotA?.personaId).toBe(null); // Non-preset stripped
		});

		it("preserves all prompt preset IDs from ARENA_PROMPTS", () => {
			setArenaHistoryEnabled(true);

			// "cipher" was missing from the old hardcoded set
			saveCompetitionToHistory({
				rounds: [],
				winner: null,
				promptPresetId: "cipher",
				comparePersonaId: null,
			});

			const entry = getArenaHistory()[0];
			expect(entry.promptPresetId).toBe("cipher");
		});

		it("strips non-preset prompt IDs", () => {
			setArenaHistoryEnabled(true);

			saveCompetitionToHistory({
				rounds: [],
				winner: null,
				promptPresetId: "custom-prompt-id",
				comparePersonaId: null,
			});

			const entry = getArenaHistory()[0];
			expect(entry.promptPresetId).toBe(null); // Non-preset stripped
		});
	});

	describe("saveCompareToHistory", () => {
		it("does not save when history is disabled", () => {
			setArenaHistoryEnabled(false);

			saveCompareToHistory({
				models: ["model-a", "model-b"],
				responses: [],
				promptPresetId: null,
				comparePersonaId: null,
			});

			expect(getArenaHistoryCount()).toBe(0);
		});

		it("saves compare mode results", () => {
			setArenaHistoryEnabled(true);

			const responses = [
				{
					model: "model-a",
					content: "Response A",
					thinkingContent: "",
					error: null,
					metrics: {
						tokensPerSecond: 10,
						durationMs: 1000,
						promptTokens: 50,
						completionTokens: 100,
					},
				},
				{
					model: "model-b",
					content: "Response B",
					thinkingContent: "",
					error: null,
					metrics: null,
				},
			];

			saveCompareToHistory({
				models: ["model-a", "model-b"],
				responses,
				promptPresetId: "lore",
				comparePersonaId: "sarge",
			});

			const history = getArenaHistory();
			expect(history).toHaveLength(1);

			const entry = history[0];
			expect(entry.mode).toBe("compare");
			expect(entry.compareModels).toEqual(["model-a", "model-b"]);
			expect(entry.compareResponses).toHaveLength(2);
			expect(entry.promptPresetId).toBe("lore");
			expect(entry.comparePersonaId).toBe("sarge");
			expect(entry.completed).toBe(true);
		});

		it("filters out null responses", () => {
			setArenaHistoryEnabled(true);

			saveCompareToHistory({
				models: ["model-a", "model-b"],
				responses: [
					{
						model: "model-a",
						content: "Response A",
						thinkingContent: "",
						error: null,
						metrics: null,
					},
					null as unknown as {
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
					},
				],
				promptPresetId: null,
				comparePersonaId: null,
			});

			const entry = getArenaHistory()[0];
			expect(entry.compareResponses).toHaveLength(1);
			expect(entry.compareResponses?.[0].modelId).toBe("model-a");
		});
	});

	describe("HistoryResponse type", () => {
		it("supports full response structure", () => {
			const response: HistoryResponse = {
				modelId: "test-model",
				content: "Test content",
				thinkingContent: "Test thinking",
				error: null,
				metrics: {
					tokensPerSecond: 15.5,
					durationMs: 2000,
					promptTokens: 100,
					completionTokens: 200,
				},
				params: { temperature: 0.7, max_tokens: 1000 },
			};

			expect(response.modelId).toBe("test-model");
			expect(response.metrics?.tokensPerSecond).toBe(15.5);
			expect(response.params?.temperature).toBe(0.7);
		});

		it("supports null metrics", () => {
			const response: HistoryResponse = {
				modelId: "test-model",
				content: "Test content",
				thinkingContent: "",
				error: "Some error",
				metrics: null,
			};

			expect(response.error).toBe("Some error");
			expect(response.metrics).toBe(null);
		});
	});

	describe("HistoryMatchupSlot type", () => {
		it("supports preset persona ID", () => {
			const slot: HistoryMatchupSlot = {
				modelId: "model-a",
				personaId: "merlin",
				params: { temperature: 0.8 },
			};

			expect(slot.personaId).toBe("merlin");
			expect(slot.params?.temperature).toBe(0.8);
		});

		it("supports null persona ID", () => {
			const slot: HistoryMatchupSlot = {
				modelId: "model-b",
				personaId: null,
			};

			expect(slot.personaId).toBe(null);
		});
	});

	describe("HistoryMatchup type", () => {
		it("supports full matchup structure", () => {
			const matchup: HistoryMatchup = {
				slotA: { modelId: "model-a", personaId: "merlin" },
				slotB: { modelId: "model-b", personaId: null },
				responseA: {
					modelId: "model-a",
					content: "Response A",
					thinkingContent: "",
					error: null,
					metrics: null,
				},
				responseB: null,
				vote: "A",
			};

			expect(matchup.slotA?.modelId).toBe("model-a");
			expect(matchup.vote).toBe("A");
		});

		it("supports null slots and responses", () => {
			const matchup: HistoryMatchup = {
				slotA: null,
				slotB: null,
				responseA: null,
				responseB: null,
				vote: null,
			};

			expect(matchup.slotA).toBe(null);
			expect(matchup.vote).toBe(null);
		});
	});

	describe("ArenaHistoryEntry type", () => {
		it("supports competition mode entry", () => {
			const entry: ArenaHistoryEntry = {
				id: "comp-1",
				timestamp: Date.now(),
				mode: "competition",
				promptPresetId: "dilemma",
				comparePersonaId: "merlin",
				rounds: [
					{
						matchups: [
							{
								slotA: { modelId: "model-a", personaId: "merlin" },
								slotB: null,
								responseA: null,
								responseB: null,
								vote: null,
							},
						],
					},
				],
				winner: "model-a",
				completed: true,
			};

			expect(entry.mode).toBe("competition");
			expect(entry.winner).toBe("model-a");
			expect(entry.rounds).toBeDefined();
		});

		it("supports compare mode entry", () => {
			const entry: ArenaHistoryEntry = {
				id: "compare-1",
				timestamp: Date.now(),
				mode: "compare",
				promptPresetId: "lore",
				comparePersonaId: "sarge",
				compareModels: ["model-a", "model-b"],
				compareResponses: [
					{
						modelId: "model-a",
						content: "Response A",
						thinkingContent: "",
						error: null,
						metrics: null,
					},
				],
				completed: true,
			};

			expect(entry.mode).toBe("compare");
			expect(entry.compareModels).toEqual(["model-a", "model-b"]);
		});
	});
});
