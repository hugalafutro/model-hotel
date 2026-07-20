import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.virtualKeys", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches virtual keys list", async () => {
			const mockKeys = [
				{ id: "1", name: "key1", key: "vk_abc123" },
				{ id: "2", name: "key2", key: "vk_def456" },
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockKeys), { status: 200 }),
			);

			const result = await api.virtualKeys.list();

			expect(result).toEqual(mockKeys);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys",
				expect.objectContaining({
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("unauthorized", { status: 401 }),
			);

			await expect(api.virtualKeys.list()).rejects.toThrow(
				"Failed to fetch virtual keys: 401 unauthorized",
			);
		});
	});

	describe("create", () => {
		it("creates a virtual key", async () => {
			const requestBody = {
				name: "new-key",
				rate_limit_rps: 10,
				rate_limit_burst: 20,
			};
			const mockResponse = {
				id: "3",
				...requestBody,
				key: "vk_new123",
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.virtualKeys.create(
				requestBody.name,
				requestBody.rate_limit_rps,
				requestBody.rate_limit_burst,
			);

			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({}),
					body: JSON.stringify(requestBody),
				}),
			);
		});

		it("creates with null rate limits", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({ id: "4", name: "unlimited" }), {
					status: 200,
				}),
			);

			await api.virtualKeys.create("unlimited", null, null);

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys",
				expect.objectContaining({
					body: JSON.stringify({
						name: "unlimited",
						rate_limit_rps: null,
						rate_limit_burst: null,
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("duplicate name", { status: 409 }),
			);

			await expect(api.virtualKeys.create("dup")).rejects.toThrow(
				"Failed to create virtual key: 409 duplicate name",
			);
		});
	});

	describe("get", () => {
		it("fetches a virtual key by id", async () => {
			const mockKey = { id: "1", name: "key1", key: "vk_abc123" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockKey), { status: 200 }),
			);

			const result = await api.virtualKeys.get("1");

			expect(result).toEqual(mockKey);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys/1",
				expect.objectContaining({
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.virtualKeys.get("nonexistent")).rejects.toThrow(
				"Failed to fetch virtual key: 404 not found",
			);
		});
	});

	describe("update", () => {
		it("updates a virtual key", async () => {
			const updateData = {
				name: "updated-key",
				rate_limit_rps: 50,
			};
			const mockResponse = { id: "1", ...updateData, key: "vk_abc123" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.virtualKeys.update("1", updateData);

			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys/1",
				expect.objectContaining({
					method: "PUT",
					headers: expect.objectContaining({}),
					body: JSON.stringify(updateData),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(
				api.virtualKeys.update("1", { name: "test" }),
			).rejects.toThrow("Failed to update virtual key: 404 not found");
		});
	});

	describe("delete", () => {
		it("deletes a virtual key", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.virtualKeys.delete("1");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/virtual-keys/1",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("throws fixed error on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 500 }),
			);

			await expect(api.virtualKeys.delete("1")).rejects.toThrow(
				"Failed to delete virtual key",
			);
		});
	});
});
