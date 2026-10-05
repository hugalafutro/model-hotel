import { act, renderHook, waitFor } from "@testing-library/react";
import i18next from "i18next";
import { HttpResponse, http } from "msw";
import { describe, expect, it, vi } from "vitest";
import type { useToast } from "../../../context/ToastContext";
import { server } from "../../../test/mocks/server";
import { useArenaRunner } from "../useArenaRunner";
import { createMockDeps, createWrapper } from "./useArenaRunner.test.helpers";

// The user's own Stop and Cancel: an aborted stream leaves the slot as the
// handler that aborted settled it, and says nothing.
describe("useArenaRunner abort", () => {
	describe("streamModel", () => {
		it("stays quiet when the user aborts the stream", async () => {
			// Stop and Cancel abort the fetch; the handler that aborted has
			// settled the slot already, so the stream must not stamp an error
			// or toast on top of it.
			let release: () => void = () => {};
			server.use(
				http.post("/api/chat/arena", async () => {
					await new Promise<void>((resolve) => {
						release = resolve;
					});
					return HttpResponse.json({ error: "too late" }, { status: 500 });
				}),
			);
			const toastMock = vi.fn();
			const deps = createMockDeps({
				toast: toastMock as ReturnType<typeof useToast>["toast"],
			});
			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});
			await waitFor(() =>
				expect(result.current.abortMapRef.current.has("0:0:A")).toBe(true),
			);
			const roundsWrites = vi.mocked(deps.setRounds).mock.calls.length;
			act(() => {
				result.current.abortMapRef.current.get("0:0:A")?.abort();
			});
			release();
			await waitFor(() =>
				expect(result.current.abortMapRef.current.has("0:0:A")).toBe(false),
			);

			expect(toastMock.mock.calls).toEqual([]);
			expect(vi.mocked(deps.setRounds).mock.calls.length).toBe(roundsWrites);
		});

		it("names the side, not the model, in a competition retry toast", async () => {
			// Competition replies stream blind; a transient 503 fires the retry
			// toast while the card still hides its model, so the toast must too.
			// Slot A sits on the right of a flipped matchup, so it is "B" to the user.
			let calls = 0;
			server.use(
				http.post("/api/chat/arena", () => {
					calls += 1;
					return HttpResponse.json({ error: "busy" }, { status: 503 });
				}),
			);
			const toastMock = vi.fn();
			const deps = createMockDeps({
				arenaModeRef: { current: "competition" },
				roundsRef: {
					current: [
						{
							matchups: [
								{
									slotA: null,
									slotB: null,
									responseA: null,
									responseB: null,
									vote: null,
									flipped: true,
								},
							],
						},
					],
				},
				toast: toastMock as ReturnType<typeof useToast>["toast"],
			});
			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});
			await waitFor(() => expect(toastMock).toHaveBeenCalled());
			expect(calls).toBe(1);
			const [message, kind] = toastMock.mock.calls[0] as [string, string];
			expect(kind).toBe("info");
			expect(message).not.toContain("P/model-a");
			expect(message).toContain(i18next.t("chat.controls.modelB"));
			// End the backoff wait: the user's Stop aborts the retried fetch.
			act(() => {
				result.current.abortMapRef.current.get("0:0:A")?.abort();
			});
			await waitFor(() =>
				expect(result.current.abortMapRef.current.size).toBe(0),
			);
		});

		it("lets a stream cancelled during retry backoff wake without unsettling its replacement", async () => {
			// The retry sleep ignores the abort, so a cancelled stream wakes up
			// to a second time later. If the slot was swapped in the meantime,
			// the new stream owns the key: the stale finally must leave it alone.
			let calls = 0;
			let release: () => void = () => {};
			server.use(
				http.post("/api/chat/arena", async () => {
					calls += 1;
					if (calls === 1) {
						return HttpResponse.json({ error: "busy" }, { status: 503 });
					}
					await new Promise<void>((resolve) => {
						release = resolve;
					});
					return HttpResponse.json({ error: "too late" }, { status: 500 });
				}),
			);
			const setRunningModels = vi.fn();
			const deps = createMockDeps({ setRunningModels });
			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});
			// The 503 landed and the retry toast fired; the stream is now asleep.
			await waitFor(() => expect(deps.toast).toHaveBeenCalled());
			act(() => {
				result.current.handleCancelSlot(0, 0, "A");
			});
			act(() => {
				result.current.streamModel("P/model-b", "", "prompt", 0, "A", 0);
			});
			await waitFor(() => expect(calls).toBe(2));
			const replacement = result.current.abortMapRef.current.get("0:0:A");
			expect(replacement).toBeDefined();
			const settles = setRunningModels.mock.calls.length;

			// Outlast the longest first backoff (1 s ± 25 %): the stale stream
			// wakes, its fetch rejects on the aborted signal, its finally runs.
			await new Promise((resolve) => setTimeout(resolve, 1400));
			expect(result.current.abortMapRef.current.get("0:0:A")).toBe(replacement);
			expect(setRunningModels.mock.calls.length).toBe(settles);

			release();
			await waitFor(() =>
				expect(result.current.abortMapRef.current.size).toBe(0),
			);
		});

		it("cancels one slot of a model streaming in both, the other keeps going", async () => {
			// A blind swap may put the opponent's model into the other slot, so
			// one model can stream twice in a matchup. Streams are tracked by
			// slot, not model: Cancel on A must abort A's fetch only.
			const releases: Array<() => void> = [];
			server.use(
				http.post("/api/chat/arena", async () => {
					await new Promise<void>((resolve) => {
						releases.push(resolve);
					});
					return HttpResponse.json({ error: "too late" }, { status: 500 });
				}),
			);
			const deps = createMockDeps();
			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});

			act(() => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
				result.current.streamModel("P/model-a", "", "prompt", 0, "B", 0);
			});
			await waitFor(() => expect(releases.length).toBe(2));
			const ctrlB = result.current.abortMapRef.current.get("0:0:B");
			expect(result.current.abortMapRef.current.size).toBe(2);

			act(() => {
				result.current.handleCancelSlot(0, 0, "A");
			});

			expect(result.current.abortMapRef.current.has("0:0:A")).toBe(false);
			expect(result.current.abortMapRef.current.get("0:0:B")).toBe(ctrlB);
			expect(ctrlB?.signal.aborted).toBe(false);
			for (const release of releases) release();
			await waitFor(() =>
				expect(result.current.abortMapRef.current.size).toBe(0),
			);
		});

		it("leaves a cancelled slot as the cancel left it", async () => {
			// An abort mid-stream ends the read without a throw; the completion
			// patch must not rebuild the slot Cancel just cleared. The stream is
			// fed by hand so a read can complete after the abort, which is how
			// the SSE reader notices the signal.
			const encoder = new TextEncoder();
			const delta = (text: string) =>
				encoder.encode(
					`data: {"choices":[{"delta":{"content":"${text}"}}]}\n\n`,
				);
			let feed: ReadableStreamDefaultController<Uint8Array> | undefined;
			server.use(
				http.post(
					"/api/chat/arena",
					() =>
						new HttpResponse(
							new ReadableStream<Uint8Array>({
								start(controller) {
									feed = controller;
									controller.enqueue(delta("partial"));
								},
							}),
							{ headers: { "Content-Type": "text/event-stream" } },
						),
				),
			);
			const toastMock = vi.fn();
			const deps = createMockDeps({
				toast: toastMock as ReturnType<typeof useToast>["toast"],
			});
			const { result } = renderHook(() => useArenaRunner(deps), {
				wrapper: createWrapper(),
			});
			act(() => {
				result.current.streamModel("P/model-a", "", "prompt", 0, "A", 0);
			});
			// The first delta landed.
			await waitFor(() =>
				expect(vi.mocked(deps.setRounds).mock.calls.length).toBeGreaterThan(0),
			);
			const roundsWrites = vi.mocked(deps.setRounds).mock.calls.length;
			act(() => {
				result.current.abortMapRef.current.get("0:0:A")?.abort();
			});
			feed?.enqueue(delta("late"));
			feed?.close();
			await waitFor(() =>
				expect(result.current.abortMapRef.current.has("0:0:A")).toBe(false),
			);

			expect(vi.mocked(deps.setRounds).mock.calls.length).toBe(roundsWrites);
			expect(toastMock).not.toHaveBeenCalled();
		});
	});
});
