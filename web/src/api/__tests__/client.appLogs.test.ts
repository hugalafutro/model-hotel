import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.appLogs", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches app logs without params", async () => {
			const mockLogs = [
				{
					timestamp: "2024-01-01",
					level: "info",
					source: "test",
					message: "OK",
				},
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockLogs), { status: 200 }),
			);

			const result = await api.appLogs.list();
			expect(result).toEqual(mockLogs);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/app",
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("fetches app logs with optional params", async () => {
			const mockLogs = [
				{
					timestamp: "2024-01-01",
					level: "info",
					source: "test",
					message: "OK",
				},
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockLogs), { status: 200 }),
			);

			const result = await api.appLogs.list({ limit: 50, after: "abc123" });
			expect(result).toEqual(mockLogs);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/app?limit=50&after=abc123",
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(api.appLogs.list()).rejects.toThrow(
				"Failed to fetch app logs: 500 error",
			);
		});
	});

	describe("purge", () => {
		it("purges app logs", async () => {
			const mockResult = { deleted: 100 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.appLogs.purge();
			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/app",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(api.appLogs.purge()).rejects.toThrow(
				"Failed to purge app logs: 500 error",
			);
		});
	});

	describe("history", () => {
		it("fetches app log history without params", async () => {
			const mockHistory = {
				entries: [],
				total: 0,
				page: 1,
				per_page: 20,
				level_counts: { info: 10, error: 5 },
				source_counts: { api: 15 },
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockHistory), { status: 200 }),
			);

			const result = await api.appLogs.history();
			expect(result).toEqual(mockHistory);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/app?history=true",
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("fetches app log history with optional params", async () => {
			const mockHistory = {
				entries: [],
				total: 0,
				page: 1,
				per_page: 20,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockHistory), { status: 200 }),
			);

			const result = await api.appLogs.history({
				level: "error",
				source: "api",
				search: "timeout",
				from: "2024-01-01",
				to: "2024-01-02",
				page: 2,
				per_page: 50,
				sort_by: "timestamp",
				sort_dir: "desc",
			});
			expect(result).toEqual(mockHistory);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/logs/app?history=true&"),
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(api.appLogs.history()).rejects.toThrow(
				"Failed to fetch app log history: 500 error",
			);
		});
	});

	describe("cursor", () => {
		it("fetches app logs with cursor pagination", async () => {
			const mockResponse = {
				data: [],
				has_more: false,
				next_cursor: null,
				prev_cursor: null,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.appLogs.cursor({
				direction: "after",
				limit: 10,
			});
			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/logs/app/cursor?"),
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("includes optional params in URL", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ data: [], has_more: false }), {
					status: 200,
				}),
			);

			await api.appLogs.cursor({
				cursor: "abc123",
				direction: "before",
				limit: 20,
				level: "error",
				source: "api",
				search: "timeout",
				from: "2024-01-01",
				to: "2024-01-02",
				sort_dir: "desc",
			});
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining(
					"/api/logs/app/cursor?cursor=abc123&direction=before&limit=20",
				),
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(
				api.appLogs.cursor({ direction: "after", limit: 10 }),
			).rejects.toThrow("Failed to fetch app logs (cursor): 500 error");
		});
	});
});
