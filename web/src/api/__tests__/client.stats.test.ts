import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.stats", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	describe("get", () => {
		it("fetches stats without options", async () => {
			const mockStats = { total_requests: 100, total_tokens: 5000 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockStats), { status: 200 }),
			);

			const result = await api.stats.get();

			expect(result).toEqual(mockStats);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats",
				expect.objectContaining({
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("fetches stats with options", async () => {
			const mockStats = { total_requests: 200 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockStats), { status: 200 }),
			);

			await api.stats.get({
				period: "7d",
				excludeDeleted: true,
				metric: "tokens",
			});

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats?period=7d&exclude_deleted=true&metric=tokens",
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.stats.get()).rejects.toThrow(
				"Failed to fetch stats: 404 not found",
			);
		});
	});

	describe("getTimeSeries", () => {
		it("fetches time series stats", async () => {
			const mockData = { data: [{ timestamp: "2024-01-01", value: 100 }] };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockData), { status: 200 }),
			);

			const result = await api.stats.getTimeSeries();

			expect(result).toEqual(mockData);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats/timeseries",
				expect.objectContaining({
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("fetches time series with period option", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ data: [] }), { status: 200 }),
			);

			await api.stats.getTimeSeries({ period: "30d" });

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats/timeseries?period=30d",
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("server error", { status: 500 }),
			);

			await expect(api.stats.getTimeSeries()).rejects.toThrow(
				"Failed to fetch time-series stats: 500 server error",
			);
		});
	});

	describe("getProviderDistribution", () => {
		it("fetches provider distribution stats", async () => {
			const mockData = { distribution: [{ provider: "openai", count: 50 }] };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockData), { status: 200 }),
			);

			const result = await api.stats.getProviderDistribution();

			expect(result).toEqual(mockData);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats/provider-distribution",
				expect.objectContaining({
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("fetches with options", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ distribution: [] }), { status: 200 }),
			);

			await api.stats.getProviderDistribution({
				period: "7d",
				metric: "requests",
				excludeDeleted: true,
			});

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/stats/provider-distribution?period=7d&metric=requests&exclude_deleted=true",
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("bad request", { status: 400 }),
			);

			await expect(api.stats.getProviderDistribution()).rejects.toThrow(
				"Failed to fetch provider distribution: 400 bad request",
			);
		});
	});
});
