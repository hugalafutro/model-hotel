import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import { server } from "../../test/server";
import { api } from "../client";

const snapshot = {
	provider_name: "nano",
	type: "nanogpt",
	kind: "usage",
	payload: { active: true },
	http_status: 200,
	fetched_at: "2026-07-26T10:00:00Z",
};

describe("quota api", () => {
	it("reads the quota snapshot list", async () => {
		server.use(
			http.get("/api/quota", () => HttpResponse.json({ quota: [snapshot] })),
		);
		const { quota } = await api.getQuota();
		expect(quota).toHaveLength(1);
		expect(quota[0].provider_name).toBe("nano");
		expect(quota[0].http_status).toBe(200);
	});

	it("reads an authoritative empty list", async () => {
		server.use(http.get("/api/quota", () => HttpResponse.json({ quota: [] })));
		const { quota } = await api.getQuota();
		expect(quota).toEqual([]);
	});

	it("rejects when the primary is unreachable", async () => {
		server.use(
			http.get("/api/quota", () =>
				HttpResponse.json(
					{ error: "could not reach the fleet primary" },
					{ status: 502 },
				),
			),
		);
		await expect(api.getQuota()).rejects.toMatchObject({ status: 502 });
	});

	it("posts a refresh and returns the counters", async () => {
		server.use(
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ results: [], refreshed: 2, failed: 0, skipped: 1 }),
			),
		);
		const out = await api.refreshQuota();
		expect(out.refreshed).toBe(2);
		expect(out.skipped).toBe(1);
	});

	it("rejects when the primary cannot refresh", async () => {
		server.use(
			http.post("/api/quota/refresh", () =>
				HttpResponse.json(
					{ error: "the primary could not refresh its quotas" },
					{ status: 502 },
				),
			),
		);
		await expect(api.refreshQuota()).rejects.toMatchObject({ status: 502 });
	});
});
