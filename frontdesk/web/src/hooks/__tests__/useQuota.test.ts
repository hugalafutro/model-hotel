import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { QuotaSnapshot } from "../../api/types";
import { server } from "../../test/server";
import { clearQuotaCache, QUOTA_CACHE_KEY, useQuota } from "../useQuota";

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

	it("does not share a mutable empty snapshots array between hook instances", () => {
		// No cache present, so both instances fall back to the empty-quota path.
		// If that fallback ever returns the same array reference twice, an
		// in-place mutation on one hook's snapshots (e.g. a consumer's .sort())
		// would corrupt every other mount in the session.
		server.use(failQuota());
		const { result: first } = renderHook(() => useQuota(false));
		expect(first.current.snapshots).toEqual([]);
		first.current.snapshots.push({ ...snapshot, provider_name: "leaked" });
		const { result: second } = renderHook(() => useQuota(false));
		expect(second.current.snapshots).toEqual([]);
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
		// An empty 200 is still a successful read, so it must stamp a fresh
		// lastUpdatedAt rather than leaving the seeded, now-stale timestamp.
		expect(result.current.lastUpdatedAt).not.toBe("2026-07-26T09:00:00Z");
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
		// The persisted cache must survive the failure too, not just in-memory
		// state: a non-2xx must never wipe what a reload would seed from.
		const cached = JSON.parse(localStorage.getItem(QUOTA_CACHE_KEY) as string);
		expect(cached.snapshots).toHaveLength(1);
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
		// Explicit handler: this test exercises refresh()'s POST, so it must not
		// rely on MSW's onUnhandledRequest fallback to make the network layer work.
		server.use(
			okQuota(),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ results: [], refreshed: 0, failed: 0, skipped: 0 }),
			),
		);
		await act(async () => {
			await result.current.refresh();
		});
		await waitFor(() => expect(result.current.error).toBe(false));
	});

	it("discards a stale read that resolves after a newer one", async () => {
		let getCalls = 0;
		let resolveFirst: (() => void) | undefined;
		const firstGate = new Promise<void>((resolve) => {
			resolveFirst = resolve;
		});
		server.use(
			http.get("/api/quota", async () => {
				getCalls++;
				if (getCalls === 1) {
					// The mount read: hangs until we explicitly release it, so it is
					// guaranteed to resolve after the second (refresh-triggered) read.
					await firstGate;
					return HttpResponse.json({
						quota: [{ ...snapshot, provider_name: "stale" }],
					});
				}
				return HttpResponse.json({
					quota: [{ ...snapshot, provider_name: "fresh" }],
				});
			}),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ results: [], refreshed: 1, failed: 0, skipped: 0 }),
			),
		);
		const { result } = renderHook(() => useQuota(false));
		// The mount read (seq 1) is still hanging. Fire a refresh, whose own
		// re-read (seq 2) resolves immediately and should become current.
		await act(async () => {
			await result.current.refresh();
		});
		expect(result.current.snapshots[0]?.provider_name).toBe("fresh");
		// Now release the older, slower mount read. Without the seq guard this
		// would clobber the newer state with the stale response.
		await act(async () => {
			resolveFirst?.();
			await firstGate;
		});
		expect(result.current.snapshots[0]?.provider_name).toBe("fresh");
	});
});

describe("clearQuotaCache", () => {
	it("removes the persisted snapshots", () => {
		localStorage.setItem(
			QUOTA_CACHE_KEY,
			JSON.stringify({
				snapshots: [snapshot],
				lastUpdatedAt: "2026-07-26T09:00:00Z",
			}),
		);
		clearQuotaCache();
		expect(localStorage.getItem(QUOTA_CACHE_KEY)).toBeNull();
	});

	it("leaves a fresh mount with nothing to seed from", () => {
		localStorage.setItem(
			QUOTA_CACHE_KEY,
			JSON.stringify({
				snapshots: [snapshot],
				lastUpdatedAt: "2026-07-26T09:00:00Z",
			}),
		);
		clearQuotaCache();
		// The failing read is the case that made the leak stick: the hook keeps
		// last-good data on a non-2xx, so if anything survived the clear it would
		// stay on screen for the whole next session.
		server.use(failQuota());
		const { result } = renderHook(() => useQuota(false));
		expect(result.current.snapshots).toEqual([]);
		expect(result.current.lastUpdatedAt).toBeNull();
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
		let getCalls = 0;
		server.use(
			http.get("/api/quota", () => {
				getCalls++;
				// The re-read after a successful refresh must reflect a genuinely
				// new GET, not the mount read's leftover state.
				const list =
					getCalls === 1
						? [snapshot]
						: [{ ...snapshot, provider_name: "after-refresh" }];
				return HttpResponse.json({ quota: list });
			}),
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
		expect(getCalls).toBe(1);
		let outcome: string | undefined;
		await act(async () => {
			outcome = await result.current.refresh();
		});
		expect(outcome).toBe("ok");
		expect(posted).toBe(1);
		expect(getCalls).toBe(2);
		expect(result.current.snapshots[0]?.provider_name).toBe("after-refresh");
	});

	it("reports failure but still re-reads so the last-good snapshot survives", async () => {
		let getCalls = 0;
		server.use(
			http.get("/api/quota", () => {
				getCalls++;
				return HttpResponse.json({ quota: [snapshot] });
			}),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ error: "nope" }, { status: 502 }),
			),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		expect(getCalls).toBe(1);
		let outcome: string | undefined;
		await act(async () => {
			outcome = await result.current.refresh();
		});
		expect(outcome).toBe("failed");
		expect(getCalls).toBe(2);
		expect(result.current.snapshots).toHaveLength(1);
	});

	it("reports failure for a 200 whose counters include a failed provider", async () => {
		// The POST succeeds at the HTTP level but the primary could not reach one
		// of the providers. Reporting "ok" here is a false success: the strip would
		// toast "refreshed" over data that did not refresh.
		let getCalls = 0;
		server.use(
			http.get("/api/quota", () => {
				getCalls++;
				return HttpResponse.json({ quota: [snapshot] });
			}),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({
					results: [],
					refreshed: 1,
					failed: 1,
					skipped: 0,
				}),
			),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		let outcome: string | undefined;
		await act(async () => {
			outcome = await result.current.refresh();
		});
		expect(outcome).toBe("failed");
		// Still re-reads, exactly as the transport-failure path does.
		expect(getCalls).toBe(2);
	});

	it("still reports success when the counters report no failures", async () => {
		// The other direction of the same check: `refreshed: 0` with nothing failed
		// (everything inside its own cooldown, so all skipped) is not a failure.
		server.use(
			okQuota(),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({
					results: [],
					refreshed: 0,
					failed: 0,
					skipped: 3,
				}),
			),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		let outcome: string | undefined;
		await act(async () => {
			outcome = await result.current.refresh();
		});
		expect(outcome).toBe("ok");
	});

	it("still enforces cooldown after a failed refresh POST", async () => {
		let posted = 0;
		server.use(
			okQuota(),
			http.post("/api/quota/refresh", () => {
				posted++;
				return HttpResponse.json({ error: "nope" }, { status: 502 });
			}),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		let first: string | undefined;
		await act(async () => {
			first = await result.current.refresh();
		});
		expect(first).toBe("failed");
		// The cooldown is measured from the last attempt actually sent, not the
		// last one that succeeded, so a failed POST must still gate the next call.
		let second: string | undefined;
		await act(async () => {
			second = await result.current.refresh();
		});
		expect(second).toBe("cooldown");
		expect(posted).toBe(1);
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
