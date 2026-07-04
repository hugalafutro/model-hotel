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
	useChatModels: vi.fn(() => ({
		// proxyModelID("TestProvider", "model-N") === "TestProvider/model-N",
		// which is the id form the set*/assert calls below use.
		data: Array.from({ length: 8 }, (_, i) => ({
			provider_name: "TestProvider",
			model_id: `model-${i + 1}`,
			enabled: true,
		})),
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

	describe("disabledReason - compare mode", () => {
		beforeEach(() => {
			arenaModeRef.current = "compare";
		});

		it('shows "Select at least 2 models" when 0 models', () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.disabledReason).toBe("Select at least 2 models");
		});

		it('shows "Pick at least 1 more model" when 1 model', () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels(["TestProvider/model-1"]);
			});

			expect(result.current.disabledReason).toBe("Pick at least 1 more model");
		});

		it('shows "No duplicate models" when duplicates present', () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels([
					"TestProvider/model-1",
					"TestProvider/model-1",
				]);
			});

			expect(result.current.disabledReason).toBe("No duplicate models");
		});

		it('shows "Enter a prompt" when models OK but no prompt', () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
			});

			expect(result.current.disabledReason).toBe("Enter a prompt");
		});

		it("is empty string when all conditions met", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.disabledReason).toBe("");
		});
	});

	describe("disabledReason - competition mode", () => {
		beforeEach(() => {
			arenaModeRef.current = "competition";
		});

		it('shows "Select 2, 4, or 8 models" with 0 models', () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.disabledReason).toBe("Select 2, 4, or 8 models");
		});

		it('shows "Pick at least 1 more model" with 1 model', () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels(["TestProvider/model-1"]);
			});

			expect(result.current.disabledReason).toBe("Pick at least 1 more model");
		});

		it('shows "No duplicate models" with duplicates', () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-1",
				]);
			});

			expect(result.current.disabledReason).toBe("No duplicate models");
		});

		it("shows pick-or-remove message for 3 models", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
					"TestProvider/model-3",
				]);
			});

			// nextBracketSize(3) = 4, so "Pick 1 more or remove to get 4"
			expect(result.current.disabledReason).toBe(
				"Pick 1 more or remove to get 4",
			);
		});

		it('shows "Enter a prompt" when models OK but no prompt', () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
			});

			expect(result.current.disabledReason).toBe("Enter a prompt");
		});

		it("is empty string when all conditions met", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.disabledReason).toBe("");
		});

		it("shows voting phase message", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
				result.current.setPrompt("Test prompt");
				result.current.setPhase("voting");
			});

			expect(result.current.disabledReason).toBe(
				"Vote on all matchups to continue to the next round",
			);
		});

		it("shows next round prompt message when next_round_ready with no prompt", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
				result.current.setPrompt("");
				result.current.setPhase("next_round_ready");
			});

			expect(result.current.disabledReason).toBe(
				"Enter a prompt for the next round",
			);
		});
	});
});
