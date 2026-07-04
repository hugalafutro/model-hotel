import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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
		// Cover every id the persistence tests seed so stale-selection
		// reconciliation (which drops ids absent from the chat list) leaves the
		// loaded line-up intact. proxyModelID(provider, model) === "provider/model".
		data: [
			...Array.from({ length: 8 }, (_, i) => ({
				provider_name: "TestProvider",
				model_id: `model-${i + 1}`,
				enabled: true,
			})),
			...Array.from({ length: 4 }, (_, i) => ({
				provider_name: `P${i + 1}`,
				model_id: `M${i + 1}`,
				enabled: true,
			})),
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

	describe("localStorage initialization with persistArena=true", () => {
		beforeEach(() => {
			localStorage.clear();
			arenaModeRef.current = "compare";
			// Enable persistence
			persistRef.current = true;
		});

		afterEach(() => {
			// Reset to default
			persistRef.current = false;
		});

		it("initializes compareModels from localStorage", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					compareModels: ["P1/M1", "P2/M2"],
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.compareModels).toEqual(["P1/M1", "P2/M2"]);
		});

		it("initializes bracketModels from localStorage", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					bracketModels: ["P1/M1", "P2/M2", "P3/M3", "P4/M4"],
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.bracketModels).toEqual([
				"P1/M1",
				"P2/M2",
				"P3/M3",
				"P4/M4",
			]);
		});

		it("initializes bracketModels from legacy group1Models/group2Groups fallback", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					group1Models: ["P1/M1", "P2/M2"],
					group2Models: ["P3/M3", "P4/M4"],
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.bracketModels).toEqual([
				"P1/M1",
				"P2/M2",
				"P3/M3",
				"P4/M4",
			]);
		});

		it("initializes savedPrompt from localStorage", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					savedPrompt: "My saved prompt",
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.savedPrompt).toBe("My saved prompt");
		});

		it("initializes rounds from localStorage", () => {
			localStorage.setItem("persistArena", "true");
			const mockRounds: import("../types").BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "TestProvider/model-1",
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
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					rounds: mockRounds,
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.rounds).toEqual(mockRounds);
		});

		it("initializes currentRound from localStorage", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					currentRound: 2,
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.currentRound).toBe(2);
		});

		it("initializes phase from localStorage", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					phase: "running",
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.phase).toBe("running");
		});

		it("initializes arenaCollapsed from localStorage", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					arenaCollapsed: true,
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.arenaCollapsed).toBe(true);
		});

		it("initializes modelParams from localStorage", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					modelParams: {
						"TestProvider/model-1": { temperature: 0.8, max_tokens: 200 },
					},
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.modelParams).toEqual({
				"TestProvider/model-1": { temperature: 0.8, max_tokens: 200 },
			});
		});

		it("falls back to defaults when localStorage parse fails", () => {
			localStorage.setItem("persistArena", "true");
			localStorage.setItem("arenaState", "invalid json");

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			expect(result.current.compareModels).toEqual([]);
			expect(result.current.bracketModels).toEqual([]);
			expect(result.current.savedPrompt).toBe("");
			expect(result.current.rounds).toEqual([]);
			expect(result.current.currentRound).toBe(0);
			expect(result.current.phase).toBe("setup");
			expect(result.current.arenaCollapsed).toBe(false);
			expect(result.current.modelParams).toEqual({});
		});
	});

	describe("localStorage initialization with persistArena=false", () => {
		beforeEach(() => {
			localStorage.clear();
			arenaModeRef.current = "compare";
			// Ensure persistence is disabled
			persistRef.current = false;
		});

		it("falls back to defaults when persistArena=false", () => {
			// Don't set persistArena in localStorage - leave it null
			// The initializers check localStorage.getItem("persistArena") === "true"
			// so without it set, they should return defaults
			localStorage.setItem(
				"arenaState",
				JSON.stringify({
					compareModels: ["P1/M1"],
					bracketModels: ["P1/M1"],
					savedPrompt: "test",
					phase: "running",
				}),
			);

			const { result } = renderHook(() => useArenaState(), {
				wrapper: createWrapper(),
			});

			// Should use defaults since persistArena is not "true" in localStorage
			expect(result.current.compareModels).toEqual([]);
			expect(result.current.bracketModels).toEqual([]);
			expect(result.current.savedPrompt).toBe("");
			expect(result.current.phase).toBe("setup");
		});
	});
});
