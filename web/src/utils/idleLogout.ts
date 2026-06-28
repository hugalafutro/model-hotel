// Dependency-free inactivity auto-logout timer.
//
// SHARED HELPER: an identical copy lives in both frontends
// (web/src/utils/idleLogout.ts and frontdesk/web/src/utils/idleLogout.ts).
// The two apps are separate Vite roots with their own `@` aliases, so the
// module cannot be imported across them — keep the two copies in sync. The
// helper is pure DOM (no React, no i18n, no app imports) precisely so the two
// copies stay trivially identical.
//
// Behaviour: arm a timer that fires `onTimeout` once after `timeoutMs` of no
// user activity. Activity is any of a set of DOM events (pointer/key/scroll/
// touch). Tabs coordinate through a shared localStorage timestamp + the
// `storage` event, so activity in one tab keeps every tab signed in and the
// deadline stays consistent across them. A `visibilitychange` re-check covers
// the case where the tab was backgrounded/suspended past the deadline (a long
// setTimeout is not guaranteed to fire on time while hidden).

export interface IdleLogoutOptions {
	/** Inactivity window in milliseconds. <= 0 disables the timer entirely. */
	timeoutMs: number;
	/** Invoked exactly once when the inactivity window elapses. */
	onTimeout: () => void;
	/** localStorage key used to coordinate the deadline across tabs. */
	storageKey?: string;
	/** DOM events that count as activity and reset the timer. */
	activityEvents?: string[];
	/** Test/SSR seam: window-like target. Defaults to the global `window`. */
	target?: Window;
}

const DEFAULT_EVENTS = [
	"mousemove",
	"mousedown",
	"keydown",
	"scroll",
	"touchstart",
	"click",
	"wheel",
];

const DEFAULT_STORAGE_KEY = "sessionLastActivityAt";

// Don't reset the timer / write to storage on every mousemove: collapse bursts
// of activity into at most one reset per second.
const THROTTLE_MS = 1000;

/**
 * Starts the idle-logout timer. Returns a cleanup function that removes every
 * listener and cancels the pending timer. Call it again with a new timeoutMs to
 * reconfigure (after running the previous cleanup).
 *
 * A timeoutMs <= 0 disables auto-logout: the function wires nothing and the
 * returned cleanup is a no-op.
 */
export function startIdleLogout(opts: IdleLogoutOptions): () => void {
	const resolved =
		opts.target ?? (typeof window !== "undefined" ? window : undefined);
	if (!resolved || opts.timeoutMs <= 0) {
		return () => {};
	}
	// Non-nullable alias: hoisted `function` declarations below don't inherit the
	// control-flow narrowing from the guard above, so capture it explicitly.
	const win: Window = resolved;

	const events = opts.activityEvents ?? DEFAULT_EVENTS;
	const storageKey = opts.storageKey ?? DEFAULT_STORAGE_KEY;
	const { timeoutMs } = opts;

	let timer: ReturnType<typeof setTimeout> | undefined;
	let lastReset = 0;
	let fired = false;

	const readStored = (): number | null => {
		try {
			const v = win.localStorage?.getItem(storageKey);
			if (!v) return null;
			const n = Number(v);
			return Number.isFinite(n) ? n : null;
		} catch {
			return null; // private mode / storage disabled
		}
	};

	const arm = (fromTs: number) => {
		if (timer) clearTimeout(timer);
		const remaining = fromTs + timeoutMs - Date.now();
		timer = setTimeout(fire, Math.max(0, remaining));
	};

	function fire() {
		if (fired) return;
		fired = true;
		cleanup();
		opts.onTimeout();
	}

	// broadcast=true writes the new timestamp so peer tabs re-arm too.
	const recordActivity = (broadcast: boolean) => {
		const ts = Date.now();
		lastReset = ts;
		if (broadcast) {
			try {
				win.localStorage?.setItem(storageKey, String(ts));
			} catch {
				// private mode: this tab still self-tracks via lastReset.
			}
		}
		arm(ts);
	};

	const onActivity = () => {
		if (Date.now() - lastReset < THROTTLE_MS) return;
		recordActivity(true);
	};

	const onStorage = (e: StorageEvent) => {
		if (e.key !== storageKey || !e.newValue) return;
		const ts = Number(e.newValue);
		if (!Number.isFinite(ts)) return;
		// A peer tab saw activity: re-arm from its timestamp without echoing it
		// back (which would loop the storage event between tabs).
		lastReset = ts;
		arm(ts);
	};

	const onVisibility = () => {
		if (win.document?.visibilityState !== "visible") return;
		// Coming back to a possibly-suspended tab: trust the shared deadline.
		const base = readStored() ?? lastReset;
		if (Date.now() >= base + timeoutMs) {
			fire();
			return;
		}
		arm(base);
	};

	function cleanup() {
		if (timer) clearTimeout(timer);
		for (const ev of events) win.removeEventListener(ev, onActivity);
		win.removeEventListener("storage", onStorage);
		win.document?.removeEventListener?.("visibilitychange", onVisibility);
	}

	for (const ev of events) {
		win.addEventListener(ev, onActivity, { passive: true });
	}
	win.addEventListener("storage", onStorage);
	win.document?.addEventListener?.("visibilitychange", onVisibility);

	// Seed from a peer tab's deadline if one is already set and still live (so
	// opening a second tab doesn't reset everyone's clock); otherwise establish
	// the baseline. A stale timestamp whose deadline already elapsed is ignored:
	// logout clears only the auth token, so the prior session's value lingers in
	// storage, and arming from it would compute a zero-delay timer that bounces
	// the user straight back to login right after they sign in. A fresh start
	// means the user is present now, so begin the window now and overwrite it.
	const seeded = readStored();
	if (
		seeded != null &&
		seeded <= Date.now() &&
		Date.now() < seeded + timeoutMs
	) {
		lastReset = seeded;
		arm(seeded);
	} else {
		recordActivity(true);
	}

	return cleanup;
}
