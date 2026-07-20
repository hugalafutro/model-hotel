import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.logs", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches logs without params", async () => {
			const mockLogs = { entries: [], total: 0, page: 1, per_page: 20 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockLogs), { status: 200 }),
			);

			const result = await api.logs.list();
			expect(result).toEqual(mockLogs);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs",
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("fetches logs with optional params", async () => {
			const mockLogs = { entries: [], total: 0, page: 1, per_page: 20 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockLogs), { status: 200 }),
			);

			const result = await api.logs.list({
				page: 2,
				per_page: 50,
				model_id: "model-1",
				provider_id: "prov-1",
				status_code: "200",
				from: "2024-01-01",
				to: "2024-01-02",
				sort_by: "timestamp",
				sort_dir: "desc",
			});
			expect(result).toEqual(mockLogs);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/logs?"),
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
			await expect(api.logs.list()).rejects.toThrow(
				"Failed to fetch logs: 500 error",
			);
		});
	});

	describe("purge", () => {
		it("purges logs older than specified date", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.logs.purge("2024-01-01");
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/purge",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
					body: JSON.stringify({ older_than: "2024-01-01" }),
				}),
			);
		});

		it("throws error with response text on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("database locked", { status: 500 }),
			);
			// The message is the bare status + body; the caller's toast supplies
			// the "Failed to delete requests" prefix.
			await expect(api.logs.purge("2024-01-01")).rejects.toThrow(
				"500 database locked",
			);
		});
	});

	describe("cursor", () => {
		it("fetches logs with cursor pagination", async () => {
			const mockResponse = {
				data: [],
				has_more: false,
				next_cursor: null,
				prev_cursor: null,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.logs.cursor({
				direction: "after",
				limit: 10,
			});
			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/logs/cursor?"),
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

			await api.logs.cursor({
				cursor: "abc123",
				direction: "before",
				limit: 20,
				model_id: "model-1",
				provider_id: "prov-1",
				status_code: "200",
				from: "2024-01-01",
				to: "2024-01-02",
				sort_dir: "desc",
			});
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining(
					"/api/logs/cursor?cursor=abc123&direction=before&limit=20",
				),
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(
				api.logs.cursor({ direction: "after", limit: 10 }),
			).rejects.toThrow("Failed to fetch logs (cursor): 500 error");
		});
	});

	describe("get", () => {
		it("fetches a log entry by id", async () => {
			const mockEntry = { id: "log-123", model_id: "gpt-4", status_code: 200 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockEntry), { status: 200 }),
			);

			const result = await api.logs.get("log-123");

			expect(result).toEqual(mockEntry);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/log-123",
				expect.objectContaining({
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("encodes the id in the URL", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ id: "uuid-with/special" }), {
					status: 200,
				}),
			);

			await api.logs.get("uuid-with/special");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/logs/uuid-with%2Fspecial",
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.logs.get("nonexistent")).rejects.toThrow(
				"Failed to fetch log: 404 not found",
			);
		});
	});
});
