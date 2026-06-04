import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useResizeObserver } from "../useResizeObserver";

describe("useResizeObserver", () => {
	it("returns a ref and zero dimensions initially", () => {
		const { result } = renderHook(() => useResizeObserver());
		expect(result.current.ref.current).toBeNull();
		expect(result.current.width).toBe(0);
		expect(result.current.height).toBe(0);
	});

	it("accepts generic type parameter for SVG elements", () => {
		const { result } = renderHook(() => useResizeObserver<SVGElement>());
		expect(result.current.ref.current).toBeNull();
	});

	it("does not throw when ResizeObserver is available", () => {
		expect(() => renderHook(() => useResizeObserver())).not.toThrow();
	});

	it("does not throw when element is set and then cleared", () => {
		const { result, rerender } = renderHook(() => useResizeObserver());

		// Set ref to an element
		const div = document.createElement("div");
		result.current.ref.current = div;
		rerender();

		// Clear ref
		result.current.ref.current = null;
		rerender();

		expect(result.current.ref.current).toBeNull();
	});

	it("cleans up observer on unmount without errors", () => {
		vi.stubGlobal(
			"ResizeObserver",
			class MockRO {
				observe() {}
				unobserve() {}
				disconnect() {}
			},
		);

		const { unmount } = renderHook(() => useResizeObserver());
		expect(() => unmount()).not.toThrow();

		vi.restoreAllMocks();
	});
});
