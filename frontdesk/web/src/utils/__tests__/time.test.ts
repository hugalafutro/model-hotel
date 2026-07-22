import i18next from "i18next";
import { describe, expect, it } from "vitest";
import {
	formatAbsolute,
	formatHourTick,
	formatRelative,
	formatTimeOfDay,
} from "../time";

// The "never" sentinel is a translation lookup, so compare against the same key
// resolution rather than a hard-coded English string (repo i18n rule).
const NEVER = i18next.t("common.never");

describe("formatRelative", () => {
	it("returns the never sentinel for undefined, zero, and unparseable input", () => {
		expect(formatRelative(undefined)).toBe(NEVER);
		expect(formatRelative("1970-01-01T00:00:00Z")).toBe(NEVER);
		expect(formatRelative("not-a-date")).toBe(NEVER);
	});

	it("renders a real relative string for a valid timestamp", () => {
		const past = new Date(Date.now() - 5 * 60_000).toISOString();
		const out = formatRelative(past);
		expect(out).not.toBe(NEVER);
		expect(out.length).toBeGreaterThan(0);
	});

	it("falls back to seconds granularity for a near-now timestamp", () => {
		const out = formatRelative(new Date(Date.now() + 500).toISOString());
		expect(out).not.toBe(NEVER);
	});
});

describe("formatTimeOfDay", () => {
	it("returns the never sentinel for undefined and unparseable input", () => {
		expect(formatTimeOfDay(undefined)).toBe(NEVER);
		expect(formatTimeOfDay("garbage")).toBe(NEVER);
	});

	it("renders a non-empty wall-clock string for a valid timestamp", () => {
		const out = formatTimeOfDay("2026-06-01T13:45:30Z");
		expect(out).not.toBe(NEVER);
		expect(out.length).toBeGreaterThan(0);
	});
});

describe("formatHourTick", () => {
	it("returns the raw string unchanged when the value is unparseable", () => {
		expect(formatHourTick("not-a-date")).toBe("not-a-date");
	});

	it("renders a short hour:minute label for a valid timestamp", () => {
		const out = formatHourTick("2026-06-01T14:00:00Z");
		expect(out).not.toBe("2026-06-01T14:00:00Z");
		expect(out.length).toBeGreaterThan(0);
	});
});

describe("formatAbsolute", () => {
	it("returns the never sentinel for undefined and unparseable input", () => {
		expect(formatAbsolute(undefined)).toBe(NEVER);
		expect(formatAbsolute("garbage")).toBe(NEVER);
	});

	it("renders a non-empty date+time string for a valid timestamp", () => {
		const out = formatAbsolute("2026-06-01T14:00:00Z");
		expect(out).not.toBe(NEVER);
		expect(out.length).toBeGreaterThan(0);
	});
});
