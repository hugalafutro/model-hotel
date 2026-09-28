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
	it("re-reads the quota queries after the sweep, whether it succeeded or not", async () => {
		for (const outcome of ["ok", "fail"] as const) {
			vi.mocked(api.providers.refreshQuotas).mockReset();
			if (outcome === "ok") {
				vi.mocked(api.providers.refreshQuotas).mockResolvedValue({
					refreshed: 1,
					failed: 0,
					skipped: 0,
					results: [],
				});
			} else {
				vi.mocked(api.providers.refreshQuotas).mockRejectedValue(
					new Error("boom"),
				);
			}
			const queryClient = new QueryClient();
			const invalidate = vi.spyOn(queryClient, "invalidateQueries");
			const { result } = renderHook(() => useQuotaRefresh(), {
				wrapper: wrap(queryClient),
			});
			act(() => result.current.refreshQuotas());
			await waitFor(() =>
				expect(
					invalidate.mock.calls.some(
						([opts]) =>
							Array.isArray(opts?.queryKey) &&
							opts.queryKey[0] === "nanogpt-usage",
					),
				).toBe(true),
			);
		}
	});
});
