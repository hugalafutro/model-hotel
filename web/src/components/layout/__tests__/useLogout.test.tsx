import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../../../api/client";
import { useLogout } from "../useLogout";

vi.mock("../../../api/client", async (importOriginal) => {
	const mod = await importOriginal<typeof import("../../../api/client")>();
	return {
		...mod,
		api: { ...mod.api, auth: { ...mod.api.auth, logout: vi.fn() } },
	};
});
vi.mock("../../../hooks/useIdleLogout", () => ({ useIdleLogout: vi.fn() }));

// The sidebar button is the common way a session ends; it must run the same
// teardown as the 401 and password-change paths, mirror included.
describe("useLogout", () => {
	// The whole descriptor is kept, not just the Location object: restoring a
	// value-only descriptor would leave window.location non-writable.
	const original = Object.getOwnPropertyDescriptor(window, "location");
	afterEach(() => {
		if (original) Object.defineProperty(window, "location", original);
		localStorage.clear();
	});

	it("drops the per-session localStorage mirror and reloads", async () => {
		vi.mocked(api.auth.logout).mockResolvedValue(undefined as never);
		const reload = vi.fn();
		Object.defineProperty(window, "location", {
			value: { ...window.location, reload },
			configurable: true,
		});
		localStorage.setItem("model-hotel:ollama-cloud-account", "{}");
		localStorage.setItem("chatMessages", "[]");
		const queryClient = new QueryClient();
		const wrapper = ({ children }: { children: ReactNode }) => (
			<QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
		);
		const { result } = renderHook(() => useLogout(), { wrapper });

		await act(async () => {
			await result.current();
		});

		expect(api.auth.logout).toHaveBeenCalled();
		expect(localStorage.getItem("model-hotel:ollama-cloud-account")).toBeNull();
		expect(localStorage.getItem("chatMessages")).toBe("[]");
		expect(reload).toHaveBeenCalled();
	});
});
