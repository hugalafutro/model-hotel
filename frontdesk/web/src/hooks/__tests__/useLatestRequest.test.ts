import { act, renderHook } from "@testing-library/react";
import { expect, it } from "vitest";
import { useLatestRequest } from "../useLatestRequest";

it("only ever holds the newest ticket", () => {
	const { result } = renderHook(() => useLatestRequest());
	const first = result.current.next();
	expect(result.current.isCurrent(first)).toBe(true);

	// A second read overtakes the first, so the slower one must not apply: this
	// is what keeps page 2's response from being overwritten by page 1's.
	const second = result.current.next();
	expect(result.current.isCurrent(first)).toBe(false);
	expect(result.current.isCurrent(second)).toBe(true);
});

it("invalidates everything in flight when a ticket is taken and not read back", () => {
	const { result } = renderHook(() => useLatestRequest());
	const inFlight = result.current.next();
	// What an unmount cleanup does: take a ticket nobody reads, so a response
	// landing afterwards sets no state.
	act(() => {
		result.current.next();
	});
	expect(result.current.isCurrent(inFlight)).toBe(false);
});

it("keeps one identity across renders so it is safe in a dependency list", () => {
	const { result, rerender } = renderHook(() => useLatestRequest());
	const before = result.current;
	rerender();
	expect(result.current).toBe(before);
});
