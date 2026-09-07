import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useArmedConfirm } from "../useArmedConfirm";

describe("useArmedConfirm", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("arms on the first press and commits on the second", () => {
		const commit = vi.fn();
		const { result } = renderHook(() => useArmedConfirm<string>());

		act(() => result.current.fire("row-1", commit));
		expect(result.current.armed).toBe("row-1");
		expect(commit).not.toHaveBeenCalled();

		act(() => result.current.fire("row-1", commit));
		expect(commit).toHaveBeenCalledTimes(1);
		expect(result.current.armed).toBeNull();
	});

	it("disarms itself after the timeout", () => {
		const commit = vi.fn();
		const { result } = renderHook(() => useArmedConfirm<string>(3000));

		act(() => result.current.fire("row-1", commit));
		act(() => {
			vi.advanceTimersByTime(3000);
		});

		expect(result.current.armed).toBeNull();
		act(() => result.current.fire("row-1", commit));
		expect(commit).not.toHaveBeenCalled();
	});

	it("holds one key at a time", () => {
		const commit = vi.fn();
		const { result } = renderHook(() => useArmedConfirm<string>());

		act(() => result.current.fire("row-1", commit));
		act(() => result.current.fire("row-2", commit));

		expect(result.current.armed).toBe("row-2");
		expect(commit).not.toHaveBeenCalled();
	});

	it("disarms on demand", () => {
		const commit = vi.fn();
		const { result } = renderHook(() => useArmedConfirm<string>());

		act(() => result.current.fire("row-1", commit));
		act(() => result.current.disarm());

		expect(result.current.armed).toBeNull();
	});
});
