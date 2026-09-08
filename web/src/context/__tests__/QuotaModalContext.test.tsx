import { act, render, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QuotaModalProvider, useQuotaModal } from "../QuotaModalContext";

describe("QuotaModalContext", () => {
	it("opens no modal by default", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		expect(result.current.open).toBeNull();
	});

	it("setOpen names the provider whose modal shows", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		act(() => {
			result.current.setOpen("nanogpt");
		});

		expect(result.current.open).toBe("nanogpt");
	});

	it("setOpen(null) closes", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		act(() => {
			result.current.setOpen("nanogpt");
		});
		act(() => {
			result.current.setOpen(null);
		});

		expect(result.current.open).toBeNull();
	});

	it("holds one open provider at a time", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		act(() => {
			result.current.setOpen("zai-coding");
		});
		act(() => {
			result.current.setOpen("kimi-code");
		});

		expect(result.current.open).toBe("kimi-code");
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
