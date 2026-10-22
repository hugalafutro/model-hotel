import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
import { afterAll, afterEach, beforeAll, beforeEach } from "vitest";
import "../i18n"; // initialize i18next (English bundled) for every test
import { server } from "./server";

// testing-library's default 1s asyncUtilTimeout is too tight for a page that
// mounts several panels, each with its own MSW reads, on a CI runner under
// coverage: the Settings page's save-error test missed its alert by ~150ms on
// two runs in a row. The per-test budget (vitest testTimeout) stays the real
// ceiling and no assertion changes, so this tolerates scheduling jitter and
// hides no failure. The main dashboard's setup carries the same setting.
configure({ asyncUtilTimeout: 5000 });

// Request diagnostics for a CI-only flake class: a mocked read fails during a
// page's mount burst and the page shows its error state, with nothing in the log
// saying why (the API client turns every rejected fetch into a bare "network"
// error). Each test records its rejected fetches, error responses and handler
// exceptions, and prints them only if it fails, so the next occurrence names its
// cause instead of a timeout on missing text.
const requestTrouble: string[] = [];
const where = (request: Request) =>
	`${request.method} ${new URL(request.url).pathname}`;
const reason = (err: unknown) =>
	err instanceof Error
		? `${err.name}: ${err.message}${err.cause ? ` (cause: ${String(err.cause)})` : ""}`
		: String(err);
let realFetch: typeof fetch;

// MSW lifecycle, shared by every test file. Unhandled requests error so a test
// that hits an unmocked endpoint fails loudly instead of silently.
beforeAll(() => {
	server.listen({ onUnhandledFrame: "error" });
	server.events.on("request:unhandled", ({ request }) => {
		requestTrouble.push(`unhandled ${where(request)}`);
	});
	server.events.on("unhandledException", ({ request, error }) => {
		requestTrouble.push(`handler threw ${where(request)}: ${reason(error)}`);
	});
	server.events.on("response:mocked", ({ request, response }) => {
		if (!response.ok) {
			requestTrouble.push(`${response.status} ${where(request)}`);
		}
	});
	// Wrapped after listen(), so this sits outside MSW's own fetch patch and
	// sees every rejection a component's fetch receives.
	realFetch = globalThis.fetch;
	globalThis.fetch = async (input, init) => {
		try {
			return await realFetch(input, init);
		} catch (err) {
			// Resolved against the page: the app fetches relative paths, which a
			// bare Request constructor rejects outside a browser.
			const url = input instanceof Request ? input.url : String(input);
			const method =
				init?.method ?? (input instanceof Request ? input.method : "GET");
			const path = new URL(url, location.href).pathname;
			requestTrouble.push(
				`fetch rejected ${method.toUpperCase()} ${path}: ${reason(err)}`,
			);
			throw err;
		}
	};
});
beforeEach(({ onTestFailed }) => {
	requestTrouble.length = 0;
	onTestFailed(() => {
		if (requestTrouble.length > 0) {
			console.error(
				`[request diagnostics] ${requestTrouble.length} event(s) in the failed test:\n  ${requestTrouble.join("\n  ")}`,
			);
		}
	});
});
afterEach(() => {
	cleanup();
	server.resetHandlers();
	localStorage.clear();
});
afterAll(() => {
	globalThis.fetch = realFetch;
	server.close();
});
