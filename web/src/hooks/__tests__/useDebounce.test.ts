import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useDebounce } from "../useDebounce";

describe("useDebounce", () => {
	beforeEach(() => {
		vi.useFakeTimers();
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("returns initial value immediately", () => {
		const { result } = renderHook(() => useDebounce("hello", 500));
		expect(result.current).toBe("hello");
	});

	it("returns old value until delay elapses", () => {
		const { result, rerender } = renderHook(
			({ value, delay }) => useDebounce(value, delay),
			{ initialProps: { value: "initial", delay: 300 } },
		);

		rerender({ value: "updated", delay: 300 });
		expect(result.current).toBe("initial");

		act(() => {
			vi.advanceTimersByTime(200);
		});
		expect(result.current).toBe("initial");
	});

	it("updates to new value after delay", () => {
		const { result, rerender } = renderHook(
			({ value, delay }) => useDebounce(value, delay),
			{ initialProps: { value: "initial", delay: 300 } },
		);

		rerender({ value: "updated", delay: 300 });

		act(() => {
			vi.advanceTimersByTime(300);
		});
		expect(result.current).toBe("updated");
	});

	it("resets timer when value changes again before delay", () => {
		const { result, rerender } = renderHook(
			({ value, delay }) => useDebounce(value, delay),
			{ initialProps: { value: "a", delay: 300 } },
		);

		rerender({ value: "b", delay: 300 });

		act(() => {
			vi.advanceTimersByTime(200);
		});

		// Change again before 300ms elapsed
		rerender({ value: "c", delay: 300 });

		act(() => {
			vi.advanceTimersByTime(100);
		});
		// Only 100ms since last change, not yet 300ms
		expect(result.current).toBe("a");

		act(() => {
			vi.advanceTimersByTime(200);
		});
		// 300ms since "c" was set
		expect(result.current).toBe("c");
	});

	it("works with numeric values", () => {
		const { result, rerender } = renderHook(
			({ value, delay }) => useDebounce(value, delay),
			{ initialProps: { value: 0, delay: 100 } },
		);

		rerender({ value: 42, delay: 100 });
		expect(result.current).toBe(0);

		act(() => {
			vi.advanceTimersByTime(100);
		});
		expect(result.current).toBe(42);
	});

	it("works with object values", () => {
		const initial = { name: "a" };
		const updated = { name: "b" };
		const { result, rerender } = renderHook(
			({ value, delay }) => useDebounce(value, delay),
			{ initialProps: { value: initial, delay: 100 } },
		);

		rerender({ value: updated, delay: 100 });

		act(() => {
			vi.advanceTimersByTime(100);
		});
		expect(result.current).toEqual(updated);
	});

	it("cleans up timer on unmount", () => {
		const { unmount, rerender } = renderHook(
			({ value, delay }) => useDebounce(value, delay),
			{ initialProps: { value: "a", delay: 300 } },
		);

		rerender({ value: "b", delay: 300 });
		unmount();

		// No error should be thrown by the timer callback after unmount
		act(() => {
			vi.advanceTimersByTime(500);
		});
	});

	it("respects changing delay", () => {
		const { result, rerender } = renderHook(
			({ value, delay }) => useDebounce(value, delay),
			{ initialProps: { value: "a", delay: 200 } },
		);

		rerender({ value: "b", delay: 500 });

		act(() => {
			vi.advanceTimersByTime(200);
		});
		expect(result.current).toBe("a");

		act(() => {
			vi.advanceTimersByTime(300);
		});
		expect(result.current).toBe("b");
	});
});
