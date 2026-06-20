import { describe, expect, it } from "vitest";
import { formatDurationCell, isCancelled, liveDurationMs } from "../logHelpers";

describe("liveDurationMs", () => {
	it("returns the gap between created_at and now", () => {
		const created = "2026-06-20T12:00:00.000Z";
		const now = new Date("2026-06-20T12:00:03.500Z").getTime();
		expect(liveDurationMs(created, now)).toBe(3500);
	});

	it("clamps to 0 when now precedes created_at (clock skew)", () => {
		const created = "2026-06-20T12:00:05.000Z";
		const now = new Date("2026-06-20T12:00:00.000Z").getTime();
		expect(liveDurationMs(created, now)).toBe(0);
	});
});

describe("formatDurationCell", () => {
	it("formats sub-second durations as whole milliseconds", () => {
		expect(formatDurationCell(0)).toBe("0ms");
		expect(formatDurationCell(742)).toBe("742ms");
		expect(formatDurationCell(999)).toBe("999ms");
	});

	it("formats >= 1s durations as seconds with one decimal", () => {
		expect(formatDurationCell(1000)).toBe("1.0s");
		expect(formatDurationCell(3500)).toBe("3.5s");
	});
});

describe("isCancelled", () => {
	it("returns false for undefined", () => {
		expect(isCancelled()).toBe(false);
	});

	it("returns false for empty string", () => {
		expect(isCancelled("")).toBe(false);
	});

	it("returns false for unrelated error message", () => {
		expect(isCancelled("500 Internal Server Error")).toBe(false);
	});

	it("returns true for message containing cancel", () => {
		expect(isCancelled("context canceled")).toBe(true);
	});

	it("returns true for message containing disconnect", () => {
		expect(isCancelled("client disconnected")).toBe(true);
	});

	it("returns true for upstream request timed out", () => {
		expect(isCancelled("upstream request timed out")).toBe(true);
	});

	it("returns true for param-strip retry timed out", () => {
		expect(isCancelled("param-strip retry timed out")).toBe(true);
	});

	it("is case-insensitive", () => {
		expect(isCancelled("Context CANCELED")).toBe(true);
		expect(isCancelled("DISCONNECTED")).toBe(true);
	});

	it("returns true when keyword is part of longer message", () => {
		expect(isCancelled("the request was cancelled by the user")).toBe(true);
	});

	describe("error_kind (object form)", () => {
		it("returns true for interruption kinds", () => {
			expect(isCancelled({ error_kind: "client_disconnect" })).toBe(true);
			expect(isCancelled({ error_kind: "failover_timeout" })).toBe(true);
			expect(isCancelled({ error_kind: "retry_timeout" })).toBe(true);
		});

		it("returns false for provider failure kinds", () => {
			expect(isCancelled({ error_kind: "provider_error" })).toBe(false);
			expect(isCancelled({ error_kind: "provider_timeout" })).toBe(false);
			expect(isCancelled({ error_kind: "internal" })).toBe(false);
		});

		it("prefers error_kind over a misleading message", () => {
			// A provider_error whose message happens to contain 'disconnect'
			// must NOT be treated as an interruption — the kind wins.
			expect(
				isCancelled({
					error_kind: "provider_error",
					error_message: "upstream disconnect",
				}),
			).toBe(false);
		});

		it("falls back to message substring matching when kind is absent", () => {
			expect(isCancelled({ error_message: "client disconnected" })).toBe(true);
			expect(isCancelled({ error_message: "500 server error" })).toBe(false);
			expect(isCancelled({})).toBe(false);
		});
	});
});
