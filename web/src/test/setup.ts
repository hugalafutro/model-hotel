import "@testing-library/jest-dom";
import { afterAll, afterEach, beforeAll, vi } from "vitest";
import "../i18n";
import { setAdminToken } from "../api/client";
import { resetStore } from "./mocks/handlers";
import { server } from "./mocks/server";

if (typeof globalThis.localStorage === "undefined") {
	const store: Record<string, string> = {};
	globalThis.localStorage = {
		getItem: (k: string) => store[k] ?? null,
		setItem: (k: string, v: string) => {
			store[k] = v;
		},
		removeItem: (k: string) => {
			delete store[k];
		},
		clear: () => {
			Object.keys(store).forEach((k) => {
				delete store[k];
			});
		},
		key: (i: number) => Object.keys(store)[i] ?? null,
		get length() {
			return Object.keys(store).length;
		},
	} as Storage;
}

// Mock EventSource for SSE testing
class MockEventSource {
	static readonly CONNECTING = 0 as const;
	static readonly OPEN = 1 as const;
	static readonly CLOSED = 2 as const;
	url: string;
	readyState: number;
	onopen: (() => void) | null = null;
	onmessage: ((event: MessageEvent) => void) | null = null;
	onerror: (() => void) | null = null;

	constructor(url: string) {
		this.url = url;
		this.readyState = 0; // CONNECTING initially
		// Fire onopen after the current synchronous block so callers can set
		// handlers (e.g. es.onopen = ...) before the callback runs.
		// queueMicrotask runs within React 18's act() scope, unlike setTimeout.
		queueMicrotask(() => {
			if (this.readyState !== 2) {
				// Don't fire if close() was called synchronously
				this.readyState = 1; // OPEN
				this.onopen?.();
			}
		});
	}

	addEventListener(
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		_event: string,
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		_listener: (event: MessageEvent) => void,
	): void {
		// No-op for basic testing
	}

	removeEventListener(
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		_event: string,
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		_listener: (event: MessageEvent) => void,
	): void {
		// No-op for basic testing
	}

	close(): void {
		this.readyState = 2; // CLOSED
	}
}

vi.stubGlobal("EventSource", MockEventSource);

// Mock scrollTo on HTMLElement (jsdom doesn't implement it)
if (typeof HTMLElement !== "undefined" && !HTMLElement.prototype.scrollTo) {
	HTMLElement.prototype.scrollTo = () => {};
}
// Mock scrollIntoView on Element (jsdom doesn't implement it)
if (typeof Element !== "undefined" && !Element.prototype.scrollIntoView) {
	Element.prototype.scrollIntoView = () => {};
}
// Suppress jsdom "Not implemented" warnings (window.scrollTo, navigation, etc.)
// jsdom's VirtualConsole forwards jsdomError events to the Node.js console.error,
// not the jsdom window.console — so wrapping window.console.error won't intercept them.
// The VirtualConsole public API (testEnvironmentOptions.virtualConsole) also doesn't work
// because VirtualConsole objects are not serializable across Vitest's forked worker boundary.
// Patching _virtualConsole.emit is the only reliable interception point.
const _suppressJsdomNotImplemented = () => {
	const win = window as unknown as {
		_virtualConsole?: { emit: (type: string, error: Error) => void };
	};
	if (win._virtualConsole) {
		const originalEmit = win._virtualConsole.emit.bind(win._virtualConsole);
		win._virtualConsole.emit = (type: string, error: Error) => {
			if (
				type === "jsdomError" &&
				error.message?.startsWith("Not implemented:")
			) {
				return;
			}
			originalEmit(type, error);
		};
	}
};

// Mock navigator.clipboard (jsdom doesn't implement it)
const clipboardWriteText = vi.fn().mockResolvedValue(undefined);
vi.stubGlobal(
	"navigator",
	Object.assign(globalThis.navigator || {}, {
		clipboard: { writeText: clipboardWriteText },
	}),
);

// Mock ResizeObserver (jsdom doesn't implement it)
if (typeof globalThis.ResizeObserver === "undefined") {
	globalThis.ResizeObserver = class ResizeObserver {
		// eslint-disable-next-line @typescript-eslint/no-unused-vars
		observe(_target: Element, _options?: ResizeObserverOptions) {}
		unobserve() {}
		disconnect() {}
	} as unknown as typeof globalThis.ResizeObserver;
}

// Mock matchMedia (jsdom doesn't implement it). Defaults to "no match" so
// prefers-color-scheme: dark reads as light; tests that need a specific scheme
// can override window.matchMedia.
if (typeof window !== "undefined" && !window.matchMedia) {
	window.matchMedia = ((query: string) => ({
		matches: false,
		media: query,
		onchange: null,
		addEventListener: () => {},
		removeEventListener: () => {},
		addListener: () => {},
		removeListener: () => {},
		dispatchEvent: () => false,
	})) as unknown as typeof window.matchMedia;
}

// Mock Element.setPointerCapture (jsdom doesn't implement it)
if (typeof Element !== "undefined" && !Element.prototype.setPointerCapture) {
	Element.prototype.setPointerCapture = () => {};
}
if (
	typeof Element !== "undefined" &&
	!Element.prototype.releasePointerCapture
) {
	Element.prototype.releasePointerCapture = () => {};
}

beforeAll(() => {
	_suppressJsdomNotImplemented();
	server.listen({ onUnhandledRequest: "warn" });
	setAdminToken("test-admin-token");
});

afterEach(() => {
	server.resetHandlers();
	resetStore();
});

afterAll(() => {
	server.close();
});
