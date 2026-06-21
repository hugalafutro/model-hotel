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

	it("setActivePromptId routes to the competition prompt id in competition mode", () => {
		// The test above only exercises whichever mode is default (compare); force
		// competition here to cover the other dispatch branch deterministically.
		arenaModeRef.current = "competition";
		const { result } = renderHook(() => useArenaState(), {
			wrapper: createWrapper(),
		});

		act(() => {
			result.current.setActivePromptId("comp-prompt-id");
		});

		expect(result.current.competitionActivePromptId).toBe("comp-prompt-id");
		expect(result.current.compareActivePromptId).not.toBe("comp-prompt-id");
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
});
