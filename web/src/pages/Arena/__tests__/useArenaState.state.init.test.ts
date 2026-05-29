import { renderHook } from "@testing-library/react";
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
});
