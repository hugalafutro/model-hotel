import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useCopyToClipboard } from "../useCopyToClipboard";

// Installs a clipboard whose writeText is the given mock. configurable so each
// test can replace the previous one.
function stubClipboard(writeText: (text: string) => Promise<void>) {
	Object.defineProperty(navigator, "clipboard", {
		value: { writeText },
		configurable: true,
	});
	return writeText;
}

describe("useCopyToClipboard", () => {
	afterEach(() => {
		vi.useRealTimers();
	});

	it("writes the text and flags the copy until the timer reverts it", async () => {
		vi.useFakeTimers();
		const writeText = vi.fn().mockResolvedValue(undefined);
		stubClipboard(writeText);
		const { result } = renderHook(() => useCopyToClipboard());

		await act(async () => {
			await expect(result.current.copy("hello")).resolves.toBe(true);
		});
		expect(writeText).toHaveBeenCalledWith("hello");
		expect(result.current.copied).toBe(true);

		act(() => {
			vi.advanceTimersByTime(2000);
		});
		expect(result.current.copied).toBe(false);
	});

	it("honours a custom reset delay", async () => {
		vi.useFakeTimers();
		stubClipboard(vi.fn().mockResolvedValue(undefined));
		const { result } = renderHook(() =>
			useCopyToClipboard({ resetAfterMs: 500 }),
		);

		await act(async () => {
			await result.current.copy("hello");
		});
		act(() => {
			vi.advanceTimersByTime(499);
		});
		expect(result.current.copied).toBe(true);
		act(() => {
			vi.advanceTimersByTime(1);
		});
		expect(result.current.copied).toBe(false);
	});

	it("restarts the timer on a re-copy instead of letting the first one fire", async () => {
		vi.useFakeTimers();
		stubClipboard(vi.fn().mockResolvedValue(undefined));
		const { result } = renderHook(() => useCopyToClipboard());

		await act(async () => {
			await result.current.copy("one");
		});
		act(() => {
			vi.advanceTimersByTime(1500);
		});
		await act(async () => {
			await result.current.copy("two");
		});
		// The first timer would have fired here; the re-copy dropped it.
		act(() => {
			vi.advanceTimersByTime(1000);
		});
		expect(result.current.copied).toBe(true);
		act(() => {
			vi.advanceTimersByTime(1000);
		});
		expect(result.current.copied).toBe(false);
	});

	it("resolves false and flags nothing when the clipboard refuses", async () => {
		stubClipboard(vi.fn().mockRejectedValue(new Error("denied")));
		const { result } = renderHook(() => useCopyToClipboard());

		await act(async () => {
			await expect(result.current.copy("hello")).resolves.toBe(false);
		});
		expect(result.current.copied).toBe(false);
	});

	it("resolves false when there is no clipboard at all", async () => {
		Object.defineProperty(navigator, "clipboard", {
			value: undefined,
			configurable: true,
		});
		const { result } = renderHook(() => useCopyToClipboard());

		await act(async () => {
			await expect(result.current.copy("hello")).resolves.toBe(false);
		});
		expect(result.current.copied).toBe(false);
	});

	it("copies without flagging when trackCopied is off", async () => {
		vi.useFakeTimers();
		const writeText = vi.fn().mockResolvedValue(undefined);
		stubClipboard(writeText);
		const { result } = renderHook(() =>
			useCopyToClipboard({ trackCopied: false }),
		);

		await act(async () => {
			await expect(result.current.copy("hello")).resolves.toBe(true);
		});
		expect(writeText).toHaveBeenCalledWith("hello");
		expect(result.current.copied).toBe(false);
		// No timer was scheduled, so there is nothing left to fire.
		expect(vi.getTimerCount()).toBe(0);
	});

	it("stops at the write when the caller unmounts mid-copy", async () => {
		let release: (() => void) | undefined;
		const writeText = vi.fn(
			() =>
				new Promise<void>((resolve) => {
					release = resolve;
				}),
		);
		stubClipboard(writeText);
		const { result, unmount } = renderHook(() => useCopyToClipboard());

		let settled: boolean | undefined;
		void result.current.copy("hello").then((ok) => {
			settled = ok;
		});
		await waitFor(() => expect(release).toBeDefined());

		unmount();
		release?.();
		// The text still reached the clipboard, but nothing was flagged and no
		// reset timer outlived the component.
		await waitFor(() => expect(settled).toBe(true));
		expect(writeText).toHaveBeenCalledWith("hello");
		expect(result.current.copied).toBe(false);
	});
});
