import { describe, expect, it } from "vitest";
import {
	formatDate,
	formatDuration,
	formatNumber,
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
	formatTokens,
} from "./format";

describe("formatDuration", () => {
	it("formats milliseconds below 1s", () => {
		expect(formatDuration(0)).toBe("0ms");
		expect(formatDuration(500)).toBe("500ms");
		expect(formatDuration(999)).toBe("999ms");
	});

	it("formats seconds for 1s+", () => {
		expect(formatDuration(1000)).toBe("1.0s");
		expect(formatDuration(2500)).toBe("2.5s");
		expect(formatDuration(60000)).toBe("60.0s");
	});
});

describe("formatRelativeTime", () => {
	it("returns 'Never' for null", () => {
		expect(formatRelativeTime(null)).toBe("Never");
	});

	it("returns 'just now' for very recent dates", () => {
		expect(formatRelativeTime(new Date().toISOString())).toBe("just now");
	});

	it("returns minutes ago", () => {
		const fiveMinAgo = new Date(Date.now() - 5 * 60 * 1000).toISOString();
		expect(formatRelativeTime(fiveMinAgo)).toBe("5m ago");
	});

	it("returns hours ago", () => {
		const threeHoursAgo = new Date(
			Date.now() - 3 * 60 * 60 * 1000,
		).toISOString();
		expect(formatRelativeTime(threeHoursAgo)).toBe("3h ago");
	});

	it("returns days ago", () => {
		const twoDaysAgo = new Date(
			Date.now() - 2 * 24 * 60 * 60 * 1000,
		).toISOString();
		expect(formatRelativeTime(twoDaysAgo)).toBe("2d ago");
	});
});

describe("formatNumber", () => {
	it("returns '-' for null", () => {
		expect(formatNumber(null)).toBe("-");
	});

	it("returns '-' for undefined", () => {
		expect(formatNumber(undefined)).toBe("-");
	});

	it("formats numbers with locale", () => {
		const result = formatNumber(1000);
		expect(result).toMatch(/1.*000/);
	});
});

describe("formatTokens", () => {
	it("returns '-' for null", () => {
		expect(formatTokens(null)).toBe("-");
	});

	it("returns '-' for undefined", () => {
		expect(formatTokens(undefined)).toBe("-");
	});

	it("formats small numbers as-is", () => {
		expect(formatTokens(42)).toBe("42");
		expect(formatTokens(999)).toBe("999");
	});

	it("formats thousands with K suffix", () => {
		expect(formatTokens(1000)).toBe("1K");
		expect(formatTokens(1500)).toBe("1.5K");
		expect(formatTokens(999_000)).toBe("999K");
	});

	it("formats millions with M suffix", () => {
		expect(formatTokens(1_000_000)).toBe("1M");
		expect(formatTokens(2_500_000)).toBe("2.5M");
	});

	it("formats billions with B suffix", () => {
		expect(formatTokens(1_000_000_000)).toBe("1B");
		expect(formatTokens(3_200_000_000)).toBe("3.2B");
	});
});

describe("formatTimestamp", () => {
	it("returns a non-empty string for a valid date", () => {
		const result = formatTimestamp("2025-01-15T10:30:00Z");
		expect(result.length).toBeGreaterThan(0);
	});

	it("handles numeric timestamps", () => {
		const result = formatTimestamp(1_705_315_200_000);
		expect(result.length).toBeGreaterThan(0);
	});
});

describe("formatDate", () => {
	it("returns a non-empty string for a valid date", () => {
		const result = formatDate("2025-06-01T12:00:00Z");
		expect(result.length).toBeGreaterThan(0);
	});
});

describe("formatTimeUntil", () => {
	it("returns 'now' for past timestamps", () => {
		expect(formatTimeUntil(Date.now() - 1000)).toBe("now");
	});

	it("returns 'now' for current timestamp", () => {
		expect(formatTimeUntil(Date.now())).toBe("now");
	});

	it("formats hours only", () => {
		const inFiveHours = Date.now() + 5 * 60 * 60 * 1000;
		expect(formatTimeUntil(inFiveHours)).toBe("in 5 hours");
	});

	it("formats single hour correctly", () => {
		const inOneHour = Date.now() + 60 * 60 * 1000;
		expect(formatTimeUntil(inOneHour)).toBe("in 1 hour");
	});

	it("formats days and hours", () => {
		const inTwoDays = Date.now() + (2 * 24 + 3) * 60 * 60 * 1000;
		expect(formatTimeUntil(inTwoDays)).toBe("in 2 days, 3 hours");
	});

	it("formats 1 day 1 hour correctly", () => {
		const inOneDayOneHour = Date.now() + 25 * 60 * 60 * 1000;
		expect(formatTimeUntil(inOneDayOneHour)).toBe("in 1 day, 1 hour");
	});
});
