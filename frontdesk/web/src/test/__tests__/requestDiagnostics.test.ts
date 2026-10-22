import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";
import {
	formatRequestTrouble,
	requestTrouble,
	startTest,
} from "../requestDiagnostics";
import { server } from "../server";

// The shared setup installs the diagnostics and calls startTest() before each
// test, so these tests read the live record of their own requests.
describe("request diagnostics", () => {
	it("records error responses, handler exceptions and unhandled requests", async () => {
		server.use(
			http.get("/api/members", () => new HttpResponse("boom", { status: 500 })),
			http.get("/api/explodes", () => {
				throw new Error("kaboom");
			}),
		);
		await fetch("/api/members");
		await fetch("/api/explodes");
		await fetch("/api/unmocked", { method: "POST" }).catch(() => {});

		const lines = requestTrouble();
		expect(lines).toContain("500 GET /api/members");
		expect(lines).toContain("handler threw GET /api/explodes: Error: kaboom");
		expect(lines).toContain("unhandled POST /api/unmocked");
		expect(
			lines.some((l) => l.startsWith("fetch rejected POST /api/unmocked: ")),
		).toBe(true);
	});

	it("records nothing for successful requests", async () => {
		server.use(http.get("/api/members", () => HttpResponse.json([])));
		await fetch("/api/members");
		expect(requestTrouble()).toEqual([]);
	});

	it("marks a request that finishes after the test that made it", async () => {
		let release!: () => void;
		const gate = new Promise<void>((resolve) => {
			release = resolve;
		});
		server.use(
			http.get("/api/slow", async () => {
				await gate;
				return new HttpResponse(null, { status: 503 });
			}),
		);
		const pending = fetch("/api/slow");
		// What the shared setup does between two tests.
		startTest();
		release();
		await pending;
		expect(requestTrouble()).toEqual([
			"503 GET /api/slow (started in an earlier test)",
		]);
	});

	it("rethrows the original rejection even when it cannot be printed", async () => {
		const unprintable = Object.create(null);
		const rejection = await fetch("/api/members", {
			signal: AbortSignal.abort(unprintable),
		}).catch((err: unknown) => err);
		expect(rejection).toBe(unprintable);
		expect(requestTrouble()).toEqual([
			"fetch rejected GET /api/members: (unprintable)",
		]);
	});

	it("formats a report only when there is trouble", () => {
		expect(formatRequestTrouble([])).toBeNull();
		expect(formatRequestTrouble(["500 GET /a", "unhandled GET /b"])).toBe(
			"[request diagnostics] 2 event(s) in the failed test:\n  500 GET /a\n  unhandled GET /b",
		);
	});
});
