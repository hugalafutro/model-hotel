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

	describe("canRun - compare mode", () => {
		beforeEach(() => {
			arenaModeRef.current = "compare";
		});

		it("is false with no models", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false with only 1 model", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels(["model-1"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false with duplicate models", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels(["model-1", "model-1"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is true with 2 distinct models and prompt", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels(["model-1", "model-2"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(true);
		});

		it("is false when prompt is empty", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels(["model-1", "model-2"]);
				result.current.setPrompt("");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false when prompt is whitespace-only", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels(["model-1", "model-2"]);
				result.current.setPrompt("   ");
			});

			expect(result.current.canRun).toBe(false);
		});
	});

	describe("canRun - competition mode", () => {
		beforeEach(() => {
			arenaModeRef.current = "competition";
		});

		it("is false with 0 models", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false with 1 model", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels(["model-1"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false with 3 models (invalid bracket size)", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels(["model-1", "model-2", "model-3"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false with 5 models (invalid bracket size)", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"model-1",
					"model-2",
					"model-3",
					"model-4",
					"model-5",
				]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is true with 2 models + prompt", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels(["model-1", "model-2"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(true);
		});

		it("is true with 4 models + prompt", () => {
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
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(true);
		});

		it("is true with 8 models + prompt", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"model-1",
					"model-2",
					"model-3",
					"model-4",
					"model-5",
					"model-6",
					"model-7",
					"model-8",
				]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(true);
		});

		it("is false with duplicate models", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels(["model-1", "model-1"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});
	});
});
