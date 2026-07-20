import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../client";

describe("api.version", () => {
	beforeEach(() => {
		document.cookie = "mh_csrf=test-csrf; path=/";
		vi.restoreAllMocks();
	});

	describe("getLatest", () => {
		it("fetches latest version", async () => {
			const mockVersion = { tag_name: "v1.2.3" };
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockVersion), { status: 200 }),
			);

			const result = await api.version.getLatest();

			expect(result).toEqual(mockVersion);
			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/version/latest",
				expect.objectContaining({
					headers: expect.objectContaining({}),
				}),
			);
		});

		it("passes custom options to fetch", async () => {
			const mockVersion = { tag_name: "v1.2.4" };
			const customOptions: RequestInit = {
				cache: "no-cache",
				signal: AbortSignal.timeout(5000),
			};
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response(JSON.stringify(mockVersion), { status: 200 }),
			);

			await api.version.getLatest(customOptions);

			expect(globalThis.fetch).toHaveBeenCalledWith(
				"/api/version/latest",
				expect.objectContaining({
					headers: expect.objectContaining({}),
					cache: "no-cache",
					signal: expect.any(AbortSignal),
				}),
			);
		});

		it("throws on error response", async () => {
			vi.spyOn(globalThis, "fetch").mockResolvedValue(
				new Response("not found", { status: 404 }),
			);

			await expect(api.version.getLatest()).rejects.toThrow(
				"Failed to fetch latest version: 404 not found",
			);
		});
	});
});
