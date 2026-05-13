import { act, renderHook } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { GenerationParams } from "../../../api/types";
import type { ArenaSubMode } from "../../../context/SidebarModeContext";
import type { useToast } from "../../../context/ToastContext";
import { server } from "../../../test/mocks/server";
import type { BracketRound } from "../types";
import { useArenaRunner } from "../useArenaRunner";

const createWrapper = () => {
	return function Wrapper({ children }: { children: React.ReactNode }) {
		return children;
	};
};

const createMockDeps = (
	overrides?: Partial<Parameters<typeof useArenaRunner>[0]>,
) => {
	const providedRoundsRef = overrides?.roundsRef;
	const roundsRef = providedRoundsRef ?? { current: [] as BracketRound[] };
	const setRoundsMock = vi.fn((fn) => {
		if (typeof fn === "function") {
			const result = fn(roundsRef.current);
			roundsRef.current = result;
		}
	});
	const baseDeps: Parameters<typeof useArenaRunner>[0] = {
		arenaModeRef: { current: "compare" as ArenaSubMode },
		savedPrompt: "Test prompt",
		prompt: "Test prompt",
		setRounds: setRoundsMock,
		setPhase: vi.fn(),
		setRunningModels: vi.fn(),
		rounds: [],
		roundsRef,
		modelParams: {},
		enabledModels: [],
		toast: vi.fn() as ReturnType<typeof useToast>["toast"],
		...overrides,
	};
	return baseDeps;
};

