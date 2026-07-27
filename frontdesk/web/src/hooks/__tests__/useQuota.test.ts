import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clearAuthToken, setAuthToken } from "../../api/client";
import type { QuotaSnapshot } from "../../api/types";
import { server } from "../../test/server";
import {
	clearQuotaCache,
	QUOTA_CACHE_PREFIX,
	quotaCacheKey,
	useQuota,
} from "../useQuota";

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

/** The key the hook would use right now. Non-null only while a token is stored. */
function currentKey(): string {
	const key = quotaCacheKey();
	if (!key) throw new Error("no auth token stored, so there is no cache key");
	return key;
}

/**
 * Writes a cache entry as `token`'s session would have, then restores whichever
 * token was in place. Lets a test plant one operator's snapshots and then look
 * at the world as a different operator.
 */
function seedCacheFor(
	token: string,
	snapshots: QuotaSnapshot[] = [snapshot],
	lastUpdatedAt: string | null = "2026-07-26T09:00:00Z",
) {
	const previous = localStorage.getItem("fdAuthToken");
	setAuthToken(token);
	localStorage.setItem(
		currentKey(),
		JSON.stringify({ snapshots, lastUpdatedAt }),
	);
	if (previous === null) clearAuthToken();
	else setAuthToken(previous);
}

