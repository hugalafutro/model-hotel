import { describe, expect, it } from "vitest";
import { convertLatexDelimiters } from "../../utils/latexDelimiters";

describe("convertLatexDelimiters", () => {
	it("converts display math \\[...\\] to $$...$$", () => {
		expect(convertLatexDelimiters("\\[x^2\\]")).toBe("$$x^2$$");
	});

	it("converts inline math \\(...\\) to $...$", () => {
		expect(convertLatexDelimiters("\\(x^2\\)")).toBe("$x^2$");
	});

	it("handles nested brackets inside display math (e.g. \\bigl[...\\bigr])", () => {
		expect(convertLatexDelimiters("\\[\\bigl[a\\bigr]\\]")).toBe(
			"$$\\bigl[a\\bigr]$$",
		);
	});

	it("handles multi-line display math", () => {
		expect(convertLatexDelimiters("Text\n\\[\nx^2 + y^2\n\\]\nMore")).toBe(
			"Text\n$$\nx^2 + y^2\n$$\nMore",
		);
	});

	it("converts multiple math blocks in one string", () => {
		expect(
			convertLatexDelimiters("Compute \\(a+b\\) then \\[c^2\\] done."),
		).toBe("Compute $a+b$ then $$c^2$$ done.");
	});

	it("does not convert markdown links", () => {
		expect(convertLatexDelimiters("[link](https://example.com)")).toBe(
			"[link](https://example.com)",
		);
	});

	it("preserves existing dollar-sign delimiters", () => {
		expect(convertLatexDelimiters("$x^2$ and \\(y^2\\)")).toBe(
			"$x^2$ and $y^2$",
		);
	});

	it("does not convert escaped delimiters \\\\[", () => {
		// Double backslash before [ means it's an escaped bracket, not a math delimiter
		expect(convertLatexDelimiters("\\\\\\[not math\\\\\\]")).toBe(
			"\\\\\\[not math\\\\\\]",
		);
	});

	it("handles mixed markdown links and display math", () => {
		expect(convertLatexDelimiters("[link](url) and \\[x^2\\]")).toBe(
			"[link](url) and $$x^2$$",
		);
	});

	it("handles the user-reported LLM output pattern", () => {
		const input =
			"Then \\[ I \\approx \\frac{0.1}{3}\\bigl[12.848858 + 4(37.924904)\\bigr] = 7.6046095. \\] Thus \\(\\text{Distance} = 2I\\).";
		const expected =
			"Then $$ I \\approx \\frac{0.1}{3}\\bigl[12.848858 + 4(37.924904)\\bigr] = 7.6046095. $$ Thus $\\text{Distance} = 2I$.";
		expect(convertLatexDelimiters(input)).toBe(expected);
	});

	it("returns plain text unchanged", () => {
		expect(convertLatexDelimiters("Hello world")).toBe("Hello world");
	});

	it("handles empty string", () => {
		expect(convertLatexDelimiters("")).toBe("");
	});

	it("handles consecutive inline math", () => {
		expect(convertLatexDelimiters("\\(a\\) and \\(b\\)")).toBe("$a$ and $b$");
	});

	it("handles display math with fractions and integrals", () => {
		expect(
			convertLatexDelimiters("\\[\\int_0^1 f(x)\\,dx = \\frac{1}{2}\\]"),
		).toBe("$$\\int_0^1 f(x)\\,dx = \\frac{1}{2}$$");
	});
});
