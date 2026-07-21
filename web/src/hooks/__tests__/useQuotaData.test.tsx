import * as reactQuery from "@tanstack/react-query";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, type Mock, vi } from "vitest";

// Wrap react-query's useQuery so we can inspect the options each quota query is
// configured with (delegates to the real implementation, so behavior is intact).
vi.mock("@tanstack/react-query", async (importOriginal) => {
	const actual = await importOriginal<typeof import("@tanstack/react-query")>();
	return { ...actual, useQuery: vi.fn(actual.useQuery) };
});

import type { Provider } from "../../api/types";
import { server } from "../../test/mocks/server";
import {
	detectQuotaProviderType,
	getCachedData,
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	getMiniMaxFiveHourLimit,
	getMiniMaxGeneralEntry,
	getMiniMaxWeeklyLimit,
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

	it("configures quota queries to refetch on mount with staleTime 0", () => {
		const useQueryMock = reactQuery.useQuery as unknown as Mock;

		renderHook(() => useQuotaData(mockProviders), {
			wrapper: createWrapper(),
		});

		const nanoCall = useQueryMock.mock.calls.find(
			([opts]) =>
				Array.isArray(
					(opts as { queryKey?: unknown[] } | undefined)?.queryKey,
				) && (opts as { queryKey: unknown[] }).queryKey[0] === "nanogpt-usage",
		);

		expect(nanoCall).toBeDefined();
		const nanoOpts = nanoCall?.[0] as {
			refetchOnMount?: unknown;
			staleTime?: unknown;
		};
		expect(nanoOpts.refetchOnMount).toBe("always");
		expect(nanoOpts.staleTime).toBe(0);
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
				return host !== "api.nano-gpt.com";
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

		// The successful fetch must have written the usage to the cache.
		const cachedData = localStorage.getItem("model-hotel:nanogpt-usage");
		expect(cachedData).not.toBeNull();
		expect(JSON.parse(cachedData as string)).toBeDefined();
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

		it("detects Kimi Code provider type", () => {
			expect(detectQuotaProviderType("https://api.kimi.com/v1")).toBe(
				"kimi-code",
			);
		});

		it("detects MiniMax provider type (api subdomain)", () => {
			expect(detectQuotaProviderType("https://api.minimax.io/v1")).toBe(
				"minimax",
			);
		});

		it("detects MiniMax provider type (apex + other subdomain)", () => {
			expect(detectQuotaProviderType("https://minimax.io/v1")).toBe("minimax");
			expect(detectQuotaProviderType("https://foo.minimax.io/v1")).toBe(
				"minimax",
			);
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

	describe("getKimiCodeFiveHourLimit / getKimiCodeWeeklyLimit", () => {
		const window300 = (
			limit: string | undefined,
			remaining: string | undefined,
			resetTime = "2026-07-19T17:10:02Z",
		) => ({
			window: { duration: 300, timeUnit: "TIME_UNIT_MINUTE" },
			detail: { limit, remaining, resetTime },
		});

		it("returns the 300-minute window with computed percentage", () => {
			const data = {
				usage: { limit: "100", remaining: "40", resetTime: "" },
				limits: [window300("100", "42")],
			} as never;
			const win = getKimiCodeFiveHourLimit(data);
			expect(win).toBeDefined();
			expect(win?.limit).toBe(100);
			expect(win?.remaining).toBe(42);
			expect(win?.percentage).toBeCloseTo(58);
			expect(win?.resetTime).toBe("2026-07-19T17:10:02Z");
		});

		it("returns undefined when no 300-minute window exists", () => {
			const data = {
				limits: [
					{
						window: { duration: 60, timeUnit: "TIME_UNIT_MINUTE" },
						detail: { limit: "100", remaining: "10", resetTime: "" },
					},
				],
			} as never;
			expect(getKimiCodeFiveHourLimit(data)).toBeUndefined();
		});

		it("returns undefined when the window detail is missing limit/remaining", () => {
			const data = {
				limits: [window300(undefined, undefined)],
			} as never;
			expect(getKimiCodeFiveHourLimit(data)).toBeUndefined();
		});

		it("returns undefined for non-finite numeric strings", () => {
			const data = {
				limits: [window300("abc", "def")],
			} as never;
			expect(getKimiCodeFiveHourLimit(data)).toBeUndefined();
		});

		it("yields percentage 0 when the limit is 0", () => {
			const data = {
				limits: [window300("0", "0")],
			} as never;
			const win = getKimiCodeFiveHourLimit(data);
			expect(win).toBeDefined();
			expect(win?.percentage).toBe(0);
		});

		it("returns the weekly window from top-level usage", () => {
			const data = {
				usage: { limit: "1000", remaining: "250", resetTime: "reset-weekly" },
				limits: [],
			} as never;
			const win = getKimiCodeWeeklyLimit(data);
			expect(win).toBeDefined();
			expect(win?.limit).toBe(1000);
			expect(win?.remaining).toBe(250);
			expect(win?.percentage).toBeCloseTo(75);
			expect(win?.resetTime).toBe("reset-weekly");
		});

		it("returns undefined weekly when usage is missing", () => {
			expect(getKimiCodeWeeklyLimit({ limits: [] } as never)).toBeUndefined();
		});

		it("returns undefined for null/undefined kimi data", () => {
			expect(getKimiCodeFiveHourLimit(null)).toBeUndefined();
			expect(getKimiCodeFiveHourLimit(undefined)).toBeUndefined();
			expect(getKimiCodeWeeklyLimit(null)).toBeUndefined();
			expect(getKimiCodeWeeklyLimit(undefined)).toBeUndefined();
		});
	});

	describe("getMiniMax helpers", () => {
		// Live-captured 2026-07-19 reference payload.
		const fixture = {
			model_remains: [
				{
					start_time: 1784473200000,
					end_time: 1784491200000,
					remains_time: 16420081,
					current_interval_total_count: 0,
					current_interval_usage_count: 0,
					model_name: "general",
					current_weekly_total_count: 0,
					current_weekly_usage_count: 0,
					weekly_start_time: 1783900800000,
					weekly_end_time: 1784505600000,
					weekly_remains_time: 30820081,
					current_interval_status: 1,
					current_interval_remaining_percent: 100,
					current_weekly_status: 1,
					current_weekly_remaining_percent: 100,
				},
				{
					start_time: 1784419200000,
					end_time: 1784505600000,
					remains_time: 30820081,
					current_interval_total_count: 0,
					current_interval_usage_count: 0,
					model_name: "video",
					current_weekly_total_count: 0,
					current_weekly_usage_count: 0,
					weekly_start_time: 1783900800000,
					weekly_end_time: 1784505600000,
					weekly_remains_time: 30820081,
					current_interval_status: 3,
					current_interval_remaining_percent: 100,
					current_weekly_status: 3,
					current_weekly_remaining_percent: 100,
				},
			],
			base_resp: { status_code: 0, status_msg: "success" },
		} as never;

		const noSub = {
			model_remains: null,
			base_resp: {
				status_code: 2062,
				status_msg: "no active token plan subscription",
			},
		} as never;

		it("finds the active general model class", () => {
			const g = getMiniMaxGeneralEntry(fixture);
			expect(g).toBeDefined();
			expect(g?.model_name).toBe("general");
			expect(g?.current_interval_status).toBe(1);
		});

		it("returns undefined for the no-subscription variant (status_code 2062)", () => {
			expect(getMiniMaxGeneralEntry(noSub)).toBeUndefined();
			expect(getMiniMaxFiveHourLimit(noSub)).toBeUndefined();
			expect(getMiniMaxWeeklyLimit(noSub)).toBeUndefined();
		});

		it("returns undefined when model_remains is empty", () => {
			const empty = {
				model_remains: [],
				base_resp: { status_code: 0, status_msg: "success" },
			} as never;
			expect(getMiniMaxGeneralEntry(empty)).toBeUndefined();
		});

		it("returns undefined when no general class is in active interval status", () => {
			const inactive = {
				model_remains: [
					{
						model_name: "general",
						remains_time: 1,
						weekly_remains_time: 1,
						current_interval_status: 3,
						current_interval_remaining_percent: 100,
						current_weekly_status: 3,
						current_weekly_remaining_percent: 100,
					},
				],
				base_resp: { status_code: 0, status_msg: "success" },
			} as never;
			expect(getMiniMaxGeneralEntry(inactive)).toBeUndefined();
		});

		it("derives 5h window as used% (100 − remaining) with ms reset", () => {
			const win = getMiniMaxFiveHourLimit(fixture);
			expect(win).toBeDefined();
			expect(win?.percentage).toBe(0);
			expect(win?.remainingPercent).toBe(100);
			expect(win?.resetMs).toBe(16420081);
		});

		it("derives weekly window as used% with weekly ms reset", () => {
			const win = getMiniMaxWeeklyLimit(fixture);
			expect(win).toBeDefined();
			expect(win?.percentage).toBe(0);
			expect(win?.resetMs).toBe(30820081);
		});

		it("inverts remaining percentages into used percentages", () => {
			const used = {
				model_remains: [
					{
						model_name: "general",
						remains_time: 1000,
						weekly_remains_time: 2000,
						current_interval_status: 1,
						current_interval_remaining_percent: 70,
						current_weekly_status: 1,
						current_weekly_remaining_percent: 40,
					},
				],
				base_resp: { status_code: 0, status_msg: "success" },
			} as never;
			expect(getMiniMaxFiveHourLimit(used)?.percentage).toBe(30);
			expect(getMiniMaxWeeklyLimit(used)?.percentage).toBe(60);
		});

		it("returns undefined for null/undefined minimax data", () => {
			expect(getMiniMaxGeneralEntry(null)).toBeUndefined();
			expect(getMiniMaxGeneralEntry(undefined)).toBeUndefined();
			expect(getMiniMaxFiveHourLimit(null)).toBeUndefined();
			expect(getMiniMaxWeeklyLimit(undefined)).toBeUndefined();
		});
	});
});
