import { beforeEach, describe, expect, it, vi } from "vitest";
import { api, setAdminToken } from "../client";

describe("api.settings", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
	});

	describe("get", () => {
		it("fetches settings", async () => {
			const mockSettings = { theme: "dark", language: "en" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockSettings), { status: 200 }),
			);

			const result = await api.settings.get();

			expect(result).toEqual(mockSettings);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/settings",
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

			await expect(api.settings.get()).rejects.toThrow(
				"Failed to fetch settings: 404 not found",
			);
		});
	});

	describe("update", () => {
		it("updates settings", async () => {
			const newSettings = { theme: "light", language: "fr" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(newSettings), { status: 200 }),
			);

			const result = await api.settings.update(newSettings);

			expect(result).toEqual(newSettings);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/settings",
				expect.objectContaining({
					method: "PUT",
					headers: expect.objectContaining({
						Authorization: "Bearer test-token",
						"Content-Type": "application/json",
					}),
					body: JSON.stringify(newSettings),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("validation failed", { status: 400 }),
			);

			await expect(api.settings.update({})).rejects.toThrow(
				"Failed to update settings: 400 validation failed",
			);
		});
	});
});
