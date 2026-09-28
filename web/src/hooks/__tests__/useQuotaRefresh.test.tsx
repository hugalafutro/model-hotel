import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { api } from "../../api/client";
import { ToastProvider } from "../../context/ToastContext";
import { useQuotaRefresh } from "../useQuotaRefresh";

vi.mock("../../api/client", () => ({
	api: { providers: { refreshQuotas: vi.fn() } },
}));

function wrap(queryClient: QueryClient) {
	return ({ children }: { children: ReactNode }) => (
		<QueryClientProvider client={queryClient}>
			<ToastProvider>{children}</ToastProvider>
		</QueryClientProvider>
	);
}

// The badges on the provider cards read the quota queries, so a sweep that
// only invalidated the provider list left them on the pre-sweep snapshot
// under a "refreshed" toast.
describe("useQuotaRefresh", () => {
	it("re-reads the quota queries once the sweep settles, whether it succeeded or not", async () => {
		for (const outcome of ["ok", "fail"] as const) {
			vi.mocked(api.providers.refreshQuotas).mockReset();
			let settle: () => void = () => {};
			const pending = new Promise<void>((resolve) => {
				settle = resolve;
			});
			vi.mocked(api.providers.refreshQuotas).mockImplementation(async () => {
				await pending;
				if (outcome === "fail") throw new Error("boom");
				return { refreshed: 1, failed: 0, skipped: 0, results: [] };
			});
			const queryClient = new QueryClient();
			const invalidate = vi.spyOn(queryClient, "invalidateQueries");
			const quotaInvalidated = () =>
				invalidate.mock.calls.some(
					([opts]) =>
						Array.isArray(opts?.queryKey) &&
						opts.queryKey[0] === "nanogpt-usage",
				);
			const { result } = renderHook(() => useQuotaRefresh(), {
				wrapper: wrap(queryClient),
			});
			act(() => result.current.refreshQuotas());
			// Not before the sweep settles: a re-read mid-sweep would land the
			// pre-sweep snapshot and the fresh one would never be read.
			await act(async () => {});
			expect(quotaInvalidated()).toBe(false);
			settle();
			await waitFor(() => expect(quotaInvalidated()).toBe(true));
		}
	});
});
