import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const getSettings = vi.fn();
const startIdleLogout = vi.fn();
vi.mock("../../api/client", () => ({
	api: { getSettings: (...args: unknown[]) => getSettings(...args) },
}));
vi.mock("@web-shared/idle-logout", () => ({
	startIdleLogout: (...args: unknown[]) => startIdleLogout(...args),
}));

import { SETTINGS_SAVED_EVENT, useIdleLogout } from "../useIdleLogout";

describe("useIdleLogout", () => {
	beforeEach(() => {
		getSettings.mockReset();
		startIdleLogout.mockReset().mockReturnValue(() => {});
	});
	afterEach(() => vi.restoreAllMocks());

	// A saved change to the idle window takes effect now, not at the next
	// login: the settings form announces the save and the hook re-reads.
	it("re-reads the window when the settings form announces a save", async () => {
		getSettings.mockResolvedValueOnce({ session_idle_timeout_minutes: 60 });
		renderHook(() => useIdleLogout(true, () => {}));
		await act(async () => {});
		expect(startIdleLogout).toHaveBeenLastCalledWith(
			expect.objectContaining({ timeoutMs: 60 * 60_000 }),
		);

		getSettings.mockResolvedValueOnce({ session_idle_timeout_minutes: 15 });
		await act(async () => {
			window.dispatchEvent(new Event(SETTINGS_SAVED_EVENT));
		});
		expect(getSettings).toHaveBeenCalledTimes(2);
		expect(startIdleLogout).toHaveBeenLastCalledWith(
			expect.objectContaining({ timeoutMs: 15 * 60_000 }),
		);
	});

	// The initial read answering after the post-save read must not put the
	// old window back.
	it("ignores a stale settings response that lands after a newer one", async () => {
		let resolveFirst: (s: { session_idle_timeout_minutes: number }) => void =
			() => {};
		getSettings.mockImplementationOnce(
			() =>
				new Promise((resolve) => {
					resolveFirst = resolve;
				}),
		);
		renderHook(() => useIdleLogout(true, () => {}));
		getSettings.mockResolvedValueOnce({ session_idle_timeout_minutes: 30 });
		await act(async () => {
			window.dispatchEvent(new Event(SETTINGS_SAVED_EVENT));
		});
		expect(startIdleLogout).toHaveBeenLastCalledWith(
			expect.objectContaining({ timeoutMs: 30 * 60_000 }),
		);
		await act(async () => {
			resolveFirst({ session_idle_timeout_minutes: 15 });
		});
		expect(startIdleLogout).toHaveBeenLastCalledWith(
			expect.objectContaining({ timeoutMs: 30 * 60_000 }),
		);
	});

	it("wires nothing while logged out", () => {
		renderHook(() => useIdleLogout(false, () => {}));
		expect(getSettings).not.toHaveBeenCalled();
		expect(startIdleLogout).not.toHaveBeenCalled();
	});
});
