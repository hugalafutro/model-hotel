import { describe, expect, it } from "vitest";
import { truncateWithEllipsis } from "../truncate";

describe("truncateWithEllipsis", () => {
	it("returns the original string when shorter than maxLength", () => {
		expect(truncateWithEllipsis("hello", 10)).toBe("hello");
	});

	it("returns the original string when equal to maxLength", () => {
		expect(truncateWithEllipsis("hello", 5)).toBe("hello");
	});

	it("truncates and appends Unicode ellipsis", () => {
		expect(truncateWithEllipsis("hello world", 5)).toBe("hello\u2026");
	});

	it("strips trailing whitespace before adding ellipsis", () => {
		expect(truncateWithEllipsis("Z.ai Coding Pro", 12)).toBe(
			"Z.ai Coding\u2026",
		);
	});

	it("handles empty string", () => {
		expect(truncateWithEllipsis("", 5)).toBe("");
	});

	it("handles single character truncation", () => {
		expect(truncateWithEllipsis("ab", 1)).toBe("a\u2026");
	});

	it("does not produce space-before-ellipsis", () => {
		const result = truncateWithEllipsis("foo bar baz", 4);
		expect(result).not.toContain(" \u2026");
		expect(result).toBe("foo\u2026");
	});
});
