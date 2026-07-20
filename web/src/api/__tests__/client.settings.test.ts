import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.settings", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
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
					headers: expect.objectContaining({}),
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

	describe("reset", () => {
		it("sends a DELETE with the keys to reset and returns the new values", async () => {
			const reset = { log_retention: "0", stale_request_timeout: "30m0s" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(reset), { status: 200 }),
			);

			const result = await api.settings.reset([
				"log_retention",
				"stale_request_timeout",
			]);

			expect(result).toEqual(reset);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/settings",
				expect.objectContaining({
					method: "DELETE",
					body: JSON.stringify({
						keys: ["log_retention", "stale_request_timeout"],
					}),
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("defaults to an empty keys list when called with no arguments", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify({}), { status: 200 }),
			);

			await api.settings.reset();

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/settings",
				expect.objectContaining({
					method: "DELETE",
					body: JSON.stringify({ keys: [] }),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("nope", { status: 500 }),
			);
			await expect(api.settings.reset(["x"])).rejects.toThrow(
				"Failed to reset settings: 500 nope",
			);
		});
	});
});
