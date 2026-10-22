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
// error). Each test records its rejected fetches, error responses, handler
// exceptions and unhandled requests, and prints them only if it fails, so the
// next occurrence names its cause instead of a timeout on missing text.
//
// Outcomes (a rejection, or a status of 400 and up) are recorded by a fetch
// wrapper that stamps each call with the test that made it, so a straggler from
// an earlier test is kept but marked: it is itself a candidate cause. MSW's own
// events add the why (unhandled, handler threw); they are stamped with the test
// in which MSW started handling the request, which can trail the fetch call by a
// tick.
let testNumber = 0;
// Never pruned: a test file's requests number in the hundreds at most.
const startedIn = new Map<string, number>();
const requestTrouble: string[] = [];
const record = (startedInTest: number | undefined, line: string) => {
	const late =
		startedInTest !== undefined && startedInTest !== testNumber
			? " (started in an earlier test)"
			: "";
	requestTrouble.push(`${line}${late}`);
};
// Never throws: a diagnostic must not replace the error it describes.
const describe = (format: () => string) => {
	try {
		return format();
	} catch {
		return "(unprintable)";
	}
};
const where = (request: Request) =>
	describe(() => `${request.method} ${new URL(request.url).pathname}`);
const reason = (err: unknown) =>
	describe(() =>
		err instanceof Error
			? `${err.name}: ${err.message}${err.cause ? ` (cause: ${String(err.cause)})` : ""}`
			: String(err),
	);
let realFetch: typeof fetch;

// MSW lifecycle, shared by every test file. Unhandled requests error so a test
// that hits an unmocked endpoint fails loudly instead of silently.
beforeAll(() => {
	server.listen({ onUnhandledFrame: "error" });
	server.events.on("request:start", ({ requestId }) => {
		startedIn.set(requestId, testNumber);
	});
	server.events.on("request:unhandled", ({ requestId, request }) => {
		record(startedIn.get(requestId), `unhandled ${where(request)}`);
	});
	server.events.on("unhandledException", ({ requestId, request, error }) => {
		record(
			startedIn.get(requestId),
			`handler threw ${where(request)}: ${reason(error)}`,
		);
	});
	// Wrapped after listen(), so this sits outside MSW's own fetch patch and
	// sees every rejection a component's fetch receives.
	realFetch = globalThis.fetch;
	globalThis.fetch = async (input, init) => {
		const startedInTest = testNumber;
		// Resolved against the page: the app fetches relative paths, which a
		// bare Request constructor rejects outside a browser.
		const target = () =>
			describe(() => {
				const url = input instanceof Request ? input.url : String(input);
				const method =
					init?.method ?? (input instanceof Request ? input.method : "GET");
				return `${method.toUpperCase()} ${new URL(url, location.href).pathname}`;
			});
		let response: Response;
		try {
			response = await realFetch(input, init);
		} catch (err) {
			record(startedInTest, `fetch rejected ${target()}: ${reason(err)}`);
			throw err;
		}
		if (response.status >= 400) {
			record(startedInTest, `${response.status} ${target()}`);
		}
		return response;
	};
});
beforeEach(({ onTestFailed }) => {
	testNumber += 1;
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
