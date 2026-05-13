import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	countLabel,
	formatDate,
	formatDuration,
	formatNumber,
	formatRelativeTime,
	formatTimestamp,
	formatTimeUntil,
	formatTokens,
} from "../format";

describe("formatDuration", () => {
	it("returns milliseconds for values under 1000", () => {
		expect(formatDuration(0)).toBe("0ms");
		expect(formatDuration(500)).toBe("500ms");
		expect(formatDuration(999)).toBe("999ms");
	});

	it("returns seconds for values 1000 and above", () => {
		expect(formatDuration(1000)).toBe("1.0s");
		expect(formatDuration(1500)).toBe("1.5s");
		expect(formatDuration(2000)).toBe("2.0s");
		expect(formatDuration(2500)).toBe("2.5s");
		expect(formatDuration(60000)).toBe("60.0s");
	});
});

describe("formatRelativeTime", () => {
	it("returns 'Never' for null input", () => {
		expect(formatRelativeTime(null)).toBe("Never");
	});

	it("returns 'just now' for very recent dates", () => {
		const now = new Date();
		expect(formatRelativeTime(now.toISOString())).toBe("just now");
	});

	it("returns minutes ago for dates within the hour", () => {
		const date = new Date(Date.now() - 30 * 60 * 1000);
		expect(formatRelativeTime(date.toISOString())).toBe("30m ago");
	});

	it("returns hours ago for dates within the day", () => {
		const date = new Date(Date.now() - 3 * 60 * 60 * 1000);
		expect(formatRelativeTime(date.toISOString())).toBe("3h ago");
	});

	it("returns days ago for older dates", () => {
		const date = new Date(Date.now() - 5 * 24 * 60 * 60 * 1000);
		expect(formatRelativeTime(date.toISOString())).toBe("5d ago");
	});
});

describe("formatNumber", () => {
	it("returns '-' for null or undefined", () => {
		expect(formatNumber(null)).toBe("-");
		expect(formatNumber(undefined)).toBe("-");
	});

	it("formats numbers with locale separators", () => {
		expect(formatNumber(1000)).toBe("1,000");
		expect(formatNumber(1000000)).toBe("1,000,000");
		expect(formatNumber(1234567)).toBe("1,234,567");
	});

	it("handles zero", () => {
		expect(formatNumber(0)).toBe("0");
	});
});

describe("formatTokens", () => {
	it("returns '-' for null or undefined", () => {
		expect(formatTokens(null)).toBe("-");
		expect(formatTokens(undefined)).toBe("-");
	});

	it("formats small numbers as-is", () => {
		expect(formatTokens(0)).toBe("0");
		expect(formatTokens(500)).toBe("500");
		expect(formatTokens(999)).toBe("999");
	});

	it("formats thousands with K suffix", () => {
		expect(formatTokens(1000)).toBe("1K");
		expect(formatTokens(1500)).toBe("1.5K");
		expect(formatTokens(5000)).toBe("5K");
		expect(formatTokens(999000)).toBe("999K");
	});

	it("formats millions with M suffix", () => {
		expect(formatTokens(1000000)).toBe("1M");
		expect(formatTokens(1500000)).toBe("1.5M");
		expect(formatTokens(5000000)).toBe("5M");
	});

	it("formats billions with B suffix", () => {
		expect(formatTokens(1000000000)).toBe("1B");
		expect(formatTokens(1500000000)).toBe("1.5B");
		expect(formatTokens(2000000000)).toBe("2B");
	});
});

describe("formatTimestamp", () => {
	it("formats numeric timestamp", () => {
		const ts = new Date("2024-06-15T14:30:00Z").getTime();
		const result = formatTimestamp(ts);
		// Matches both en-GB ("15 Jun 2024") and en-US ("Jun 15, 2024")
		expect(result).toMatch(/15.*Jun.*2024|Jun.*15.*2024/);
		expect(result).toMatch(/\d{1,2}:\d{2}/);
	});

	it("formats string timestamp", () => {
		const result = formatTimestamp("2024-06-15T14:30:00Z");
		expect(result).toMatch(/15.*Jun.*2024|Jun.*15.*2024/);
		expect(result).toMatch(/\d{1,2}:\d{2}/);
	});
});

