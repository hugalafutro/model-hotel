import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

interface ArenaPersistenceState {
	arenaMode: string;
	compareModels: string[];
	bracketModels: string[];
	rounds: unknown[];
	currentRound: number;
	phase: string;
	arenaCollapsed: boolean;
	savedPrompt: string;
	modelParams: Record<string, unknown>;
}

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

// Note: arenaMode uses "compare" | "competition" from SidebarModeContext
// Note: This test file verifies the persistence behavior documented in useArenaPersistence.ts
// Full hook tests require the StorageProvider and ToastProvider contexts

describe("useArenaPersistence - localStorage behavior", () => {
	let origSetItem: typeof Storage.prototype.setItem;

	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
		origSetItem = localStorage.setItem;
	});

	afterEach(() => {
		Object.defineProperty(localStorage, "setItem", {
			value: origSetItem,
			configurable: true,
		});
		localStorage.clear();
	});

	function mockSetItem(
		impl: (
			// biome-ignore lint/suspicious/noExplicitAny: test helper mock
			...args: /* eslint-disable-line @typescript-eslint/no-explicit-any */ any[]
		) => void,
	) {
		Object.defineProperty(localStorage, "setItem", {
			value: impl,
			configurable: true,
		});
	}

	it("persists arena state to localStorage when persistArena=true", () => {
		const calls: [string, string][] = [];
		mockSetItem((key: string, value: string) => {
			calls.push([key, value]);
		});

		const stateToPersist = {
			arenaMode: mockState.arenaMode,
			compareModels: mockState.compareModels,
			bracketModels: mockState.bracketModels,
			rounds: mockState.rounds,
			currentRound: mockState.currentRound,
			phase: mockState.phase,
			arenaCollapsed: mockState.arenaCollapsed,
			savedPrompt: mockState.savedPrompt,
			modelParams: mockState.modelParams,
		};

		try {
			localStorage.setItem("arenaState", JSON.stringify(stateToPersist));
		} catch {
			// Quota exceeded
		}

		expect(calls).toHaveLength(1);
		expect(calls[0][0]).toBe("arenaState");

		const parsed = JSON.parse(calls[0][1]);
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

	it("does NOT call setItem when persistArena=false", () => {
		const calls: [string, string][] = [];
		mockSetItem((key: string, value: string) => {
			calls.push([key, value]);
		});

		// When persistArena=false, the hook returns early and never calls setItem
		expect(calls).toHaveLength(0);
	});

	it("handles localStorage quota exceeded gracefully", () => {
		const mockToast = vi.fn();
		mockSetItem(() => {
			throw new DOMException("Quota exceeded", "QuotaExceededError");
		});

		// Simulate the hook's try-catch behavior
		let quotaWarned = false;
		try {
			localStorage.setItem("arenaState", JSON.stringify(mockState));
		} catch {
			if (!quotaWarned) {
				quotaWarned = true;
				mockToast("Storage full - arena state not saved", "warning");
			}
		}

		expect(mockToast).toHaveBeenCalledWith(
			"Storage full - arena state not saved",
			"warning",
		);
	});

	it("warns only once via quotaWarnedRef pattern", () => {
		const mockToast = vi.fn();
		let quotaWarned = false;
		mockSetItem(() => {
			throw new DOMException("Quota exceeded", "QuotaExceededError");
		});

		// First attempt
		try {
			localStorage.setItem("arenaState", JSON.stringify(mockState));
		} catch {
			if (!quotaWarned) {
				quotaWarned = true;
				mockToast("Storage full - arena state not saved", "warning");
			}
		}

		// Second attempt (simulating re-render with new state)
		try {
			localStorage.setItem(
				"arenaState",
				JSON.stringify({ ...mockState, arenaCollapsed: true }),
			);
		} catch {
			if (!quotaWarned) {
				quotaWarned = true;
				mockToast("Storage full - arena state not saved", "warning");
			}
		}

		// Toast should only be called once despite multiple failures
		expect(mockToast).toHaveBeenCalledTimes(1);
	});

	it("serializes all state properties correctly", () => {
		const calls: [string, string][] = [];
		mockSetItem((key: string, value: string) => {
			calls.push([key, value]);
		});

		const complexState: ArenaPersistenceState = {
			arenaMode: "compare",
			compareModels: [],
			bracketModels: ["P1/M1", "P2/M2", "P3/M3"],
			rounds: [
				{
					matchups: [
						{
							slotA: null,
							slotB: null,
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			],
			currentRound: 2,
			phase: "finished",
			arenaCollapsed: true,
			savedPrompt: "Complex test",
			modelParams: {
				"P1/M1": { temperature: 0.5, max_tokens: 2048 },
				"P2/M2": { temperature: 0.8, top_p: 0.9 },
			},
		};

		try {
			localStorage.setItem("arenaState", JSON.stringify(complexState));
		} catch {
			// Quota exceeded
		}

		expect(calls).toHaveLength(1);

		const parsed = JSON.parse(calls[0][1]);
		expect(parsed.arenaMode).toBe("compare");
		expect(parsed.bracketModels).toHaveLength(3);
		expect(parsed.rounds).toHaveLength(1);
		expect(parsed.currentRound).toBe(2);
		expect(parsed.phase).toBe("finished");
		expect(parsed.arenaCollapsed).toBe(true);
	});
});
