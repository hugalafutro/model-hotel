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

	it("clears the reset timer on unmount", async () => {
		vi.useFakeTimers();
		const clearSpy = vi.spyOn(globalThis, "clearTimeout");
		const { result, unmount } = renderHook(() => useCopyToClipboard());

		await act(async () => {
			await result.current.copy("hello");
		});
		unmount();

		expect(clearSpy).toHaveBeenCalled();
		clearSpy.mockRestore();
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
