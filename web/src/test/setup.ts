import { Blob as NodeBlob, File as NodeFile } from "node:buffer";
import "@testing-library/jest-dom";
import { configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, vi } from "vitest";
import "../i18n";

// The full-app integration tests render the shell, navigate, and then wait on a
// page's async-loaded content. testing-library's default 1s asyncUtilTimeout is
// too tight for that chain on a loaded CI shard (several concurrent MSW queries,
// including the app-wide /api/auth/me identity fetch), producing intermittent
// "unable to find text" flakes. Give waitFor/findBy more patience; the per-test
// budget (vitest testTimeout) stays the real ceiling and assertions are
// unchanged, so this only tolerates scheduling jitter, it does not hide failures.
configure({ asyncUtilTimeout: 5000 });

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
		_event: string,
		_listener: (event: MessageEvent) => void,
	): void {
		// No-op for basic testing
	}

	removeEventListener(
		_event: string,
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
// jsdom has no Web Animations API. A no-op Animation with cancel() and a
// settled `finished` keeps .animate() callers from failing a test on a
// missing API instead of on the behaviour under test.
if (typeof Element !== "undefined" && !Element.prototype.animate) {
	Element.prototype.animate = (() => ({
		cancel: () => {},
		finished: Promise.resolve(),
	})) as unknown as Element["animate"];
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
		observe(_target: Element, _options?: ResizeObserverOptions) {}
		unobserve() {}
		disconnect() {}
	} as unknown as typeof globalThis.ResizeObserver;
}

// Mock IntersectionObserver (jsdom doesn't implement it). Inert: it never
// reports an intersection, because jsdom has no layout to derive one from.
// Tests that care about the observed behaviour (the discrepancy modal's
// return-to-top control) stub this again locally and drive the callback
// themselves.
if (typeof globalThis.IntersectionObserver === "undefined") {
	globalThis.IntersectionObserver = class IntersectionObserver {
		readonly root = null;
		readonly rootMargin = "";
		readonly thresholds: number[] = [];
		observe(_target: Element) {}
		unobserve(_target: Element) {}
		disconnect() {}
		takeRecords(): IntersectionObserverEntry[] {
			return [];
		}
	} as unknown as typeof globalThis.IntersectionObserver;
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

// A browser sends the document's cookies on every same-origin request made
// with credentials other than "omit"; Node's fetch sends none. msw 2 papered
// over that by appending document.cookie to the intercepted request, msw 3
// dropped cookie handling in Node, so the browser rule lives here instead. The
// mock handlers gate on the mh_csrf cookie and the api client relies on it.
// Installed after server.listen(): msw wraps globalThis.fetch itself and
// serialises the body before handing it down, so this has to sit above it.
function installBrowserFetchRules() {
	const nodeFetch = globalThis.fetch;
	globalThis.fetch = (input, init) => {
		const target =
			typeof input === "string"
				? input
				: input instanceof URL
					? input.href
					: input.url;
		const credentials =
			init?.credentials ??
			(input instanceof Request ? input.credentials : "same-origin");
		const sameOrigin =
			new URL(target, window.location.href).origin === window.location.origin;
		const headers = new Headers(
			init?.headers ?? (input instanceof Request ? input.headers : undefined),
		);
		const sendsCookies =
			credentials === "include" ||
			(credentials === "same-origin" && sameOrigin);
		if (sendsCookies && document.cookie && !headers.has("Cookie")) {
			headers.set("Cookie", document.cookie);
		}
		// A jsdom FormData or Blob body has to be rebuilt from Node's classes before
		// undici serialises it: it sizes the parts from the jsdom Blob but streams
		// no bytes for them, and the request dies with "Request body length does
		// not match content-length header". msw 2 never serialised the body, msw 3
		// puts it on the wire. vitest's own jsdom shim converts only inside the
		// Request constructor, and drops the file bytes too, so rebuild here.
		if (init?.body instanceof FormData || init?.body instanceof Blob) {
			return toNodeBody(init.body).then((body) =>
				nodeFetch(input, { ...init, headers, body }),
			);
		}
		return nodeFetch(input, { ...init, headers });
	};
}

async function toNodeBody(body: FormData | Blob): Promise<BodyInit> {
	if (body instanceof Blob) {
		// Node's Blob is what undici serialises; the DOM lib type is a formality here.
		return new NodeBlob([await body.arrayBuffer()], {
			type: body.type,
		}) as unknown as Blob;
	}
	// Node's FormData class is not reachable from here (jsdom owns the global),
	// but parsing a one-field multipart body through Request yields one.
	const seed = new FormData();
	seed.append("seed", "");
	const form = await new Request(window.location.href, {
		method: "POST",
		body: seed,
	}).formData();
	form.delete("seed");
	for (const [name, value] of body.entries()) {
		if (value instanceof Blob) {
			const file = value as File;
			form.append(
				name,
				new NodeFile([await value.arrayBuffer()], file.name ?? "blob", {
					type: value.type,
				}) as unknown as Blob,
			);
		} else {
			form.append(name, value);
		}
	}
	return form;
}

beforeAll(() => {
	_suppressJsdomNotImplemented();
	server.listen({ onUnhandledFrame: "warn" });
	installBrowserFetchRules();
	// Cookie-session auth: seed the readable CSRF cookie so isAuthenticated()
	// reports logged-in and same-origin requests carry the session cookie to the
	// MSW handlers (which gate on mh_csrf). httpOnly cookies can't be set from JS,
	// so the readable half stands in for the session in tests.
	document.cookie = "mh_csrf=test-csrf; path=/";
});

afterEach(() => {
	server.resetHandlers();
	resetStore();
	// Re-seed the session cookie between tests. A 401 response clears it (the api
	// client's clearAuth on 401), and tests that exercise logged-out flows clear
	// it in their own beforeEach; re-seeding here keeps the default state
	// "logged in" and makes the suite order-independent.
	document.cookie = "mh_csrf=test-csrf; path=/";
});

afterAll(() => {
	server.close();
});
