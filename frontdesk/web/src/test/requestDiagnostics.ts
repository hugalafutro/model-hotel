import { server } from "./server";

// Request diagnostics for a CI-only flake class: a mocked read fails during a
// page's mount burst and the page shows its error state, with nothing in the log
// saying why (the API client turns every rejected fetch into a bare "network"
// error). The shared setup records each test's request trouble and prints it
// only if the test fails, so the next occurrence names its cause instead of a
// timeout on missing text.
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
const trouble: string[] = [];
let realFetch: typeof fetch | undefined;

const record = (startedInTest: number | undefined, line: string) => {
	const late =
		startedInTest !== undefined && startedInTest !== testNumber
			? " (started in an earlier test)"
			: "";
	trouble.push(`${line}${late}`);
};

// Never throws: a diagnostic must not replace the error it describes.
const safely = (format: () => string) => {
	try {
		return format();
	} catch {
		return "(unprintable)";
	}
};
const where = (request: Request) =>
	safely(() => `${request.method} ${new URL(request.url).pathname}`);
const reason = (err: unknown) =>
	safely(() =>
		err instanceof Error
			? `${err.name}: ${err.message}${err.cause ? ` (cause: ${String(err.cause)})` : ""}`
			: String(err),
	);

// Call once per test file, after server.listen(): the fetch wrapper then sits
// outside MSW's own fetch patch and sees every rejection a component receives.
export function installRequestDiagnostics() {
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
	const wrapped = globalThis.fetch;
	realFetch = wrapped;
	globalThis.fetch = async (input, init) => {
		const startedInTest = testNumber;
		// Resolved against the page: the app fetches relative paths, which a
		// bare Request constructor rejects outside a browser.
		const target = () =>
			safely(() => {
				const url = input instanceof Request ? input.url : String(input);
				const method =
					init?.method ?? (input instanceof Request ? input.method : "GET");
				return `${method.toUpperCase()} ${new URL(url, location.href).pathname}`;
			});
		let response: Response;
		try {
			response = await wrapped(input, init);
		} catch (err) {
			record(startedInTest, `fetch rejected ${target()}: ${reason(err)}`);
			throw err;
		}
		if (response.status >= 400) {
			record(startedInTest, `${response.status} ${target()}`);
		}
		return response;
	};
}

export function uninstallRequestDiagnostics() {
	if (realFetch) globalThis.fetch = realFetch;
	realFetch = undefined;
}

// Begins a new test: clears the record and advances the stamp, so anything
// still in flight from the previous test reads as "started in an earlier test".
export function startTest() {
	testNumber += 1;
	trouble.length = 0;
}

export function requestTrouble(): readonly string[] {
	return [...trouble];
}

// The report printed for a failed test, or null when there is nothing to say.
export function formatRequestTrouble(lines: readonly string[]): string | null {
	if (lines.length === 0) return null;
	return `[request diagnostics] ${lines.length} event(s) in the failed test:\n  ${lines.join("\n  ")}`;
}
