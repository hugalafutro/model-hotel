import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useDocumentVisible } from "../useDocumentVisible";

function setHidden(hidden: boolean) {
	Object.defineProperty(document, "hidden", {
		configurable: true,
		get: () => hidden,
	});
	document.dispatchEvent(new Event("visibilitychange"));
}

describe("useDocumentVisible", () => {
	afterEach(() => {
		setHidden(false);
		vi.restoreAllMocks();
	});

	it("reports the current visibility and follows changes", () => {
		const { result } = renderHook(() => useDocumentVisible());
		expect(result.current).toBe(true);

		act(() => setHidden(true));
		expect(result.current).toBe(false);

		act(() => setHidden(false));
		expect(result.current).toBe(true);
	});

	it("detaches its listener on unmount", () => {
		const remove = vi.spyOn(document, "removeEventListener");
		const { unmount } = renderHook(() => useDocumentVisible());

		unmount();

		expect(remove).toHaveBeenCalledWith(
			"visibilitychange",
			expect.any(Function),
		);
	});
});
