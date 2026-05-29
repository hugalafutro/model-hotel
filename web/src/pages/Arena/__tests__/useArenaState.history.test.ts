import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useArenaState } from "../useArenaState";
import {
	arenaHistoryMocks,
	arenaModeRef,
	createMockRoundsWithResponses,
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

	describe("compare history save effect", () => {
		beforeEach(() => {
			localStorage.clear();
			arenaModeRef.current = "compare";
			// Reset to default
			persistRef.current = false;
			// Reset mocks
			arenaHistoryMocks.saveCompareToHistory.mockClear();
			arenaHistoryMocks.getArenaHistoryEnabled.mockReturnValue(false);
		});

		it("saves compare history when phase becomes finished in compare mode", async () => {
			arenaHistoryMocks.getArenaHistoryEnabled.mockReturnValue(true);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			const mockRounds = createMockRoundsWithResponses();

			// First set rounds - this will sync roundsRef
			act(() => {
				result.current.setRounds(mockRounds);
			});

			// Flush the roundsRef sync effect before changing phase
			await act(async () => {});

			// Now set phase to finished - this triggers the effect
			act(() => {
				result.current.setPhase("finished");
			});

			// Flush pending effects
			await act(async () => {});

			expect(arenaHistoryMocks.saveCompareToHistory).toHaveBeenCalled();
			expect(arenaHistoryMocks.getArenaHistoryEnabled).toHaveBeenCalled();
		});

		it("does NOT save compare history when arenaMode is competition", async () => {
			arenaHistoryMocks.getArenaHistoryEnabled.mockReturnValue(true);
			arenaModeRef.current = "competition";

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setRounds([{ matchups: [] }]);
				result.current.setPhase("finished");
			});

			await act(async () => {});

			expect(arenaHistoryMocks.saveCompareToHistory).not.toHaveBeenCalled();
		});

		it("does NOT save compare history when getArenaHistoryEnabled is false", async () => {
			arenaHistoryMocks.getArenaHistoryEnabled.mockReturnValue(false);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setRounds([{ matchups: [] }]);
				result.current.setPhase("finished");
			});

			await act(async () => {});

			expect(arenaHistoryMocks.saveCompareToHistory).not.toHaveBeenCalled();
		});

		it("resets compareHistorySavedRef when phase leaves finished", async () => {
			arenaHistoryMocks.getArenaHistoryEnabled.mockReturnValue(true);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			const mockRounds = createMockRoundsWithResponses();

			// First set rounds
			act(() => {
				result.current.setRounds(mockRounds);
			});

			// Flush the roundsRef sync effect
			await act(async () => {});

			// Set phase to finished - should trigger save
			act(() => {
				result.current.setPhase("finished");
			});
			await act(async () => {});

			expect(arenaHistoryMocks.saveCompareToHistory).toHaveBeenCalledTimes(1);

			// Go back to setup - should reset the flag
			act(() => {
				result.current.setPhase("setup");
			});
			await act(async () => {});

			// Now if we go to finished again, it should save again (flag was reset)
			act(() => {
				result.current.setPhase("finished");
			});
			await act(async () => {});

			// Should have been called twice now
			expect(arenaHistoryMocks.saveCompareToHistory).toHaveBeenCalledTimes(2);
		});
	});
});
