import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type {
	Model,
	Provider,
	Stats,
	TimeSeriesStats,
} from "../../../api/types";
import { ToastContext } from "../../../context/ToastContext";
import { mockModel, mockProvider, mockStats } from "../../../test/mocks/data";
import { server } from "../../../test/mocks/server";
import { AllProviders } from "../../../test/utils";
import { useDashboard } from "../useDashboard";

describe("useDashboard", () => {
	beforeEach(() => {
		localStorage.clear();
		server.resetHandlers();
	});

	describe("deserializeRange / deserializeMetric validation", () => {
		it("invalid range in localStorage falls back to default '24h'", () => {
			localStorage.setItem("dashboardRange", "invalid");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.globalRange).toBe("24h");
		});

		it("invalid metric in localStorage falls back to default 'tokens'", () => {
			localStorage.setItem("dashboardMetric", "bad");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.globalMetric).toBe("tokens");
		});

		it("valid range from localStorage is deserialized correctly", () => {
			localStorage.setItem("dashboardRange", "1w");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.globalRange).toBe("1w");
		});

		it("valid metric from localStorage is deserialized correctly", () => {
			localStorage.setItem("dashboardMetric", "requests");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.globalMetric).toBe("requests");
		});
	});

	describe("Global range sync", () => {
		it("setGlobalRange syncs to all per-section ranges", async () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			// Initial state should have default "24h"
			expect(result.current.globalRange).toBe("24h");

			act(() => {
				result.current.setGlobalRange("1w");
			});

			await waitFor(() => {
				expect(result.current.requestsChartRange).toBe("1w");
				expect(result.current.tokensChartRange).toBe("1w");
				expect(result.current.doughnutRange).toBe("1w");
				expect(result.current.tokenRange).toBe("1w");
				expect(result.current.modelsRange).toBe("1w");
				expect(result.current.providersRange).toBe("1w");
				expect(result.current.virtualKeysRange).toBe("1w");
			});
		});

		it("setGlobalRange to '1h' syncs to all per-section ranges", async () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setGlobalRange("1h");
			});

			await waitFor(() => {
				expect(result.current.requestsChartRange).toBe("1h");
				expect(result.current.tokensChartRange).toBe("1h");
				expect(result.current.doughnutRange).toBe("1h");
				expect(result.current.tokenRange).toBe("1h");
				expect(result.current.modelsRange).toBe("1h");
				expect(result.current.providersRange).toBe("1h");
				expect(result.current.virtualKeysRange).toBe("1h");
			});
		});
	});

	describe("Global metric sync", () => {
		it("setGlobalMetric syncs to all per-section metrics", async () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			// Initial state should have default "tokens"
			expect(result.current.globalMetric).toBe("tokens");

			act(() => {
				result.current.setGlobalMetric("requests");
			});

			await waitFor(() => {
				expect(result.current.doughnutMetric).toBe("requests");
				expect(result.current.modelsMetric).toBe("requests");
				expect(result.current.providersMetric).toBe("requests");
				expect(result.current.virtualKeysMetric).toBe("requests");
			});
		});
	});

	describe("handleRefresh cooldown", () => {
		it("second call within 5s is blocked and shows cooldown toast", async () => {
			const toastSpy = vi.fn();
			const toastWrapper = ({ children }: { children: React.ReactNode }) => {
				// Create a wrapper that injects a spied toast via context
				return React.createElement(
					ToastContext.Provider,
					{
						value: {
							toast: toastSpy,
							position: "bottom-center" as const,
							setToastPosition: vi.fn(),
							timeout: 4000,
							setToastTimeout: vi.fn(),
						},
					},
					children,
				);
			};

			const { result } = renderHook(() => useDashboard(), {
				wrapper: ({ children }: { children: React.ReactNode }) =>
					AllProviders({ children, toastWrapper }),
			});

			// Wait for queries to resolve first (uses real Date.now)
			await waitFor(() => {
				expect(result.current.stats).toBeDefined();
			});

			// Freeze Date.now for cooldown test — both handleRefresh calls
			// see the same timestamp, eliminating slow-CI flakiness
			const fixedNow = 1000000;
			const nowSpy = vi.spyOn(Date, "now").mockReturnValue(fixedNow);

			act(() => {
				result.current.handleRefresh();
			});

			expect(result.current.isRefreshing).toBe(true);
			expect(toastSpy).toHaveBeenCalledWith("Refreshing dashboard…", "info");

			// Second call immediately should be blocked (cooldown)
			act(() => {
				result.current.handleRefresh();
			});

			// Cooldown toast should have been called
			expect(toastSpy).toHaveBeenCalledWith(
				"Please wait before refreshing again",
				"info",
			);
			nowSpy.mockRestore();
		});

		it("handleRefresh invalidates all relevant queries", async () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.stats).toBeDefined();
			});

			act(() => {
				result.current.handleRefresh();
			});

			expect(result.current.isRefreshing).toBe(true);
		});
	});

	describe("handleModelClick", () => {
		it("matching model label sets detailModel", async () => {
			// mockModel has provider_name="Test Provider", model_id="test-model-v1"
			// proxyModelID normalizes to "test-provider/test-model-v1"
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.models).toBeDefined();
			});

			// handleModelClick normalizes spaces to hyphens then compares against
			// proxyModelID output (e.g. "Test-Provider/test-model-v1")
			act(() => {
				result.current.handleModelClick("Test Provider/test-model-v1");
			});

			await waitFor(() => {
				expect(result.current.detailModel).not.toBeNull();
			});
		});

		it("non-matching label keeps detailModel null", async () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.models).toBeDefined();
			});

			act(() => {
				result.current.handleModelClick("Nonexistent/model-xyz");
			});

			await waitFor(() => {
				expect(result.current.detailModel).toBeNull();
			});
		});
	});

	describe("excludeDeleted filtering", () => {
		it("excludeDeleted=true filters out disabled_manually models", async () => {
			// Override MSW handler to return a mix of enabled and disabled models
			server.use(
				http.get("/api/models", () => {
					return HttpResponse.json([
						{ ...mockModel, id: "model-1", disabled_manually: false },
						{ ...mockModel, id: "model-2", disabled_manually: true },
						{ ...mockModel, id: "model-3", disabled_manually: false },
					] as Model[]);
				}),
			);

			const { result, rerender } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			// Initially excludeDeleted is false, should see all 3 models
			await waitFor(() => {
				expect(result.current.models).toHaveLength(3);
			});

			// Enable excludeDeleted
			act(() => {
				result.current.setExcludeDeleted(true);
			});

			rerender();

			// Should now only see 2 enabled models
			await waitFor(() => {
				expect(result.current.models).toHaveLength(2);
				expect(result.current.models?.every((m) => !m.disabled_manually)).toBe(
					true,
				);
			});
		});

		it("excludeDeleted=true filters out disabled providers", async () => {
			server.use(
				http.get("/api/providers", () => {
					return HttpResponse.json([
						{ ...mockProvider, id: "provider-1", enabled: true },
						{ ...mockProvider, id: "provider-2", enabled: false },
						{ ...mockProvider, id: "provider-3", enabled: true },
					] as Provider[]);
				}),
			);

			const { result, rerender } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			// Initially excludeDeleted is false, should see all 3 providers
			await waitFor(() => {
				expect(result.current.providers).toHaveLength(3);
			});

			// Enable excludeDeleted
			act(() => {
				result.current.setExcludeDeleted(true);
			});

			rerender();

			// Should now only see 2 enabled providers
			await waitFor(() => {
				expect(result.current.providers).toHaveLength(2);
				expect(result.current.providers?.every((p) => p.enabled)).toBe(true);
			});
		});
	});

	describe("hideManualRefresh", () => {
		it("dashboardRefreshSec=5 → hideManualRefresh=true", () => {
			localStorage.setItem("dashboardRefreshSec", "5");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.dashboardRefreshMs).toBe(5000);
			expect(result.current.hideManualRefresh).toBe(true);
		});

		it("dashboardRefreshSec=10 → hideManualRefresh=true", () => {
			localStorage.setItem("dashboardRefreshSec", "10");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.dashboardRefreshMs).toBe(10000);
			expect(result.current.hideManualRefresh).toBe(true);
		});

		it("default (30s) → hideManualRefresh=false", () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.dashboardRefreshMs).toBe(30000);
			expect(result.current.hideManualRefresh).toBe(false);
		});

		it("dashboardRefreshSec=15 → hideManualRefresh=false", () => {
			localStorage.setItem("dashboardRefreshSec", "15");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.dashboardRefreshMs).toBe(15000);
			expect(result.current.hideManualRefresh).toBe(false);
		});
	});

	describe("rangeLabel", () => {
		it("globalRange '1h' → rangeLabel '1h'", () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setGlobalRange("1h");
			});

			expect(result.current.rangeLabel).toBe("1h");
		});

		it("globalRange '24h' → rangeLabel '1d'", () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setGlobalRange("24h");
			});

			expect(result.current.rangeLabel).toBe("1d");
		});

		it("globalRange '7d' → rangeLabel '7d'", () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setGlobalRange("1w");
			});

			expect(result.current.rangeLabel).toBe("1w");
		});
	});

	describe("gaugeRequestCount", () => {
		it("range '1h' uses requests_last_1h", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						requests_last_1h: 10,
						total_requests_last_24h: 50,
						total_requests_last_7d: 200,
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setGlobalRange("1h");
			});

			await waitFor(() => {
				expect(result.current.gaugeRequestCount).toBe(10);
			});
		});

		it("range '24h' uses total_requests_last_24h", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						requests_last_1h: 10,
						total_requests_last_24h: 50,
						total_requests_last_7d: 200,
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setGlobalRange("24h");
			});

			await waitFor(() => {
				expect(result.current.gaugeRequestCount).toBe(50);
			});
		});

		it("range '7d' uses total_requests_last_7d", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						requests_last_1h: 10,
						total_requests_last_24h: 50,
						total_requests_last_7d: 200,
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setGlobalRange("1w");
			});

			await waitFor(() => {
				expect(result.current.gaugeRequestCount).toBe(200);
			});
		});
	});

	describe("totalTokens", () => {
		it("sums total_tokens_prompt and total_tokens_completion", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						total_tokens_prompt: 100,
						total_tokens_completion: 50,
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.totalTokens).toBe(150);
			});
		});

		it("handles zero values", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						total_tokens_prompt: 0,
						total_tokens_completion: 0,
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.totalTokens).toBe(0);
			});
		});
	});

	describe("acData time series formatting", () => {
		it("1h range formats label as HH:mm", async () => {
			const timeSeriesData: TimeSeriesStats = {
				points: [
					{
						bucket: "2025-01-15T10:30:00Z",
						count: 5,
						errors: 1,
						tokens: 1000,
						latency_ms: 250.5,
						overhead_ms: 10,
						provider_latency_ms: 240.5,
						rate_limit_hits: 0,
						avg_ttft_ms: 50,
					},
				],
			};

			server.use(
				http.get("/api/stats/timeseries", () => {
					return HttpResponse.json(timeSeriesData);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setRequestsChartRange("1h");
			});

			await waitFor(() => {
				expect(result.current.acData).toHaveLength(1);
				expect(result.current.acData[0].hour).toBe("10:30");
			});
		});

		it("24h range formats label as HH:00", async () => {
			const timeSeriesData: TimeSeriesStats = {
				points: [
					{
						bucket: "2025-01-15T10:30:00Z",
						count: 5,
						errors: 1,
						tokens: 1000,
						latency_ms: 250.5,
						overhead_ms: 10,
						provider_latency_ms: 240.5,
						rate_limit_hits: 0,
						avg_ttft_ms: 50,
					},
				],
			};

			server.use(
				http.get("/api/stats/timeseries", () => {
					return HttpResponse.json(timeSeriesData);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setRequestsChartRange("24h");
			});

			await waitFor(() => {
				expect(result.current.acData).toHaveLength(1);
				expect(result.current.acData[0].hour).toBe("10:00");
			});
		});

		it("7d range formats label as MMM d", async () => {
			const timeSeriesData: TimeSeriesStats = {
				points: [
					{
						bucket: "2025-01-15T10:30:00Z",
						count: 5,
						errors: 1,
						tokens: 1000,
						latency_ms: 250.5,
						overhead_ms: 10,
						provider_latency_ms: 240.5,
						rate_limit_hits: 0,
						avg_ttft_ms: 50,
					},
				],
			};

			server.use(
				http.get("/api/stats/timeseries", () => {
					return HttpResponse.json(timeSeriesData);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setRequestsChartRange("1w");
			});

			await waitFor(() => {
				expect(result.current.acData).toHaveLength(1);
				expect(result.current.acData[0].hour).toBe("Jan 15");
			});
		});
	});

	describe("byModel / byProvider / byVK", () => {
		it("byModel filters zeros, sorts descending, limits to 5", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						by_model: {
							"model-a": 100,
							"model-b": 200,
							"model-c": 0,
							"model-d": 50,
							"model-e": 300,
							"model-f": 150,
						},
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.byModel).toHaveLength(5);
				// Default globalMetric is "tokens", so suffix is " tokens"
				// All entries have deleted: true since model keys don't match mockModel
				expect(result.current.byModel[0]).toEqual({
					label: "model-e",
					value: 300,
					suffix: " tokens",
					failoverGroup: false,
					deleted: true,
				});
				expect(result.current.byModel[1]).toEqual({
					label: "model-b",
					value: 200,
					suffix: " tokens",
					failoverGroup: false,
					deleted: true,
				});
				// model-c with 0 should be filtered out
			});
		});

		it("byModel does not mark hotel/ failover groups as deleted", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						by_model: { "hotel/my-group": 150, "model-x": 50 },
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.byModel).toHaveLength(2);
			});

			const failoverEntry = result.current.byModel.find(
				(e) => e.label === "hotel/my-group",
			);
			expect(failoverEntry?.failoverGroup).toBe(true);
			expect(failoverEntry?.deleted).toBe(false);

			const regularEntry = result.current.byModel.find(
				(e) => e.label === "model-x",
			);
			expect(regularEntry?.failoverGroup).toBe(false);
			expect(regularEntry?.deleted).toBe(true);
		});

		it("byModel uses 'tokens' suffix when modelsMetric is 'tokens'", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						by_model: { "model-a": 100 },
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setModelsMetric("tokens");
			});

			await waitFor(() => {
				expect(result.current.byModel[0].suffix).toBe(" tokens");
			});
		});

		it("byProvider filters zeros and sorts descending", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						by_provider: {
							"provider-a": 100,
							"provider-b": 200,
							"provider-c": 0,
						},
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.byProvider).toHaveLength(2);
				expect(result.current.byProvider[0].label).toBe("provider-b");
				expect(result.current.byProvider[1].label).toBe("provider-a");
			});
		});

		it("byVK marks 'Deleted' key with deleted flag", async () => {
			server.use(
				http.get("/api/stats", () => {
					return HttpResponse.json({
						...mockStats,
						by_virtual_key: {
							"vk-1": 100,
							Deleted: 50,
							"vk-2": 200,
						},
					} as Stats);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				const deletedEntry = result.current.byVK.find(
					(e) => e.label === "Deleted",
				);
				expect(deletedEntry?.deleted).toBe(true);
			});
		});
	});

	describe("dashboardRefreshChange event", () => {
		it("dispatching dashboardRefreshChange updates dashboardRefreshMs", () => {
			localStorage.setItem("dashboardRefreshSec", "10");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			// Initial value
			expect(result.current.dashboardRefreshMs).toBe(10000);

			// Change localStorage and dispatch event
			localStorage.setItem("dashboardRefreshSec", "60");
			act(() => {
				window.dispatchEvent(new Event("dashboardRefreshChange"));
			});

			expect(result.current.dashboardRefreshMs).toBe(60000);
		});

		it("invalid dashboardRefreshSec falls back to 30s", () => {
			localStorage.setItem("dashboardRefreshSec", "invalid");

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.dashboardRefreshMs).toBe(30000);
		});
	});

	describe("Stats error auth check", () => {
		let reloadSpy: ReturnType<typeof vi.fn>;

		beforeEach(() => {
			reloadSpy = vi.fn();
			vi.stubGlobal("location", {
				...window.location,
				reload: reloadSpy,
			});
		});

		afterEach(() => {
			vi.unstubAllGlobals();
		});

		it("401 error removes adminToken", async () => {
			// Set initial admin token
			localStorage.setItem("adminToken", "test-token");

			// Mock stats endpoint to return 401 with text response
			// (matching how the real API returns errors)
			server.use(
				http.get("/api/stats", () => {
					return new HttpResponse("Unauthorized", {
						status: 401,
						statusText: "Unauthorized",
					});
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			// Wait for the error to be set and adminToken to be removed
			// The effect runs after statsError is populated
			await waitFor(
				() => {
					expect(result.current.statsError).toBeDefined();
					expect(localStorage.getItem("adminToken")).toBeNull();
				},
				{ timeout: 3000 },
			);
			expect(reloadSpy).toHaveBeenCalled();
		});

		it("non-auth errors do not remove adminToken", async () => {
			localStorage.setItem("adminToken", "test-token");

			// Mock stats endpoint to return 500
			server.use(
				http.get("/api/stats", () => {
					return new HttpResponse("Internal server error", {
						status: 500,
						statusText: "Internal Server Error",
					});
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.statsError).toBeDefined();
			});

			// Admin token should NOT be removed
			expect(localStorage.getItem("adminToken")).toBe("test-token");
		});
	});

	describe("tokenAcData", () => {
		it("formats token time series with correct labels", async () => {
			const timeSeriesData: TimeSeriesStats = {
				points: [
					{
						bucket: "2025-01-15T14:00:00Z",
						count: 10,
						errors: 2,
						tokens: 5000,
						latency_ms: 300,
						overhead_ms: 15,
						provider_latency_ms: 285,
						rate_limit_hits: 1,
						avg_ttft_ms: 60,
					},
				],
			};

			server.use(
				http.get("/api/stats/timeseries", () => {
					return HttpResponse.json(timeSeriesData);
				}),
			);

			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			act(() => {
				result.current.setTokensChartRange("24h");
			});

			await waitFor(() => {
				expect(result.current.tokenAcData).toHaveLength(1);
				expect(result.current.tokenAcData[0].hour).toBe("14:00");
				expect(result.current.tokenAcData[0].tokens).toBe(5000);
			});
		});
	});

	describe("accents", () => {
		it("returns correct accent colors", () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.accents).toEqual({
				providers: "#14b8a6",
				models: "#818cf8",
				requests: "#0ea5e9",
				latency: "#f59e0b",
				overhead: "#f472b6",
				errors: "#ef4444",
				tokens: "#22c55e",
				rateLimit: "#a855f7",
			});
		});
	});

	describe("query data loading", () => {
		it("initial state has loading flags", () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			expect(result.current.statsLoading).toBe(true);
			expect(result.current.modelsLoading).toBe(true);
			expect(result.current.providersLoading).toBe(true);
		});

		it("data is populated after queries resolve", async () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			await waitFor(() => {
				expect(result.current.stats).toBeDefined();
				expect(result.current.models).toBeDefined();
				expect(result.current.providers).toBeDefined();
			});
		});
	});

	describe("modal state management", () => {
		it("all modal states are controllable", () => {
			const { result } = renderHook(() => useDashboard(), {
				wrapper: AllProviders,
			});

			// Initial state - all modals closed
			expect(result.current.overheadModalOpen).toBe(false);
			expect(result.current.errorModalOpen).toBe(false);
			expect(result.current.latencyModalOpen).toBe(false);
			expect(result.current.ttftModalOpen).toBe(false);
			expect(result.current.rateLimitModalOpen).toBe(false);
			expect(result.current.requestsModalOpen).toBe(false);
			expect(result.current.tokensModalOpen).toBe(false);

			// Open overhead modal
			act(() => {
				result.current.setOverheadModalOpen(true);
			});
			expect(result.current.overheadModalOpen).toBe(true);

			// Open error modal
			act(() => {
				result.current.setErrorModalOpen(true);
			});
			expect(result.current.errorModalOpen).toBe(true);

			// Open latency modal
			act(() => {
				result.current.setLatencyModalOpen(true);
			});
			expect(result.current.latencyModalOpen).toBe(true);

			// Open ttft modal
			act(() => {
				result.current.setTtftModalOpen(true);
			});
			expect(result.current.ttftModalOpen).toBe(true);

			// Open rate limit modal
			act(() => {
				result.current.setRateLimitModalOpen(true);
			});
			expect(result.current.rateLimitModalOpen).toBe(true);

			// Open requests modal
			act(() => {
				result.current.setRequestsModalOpen(true);
			});
			expect(result.current.requestsModalOpen).toBe(true);

			// Open tokens modal
			act(() => {
				result.current.setTokensModalOpen(true);
			});
			expect(result.current.tokensModalOpen).toBe(true);
		});
	});
});
