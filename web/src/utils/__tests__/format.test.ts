import i18next from "i18next";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
	countLabel,
	dropTrailingZero,
	encodeCursor,
	formatCompact,
	formatDate,
	formatDollars,
	formatDuration,
	formatKwh,
	formatNumber,
	formatPercent,
	formatRelativeTime,
	formatTime,
	formatTimestamp,
	formatTimeUntil,
	formatTokens,
	formatWithCommas,
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

describe("formatWithCommas", () => {
	it("formats integers with locale separators", () => {
		expect(formatWithCommas(0)).toBe("0");
		expect(formatWithCommas(999)).toBe("999");
		expect(formatWithCommas(1000)).toBe("1,000");
		expect(formatWithCommas(1000000)).toBe("1,000,000");
		expect(formatWithCommas(1234567)).toBe("1,234,567");
	});

	it("rounds fractional values", () => {
		expect(formatWithCommas(1234.5)).toBe("1,235");
		expect(formatWithCommas(0.9)).toBe("1");
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
	// Synthetic keys, so the assertions own their text and never depend on a
	// repo translation that a later wording change would break. The Russian
	// forms are deliberately all distinct: that is what makes a wrong category
	// visible.
	const KEY = "test.countLabelFixture";
	const EN = {
		[`${KEY}_one`]: "Model",
		[`${KEY}_other`]: "Models",
	};
	const RU = {
		[`${KEY}_one`]: "RU-ONE",
		[`${KEY}_few`]: "RU-FEW",
		[`${KEY}_many`]: "RU-MANY",
		[`${KEY}_other`]: "RU-OTHER",
	};

	beforeEach(() => {
		i18next.addResourceBundle("en", "translation", EN, true, true);
		i18next.addResourceBundle("ru", "translation", RU, true, true);
	});

	afterEach(async () => {
		await i18next.changeLanguage("en");
	});

	it("names the collection without a numeral when the count is 0", () => {
		expect(countLabel(0, KEY)).toBe("Models");
	});

	it("names the collection without a numeral when the count is undefined", () => {
		expect(countLabel(undefined, KEY)).toBe("Models");
	});

	it("prefixes the numeral and takes the singular at 1", () => {
		expect(countLabel(1, KEY)).toBe("1 Model");
	});

	it("prefixes the numeral and takes the plural above 1", () => {
		expect(countLabel(2, KEY)).toBe("2 Models");
		expect(countLabel(100, KEY)).toBe("100 Models");
	});

	it("resolves the category through the active language, not the system locale", async () => {
		// The regression this guards: picking the form with a bare
		// `new Intl.PluralRules()` uses the host's locale, and collapsing the
		// result to one/other can never reach _few or _many. Under en rules
		// both 2 and 5 would land on _other; Russian needs a third form at 2
		// and a fourth at 5.
		await i18next.changeLanguage("ru");
		expect(countLabel(1, KEY)).toBe("1 RU-ONE");
		expect(countLabel(2, KEY)).toBe("2 RU-FEW");
		expect(countLabel(5, KEY)).toBe("5 RU-MANY");
		expect(countLabel(22, KEY)).toBe("22 RU-FEW");
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

describe("formatTime", () => {
	it("formats timestamp as HH:MM", () => {
		const ts = new Date("2024-01-15T14:30:00Z").getTime();
		const result = formatTime(ts);

		expect(result).toMatch(/\d{1,2}:\d{2}/);
		expect(result).toContain(":");
	});

	it("formats midnight correctly", () => {
		const ts = new Date("2024-01-15T00:00:00Z").getTime();
		const result = formatTime(ts);

		// Matches 00:00 (24h) or 12:00 AM (12h)
		expect(result).toMatch(/00:00|12:00\s*AM/i);
	});

	it("formats noon correctly", () => {
		const ts = new Date("2024-01-15T12:00:00Z").getTime();
		const result = formatTime(ts);

		// Matches 12:00 (24h) or 12:00 PM (12h)
		expect(result).toMatch(/12:00/);
	});

	it("accepts a date string as well as a numeric timestamp", () => {
		const result = formatTime("2024-01-15T14:30:00Z");

		expect(typeof result).toBe("string");
		expect(result).toContain(":");
	});
});

describe("formatPercent", () => {
	it("shows <0.1% for zero", () => {
		expect(formatPercent(0)).toBe("<0.1%");
	});

	it("shows <0.1% for tiny non-zero shares", () => {
		expect(formatPercent(0.001)).toBe("<0.1%");
		expect(formatPercent(0.02)).toBe("<0.1%");
		expect(formatPercent(0.04)).toBe("<0.1%");
	});

	it("shows 0.1% at the rounding boundary (0.05)", () => {
		expect(formatPercent(0.05)).toBe("0.1%");
		expect(formatPercent(0.049)).toBe("<0.1%");
	});

	it("formats normal percentages with one decimal", () => {
		expect(formatPercent(0.1)).toBe("0.1%");
		expect(formatPercent(1)).toBe("1.0%");
		expect(formatPercent(21.4)).toBe("21.4%");
		expect(formatPercent(76.6)).toBe("76.6%");
		expect(formatPercent(100)).toBe("100.0%");
	});
});

describe("encodeCursor", () => {
	it("encodes a value via JSON.stringify then base64", () => {
		const result = encodeCursor("hello");
		expect(atob(result)).toBe(JSON.stringify("hello"));
	});

	it("encodes numbers", () => {
		const result = encodeCursor(42);
		expect(atob(result)).toBe("42");
	});

	it("encodes objects as JSON then base64", () => {
		const result = encodeCursor({ id: 1, name: "test" });
		expect(JSON.parse(atob(result))).toEqual({ id: 1, name: "test" });
	});

	it("handles Unicode characters safely", () => {
		const input = "héllo wörld";
		const result = encodeCursor(input);
		// decode: base64 → percent-encoded string → URI-decode → JSON-parse
		const jsonStr = decodeURIComponent(
			Array.from(
				atob(result),
				(c) => `%${`00${c.charCodeAt(0).toString(16)}`.slice(-2)}`,
			).join(""),
		);
		expect(JSON.parse(jsonStr)).toBe(input);
	});

	it("encodes null and arrays", () => {
		expect(JSON.parse(atob(encodeCursor(null)))).toBeNull();
		expect(JSON.parse(atob(encodeCursor([1, 2, 3])))).toEqual([1, 2, 3]);
	});
});

describe("formatCompact", () => {
	it("returns '0' for zero", () => {
		expect(formatCompact(0)).toBe("0");
	});

	it("formats small numbers with one decimal place", () => {
		expect(formatCompact(1.5)).toBe("1.5");
		expect(formatCompact(42)).toBe("42");
		expect(formatCompact(999)).toBe("999");
	});

	it("drops trailing .0 for whole numbers", () => {
		expect(formatCompact(1)).toBe("1");
		expect(formatCompact(100)).toBe("100");
	});

	it("formats thousands with K suffix", () => {
		expect(formatCompact(1000)).toBe("1K");
		expect(formatCompact(1500)).toBe("1.5K");
		expect(formatCompact(999000)).toBe("999K");
	});

	it("formats millions with M suffix", () => {
		expect(formatCompact(1_000_000)).toBe("1M");
		expect(formatCompact(1_500_000)).toBe("1.5M");
		expect(formatCompact(25_000_000)).toBe("25M");
	});

	it("formats billions with B suffix", () => {
		expect(formatCompact(3_400_000_000)).toBe("3.4B");
	});

	it("handles negative numbers", () => {
		expect(formatCompact(-1500)).toBe("-1.5K");
		expect(formatCompact(-1_000_000)).toBe("-1M");
	});
});

describe("formatDollars", () => {
	it("renders a USD amount", () => {
		expect(formatDollars(12.5)).toBe("$12.50");
	});
});

describe("formatKwh", () => {
	it("caps at two decimal places", () => {
		// Deliberately not a digit sequence of pi: biome's
		// lint/suspicious/noApproximativeNumericConstant flags that literal.
		expect(formatKwh(7.86432)).toBe("7.86");
	});
});

describe("dropTrailingZero", () => {
	it("drops trailing zeros after decimal", () => {
		expect(dropTrailingZero(1.5, 2)).toBe("1.5");
		expect(dropTrailingZero(1.0, 2)).toBe("1");
		expect(dropTrailingZero(1.25, 2)).toBe("1.25");
	});

	it("drops decimal point when all trailing zeros", () => {
		expect(dropTrailingZero(5.0, 1)).toBe("5");
		expect(dropTrailingZero(10.0, 3)).toBe("10");
	});

	it("preserves integer format with zero decimals", () => {
		expect(dropTrailingZero(42, 0)).toBe("42");
		expect(dropTrailingZero(3, 0)).toBe("3");
	});

	it("keeps non-zero decimals", () => {
		expect(dropTrailingZero(3.14, 2)).toBe("3.14");
		expect(dropTrailingZero(0.01, 2)).toBe("0.01");
	});

	it("handles mixed trailing zeros", () => {
		expect(dropTrailingZero(1.2, 3)).toBe("1.2");
		expect(dropTrailingZero(1.23, 4)).toBe("1.23");
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

	// Sub-hour resets are minutes, not "0 hours". Kimi's 5-hour window and the
	// Z.ai ones routinely sit inside the last hour before a reset.
	it("returns minutes for timestamps less than an hour away", () => {
		expect(formatTimeUntil(Date.now() + 60 * 1000)).toBe("in 1 minute");
		expect(formatTimeUntil(Date.now() + 30 * 60 * 1000)).toBe("in 30 minutes");
		expect(formatTimeUntil(Date.now() + 59 * 60 * 1000)).toBe("in 59 minutes");
	});

	it("rounds a sub-minute gap up to one minute rather than zero", () => {
		expect(formatTimeUntil(Date.now() + 1000)).toBe("in 1 minute");
		expect(formatTimeUntil(Date.now() + 59 * 1000)).toBe("in 1 minute");
	});

	it("switches to hours at exactly one hour", () => {
		// The hour strings come from i18next, which glues the number to its unit
		// with a non-breaking space; the Intl minute strings above use a plain one.
		expect(formatTimeUntil(Date.now() + 60 * 60 * 1000)).toBe("in 1\u00a0hour");
		expect(formatTimeUntil(Date.now() + 61 * 60 * 1000)).toBe("in 1\u00a0hour");
	});

	// Each number is glued to its unit word with a non-breaking space ( )
	// so a line wrap can't strand the digit from "day(s)"/"hour(s)".
	it("returns hours for timestamps less than a day away", () => {
		const oneHour = Date.now() + 1000 * 60 * 60;
		expect(formatTimeUntil(oneHour)).toBe("in 1\u00a0hour");

		const threeHours = Date.now() + 3 * 1000 * 60 * 60;
		expect(formatTimeUntil(threeHours)).toBe("in 3\u00a0hours");

		const twentyThreeHours = Date.now() + 23 * 1000 * 60 * 60;
		expect(formatTimeUntil(twentyThreeHours)).toBe("in 23\u00a0hours");
	});

	it("returns days and hours for timestamps more than a day away", () => {
		const oneDay = Date.now() + 24 * 1000 * 60 * 60;
		expect(formatTimeUntil(oneDay)).toBe("in 1\u00a0day, 0\u00a0hours");

		const oneDayOneHour = Date.now() + 25 * 1000 * 60 * 60;
		expect(formatTimeUntil(oneDayOneHour)).toBe("in 1\u00a0day, 1\u00a0hour");

		const twoDaysThreeHours = Date.now() + (2 * 24 + 3) * 1000 * 60 * 60;
		expect(formatTimeUntil(twoDaysThreeHours)).toBe(
			"in 2\u00a0days, 3\u00a0hours",
		);
	});

	it("handles singular/plural correctly for days and hours", () => {
		const oneDayZeroHours = Date.now() + 24 * 1000 * 60 * 60;
		expect(formatTimeUntil(oneDayZeroHours)).toBe(
			"in 1\u00a0day, 0\u00a0hours",
		);

		const oneDayOneHour = Date.now() + 25 * 1000 * 60 * 60;
		expect(formatTimeUntil(oneDayOneHour)).toBe("in 1\u00a0day, 1\u00a0hour");

		const twoDaysOneHour = Date.now() + (2 * 24 + 1) * 1000 * 60 * 60;
		expect(formatTimeUntil(twoDaysOneHour)).toBe("in 2\u00a0days, 1\u00a0hour");
	});
});