// The strip only ever mounts inside the authenticated shell, so every test here
// runs with a session token unless it is specifically about not having one.
// setup.ts clears localStorage after each test, so this does not leak.
beforeEach(() => setAuthToken("operator-a"));

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
		const cached = JSON.parse(localStorage.getItem(currentKey()) as string);
		expect(cached.snapshots).toHaveLength(1);
	});

	it("seeds from the cache before the first response lands", () => {
		seedCacheFor("operator-a");
		server.use(okQuota());
		const { result } = renderHook(() => useQuota(false));
		expect(result.current.snapshots).toHaveLength(1);
		expect(result.current.lastUpdatedAt).toBe("2026-07-26T09:00:00Z");
	});

	it("ignores a malformed cache entry", () => {
		localStorage.setItem(currentKey(), "{not json");
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
		seedCacheFor("operator-a");
		server.use(okQuota([]));
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.snapshots).toEqual([]));
		// An empty 200 is still a successful read, so it must stamp a fresh
		// lastUpdatedAt rather than leaving the seeded, now-stale timestamp.
		expect(result.current.lastUpdatedAt).not.toBe("2026-07-26T09:00:00Z");
		const cached = JSON.parse(localStorage.getItem(currentKey()) as string);
		expect(cached.snapshots).toEqual([]);
	});

	it("preserves cached snapshots on a 502 and marks them stale", async () => {
		seedCacheFor("operator-a");
		server.use(failQuota());
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.error).toBe(true));
		expect(result.current.snapshots).toHaveLength(1);
		expect(result.current.stale).toBe(true);
		// The persisted cache must survive the failure too, not just in-memory
		// state: a non-2xx must never wipe what a reload would seed from.
		const cached = JSON.parse(localStorage.getItem(currentKey()) as string);
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
		seedCacheFor("operator-a");
		const key = currentKey();
		clearQuotaCache();
		expect(localStorage.getItem(key)).toBeNull();
	});

	it("removes every namespaced entry, not only the signed-in session's", () => {
		// Keys accumulate one per token this browser has signed in with. A clear
		// that only removed the current one would narrow what this used to do and
		// leave the older operators' snapshots sitting in localStorage.
		seedCacheFor("operator-a");
		seedCacheFor("operator-b");
		seedCacheFor("operator-c");
		const keys = Object.keys(localStorage).filter((k) =>
			k.startsWith(QUOTA_CACHE_PREFIX),
		);
		expect(keys).toHaveLength(3);

		clearQuotaCache();

		expect(
			Object.keys(localStorage).filter((k) => k.startsWith(QUOTA_CACHE_PREFIX)),
		).toEqual([]);
	});

	it("still clears after the auth token has already been dropped", () => {
		// App's logout clears the token before calling this, so a clear that
		// resolved the key from the current token would find null and no-op.
		seedCacheFor("operator-a");
		const key = currentKey();
		clearAuthToken();
		clearQuotaCache();
		expect(localStorage.getItem(key)).toBeNull();
	});

	it("leaves other localStorage keys alone", () => {
		seedCacheFor("operator-a");
		localStorage.setItem("fdQuotaCollapsed", "true");
		localStorage.setItem("fdQuotaBarMode", "used");
		clearQuotaCache();
		expect(localStorage.getItem("fdQuotaCollapsed")).toBe("true");
		expect(localStorage.getItem("fdQuotaBarMode")).toBe("used");
		expect(localStorage.getItem("fdAuthToken")).toBe("operator-a");
	});

	it("leaves a fresh mount with nothing to seed from", () => {
		seedCacheFor("operator-a");
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

describe("useQuota cache namespacing", () => {
	it("does not seed one operator's snapshots into another operator's session", async () => {
		// Operator A signs in and their read lands, filling the cache.
		setAuthToken("operator-a");
		server.use(okQuota([{ ...snapshot, provider_name: "a-only" }]));
		const a = renderHook(() => useQuota(false));
		await waitFor(() => expect(a.result.current.loading).toBe(false));
		expect(a.result.current.snapshots[0]?.provider_name).toBe("a-only");
		a.unmount();

		// Operator B reloads on the same browser with their own (here: expired)
		// token, so the shell mounts before anything is validated and the first
		// read fails with a 502 rather than a 401. That is the path where the
		// cleanup on logout/401 never runs, so the seed is the only thing that
		// could put A's numbers on B's screen.
		setAuthToken("operator-b");
		server.use(failQuota());
		const b = renderHook(() => useQuota(false));
		// Asserted before the response as well as after: the leak is a first-paint
		// leak, so an empty result that only arrives once the read finishes would
		// not be a fix.
		expect(b.result.current.snapshots).toEqual([]);
		expect(b.result.current.lastUpdatedAt).toBeNull();
		await waitFor(() => expect(b.result.current.error).toBe(true));
		expect(b.result.current.snapshots).toEqual([]);
		expect(b.result.current.stale).toBe(false);
	});

	it("still repaints the same operator's snapshots on remount", async () => {
		// The whole point of the cache. Namespacing must not quietly delete it.
		setAuthToken("operator-a");
		server.use(okQuota([{ ...snapshot, provider_name: "a-only" }]));
		const first = renderHook(() => useQuota(false));
		await waitFor(() => expect(first.result.current.loading).toBe(false));
		first.unmount();

		// Same token, and the read fails this time, so anything on screen can only
		// have come from the seed.
		server.use(failQuota());
		const second = renderHook(() => useQuota(false));
		expect(second.result.current.snapshots[0]?.provider_name).toBe("a-only");
		expect(second.result.current.lastUpdatedAt).not.toBeNull();
		await waitFor(() => expect(second.result.current.error).toBe(true));
		expect(second.result.current.stale).toBe(true);
	});

	it("does not write a cache entry when no token is stored", async () => {
		clearAuthToken();
		server.use(okQuota());
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		// The read still applies in memory; there is just no session to persist it
		// under, so nothing is left behind for the next operator to pick up.
		expect(result.current.snapshots).toHaveLength(1);
		expect(
			Object.keys(localStorage).filter((k) => k.startsWith(QUOTA_CACHE_PREFIX)),
		).toEqual([]);
	});

	it("does not seed from anything when no token is stored", () => {
		seedCacheFor("operator-a");
		clearAuthToken();
		server.use(failQuota());
		const { result } = renderHook(() => useQuota(false));
		expect(result.current.snapshots).toEqual([]);
		expect(result.current.lastUpdatedAt).toBeNull();
	});

	it("keys the entry by the token without storing the token itself", () => {
		setAuthToken("super-secret-session-token");
		server.use(failQuota());
		renderHook(() => useQuota(false));
		const key = currentKey();
		expect(key.startsWith(`${QUOTA_CACHE_PREFIX}:`)).toBe(true);
		expect(key).not.toContain("super-secret-session-token");
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

	it("reports failure when the POST succeeds but the read-back does not", async () => {
		// The sweep was accepted and every provider answered, but we could not read
		// the result back. Nothing on screen came from that sweep: the badges (and
		// any open modal) still hold the pre-refresh numbers and are flagged stale.
		// Claiming success there tells the operator their data is current when the
		// UI is showing exactly what it showed before they pressed the button.
		let getCalls = 0;
		server.use(
			http.get("/api/quota", () => {
				getCalls++;
				return getCalls === 1
					? HttpResponse.json({ quota: [snapshot] })
					: HttpResponse.json({ error: "nope" }, { status: 502 });
			}),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ results: [], refreshed: 1, failed: 0, skipped: 0 }),
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
		// The last-good preservation in `read` is untouched: this fix changes what
		// refresh REPORTS, not what it keeps.
		expect(result.current.snapshots).toHaveLength(1);
		expect(result.current.stale).toBe(true);
	});

	it("reports success only when the read-back also lands", async () => {
		// The other direction of the same check, on the same handler shape: the
		// second GET succeeds, so the numbers on screen really are the swept ones.
		let getCalls = 0;
		server.use(
			http.get("/api/quota", () => {
				getCalls++;
				return HttpResponse.json({ quota: [snapshot] });
			}),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ results: [], refreshed: 1, failed: 0, skipped: 0 }),
			),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		let outcome: string | undefined;
		await act(async () => {
			outcome = await result.current.refresh();
		});
		expect(outcome).toBe("ok");
		expect(getCalls).toBe(2);
		expect(result.current.stale).toBe(false);
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

	it("reports success when nothing supersedes the read-back", async () => {
		// The control for the superseded-read-back fix below: reporting failure
		// only when a read could not apply its own result must not degrade into
		// reporting failure always. Nothing overtakes this read-back, so it
		// applies, and the outcome stays "ok".
		let getCalls = 0;
		server.use(
			http.get("/api/quota", () => {
				getCalls++;
				return HttpResponse.json({
					quota: [{ ...snapshot, provider_name: `read-${getCalls}` }],
				});
			}),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ results: [], refreshed: 1, failed: 0, skipped: 0 }),
			),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		let outcome: string | undefined;
		await act(async () => {
			outcome = await result.current.refresh();
		});
		expect(outcome).toBe("ok");
		// The read-back really did apply: the badge shows the second GET's body.
		expect(result.current.snapshots[0]?.provider_name).toBe("read-2");
		expect(result.current.error).toBe(false);
		expect(result.current.stale).toBe(false);
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

// A refresh's read-back can be overtaken by the 60 second poll. The sequence
// guard then throws the read-back's response away, so a 200 on that GET says
// nothing about what ends up on screen, and reporting it as success put a
// "refreshed" toast over numbers the superseding poll had just flagged stale.
// Fake timers drive the real interleaving rather than stubbing what `read`
// returns, matching the pattern in the "useQuota polling" describe above.
describe("useQuota refresh superseded by a poll", () => {
	beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
	afterEach(() => vi.useRealTimers());

	it("reports failure when the poll that supersedes the read-back fails", async () => {
		let getCalls = 0;
		let releaseReadBack: (() => void) | undefined;
		const readBackGate = new Promise<void>((resolve) => {
			releaseReadBack = resolve;
		});
		server.use(
			http.get("/api/quota", async () => {
				getCalls++;
				if (getCalls === 2) {
					// The refresh's read-back. It hangs until we release it, so the
					// poll is guaranteed to start (and bump the sequence) first, and
					// it comes back 200: the whole point is that a 200 is not enough.
					await readBackGate;
					return HttpResponse.json({
						quota: [{ ...snapshot, provider_name: "read-back" }],
					});
				}
				if (getCalls >= 3) {
					// The superseding poll, which fails.
					return HttpResponse.json({ error: "nope" }, { status: 502 });
				}
				return HttpResponse.json({ quota: [snapshot] });
			}),
			http.post("/api/quota/refresh", () =>
				HttpResponse.json({ results: [], refreshed: 1, failed: 0, skipped: 0 }),
			),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		expect(getCalls).toBe(1);

		// Start the refresh but do not await it: its read-back is the GET we need
		// to leave in flight.
		let pending: Promise<string> | undefined;
		act(() => {
			pending = result.current.refresh();
		});
		await waitFor(() => expect(getCalls).toBe(2));

		// The poll fires while that read-back is still open, so it takes over the
		// sequence, and it fails.
		await act(async () => {
			await vi.advanceTimersByTimeAsync(60_000);
		});
		await waitFor(() => expect(getCalls).toBe(3));
		await waitFor(() => expect(result.current.error).toBe(true));

		// Now let the superseded read-back land.
		releaseReadBack?.();
		let outcome: string | undefined;
		await act(async () => {
			outcome = await pending;
		});
		expect(outcome).toBe("failed");
		// Its response was discarded, so the pre-refresh snapshot is still what the
		// operator sees, and it is flagged stale. A success toast over that would
		// be a lie, which is exactly what the outcome above prevents.
		expect(result.current.snapshots[0]?.provider_name).toBe("nano");
		expect(result.current.stale).toBe(true);
	});
});
