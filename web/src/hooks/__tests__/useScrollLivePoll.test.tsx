import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useScrollLivePoll } from "../useScrollLivePoll";

function setHidden(hidden: boolean) {
	Object.defineProperty(document, "hidden", {
		configurable: true,
		get: () => hidden,
	});
	document.dispatchEvent(new Event("visibilitychange"));
}

describe("useScrollLivePoll", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
		setHidden(false);
	});

	it("polls on the interval while the tab is visible", () => {
		const fetchNewer = vi.fn();
		renderHook(() =>
			useScrollLivePoll({ enabled: true, fetchNewer, intervalMs: 1000 }),
		);

		act(() => {
			vi.advanceTimersByTime(2000);
		});

		expect(fetchNewer).toHaveBeenCalledTimes(2);
	});

	it("skips the poll while the tab is hidden", () => {
		const fetchNewer = vi.fn();
		renderHook(() =>
			useScrollLivePoll({ enabled: true, fetchNewer, intervalMs: 1000 }),
		);
		act(() => setHidden(true));

		act(() => {
			vi.advanceTimersByTime(3000);
		});

		expect(fetchNewer).not.toHaveBeenCalled();
	});

	it("refreshes when the tab comes back", () => {
		const fetchNewer = vi.fn();
		renderHook(() =>
			useScrollLivePoll({ enabled: true, fetchNewer, intervalMs: 60000 }),
		);

		act(() => setHidden(true));
		expect(fetchNewer).not.toHaveBeenCalled();

		act(() => setHidden(false));
		expect(fetchNewer).toHaveBeenCalledTimes(1);
	});

	it("does nothing while disabled", () => {
		const fetchNewer = vi.fn();
		renderHook(() =>
			useScrollLivePoll({ enabled: false, fetchNewer, intervalMs: 1000 }),
		);

		act(() => {
			vi.advanceTimersByTime(5000);
		});
		act(() => setHidden(false));

		expect(fetchNewer).not.toHaveBeenCalled();
	});
});
