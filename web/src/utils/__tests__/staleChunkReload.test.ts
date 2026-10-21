import { installStaleChunkReload } from "@web-shared/stale-chunk-reload";
import { afterEach, describe, expect, it, vi } from "vitest";

function fakeWindow(storage: Pick<Storage, "getItem" | "setItem">) {
	const target = new EventTarget();
	const reload = vi.fn();
	const win = {
		addEventListener: target.addEventListener.bind(target),
		sessionStorage: storage,
		location: { reload },
	} as unknown as Window;
	const fire = () => {
		const event = new Event("vite:preloadError", { cancelable: true });
		target.dispatchEvent(event);
		return event;
	};
	return { win, reload, fire };
}

function memoryStorage() {
	const map = new Map<string, string>();
	return {
		getItem: (k: string) => map.get(k) ?? null,
		setItem: (k: string, v: string) => void map.set(k, v),
	};
}

describe("installStaleChunkReload", () => {
	afterEach(() => vi.useRealTimers());

	it("reloads once on a failed chunk and suppresses Vite's throw", () => {
		const { win, reload, fire } = fakeWindow(memoryStorage());
		installStaleChunkReload(win);
		expect(fire().defaultPrevented).toBe(true);
		expect(reload).toHaveBeenCalledOnce();
	});

	it("does not loop: a second failure inside 10 s is left to the error boundary", () => {
		vi.useFakeTimers();
		const { win, reload, fire } = fakeWindow(memoryStorage());
		installStaleChunkReload(win);
		fire();
		vi.advanceTimersByTime(5_000);
		expect(fire().defaultPrevented).toBe(false);
		expect(reload).toHaveBeenCalledOnce();
		vi.advanceTimersByTime(6_000);
		fire();
		expect(reload).toHaveBeenCalledTimes(2);
	});

	it("does not reload when the loop guard cannot be saved", () => {
		const blockedWrite = {
			...memoryStorage(),
			setItem: () => {
				throw new Error("denied");
			},
		};
		const { win, reload, fire } = fakeWindow(blockedWrite);
		installStaleChunkReload(win);
		expect(fire().defaultPrevented).toBe(false);
		expect(reload).not.toHaveBeenCalled();
	});
});
