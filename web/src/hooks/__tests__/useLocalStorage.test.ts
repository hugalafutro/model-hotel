import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useLocalStorage, useLocalStorageValue } from "../useLocalStorage";

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

describe("useLocalStorageValue", () => {
	const key = "mirrored-key";

	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("reads the stored value on mount", () => {
		localStorage.setItem(key, "stored");
		const { result } = renderHook(() => useLocalStorageValue(key, "fallback"));
		expect(result.current).toBe("stored");
	});

	it("returns the fallback when the key is absent", () => {
		const { result } = renderHook(() => useLocalStorageValue(key, "fallback"));
		expect(result.current).toBe("fallback");
	});

	it("deserializes the raw string, nulls included", () => {
		localStorage.setItem(key, "5");
		const { result } = renderHook(() =>
			useLocalStorageValue(key, 30_000, {
				deserialize: (stored, fallback) =>
					stored === null ? fallback : Number(stored) * 1000,
			}),
		);
		expect(result.current).toBe(5000);
	});

	it("reads the new key immediately when the caller rerenders with one", () => {
		localStorage.setItem(key, "first");
		localStorage.setItem("other-key", "second");
		const { result, rerender } = renderHook(
			({ k }) => useLocalStorageValue(k, "fallback"),
			{ initialProps: { k: key } },
		);
		expect(result.current).toBe("first");

		rerender({ k: "other-key" });
		expect(result.current).toBe("second");
	});

	it("falls back immediately when the new key is absent", () => {
		localStorage.setItem(key, "first");
		const { result, rerender } = renderHook(
			({ k }) => useLocalStorageValue(k, "fallback"),
			{ initialProps: { k: key } },
		);

		rerender({ k: "absent-key" });
		expect(result.current).toBe("fallback");
	});

	it("follows a new fallback while the key is absent", () => {
		const { result, rerender } = renderHook(
			({ f }) => useLocalStorageValue(key, f),
			{ initialProps: { f: "first" } },
		);
		expect(result.current).toBe("first");

		rerender({ f: "second" });
		expect(result.current).toBe("second");
	});

	it("re-parses when the caller rerenders with a new deserializer", () => {
		localStorage.setItem(key, "5");
		const { result, rerender } = renderHook(
			({ d }) =>
				useLocalStorageValue(key, 0, {
					deserialize: d,
				}),
			{
				initialProps: {
					d: (stored: string | null, fallback: number) =>
						stored === null ? fallback : Number(stored),
				},
			},
		);
		expect(result.current).toBe(5);

		rerender({
			d: (stored: string | null, fallback: number) =>
				stored === null ? fallback : Number(stored) * 1000,
		});
		expect(result.current).toBe(5000);
	});

	it("re-reads on a cross-tab storage event for its key", () => {
		localStorage.setItem(key, "first");
		const { result } = renderHook(() => useLocalStorageValue(key, "fallback"));

		localStorage.setItem(key, "second");
		act(() => {
			window.dispatchEvent(new StorageEvent("storage", { key }));
		});
		expect(result.current).toBe("second");
	});

	it("re-reads on the localStorageChange event for its key", () => {
		localStorage.setItem(key, "first");
		const { result } = renderHook(() => useLocalStorageValue(key, "fallback"));

		localStorage.setItem(key, "second");
		act(() => {
			window.dispatchEvent(
				new CustomEvent("localStorageChange", { detail: { key } }),
			);
		});
		expect(result.current).toBe("second");
	});

	it("re-reads on an extra event the writer announces", () => {
		localStorage.setItem(key, "first");
		const { result } = renderHook(() =>
			useLocalStorageValue(key, "fallback", { events: ["writerToggle"] }),
		);

		localStorage.setItem(key, "second");
		act(() => {
			window.dispatchEvent(new CustomEvent("writerToggle"));
		});
		expect(result.current).toBe("second");
	});

	it("subscribes to an event name containing a space", () => {
		localStorage.setItem(key, "first");
		const { result } = renderHook(() =>
			useLocalStorageValue(key, "fallback", { events: ["writer toggle"] }),
		);

		localStorage.setItem(key, "second");
		act(() => {
			window.dispatchEvent(new CustomEvent("writer toggle"));
		});
		expect(result.current).toBe("second");
	});

	it("ignores changes to other keys", () => {
		localStorage.setItem(key, "first");
		const { result } = renderHook(() => useLocalStorageValue(key, "fallback"));

		localStorage.setItem(key, "second");
		act(() => {
			window.dispatchEvent(new StorageEvent("storage", { key: "other" }));
			window.dispatchEvent(
				new CustomEvent("localStorageChange", { detail: { key: "other" } }),
			);
		});
		expect(result.current).toBe("first");
	});

	it("never writes, not even while following a change", () => {
		localStorage.setItem(key, "first");
		const setItemSpy = vi.spyOn(Storage.prototype, "setItem");
		const dispatchSpy = vi.spyOn(window, "dispatchEvent");
		const { result, unmount } = renderHook(() =>
			useLocalStorageValue(key, "fallback"),
		);

		act(() => {
			window.dispatchEvent(
				new CustomEvent("localStorageChange", { detail: { key } }),
			);
		});
		unmount();

		expect(result.current).toBe("first");
		expect(setItemSpy).not.toHaveBeenCalled();
		// Only the event the test itself fired; the hook announces nothing.
		expect(dispatchSpy).toHaveBeenCalledTimes(1);
		setItemSpy.mockRestore();
		dispatchSpy.mockRestore();
	});

	it("falls back when the deserializer throws", () => {
		localStorage.setItem(key, "not-json");
		const { result } = renderHook(() =>
			useLocalStorageValue(key, "fallback", {
				deserialize: (stored) => JSON.parse(stored ?? "") as string,
			}),
		);

		expect(result.current).toBe("fallback");
	});

	it("falls back when localStorage throws", () => {
		const getItemSpy = vi
			.spyOn(Storage.prototype, "getItem")
			.mockImplementation(() => {
				throw new Error("SecurityError");
			});

		const { result } = renderHook(() => useLocalStorageValue(key, "fallback"));

		expect(result.current).toBe("fallback");
		getItemSpy.mockRestore();
	});

	it("stops following once unmounted", () => {
		localStorage.setItem(key, "first");
		const { result, unmount } = renderHook(() =>
			useLocalStorageValue(key, "fallback"),
		);
		unmount();

		localStorage.setItem(key, "second");
		act(() => {
			window.dispatchEvent(
				new CustomEvent("localStorageChange", { detail: { key } }),
			);
		});
		expect(result.current).toBe("first");
	});
});
