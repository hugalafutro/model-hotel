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
		// which is the id the setCompare/setBracket calls below use.
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
				result.current.setCompareModels(["TestProvider/model-1"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false with duplicate models", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels([
					"TestProvider/model-1",
					"TestProvider/model-1",
				]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is true with 2 distinct models and prompt", () => {
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

			expect(result.current.canRun).toBe(true);
		});

		it("is false when prompt is empty", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
				result.current.setPrompt("");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false when prompt is whitespace-only", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setCompareModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
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
				result.current.setBracketModels(["TestProvider/model-1"]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});

		it("is false with 3 models (invalid bracket size)", () => {
			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
					"TestProvider/model-3",
				]);
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
					"TestProvider/model-1",
					"TestProvider/model-2",
					"TestProvider/model-3",
					"TestProvider/model-4",
					"TestProvider/model-5",
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
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-2",
				]);
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
					"TestProvider/model-1",
					"TestProvider/model-2",
					"TestProvider/model-3",
					"TestProvider/model-4",
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
					"TestProvider/model-1",
					"TestProvider/model-2",
					"TestProvider/model-3",
					"TestProvider/model-4",
					"TestProvider/model-5",
					"TestProvider/model-6",
					"TestProvider/model-7",
					"TestProvider/model-8",
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
				result.current.setBracketModels([
					"TestProvider/model-1",
					"TestProvider/model-1",
				]);
				result.current.setPrompt("Test prompt");
			});

			expect(result.current.canRun).toBe(false);
		});
	});

	describe("stale-selection reconciliation", () => {
		// A persisted line-up can contain an id that is no longer a valid chat
		// model (e.g. it became an embedding/rerank model, or got disabled).
		// Once the chat list loads, those ids are dropped so a run can't start
		// against a model that can't serve chat.
		it("drops persisted compare models absent from the chat list on load", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					phase: "setup",
					compareModels: ["TestProvider/model-1", "TestProvider/model-99"],
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			// model-99 is not in the mocked 8-model chat list.
			expect(result.current.compareModels).toEqual(["TestProvider/model-1"]);
		});

		it("drops persisted bracket models absent from the chat list on load", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					phase: "setup",
					bracketModels: ["TestProvider/model-1", "TestProvider/model-99"],
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.bracketModels).toEqual(["TestProvider/model-1"]);
		});
	});
});
