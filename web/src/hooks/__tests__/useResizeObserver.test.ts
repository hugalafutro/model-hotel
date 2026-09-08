import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useResizeObserver } from "../useResizeObserver";

describe("useResizeObserver", () => {
	it("starts with no element and zero dimensions", () => {
		const { result } = renderHook(() => useResizeObserver());
		expect(result.current.el).toBeNull();
		expect(result.current.width).toBe(0);
		expect(result.current.height).toBe(0);
	});

	it("accepts a generic type parameter for SVG elements", () => {
		const { result } = renderHook(() => useResizeObserver<SVGElement>());
		expect(result.current.el).toBeNull();
	});

	it("observes the attached element and measures it", () => {
		const observed: Element[] = [];
		vi.stubGlobal(
			"ResizeObserver",
			class MockRO {
				observe(el: Element) {
					observed.push(el);
				}
				unobserve() {}
				disconnect() {}
			},
		);
		const div = document.createElement("div");
		vi.spyOn(div, "getBoundingClientRect").mockReturnValue({
			width: 120,
			height: 40,
		} as DOMRect);

		const { result } = renderHook(() => useResizeObserver());
		act(() => result.current.ref(div));

		expect(result.current.el).toBe(div);
		expect(observed).toEqual([div]);
		expect(result.current.width).toBe(120);
		expect(result.current.height).toBe(40);

		vi.unstubAllGlobals();
	});

	it("detaching the element stops the observation without throwing", () => {
		let disconnected = 0;
		vi.stubGlobal(
			"ResizeObserver",
			class MockRO {
				observe() {}
				unobserve() {}
				disconnect() {
					disconnected += 1;
				}
			},
		);

		const { result } = renderHook(() => useResizeObserver());
		act(() => result.current.ref(document.createElement("div")));
		act(() => result.current.ref(null));

		expect(result.current.el).toBeNull();
		expect(disconnected).toBe(1);

		vi.unstubAllGlobals();
	});

	it("cleans up the observer on unmount", () => {
		let disconnected = 0;
		vi.stubGlobal(
			"ResizeObserver",
			class MockRO {
				observe() {}
				unobserve() {}
				disconnect() {
					disconnected += 1;
				}
			},
		);

		const { result, unmount } = renderHook(() => useResizeObserver());
		act(() => result.current.ref(document.createElement("div")));
		expect(() => unmount()).not.toThrow();
		expect(disconnected).toBe(1);

		vi.unstubAllGlobals();
	});
});
