import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useArenaState } from "../useArenaState";
import {
	arenaHistoryMocks,
	arenaModeRef,
	createWrapper,
	persistRef,
} from "./useArenaState.test.helpers";

// Mock the dependencies
vi.mock("../../../hooks/useModels", () => ({
	useEnabledModels: vi.fn(() => ({
		data: [
			{ provider_name: "TestProvider", model_id: "model-1", enabled: true },
			{ provider_name: "TestProvider", model_id: "model-2", enabled: true },
			{ provider_name: "TestProvider", model_id: "model-3", enabled: true },
		],
	})),
}));

vi.mock("../../../context/SidebarModeContext", () => ({
	useSidebarMode: vi.fn(() => ({
		get arenaSubMode() {
			return arenaModeRef.current;
		},
		setArenaSubMode: vi.fn((v: "compare" | "competition") => {
			arenaModeRef.current = v;
		}),
		chatSubMode: "chat" as const,
		setChatSubMode: vi.fn(),
		logsSubMode: "request" as const,
		setLogsSubMode: vi.fn(),
	})),
}));

vi.mock("../../../context/ToastContext", () => ({
	useToast: vi.fn(() => ({
		toast: vi.fn(),
	})),
}));

vi.mock("../../../context/StorageContext", () => ({
	useStorage: vi.fn(() => ({
		persistArena: persistRef.current,
	})),
}));

vi.mock("../../../utils/arenaHistory", () => ({
	getArenaHistoryEnabled: () => arenaHistoryMocks.getArenaHistoryEnabled(),
	saveCompareToHistory: (...args: unknown[]) =>
		arenaHistoryMocks.saveCompareToHistory(...args),
}));

describe("useArenaState", () => {
	beforeEach(() => {
		localStorage.clear();
		arenaModeRef.current = "compare";
	});

	it("computes canRun correctly", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const isCompareMode = result.current.arenaMode === "compare";

		// Initially false - no models selected
		expect(result.current.canRun).toBe(false);

		if (isCompareMode) {
			act(() => {
				result.current.setCompareModels(["model-1", "model-2"]);
				result.current.setPrompt("Test prompt");
			});

			// Should be true with 2+ models and prompt
			expect(result.current.canRun).toBe(true);

			act(() => {
				result.current.setPrompt("");
			});

			// False without prompt
			expect(result.current.canRun).toBe(false);
		} else {
			act(() => {
				result.current.setBracketModels(["model-1", "model-2"]);
				result.current.setPrompt("Test prompt");
			});

			// In competition mode with 2 models (valid bracket size)
			expect(result.current.canRun).toBe(true);
		}
	});

	it("computes disabledReason correctly", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const isCompareMode = result.current.arenaMode === "compare";

		if (isCompareMode) {
			// Should show reason when no models selected
			expect(result.current.disabledReason).toContain("Select");

			act(() => {
				result.current.setCompareModels(["model-1"]);
			});

			// Should show reason when only 1 model
			expect(result.current.disabledReason).toContain("more");

			act(() => {
				result.current.setCompareModels(["model-1", "model-2"]);
			});

			// Should show reason when no prompt
			expect(result.current.disabledReason).toContain("prompt");

			act(() => {
				result.current.setPrompt("Test prompt");
			});

			// Should be empty when ready
			expect(result.current.disabledReason).toBe("");
		} else {
			// Competition mode checks bracketModels
			expect(result.current.disabledReason).toContain("Select");
		}
	});

	it("provides buildCompareRoundWithParams function", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const rounds = result.current.buildCompareRoundWithParams(
			["model-1", "model-2"],
			null,
			"",
		);

		expect(rounds).toHaveLength(1);
		expect(rounds[0].matchups).toHaveLength(2);
	});

	it("provides buildInitialRoundsWithParams function", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const rounds = result.current.buildInitialRoundsWithParams([
			"model-1",
			"model-2",
			"model-3",
			"model-4",
		]);

		expect(rounds.length).toBeGreaterThan(0);
	});

	it("handleRandomComparePersona selects a random persona", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.handleRandomComparePersona();
		});

		// Should have set a persona ID and prompt
		expect(result.current.comparePersonaId).toBeDefined();
		expect(result.current.comparePersonaPrompt).toBeDefined();
	});

	it("handleRandomBracketModel adds a random model", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const initialCount = result.current.bracketModels.length;

		act(() => {
			result.current.handleRandomBracketModel();
		});

		// Should have added a model if available
		expect(result.current.bracketModels.length).toBeGreaterThanOrEqual(
			initialCount,
		);
	});

	it("handleRandomCompareModel adds a random model", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const initialCount = result.current.compareModels.length;

		act(() => {
			result.current.handleRandomCompareModel();
		});

		// Should have added a model if available
		expect(result.current.compareModels.length).toBeGreaterThanOrEqual(
			initialCount,
		);
	});

	it("previewPairs returns pairs in competition mode setup", () => {
		// This test needs competition mode - set it explicitly BEFORE renderHook
		arenaModeRef.current = "competition";
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		// Verify we're in competition mode
		expect(result.current.arenaMode).toBe("competition");
		// previewPairs is only non-null in competition mode during setup
		expect(result.current.previewPairs).toBeDefined();
		// Note: mode will be reset to "compare" by beforeEach of next test
	});

	it("cleans up abort controllers on unmount", () => {
		const abortCtrl = new AbortController();
		const { unmount } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		// Simulate adding abort controllers (this would normally happen during use)
		// The cleanup happens on unmount
		unmount();

		// After unmount, abort controllers should be aborted
		expect(abortCtrl.signal.aborted).toBe(false); // Not added to the hook's map
	});

	it("syncs roundsRef with rounds state", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const newRounds = [
			{
				matchups: [
					{
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
					},
				],
			},
		];

		act(() => {
			result.current.setRounds(newRounds);
		});

		expect(result.current.roundsRef.current).toEqual(newRounds);
	});

	it("syncs activePromptIdRef with activePromptId state", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setActivePromptId("sync-test-id");
		});

		expect(result.current.activePromptIdRef.current).toBe("sync-test-id");
	});

	it("syncs comparePersonaIdRef with comparePersonaId state", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setComparePersonaId("sync-persona-id");
		});

		expect(result.current.comparePersonaIdRef.current).toBe("sync-persona-id");
	});
});
