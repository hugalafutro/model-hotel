import "@testing-library/jest-dom";
import { afterAll, afterEach, beforeAll, vi } from "vitest";
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
		this.readyState = 0; // CONNECTING
		// Simulate connection
		setTimeout(() => {
			this.readyState = 1; // OPEN
			this.onopen?.();
		}, 0);
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

// Mock Element.scrollTo (jsdom doesn't implement it)
if (typeof HTMLElement !== "undefined" && !HTMLElement.prototype.scrollTo) {
	HTMLElement.prototype.scrollTo = function () {};
}

// Mock navigator.clipboard (jsdom doesn't implement it)
const clipboardWriteText = vi.fn().mockResolvedValue(undefined);
vi.stubGlobal(
	"navigator",
	Object.assign(globalThis.navigator || {}, {
		clipboard: { writeText: clipboardWriteText },
	}),
);

beforeAll(() => {
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
