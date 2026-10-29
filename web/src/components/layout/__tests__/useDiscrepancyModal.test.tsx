import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import type { MergedProvider } from "../../../hooks/useDiscrepancies";
import i18n from "../../../i18n";
import { createQueryWrapper } from "../../../test/utils";
import { useDiscrepancyModal } from "../useDiscrepancyModal";

// The retest / dismiss orchestration inside useDiscrepancyModal, with its
// collaborators stubbed so each guard and each summary toast is driven directly.

const toast = vi.fn();
const retestAsync = vi.fn();
const refresh = vi.fn(async () => {});
const dismissClaim = vi.fn();
let snapshot: MergedProvider[] = [];

vi.mock("../../../context/ToastContext", () => ({
	useToast: () => ({ toast }),
}));
vi.mock("../../../context/EventContext", () => ({
	useServerEvent: () => {},
}));
vi.mock("../../../hooks/useRefreshDiscoveryBadge", () => ({
	useRefreshDiscoveryBadge: () => () => {},
}));
vi.mock("../../../pages/Providers/useDiscoveryRetest", () => ({
	useDiscoveryRetest: () => ({
		retestAsync,
		retestingKey: undefined,
		isAnyRetesting: false,
	}),
}));
vi.mock("../../../hooks/useDiscrepancies", async (importOriginal) => ({
	...(await importOriginal<typeof import("../../../hooks/useDiscrepancies")>()),
	useDiscrepancies: () => ({
		snapshot,
		groupClaims: [],
		informational: [],
		refresh,
		loading: false,
		isError: false,
		error: null,
		refreshError: null,
		dismissClaim,
	}),
}));

// A provider with one pending "gone" claim: something a retest can disprove.
function pendingProvider(id: string): MergedProvider {
	return {
		provider_id: id,
		provider_name: `name-${id}`,
		gone: [{ model_id: "m", status: "pending" } as MergedProvider["gone"][0]],
		stale: [],
		suspect: [],
		retired: [],
		pinned: [],
	};
}

function deferred() {
	let resolve!: () => void;
	let reject!: (e: unknown) => void;
	const promise = new Promise<void>((res, rej) => {
		resolve = res;
		reject = rej;
	});
	return { promise, resolve, reject };
}

function setup() {
	return renderHook(() => useDiscrepancyModal(), {
		wrapper: createQueryWrapper(),
	});
}

beforeEach(() => {
	snapshot = [];
	vi.spyOn(api.discovery, "status").mockResolvedValue({
		claim_count: 0,
		informational_unseen: 0,
		claims: [],
	} as unknown as Awaited<ReturnType<typeof api.discovery.status>>);
});

afterEach(() => {
	vi.restoreAllMocks();
	toast.mockReset();
	retestAsync.mockReset();
	refresh.mockClear();
	dismissClaim.mockReset();
});

describe("useDiscrepancyModal retests", () => {
	it("refuses a second retest while one is in flight", async () => {
		const first = deferred();
		retestAsync.mockReturnValueOnce(first.promise);
		const { result } = setup();
		act(() => result.current.modalProps.onRetest("p1", "name-p1"));
		act(() => result.current.modalProps.onRetest("p2", "name-p2"));
		expect(retestAsync).toHaveBeenCalledTimes(1);
		await act(async () => first.resolve());
	});

	it("records a failed retest per provider and clears it once a retest succeeds", async () => {
		retestAsync.mockRejectedValueOnce(new Error("upstream down"));
		const { result } = setup();
		await act(async () => result.current.modalProps.onRetest("p1", "name-p1"));
		await waitFor(() =>
			expect(result.current.modalProps.errors).toEqual({
				p1: "upstream down",
			}),
		);
		// Another provider's success leaves p1's error alone.
		retestAsync.mockResolvedValueOnce(undefined);
		await act(async () => result.current.modalProps.onRetest("p2", "name-p2"));
		await waitFor(() => expect(refresh).toHaveBeenCalledTimes(1));
		expect(result.current.modalProps.errors).toEqual({ p1: "upstream down" });
		// p1's own success clears it.
		retestAsync.mockResolvedValueOnce(undefined);
		await act(async () => result.current.modalProps.onRetest("p1", "name-p1"));
		await waitFor(() => expect(result.current.modalProps.errors).toEqual({}));
	});

	it("retest-all is a no-op with nothing a retest could disprove", async () => {
		snapshot = [{ ...pendingProvider("p1"), gone: [] }];
		const { result } = setup();
		await act(async () => result.current.modalProps.onRetestAll());
		expect(retestAsync).not.toHaveBeenCalled();
		expect(toast).not.toHaveBeenCalled();
	});

	it("retest-all is a no-op while a single retest is running", async () => {
		snapshot = [pendingProvider("p1"), pendingProvider("p2")];
		const single = deferred();
		retestAsync.mockReturnValueOnce(single.promise);
		const { result } = setup();
		act(() => result.current.modalProps.onRetest("p9", "name-p9"));
		await act(async () => result.current.modalProps.onRetestAll());
		expect(retestAsync).toHaveBeenCalledTimes(1);
		expect(result.current.modalProps.retestAllProgress).toBeUndefined();
		await act(async () => single.resolve());
	});

	it("stops the walk before the next provider when cancelled", async () => {
		snapshot = [pendingProvider("p1"), pendingProvider("p2")];
		const first = deferred();
		retestAsync.mockReturnValueOnce(first.promise);
		const { result } = setup();
		act(() => result.current.modalProps.onRetestAll());
		await waitFor(() => expect(retestAsync).toHaveBeenCalledTimes(1));
		act(() => result.current.modalProps.onCancelRetestAll());
		await act(async () => first.resolve());
		await waitFor(() =>
			expect(toast).toHaveBeenCalledWith(
				i18n.t("providers.discrepancies.retestAllCancelled", { count: 1 }),
				"info",
			),
		);
		// p2 was never started.
		expect(retestAsync).toHaveBeenCalledTimes(1);
	});

	it("reports how many providers failed in one summary toast", async () => {
		snapshot = [pendingProvider("p1"), pendingProvider("p2")];
		retestAsync.mockRejectedValue(new Error("nope"));
		const { result } = setup();
		await act(async () => result.current.modalProps.onRetestAll());
		await waitFor(() =>
			expect(toast).toHaveBeenCalledWith(
				i18n.t("providers.discrepancies.retestAllFailed", { count: 2 }),
				"error",
			),
		);
		expect(toast).toHaveBeenCalledTimes(1);
	});
});

describe("useDiscrepancyModal dismiss guards", () => {
	it("dismissing an empty model list sends nothing", async () => {
		const dismiss = vi.spyOn(api.discovery, "dismiss");
		const { result } = setup();
		await act(async () => result.current.modalProps.onDismissAll("p1", []));
		await act(async () => result.current.modalProps.onDismissEverything([]));
		expect(dismiss).not.toHaveBeenCalled();
		expect(toast).not.toHaveBeenCalled();
	});
});
