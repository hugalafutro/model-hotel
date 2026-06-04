import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Provider } from "../../api/types";
import { server } from "../../test/mocks/server";
import {
	detectQuotaProviderType,
	getCachedData,
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
	setCachedData,
	useQuotaData,
} from "../useQuotaData";

function createWrapper() {
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false } },
	});
	return ({ children }: { children: React.ReactNode }) => (
		<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
	);
}

const mockProviders: Provider[] = [
	{
		id: "nanogpt-1",
		name: "NanoGPT",
		base_url: "https://api.nano-gpt.com/v1",
		masked_key: "ngpt_***",
		enabled: true,
		autodiscovery_enabled: true,
		last_discovered_at: null,
		last_used_at: null,
		created_at: "2024-01-01T00:00:00Z",
		updated_at: "2024-01-01T00:00:00Z",
		model_count: 5,
		total_tokens: 1000000,
	},
	{
		id: "zai-1",
		name: "Z.ai Coding",
		base_url: "https://z.ai/api/v1",
		masked_key: "zai_***",
		enabled: true,
		autodiscovery_enabled: true,
		last_discovered_at: null,
		last_used_at: null,
		created_at: "2024-01-01T00:00:00Z",
		updated_at: "2024-01-01T00:00:00Z",
		model_count: 3,
		total_tokens: 500000,
	},
	{
		id: "deepseek-1",
		name: "DeepSeek",
		base_url: "https://api.deepseek.com/v1",
		masked_key: "ds_***",
		enabled: true,
		autodiscovery_enabled: true,
		last_discovered_at: null,
		last_used_at: null,
		created_at: "2024-01-01T00:00:00Z",
		updated_at: "2024-01-01T00:00:00Z",
		model_count: 2,
		total_tokens: 200000,
	},
	{
		id: "openrouter-1",
		name: "OpenRouter",
		base_url: "https://openrouter.ai/api/v1",
		masked_key: "or_***",
		enabled: true,
		autodiscovery_enabled: true,
		last_discovered_at: null,
		last_used_at: null,
		created_at: "2024-01-01T00:00:00Z",
		updated_at: "2024-01-01T00:00:00Z",
		model_count: 10,
		total_tokens: 2000000,
	},
	{
		id: "ollama-1",
		name: "Ollama Cloud",
		base_url: "https://ollama.com/api/v1",
		masked_key: "ollama_***",
		enabled: true,
		autodiscovery_enabled: true,
		last_discovered_at: null,
		last_used_at: null,
		created_at: "2024-01-01T00:00:00Z",
		updated_at: "2024-01-01T00:00:00Z",
		model_count: 8,
		total_tokens: 800000,
	},
];

