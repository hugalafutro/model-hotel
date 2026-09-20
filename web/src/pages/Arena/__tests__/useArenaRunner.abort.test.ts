import { act, renderHook, waitFor } from "@testing-library/react";
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
				expect(result.current.abortMapRef.current.has("P/model-a")).toBe(true),
			);
			const roundsWrites = vi.mocked(deps.setRounds).mock.calls.length;
			act(() => {
				result.current.abortMapRef.current.get("P/model-a")?.abort();
			});
			release();
			await waitFor(() =>
				expect(result.current.abortMapRef.current.has("P/model-a")).toBe(false),
			);

			expect(toastMock.mock.calls).toEqual([]);
			expect(vi.mocked(deps.setRounds).mock.calls.length).toBe(roundsWrites);
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
				result.current.abortMapRef.current.get("P/model-a")?.abort();
			});
			feed?.enqueue(delta("late"));
			feed?.close();
			await waitFor(() =>
				expect(result.current.abortMapRef.current.has("P/model-a")).toBe(false),
			);

			expect(vi.mocked(deps.setRounds).mock.calls.length).toBe(roundsWrites);
			expect(toastMock).not.toHaveBeenCalled();
		});
	});
});
