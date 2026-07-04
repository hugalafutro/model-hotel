import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { Model } from "../../../api/types";
import type { ArenaSubMode } from "../../../context/SidebarModeContext";
import type { useToast } from "../../../context/ToastContext";
import type { BracketRound } from "../types";
import { useArena } from "../useArena";

// Mock the dependencies
vi.mock("../useArenaState", () => ({
	useArenaState: vi.fn(),
}));

vi.mock("../useArenaRunner", () => ({
	useArenaRunner: vi.fn(),
}));

vi.mock("../../../utils/arenaHistory", () => ({
	getArenaHistoryEnabled: vi.fn(),
	saveCompetitionToHistory: vi.fn(),
}));

// Import the mocked modules
const { useArenaState } = await import("../useArenaState");
const { useArenaRunner } = await import("../useArenaRunner");
const { getArenaHistoryEnabled, saveCompetitionToHistory } = await import(
	"../../../utils/arenaHistory"
);

const createMockArenaState = (
	overrides?: Partial<ReturnType<typeof useArenaState>>,
): ReturnType<typeof useArenaState> => {
	const mockState: ReturnType<typeof useArenaState> = {
		// State values
		compareModels: [],
		setCompareModels: vi.fn(),
		bracketModels: [],
		setBracketModels: vi.fn(),
		competitionActivePromptId: null,
		setCompetitionActivePromptId: vi.fn(),
		compareActivePromptId: null,
		setCompareActivePromptId: vi.fn(),
		competitionPrompt: "",
		setCompetitionPrompt: vi.fn(),
		comparePrompt: "",
		setComparePrompt: vi.fn(),
		prompt: "",
		setPrompt: vi.fn(),
		activePromptId: null,
		setActivePromptId: vi.fn(),
		savedPrompt: "",
		setSavedPrompt: vi.fn(),
		comparePersonaId: null,
		setComparePersonaId: vi.fn(),
		comparePersonaPrompt: "",
		setComparePersonaPrompt: vi.fn(),
		rounds: [],
		setRounds: vi.fn(),
		currentRound: 0,
		setCurrentRound: vi.fn(),
		phase: "setup" as const,
		setPhase: vi.fn(),
		runningModels: new Set(),
		setRunningModels: vi.fn(),
		winnerModal: null,
		setWinnerModal: vi.fn(),
		disabledModels: new Set(),
		setDisabledModels: vi.fn(),
		arenaCollapsed: false,
		setArenaCollapsed: vi.fn(),
		pendingFullReset: false,
		setPendingFullReset: vi.fn(),
		showHistoryModal: false,
		setShowHistoryModal: vi.fn(),
		modelParams: {},
		setModelParams: vi.fn(),
		paramEditorModel: null,
		setParamEditorModel: vi.fn(),
		arenaMode: "compare" as ArenaSubMode,
		setArenaMode: vi.fn(),
		// Refs
		abortMapRef: { current: new Map() },
		lastExtractLenRef: { current: new Map() },
		currentRoundRef: { current: 0 },
		roundsLengthRef: { current: 0 },
		roundsRef: { current: [] },
		activePromptIdRef: { current: null },
		comparePersonaIdRef: { current: null },
		arenaModeRef: { current: "compare" as ArenaSubMode },
		// Computed values
		canRun: false,
		disabledReason: "",
		buildCompareRoundWithParams: vi.fn(),
		buildInitialRoundsWithParams: vi.fn(),
		handleRandomComparePersona: vi.fn(),
		handleRandomBracketModel: vi.fn(),
		handleRandomCompareModel: vi.fn(),
		previewPairs: null,
		// Dependencies
		enabledModels: [] as Model[],
		modelsReady: true,
		toast: vi.fn() as ReturnType<typeof useToast>["toast"],
		...overrides,
	};
	return mockState;
};