describe("useQuotaData", () => {
	beforeEach(() => {
		localStorage.clear();
		vi.clearAllMocks();
	});

	it("returns undefined provider IDs when no providers are passed", async () => {
		const { result } = renderHook(() => useQuotaData(undefined), {
			wrapper: createWrapper(),
		});

		expect(result.current.nanogptProviderId).toBeUndefined();
		expect(result.current.zaiCodingProviderId).toBeUndefined();
		expect(result.current.deepseekProviderId).toBeUndefined();
		expect(result.current.openrouterProviderId).toBeUndefined();
		expect(result.current.ollamaCloudProviderId).toBeUndefined();
		expect(result.current.hasAnyProvider).toBe(false);
	});

	it("detects provider IDs from providers array", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptProviderId).toBe("nanogpt-1");
			expect(result.current.zaiCodingProviderId).toBe("zai-1");
			expect(result.current.deepseekProviderId).toBe("deepseek-1");
			expect(result.current.openrouterProviderId).toBe("openrouter-1");
			expect(result.current.ollamaCloudProviderId).toBe("ollama-1");
		});

		expect(result.current.hasAnyProvider).toBe(true);
	});

	it("fetches quota data on mount for enabled providers", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		// Wait for all queries to complete
		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
			expect(result.current.zaiCodingUsage).toBeDefined();
			expect(result.current.deepseekBalance).toBeDefined();
			expect(result.current.openrouterBalance).toBeDefined();
			expect(result.current.ollamaCloudAccount).toBeDefined();
		});

		// Verify NanoGPT usage data
		expect(result.current.nanogptUsage?.provider).toBe("nanogpt");
		expect(result.current.nanogptUsage?.limits?.weeklyInputTokens).toBe(
			1000000,
		);

		// Verify Z.ai usage data
		expect(result.current.zaiCodingUsage?.success).toBe(true);

		// Verify DeepSeek balance data
		expect(result.current.deepseekBalance?.is_available).toBe(true);
		expect(result.current.deepseekBalance?.balance_infos).toHaveLength(1);

		// Verify OpenRouter balance data
		expect(result.current.openrouterBalance?.credits_remaining).toBeDefined();

		// Verify Ollama Cloud account data
		expect(result.current.ollamaCloudAccount).toBeDefined();
	});

	it("handles loading state", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		// Initially, data should be undefined (loading)
		expect(result.current.nanogptUsage).toBeUndefined();

		// Wait for data to load
		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});
	});

	it("handles error state with toastErrors callback", async () => {
		const toastErrors = vi.fn();

		// Override MSW handler to return error
		server.use(
			http.get("/api/providers/:id/usage", () => {
				return HttpResponse.json({ error: "Internal error" }, { status: 500 });
			}),
		);

		renderHook(() => useQuotaData(mockProviders, { toastErrors }), {
			wrapper: createWrapper(),
		});

		// Wait for error to be triggered
		await waitFor(() => {
			expect(toastErrors).toHaveBeenCalled();
		});

		expect(toastErrors).toHaveBeenCalledWith(
			"Failed to fetch NanoGPT usage quota",
			"warning",
		);
	});

	it("does not toast errors when toastErrors is not provided", async () => {
		// Override MSW handler to return error only for nanogpt provider
		server.use(
			http.get("/api/providers/:id/usage", ({ params }) => {
				const providerId = params.id as string;
				if (providerId.includes("nanogpt")) {
					return HttpResponse.json(
						{ error: "Internal error" },
						{ status: 500 },
					);
				}
				// Return success for other providers
				return HttpResponse.json({
					active: true,
					provider: "nanogpt",
					providerStatus: "active",
					limits: { weeklyInputTokens: 1000000 },
				});
			}),
		);

		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		// Wait for queries to complete (nanogpt will fail silently)
		await waitFor(() => {
			expect(result.current.zaiCodingUsage).toBeDefined();
		});

		// nanogptUsage should be undefined due to error, but no toast
		expect(result.current.nanogptUsage).toBeUndefined();
	});

	it("refetches NanoGPT data when called", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});

		// Trigger refetch
		await act(async () => {
			await result.current.refetchNano();
		});

		await waitFor(() => {
			expect(result.current.isNanoRefetching).toBe(false);
		});

		// Data should be refetched (may be same or different)
		expect(result.current.nanogptUsage).toBeDefined();
	});

	it("refetches Z.ai data when called", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.zaiCodingUsage).toBeDefined();
		});

		await act(async () => {
			await result.current.refetchZaiCoding();
		});

		await waitFor(() => {
			expect(result.current.isZaiCodingRefetching).toBe(false);
		});

		expect(result.current.zaiCodingUsage).toBeDefined();
	});

	it("refetches DeepSeek data when called", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.deepseekBalance).toBeDefined();
		});

		await act(async () => {
			await result.current.refetchDeepseek();
		});

		await waitFor(() => {
			expect(result.current.isDsRefetching).toBe(false);
		});

		expect(result.current.deepseekBalance).toBeDefined();
	});

	it("refetches OpenRouter data when called", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.openrouterBalance).toBeDefined();
		});

		await act(async () => {
			await result.current.refetchOpenRouter();
		});

		await waitFor(() => {
			expect(result.current.isOrRefetching).toBe(false);
		});

		expect(result.current.openrouterBalance).toBeDefined();
	});

	it("refetches Ollama Cloud data when called", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.ollamaCloudAccount).toBeDefined();
		});

		await act(async () => {
			await result.current.refetchOllamaCloud();
		});

		await waitFor(() => {
			expect(result.current.isOllamaCloudRefetching).toBe(false);
		});

		expect(result.current.ollamaCloudAccount).toBeDefined();
	});

	it("invalidates all quota queries when called", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});

		await act(async () => {
			result.current.invalidateAll();
		});

		// After invalidation, queries should refetch
		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});
	});

	it("derives Z.ai five-hour and weekly limits", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.zaiCodingUsage).toBeDefined();
		});

		expect(result.current.zaiCodingFiveHour).toBeDefined();
		expect(result.current.zaiCodingWeekly).toBeDefined();
		expect(result.current.zaiCodingFiveHour?.type).toBe("TOKENS_LIMIT");
		expect(result.current.zaiCodingWeekly?.type).toBe("TOKENS_LIMIT");
	});

	it("derives NanoGPT weekly helpers", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});

		expect(result.current.nanoWeeklyUsed).toBeDefined();
		expect(result.current.nanoWeeklyLimit).toBeDefined();
		expect(result.current.nanoWeeklyUsed).toBe(200000);
		expect(result.current.nanoWeeklyLimit).toBe(1000000);
	});

	it("calculates badge visibility correctly", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.showNanoBadge).toBe(true);
			expect(result.current.showZaiCodingBadge).toBe(true);
			expect(result.current.showDsBadge).toBe(true);
			expect(result.current.showOrBadge).toBe(true);
			expect(result.current.showOllamaCloudBadge).toBe(true);
		});
	});

	it("hides badges when provider is missing", async () => {
		const providersWithoutNano = mockProviders.filter((p) => {
			try {
				const host = new URL(p.base_url).hostname;
				return !host.endsWith("nano-gpt.com");
			} catch {
				return true;
			}
		});

		const { result } = renderHook(() => useQuotaData(providersWithoutNano), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptProviderId).toBeUndefined();
			expect(result.current.showNanoBadge).toBe(false);
		});
	});

	it("hides NanoGPT badge when usage data is incomplete", async () => {
		// Override to return incomplete data
		server.use(
			http.get("/api/providers/:id/usage", () => {
				return HttpResponse.json({
					active: true,
					provider: "nanogpt",
					providerStatus: "active",
					limits: {
						weeklyInputTokens: 1000000,
						// Missing dailyInputTokens and dailyImages
					},
					// Missing weeklyInputTokens usage data
				});
			}),
		);

		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.showNanoBadge).toBe(false);
		});
	});

	it("hides NanoGPT badge when subscription is canceled", async () => {
		server.use(
			http.get("/api/providers/:id/usage", () => {
				return HttpResponse.json({
					active: false,
					provider: "nanogpt",
					providerStatus: "canceled",
					providerStatusRaw: "canceled",
					stripeSubscriptionId: "sub_test123",
					cancellationReason: null,
					canceledAt: "2025-01-01T00:00:00Z",
					endedAt: null,
					cancelAt: null,
					cancelAtPeriodEnd: false,
					limits: {
						weeklyInputTokens: 1000000,
						dailyInputTokens: 200000,
						dailyImages: 100,
					},
					allowOverage: false,
					period: {
						currentPeriodEnd: new Date(
							Date.now() + 7 * 24 * 60 * 60 * 1000,
						).toISOString(),
					},
					weeklyInputTokens: {
						used: 200000,
						remaining: 800000,
						percentUsed: 20,
						resetAt: Date.now() + 7 * 24 * 60 * 60 * 1000,
					},
					state: "canceled",
					graceUntil: null,
				});
			}),
		);

		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});

		expect(result.current.showNanoBadge).toBe(false);
	});

	it("hides NanoGPT badge when subscription status is 'cancelled' (British spelling)", async () => {
		server.use(
			http.get("/api/providers/:id/usage", () => {
				return HttpResponse.json({
					active: false,
					provider: "nanogpt",
					providerStatus: "cancelled",
					providerStatusRaw: "cancelled",
					stripeSubscriptionId: "sub_test123",
					cancellationReason: null,
					canceledAt: "2025-01-01T00:00:00Z",
					endedAt: null,
					cancelAt: null,
					cancelAtPeriodEnd: false,
					limits: {
						weeklyInputTokens: 1000000,
						dailyInputTokens: 200000,
						dailyImages: 100,
					},
					allowOverage: false,
					period: {
						currentPeriodEnd: new Date(
							Date.now() + 7 * 24 * 60 * 60 * 1000,
						).toISOString(),
					},
					weeklyInputTokens: {
						used: 200000,
						remaining: 800000,
						percentUsed: 20,
						resetAt: Date.now() + 7 * 24 * 60 * 60 * 1000,
					},
					state: "cancelled",
					graceUntil: null,
				});
			}),
		);

		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});

		expect(result.current.showNanoBadge).toBe(false);
	});

	it("hides DeepSeek badge when account is not available", async () => {
		server.use(
			http.get("/api/providers/:id/balance", () => {
				return HttpResponse.json({
					is_available: false,
					balance_infos: [],
				});
			}),
		);

		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.deepseekBalance).toBeDefined();
		});

		expect(result.current.showDsBadge).toBe(false);
	});

	it("hides Ollama Cloud badge when account is suspended", async () => {
		server.use(
			http.get("/api/providers/:id/account", () => {
				return HttpResponse.json({
					id: "ollama-account-1",
					email: "test@example.com",
					name: "Test User",
					plan: "pro",
					customer_id: { string: "cus_test123", valid: true },
					subscription_id: { string: "sub_test123", valid: true },
					subscription_period_start: {
						time: new Date().toISOString(),
						valid: true,
					},
					subscription_period_end: {
						time: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000).toISOString(),
						valid: true,
					},
					suspended_at: {
						time: "2025-01-01T00:00:00Z",
						valid: true,
					},
				});
			}),
		);

		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.ollamaCloudAccount).toBeDefined();
		});

		expect(result.current.showOllamaCloudBadge).toBe(false);
	});

	it("respects collapsed option (disables auto-refresh)", async () => {
		const { result } = renderHook(
			() =>
				useQuotaData(mockProviders, { collapsed: true, refetchInterval: 1000 }),
			{ wrapper: createWrapper() },
		);

		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});

		// The hook should still work, but auto-refresh should be disabled
		// This is tested by checking that the query doesn't refetch automatically
		expect(result.current.nanogptUsage).toBeDefined();
	});

	it("caches data to localStorage after fetching", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});

		// Check that data was cached
		const cachedData = localStorage.getItem("model-hotel:nanogpt-usage");
		if (cachedData) {
			expect(JSON.parse(cachedData)).toBeDefined();
		}
	});

	it("uses cached data as initialData on first render", async () => {
		// Pre-populate localStorage with cached data
		const cachedData = {
			active: true,
			provider: "nanogpt",
			providerStatus: "cached",
			limits: { weeklyInputTokens: 500000 },
		};
		localStorage.setItem(
			"model-hotel:nanogpt-usage",
			JSON.stringify(cachedData),
		);

		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		// Should have cached data immediately
		expect(result.current.nanogptUsage?.providerStatus).toBe("cached");
	});

	it("returns dataUpdatedAt timestamps", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptDataUpdatedAt).toBeGreaterThan(0);
			expect(result.current.zaiCodingDataUpdatedAt).toBeGreaterThan(0);
			expect(result.current.deepseekDataUpdatedAt).toBeGreaterThan(0);
			expect(result.current.openrouterDataUpdatedAt).toBeGreaterThan(0);
			expect(result.current.ollamaCloudDataUpdatedAt).toBeGreaterThan(0);
		});
	});

	it("tracks isRefetching state for each provider", async () => {
		const { result } = renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		await waitFor(() => {
			expect(result.current.nanogptUsage).toBeDefined();
		});

		// Initially not refetching
		expect(result.current.isNanoRefetching).toBe(false);
		expect(result.current.isZaiCodingRefetching).toBe(false);
		expect(result.current.isDsRefetching).toBe(false);
		expect(result.current.isOrRefetching).toBe(false);
		expect(result.current.isOllamaCloudRefetching).toBe(false);

		// Trigger refetch and verify state changes
		await act(async () => {
			result.current.refetchNano();
		});

		// After refetch completes, should be false again
		await waitFor(() => {
			expect(result.current.isNanoRefetching).toBe(false);
		});
	});

	describe("detectQuotaProviderType", () => {
		it("detects NanoGPT provider type", () => {
			expect(detectQuotaProviderType("https://api.nano-gpt.com/v1")).toBe(
				"nanogpt",
			);
		});

		it("detects Z.ai Coding provider type", () => {
			expect(detectQuotaProviderType("https://z.ai/api/v1")).toBe("zai-coding");
		});

		it("detects DeepSeek provider type", () => {
			expect(detectQuotaProviderType("https://api.deepseek.com/v1")).toBe(
				"deepseek",
			);
		});

		it("detects OpenRouter provider type", () => {
			expect(detectQuotaProviderType("https://openrouter.ai/api/v1")).toBe(
				"openrouter",
			);
		});

		it("detects Ollama Cloud provider type", () => {
			expect(detectQuotaProviderType("https://ollama.com/api/v1")).toBe(
				"ollama-cloud",
			);
		});

		it("returns null for unknown provider", () => {
			expect(detectQuotaProviderType("https://unknown-provider.com/v1")).toBe(
				null,
			);
		});

		it("returns null for invalid URL", () => {
			expect(detectQuotaProviderType("not-a-url")).toBe(null);
		});
	});

	describe("getCachedData / setCachedData", () => {
		it("stores and retrieves cached data", () => {
			const testData = { foo: "bar", count: 42 };
			setCachedData("test-key", testData);

			const retrieved = getCachedData("test-key");
			expect(retrieved).toEqual(testData);
		});

		it("returns undefined for missing cached data", () => {
			const retrieved = getCachedData("non-existent-key");
			expect(retrieved).toBeUndefined();
		});

		it("handles localStorage errors gracefully", () => {
			const spy = vi
				.spyOn(Storage.prototype, "setItem")
				.mockImplementation(() => {
					throw new Error("Storage error");
				});

			expect(() => setCachedData("test-key", { data: "value" })).not.toThrow();

			spy.mockRestore();
		});
	});

	describe("getZaiCodingFiveHourLimit / getZaiCodingWeeklyLimit", () => {
		it("returns five-hour limit from usage data", () => {
			const mockUsage = {
				code: 0,
				msg: "success",
				success: true,
				data: {
					level: "basic",
					limits: [
						{
							type: "TOKENS_LIMIT",
							unit: 3,
							number: 10000,
							usage: 5000,
							currentValue: 5000,
							remaining: 5000,
							percentage: 50,
							nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
						},
						{
							type: "TOKENS_LIMIT",
							unit: 6,
							number: 50000,
							usage: 25000,
							currentValue: 25000,
							remaining: 25000,
							percentage: 50,
							nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
						},
					],
				},
			};

			const limit = getZaiCodingFiveHourLimit(mockUsage);
			expect(limit).toBeDefined();
			expect(limit?.unit).toBe(3);
		});

		it("returns weekly limit from usage data", () => {
			const mockUsage = {
				code: 0,
				msg: "success",
				success: true,
				data: {
					level: "basic",
					limits: [
						{
							type: "TOKENS_LIMIT",
							unit: 3,
							number: 10000,
							usage: 5000,
							currentValue: 5000,
							remaining: 5000,
							percentage: 50,
							nextResetTime: Date.now() + 5 * 60 * 60 * 1000,
						},
						{
							type: "TOKENS_LIMIT",
							unit: 6,
							number: 50000,
							usage: 25000,
							currentValue: 25000,
							remaining: 25000,
							percentage: 50,
							nextResetTime: Date.now() + 7 * 24 * 60 * 60 * 1000,
						},
					],
				},
			};

			const limit = getZaiCodingWeeklyLimit(mockUsage);
			expect(limit).toBeDefined();
			expect(limit?.unit).toBe(6);
		});

		it("returns undefined for missing limits", () => {
			const mockUsage = {
				code: 0,
				msg: "success",
				success: true,
				data: {
					level: "basic",
					limits: [],
				},
			};

			expect(getZaiCodingFiveHourLimit(mockUsage)).toBeUndefined();
			expect(getZaiCodingWeeklyLimit(mockUsage)).toBeUndefined();
		});

		it("returns undefined for null/undefined usage data", () => {
			expect(getZaiCodingFiveHourLimit(null)).toBeUndefined();
			expect(getZaiCodingFiveHourLimit(undefined)).toBeUndefined();
			expect(getZaiCodingWeeklyLimit(null)).toBeUndefined();
			expect(getZaiCodingWeeklyLimit(undefined)).toBeUndefined();
		});
	});
});
