import { act, render, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QuotaModalProvider, useQuotaModal } from "../QuotaModalContext";

describe("QuotaModalContext", () => {
	it("useQuotaModal returns false defaults", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		expect(result.current.isNanoOpen).toBe(false);
		expect(result.current.isZaiCodingOpen).toBe(false);
		expect(result.current.isOpenRouterOpen).toBe(false);
		expect(result.current.isOllamaCloudOpen).toBe(false);
	});

	it("setNanoOpen sets true", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		act(() => {
			result.current.setNanoOpen(true);
		});

		expect(result.current.isNanoOpen).toBe(true);
	});

	it("setNanoOpen sets false", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		act(() => {
			result.current.setNanoOpen(true);
		});

		expect(result.current.isNanoOpen).toBe(true);

		act(() => {
			result.current.setNanoOpen(false);
		});

		expect(result.current.isNanoOpen).toBe(false);
	});

	it("setZaiCodingOpen sets true", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		act(() => {
			result.current.setZaiCodingOpen(true);
		});

		expect(result.current.isZaiCodingOpen).toBe(true);
	});

	it("setOpenRouterOpen sets true", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		act(() => {
			result.current.setOpenRouterOpen(true);
		});

		expect(result.current.isOpenRouterOpen).toBe(true);
	});

	it("setOllamaCloudOpen sets true", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		act(() => {
			result.current.setOllamaCloudOpen(true);
		});

		expect(result.current.isOllamaCloudOpen).toBe(true);
	});

	it("Throws error when used outside provider", () => {
		// Suppress console.error for this test since we expect an error
		const consoleError = vi
			.spyOn(console, "error")
			.mockImplementation(() => {});

		const TestComponent = () => {
			useQuotaModal();
			return null;
		};

		expect(() => {
			render(<TestComponent />);
		}).toThrow("useQuotaModal must be used within QuotaModalProvider");

		consoleError.mockRestore();
	});
});
