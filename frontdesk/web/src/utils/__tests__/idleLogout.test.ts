import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { startIdleLogout } from "../idleLogout";

// Pure-timer behaviour, driven by fake timers in the jsdom window. A unique
// storage key per test keeps the cross-tab coordination isolated.
describe("startIdleLogout", () => {
	let key = "";

	beforeEach(() => {
		vi.useFakeTimers();
		localStorage.clear();
		key = `idle-test-${Math.random().toString(36).slice(2)}`;
	});

	afterEach(() => {
		vi.runOnlyPendingTimers();
		vi.useRealTimers();
	});

	it("fires onTimeout once after the inactivity window", () => {
		const onTimeout = vi.fn();
		const stop = startIdleLogout({
			timeoutMs: 5000,
			onTimeout,
			storageKey: key,
		});

		vi.advanceTimersByTime(4999);
		expect(onTimeout).not.toHaveBeenCalled();

		vi.advanceTimersByTime(1);
		expect(onTimeout).toHaveBeenCalledTimes(1);

		// Never fires twice even if time marches on.
		vi.advanceTimersByTime(60_000);
		expect(onTimeout).toHaveBeenCalledTimes(1);
		stop();
	});

	it("does nothing when the timeout is disabled (<= 0)", () => {
		const onTimeout = vi.fn();
		const stop = startIdleLogout({ timeoutMs: 0, onTimeout, storageKey: key });
		vi.advanceTimersByTime(10 * 60_000);
		expect(onTimeout).not.toHaveBeenCalled();
		stop();
	});

	it("resets the window on user activity", () => {
		const onTimeout = vi.fn();
		const stop = startIdleLogout({
			timeoutMs: 5000,
			onTimeout,
			storageKey: key,
		});

		// Activity after the throttle window pushes the deadline out.
		vi.advanceTimersByTime(2000);
		window.dispatchEvent(new KeyboardEvent("keydown", { key: "a" }));

		vi.advanceTimersByTime(3000); // original deadline (t=5000) passes
		expect(onTimeout).not.toHaveBeenCalled();

		vi.advanceTimersByTime(2000); // new deadline t=7000
		expect(onTimeout).toHaveBeenCalledTimes(1);
		stop();
	});

	it("extends the deadline when a peer tab reports activity", () => {
		const onTimeout = vi.fn();
		const stop = startIdleLogout({
			timeoutMs: 5000,
			onTimeout,
			storageKey: key,
		});

		const peerTs = Date.now() + 2000;
		window.dispatchEvent(
			new StorageEvent("storage", { key, newValue: String(peerTs) }),
		);

		vi.advanceTimersByTime(5000); // original deadline passes
		expect(onTimeout).not.toHaveBeenCalled();

		vi.advanceTimersByTime(2000); // peer-derived deadline (t=7000)
		expect(onTimeout).toHaveBeenCalledTimes(1);
		stop();
	});

	it("ignores activity within the throttle window", () => {
		const onTimeout = vi.fn();
		const stop = startIdleLogout({
			timeoutMs: 5000,
			onTimeout,
			storageKey: key,
		});

		// Activity 500ms in (< 1000ms throttle) must NOT push the deadline out.
		vi.advanceTimersByTime(500);
		window.dispatchEvent(new KeyboardEvent("keydown", { key: "a" }));

		vi.advanceTimersByTime(4500); // original deadline t=5000
		expect(onTimeout).toHaveBeenCalledTimes(1);
		stop();
	});

	it("fires immediately on visibilitychange when the deadline already passed", () => {
		const onTimeout = vi.fn();
		const stop = startIdleLogout({
			timeoutMs: 5000,
			onTimeout,
			storageKey: key,
		});

		// Simulate a tab that was suspended past its deadline: the shared
		// timestamp is older than now - timeout, so returning to the tab must log
		// out at once rather than waiting for the (possibly throttled) timer.
		localStorage.setItem(key, String(Date.now() - 10_000));
		document.dispatchEvent(new Event("visibilitychange"));

		expect(onTimeout).toHaveBeenCalledTimes(1);
		stop();
	});

	it("seeds the deadline from an existing peer timestamp on startup", () => {
		// A peer tab recorded activity 2s ago before this timer started.
		localStorage.setItem(key, String(Date.now() - 2000));

		const onTimeout = vi.fn();
		const stop = startIdleLogout({
			timeoutMs: 5000,
			onTimeout,
			storageKey: key,
		});

		// Deadline is peer + 5000 = now + 3000, not a fresh now + 5000.
		vi.advanceTimersByTime(2999);
		expect(onTimeout).not.toHaveBeenCalled();
		vi.advanceTimersByTime(1);
		expect(onTimeout).toHaveBeenCalledTimes(1);
		stop();
	});

	it("ignores a stale stored deadline on startup (no instant re-logout)", () => {
		// A previous session left an expired timestamp behind: logout clears only
		// the auth token, not this key. Startup must begin a fresh window rather
		// than arm a zero-delay timer that bounces the user back to login.
		localStorage.setItem(key, String(Date.now() - 10 * 60_000));

		const onTimeout = vi.fn();
		const stop = startIdleLogout({
			timeoutMs: 5000,
			onTimeout,
			storageKey: key,
		});

		// Did not fire immediately, and fires on the fresh window, not the stale one.
		expect(onTimeout).not.toHaveBeenCalled();
		vi.advanceTimersByTime(5000);
		expect(onTimeout).toHaveBeenCalledTimes(1);
		stop();
	});

	it("stops firing after cleanup", () => {
		const onTimeout = vi.fn();
		const stop = startIdleLogout({
			timeoutMs: 5000,
			onTimeout,
			storageKey: key,
		});
		stop();
		vi.advanceTimersByTime(60_000);
		expect(onTimeout).not.toHaveBeenCalled();
	});
});