const createMockArenaRunner = (
	overrides?: Partial<ReturnType<typeof useArenaRunner>>,
): ReturnType<typeof useArenaRunner> => {
	return {
		streamModel: vi.fn(),
		runRound: vi.fn(),
		handleStopAll: vi.fn(),
		handleRetry: vi.fn(),
		handleCancelSlot: vi.fn(),
		handleSwapComplete: vi.fn(),
		abortMapRef: { current: new Map() },
		...overrides,
	};
};

const createWrapper = () => {
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return children;
	};
};

describe("useArena", () => {
	describe("phase correction effect", () => {
		it("phase='voting', all voted, last round → phase='finished', winnerModal set", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: "A" as const,
						},
					],
				},
			];
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();
			const setRoundsMock = vi.fn();
			const setCurrentRoundMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "voting",
					rounds,
					currentRound: 0,
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					setRounds: setRoundsMock,
					setCurrentRound: setCurrentRoundMock,
					roundsRef: { current: rounds },
					currentRoundRef: { current: 0 },
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			// Effect should have run
			expect(setPhaseMock).toHaveBeenCalledWith("finished");
			expect(setWinnerModalMock).toHaveBeenCalledWith({
				winner: "model-a",
				rounds,
			});
		});

		it("phase='voting', all voted, not last round → advances to next round", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: "A" as const,
						},
					],
				},
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
			];
			const setPhaseMock = vi.fn();
			const setRoundsMock = vi.fn();
			const setCurrentRoundMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "voting",
					rounds,
					currentRound: 0,
					setPhase: setPhaseMock,
					setRounds: setRoundsMock,
					setCurrentRound: setCurrentRoundMock,
					roundsRef: { current: rounds },
					currentRoundRef: { current: 0 },
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			// Should advance to next round
			expect(setCurrentRoundMock).toHaveBeenCalledWith(1);
			expect(setPhaseMock).toHaveBeenCalledWith("next_round_ready");
			expect(setRoundsMock).toHaveBeenCalled();
		});

		it("phase='setup' → no correction", () => {
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();
			const setRoundsMock = vi.fn();
			const setCurrentRoundMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "setup",
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					setRounds: setRoundsMock,
					setCurrentRound: setCurrentRoundMock,
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			renderHook(() => useArena(), { wrapper: createWrapper() });

			expect(setPhaseMock).not.toHaveBeenCalled();
			expect(setWinnerModalMock).not.toHaveBeenCalled();
		});

		it("phase='voting' but not all voted → no correction", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "voting",
					rounds,
					currentRound: 0,
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					roundsRef: { current: rounds },
					currentRoundRef: { current: 0 },
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			renderHook(() => useArena(), { wrapper: createWrapper() });

			expect(setPhaseMock).not.toHaveBeenCalled();
			expect(setWinnerModalMock).not.toHaveBeenCalled();
		});

		it("phase correction returns early when current round is undefined", () => {
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "voting",
					rounds: [],
					currentRound: 0,
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					roundsRef: { current: [] },
					currentRoundRef: { current: 0 },
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			renderHook(() => useArena(), { wrapper: createWrapper() });

			expect(setPhaseMock).not.toHaveBeenCalled();
			expect(setWinnerModalMock).not.toHaveBeenCalled();
		});

		it("phase correction with B vote winner", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: "B" as const,
						},
					],
				},
			];
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "voting",
					rounds,
					currentRound: 0,
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					roundsRef: { current: rounds },
					currentRoundRef: { current: 0 },
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			renderHook(() => useArena(), { wrapper: createWrapper() });

			expect(setPhaseMock).toHaveBeenCalledWith("finished");
			expect(setWinnerModalMock).toHaveBeenCalledWith({
				winner: "model-b",
				rounds,
			});
		});
	});

	describe("deferred run recovery effect", () => {
		const pendingRounds = (): BracketRound[] => [
			{
				matchups: [
					{
						slotA: {
							modelId: "P/model-a",
							personaId: null,
							personaPrompt: "",
							params: {},
						},
						slotB: null,
						responseA: {
							model: "P/model-a",
							rawContent: "",
							content: "",
							thinkingContent: "",
							startTimeMs: 0,
							done: false,
							error: null,
							metrics: null,
						},
						responseB: null,
						vote: null,
					},
				],
			},
		];

		const setState = (
			runRoundMock: (roundIdx: number) => void,
			opts: {
				rounds: BracketRound[];
				modelsReady: boolean;
				enabledModels: Model[];
				phase?: "running" | "setup";
				abortMap?: Map<string, AbortController>;
			},
		) => {
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: opts.phase ?? "running",
					rounds: opts.rounds,
					currentRound: 0,
					roundsRef: { current: opts.rounds },
					currentRoundRef: { current: 0 },
					modelsReady: opts.modelsReady,
					enabledModels: opts.enabledModels,
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({
					runRound: runRoundMock,
					abortMapRef: { current: opts.abortMap ?? new Map() },
				}),
			);
		};

		it("re-dispatches the current round when the allowlist becomes usable", () => {
			const runRoundMock = vi.fn();
			const rounds = pendingRounds();
			setState(runRoundMock, {
				rounds,
				modelsReady: false,
				enabledModels: [],
			});
			const { rerender } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});
			expect(runRoundMock).not.toHaveBeenCalled();

			setState(runRoundMock, {
				rounds,
				modelsReady: true,
				enabledModels: [{ provider_name: "P", model_id: "model-a" } as Model],
			});
			rerender();

			expect(runRoundMock).toHaveBeenCalledWith(0);
		});

		it("re-dispatches on mount when a saved running round reloads with a warm cache", () => {
			// Warm model cache: the allowlist is usable on the very first render, so
			// there is no false->true transition to observe. A persisted "running"
			// round with pending slots and no active stream must still be recovered.
			const runRoundMock = vi.fn();
			const rounds = pendingRounds();
			setState(runRoundMock, {
				rounds,
				modelsReady: true,
				enabledModels: [{ provider_name: "P", model_id: "model-a" } as Model],
			});

			renderHook(() => useArena(), { wrapper: createWrapper() });

			expect(runRoundMock).toHaveBeenCalledWith(0);
		});

		it("does not re-dispatch while streams are in flight", () => {
			const runRoundMock = vi.fn();
			const rounds = pendingRounds();
			setState(runRoundMock, {
				rounds,
				modelsReady: false,
				enabledModels: [],
			});
			const { rerender } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			// Allowlist becomes usable but a stream is already active.
			setState(runRoundMock, {
				rounds,
				modelsReady: true,
				enabledModels: [{ provider_name: "P", model_id: "model-a" } as Model],
				abortMap: new Map([["P/model-a", new AbortController()]]),
			});
			rerender();

			expect(runRoundMock).not.toHaveBeenCalled();
		});

		it("does not re-dispatch when not in the running phase", () => {
			const runRoundMock = vi.fn();
			const rounds = pendingRounds();
			setState(runRoundMock, {
				rounds,
				modelsReady: false,
				enabledModels: [],
				phase: "setup",
			});
			const { rerender } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			setState(runRoundMock, {
				rounds,
				modelsReady: true,
				enabledModels: [{ provider_name: "P", model_id: "model-a" } as Model],
				phase: "setup",
			});
			rerender();

			expect(runRoundMock).not.toHaveBeenCalled();
		});

		it("re-dispatches for a pending slot B", () => {
			const runRoundMock = vi.fn();
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: null,
							slotB: {
								modelId: "P/model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: {
								model: "P/model-b",
								rawContent: "",
								content: "",
								thinkingContent: "",
								startTimeMs: 0,
								done: false,
								error: null,
								metrics: null,
							},
							vote: null,
						},
					],
				},
			];
			setState(runRoundMock, {
				rounds,
				modelsReady: false,
				enabledModels: [],
			});
			const { rerender } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			setState(runRoundMock, {
				rounds,
				modelsReady: true,
				enabledModels: [{ provider_name: "P", model_id: "model-b" } as Model],
			});
			rerender();

			expect(runRoundMock).toHaveBeenCalledWith(0);
		});

		it("drops a stuck running round to setup when no chat models exist", () => {
			const runRoundMock = vi.fn();
			const setPhaseMock = vi.fn();
			const rounds = pendingRounds();
			// Settled (modelsReady) but empty list, still "running" with a pending
			// slot: the run can't proceed, so it must leave the running phase.
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "running",
					rounds,
					currentRound: 0,
					roundsRef: { current: rounds },
					currentRoundRef: { current: 0 },
					modelsReady: true,
					enabledModels: [],
					setPhase: setPhaseMock,
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({
					runRound: runRoundMock,
					abortMapRef: { current: new Map() },
				}),
			);

			renderHook(() => useArena(), { wrapper: createWrapper() });

			expect(setPhaseMock).toHaveBeenCalledWith("setup");
			expect(runRoundMock).not.toHaveBeenCalled();
		});

		it("does not re-dispatch when the current round is missing", () => {
			const runRoundMock = vi.fn();
			setState(runRoundMock, {
				rounds: [],
				modelsReady: false,
				enabledModels: [],
			});
			const { rerender } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			setState(runRoundMock, {
				rounds: [],
				modelsReady: true,
				enabledModels: [{ provider_name: "P", model_id: "model-a" } as Model],
			});
			rerender();

			expect(runRoundMock).not.toHaveBeenCalled();
		});

		it("does not re-dispatch when every slot is already done", () => {
			const runRoundMock = vi.fn();
			const doneRounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "P/model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: null,
							responseA: {
								model: "P/model-a",
								rawContent: "hi",
								content: "hi",
								thinkingContent: "",
								startTimeMs: 0,
								done: true,
								error: null,
								metrics: null,
							},
							responseB: null,
							vote: null,
						},
					],
				},
			];
			setState(runRoundMock, {
				rounds: doneRounds,
				modelsReady: false,
				enabledModels: [],
			});
			const { rerender } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			setState(runRoundMock, {
				rounds: doneRounds,
				modelsReady: true,
				enabledModels: [{ provider_name: "P", model_id: "model-a" } as Model],
			});
			rerender();

			expect(runRoundMock).not.toHaveBeenCalled();
		});
	});

	describe("handleRunArena", () => {
		it("canRun=false → does nothing (no state changes)", () => {
			const setSavedPromptMock = vi.fn();
			const setRoundsMock = vi.fn();
			const setCurrentRoundMock = vi.fn();
			const setPhaseMock = vi.fn();
			const setRunningModelsMock = vi.fn();
			const buildCompareMock = vi.fn();
			const buildInitialMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					canRun: false,
					setSavedPrompt: setSavedPromptMock,
					setRounds: setRoundsMock,
					setCurrentRound: setCurrentRoundMock,
					setPhase: setPhaseMock,
					setRunningModels: setRunningModelsMock,
					buildCompareRoundWithParams: buildCompareMock,
					buildInitialRoundsWithParams: buildInitialMock,
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleRunArena();
			});

			expect(setSavedPromptMock).not.toHaveBeenCalled();
			expect(setRoundsMock).not.toHaveBeenCalled();
			expect(setPhaseMock).not.toHaveBeenCalled();
			expect(buildCompareMock).not.toHaveBeenCalled();
			expect(buildInitialMock).not.toHaveBeenCalled();
		});

		it("compare mode → calls buildCompareRoundWithParams", () => {
			const compareModels = ["model-a", "model-b"];
			const comparePersonaId = "persona-1";
			const comparePersonaPrompt = "Test persona";
			const prompt = "Test prompt";
			const mockRounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
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
			const setSavedPromptMock = vi.fn();
			const setRoundsMock = vi.fn();
			const setCurrentRoundMock = vi.fn();
			const setPhaseMock = vi.fn();
			const setRunningModelsMock = vi.fn();
			const buildCompareMock = vi.fn().mockReturnValue(mockRounds);

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					canRun: true,
					arenaMode: "compare",
					compareModels,
					comparePersonaId,
					comparePersonaPrompt,
					prompt,
					setSavedPrompt: setSavedPromptMock,
					setRounds: setRoundsMock,
					setCurrentRound: setCurrentRoundMock,
					setPhase: setPhaseMock,
					setRunningModels: setRunningModelsMock,
					buildCompareRoundWithParams: buildCompareMock,
					enabledModels: [
						{ provider_name: "openai", model_id: "gpt-4" } as Model,
					],
				}),
			);

			const streamModelMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ streamModel: streamModelMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleRunArena();
			});

			expect(buildCompareMock).toHaveBeenCalledWith(
				compareModels,
				comparePersonaId,
				comparePersonaPrompt,
			);
			expect(setSavedPromptMock).toHaveBeenCalledWith(prompt);
			expect(setRoundsMock).toHaveBeenCalled();
			expect(setPhaseMock).toHaveBeenCalledWith("running");
			expect(setCurrentRoundMock).toHaveBeenCalledWith(0);
		});

		it("competition mode → calls buildInitialRoundsWithParams", () => {
			const bracketModels = ["model-a", "model-b"];
			const prompt = "Test prompt";
			const mockRounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const setSavedPromptMock = vi.fn();
			const setRoundsMock = vi.fn();
			const setCurrentRoundMock = vi.fn();
			const setPhaseMock = vi.fn();
			const setRunningModelsMock = vi.fn();
			const buildInitialMock = vi.fn().mockReturnValue(mockRounds);

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					canRun: true,
					arenaMode: "competition",
					bracketModels,
					prompt,
					setSavedPrompt: setSavedPromptMock,
					setRounds: setRoundsMock,
					setCurrentRound: setCurrentRoundMock,
					setPhase: setPhaseMock,
					setRunningModels: setRunningModelsMock,
					buildInitialRoundsWithParams: buildInitialMock,
					enabledModels: [
						{ provider_name: "openai", model_id: "gpt-4" } as Model,
					],
				}),
			);

			const streamModelMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ streamModel: streamModelMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleRunArena();
			});

			expect(buildInitialMock).toHaveBeenCalledWith(bracketModels);
			expect(setSavedPromptMock).toHaveBeenCalledWith(prompt);
			expect(setRoundsMock).toHaveBeenCalled();
			expect(setPhaseMock).toHaveBeenCalledWith("running");
		});

		it("handleRunArena sets runningModels with model IDs from first round matchups", () => {
			const compareModels = ["model-a", "model-b"];
			const prompt = "Test prompt";
			const mockRounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const setSavedPromptMock = vi.fn();
			const setRoundsMock = vi.fn();
			const setCurrentRoundMock = vi.fn();
			const setPhaseMock = vi.fn();
			const setRunningModelsMock = vi.fn();
			const buildCompareMock = vi.fn().mockReturnValue(mockRounds);

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					canRun: true,
					arenaMode: "compare",
					compareModels,
					prompt,
					setSavedPrompt: setSavedPromptMock,
					setRounds: setRoundsMock,
					setCurrentRound: setCurrentRoundMock,
					setPhase: setPhaseMock,
					setRunningModels: setRunningModelsMock,
					buildCompareRoundWithParams: buildCompareMock,
					enabledModels: [
						{ provider_name: "openai", model_id: "gpt-4" } as Model,
					],
				}),
			);

			const streamModelMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ streamModel: streamModelMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleRunArena();
			});

			const runningModels = setRunningModelsMock.mock
				.calls[0]?.[0] as Set<string>;
			expect(runningModels).toBeInstanceOf(Set);
			expect(runningModels.has("model-a")).toBe(true);
			expect(runningModels.has("model-b")).toBe(true);
		});
	});

	describe("handleVote", () => {
		it("toggle vote on a matchup", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const roundsRef = { current: rounds };
			const setRoundsMock = vi.fn((arg) => {
				if (typeof arg === "function") {
					const result = arg(rounds);
					roundsRef.current = result;
				} else {
					// produce() returns a new array
					roundsRef.current = arg;
				}
			});

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					currentRound: 0,
					setRounds: setRoundsMock,
					roundsRef,
					currentRoundRef: { current: 0 },
				}),
			);
			const runRoundMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ runRound: runRoundMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleVote(0, 0, "A");
			});

			expect(setRoundsMock).toHaveBeenCalled();
			// Vote should be set to A
			expect(roundsRef.current[0].matchups[0].vote).toBe("A");
		});

		it("all voted in last round → winner declared, phase='finished'", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const roundsRef = { current: rounds };
			const setRoundsMock = vi.fn((arg) => {
				if (typeof arg === "function") {
					const result = arg(rounds);
					roundsRef.current = result;
				} else {
					roundsRef.current = arg;
				}
			});
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					currentRound: 0,
					setRounds: setRoundsMock,
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					roundsRef,
					currentRoundRef: { current: 0 },
				}),
			);
			const runRoundMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ runRound: runRoundMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			// Vote to trigger the winner declaration
			act(() => {
				result.current.handleVote(0, 0, "A");
			});

			expect(setPhaseMock).toHaveBeenCalledWith("finished");
			expect(setWinnerModalMock).toHaveBeenCalledWith({
				winner: "model-a",
				rounds: roundsRef.current,
			});
		});

		it("all voted in non-last round → round advances, phase='running'", async () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
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
			];
			const roundsRef = { current: rounds };
			const setRoundsMock = vi.fn((arg) => {
				if (typeof arg === "function") {
					const result = arg(rounds);
					roundsRef.current = result;
				} else {
					roundsRef.current = arg;
				}
			});
			const setPhaseMock = vi.fn();
			const setCurrentRoundMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					currentRound: 0,
					setRounds: setRoundsMock,
					setPhase: setPhaseMock,
					setCurrentRound: setCurrentRoundMock,
					roundsRef,
					currentRoundRef: { current: 0 },
				}),
			);
			const runRoundMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ runRound: runRoundMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleVote(0, 0, "A");
			});

			// Should advance to next round
			expect(setCurrentRoundMock).toHaveBeenCalledWith(1);
			expect(setPhaseMock).toHaveBeenCalledWith("running");
			// runRound is called via queueMicrotask — flush it before asserting
			await act(async () => {
				await new Promise<void>((resolve) => queueMicrotask(resolve));
			});
			expect(runRoundMock).toHaveBeenCalledWith(1);
		});

		it("declares winner and sets phase=finished when last round fully voted", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const roundsRef = { current: rounds };
			const setRoundsMock = vi.fn((arg) => {
				if (typeof arg === "function") {
					const result = arg(rounds);
					roundsRef.current = result;
				} else {
					roundsRef.current = arg;
				}
			});
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					currentRound: 0,
					setRounds: setRoundsMock,
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					roundsRef,
					currentRoundRef: { current: 0 },
					activePromptIdRef: { current: "prompt-1" },
				}),
			);
			const runRoundMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ runRound: runRoundMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleVote(0, 0, "A");
			});

			// Verify winner declaration code path is exercised
			expect(setPhaseMock).toHaveBeenCalledWith("finished");
			expect(setWinnerModalMock).toHaveBeenCalled();
		});

		it("handleVote toggles vote off when same vote clicked again", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: "A" as const,
						},
					],
				},
			];
			const roundsRef = { current: rounds };
			const setRoundsMock = vi.fn((arg) => {
				if (typeof arg === "function") {
					const result = arg(rounds);
					roundsRef.current = result;
				} else {
					roundsRef.current = arg;
				}
			});

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					currentRound: 0,
					setRounds: setRoundsMock,
					roundsRef,
					currentRoundRef: { current: 0 },
				}),
			);
			const runRoundMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ runRound: runRoundMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleVote(0, 0, "A");
			});

			// Vote should be toggled off to null
			expect(roundsRef.current[0].matchups[0].vote).toBe(null);
		});

		it("handleVote declares slotB winner when final vote is B", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const roundsRef = { current: rounds };
			const setRoundsMock = vi.fn((arg) => {
				if (typeof arg === "function") {
					const result = arg(rounds);
					roundsRef.current = result;
				} else {
					roundsRef.current = arg;
				}
			});
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					currentRound: 0,
					setRounds: setRoundsMock,
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					roundsRef,
					currentRoundRef: { current: 0 },
				}),
			);
			const runRoundMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ runRound: runRoundMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleVote(0, 0, "B");
			});

			expect(setPhaseMock).toHaveBeenCalledWith("finished");
			expect(setWinnerModalMock).toHaveBeenCalledWith({
				winner: "model-b",
				rounds: roundsRef.current,
			});
		});

		it("handleVote saves to arena history with correct arguments when history enabled", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const roundsRef = { current: rounds };
			const setRoundsMock = vi.fn((arg) => {
				if (typeof arg === "function") {
					const result = arg(rounds);
					roundsRef.current = result;
				} else {
					roundsRef.current = arg;
				}
			});
			const setPhaseMock = vi.fn();
			const setWinnerModalMock = vi.fn();

			vi.mocked(getArenaHistoryEnabled).mockReturnValue(true);

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					currentRound: 0,
					setRounds: setRoundsMock,
					setPhase: setPhaseMock,
					setWinnerModal: setWinnerModalMock,
					roundsRef,
					currentRoundRef: { current: 0 },
					activePromptIdRef: { current: "prompt-1" },
				}),
			);
			const runRoundMock = vi.fn();
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ runRound: runRoundMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleVote(0, 0, "A");
			});

			expect(setPhaseMock).toHaveBeenCalledWith("finished");
			expect(setWinnerModalMock).toHaveBeenCalled();
			expect(saveCompetitionToHistory).toHaveBeenCalledWith({
				rounds: roundsRef.current,
				winner: "model-a",
				promptPresetId: "prompt-1",
				comparePersonaId: null,
			});

			// Reset mock to prevent leakage into subsequent tests
			vi.mocked(getArenaHistoryEnabled).mockReset();
			vi.mocked(saveCompetitionToHistory).mockClear();
		});
	});

	describe("handleSwapModel", () => {
		it("adds model to disabledModels, nulls slot", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const setDisabledModelsMock = vi.fn();
			const setRoundsMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					setDisabledModels: setDisabledModelsMock,
					setRounds: setRoundsMock,
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleSwapModel(0, 0, "A", "model-a");
			});

			expect(setDisabledModelsMock).toHaveBeenCalled();
			expect(setRoundsMock).toHaveBeenCalled();
		});
	});

	describe("handleSwapCompleteAndUpdate", () => {
		it("calls handleSwapComplete with correct parameters", () => {
			const handleSwapCompleteMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(createMockArenaState());
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ handleSwapComplete: handleSwapCompleteMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleSwapCompleteAndUpdate(0, 0, "A", "new-model");
			});

			// handleSwapComplete should always be called
			expect(handleSwapCompleteMock).toHaveBeenCalledWith(
				0,
				0,
				"A",
				"new-model",
			);
		});

		it("handleSwapCompleteAndUpdate updates bracketModels when oldModelId tracked", () => {
			const handleSwapCompleteMock = vi.fn();
			const setBracketModelsMock = vi.fn();

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					bracketModels: ["old-model", "other-model"],
					setBracketModels: setBracketModelsMock,
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(
				createMockArenaRunner({ handleSwapComplete: handleSwapCompleteMock }),
			);

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			// First call handleSwapModel to track the old model
			act(() => {
				result.current.handleSwapModel(0, 0, "A", "old-model");
			});

			// Then call handleSwapCompleteAndUpdate to replace it
			act(() => {
				result.current.handleSwapCompleteAndUpdate(0, 0, "A", "new-model");
			});

			expect(handleSwapCompleteMock).toHaveBeenCalledWith(
				0,
				0,
				"A",
				"new-model",
			);
			expect(setBracketModelsMock).toHaveBeenCalled();
			const bracketModelsCall = setBracketModelsMock.mock.calls[0];
			expect(bracketModelsCall?.[0]).toBeInstanceOf(Function);
			const updaterFn = bracketModelsCall?.[0] as (prev: string[]) => string[];
			const updatedModels = updaterFn(["old-model", "other-model"]);
			expect(updatedModels).toEqual(["new-model", "other-model"]);
		});
	});

	describe("handlePersonaChange", () => {
		it("updates slot personaId and personaPrompt", () => {
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "model-a",
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
			const roundsRef = { current: rounds };
			const setRoundsMock = vi.fn((arg) => {
				if (typeof arg === "function") {
					const result = arg(rounds);
					roundsRef.current = result;
				} else {
					roundsRef.current = arg;
				}
			});

			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					rounds,
					setRounds: setRoundsMock,
					roundsRef,
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handlePersonaChange(
					0,
					0,
					"A",
					"persona-1",
					"Test persona prompt",
				);
			});

			expect(setRoundsMock).toHaveBeenCalled();
			expect(roundsRef.current[0].matchups[0].slotA?.personaId).toBe(
				"persona-1",
			);
			expect(roundsRef.current[0].matchups[0].slotA?.personaPrompt).toBe(
				"Test persona prompt",
			);
		});
	});

	describe("computed values", () => {
		it("isRunning when runningModels has entries", () => {
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					runningModels: new Set(["model-a", "model-b"]),
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			expect(result.current.isRunning).toBe(true);
		});

		it("arenaIcon is Swords for competition mode", () => {
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					arenaMode: "competition",
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			expect(result.current.arenaIcon).toBeDefined();
			// Check that it's the Swords icon by checking displayName
			expect(result.current.arenaIcon.displayName).toBe("Swords");
		});

		it("arenaIcon is GitCompare for compare mode", () => {
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					arenaMode: "compare",
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			expect(result.current.arenaIcon.displayName).toBe("GitCompare");
		});

		it("buttonLabel: 'Stop' when running, 'Run Arena' in setup, null otherwise", () => {
			// Test "Stop" when running
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					runningModels: new Set(["model-a"]),
					phase: "running",
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result: result1 } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});
			expect(result1.current.buttonLabel).toBe("Stop All");

			// Test "Run Arena" in setup
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					runningModels: new Set(),
					phase: "setup",
				}),
			);
			const { result: result2 } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});
			expect(result2.current.buttonLabel).toBe("Run Arena");

			// Test null in voting phase
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					runningModels: new Set(),
					phase: "voting",
				}),
			);
			const { result: result3 } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});
			expect(result3.current.buttonLabel).toBeNull();
		});

		it("showResponseGrid is false in setup, true otherwise", () => {
			// Test setup phase
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "setup",
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result: result1 } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});
			expect(result1.current.showResponseGrid).toBe(false);

			// Test running phase
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "running",
				}),
			);
			const { result: result2 } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});
			expect(result2.current.showResponseGrid).toBe(true);

			// Test voting phase
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					phase: "voting",
				}),
			);
			const { result: result3 } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});
			expect(result3.current.showResponseGrid).toBe(true);
		});
	});

	describe("roundLabel helper", () => {
		it("returns 'Generation' for compare mode", () => {
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					arenaMode: "compare",
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			expect(result.current.roundLabel(0, 1)).toBe("Generation");
		});

		it("returns 'Final' for last round in competition", () => {
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					arenaMode: "competition",
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			expect(result.current.roundLabel(1, 2)).toBe("Final");
		});

		it("returns 'Semifinals' for second-to-last round", () => {
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					arenaMode: "competition",
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			expect(result.current.roundLabel(0, 2)).toBe("Semifinals");
		});

		it("returns 'Round N' for other rounds", () => {
			vi.mocked(useArenaState).mockReturnValue(
				createMockArenaState({
					arenaMode: "competition",
				}),
			);
			vi.mocked(useArenaRunner).mockReturnValue(createMockArenaRunner());

			const { result } = renderHook(() => useArena(), {
				wrapper: createWrapper(),
			});

			expect(result.current.roundLabel(0, 4)).toBe("Round 1");
		});
	});
});
