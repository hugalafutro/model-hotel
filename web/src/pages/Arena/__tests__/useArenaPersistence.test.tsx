import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BracketRound } from "../types";
import type { ArenaPersistenceState } from "../useArenaPersistence";
import { useArenaPersistence } from "../useArenaPersistence";

// Mock contexts
const mockPersistArena = { current: true };
const mockToast = vi.fn();

vi.mock("../../../context/StorageContext", () => ({
	useStorage: () => ({
		persistArena: mockPersistArena.current,
		setPersistArena: vi.fn(),
		persistChat: false,
		setPersistChat: vi.fn(),
		persistConversation: false,
		setPersistConversation: vi.fn(),
		arenaHistoryEnabled: false,
		setArenaHistoryEnabled: vi.fn(),
		arenaHistoryLimit: 25,
		setArenaHistoryLimit: vi.fn(),
	}),
}));

vi.mock("../../../context/ToastContext", () => ({
	useToast: () => ({ toast: mockToast }),
}));

const mockState: ArenaPersistenceState = {
	arenaMode: "compare",
	compareModels: ["Provider/model-1", "Provider/model-2"],
	bracketModels: ["Provider/model-1"],
	rounds: [],
	currentRound: 1,
	phase: "running",
	arenaCollapsed: false,
	savedPrompt: "Test prompt",
	modelParams: { "Provider/model-1": { temperature: 0.7 } },
};

