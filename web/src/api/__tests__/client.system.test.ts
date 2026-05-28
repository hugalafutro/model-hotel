import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api, setAdminToken } from "../client";

describe("api.system", () => {
	beforeEach(() => {
		setAdminToken("test-token");
		vi.restoreAllMocks();
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2024-01-15T10:30:00Z"));
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("fetches system stats with since parameter", async () => {
		const mockStats = {
			cpu_usage: 45.2,
			memory_usage: 67.8,
			disk_usage: 32.1,
		};
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response(JSON.stringify(mockStats), { status: 200 }),
		);

		const result = await api.system.get();

		// Compute expected midnight using the same local-timezone logic
		// as the implementation (new Date(year, month, date))
		const now = new Date();
		const midnight = new Date(now.getFullYear(), now.getMonth(), now.getDate());
		const expectedSince = encodeURIComponent(midnight.toISOString());

		expect(result).toEqual(mockStats);
		expect(globalThis.fetch).toHaveBeenCalledWith(
			`/api/system?since=${expectedSince}`,
			expect.objectContaining({
				headers: expect.objectContaining({
					Authorization: "Bearer test-token",
				}),
			}),
		);
	});

	it("throws on error response", async () => {
		vi.spyOn(globalThis, "fetch").mockResolvedValue(
			new Response("internal error", { status: 500 }),
		);

		await expect(api.system.get()).rejects.toThrow(
			"Failed to fetch system stats: 500 internal error",
		);
	});
});
