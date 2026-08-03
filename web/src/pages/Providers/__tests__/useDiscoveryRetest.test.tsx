import { useQuery } from "@tanstack/react-query";
import { act, renderHook, screen, waitFor } from "@testing-library/react";
import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import type { DiscoveryDiff } from "../../../api/types";
import { server } from "../../../test/mocks/server";
import { AllProviders } from "../../../test/utils";
import type { DiscoverySummaryEntry } from "../DiscoverySummaryModal";
import { useDiscoveryRetest } from "../useDiscoveryRetest";

const discover = vi.fn();
vi.mock("../../../api/client", async (importOriginal) => {
	const actual = await importOriginal<typeof import("../../../api/client")>();
	return {
		...actual,
		api: {
			...actual.api,
			providers: {
				...actual.api.providers,
				discover: (id: string) => discover(id),
			},
		},
	};
});

const diff: DiscoveryDiff = {
	added: [{ model_id: "m", reason: "new_model" }],
};

describe("useDiscoveryRetest", () => {
	beforeEach(() => {
		discover.mockReset();
	});

	it("re-runs discovery and patches the entry on success", async () => {
		discover.mockResolvedValue({ discovered: 1, diff });
		const patchEntry = vi.fn();
		const { result } = renderHook(() => useDiscoveryRetest(patchEntry), {
			wrapper: AllProviders,
		});
		const entry: DiscoverySummaryEntry = {
			providerName: "Prov",
			providerId: "p1",
			diff,
		};

		act(() => {
			result.current.onRetest(entry);
		});

		await waitFor(() => expect(patchEntry).toHaveBeenCalledWith("Prov", diff));
		expect(discover).toHaveBeenCalledWith("p1");
		// retestingKey clears once the mutation settles.
		await waitFor(() => expect(result.current.retestingKey).toBeUndefined());
	});

	it("marks the entryKey as retesting while the request is in flight", async () => {
		let resolveDiscover: (value: unknown) => void = () => {};
		discover.mockReturnValue(
			new Promise((resolve) => {
				resolveDiscover = resolve;
			}),
		);
		const { result } = renderHook(() => useDiscoveryRetest(vi.fn()), {
			wrapper: AllProviders,
		});
		const entry: DiscoverySummaryEntry = {
			providerName: "Prov",
			entryKey: "k1",
			providerId: "p1",
		};

		act(() => {
			result.current.onRetest(entry);
		});

		await waitFor(() => expect(result.current.retestingKey).toBe("k1"));

		act(() => {
			resolveDiscover({ discovered: 0, diff: {} });
		});
		await waitFor(() => expect(result.current.retestingKey).toBeUndefined());
	});

	it("never hits the API for an entry without a providerId", async () => {
		const patchEntry = vi.fn();
		const { result } = renderHook(() => useDiscoveryRetest(patchEntry), {
			wrapper: AllProviders,
		});

		act(() => {
			result.current.onRetest({ providerName: "NoId" });
		});

		await waitFor(() => expect(result.current.retestingKey).toBeUndefined());
		expect(discover).not.toHaveBeenCalled();
		expect(patchEntry).not.toHaveBeenCalled();
		// The rejection still runs through onSettled, so the global flag must
		// clear too — otherwise every Retest button stays disabled for the rest
		// of the session after one bad entry.
		await waitFor(() => expect(result.current.isAnyRetesting).toBe(false));
	});

	it("does not re-read the badge for an entry that never left the browser", async () => {
		// onSettled runs for a local rejection too, so the badge refresh has to be
		// guarded: nothing can have changed server-side when no request was sent.
		// Observed through a mounted ["discovery-status"] query, because an
		// invalidation with nothing listening is not observable at all.
		let statusReads = 0;
		server.use(
			http.get("/api/discovery/status", () => {
				statusReads++;
				return HttpResponse.json({
					claims: [],
					group_claims: [],
					informational: [],
					claim_count: 0,
					informational_unseen: 0,
				});
			}),
		);

		function BadgeProbe() {
			useQuery({
				queryKey: ["discovery-status"],
				queryFn: () => api.discovery.status(false),
			});
			return null;
		}

		const { result } = renderHook(() => useDiscoveryRetest(vi.fn()), {
			wrapper: ({ children }) => (
				<AllProviders>
					<BadgeProbe />
					{children}
				</AllProviders>
			),
		});
		await waitFor(() => expect(statusReads).toBe(1));

		act(() => {
			result.current.onRetest({ providerName: "NoId" });
		});
		await waitFor(() => expect(result.current.isAnyRetesting).toBe(false));

		// A real retest re-reads; this one must not, so the count stays put.
		expect(statusReads).toBe(1);
	});

	it("reports a global in-flight flag and ignores a second onRetest while one is running", async () => {
		// Three rapid clicks currently stomp each other: each onMutate overwrites the
		// shared key, so the first row stops spinning while its request is still out.
		// The modal disables all buttons off isAnyRetesting, so it must stay true for
		// the whole in-flight window and clear only when the request settles. The
		// real regression is a SECOND request starting at all, not just the flag
		// reading true, so this asserts discover() is only ever called once.
		let resolveDiscover: (value: unknown) => void = () => {};
		discover.mockReturnValue(
			new Promise((resolve) => {
				resolveDiscover = resolve;
			}),
		);
		const { result } = renderHook(() => useDiscoveryRetest(vi.fn()), {
			wrapper: AllProviders,
		});

		expect(result.current.isAnyRetesting).toBe(false);

		act(() => {
			result.current.onRetest({
				providerName: "NanoGPT",
				providerId: "p1",
				entryKey: "k1",
			});
		});
		await waitFor(() => expect(result.current.isAnyRetesting).toBe(true));
		expect(result.current.retestingKey).toBe("k1");

		// A second click, e.g. on a different row, while the first is still in
		// flight must not start another mutation.
		act(() => {
			result.current.onRetest({
				providerName: "Other",
				providerId: "p2",
				entryKey: "k2",
			});
		});
		expect(discover).toHaveBeenCalledTimes(1);
		expect(discover).toHaveBeenCalledWith("p1");
		expect(result.current.retestingKey).toBe("k1");

		act(() => {
			resolveDiscover({ discovered: 0, diff: {} });
		});
		await waitFor(() => expect(result.current.isAnyRetesting).toBe(false));
		expect(discover).toHaveBeenCalledTimes(1);
	});
	it("toasts per provider by default", async () => {
		discover.mockResolvedValue({ discovered: 1, diff });
		const { result } = renderHook(() => useDiscoveryRetest(vi.fn()), {
			wrapper: AllProviders,
		});

		await act(async () => {
			await result.current.retestAsync({
				providerName: "Prov",
				providerId: "p1",
			});
		});

		expect(screen.getAllByTestId("toast")).toHaveLength(1);
	});

	it("suppresses its own toast when the caller takes over the messaging", async () => {
		// A walk over eight providers would otherwise stack eight toasts. Nothing
		// collapses them: ToastContext dedupes by message and every message names a
		// different provider.
		discover.mockResolvedValue({ discovered: 1, diff });
		const { result } = renderHook(() => useDiscoveryRetest(vi.fn()), {
			wrapper: AllProviders,
		});

		await act(async () => {
			await result.current.retestAsync(
				{ providerName: "Prov", providerId: "p1" },
				true,
			);
		});

		expect(screen.queryAllByTestId("toast")).toHaveLength(0);
	});

	it("suppresses the failure toast too when silenced", async () => {
		discover.mockRejectedValue(new Error("upstream down"));
		const { result } = renderHook(() => useDiscoveryRetest(vi.fn()), {
			wrapper: AllProviders,
		});

		await act(async () => {
			await result.current
				.retestAsync({ providerName: "Prov", providerId: "p1" }, true)
				.catch(() => {});
		});

		expect(screen.queryAllByTestId("toast")).toHaveLength(0);
	});
});
