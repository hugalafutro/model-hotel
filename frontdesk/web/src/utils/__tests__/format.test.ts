import { describe, expect, it } from "vitest";
import { formatCount } from "../format";

// The compact/currency/kWh formatters are shared with the main dashboard and
// are covered in web/, which owns web-shared/ for coverage. formatCount is
// Front Desk's own, and exists precisely because it does NOT abbreviate.
describe("formatCount", () => {
	it("renders a dash for null and undefined", () => {
		expect(formatCount(null)).toBe("-");
		expect(formatCount(undefined)).toBe("-");
	});

	it("renders zero as zero, not a dash", () => {
		expect(formatCount(0)).toBe("0");
	});

	it("groups digits instead of abbreviating", () => {
		expect(formatCount(1_200)).toBe("1,200");
		expect(formatCount(1_249)).toBe("1,249");
	});

	it("rounds a fractional allowance to a whole item", () => {
		expect(formatCount(3.6)).toBe("4");
	});
});
