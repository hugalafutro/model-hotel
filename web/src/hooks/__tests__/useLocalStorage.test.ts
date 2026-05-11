import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useLocalStorage } from "../useLocalStorage";

describe("useLocalStorage", () => {
	const key = "test-key";

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("returns initialValue when localStorage is empty", () => {
		const { result } = renderHook(() => useLocalStorage(key, "default"));
		expect(result.current[0]).toBe("default");
	});

	it("reads existing value from localStorage on mount", () => {
		localStorage.setItem(key, "persisted");
		const { result } = renderHook(() => useLocalStorage(key, "default"));
		expect(result.current[0]).toBe("persisted");
	});

	it("writes to localStorage when setter is called", () => {
		const { result } = renderHook(() => useLocalStorage(key, "initial"));
		act(() => {
			result.current[1]("updated");
		});
		expect(localStorage.getItem(key)).toBe("updated");
		expect(result.current[0]).toBe("updated");
	});

	it("supports functional updates", () => {
		const { result } = renderHook(() => useLocalStorage(key, 0));
		act(() => {
			result.current[1]((prev) => prev + 1);
		});
		expect(result.current[0]).toBe(1);
		expect(localStorage.getItem(key)).toBe("1");
	});

	it("skips read and write when enabled=false", () => {
		localStorage.setItem(key, "should-be-ignored");
		const { result } = renderHook(() =>
			useLocalStorage(key, "fallback", { enabled: false }),
		);
		expect(result.current[0]).toBe("fallback");

		act(() => {
			result.current[1]("new-value");
		});
		expect(localStorage.getItem(key)).toBe("should-be-ignored");
		expect(result.current[0]).toBe("new-value");
	});

	it("uses custom serialize/deserialize when provided", () => {
		const serialize = (v: number) => `num:${v}`;
		const deserialize = (s: string) => Number(s.split(":")[1]);

		const { result } = renderHook(() =>
			useLocalStorage(key, 42, { serialize, deserialize }),
		);
		expect(result.current[0]).toBe(42);

		act(() => {
			result.current[1](100);
		});
		expect(localStorage.getItem(key)).toBe("num:100");

		// re-render with same key should read it back
		const { result: result2 } = renderHook(() =>
			useLocalStorage(key, 0, { serialize, deserialize }),
		);
		expect(result2.current[0]).toBe(100);
	});

	it("silently ignores localStorage quota errors", () => {
		const setItemSpy = vi
			.spyOn(Storage.prototype, "setItem")
			.mockImplementation(() => {
				throw new Error("QuotaExceededError");
			});

		const { result } = renderHook(() => useLocalStorage(key, "ok"));
		expect(() => {
			act(() => {
				result.current[1]("fail");
			});
		}).not.toThrow();

		expect(result.current[0]).toBe("fail"); // state still updated
		setItemSpy.mockRestore();
	});
});
