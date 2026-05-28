import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, setAdminToken } from "../client";

describe("api.failoverGroups", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("list", () => {
		it("fetches failover groups list", async () => {
			const mockGroups = [
				{ id: "1", name: "group1", models: ["model-a"] },
				{ id: "2", name: "group2", models: ["model-b"] },
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockGroups), { status: 200 }),
			);

			const result = await api.failoverGroups.list();

			expect(result).toEqual(mockGroups);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("unauthorized", { status: 401 }),
			);

			await expect(api.failoverGroups.list()).rejects.toThrow(
				"Failed to fetch failover groups: 401 unauthorized",
			);
		});
	});

	describe("get", () => {
		it("fetches a failover group by id", async () => {
			const mockGroup = {
				id: "1",
				name: "primary-group",
				models: ["openai/gpt-4", "anthropic/claude-3"],
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockGroup), { status: 200 }),
			);

			const result = await api.failoverGroups.get("1");

			expect(result).toEqual(mockGroup);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/1",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.failoverGroups.get("nonexistent")).rejects.toThrow(
				"Failed to fetch failover group: 404 not found",
			);
		});
	});

	describe("create", () => {
		it("creates a failover group", async () => {
			const createData = {
				display_model: "hotel/my-group",
				display_name: "new-group",
				description: "Test failover group",
				entry_ids: ["model-a", "model-b"],
			};
			const mockResponse = { id: "3", ...createData };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.failoverGroups.create(createData);

			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					body: JSON.stringify(createData),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("duplicate name", { status: 409 }),
			);

			await expect(
				api.failoverGroups.create({
					display_model: "hotel/dup",
					entry_ids: [],
				}),
			).rejects.toThrow("Failed to create failover group: 409 duplicate name");
		});
	});

	describe("update", () => {
		it("updates a failover group", async () => {
			const updateData = {
				display_name: "updated-group",
				description: "Updated description",
				group_enabled: true,
				priority_order: ["model-c"],
			};
			const mockResponse = { id: "1", ...updateData };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResponse), { status: 200 }),
			);

			const result = await api.failoverGroups.update("1", updateData);

			expect(result).toEqual(mockResponse);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/1",
				expect.objectContaining({
					method: "PUT",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
					body: JSON.stringify(updateData),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(
				api.failoverGroups.update("1", {
					display_name: "test",
					group_enabled: false,
				}),
			).rejects.toThrow("Failed to update failover group: 404 not found");
		});
	});

	describe("delete", () => {
		it("deletes a failover group", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 204 }),
			);

			await api.failoverGroups.delete("1");

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/1",
				expect.objectContaining({
					method: "DELETE",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws fixed error on failure", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(null, { status: 500 }),
			);

			await expect(api.failoverGroups.delete("1")).rejects.toThrow(
				"Failed to delete failover group",
			);
		});
	});

	describe("sync", () => {
		it("syncs failover groups", async () => {
			const mockResult = { synced: 5, failed: 0 };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockResult), { status: 200 }),
			);

			const result = await api.failoverGroups.sync();

			expect(result).toEqual(mockResult);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/sync",
				expect.objectContaining({
					method: "POST",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("sync failed", { status: 500 }),
			);

			await expect(api.failoverGroups.sync()).rejects.toThrow(
				"Failed to sync failover groups: 500 sync failed",
			);
		});
	});

	describe("candidates", () => {
		it("fetches candidate models", async () => {
			const mockCandidates = [
				{ model_id: "gpt-4", provider_id: "openai" },
				{ model_id: "claude-3", provider_id: "anthropic" },
			];
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockCandidates), { status: 200 }),
			);

			const result = await api.failoverGroups.candidates();

			expect(result).toEqual(mockCandidates);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/failover-groups/candidates",
				expect.objectContaining({
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
					}),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("service unavailable", { status: 503 }),
			);

			await expect(api.failoverGroups.candidates()).rejects.toThrow(
				"Failed to fetch candidates: 503 service unavailable",
			);
		});
	});
});