describe("useArenaPersistence", () => {
	let realStorage: Storage;
	let storedItems: Record<string, string>;

	beforeEach(() => {
		vi.clearAllMocks();
		mockPersistArena.current = true;
		mockToast.mockClear();
		realStorage = globalThis.localStorage;
		storedItems = {};

		// Mock localStorage with quota tracking
		globalThis.localStorage = {
			getItem: (key: string) => storedItems[key] ?? null,
			setItem: (key: string, value: string) => {
				const newSize = JSON.stringify(storedItems).length + value.length;
				// Simulate 5MB quota
				if (newSize > 5 * 1024 * 1024) {
					throw new DOMException("Quota exceeded", "QuotaExceededError");
				}
				storedItems[key] = value;
			},
			removeItem: (key: string) => {
				delete storedItems[key];
			},
			clear: () => {
				for (const k of Object.keys(storedItems)) delete storedItems[k];
			},
			get length() {
				return Object.keys(storedItems).length;
			},
			key: (i: number) => Object.keys(storedItems)[i] ?? null,
		} as Storage;
	});

	afterEach(() => {
		globalThis.localStorage = realStorage;
	});

	it("persists arena state to localStorage when persistArena=true", () => {
		renderHook(() => useArenaPersistence(mockState));

		const stored = localStorage.getItem("arenaState");
		expect(stored).not.toBeNull();

		const parsed = JSON.parse(stored as string);
		expect(parsed.arenaMode).toBe("compare");
		expect(parsed.compareModels).toEqual([
			"Provider/model-1",
			"Provider/model-2",
		]);
		expect(parsed.bracketModels).toEqual(["Provider/model-1"]);
		expect(parsed.currentRound).toBe(1);
		expect(parsed.phase).toBe("running");
		expect(parsed.arenaCollapsed).toBe(false);
		expect(parsed.savedPrompt).toBe("Test prompt");
		expect(parsed.modelParams).toEqual({
			"Provider/model-1": { temperature: 0.7 },
		});
	});

	it("does NOT persist when persistArena=false", () => {
		mockPersistArena.current = false;

		renderHook(() => useArenaPersistence(mockState));

		const stored = localStorage.getItem("arenaState");
		expect(stored).toBeNull();
	});

	it("handles localStorage quota exceeded gracefully", () => {
		// Fill localStorage to trigger quota exceeded
		vi.stubGlobal("localStorage", {
			getItem: (key: string) => storedItems[key] ?? null,
			setItem: () => {
				throw new DOMException("Quota exceeded", "QuotaExceededError");
			},
			removeItem: (key: string) => {
				delete storedItems[key];
			},
			clear: () => {
				for (const k of Object.keys(storedItems)) delete storedItems[k];
			},
			get length() {
				return Object.keys(storedItems).length;
			},
			key: (i: number) => Object.keys(storedItems)[i] ?? null,
		});

		renderHook(() => useArenaPersistence(mockState));

		expect(mockToast).toHaveBeenCalledWith(
			"Storage full - arena state not saved",
			"warning",
		);
	});

	it("warns only once via quotaWarnedRef when multiple state changes occur", () => {
		vi.stubGlobal("localStorage", {
			getItem: (key: string) => storedItems[key] ?? null,
			setItem: () => {
				throw new DOMException("Quota exceeded", "QuotaExceededError");
			},
			removeItem: (key: string) => {
				delete storedItems[key];
			},
			clear: () => {
				for (const k of Object.keys(storedItems)) delete storedItems[k];
			},
			get length() {
				return Object.keys(storedItems).length;
			},
			key: (i: number) => Object.keys(storedItems)[i] ?? null,
		});

		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		// Trigger re-render with new state
		const newState: ArenaPersistenceState = {
			...mockState,
			arenaCollapsed: true,
		};
		rerender({ state: newState });

		// Toast should only be called once despite multiple failures
		expect(mockToast).toHaveBeenCalledTimes(1);
	});

	it("updates localStorage when state changes", () => {
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		const initialStored = localStorage.getItem("arenaState");
		expect(initialStored).not.toBeNull();
		let parsed = JSON.parse(initialStored as string);
		expect(parsed.arenaCollapsed).toBe(false);

		// Change state
		const newState: ArenaPersistenceState = {
			...mockState,
			arenaCollapsed: true,
			currentRound: 2,
		};
		rerender({ state: newState });

		const updatedStored = localStorage.getItem("arenaState");
		parsed = JSON.parse(updatedStored as string);
		expect(parsed.arenaCollapsed).toBe(true);
		expect(parsed.currentRound).toBe(2);
	});

	it("persists all 9 state fields correctly", () => {
		const complexState: ArenaPersistenceState = {
			arenaMode: "competition",
			compareModels: [],
			bracketModels: ["P1/M1", "P2/M2", "P3/M3"],
			rounds: [
				{
					matchups: [
						{
							slotA: {
								modelId: "P1/M1",
								personaId: "persona-1",
								personaPrompt: "Test persona A",
							},
							slotB: {
								modelId: "P2/M2",
								personaId: "persona-2",
								personaPrompt: "Test persona B",
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			] as BracketRound[],
			currentRound: 2,
			phase: "finished",
			arenaCollapsed: true,
			savedPrompt: "Complex test prompt",
			modelParams: {
				"P1/M1": { temperature: 0.5, max_tokens: 2048 },
				"P2/M2": { temperature: 0.8, top_p: 0.9 },
			},
		};

		renderHook(() => useArenaPersistence(complexState));

		const stored = localStorage.getItem("arenaState");
		expect(stored).not.toBeNull();

		const parsed = JSON.parse(stored as string);

		// Verify all 9 fields are present
		expect(parsed).toHaveProperty("arenaMode");
		expect(parsed).toHaveProperty("compareModels");
		expect(parsed).toHaveProperty("bracketModels");
		expect(parsed).toHaveProperty("rounds");
		expect(parsed).toHaveProperty("currentRound");
		expect(parsed).toHaveProperty("phase");
		expect(parsed).toHaveProperty("arenaCollapsed");
		expect(parsed).toHaveProperty("savedPrompt");
		expect(parsed).toHaveProperty("modelParams");

		// Verify values
		expect(parsed.arenaMode).toBe("competition");
		expect(parsed.compareModels).toEqual([]);
		expect(parsed.bracketModels).toHaveLength(3);
		expect(parsed.rounds).toHaveLength(1);
		expect(parsed.currentRound).toBe(2);
		expect(parsed.phase).toBe("finished");
		expect(parsed.arenaCollapsed).toBe(true);
		expect(parsed.savedPrompt).toBe("Complex test prompt");
		expect(parsed.modelParams).toEqual({
			"P1/M1": { temperature: 0.5, max_tokens: 2048 },
			"P2/M2": { temperature: 0.8, top_p: 0.9 },
		});
	});

	it("persists state when arenaMode changes", () => {
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		const newState: ArenaPersistenceState = {
			...mockState,
			arenaMode: "competition",
		};
		rerender({ state: newState });

		const stored = localStorage.getItem("arenaState");
		const parsed = JSON.parse(stored as string);
		expect(parsed.arenaMode).toBe("competition");
	});

	it("persists state when compareModels changes", () => {
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		const newState: ArenaPersistenceState = {
			...mockState,
			compareModels: ["Provider/model-3"],
		};
		rerender({ state: newState });

		const stored = localStorage.getItem("arenaState");
		const parsed = JSON.parse(stored as string);
		expect(parsed.compareModels).toEqual(["Provider/model-3"]);
	});

	it("persists state when bracketModels changes", () => {
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		const newState: ArenaPersistenceState = {
			...mockState,
			bracketModels: ["P1/M1", "P2/M2"],
		};
		rerender({ state: newState });

		const stored = localStorage.getItem("arenaState");
		const parsed = JSON.parse(stored as string);
		expect(parsed.bracketModels).toEqual(["P1/M1", "P2/M2"]);
	});

	it("persists state when rounds changes", () => {
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		const newRounds: BracketRound[] = [
			{
				matchups: [
					{
						slotA: {
							modelId: "M1",
							personaId: null,
							personaPrompt: "",
						},
						slotB: null,
						responseA: null,
						responseB: null,
						vote: null,
					},
				],
			},
		];

		const newState: ArenaPersistenceState = {
			...mockState,
			rounds: newRounds,
		};
		rerender({ state: newState });

		const stored = localStorage.getItem("arenaState");
		const parsed = JSON.parse(stored as string);
		expect(parsed.rounds).toHaveLength(1);
	});

	it("persists state when phase changes", () => {
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		const newState: ArenaPersistenceState = {
			...mockState,
			phase: "voting",
		};
		rerender({ state: newState });

		const stored = localStorage.getItem("arenaState");
		const parsed = JSON.parse(stored as string);
		expect(parsed.phase).toBe("voting");
	});

	it("persists state when savedPrompt changes", () => {
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		const newState: ArenaPersistenceState = {
			...mockState,
			savedPrompt: "New prompt",
		};
		rerender({ state: newState });

		const stored = localStorage.getItem("arenaState");
		const parsed = JSON.parse(stored as string);
		expect(parsed.savedPrompt).toBe("New prompt");
	});

	it("persists state when modelParams changes", () => {
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		const newState: ArenaPersistenceState = {
			...mockState,
			modelParams: {
				"Provider/model-1": { temperature: 0.9, max_tokens: 4096 },
				"Provider/model-2": { temperature: 0.3 },
			},
		};
		rerender({ state: newState });

		const stored = localStorage.getItem("arenaState");
		const parsed = JSON.parse(stored as string);
		expect(parsed.modelParams).toEqual({
			"Provider/model-1": { temperature: 0.9, max_tokens: 4096 },
			"Provider/model-2": { temperature: 0.3 },
		});
	});

	it("does not call toast when localStorage succeeds", () => {
		renderHook(() => useArenaPersistence(mockState));

		expect(mockToast).not.toHaveBeenCalled();
	});

	it("re-persists when persistArena toggles from false to true", () => {
		mockPersistArena.current = false;
		const { rerender } = renderHook(({ state }) => useArenaPersistence(state), {
			initialProps: { state: mockState },
		});

		// Should not persist when false
		expect(localStorage.getItem("arenaState")).toBeNull();

		// Toggle to true
		mockPersistArena.current = true;
		rerender({ state: mockState });

		// Should persist now
		const stored = localStorage.getItem("arenaState");
		expect(stored).not.toBeNull();
	});
});
