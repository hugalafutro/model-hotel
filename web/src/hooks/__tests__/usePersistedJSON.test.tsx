import { renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { usePersistedJSON } from "../usePersistedJSON";

const mockToast = vi.fn();

vi.mock("../../context/ToastContext", () => ({
	useToast: () => ({ toast: mockToast }),
}));

describe("usePersistedJSON", () => {
	afterEach(() => {
		localStorage.clear();
		mockToast.mockClear();
		vi.restoreAllMocks();
	});

	it("mirrors the value into localStorage", () => {
		renderHook(() =>
			usePersistedJSON("thing", { a: 1 }, true, "warn.storageFull"),
		);

		expect(localStorage.getItem("thing")).toBe('{"a":1}');
	});

	it("writes nothing while disabled", () => {
		renderHook(() =>
			usePersistedJSON("thing", { a: 1 }, false, "warn.storageFull"),
		);

		expect(localStorage.getItem("thing")).toBeNull();
	});

	it("warns once when the store is full", () => {
		const spy = vi
			.spyOn(Storage.prototype, "setItem")
			.mockImplementation(() => {
				throw new Error("quota");
			});

		const { rerender } = renderHook(
			({ value }) => usePersistedJSON("thing", value, true, "warn.storageFull"),
			{ initialProps: { value: { a: 1 } } },
		);
		rerender({ value: { a: 2 } });
		rerender({ value: { a: 3 } });

		expect(spy).toHaveBeenCalledTimes(3);
		expect(mockToast).toHaveBeenCalledTimes(1);
		expect(mockToast).toHaveBeenCalledWith("warn.storageFull", "warning");
	});
});