describe("countLabel", () => {
	it("returns plural form when count is 0", () => {
		expect(countLabel(0, "Model", "Models")).toBe("Models");
		expect(countLabel(0, "Request", "Requests")).toBe("Requests");
	});

	it("returns plural form when count is undefined", () => {
		expect(countLabel(undefined, "Model", "Models")).toBe("Models");
	});

	it("returns singular form with '1' when count is 1", () => {
		expect(countLabel(1, "Model", "Models")).toBe("1 Model");
		expect(countLabel(1, "Request", "Requests")).toBe("1 Request");
	});

	it("returns plural form with count when count > 1", () => {
		expect(countLabel(2, "Model", "Models")).toBe("2 Models");
		expect(countLabel(5, "Request", "Requests")).toBe("5 Requests");
		expect(countLabel(100, "Item", "Items")).toBe("100 Items");
	});
});

describe("formatDate", () => {
	it("formats numeric timestamp", () => {
		const ts = new Date("2024-06-15T00:00:00Z").getTime();
		const result = formatDate(ts);
		// Matches both en-GB ("15 Jun 2024") and en-US ("Jun 15, 2024")
		expect(result).toMatch(/15.*Jun.*2024|Jun.*15.*2024/);
	});

	it("formats string timestamp", () => {
		const result = formatDate("2024-12-25T00:00:00Z");
		expect(result).toMatch(/25.*Dec.*2024|Dec.*25.*2024/);
	});

	it("formats date with different month", () => {
		const result = formatDate("2024-01-01T00:00:00Z");
		expect(result).toMatch(/1.*Jan.*2024|Jan.*1.*2024/);
	});

	it("handles current year correctly", () => {
		const now = new Date();
		const result = formatDate(now.toISOString());
		expect(result).toContain(now.getFullYear().toString());
	});
});

describe("formatTimeUntil", () => {
	beforeEach(() => {
		vi.useFakeTimers();
		vi.setSystemTime(new Date("2024-06-15T12:00:00Z"));
	});

	afterEach(() => {
		vi.useRealTimers();
	});

	it("returns 'now' when timestamp is in the past", () => {
		const past = Date.now() - 1000;
		expect(formatTimeUntil(past)).toBe("now");
	});

	it("returns 'now' when timestamp is exactly now", () => {
		expect(formatTimeUntil(Date.now())).toBe("now");
	});

	it("returns hours for timestamps less than a day away", () => {
		const oneHour = Date.now() + 1000 * 60 * 60;
		expect(formatTimeUntil(oneHour)).toBe("in 1 hour");

		const threeHours = Date.now() + 3 * 1000 * 60 * 60;
		expect(formatTimeUntil(threeHours)).toBe("in 3 hours");

		const twentyThreeHours = Date.now() + 23 * 1000 * 60 * 60;
		expect(formatTimeUntil(twentyThreeHours)).toBe("in 23 hours");
	});

	it("returns days and hours for timestamps more than a day away", () => {
		const oneDay = Date.now() + 24 * 1000 * 60 * 60;
		expect(formatTimeUntil(oneDay)).toBe("in 1 day, 0 hours");

		const oneDayOneHour = Date.now() + 25 * 1000 * 60 * 60;
		expect(formatTimeUntil(oneDayOneHour)).toBe("in 1 day, 1 hour");

		const twoDaysThreeHours = Date.now() + (2 * 24 + 3) * 1000 * 60 * 60;
		expect(formatTimeUntil(twoDaysThreeHours)).toBe("in 2 days, 3 hours");
	});

	it("handles singular/plural correctly for days and hours", () => {
		const oneDayZeroHours = Date.now() + 24 * 1000 * 60 * 60;
		expect(formatTimeUntil(oneDayZeroHours)).toBe("in 1 day, 0 hours");

		const oneDayOneHour = Date.now() + 25 * 1000 * 60 * 60;
		expect(formatTimeUntil(oneDayOneHour)).toBe("in 1 day, 1 hour");

		const twoDaysOneHour = Date.now() + (2 * 24 + 1) * 1000 * 60 * 60;
		expect(formatTimeUntil(twoDaysOneHour)).toBe("in 2 days, 1 hour");
	});
});
