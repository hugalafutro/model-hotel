import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useCopyToClipboard } from "../useCopyToClipboard";

describe("useCopyToClipboard", () => {
	const writeText = navigator.clipboard.writeText as ReturnType<typeof vi.fn>;

	beforeEach(() => {
		vi.clearAllMocks();
		writeText.mockResolvedValue(undefined);
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("writes to the clipboard and reports success", async () => {
		const { result } = renderHook(() => useCopyToClipboard());
		expect(result.current.copied).toBe(false);

		let ok: boolean | undefined;
		await act(async () => {
			ok = await result.current.copy("hello");
		});

		expect(ok).toBe(true);
		expect(writeText).toHaveBeenCalledWith("hello");
		expect(result.current.copied).toBe(true);
	});

	it("reverts copied after the reset delay", async () => {
		vi.useFakeTimers();
		const { result } = renderHook(() =>
			useCopyToClipboard({ resetAfterMs: 50 }),
		);

		await act(async () => {
			await result.current.copy("hello");
		});
		expect(result.current.copied).toBe(true);

		act(() => {
			vi.advanceTimersByTime(50);
		});
		expect(result.current.copied).toBe(false);
	});

	it("restarts the reset timer on a second copy", async () => {
		vi.useFakeTimers();
		const { result } = renderHook(() =>
			useCopyToClipboard({ resetAfterMs: 100 }),
		);

		await act(async () => {
			await result.current.copy("first");
		});
		act(() => {
			vi.advanceTimersByTime(80);
		});
		await act(async () => {
			await result.current.copy("second");
		});

		// The first timer would have fired at 100ms; the second copy replaced it.
		act(() => {
			vi.advanceTimersByTime(80);
		});
		expect(result.current.copied).toBe(true);

		act(() => {
			vi.advanceTimersByTime(20);
		});
		expect(result.current.copied).toBe(false);
	});

	it("clears the reset timer it scheduled on unmount", async () => {
		vi.useFakeTimers();
		const setSpy = vi.spyOn(globalThis, "setTimeout");
		const clearSpy = vi.spyOn(globalThis, "clearTimeout");
		const { result, unmount } = renderHook(() => useCopyToClipboard());

		await act(async () => {
			await result.current.copy("hello");
		});
		// The id of the reset timer, so the assertion pins this timer and not
		// whichever clearTimeout React or the fake-timer plumbing happens to call.
		const timerId = setSpy.mock.results.at(-1)?.value;
		expect(timerId).toBeDefined();
		unmount();

		expect(clearSpy).toHaveBeenCalledWith(timerId);
		expect(vi.getTimerCount()).toBe(0);
		setSpy.mockRestore();
		clearSpy.mockRestore();
	});

	it("schedules nothing when the component unmounts mid-write", async () => {
		vi.useFakeTimers();
		let settle: (() => void) | undefined;
		writeText.mockImplementation(
			() =>
				new Promise<void>((resolve) => {
					settle = resolve;
				}),
		);
		const { result, unmount } = renderHook(() => useCopyToClipboard());

		let pending: Promise<boolean> | undefined;
		act(() => {
			pending = result.current.copy("hello");
		});
		unmount();
		await act(async () => {
			settle?.();
			await pending;
		});

		// The write landed, so the copy still reports success, but nothing was
		// flagged and no timer outlived the cleanup that would have cleared it.
		expect(await pending).toBe(true);
		expect(result.current.copied).toBe(false);
		expect(vi.getTimerCount()).toBe(0);
	});

	it("skips the flag and the timer when trackCopied is false", async () => {
		vi.useFakeTimers();
		const { result } = renderHook(() =>
			useCopyToClipboard({ trackCopied: false }),
		);

		let ok: boolean | undefined;
		await act(async () => {
			ok = await result.current.copy("hello");
		});

		expect(ok).toBe(true);
		expect(writeText).toHaveBeenCalledWith("hello");
		expect(result.current.copied).toBe(false);
		expect(vi.getTimerCount()).toBe(0);
	});

	it("reports failure without throwing when the write rejects", async () => {
		writeText.mockRejectedValueOnce(new Error("denied"));
		const { result } = renderHook(() => useCopyToClipboard());

		let ok: boolean | undefined;
		await act(async () => {
			ok = await result.current.copy("hello");
		});

		expect(ok).toBe(false);
		expect(result.current.copied).toBe(false);
	});

	it("reports failure when the Clipboard API is missing", async () => {
		const original = navigator.clipboard;
		Object.defineProperty(navigator, "clipboard", {
			value: undefined,
			configurable: true,
			writable: true,
		});
		const { result } = renderHook(() => useCopyToClipboard());

		let ok: boolean | undefined;
		await act(async () => {
			ok = await result.current.copy("hello");
		});

		expect(ok).toBe(false);
		Object.defineProperty(navigator, "clipboard", {
			value: original,
			configurable: true,
			writable: true,
		});
	});

	it("uses a caller-supplied writer instead of the Clipboard API", async () => {
		const write = vi.fn().mockResolvedValue(undefined);
		const { result } = renderHook(() => useCopyToClipboard({ write }));

		let ok: boolean | undefined;
		await act(async () => {
			ok = await result.current.copy("hello");
		});

		expect(ok).toBe(true);
		expect(write).toHaveBeenCalledWith("hello");
		expect(writeText).not.toHaveBeenCalled();
	});

	it("reports failure when the caller-supplied writer throws", async () => {
		const write = vi.fn(() => {
			throw new Error("no clipboard");
		});
		const { result } = renderHook(() => useCopyToClipboard({ write }));

		let ok: boolean | undefined;
		await act(async () => {
			ok = await result.current.copy("hello");
		});

		expect(ok).toBe(false);
		expect(result.current.copied).toBe(false);
	});
});
