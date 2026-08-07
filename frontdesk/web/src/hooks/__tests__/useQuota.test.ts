import { act, renderHook, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { QuotaSnapshot } from "../../api/types";
import { server } from "../../test/server";
import { useQuota } from "../useQuota";

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

// The strip only ever mounts inside the authenticated shell, so every test here
// runs with the readable half of the session cookie pair present, exactly as the
// real hook is used (its refresh POST reads it for CSRF). Cookies outlive a test
// in jsdom, so it is cleared again after each one.
beforeEach(() => {
	document.cookie = "fd_csrf=csrf-abc; path=/";
});
afterEach(() => {
	document.cookie = "fd_csrf=; path=/; max-age=0";
});

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

	it("starts empty before the first response lands", () => {
		server.use(okQuota());
		const { result } = renderHook(() => useQuota(false));
		expect(result.current.snapshots).toEqual([]);
		expect(result.current.lastUpdatedAt).toBeNull();
		expect(result.current.loading).toBe(true);
	});

	it("does not share a mutable empty snapshots array between hook instances", () => {
		// Both instances start from the empty-quota path. If that ever returns the
		// same array reference twice, an in-place mutation on one hook's snapshots
		// (e.g. a consumer's .sort()) would corrupt every other mount in the
		// session.
		server.use(failQuota());
		const { result: first } = renderHook(() => useQuota(false));
		expect(first.current.snapshots).toEqual([]);
		first.current.snapshots.push({ ...snapshot, provider_name: "leaked" });
		const { result: second } = renderHook(() => useQuota(false));
		expect(second.current.snapshots).toEqual([]);
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

// Last-good is the whole degrade story of the badges, and it is now purely
// in-memory: within one mount, a read that succeeded keeps showing until another
// read succeeds. Driven through the 60 second poll rather than a refresh, so
// these two stay about `read` itself and not about what refresh reports.
describe("useQuota last-good snapshots", () => {
	beforeEach(() => vi.useFakeTimers({ shouldAdvanceTime: true }));
	afterEach(() => vi.useRealTimers());

	it("keeps the last-good snapshots when a later poll fails and marks them stale", async () => {
		let getCalls = 0;
		server.use(
			http.get("/api/quota", () => {
				getCalls++;
				return getCalls === 1
					? HttpResponse.json({ quota: [snapshot] })
					: HttpResponse.json({ error: "nope" }, { status: 502 });
			}),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		expect(result.current.snapshots).toHaveLength(1);
		const firstStamp = result.current.lastUpdatedAt;
		expect(firstStamp).not.toBeNull();

		await act(async () => {
			await vi.advanceTimersByTimeAsync(60_000);
		});
		await waitFor(() => expect(result.current.error).toBe(true));
		expect(getCalls).toBe(2);
		// A non-2xx means we could not ask the primary, which is not the same as
		// "there is nothing to show": the numbers stay, flagged as unconfirmed, and
		// the stamp keeps saying when they were actually read.
		expect(result.current.snapshots).toHaveLength(1);
		expect(result.current.snapshots[0]?.provider_name).toBe("nano");
		expect(result.current.stale).toBe(true);
		expect(result.current.lastUpdatedAt).toBe(firstStamp);
	});

	it("clears the badges on an authoritative empty 200", async () => {
		// The other half of the same rule, and the distinction that matters: a 200
		// is authoritative in both directions, so an empty list (no primary
		// designated) must WIPE what a failed read would have kept.
		let getCalls = 0;
		server.use(
			http.get("/api/quota", () => {
				getCalls++;
				return HttpResponse.json({ quota: getCalls === 1 ? [snapshot] : [] });
			}),
		);
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		expect(result.current.snapshots).toHaveLength(1);
		const firstStamp = result.current.lastUpdatedAt;

		await act(async () => {
			await vi.advanceTimersByTimeAsync(60_000);
		});
		await waitFor(() => expect(result.current.snapshots).toEqual([]));
		expect(getCalls).toBe(2);
		expect(result.current.error).toBe(false);
		expect(result.current.stale).toBe(false);
		// An empty 200 is still a successful read, so it stamps a fresh
		// lastUpdatedAt rather than leaving the previous, now-superseded one.
		expect(result.current.lastUpdatedAt).not.toBe(firstStamp);
		expect(result.current.lastUpdatedAt).not.toBeNull();
	});
});

// Snapshots used to be persisted to localStorage so a reload repainted the
// badges instantly. That is removed on purpose: Front Desk is a shared control
// plane and the exposure was the STORAGE, not the paint. These two pin the
// removal from both sides, because every other test here would still pass if
// somebody reintroduced the seed.
describe("useQuota persists nothing", () => {
	it("never seeds from localStorage, whatever is stored there", async () => {
		const planted = JSON.stringify({
			snapshots: [{ ...snapshot, provider_name: "from-storage" }],
			lastUpdatedAt: "2026-07-26T09:00:00Z",
		});
		// Every key shape the removed cache ever used, plus a bare guess, so a
		// reintroduced seed under any of them trips this.
		for (const key of [
			"fdQuotaSnapshots",
			"fdQuotaSnapshots:1a2b3c4d",
			"quotaSnapshots",
			"fdQuota",
		]) {
			localStorage.setItem(key, planted);
		}
		// The read fails, so anything the hook reports could only have come from
		// storage: with no seed there is nothing to keep and nothing to go stale.
		server.use(failQuota());
		const { result } = renderHook(() => useQuota(false));
		// Asserted before the response as well as after: a seed is a first-paint
		// thing, so only checking the settled state would miss it.
		expect(result.current.snapshots).toEqual([]);
		expect(result.current.lastUpdatedAt).toBeNull();
		await waitFor(() => expect(result.current.error).toBe(true));
		expect(result.current.snapshots).toEqual([]);
		expect(result.current.lastUpdatedAt).toBeNull();
		expect(result.current.stale).toBe(false);
	});

	it("writes nothing to localStorage when a read succeeds", async () => {
		// The exposure itself: whatever is written is readable with devtools by
		// whoever sits down at this browser next, painted or not.
		const before = Object.keys(localStorage).sort();
		server.use(okQuota());
		const { result } = renderHook(() => useQuota(false));
		await waitFor(() => expect(result.current.loading).toBe(false));
		expect(result.current.snapshots).toHaveLength(1);
		expect(Object.keys(localStorage).sort()).toEqual(before);
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
