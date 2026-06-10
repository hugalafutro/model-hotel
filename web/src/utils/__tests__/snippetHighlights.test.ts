import { describe, expect, it } from "vitest";
import { type SnippetToken, splitLineByHighlights } from "../snippetHighlights";

function text(segments: ReturnType<typeof splitLineByHighlights>) {
	return segments.map((s) => s.content).join("");
}

describe("splitLineByHighlights", () => {
	it("returns tokens unchanged when there are no targets", () => {
		const tokens: SnippetToken[] = [
			{ content: "curl -X POST ", color: "#aaa" },
			{ content: "http://x/v1", color: "#bbb" },
		];
		const segs = splitLineByHighlights(tokens, []);
		expect(segs).toEqual([
			{ content: "curl -X POST ", color: "#aaa", highlighted: false },
			{ content: "http://x/v1", color: "#bbb", highlighted: false },
		]);
	});

	it("highlights a target inside a single token", () => {
		const tokens: SnippetToken[] = [
			{ content: '"Authorization: Bearer YOUR_API_KEY"', color: "#ce9178" },
		];
		const segs = splitLineByHighlights(tokens, ["YOUR_API_KEY"]);
		expect(segs).toEqual([
			{
				content: '"Authorization: Bearer ',
				color: "#ce9178",
				highlighted: false,
			},
			{ content: "YOUR_API_KEY", color: "#ce9178", highlighted: true },
			{ content: '"', color: "#ce9178", highlighted: false },
		]);
		expect(text(segs)).toBe('"Authorization: Bearer YOUR_API_KEY"');
	});

	it("highlights a target spanning multiple tokens", () => {
		const tokens: SnippetToken[] = [
			{ content: "url = http://", color: "#aaa" },
			{ content: "host:8080", color: "#bbb" },
			{ content: "/v1", color: "#ccc" },
		];
		const segs = splitLineByHighlights(tokens, ["http://host:8080"]);
		expect(segs).toEqual([
			{ content: "url = ", color: "#aaa", highlighted: false },
			{ content: "http://", color: "#aaa", highlighted: true },
			{ content: "host:8080", color: "#bbb", highlighted: true },
			{ content: "/v1", color: "#ccc", highlighted: false },
		]);
		expect(text(segs)).toBe("url = http://host:8080/v1");
	});

	it("highlights every occurrence of a target", () => {
		const tokens: SnippetToken[] = [{ content: "model_name and model_name" }];
		const segs = splitLineByHighlights(tokens, ["model_name"]);
		expect(segs.filter((s) => s.highlighted)).toHaveLength(2);
		expect(text(segs)).toBe("model_name and model_name");
	});

	it("merges overlapping target ranges", () => {
		const tokens: SnippetToken[] = [{ content: "abcdef" }];
		const segs = splitLineByHighlights(tokens, ["abcd", "cdef"]);
		expect(segs).toEqual([{ content: "abcdef", highlighted: true }]);
	});

	it("ignores empty targets", () => {
		const tokens: SnippetToken[] = [{ content: "plain" }];
		const segs = splitLineByHighlights(tokens, [""]);
		expect(segs).toEqual([{ content: "plain", highlighted: false }]);
	});
});
