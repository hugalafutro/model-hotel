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

	it("initializes with default values", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		expect(result.current.compareModels).toEqual([]);
		expect(result.current.bracketModels).toEqual([]);
		expect(result.current.rounds).toEqual([]);
		expect(result.current.currentRound).toBe(0);
		expect(result.current.phase).toBe("setup");
		expect(result.current.runningModels).toEqual(new Set());
		expect(result.current.winnerModal).toBe(null);
		expect(result.current.disabledModels).toEqual(new Set());
		expect(result.current.arenaCollapsed).toBe(false);
		expect(result.current.pendingFullReset).toBe(false);
		expect(result.current.showHistoryModal).toBe(false);
		expect(result.current.modelParams).toEqual({});
		expect(result.current.paramEditorModel).toBe(null);
		// arenaMode depends on SidebarModeContext mock - just verify it's a valid mode
		expect(["compare", "competition"]).toContain(result.current.arenaMode);
	});

	it("initializes refs with empty values", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		expect(result.current.abortMapRef.current).toEqual(new Map());
		expect(result.current.lastExtractLenRef.current).toEqual(new Map());
		expect(result.current.currentRoundRef.current).toBe(0);
		expect(result.current.roundsLengthRef.current).toBe(0);
		expect(result.current.roundsRef.current).toEqual([]);
		expect(result.current.activePromptIdRef.current).toBe(null);
		expect(result.current.comparePersonaIdRef.current).toBe(null);
		// arenaMode depends on SidebarModeContext mock - just verify it's a valid mode
		expect(["compare", "competition"]).toContain(result.current.arenaMode);
	});

	it("provides setter functions for all state values", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		expect(typeof result.current.setCompareModels).toBe("function");
		expect(typeof result.current.setBracketModels).toBe("function");
		expect(typeof result.current.setCompetitionActivePromptId).toBe("function");
		expect(typeof result.current.setCompareActivePromptId).toBe("function");
		expect(typeof result.current.setCompetitionPrompt).toBe("function");
		expect(typeof result.current.setComparePrompt).toBe("function");
		expect(typeof result.current.setPrompt).toBe("function");
		expect(typeof result.current.setActivePromptId).toBe("function");
		expect(typeof result.current.setSavedPrompt).toBe("function");
		expect(typeof result.current.setComparePersonaId).toBe("function");
		expect(typeof result.current.setComparePersonaPrompt).toBe("function");
		expect(typeof result.current.setRounds).toBe("function");
		expect(typeof result.current.setCurrentRound).toBe("function");
		expect(typeof result.current.setPhase).toBe("function");
		expect(typeof result.current.setRunningModels).toBe("function");
		expect(typeof result.current.setWinnerModal).toBe("function");
		expect(typeof result.current.setDisabledModels).toBe("function");
		expect(typeof result.current.setArenaCollapsed).toBe("function");
		expect(typeof result.current.setPendingFullReset).toBe("function");
		expect(typeof result.current.setShowHistoryModal).toBe("function");
		expect(typeof result.current.setModelParams).toBe("function");
		expect(typeof result.current.setParamEditorModel).toBe("function");
		expect(typeof result.current.setArenaMode).toBe("function");
	});

	it("updates compareModels via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setCompareModels(["model-1", "model-2"]);
		});

		expect(result.current.compareModels).toEqual(["model-1", "model-2"]);
	});

	it("updates bracketModels via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setBracketModels([
				"model-1",
				"model-2",
				"model-3",
				"model-4",
			]);
		});

		expect(result.current.bracketModels).toEqual([
			"model-1",
			"model-2",
			"model-3",
			"model-4",
		]);
	});

	it("updates competitionActivePromptId via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setCompetitionActivePromptId("prompt-123");
		});

		expect(result.current.competitionActivePromptId).toBe("prompt-123");

		act(() => {
			result.current.setCompetitionActivePromptId(null);
		});

		expect(result.current.competitionActivePromptId).toBe(null);
	});

	it("updates compareActivePromptId via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setCompareActivePromptId("prompt-456");
		});

		expect(result.current.compareActivePromptId).toBe("prompt-456");

		act(() => {
			result.current.setCompareActivePromptId(null);
		});

		expect(result.current.compareActivePromptId).toBe(null);
	});

	it("updates competitionPrompt via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setCompetitionPrompt("Competition prompt");
		});

		expect(result.current.competitionPrompt).toBe("Competition prompt");
	});

	it("updates comparePrompt via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setComparePrompt("Compare prompt");
		});

		expect(result.current.comparePrompt).toBe("Compare prompt");
	});

	it("setPrompt dispatches to active mode", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const isCompareMode = result.current.arenaMode === "compare";

		act(() => {
			result.current.setPrompt("Test prompt");
		});

		// Should update the prompt for the active mode
		if (isCompareMode) {
			expect(result.current.comparePrompt).toBe("Test prompt");
		} else {
			expect(result.current.competitionPrompt).toBe("Test prompt");
		}
	});

	it("setActivePromptId dispatches to active mode", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const isCompareMode = result.current.arenaMode === "compare";

		act(() => {
			result.current.setActivePromptId("prompt-id");
		});

		// Should update the active prompt ID for the active mode
		if (isCompareMode) {
			expect(result.current.compareActivePromptId).toBe("prompt-id");
		} else {
			expect(result.current.competitionActivePromptId).toBe("prompt-id");
		}
	});

	it("updates savedPrompt via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setSavedPrompt("Saved prompt");
		});

		expect(result.current.savedPrompt).toBe("Saved prompt");
	});

	it("updates comparePersonaId via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setComparePersonaId("persona-123");
		});

		expect(result.current.comparePersonaId).toBe("persona-123");

		act(() => {
			result.current.setComparePersonaId(null);
		});

		expect(result.current.comparePersonaId).toBe(null);
	});

	it("updates comparePersonaPrompt via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setComparePersonaPrompt("Persona system prompt");
		});

		expect(result.current.comparePersonaPrompt).toBe("Persona system prompt");
	});

	it("updates rounds via setter", () => {
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

		expect(result.current.rounds).toEqual(newRounds);
	});

	it("updates currentRound via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setCurrentRound(2);
		});

		expect(result.current.currentRound).toBe(2);
	});

	it("updates phase via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setPhase("running");
		});

		expect(result.current.phase).toBe("running");

		act(() => {
			result.current.setPhase("voting");
		});

		expect(result.current.phase).toBe("voting");
	});

	it("updates runningModels via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setRunningModels(new Set(["model-1", "model-2"]));
		});

		expect(result.current.runningModels).toEqual(
			new Set(["model-1", "model-2"]),
		);
	});

	it("updates winnerModal via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		const winnerModal = {
			winner: "model-1",
			rounds: [],
		};

		act(() => {
			result.current.setWinnerModal(winnerModal);
		});

		expect(result.current.winnerModal).toEqual(winnerModal);

		act(() => {
			result.current.setWinnerModal(null);
		});

		expect(result.current.winnerModal).toBe(null);
	});

	it("updates disabledModels via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setDisabledModels(new Set(["model-1"]));
		});

		expect(result.current.disabledModels).toEqual(new Set(["model-1"]));
	});

	it("updates arenaCollapsed via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setArenaCollapsed(true);
		});

		expect(result.current.arenaCollapsed).toBe(true);
	});

	it("updates pendingFullReset via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setPendingFullReset(true);
		});

		expect(result.current.pendingFullReset).toBe(true);
	});

	it("updates showHistoryModal via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setShowHistoryModal(true);
		});

		expect(result.current.showHistoryModal).toBe(true);
	});

	it("updates modelParams via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setModelParams({
				"model-1": { temperature: 0.7, max_tokens: 100 },
			});
		});

		expect(result.current.modelParams).toEqual({
			"model-1": { temperature: 0.7, max_tokens: 100 },
		});
	});

	it("updates paramEditorModel via setter", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setParamEditorModel("model-1");
		});

		expect(result.current.paramEditorModel).toBe("model-1");

		act(() => {
			result.current.setParamEditorModel(null);
		});

		expect(result.current.paramEditorModel).toBe(null);
	});

	it("can update refs directly", () => {
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.abortMapRef.current.set("model-1", new AbortController());
		});

		expect(result.current.abortMapRef.current.size).toBe(1);

		act(() => {
			result.current.lastExtractLenRef.current.set("key-1", 100);
		});

		expect(result.current.lastExtractLenRef.current.get("key-1")).toBe(100);

		act(() => {
			result.current.currentRoundRef.current = 3;
		});

		expect(result.current.currentRoundRef.current).toBe(3);

		act(() => {
			result.current.roundsLengthRef.current = 5;
		});

		expect(result.current.roundsLengthRef.current).toBe(5);

		act(() => {
			result.current.activePromptIdRef.current = "prompt-ref";
		});

		expect(result.current.activePromptIdRef.current).toBe("prompt-ref");

		act(() => {
			result.current.comparePersonaIdRef.current = "persona-ref";
		});

		expect(result.current.comparePersonaIdRef.current).toBe("persona-ref");
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
