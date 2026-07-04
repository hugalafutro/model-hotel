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
		enabledModels: [
			{ provider_name: "P", model_id: "model-a" },
			{ provider_name: "P", model_id: "model-b" },
			{ provider_name: "P", model_id: "new-model" },
		],
		modelsReady: true,
		toast: vi.fn() as ReturnType<typeof useToast>["toast"],
		...overrides,
	};
	return baseDeps;
};

describe("useArenaRunner", () => {
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
								modelId: "P/model-a",
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
					"P/model-a",
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
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});

			expect(toastMock).toHaveBeenCalled();
			expect(setRoundsMock).toHaveBeenCalled();
		});

		it("records the error and marks the response done when the stream fails mid-round", async () => {
			// Unlike the toast test above (which mocks setRounds away), apply the
			// produce to real rounds so the error path that stamps done+error+metrics
			// onto the matchup response is actually exercised.
			server.use(
				http.post("/api/chat/arena", () =>
					HttpResponse.json({ error: "Failed" }, { status: 500 }),
				),
			);

			const now = Date.now();
			const rounds: BracketRound[] = [
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
			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: vi.fn((fn) => {
					if (typeof fn === "function") fn(new Set());
				}),
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});

			expect(roundsRef.current[0].matchups[0].responseA?.done).toBe(true);
			expect(roundsRef.current[0].matchups[0].responseA?.error).toBeTruthy();
		});

		it("does not dispatch a slot model absent from the chat list", async () => {
			// A persisted competition can reload with a round slot pointing at a
			// model that is no longer a valid chat target. It must be stamped as an
			// errored slot instead of streaming a request to a non-chat endpoint.
			let hit = false;
			server.use(
				http.post("/api/chat/arena", () => {
					hit = true;
					return HttpResponse.json(
						{ error: "should not run" },
						{ status: 500 },
					);
				}),
			);

			const now = Date.now();
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "P/embedding-model",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: null,
							responseA: {
								model: "P/embedding-model",
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
			const setPhaseMock = vi.fn();
			// enabledModels (default) does not include "P/embedding-model".
			const deps = createMockDeps({
				rounds,
				roundsRef,
				setPhase: setPhaseMock,
				// Invoke the updater with the model still present so the guard's
				// running-set cleanup (delete + empty-set phase transition) runs.
				setRunningModels: vi.fn((fn) => {
					if (typeof fn === "function") fn(new Set(["P/embedding-model"]));
				}),
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel(
					"P/embedding-model",
					"",
					"prompt",
					0,
					"A",
					0,
				);
			});

			expect(hit).toBe(false);
			const response = roundsRef.current[0].matchups[0].responseA;
			expect(response?.done).toBe(true);
			expect(response?.error).toBeTruthy();
			// compare mode empties the running set -> back to "finished".
			expect(setPhaseMock).toHaveBeenCalledWith("finished");
		});

		it("defers (does not error) an unrecognised slot while models load", async () => {
			// While the chat list is still loading we can't classify the model, so
			// the pending response is cleared for retry rather than permanently
			// failed, and nothing is dispatched.
			let hit = false;
			server.use(
				http.post("/api/chat/arena", () => {
					hit = true;
					return new Response("data: [DONE]\n\n", {
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
			const deps = createMockDeps({
				// Not loaded yet, and no ids to validate against.
				modelsReady: false,
				enabledModels: [],
				rounds,
				roundsRef,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});

			expect(hit).toBe(false);
			// Pending response cleared (retryable), not stamped as an error.
			expect(roundsRef.current[0].matchups[0].responseA).toBeNull();
		});

		it("stamps a non-chat slot B and returns to voting in competition mode", () => {
			const setPhaseMock = vi.fn();
			const now = Date.now();
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: null,
							slotB: {
								modelId: "P/rerank-model",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: null,
							responseB: {
								model: "P/rerank-model",
								rawContent: "",
								content: "",
								thinkingContent: "",
								startTimeMs: now,
								done: false,
								error: null,
								metrics: null,
							},
							vote: null,
						},
					],
				},
			];
			const roundsRef = { current: rounds };
			const deps = createMockDeps({
				arenaModeRef: { current: "competition" as ArenaSubMode },
				rounds,
				roundsRef,
				setPhase: setPhaseMock,
				setRunningModels: vi.fn((fn) => {
					if (typeof fn === "function") fn(new Set(["P/rerank-model"]));
				}),
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.streamModel("P/rerank-model", "", "prompt", 0, "B", 0);
			});

			const response = roundsRef.current[0].matchups[0].responseB;
			expect(response?.done).toBe(true);
			expect(response?.error).toBeTruthy();
			// competition mode empties the running set -> back to "voting".
			expect(setPhaseMock).toHaveBeenCalledWith("voting");
		});

		it("blocks a persisted model while the chat list is empty", async () => {
			// An empty enabledModels (nothing recognised as a chat model) must not
			// leave the bypass open: a persisted slot model is stamped as errored
			// rather than dispatched to /api/chat/arena.
			let hit = false;
			server.use(
				http.post("/api/chat/arena", () => {
					hit = true;
					return new Response("data: [DONE]\n\n", {
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
								modelId: "P/embedding-model",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: null,
							responseA: {
								model: "P/embedding-model",
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
			const deps = createMockDeps({
				enabledModels: [],
				rounds,
				roundsRef,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel(
					"P/embedding-model",
					"",
					"prompt",
					0,
					"A",
					0,
				);
			});

			expect(hit).toBe(false);
			expect(roundsRef.current[0].matchups[0].responseA?.done).toBe(true);
			expect(roundsRef.current[0].matchups[0].responseA?.error).toBeTruthy();
		});

		it("tolerates an out-of-range matchup and a non-empty running set", () => {
			// Defensive edges of the guard: an unresolvable matchup index leaves the
			// rounds untouched, and while other models are still running the phase
			// is not flipped.
			const setPhaseMock = vi.fn();
			const rounds: BracketRound[] = [{ matchups: [] }];
			const roundsRef = { current: rounds };
			const deps = createMockDeps({
				rounds,
				roundsRef,
				setPhase: setPhaseMock,
				setRunningModels: vi.fn((fn) => {
					if (typeof fn === "function") {
						fn(new Set(["P/embedding-model", "P/model-a"]));
					}
				}),
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.streamModel(
					"P/embedding-model",
					"",
					"prompt",
					0,
					"A",
					5,
				);
			});

			// Non-existent matchup -> no response written.
			expect(roundsRef.current[0].matchups[5]).toBeUndefined();
			// Another model still running -> phase untouched.
			expect(setPhaseMock).not.toHaveBeenCalled();
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
					"P/model-a",
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

		it("accumulates reasoning deltas into thinkingContent", async () => {
			// Reasoning models stream chain-of-thought as delta.reasoning_content (or
			// the older delta.reasoning), which must accumulate into thinkingContent
			// separately from the visible answer content.
			server.use(
				http.post("/api/chat/arena", async () => {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"reasoning_content":"Let me "}}]}\n\n',
								),
							);
							// Older providers use `reasoning`; exercise the ?? fallback.
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"reasoning":"think."}}]}\n\n',
								),
							);
							controller.enqueue(
								encoder.encode(
									'data: {"choices":[{"delta":{"content":"Answer"}}]}\n\n',
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
			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: vi.fn((fn) => {
					if (typeof fn === "function") fn(new Set());
				}),
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});

			expect(roundsRef.current[0].matchups[0].responseA?.thinkingContent).toBe(
				"Let me think.",
			);
			expect(roundsRef.current[0].matchups[0].responseA?.content).toBe(
				"Answer",
			);
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
								modelId: "P/model-a",
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
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
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
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
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
				result.current.abortMapRef.current.set("P/model-a", abortCtrl);
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
								modelId: "P/model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: null,
							responseA: {
								model: "P/model-a",
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
			const setRunningModelsMock = vi.fn((fn) => fn(new Set(["P/model-a"])));

			const deps = createMockDeps({
				setRounds: setRoundsMock,
				setRunningModels: setRunningModelsMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.abortMapRef.current.set("P/model-a", abortCtrl);
			});

			act(() => {
				result.current.handleCancelSlot(0, 0, "A", "P/model-a");
			});

			expect(abortCtrl.signal.aborted).toBe(true);
			expect(setRoundsMock).toHaveBeenCalled();
		});

		it("handleCancelSlot transitions phase when last model is cancelled", () => {
			const abortCtrl = new AbortController();
			const setPhaseMock = vi.fn();
			// Simulate cancelling the only running model (set becomes empty after delete)
			const setRunningModelsMock = vi.fn((fn) => fn(new Set(["P/model-a"])));
			const rounds: BracketRound[] = [
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
				setRunningModels: setRunningModelsMock,
				setPhase: setPhaseMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.abortMapRef.current.set("P/model-a", abortCtrl);
			});

			act(() => {
				result.current.handleCancelSlot(0, 0, "A", "P/model-a");
			});

			// Phase should transition because runningModels becomes empty
			expect(setPhaseMock).toHaveBeenCalledWith("finished");
		});

		it("handleCancelSlot does not transition phase when other models still running", () => {
			const abortCtrl = new AbortController();
			const setPhaseMock = vi.fn();
			// Simulate cancelling one model while another is still running
			const setRunningModelsMock = vi.fn((fn) =>
				fn(new Set(["P/model-a", "P/model-b"])),
			);

			const deps = createMockDeps({
				setRunningModels: setRunningModelsMock,
				setPhase: setPhaseMock,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.abortMapRef.current.set("P/model-a", abortCtrl);
			});

			act(() => {
				result.current.handleCancelSlot(0, 0, "A", "P/model-a");
			});

			// Phase should NOT transition because model-b is still running
			expect(setPhaseMock).not.toHaveBeenCalled();
		});

		it("handleCancelSlot nulls slot and response", () => {
			const abortCtrl = new AbortController();
			const setRunningModelsMock = vi.fn((fn) => fn(new Set(["P/model-a"])));
			const rounds: BracketRound[] = [
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
				result.current.abortMapRef.current.set("P/model-a", abortCtrl);
			});

			act(() => {
				result.current.handleCancelSlot(0, 0, "A", "P/model-a");
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
				"P/new-model": { temperature: 0.7 },
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
				result.current.handleSwapComplete(0, 0, "A", "P/new-model");
			});

			expect(setRoundsMock).toHaveBeenCalled();
			expect(setRunningModelsMock).toHaveBeenCalled();
			expect(setPhaseMock).toHaveBeenCalledWith("running");
		});

		it("handleSwapComplete replaces slot model and resets response", () => {
			const setRunningModelsMock = vi.fn();
			const setPhaseMock = vi.fn();
			const modelParams: Record<string, GenerationParams> = {
				"P/new-model": { temperature: 0.7, max_tokens: 100 },
			};
			const rounds: BracketRound[] = [
				{
					matchups: [
						{
							slotA: {
								modelId: "P/old-model",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: null,
							responseA: {
								model: "P/old-model",
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
				result.current.handleSwapComplete(0, 0, "A", "P/new-model");
			});

			// Verify the immer produce() path was exercised - slot model replaced and response reset
			expect(roundsRef.current[0].matchups[0].slotA?.modelId).toBe(
				"P/new-model",
			);
			const response = roundsRef.current[0].matchups[0].responseA;
			expect(response).toBeDefined();
			expect(response?.model).toBe("P/new-model");
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
								modelId: "P/model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "P/model-b",
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

		it("initializes the round's matchup responses before streaming", async () => {
			// With setRounds applied to real rounds (not mocked away), the init
			// produce that maps initMatchupResponses over the round actually runs,
			// replacing the null slots with fresh ArenaResponse objects.
			server.use(
				http.post("/api/chat/arena", () => {
					const encoder = new TextEncoder();
					const stream = new ReadableStream({
						start(controller) {
							controller.enqueue(encoder.encode("data: [DONE]\n\n"));
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
								modelId: "P/model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "P/model-b",
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
			const deps = createMockDeps({
				rounds,
				roundsRef,
				setRunningModels: vi.fn((fn) => {
					if (typeof fn === "function") fn(new Set());
				}),
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.runRound(0);
			});

			expect(roundsRef.current[0].matchups[0].responseA).not.toBeNull();
			expect(roundsRef.current[0].matchups[0].responseB).not.toBeNull();
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
								modelId: "P/model-a",
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
				result.current.abortMapRef.current.set("P/model-a", abortCtrlA);
				result.current.abortMapRef.current.set("P/model-b", abortCtrlB);
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
								modelId: "P/model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: null,
							responseA: {
								model: "P/model-a",
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
								modelId: "P/model-a",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							slotB: {
								modelId: "P/model-b",
								personaId: null,
								personaPrompt: "",
								params: {},
							},
							responseA: {
								model: "P/model-a",
								rawContent: "partial content",
								content: "partial content",
								thinkingContent: "",
								startTimeMs: 0,
								done: false,
								error: null,
								metrics: null,
							},
							responseB: {
								model: "P/model-b",
								rawContent: "partial B",
								content: "partial B",
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

			// The immer produce() marks BOTH partially-streamed slots done (covering
			// the responseA and responseB branches), preserving their content.
			expect(roundsRef.current[0].matchups[0].responseA?.done).toBe(true);
			expect(roundsRef.current[0].matchups[0].responseB?.done).toBe(true);
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
				result.current.streamModel("P/model-a", "", "test prompt", 0, "A", 0);
			});

			expect(toastMock).not.toHaveBeenCalled();
			expect(setPhaseMock).toHaveBeenCalledWith("finished");
		});

		it("includes model params in API request", async () => {
			let capturedBody: Record<string, unknown> | null = null;
			server.use(
				http.post("/api/chat/arena", async ({ request }) => {
					capturedBody = (await request.json()) as Record<string, unknown>;
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
				"P/model-a": { temperature: 0.8, max_tokens: 500 },
			};
			const deps = createMockDeps({
				modelParams,
			});

			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			await act(async () => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0, {
					temperature: 0.8,
					max_tokens: 500,
				});
			});

			// TS cannot see the closure assignment above, so re-widen before narrowing
			const body = capturedBody as Record<string, unknown> | null;
			if (!body) throw new Error("request body was not captured");
			expect(body.temperature).toBe(0.8);
			expect(body.max_tokens).toBe(500);
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
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});

			expect(setRoundsMock).toHaveBeenCalled();
		});
	});
});
