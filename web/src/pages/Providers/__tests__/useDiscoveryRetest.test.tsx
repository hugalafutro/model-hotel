import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DiscoveryDiff } from "../../../api/types";
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
});
