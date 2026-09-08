import { describe, expect, it } from "vitest";
import {
	formatDurationCell,
	formatMs,
	formatTPS,
	isCancelled,
	liveDurationMs,
} from "../logHelpers";

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

	it("returns 0 (never NaN) for an unparseable created_at", () => {
		expect(liveDurationMs("", Date.now())).toBe(0);
		expect(liveDurationMs("not-a-date", Date.now())).toBe(0);
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

	it("returns false for an empty message", () => {
		expect(isCancelled({ error_message: "" })).toBe(false);
	});

	it("returns false for an unrelated error message", () => {
		expect(isCancelled({ error_message: "500 Internal Server Error" })).toBe(
			false,
		);
	});

	it("returns true for message containing cancel", () => {
		expect(isCancelled({ error_message: "context canceled" })).toBe(true);
	});

	it("returns true for message containing disconnect", () => {
		expect(isCancelled({ error_message: "client disconnected" })).toBe(true);
	});

	it("returns true for upstream request timed out", () => {
		expect(isCancelled({ error_message: "upstream request timed out" })).toBe(
			true,
		);
	});

	it("returns true for param-strip retry timed out", () => {
		expect(isCancelled({ error_message: "param-strip retry timed out" })).toBe(
			true,
		);
	});

	it("is case-insensitive", () => {
		expect(isCancelled({ error_message: "Context CANCELED" })).toBe(true);
		expect(isCancelled({ error_message: "DISCONNECTED" })).toBe(true);
	});

	it("returns true when keyword is part of longer message", () => {
		expect(
			isCancelled({ error_message: "the request was cancelled by the user" }),
		).toBe(true);
	});

	describe("error_kind (object form)", () => {
		it("returns true for interruption kinds", () => {
			expect(isCancelled({ error_kind: "client_disconnect" })).toBe(true);
			expect(isCancelled({ error_kind: "hedge_superseded" })).toBe(true);
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

describe("formatTPS", () => {
	it("returns '-' for null", () => {
		expect(formatTPS(null)).toBe("-");
	});

	it("returns '-' for zero", () => {
		expect(formatTPS(0)).toBe("-");
	});

	it("formats 45.5 as '45.5'", () => {
		expect(formatTPS(45.5)).toBe("45.5");
	});

	it("formats 1000.123 as '1000.1' (1 decimal)", () => {
		expect(formatTPS(1000.123)).toBe("1000.1");
	});

	it("returns '-' for undefined", () => {
		expect(formatTPS(undefined as unknown as null)).toBe("-");
	});
});

describe("formatMs", () => {
	it("returns '-' for null", () => {
		expect(formatMs(null)).toBe("-");
	});

	it("returns '-' for undefined", () => {
		expect(formatMs(undefined)).toBe("-");
	});

	it("returns '-' for zero", () => {
		expect(formatMs(0)).toBe("-");
	});

	it("formats number with 2 decimals by default", () => {
		expect(formatMs(100)).toBe("100.00ms");
		expect(formatMs(100.5)).toBe("100.50ms");
		expect(formatMs(100.123)).toBe("100.12ms");
	});

	it("respects custom decimals parameter", () => {
		expect(formatMs(100, 0)).toBe("100ms");
		expect(formatMs(100, 1)).toBe("100.0ms");
		expect(formatMs(100, 3)).toBe("100.000ms");
		expect(formatMs(100.1234, 3)).toBe("100.123ms");
	});

	it("handles small values", () => {
		expect(formatMs(0.5)).toBe("0.50ms");
		expect(formatMs(0.001)).toBe("0.00ms");
	});

	it("handles large values", () => {
		expect(formatMs(1000)).toBe("1000.00ms");
		expect(formatMs(10000.5)).toBe("10000.50ms");
	});
});