describe("useArenaRunner", () => {
	it("returns all expected methods", () => {
		const deps = createMockDeps();
		const { result } = renderHook(() => useArenaRunner(deps), {
			wrapper: createWrapper(),
		});

		expect(result.current.streamModel).toBeDefined();
		expect(result.current.runRound).toBeDefined();
		expect(result.current.handleStopAll).toBeDefined();
		expect(result.current.handleRetry).toBeDefined();
		expect(result.current.handleCancelSlot).toBeDefined();
		expect(result.current.handleSwapComplete).toBeDefined();
		expect(result.current.abortMapRef).toBeDefined();
	});

	it("initializes with empty abort map", () => {
		const deps = createMockDeps();
		const { result } = renderHook(() => useArenaRunner(deps), {
			wrapper: createWrapper(),
		});

		expect(result.current.abortMapRef.current.size).toBe(0);
	});

	describe("streamModel", () => {
		it("streams response and updates rounds", async () => {
			server.use(
				http.post("/api/chat/arena", async () => {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
								),
							);
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":" world"}}]}\n\n',
								),
							);
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}\n\n',
								),
							);
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
							controller.close();
						},
					});
					return new Response(stream, {
						headers: { "Content-Type": "text/event-stream" },
					});
				}),
			);

			const setRoundsMock = vi.fn();
			const setPhaseMock = vi.fn();
			const arenaModeRef = { current: "compare" as ArenaSubMode };
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

			// Mock setRunningModels to invoke the callback with empty set
			const setRunningModelsMock = vi.fn((fn) => {
				if (typeof fn === "function") {
					fn(new Set());
				}
			});

			const deps = createMockDeps({
				rounds,
				roundsRef: { current: rounds },
				setRounds: setRoundsMock,
				setRunningModels: setRunningModelsMock,
				setPhase: setPhaseMock,
				arenaModeRef,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel(
					"model-a",
					"system prompt",
					"user prompt",
					0,
					"A",
					0,
				);
			});

			expect(setRoundsMock).toHaveBeenCalled();
			expect(setRunningModelsMock).toHaveBeenCalled();
			expect(setPhaseMock).toHaveBeenCalledWith("finished");
		});

		it("handles stream error and shows toast", async () => {
			server.use(
				http.post("/api/chat/arena", () => {
					return HttpResponse.json({ error: "Failed" }, { status: 500 });
				}),
			);

			const toastMock = vi.fn();
			const setRoundsMock = vi.fn();
			const setRunningModelsMock = vi.fn();

			const deps = createMockDeps({
				toast: toastMock as ReturnType<typeof useToast>["toast"],
				setRounds: setRoundsMock,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("model-a", "", "prompt", 0, "A", 0);
			});

			expect(toastMock).toHaveBeenCalled();
			expect(setRoundsMock).toHaveBeenCalled();
		});

		it("streamModel updates response content in rounds", async () => {
			server.use(
				http.post("/api/chat/arena", async () => {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
								),
							);
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":" world"}}]}\n\n',
								),
							);
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
							controller.close();
						},
					});
					return new Response(stream, {
						headers: { "Content-Type": "text/event-stream" },
					});
				}),
			);

			const now = Date.now();
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
							responseA: {
								model: "model-a",
								rawContent: "",
								content: "",
								thinkingContent: "",
								startTimeMs: now,
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
			const roundsRef = { current: rounds };
			const setRunningModelsMock = vi.fn((fn) => {
				if (typeof fn === "function") {
					fn(new Set());
				}
			});

			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel(
					"model-a",
					"system prompt",
					"user prompt",
					0,
					"A",
					0,
				);
			});

			// Verify the immer produce() path was exercised - response content was updated
			expect(roundsRef.current[0].matchups[0].responseA).toBeDefined();
			expect(roundsRef.current[0].matchups[0].responseA?.content).toBe(
				"Hello world",
			);
			expect(roundsRef.current[0].matchups[0].responseA?.done).toBe(true);
		});

		it("streamModel sets truncationError when stream ends without [DONE]", async () => {
			server.use(
				http.post("/api/chat/arena", async () => {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"Partial"}}]}\n\n',
								),
							);
							// No [DONE] sentinel - stream closes without it
							controller.close();
						},
					});
					return new Response(stream, {
						headers: { "Content-Type": "text/event-stream" },
					});
				}),
			);

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
			const setRunningModelsMock = vi.fn((fn) => {
				if (typeof fn === "function") {
					fn(new Set());
				}
			});

			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("model-a", "", "prompt", 0, "A", 0);
			});

			// Verify truncation error was set when stream ended without [DONE]
			const response = roundsRef.current[0].matchups[0].responseA;
			expect(response).toBeDefined();
			expect(response?.error).toMatch(/Stream was cut off|incomplete/);
			expect(response?.done).toBe(true);
		});

		it("streamModel records metrics on completion", async () => {
			server.use(
				http.post("/api/chat/arena", async () => {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"Hello"}}]}\n\n',
								),
							);
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":" world"}}]}\n\n',
								),
							);
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}\n\n',
								),
							);
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
							controller.close();
						},
					});
					return new Response(stream, {
						headers: { "Content-Type": "text/event-stream" },
					});
				}),
			);

			const now = Date.now();
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
							responseA: {
								model: "model-a",
								rawContent: "",
								content: "",
								thinkingContent: "",
								startTimeMs: now,
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
			const roundsRef = { current: rounds };
			const setRunningModelsMock = vi.fn((fn) => {
				if (typeof fn === "function") {
					fn(new Set());
				}
			});

			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("model-a", "", "prompt", 0, "A", 0);
			});

			// Verify metrics were recorded in the response
			const response = roundsRef.current[0].matchups[0].responseA;
			expect(response).toBeDefined();
			expect(response?.metrics).toBeDefined();
			expect(response?.metrics?.promptTokens).toBe(10);
			expect(response?.metrics?.completionTokens).toBe(5);
			expect(response?.metrics?.durationMs).toBeGreaterThan(0);
			expect(response?.metrics?.tokensPerSecond).toBeGreaterThan(0);
		});

		it("aborts on signal abort", async () => {
			const abortCtrl = new AbortController();
			const deps = createMockDeps();

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.abortMapRef.current.set("model-a", abortCtrl);
			});

			act(() => {
				result.current.handleStopAll();
			});

			expect(abortCtrl.signal.aborted).toBe(true);
			expect(result.current.abortMapRef.current.size).toBe(0);
		});

		it("handles retry for a slot", () => {
			const setRoundsMock = vi.fn();
			const setRunningModelsMock = vi.fn();
			const setPhaseMock = vi.fn();
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
							responseA: {
								model: "model-a",
								rawContent: "",
								content: "",
								thinkingContent: "",
								startTimeMs: 0,
								done: true,
								error: "Error occurred",
								metrics: null,
							},
							responseB: null,
							vote: null,
						},
					],
				},
			];

			const deps = createMockDeps({
				rounds,
				roundsRef: { current: rounds },
				setRounds: setRoundsMock,
				setRunningModels: setRunningModelsMock,
				setPhase: setPhaseMock,
				savedPrompt: "retry prompt",
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleRetry(0, 0, "A");
			});

			expect(setRoundsMock).toHaveBeenCalled();
			expect(setRunningModelsMock).toHaveBeenCalled();
			expect(setPhaseMock).toHaveBeenCalledWith("running");
		});

		it("handleRetry resets response to empty", () => {
			const setRunningModelsMock = vi.fn();
			const setPhaseMock = vi.fn();
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
							responseA: {
								model: "model-a",
								rawContent: "old content",
								content: "old content",
								thinkingContent: "old thinking",
								startTimeMs: 0,
								done: true,
								error: "Error occurred",
								metrics: null,
							},
							responseB: null,
							vote: null,
						},
					],
				},
			];
			const roundsRef = { current: rounds };

			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: setRunningModelsMock,
				setPhase: setPhaseMock,
				savedPrompt: "retry prompt",
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleRetry(0, 0, "A");
			});

			// Verify the immer produce() path was exercised - response was reset
			const response = roundsRef.current[0].matchups[0].responseA;
			expect(response).toBeDefined();
			expect(response?.content).toBe("");
			expect(response?.done).toBe(false);
			expect(response?.error).toBeNull();
			expect(response?.rawContent).toBe("");
			expect(response?.thinkingContent).toBe("");
		});

		it("handles cancel for a slot", () => {
			const abortCtrl = new AbortController();
			const setRoundsMock = vi.fn();
			const setRunningModelsMock = vi.fn((fn) => fn(new Set(["model-a"])));

			const deps = createMockDeps({
				setRounds: setRoundsMock,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.abortMapRef.current.set("model-a", abortCtrl);
			});

			act(() => {
				result.current.handleCancelSlot(0, 0, "A", "model-a");
			});

			expect(abortCtrl.signal.aborted).toBe(true);
			expect(setRoundsMock).toHaveBeenCalled();
		});

		it("handleCancelSlot nulls slot and response", () => {
			const abortCtrl = new AbortController();
			const setRunningModelsMock = vi.fn((fn) => fn(new Set(["model-a"])));
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
							responseA: {
								model: "model-a",
								rawContent: "content",
								content: "content",
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
			const roundsRef = { current: rounds };

			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.abortMapRef.current.set("model-a", abortCtrl);
			});

			act(() => {
				result.current.handleCancelSlot(0, 0, "A", "model-a");
			});

			// Verify the immer produce() path was exercised - slot and response were nulled
			expect(roundsRef.current[0].matchups[0].slotA).toBeNull();
			expect(roundsRef.current[0].matchups[0].responseA).toBeNull();
		});

		it("handles swap complete for a slot", () => {
			const setRoundsMock = vi.fn();
			const setRunningModelsMock = vi.fn();
			const setPhaseMock = vi.fn();
			const modelParams: Record<string, GenerationParams> = {
				"new-model": { temperature: 0.7 },
			};

			const deps = createMockDeps({
				setRounds: setRoundsMock,
				setRunningModels: setRunningModelsMock,
				setPhase: setPhaseMock,
				modelParams,
				savedPrompt: "swap prompt",
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleSwapComplete(0, 0, "A", "new-model");
			});

			expect(setRoundsMock).toHaveBeenCalled();
			expect(setRunningModelsMock).toHaveBeenCalled();
			expect(setPhaseMock).toHaveBeenCalledWith("running");
		});

		it("handleSwapComplete replaces slot model and resets response", () => {
			const setRunningModelsMock = vi.fn();
			const setPhaseMock = vi.fn();
			const modelParams: Record<string, GenerationParams> = {
				"new-model": { temperature: 0.7, max_tokens: 100 },
			};
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "old-model",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: null,
							responseA: {
								model: "old-model",
								rawContent: "old content",
								content: "old content",
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
			const roundsRef = { current: rounds };

			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: setRunningModelsMock,
				setPhase: setPhaseMock,
				modelParams,
				savedPrompt: "swap prompt",
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleSwapComplete(0, 0, "A", "new-model");
			});

			// Verify the immer produce() path was exercised - slot model replaced and response reset
			expect(roundsRef.current[0].matchups[0].slotA?.modelId).toBe("new-model");
			const response = roundsRef.current[0].matchups[0].responseA;
			expect(response).toBeDefined();
			expect(response?.model).toBe("new-model");
			expect(response?.content).toBe("");
			expect(response?.done).toBe(false);
			expect(response?.error).toBeNull();
		});
	});

	describe("runRound", () => {
		it("runs a round and streams all slots", () => {
			const setRoundsMock = vi.fn();
			const setPhaseMock = vi.fn();
			const setRunningModelsMock = vi.fn();
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

			const deps = createMockDeps({
				rounds,
				roundsRef: { current: rounds },
				setRounds: setRoundsMock,
				setPhase: setPhaseMock,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.runRound(0);
			});

			expect(setPhaseMock).toHaveBeenCalledWith("running");
			expect(setRunningModelsMock).toHaveBeenCalled();
			expect(setRoundsMock).toHaveBeenCalled();
		});

		it("does not run if round does not exist", () => {
			const setRoundsMock = vi.fn();
			const setPhaseMock = vi.fn();

			const deps = createMockDeps({
				rounds: [],
				roundsRef: { current: [] },
				setRounds: setRoundsMock,
				setPhase: setPhaseMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.runRound(0);
			});

			expect(setPhaseMock).not.toHaveBeenCalled();
			expect(setRoundsMock).not.toHaveBeenCalled();
		});

		it("uses savedPrompt if available", () => {
			const setRoundsMock = vi.fn();
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

			const deps = createMockDeps({
				rounds,
				roundsRef: { current: rounds },
				setRounds: setRoundsMock,
				savedPrompt: "saved prompt",
				prompt: "current prompt",
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.runRound(0);
			});

			expect(setRoundsMock).toHaveBeenCalled();
		});
	});

	describe("handleStopAll", () => {
		it("aborts all running models", () => {
			const abortCtrlA = new AbortController();
			const abortCtrlB = new AbortController();
			const setRoundsMock = vi.fn();
			const setPhaseMock = vi.fn();
			const setRunningModelsMock = vi.fn();

			const deps = createMockDeps({
				setRounds: setRoundsMock,
				setPhase: setPhaseMock,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.abortMapRef.current.set("model-a", abortCtrlA);
				result.current.abortMapRef.current.set("model-b", abortCtrlB);
			});

			act(() => {
				result.current.handleStopAll();
			});

			expect(abortCtrlA.signal.aborted).toBe(true);
			expect(abortCtrlB.signal.aborted).toBe(true);
			expect(result.current.abortMapRef.current.size).toBe(0);
			expect(setRunningModelsMock).toHaveBeenCalledWith(new Set());
			expect(setPhaseMock).toHaveBeenCalledWith("finished");
		});

		it("marks partially streamed responses as done", () => {
			const setRoundsMock = vi.fn();
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
							responseA: {
								model: "model-a",
								rawContent: "partial",
								content: "partial",
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

			const deps = createMockDeps({
				rounds,
				roundsRef: { current: rounds },
				setRounds: setRoundsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleStopAll();
			});

			expect(setRoundsMock).toHaveBeenCalled();
		});

		it("handleStopAll marks incomplete responses as done with error", () => {
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
							responseA: {
								model: "model-a",
								rawContent: "partial content",
								content: "partial content",
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
			const roundsRef = { current: rounds };

			const deps = createMockDeps({
				rounds,
				roundsRef,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleStopAll();
			});

			// Verify the immer produce() path was exercised - response was marked done
			expect(roundsRef.current[0].matchups[0].responseA?.done).toBe(true);
		});

		it("sets phase to voting in competition mode", () => {
			const setPhaseMock = vi.fn();
			const deps = createMockDeps({
				arenaModeRef: { current: "competition" as ArenaSubMode },
				setPhase: setPhaseMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.handleStopAll();
			});

			expect(setPhaseMock).toHaveBeenCalledWith("voting");
		});
	});

	describe("integration with API", () => {
		it("handles successful arena streaming", async () => {
			server.use(
				http.post("/api/chat/arena", async () => {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"Response"}}]}\n\n',
								),
							);
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}\n\n',
								),
							);
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
							controller.close();
						},
					});
					return new Response(stream, {
						headers: { "Content-Type": "text/event-stream" },
					});
				}),
			);

			const setRoundsMock = vi.fn();
			const setRunningModelsMock = vi.fn((fn) => fn(new Set()));
			const setPhaseMock = vi.fn();
			const toastMock = vi.fn();

			const deps = createMockDeps({
				setRounds: setRoundsMock,
				setRunningModels: setRunningModelsMock,
				setPhase: setPhaseMock,
				toast: toastMock as ReturnType<typeof useToast>["toast"],
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("model-a", "", "test prompt", 0, "A", 0);
			});

			expect(toastMock).not.toHaveBeenCalled();
			expect(setPhaseMock).toHaveBeenCalledWith("finished");
		});

		it("includes model params in API request", async () => {
			let capturedBody: Record<string, unknown> | null = null;
			server.use(
				http.post("/api/chat/arena", async ({ request }) => {
					capturedBody = await request.json();
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode('data: {"choices":[{"delta":{}}]}\n\n'),
							);
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
							controller.close();
						},
					});
					return new Response(stream, {
						headers: { "Content-Type": "text/event-stream" },
					});
				}),
			);

			const modelParams: Record<string, GenerationParams> = {
				"model-a": { temperature: 0.8, max_tokens: 500 },
			};
			const deps = createMockDeps({
				modelParams,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("model-a", "", "prompt", 0, "A", 0, {
					temperature: 0.8,
					max_tokens: 500,
				});
			});

			expect(capturedBody?.temperature).toBe(0.8);
			expect(capturedBody?.max_tokens).toBe(500);
		});

		it("handles thinking content in stream", async () => {
			server.use(
				http.post("/api/chat/arena", async () => {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"reasoning_content":"Thinking..."}}]}\n\n',
								),
							);
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"Response"}}]}\n\n',
								),
							);
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
							controller.close();
						},
					});
					return new Response(stream, {
						headers: { "Content-Type": "text/event-stream" },
					});
				}),
			);

			const setRoundsMock = vi.fn();
			const deps = createMockDeps({
				setRounds: setRoundsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("model-a", "", "prompt", 0, "A", 0);
			});

			expect(setRoundsMock).toHaveBeenCalled();
		});
	});
});
