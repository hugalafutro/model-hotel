import { describe, expect, it } from "vitest";
import {
	formatCompact,
	formatDollars,
	formatKwh,
	formatTokens,
} from "../format";

describe("formatCompact", () => {
	it("returns a bare zero", () => {
		expect(formatCompact(0)).toBe("0");
	});

	it("leaves values under a thousand alone", () => {
		expect(formatCompact(999)).toBe("999");
	});

	it("abbreviates thousands, millions and billions", () => {
		expect(formatCompact(1_500)).toBe("1.5K");
		expect(formatCompact(2_000_000)).toBe("2M");
		expect(formatCompact(3_400_000_000)).toBe("3.4B");
	});

	it("drops a trailing .0", () => {
		expect(formatCompact(1_000)).toBe("1K");
	});

	it("abbreviates negatives by magnitude", () => {
		expect(formatCompact(-1_500)).toBe("-1.5K");
	});
});

describe("formatTokens", () => {
	it("renders a dash for null and undefined", () => {
		expect(formatTokens(null)).toBe("-");
		expect(formatTokens(undefined)).toBe("-");
	});

	it("renders zero as zero, not a dash", () => {
		expect(formatTokens(0)).toBe("0");
	});

	it("delegates to formatCompact", () => {
		expect(formatTokens(12_345)).toBe("12.3K");
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
