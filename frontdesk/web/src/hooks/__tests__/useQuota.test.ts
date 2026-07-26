import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { QuotaSnapshot } from "../../api/types";
import { server } from "../../test/server";
import { QUOTA_CACHE_KEY, useQuota } from "../useQuota";

const snapshot: QuotaSnapshot = {
	provider_name: "nano",
	type: "nanogpt",
	kind: "usage",
	payload: { active: true },
	http_status: 200,
	fetched_at: "2026-07-26T10:00:00Z",
};

function okQuota(list: QuotaSnapshot[] = [snapshot]) {
	return http.get("/api/quota", () => HttpResponse.json({ quota: list }));
}

function failQuota(status = 502) {
	return http.get("/api/quota", () =>
		HttpResponse.json({ error: "nope" }, { status }),
	);
}

describe("useQuota", () => {
	it("loads snapshots and records a last-updated stamp", async () => {
		server.use(okQuota());
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		expect(result.current.snapshots).toHaveLength(1);
		expect(result.current.error).toBe(false);
		expect(result.current.stale).toBe(false);
		expect(result.current.lastUpdatedAt).not.toBeNull();
	});

	it("writes successful reads to the cache", async () => {
		server.use(okQuota());
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		const cached = JSON.parse(localStorage.getItem(QUOTA_CACHE_KEY) as string);
		expect(cached.snapshots).toHaveLength(1);
	});

	it("seeds from the cache before the first response lands", () => {
		localStorage.setItem(
			QUOTA_CACHE_KEY,
			JSON.stringify({
				snapshots: [snapshot],
				lastUpdatedAt: "2026-07-26T09:00:00Z",
			}),
		);
		server.use(okQuota());
		const { result } = renderHook(() => useQuota(false));
		expect(result.current.snapshots).toHaveLength(1);
		expect(result.current.lastUpdatedAt).toBe("2026-07-26T09:00:00Z");
	});

	it("ignores a malformed cache entry", () => {
		localStorage.setItem(QUOTA_CACHE_KEY, "{not json");
		server.use(okQuota());
		const { result } = renderHook(() => useQuota(false));
		expect(result.current.snapshots).toEqual([]);
	});

	it("clears snapshots and cache on an authoritative empty 200", async () => {
		localStorage.setItem(
			QUOTA_CACHE_KEY,
			JSON.stringify({
				snapshots: [snapshot],
				lastUpdatedAt: "2026-07-26T09:00:00Z",
			}),
		);
		server.use(okQuota([]));
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.snapshots).toEqual([]));
		const cached = JSON.parse(localStorage.getItem(QUOTA_CACHE_KEY) as string);
		expect(cached.snapshots).toEqual([]);
	});

	it("preserves cached snapshots on a 502 and marks them stale", async () => {
		localStorage.setItem(
			QUOTA_CACHE_KEY,
			JSON.stringify({
				snapshots: [snapshot],
				lastUpdatedAt: "2026-07-26T09:00:00Z",
			}),
		);
		server.use(failQuota());
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.error).toBe(true));
		expect(result.current.snapshots).toHaveLength(1);
		expect(result.current.stale).toBe(true);
	});

	it("is not stale on a failure when nothing was ever known", async () => {
		server.use(failQuota());
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.error).toBe(true));
		expect(result.current.snapshots).toEqual([]);
		expect(result.current.stale).toBe(false);
	});

	it("clears the error flag once a read succeeds again", async () => {
		server.use(failQuota());
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.error).toBe(true));
		server.use(okQuota());
		await act(async () => {
			await result.current.refresh();
		});
		await waitFor(() => expect(result.current.error).toBe(false));
	});
});

describe("useQuota polling", () => {
	beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
	afterEach(() => vi.useRealTimers());

	it("re-reads every 60 seconds while expanded", async () => {
		let calls = 0;
		server.use(
			http.get("/api/quota", () => {
				calls++;
				return HttpResponse.json({ quota: [snapshot] });
			}),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		expect(calls).toBe(1);
		await act(async () => {
			await vi.advanceTimersByTimeAsync(60_000);
		});
		await waitFor(() => expect(calls).toBe(2));
	});

	it("does not poll while collapsed", async () => {
		let calls = 0;
		server.use(
			http.get("/api/quota", () => {
				calls++;
				return HttpResponse.json({ quota: [snapshot] });
			}),
		);
		const { result } = renderHook(() => useQuota(true));
		await waitFor(() => expect(result.current.loading).toBe(false));
		expect(calls).toBe(1); // the mount read still happens
		await act(async () => {
			await vi.advanceTimersByTimeAsync(180_000);
		});
		expect(calls).toBe(1);
	});
});

describe("useQuota refresh", () => {
	it("posts a refresh, re-reads, and reports success", async () => {
		let posted = 0;
		server.use(
			okQuota(),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({
					results: [],
					refreshed: 1,
					failed: 0,
					skipped: 0,
				});
			}),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		let outcome: string | undefined;
		await act(async () => {
			outcome = await result.current.refresh();
		});
		expect(outcome).toBe("ok");
		expect(posted).toBe(1);
	});

	it("reports failure but still re-reads so the last-good snapshot survives", async () => {
		server.use(
			okQuota(),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ error: "nope" }, { status: 502 }),
			),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		let outcome: string | undefined;
		await act(async () => {
			outcome = await result.current.refresh();
		});
		expect(outcome).toBe("failed");
		expect(result.current.snapshots).toHaveLength(1);
	});

	it("refuses a second refresh inside the 10 second cooldown", async () => {
		let posted = 0;
		server.use(
			okQuota(),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({
					results: [],
					refreshed: 0,
					failed: 0,
					skipped: 0,
				});
			}),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		await act(async () => {
			await result.current.refresh();
		});
		let second: string | undefined;
		await act(async () => {
			second = await result.current.refresh();
		});
		expect(second).toBe("cooldown");
		expect(posted).toBe(1);
	});
});
