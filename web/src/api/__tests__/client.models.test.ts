import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.models", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches all models without provider_id", async () => {
			const mockModels = [{ id: "1", name: "gpt-4" }];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockModels), { status: 200 }),
			);

			const result = await api.models.list();
			expect(result).toEqual(mockModels);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models",
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("fetches models filtered by provider_id", async () => {
			const mockModels = [{ id: "1", name: "gpt-4" }];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockModels), { status: 200 }),
			);

			const result = await api.models.list("provider-123");
			expect(result).toEqual(mockModels);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models?provider_id=provider-123",
				expect.objectContaining({
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);
			await expect(api.models.list()).rejects.toThrow(
				"Failed to fetch models: 404 not found",
			);
		});
	});

	describe("cursor", () => {
		it("fetches models with cursor pagination", async () => {
			const mockResponse = {
				data: [{ id: "1", name: "gpt-4" }],
				has_more: false,
				next_cursor: null,
				prev_cursor: null,
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.models.cursor({
				direction: "after",
				limit: 10,
			});
			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining("/api/models/cursor?"),
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

			await api.models.cursor({
				cursor: "abc123",
				direction: "before",
				limit: 20,
				sort_by: "name",
				sort_dir: "desc",
				provider_id: "prov-1",
				search: "gpt",
				capabilities: "vision",
				outputs: "image,embedding",
			});
			expect(globalThis.fetch).toHaveBeenCalledWith(
				expect.stringContaining(
					"/api/models/cursor?cursor=abc123&direction=before&limit=20&sort_by=name&sort_dir=desc&provider_id=prov-1&search=gpt&capabilities=vision&outputs=image%2Cembedding",
				),
				expect.anything(),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("error", { status: 500 }),
			);
			await expect(
				api.models.cursor({ direction: "after", limit: 10 }),
			).rejects.toThrow("Failed to fetch models (cursor): 500 error");
		});
	});

	describe("update", () => {
		it("updates a model with PATCH request", async () => {
			const mockModel = { id: "1", display_name: "Updated Model" };
			const data = { display_name: "Updated Model", enabled: true };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockModel), { status: 200 }),
			);

			const result = await api.models.update("123", data);
			expect(result).toEqual(mockModel);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models/123",
				expect.objectContaining({
					method: "PATCH",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
					body: JSON.stringify(data),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);
			await expect(
				api.models.update("123", { display_name: "Test" }),
			).rejects.toThrow("Failed to update model: 404 not found");
		});
	});

	describe("test", () => {
		it("tests a model with POST request", async () => {
			const mockResult = {
				success: true,
				ttft_ms: 150,
				duration_ms: 500,
				streaming: true,
				response: "Hello!",
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.models.test("123");
			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models/123/test",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("test failed", { status: 500 }),
			);
			await expect(api.models.test("123")).rejects.toThrow(
				"Test failed: 500 test failed",
			);
		});
	});

	describe("delete", () => {
		it("deletes a model with DELETE request", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.models.delete("123");
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/models/123",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						"Content-Type": "application/json",
					}),
				}),
			);
		});

		it("throws fixed error on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 500 }),
			);
			await expect(api.models.delete("123")).rejects.toThrow(
				"Failed to delete model",
			);
		});
	});
});
