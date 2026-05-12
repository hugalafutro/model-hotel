import { act, render, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type {
	NanoGPTUsage,
	OllamaCloudAccount,
	OpenRouterBalance,
	ZAICodingQuotaResponse,
} from "../../api/types";
import { QuotaModalProvider, useQuotaModal } from "../QuotaModalContext";

describe("QuotaModalContext", () => {
	it("useQuotaModal returns null defaults", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		expect(result.current.nanogptUsage).toBeNull();
		expect(result.current.zaiCodingUsage).toBeNull();
		expect(result.current.openrouterBalance).toBeNull();
		expect(result.current.ollamaCloudAccount).toBeNull();
	});

	it("setNanogptUsage sets a value", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		const mockUsage: NanoGPTUsage = {
			active: true,
			provider: "nano-gpt",
			providerStatus: "active",
			providerStatusRaw: "active",
			stripeSubscriptionId: "sub_123",
			cancellationReason: null,
			canceledAt: null,
			endedAt: null,
			cancelAt: null,
			cancelAtPeriodEnd: false,
			limits: {
				weeklyInputTokens: 10000,
				dailyInputTokens: 5000,
				dailyImages: 100,
			},
			allowOverage: false,
			period: {
				currentPeriodEnd: "2026-05-31T23:59:59Z",
			},
			dailyImages: {
				used: 10,
				remaining: 90,
				percentUsed: 10,
				resetAt: Date.now() + 86400000,
			},
			dailyInputTokens: {
				used: 1000,
				remaining: 4000,
				percentUsed: 20,
				resetAt: Date.now() + 86400000,
			},
			weeklyInputTokens: {
				used: 5000,
				remaining: 5000,
				percentUsed: 50,
				resetAt: Date.now() + 604800000,
			},
			state: "active",
			graceUntil: null,
		};

		act(() => {
			result.current.setNanogptUsage(mockUsage);
		});

		expect(result.current.nanogptUsage).toEqual(mockUsage);
	});

	it("setZaiCodingUsage sets a value", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		const mockUsage: ZAICodingQuotaResponse = {
			code: 200,
			msg: "success",
			data: {
				limits: [
					{
						type: "daily",
						unit: 1,
						number: 5000,
						usage: 500,
						currentValue: 500,
						remaining: 4500,
						percentage: 10,
						nextResetTime: Date.now() + 86400000,
					},
				],
				level: "pro",
			},
			success: true,
		};

		act(() => {
			result.current.setZaiCodingUsage(mockUsage);
		});

		expect(result.current.zaiCodingUsage).toEqual(mockUsage);
	});

	it("setOpenrouterBalance sets a value", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		const mockBalance: OpenRouterBalance = {
			label: "OpenRouter Balance",
			limit: null,
			limit_reset: "",
			limit_remaining: null,
			usage: 5.25,
			usage_daily: 1.5,
			usage_weekly: 5.25,
			usage_monthly: 20.0,
			credits_total: 50.0,
			credits_used: 10.0,
			credits_remaining: 40.0,
			is_free_tier: false,
		};

		act(() => {
			result.current.setOpenrouterBalance(mockBalance);
		});

		expect(result.current.openrouterBalance).toEqual(mockBalance);
	});

	it("setOllamaCloudAccount sets a value", () => {
		const { result } = renderHook(() => useQuotaModal(), {
			wrapper: QuotaModalProvider,
		});

		const mockAccount: OllamaCloudAccount = {
			id: "acc_123",
			email: "test@example.com",
			name: "Test User",
			plan: "pro",
			customer_id: { string: "cus_123", valid: true },
			subscription_id: { string: "sub_123", valid: true },
			subscription_period_start: { time: "2026-05-01T00:00:00Z", valid: true },
			subscription_period_end: { time: "2026-05-31T23:59:59Z", valid: true },
			suspended_at: { time: "", valid: false },
		};

		act(() => {
			result.current.setOllamaCloudAccount(mockAccount);
		});

		expect(result.current.ollamaCloudAccount).toEqual(mockAccount);
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
